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
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteNDEFWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	t.Parallel()

	callCount := 0
	writeFunc := func(_ context.Context) error {
		callCount++
		return nil
	}

	err := WriteNDEFWithRetry(context.Background(), writeFunc, 3, "TEST")

	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "should only call writeFunc once on success")
}

func TestWriteNDEFWithRetry_SuccessAfterRetries(t *testing.T) {
	t.Parallel()

	callCount := 0
	writeFunc := func(_ context.Context) error {
		callCount++
		if callCount < 3 {
			// Return a retryable error for the first 2 attempts
			return ErrTransportTimeout
		}
		return nil
	}

	err := WriteNDEFWithRetry(context.Background(), writeFunc, 3, "TEST")

	require.NoError(t, err)
	assert.Equal(t, 3, callCount, "should call writeFunc 3 times")
}

func TestWriteNDEFWithRetry_NonRetryableErrorAbortsImmediately(t *testing.T) {
	t.Parallel()

	callCount := 0
	nonRetryableErr := errors.New("permanent error")
	writeFunc := func(_ context.Context) error {
		callCount++
		return nonRetryableErr
	}

	err := WriteNDEFWithRetry(context.Background(), writeFunc, 3, "TEST")

	require.Error(t, err)
	assert.Equal(t, nonRetryableErr, err, "should return the non-retryable error directly")
	assert.Equal(t, 1, callCount, "should only call writeFunc once for non-retryable error")
}

func TestWriteNDEFWithRetry_ContextCancelledBeforeAttempt(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	callCount := 0
	writeFunc := func(_ context.Context) error {
		callCount++
		return nil
	}

	err := WriteNDEFWithRetry(ctx, writeFunc, 3, "TEST")

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, callCount, "should not call writeFunc when context is already cancelled")
}

func TestWriteNDEFWithRetry_ContextCancelledDuringBackoff(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	var callCount atomic.Int32
	writeFunc := func(_ context.Context) error {
		count := callCount.Add(1)
		if count == 1 {
			// Cancel after first failure to interrupt backoff
			go func() {
				time.Sleep(10 * time.Millisecond)
				cancel()
			}()
			return ErrTransportTimeout // Retryable error
		}
		return nil
	}

	start := time.Now()
	err := WriteNDEFWithRetry(ctx, writeFunc, 3, "TEST")
	elapsed := time.Since(start)

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(1), callCount.Load(), "should only call writeFunc once before cancellation")
	// Should return quickly after cancellation, not wait for full backoff (100ms)
	assert.Less(t, elapsed, 80*time.Millisecond, "should cancel during backoff, not wait full delay")
}

func TestWriteNDEFWithRetry_MaxRetriesExhausted(t *testing.T) {
	t.Parallel()

	callCount := 0
	writeFunc := func(_ context.Context) error {
		callCount++
		return ErrTransportTimeout // Always return retryable error
	}

	err := WriteNDEFWithRetry(context.Background(), writeFunc, 3, "TEST")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write TEST NDEF data after 3 retries")
	require.ErrorIs(t, err, ErrTransportTimeout)
	assert.Equal(t, 3, callCount, "should call writeFunc maxRetries times")
}

func TestWriteNDEFWithRetry_DefaultMaxRetries(t *testing.T) {
	t.Parallel()

	callCount := 0
	writeFunc := func(_ context.Context) error {
		callCount++
		return ErrTransportTimeout // Always return retryable error
	}

	err := WriteNDEFWithRetry(context.Background(), writeFunc, 0, "TEST")

	require.Error(t, err)
	assert.Equal(t, 3, callCount, "should default to 3 retries when maxRetries is 0")
}

