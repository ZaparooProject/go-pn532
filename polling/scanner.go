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
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/go-pn532"
)

// Scanner provides a high-level interface for continuous NFC tag scanning
// with coordinated write operations. It wraps the lower-level Monitor to
// provide thread-safe, user-friendly scanning functionality.
type Scanner struct {
	device        *pn532.Device
	config        *ScanConfig
	monitor       *Monitor
	pendingWrite  atomic.Pointer[WriteRequest]
	cancelFunc    context.CancelFunc
	OnTagDetected func(*pn532.DetectedTag) error
	OnTagRemoved  func()
	OnTagChanged  func(*pn532.DetectedTag) error
	writeMutex    sync.Mutex
	stopMutex     sync.Mutex
	running       atomic.Bool
}

// ScanConfig holds configuration options for the Scanner
type ScanConfig struct {
	PollInterval       time.Duration
	CardRemovalTimeout time.Duration

	// Advanced configuration
	MaxRetries   int
	RetryBackoff time.Duration
}

// WriteRequest represents a pending write operation
type WriteRequest struct {
	operation func(pn532.Tag) error
	result    chan error
	ctx       context.Context
	createdAt time.Time
}

// Scanner-specific errors
var (
	ErrWriteAlreadyPending = errors.New("write operation already pending")
	ErrScannerNotRunning   = errors.New("scanner is not running")
	ErrScannerStopped      = errors.New("scanner was stopped")
)

// NewScanner creates a new scanner instance with the given device and configuration
func NewScanner(device *pn532.Device, config *ScanConfig) (*Scanner, error) {
	if device == nil {
		return nil, errors.New("device cannot be nil")
	}
	if config == nil {
		config = DefaultScanConfig()
	}

	return &Scanner{
		device: device,
		config: config,
	}, nil
}

// DefaultScanConfig returns sensible default configuration values
func DefaultScanConfig() *ScanConfig {
	return &ScanConfig{
		PollInterval:       250 * time.Millisecond,
		CardRemovalTimeout: 2 * time.Second,
		MaxRetries:         3,
		RetryBackoff:       100 * time.Millisecond,
	}
}

// Start begins continuous scanning (non-blocking)
// Returns an error if the scanner is already running or if device initialization fails
func (s *Scanner) Start(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		return errors.New("scanner is already running")
	}

	// Create cancellable context for internal operations
	scanCtx, cancel := context.WithCancel(ctx)
	s.stopMutex.Lock()
	s.cancelFunc = cancel
	s.stopMutex.Unlock()

	// Start scanning in background goroutine
	go func() {
		defer func() {
			s.running.Store(false)
			s.stopMutex.Lock()
			s.cancelFunc = nil
			s.stopMutex.Unlock()
		}()

		if err := s.startScanning(scanCtx); err != nil && !errors.Is(err, context.Canceled) {
			// TODO: Consider adding error callback or logging
			_ = err
		}
	}()

	return nil
}

// Stop gracefully stops the scanner
// Blocks until the scanner has fully stopped
func (s *Scanner) Stop() error {
	if !s.running.Load() {
		return nil
	}

	s.stopMutex.Lock()
	cancelFunc := s.cancelFunc
	s.stopMutex.Unlock()

	if cancelFunc != nil {
		cancelFunc()
	}

	// Wait for scanner to stop
	for s.running.Load() {
		time.Sleep(10 * time.Millisecond)
	}

	return nil
}

// IsRunning returns whether the scanner is currently active
func (s *Scanner) IsRunning() bool {
	return s.running.Load()
}

// HasPendingWrite returns true if a write operation is waiting
func (s *Scanner) HasPendingWrite() bool {
	return s.pendingWrite.Load() != nil
}
