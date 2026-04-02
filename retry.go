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

package pn532

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
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

	for attempt := range config.MaxAttempts {
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
			delay := calculateJitteredSleep(backoff, config.Jitter)
			if err := sleep(ctx, delay, lastErr); err != nil {
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

func sleep(ctx context.Context, d time.Duration, lastErr error) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return lastErr
	case <-timer.C:
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
