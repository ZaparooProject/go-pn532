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

// Package transport provides internal transport utilities
package transport

import (
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
)

// RetryOperation represents a function that can be retried
// Returns: data, shouldRetry, error
// - data: the result if successful
// - shouldRetry: true if the operation should be retried
// - error: any permanent error that should stop retries
type RetryOperation[T any] func() (T, bool, error)

// RetryConfig configures retry behavior
type RetryConfig struct {
	OnRetry       func() error
	OnRetryFailed func() error
	Description   string
	MaxRetries    int
	RetryDelay    time.Duration
}

// WithRetry executes an operation with retry logic
// This consolidates the common retry pattern used across transports
func WithRetry[T any](config RetryConfig, operation RetryOperation[T]) (T, error) {
	var zero T

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		result, shouldRetry, err := operation()
		if err != nil {
			return zero, err
		}

		if !shouldRetry {
			return result, nil
		}

		// If we should retry but we're at max attempts, break
		if attempt >= config.MaxRetries {
			break
		}

		if err := executeRetryCallback(config); err != nil {
			return zero, err
		}

		if config.RetryDelay > 0 {
			time.Sleep(config.RetryDelay)
		}
	}

	return handleRetriesExhausted[T](config)
}

// executeRetryCallback executes the retry callback if provided
func executeRetryCallback(config RetryConfig) error {
	if config.OnRetry != nil {
		return config.OnRetry()
	}
	return nil
}

// handleRetriesExhausted handles the case when all retries are exhausted
func handleRetriesExhausted[T any](config RetryConfig) (T, error) {
	var zero T

	if config.OnRetryFailed != nil {
		if failErr := config.OnRetryFailed(); failErr != nil {
			return zero, failErr
		}
	}

	// Return a default "retries exhausted" error - caller should provide more specific error
	return zero, pn532.NewTransportError("retry", "unknown", pn532.ErrCommunicationFailed, pn532.ErrorTypeTransient)
}

// TimeoutRetry executes an operation with timeout-based retry logic
// Common pattern for polling operations (like waiting for device ready)
func TimeoutRetry[T any](timeout time.Duration, operation RetryOperation[T]) (T, error) {
	var zero T
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		result, shouldRetry, err := operation()
		if err != nil {
			return zero, err
		}

		if !shouldRetry {
			return result, nil
		}

		// Small delay before next attempt
		time.Sleep(time.Millisecond)
	}

	return zero, pn532.NewTimeoutError("timeoutRetry", "unknown")
}
