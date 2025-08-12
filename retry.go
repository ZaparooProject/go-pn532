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

package pn532

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// RetryConfig configures retry behavior
type RetryConfig struct {
	// MaxAttempts is the maximum number of attempts (0 = no retry)
	MaxAttempts int
	// InitialBackoff is the initial backoff duration
	InitialBackoff time.Duration
	// MaxBackoff is the maximum backoff duration
	MaxBackoff time.Duration
	// BackoffMultiplier is the factor by which the backoff increases
	BackoffMultiplier float64
	// Jitter adds randomness to backoff to avoid thundering herd
	Jitter float64
	// RetryTimeout is the overall timeout for all retry attempts
	RetryTimeout time.Duration
}

// DefaultRetryConfig returns a default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:       3,
		InitialBackoff:    10 * time.Millisecond,
		MaxBackoff:        1 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            0.1,
		RetryTimeout:      5 * time.Second,
	}
}

// RetryableFunc is a function that can be retried
type RetryableFunc func() error

// RetryWithConfig executes a function with retry logic
func RetryWithConfig(ctx context.Context, config *RetryConfig, retryFunc RetryableFunc) error {
	if config == nil {
		config = DefaultRetryConfig()
	}

	if config.MaxAttempts <= 0 {
		return retryFunc()
	}

	retryCtx, cancel := setupRetryContext(ctx, config)
	defer cancel()
	return executeWithRetry(retryCtx, config, retryFunc)
}

func setupRetryContext(ctx context.Context, config *RetryConfig) (context.Context, context.CancelFunc) {
	if config.RetryTimeout > 0 {
		return context.WithTimeout(ctx, config.RetryTimeout)
	}
	return ctx, func() {}
}

func executeWithRetry(ctx context.Context, config *RetryConfig, retryFunc RetryableFunc) error {
	var lastErr error
	backoff := config.InitialBackoff

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		if err := checkContextCancellation(ctx, lastErr); err != nil {
			return err
		}

		err := retryFunc()
		if err == nil {
			return nil
		}
		if !IsRetryable(err) {
			return err
		}
		lastErr = err

		if attempt < config.MaxAttempts-1 {
			sleep := calculateJitteredSleep(backoff, config.Jitter)
			if err := sleepWithContext(ctx, sleep, lastErr); err != nil {
				return err
			}
			backoff = calculateNextBackoff(backoff, config)
		}
	}

	return lastErr
}

func checkContextCancellation(ctx context.Context, lastErr error) error {
	select {
	case <-ctx.Done():
		if lastErr != nil {
			return lastErr
		}
		return fmt.Errorf("retry context cancelled: %w", ctx.Err())
	default:
		return nil
	}
}

func sleepWithContext(ctx context.Context, sleep time.Duration, lastErr error) error {
	select {
	case <-ctx.Done():
		return lastErr
	case <-time.After(sleep):
		return nil
	}
}

func calculateNextBackoff(backoff time.Duration, config *RetryConfig) time.Duration {
	newBackoff := time.Duration(float64(backoff) * config.BackoffMultiplier)
	if newBackoff > config.MaxBackoff {
		return config.MaxBackoff
	}
	return newBackoff
}

// Retry executes a function with default retry configuration
func Retry(ctx context.Context, fn RetryableFunc) error {
	return RetryWithConfig(ctx, DefaultRetryConfig(), fn)
}

// ExponentialBackoff calculates exponential backoff duration
func ExponentialBackoff(
	attempt int, initial time.Duration, maxDuration time.Duration, multiplier float64,
) time.Duration {
	if attempt <= 0 {
		return initial
	}

	backoff := float64(initial) * math.Pow(multiplier, float64(attempt-1))
	if backoff > float64(maxDuration) {
		return maxDuration
	}

	return time.Duration(backoff)
}

// calculateJitteredSleep calculates sleep duration with jitter
func calculateJitteredSleep(baseSleep time.Duration, jitterFactor float64) time.Duration {
	sleep := baseSleep
	if jitterFactor > 0 {
		// Use crypto/rand for secure random jitter
		var randBytes [8]byte
		if _, err := rand.Read(randBytes[:]); err == nil {
			// Convert to float64 in range [0, 1)
			randUint := binary.LittleEndian.Uint64(randBytes[:])
			randFloat := float64(randUint) / float64(1<<64)
			jitter := float64(sleep) * jitterFactor
			sleep += time.Duration(randFloat * jitter)
		}
	}
	return sleep
}
