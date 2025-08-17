// go-pn532
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of go-pn532.
//
// go-pn532 is free software; you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public
// License as published by the Free Software Foundation; either
// version 3 of the License, or (at your option) any later version.
//
// go-pn532 is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
// Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with go-pn532; if not, write to the Free Software Foundation,
// Inc., 51 Franklin Street, Fifth Floor, Boston, MA  02110-1301, USA.

package polling

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/go-pn532"
)

// DeviceCallbacks defines callback functions for device events
type DeviceCallbacks struct {
	OnCardDetected func(tag *pn532.DetectedTag) error
	OnCardRemoved  func()
	OnCardChanged  func(tag *pn532.DetectedTag) error
}

// DeviceMetrics tracks operational metrics for DeviceActor
type DeviceMetrics struct {
	PollCycles      int64         // Total number of polling cycles
	PollErrors      int64         // Number of polling errors
	CardsDetected   int64         // Number of cards detected
	CallbackErrors  int64         // Number of callback errors
	LastPollLatency time.Duration // Duration of last polling operation
}

// DeviceActor minimal implementation to make the test pass
type DeviceActor struct {
	device    *pn532.Device
	config    *Config
	callbacks DeviceCallbacks
	stopChan  chan struct{}
	// Atomic counters for metrics
	pollCycles      int64
	cardsDetected   int64
	pollErrors      int64
	lastPollLatency int64 // in nanoseconds
	// Adaptive polling state
	currentInterval   int64 // Current polling interval in nanoseconds
	lastCardDetection int64 // Timestamp of last card detection
}

// NewDeviceActor creates a new device actor (minimal implementation to pass test)
func NewDeviceActor(device *pn532.Device, config *Config, callbacks DeviceCallbacks) *DeviceActor {
	now := time.Now().UnixNano()
	return &DeviceActor{
		device:            device,
		config:            config,
		callbacks:         callbacks,
		stopChan:          make(chan struct{}),
		currentInterval:   config.PollInterval.Nanoseconds(),
		lastCardDetection: now,
	}
}

// Start minimal implementation to pass test
func (da *DeviceActor) Start(_ context.Context) error {
	// Start continuous polling in a goroutine
	go da.pollLoop()
	return nil
}

// pollLoop runs continuous polling until stopped
func (da *DeviceActor) pollLoop() {
	ticker := time.NewTicker(da.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if da.device == nil || da.callbacks.OnCardDetected == nil {
				continue
			}

			start := time.Now()
			detectedTags, err := da.device.InitiatorListPassiveTargets(1, pn532.TagTypeAny, nil)
			pollDuration := time.Since(start)

			// Track poll cycle
			atomic.AddInt64(&da.pollCycles, 1)
			atomic.StoreInt64(&da.lastPollLatency, pollDuration.Nanoseconds())

			if err != nil {
				// Track poll error
				atomic.AddInt64(&da.pollErrors, 1)
			}

			if err == nil && len(detectedTags) > 0 {
				// Track card detected and update timestamp
				atomic.AddInt64(&da.cardsDetected, 1)
				atomic.StoreInt64(&da.lastCardDetection, start.UnixNano())
				_ = da.callbacks.OnCardDetected(detectedTags[0])
			}

			// Adaptive polling: adjust interval based on card presence
			da.adjustPollInterval()

			// Update ticker with new interval
			newInterval := time.Duration(atomic.LoadInt64(&da.currentInterval))
			ticker.Reset(newInterval)
		case <-da.stopChan:
			return
		}
	}
}

// adjustPollInterval implements adaptive polling logic
func (da *DeviceActor) adjustPollInterval() {
	now := time.Now().UnixNano()
	lastDetection := atomic.LoadInt64(&da.lastCardDetection)
	timeSinceLastCard := time.Duration(now - lastDetection)

	// If no card detected for 5+ seconds, slow down polling
	if timeSinceLastCard > 5*time.Second {
		// Increase interval to 5x the original (up to 500ms max)
		slowInterval := da.config.PollInterval * 5
		if slowInterval > 500*time.Millisecond {
			slowInterval = 500 * time.Millisecond
		}
		atomic.StoreInt64(&da.currentInterval, slowInterval.Nanoseconds())
	} else {
		// Card detected recently, use normal speed
		atomic.StoreInt64(&da.currentInterval, da.config.PollInterval.Nanoseconds())
	}
}

// Stop minimal implementation to pass test
func (*DeviceActor) Stop(_ context.Context) error {
	return nil
}

// GetMetrics returns current operational metrics
func (da *DeviceActor) GetMetrics() DeviceMetrics {
	return DeviceMetrics{
		PollCycles:      atomic.LoadInt64(&da.pollCycles),
		PollErrors:      atomic.LoadInt64(&da.pollErrors),
		CardsDetected:   atomic.LoadInt64(&da.cardsDetected),
		LastPollLatency: time.Duration(atomic.LoadInt64(&da.lastPollLatency)),
	}
}

// GetCurrentPollInterval returns the current adaptive polling interval
func (da *DeviceActor) GetCurrentPollInterval() time.Duration {
	return time.Duration(atomic.LoadInt64(&da.currentInterval))
}
