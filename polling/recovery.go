// Copyright 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package polling

import (
	"context"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/internal/syncutil"
)

// DeviceRecoverer handles device recovery after sleep/wake or errors
type DeviceRecoverer interface {
	// AttemptRecovery tries to recover the device connection.
	// Returns nil if recovery was successful, error otherwise.
	AttemptRecovery(ctx context.Context) error

	// GetDevice returns the current device reference (may change after reconnection)
	GetDevice() *pn532.Device
}

// ReopenFunc is a function that attempts to reopen/reconnect the device
type ReopenFunc func() (*pn532.Device, error)

// DefaultRecoverer implements a tiered recovery strategy:
// 1. Soft reset via SAMConfiguration
// 2. Full reconnection via user-provided reopen function
type DefaultRecoverer struct {
	device      *pn532.Device
	reopenFunc  ReopenFunc
	backoff     time.Duration
	maxAttempts int
	mu          syncutil.Mutex
}

// NewDefaultRecoverer creates a recoverer with tiered recovery strategy.
// If reopenFunc is nil, only soft reset will be attempted.
func NewDefaultRecoverer(
	device *pn532.Device,
	reopenFunc ReopenFunc,
	backoff time.Duration,
	maxAttempts int,
) *DefaultRecoverer {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}
	return &DefaultRecoverer{
		device:      device,
		reopenFunc:  reopenFunc,
		backoff:     backoff,
		maxAttempts: maxAttempts,
	}
}

// AttemptRecovery implements tiered recovery:
// 1. Try soft reset (SAMConfiguration) - works if USB port still valid
// 2. If that fails and reopenFunc is provided, try full reconnection
func (r *DefaultRecoverer) AttemptRecovery(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error

	for attempt := range r.maxAttempts {
		if attempt > 0 {
			timer := time.NewTimer(r.backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}

		// Tier 1: Soft reset via SAMConfiguration
		err := r.device.SAMConfiguration(ctx, pn532.SAMNormal, 0, 0)
		if err == nil {
			return nil // Success!
		}
		lastErr = err

		// Tier 2: Full reconnection (if reopenFunc provided)
		if r.reopenFunc != nil {
			_ = r.device.Close()
			newDevice, reopenErr := r.reopenFunc()
			if reopenErr == nil {
				r.device = newDevice
				return nil
			}
			lastErr = reopenErr
		}
	}

	return lastErr
}

// GetDevice returns the current device reference.
// This may return a different device after a successful reconnection.
func (r *DefaultRecoverer) GetDevice() *pn532.Device {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.device
}