func TestWriteNDEFWithRetry_ExponentialBackoff(t *testing.T) {
	t.Parallel()

	var timestamps []time.Time
	writeFunc := func(_ context.Context) error {
		timestamps = append(timestamps, time.Now())
		return ErrTransportTimeout // Always return retryable error
	}

	_ = WriteNDEFWithRetry(context.Background(), writeFunc, 3, "TEST")

	require.Len(t, timestamps, 3, "should have 3 attempts")

	// Just verify delays exist and are increasing (exponential backoff)
	// Don't check exact timings - too flaky on CI
	if len(timestamps) >= 3 {
		delay1 := timestamps[1].Sub(timestamps[0])
		delay2 := timestamps[2].Sub(timestamps[1])
		assert.Greater(t, delay1, time.Duration(0), "should have delay between attempts")
		assert.GreaterOrEqual(t, delay2, delay1, "delays should increase (exponential backoff)")
	}
}

func TestWriteNDEFWithRetry_MixedRetryableAndNonRetryable(t *testing.T) {
	t.Parallel()

	callCount := 0
	nonRetryableErr := errors.New("permanent failure")
	writeFunc := func(_ context.Context) error {
		callCount++
		if callCount == 1 {
			return ErrTransportTimeout // First: retryable
		}
		return nonRetryableErr // Second: non-retryable
	}

	err := WriteNDEFWithRetry(context.Background(), writeFunc, 3, "TEST")

	require.Error(t, err)
	assert.Equal(t, nonRetryableErr, err, "should return non-retryable error immediately")
	assert.Equal(t, 2, callCount, "should stop after non-retryable error")
}

func TestWriteNDEFWithRetry_VariousRetryableErrors(t *testing.T) {
	t.Parallel()

	retryableErrors := []error{
		ErrTransportTimeout,
		ErrFrameCorrupted,
		NewTransportError("write", "/dev/test", ErrTransportTimeout, ErrorTypeTransient),
	}

	for _, testErr := range retryableErrors {
		t.Run(testErr.Error(), func(t *testing.T) {
			t.Parallel()

			callCount := 0
			writeFunc := func(_ context.Context) error {
				callCount++
				if callCount < 2 {
					return testErr
				}
				return nil
			}

			err := WriteNDEFWithRetry(context.Background(), writeFunc, 3, "TEST")

			require.NoError(t, err, "error %v should be retryable", testErr)
			assert.Equal(t, 2, callCount, "should retry on error %v", testErr)
		})
	}
}

func TestReadNDEFWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	t.Parallel()

	callCount := 0
	expectedMsg := &NDEFMessage{
		Records: []NDEFRecord{{Type: NDEFTypeText, Text: "test"}},
	}
	readFunc := func() (*NDEFMessage, error) {
		callCount++
		return expectedMsg, nil
	}
	isRetryable := func(_ error) bool { return true }

	msg, err := readNDEFWithRetry(readFunc, isRetryable, "TEST")

	require.NoError(t, err)
	assert.Equal(t, expectedMsg, msg)
	assert.Equal(t, 1, callCount)
}

func TestReadNDEFWithRetry_EmptyDataIsValidSuccess(t *testing.T) {
	t.Parallel()

	callCount := 0
	emptyMsg := &NDEFMessage{}
	readFunc := func() (*NDEFMessage, error) {
		callCount++
		return emptyMsg, nil // Empty data is valid (e.g., tag with [03 00 FE] TLV)
	}
	isRetryable := func(_ error) bool { return true }

	msg, err := readNDEFWithRetry(readFunc, isRetryable, "TEST")

	require.NoError(t, err, "empty NDEF should be valid, not an error")
	assert.Equal(t, emptyMsg, msg)
	assert.Equal(t, 1, callCount, "should succeed on first attempt, no retries needed")
}

func TestReadNDEFWithRetry_NonRetryableError(t *testing.T) {
	t.Parallel()

	callCount := 0
	testErr := errors.New("permanent error")
	readFunc := func() (*NDEFMessage, error) {
		callCount++
		return nil, testErr
	}
	isRetryable := func(_ error) bool { return false }

	msg, err := readNDEFWithRetry(readFunc, isRetryable, "TEST")

	require.Error(t, err)
	assert.Equal(t, testErr, err)
	assert.Nil(t, msg)
	assert.Equal(t, 1, callCount, "should not retry non-retryable errors")
}
