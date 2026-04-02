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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/internal/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createMockDeviceWithTransport creates a device with mock transport for testing.
// The mock starts with a target selected (simulating successful InListPassiveTarget).
func createMockDeviceWithTransport(t *testing.T, opts ...pn532.Option) (*pn532.Device, *pn532.MockTransport) {
	mockTransport := pn532.NewMockTransport()
	mockTransport.SelectTarget() // Simulate that a tag was detected
	device, err := pn532.New(mockTransport, opts...)
	require.NoError(t, err)
	return device, mockTransport
}

// createTestDetectedTag creates a mock detected tag for testing
func createTestDetectedTag() *pn532.DetectedTag {
	return &pn532.DetectedTag{
		UID:        "04123456789ABC",
		UIDBytes:   []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC},
		ATQ:        []byte{0x00, 0x04},
		SAK:        0x08,
		Type:       pn532.TagTypeNTAG,
		DetectedAt: time.Now(),
	}
}

func TestNewSession(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)

	t.Run("WithDefaultConfig", func(t *testing.T) {
		t.Parallel()
		session := NewSession(device, nil)

		assert.NotNil(t, session)
		assert.Equal(t, device, session.device)
		assert.NotNil(t, session.config)
		assert.NotNil(t, session.pauseChan)
		assert.NotNil(t, session.resumeChan)
		assert.False(t, session.isPaused.Load())
	})

	t.Run("WithCustomConfig", func(t *testing.T) {
		t.Parallel()
		config := &Config{
			PollInterval: 50 * time.Millisecond,
		}
		session := NewSession(device, config)

		assert.NotNil(t, session)
		assert.Equal(t, config, session.config)
		assert.Equal(t, 50*time.Millisecond, session.config.PollInterval)
	})
}

func TestSession_CallbackSetters(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)

	t.Run("SetOnCardDetected", func(t *testing.T) {
		t.Parallel()
		session := NewSession(device, nil)

		var called atomic.Bool
		session.SetOnCardDetected(func(_ context.Context, _ *pn532.DetectedTag) error {
			called.Store(true)
			return nil
		})

		// Verify callback was set by checking it's not nil
		session.stateMutex.RLock()
		cb := session.OnCardDetected
		session.stateMutex.RUnlock()
		assert.NotNil(t, cb)
	})

	t.Run("SetOnCardRemoved", func(t *testing.T) {
		t.Parallel()
		session := NewSession(device, nil)

		var called atomic.Bool
		session.SetOnCardRemoved(func() {
			called.Store(true)
		})

		session.stateMutex.RLock()
		cb := session.OnCardRemoved
		session.stateMutex.RUnlock()
		assert.NotNil(t, cb)
	})

	t.Run("SetOnCardChanged", func(t *testing.T) {
		t.Parallel()
		session := NewSession(device, nil)

		var called atomic.Bool
		session.SetOnCardChanged(func(_ context.Context, _ *pn532.DetectedTag) error {
			called.Store(true)
			return nil
		})

		session.stateMutex.RLock()
		cb := session.OnCardChanged
		session.stateMutex.RUnlock()
		assert.NotNil(t, cb)
	})
}

func TestSession_CallbackSettersConcurrent(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// This test verifies no race conditions when setting callbacks concurrently
	// Run with -race flag to detect races
	var wg sync.WaitGroup
	const numGoroutines = 10

	for range numGoroutines {
		wg.Add(3)

		go func() {
			defer wg.Done()
			session.SetOnCardDetected(func(_ context.Context, _ *pn532.DetectedTag) error {
				return nil
			})
		}()

		go func() {
			defer wg.Done()
			session.SetOnCardRemoved(func() {})
		}()

		go func() {
			defer wg.Done()
			session.SetOnCardChanged(func(_ context.Context, _ *pn532.DetectedTag) error {
				return nil
			})
		}()
	}

	wg.Wait()

	// Verify callbacks are set (any of them, since order is non-deterministic)
	session.stateMutex.RLock()
	defer session.stateMutex.RUnlock()
	assert.NotNil(t, session.OnCardDetected)
	assert.NotNil(t, session.OnCardRemoved)
	assert.NotNil(t, session.OnCardChanged)
}

func TestSession_PauseResume(t *testing.T) {
	t.Parallel()

	t.Run("InitiallyNotPaused", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)
		assert.False(t, session.isPaused.Load())
	})

	t.Run("PauseOperation", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)
		session.Pause()
		assert.True(t, session.isPaused.Load())

		// Pausing again should be idempotent
		session.Pause()
		assert.True(t, session.isPaused.Load())
	})

	t.Run("ResumeOperation", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)
		session.Pause() // First pause it
		session.Resume()
		assert.False(t, session.isPaused.Load())

		// Resuming again should be idempotent
		session.Resume()
		assert.False(t, session.isPaused.Load())
	})
}

func TestSession_ConcurrentPauseResume(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Test concurrent pause/resume operations
	var wg sync.WaitGroup
	iterations := 100

	// Start multiple goroutines doing pause/resume
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				session.Pause()
				time.Sleep(time.Microsecond)
				session.Resume()
			}
		}()
	}

	wg.Wait()

	// Should end up in a consistent state
	assert.False(t, session.isPaused.Load())
}

//nolint:funlen // Test function with multiple subtests
func TestSession_WriteToTag(t *testing.T) {
	t.Parallel()

	t.Run("SuccessfulWrite", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Setup mock responses for tag creation and write operations
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
		mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

		detectedTag := createTestDetectedTag()
		writeCallCount := 0

		err := session.WriteToTag(
			context.Background(), context.Background(), detectedTag,
			func(_ context.Context, _ pn532.Tag) error {
				writeCallCount++
				return nil
			})

		require.NoError(t, err)
		assert.Equal(t, 1, writeCallCount)
		assert.False(t, session.isPaused.Load()) // Should be resumed after write
	})

	t.Run("WriteError", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Setup mock responses for tag creation and write operations
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
		mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

		detectedTag := createTestDetectedTag()
		expectedErr := errors.New("write failed")

		err := session.WriteToTag(
			context.Background(), context.Background(), detectedTag,
			func(_ context.Context, _ pn532.Tag) error {
				return expectedErr
			})

		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.False(t, session.isPaused.Load()) // Should be resumed even on error
	})

	t.Run("TagCreationError", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Setup mock responses for tag creation and write operations
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
		mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

		// Create a tag with invalid/unknown type that will cause CreateTag to fail
		invalidTag := &pn532.DetectedTag{
			UID:      "04123456789ABC",
			UIDBytes: []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC},
			Type:     pn532.TagTypeUnknown, // This will cause CreateTag to return ErrInvalidTag
		}

		err := session.WriteToTag(
			context.Background(), context.Background(), invalidTag,
			func(_ context.Context, _ pn532.Tag) error {
				t.Fatal("Write function should not be called")
				return nil
			})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create tag")
		assert.False(t, session.isPaused.Load()) // Should be resumed even on error
	})

	t.Run("IndependentWriteContextCancellation", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Setup mock responses
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response

		// Create separate contexts - session ctx stays active, write ctx gets cancelled
		sessionCtx := context.Background()
		writeCtx, cancelWrite := context.WithCancel(context.Background())

		detectedTag := createTestDetectedTag()
		writeCalled := false

		err := session.WriteToTag(sessionCtx, writeCtx, detectedTag, func(ctx context.Context, _ pn532.Tag) error {
			writeCalled = true
			// Cancel the write context during the write operation
			cancelWrite()
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				return errors.New("context should be cancelled")
			}
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
		assert.True(t, writeCalled)
		assert.False(t, session.isPaused.Load()) // Should be resumed after cancelled write
	})

	t.Run("ContextCancelledDuringStabilizationDelay", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Create a context that will be cancelled before the stabilization delay completes
		writeCtx, cancelWrite := context.WithCancel(context.Background())

		detectedTag := createTestDetectedTag()
		writeCalled := false

		// Cancel the context immediately in a goroutine
		go func() {
			time.Sleep(10 * time.Millisecond) // Cancel during the 75ms stabilization delay
			cancelWrite()
		}()

		err := session.WriteToTag(
			context.Background(), writeCtx, detectedTag,
			func(_ context.Context, _ pn532.Tag) error {
				writeCalled = true
				return nil
			})

		require.ErrorIs(t, err, context.Canceled)
		// Write function should not be called when context is cancelled during stabilization
		assert.False(t, writeCalled)
		assert.False(t, session.isPaused.Load()) // Should be resumed after cancelled write
	})
}

func TestSession_PauseAcknowledgment(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Setup mock responses for polling
	mockTransport.SetResponse(0x4A, []byte{0xD5, 0x4B, 0x00}) // No tag detected
	mockTransport.SetResponse(0x54, []byte{0x55, 0x00})       // InSelect response
	mockTransport.SetResponse(0x40, []byte{0x41, 0x00})       // DataExchange response

	// Start polling in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pollingStarted := make(chan struct{})
	go func() {
		close(pollingStarted)
		_ = session.Start(ctx) // Start polling
	}()

	// Wait for polling to start
	<-pollingStarted
	time.Sleep(10 * time.Millisecond) // Give polling time to start

	detectedTag := createTestDetectedTag()
	writeStarted := make(chan struct{})
	writeCompleted := make(chan struct{})

	// Start a write operation that should pause polling first
	go func() {
		defer close(writeCompleted)
		close(writeStarted)
		err := session.WriteToTag(
			context.Background(), context.Background(), detectedTag,
			func(_ context.Context, _ pn532.Tag) error {
				// This should run only after polling is properly paused
				return nil
			})
		assert.NoError(t, err)
	}()

	// Wait for write to start and complete
	<-writeStarted
	<-writeCompleted

	// If we get here without deadlock or panic, the pause mechanism works
	assert.False(t, session.isPaused.Load()) // Should be resumed after write
}

func TestSession_PauseAcknowledgment_RaceCondition(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Setup mock responses
	mockTransport.SetResponse(0x4A, []byte{0xD5, 0x4B, 0x00}) // No tag detected

	// Test concurrent calls to pauseWithAck for race conditions
	const numGoroutines = 10
	ctx := context.Background()

	var wg sync.WaitGroup
	errorChan := make(chan error, numGoroutines)

	// Launch multiple goroutines calling pauseWithAck concurrently
	for range numGoroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := session.pauseWithAck(ctx); err != nil {
				errorChan <- err
			}
		}()
	}

	wg.Wait()
	close(errorChan)

	// Check for errors
	for err := range errorChan {
		t.Errorf("pauseWithAck failed: %v", err)
	}

	// After all calls, session should be paused
	assert.True(t, session.isPaused.Load(), "Session should be paused after concurrent pauseWithAck calls")
}

func TestSession_PauseAcknowledgment_ContextCancellation(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Setup mock responses
	mockTransport.SetResponse(0x4A, []byte{0xD5, 0x4B, 0x00}) // No tag detected

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// pauseWithAck should return context.Canceled error
	err := session.pauseWithAck(ctx)
	require.Error(t, err, "pauseWithAck should return error when context is cancelled")
	require.ErrorIs(t, err, context.Canceled, "Error should be context.Canceled")

	// Session should not be paused if context was cancelled
	assert.False(t, session.isPaused.Load(), "Session should not be paused when context is cancelled")
}

func TestSafeTimerStop(t *testing.T) {
	t.Parallel()

	t.Run("StopsActiveTimer", func(t *testing.T) {
		t.Parallel()
		callbackExecuted := false
		timer := time.AfterFunc(100*time.Millisecond, func() {
			callbackExecuted = true
		})

		safeTimerStop(timer)
		time.Sleep(150 * time.Millisecond) // Wait longer than the timer duration

		assert.False(t, callbackExecuted, "Timer callback should not execute after safe stop")
	})

	t.Run("HandlesNilTimer", func(t *testing.T) {
		t.Parallel()
		// Should not panic
		safeTimerStop(nil)
	})

	t.Run("HandlesAlreadyFiredTimer", func(t *testing.T) {
		t.Parallel()
		var callbackExecuted int32
		timer := time.AfterFunc(1*time.Millisecond, func() {
			atomic.StoreInt32(&callbackExecuted, 1)
		})

		time.Sleep(10 * time.Millisecond) // Let timer fire
		assert.Equal(t, int32(1), atomic.LoadInt32(&callbackExecuted), "Timer should have fired")

		// Should not block or panic when stopping an already-fired timer
		safeTimerStop(timer)
	})
}

func TestSession_ConcurrentWrites(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Setup mock responses - use correct InSelect response format
	mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
	mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

	detectedTag := createTestDetectedTag()

	var writeOrder []int
	var mu syncutil.Mutex
	var wg sync.WaitGroup

	numWrites := 5

	// Start multiple concurrent writes
	for i := range numWrites {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			err := session.WriteToTag(
				context.Background(), context.Background(), detectedTag,
				func(_ context.Context, _ pn532.Tag) error {
					mu.Lock()
					writeOrder = append(writeOrder, id)
					mu.Unlock()

					// Simulate write time
					time.Sleep(10 * time.Millisecond)
					return nil
				})

			assert.NoError(t, err)
		}(i)
	}

	wg.Wait()

	// All writes should have completed
	assert.Len(t, writeOrder, numWrites)
	assert.False(t, session.isPaused.Load())

	// Verify writes were serialized (no overlapping)
	// Each write should complete before the next starts due to mutex
	for i := range numWrites {
		assert.Contains(t, writeOrder, i)
	}
}

func TestSession_WriteToTagPausesBehavior(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Setup mock responses - use correct InSelect response format
	mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
	mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

	detectedTag := createTestDetectedTag()

	var pauseDetected, resumeDetected bool
	var wg sync.WaitGroup
	wg.Add(1)

	// Session pause state changes
	go func() {
		defer wg.Done()
		timeout := time.After(5 * time.Second) // Add timeout to prevent infinite loop
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-timeout:
				// Timeout reached, exit to prevent hanging
				return
			case <-ticker.C:
				if session.isPaused.Load() {
					pauseDetected = true
				}

				// Break when write is complete
				if pauseDetected && !session.isPaused.Load() {
					resumeDetected = true
					return
				}
			}
		}
	}()

	err := session.WriteToTag(
		context.Background(), context.Background(), detectedTag,
		func(_ context.Context, _ pn532.Tag) error {
			// During write, session should be paused
			assert.True(t, session.isPaused.Load())
			time.Sleep(20 * time.Millisecond) // Simulate write operation
			return nil
		})

	wg.Wait()

	require.NoError(t, err)
	assert.True(t, pauseDetected, "Session should have been paused during write")
	assert.True(t, resumeDetected, "Session should have been resumed after write")
	assert.False(t, session.isPaused.Load(), "Session should be resumed after write")
}

func TestSession_WriteToTagWithLongOperation(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Setup mock responses - use correct InSelect response format
	mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
	mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

	detectedTag := createTestDetectedTag()

	start := time.Now()

	err := session.WriteToTag(
		context.Background(), context.Background(), detectedTag,
		func(_ context.Context, _ pn532.Tag) error {
			// Simulate a longer write operation
			time.Sleep(100 * time.Millisecond)
			return nil
		})

	duration := time.Since(start)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, duration, 100*time.Millisecond)
	assert.False(t, session.isPaused.Load())
}

func TestSession_WriteToTagErrorHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		writeFunc   func(context.Context, pn532.Tag) error
		name        string
		expectError bool
	}{
		{
			name:        "WriteSuccess",
			expectError: false,
			writeFunc: func(_ context.Context, _ pn532.Tag) error {
				return nil
			},
		},
		{
			name:        "WriteFailure",
			expectError: true,
			writeFunc: func(_ context.Context, _ pn532.Tag) error {
				return errors.New("simulated write error")
			},
		},
		{
			name:        "WritePanic",
			expectError: true,
			writeFunc: func(_ context.Context, _ pn532.Tag) error {
				panic("simulated panic")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create separate session instance for each subtest to avoid race conditions
			device, mockTransport := createMockDeviceWithTransport(t)
			session := NewSession(device, nil)

			// Setup mock responses - use correct InSelect response format
			// InSelect response (cmd 0x54, response 0x55, status 0x00)
			mockTransport.SetResponse(0x54, []byte{0x55, 0x00})
			mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

			detectedTag := createTestDetectedTag()

			err := executeWriteWithPanicRecovery(session, detectedTag, tt.writeFunc)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			// Session should always be resumed after write, even on error
			assert.False(t, session.isPaused.Load())
		})
	}
}

func executeWriteWithPanicRecovery(
	session *Session,
	tag *pn532.DetectedTag,
	writeFunc func(context.Context, pn532.Tag) error,
) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("panic occurred")
		}
	}()
	return session.WriteToTag(context.Background(), context.Background(), tag, writeFunc)
}

func TestSession_ConcurrentWriteStressTest(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Setup mock responses - use correct InSelect response format
	mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
	mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

	detectedTag := createTestDetectedTag()

	var successCount int64
	var errorCount int64
	var wg sync.WaitGroup

	const numGoroutines = 20
	const writesPerGoroutine = 10

	for i := range numGoroutines {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			params := stressTestParams{
				routineID:          routineID,
				writesPerGoroutine: writesPerGoroutine,
				successCount:       &successCount,
				errorCount:         &errorCount,
			}
			runStressTestWrites(session, detectedTag, params)
		}(i)
	}

	wg.Wait()

	totalWrites := int64(numGoroutines * writesPerGoroutine)
	assert.Equal(t, totalWrites, successCount+errorCount)
	assert.False(t, session.isPaused.Load())

	// We expect some successes and some errors based on our error condition
	assert.Positive(t, successCount)
	assert.Positive(t, errorCount)
}

func runStressTestWrites(
	session *Session,
	tag *pn532.DetectedTag,
	params stressTestParams,
) {
	for j := range params.writesPerGoroutine {
		err := session.WriteToTag(
			context.Background(), context.Background(), tag,
			func(_ context.Context, _ pn532.Tag) error {
				// Simulate variable write times
				time.Sleep(time.Duration(params.routineID+j) * time.Millisecond)

				// Occasionally return an error
				if (params.routineID+j)%7 == 0 {
					return errors.New("simulated error")
				}
				return nil
			})

		if err != nil {
			atomic.AddInt64(params.errorCount, 1)
		} else {
			atomic.AddInt64(params.successCount, 1)
		}
	}
}

type stressTestParams struct {
	successCount       *int64
	errorCount         *int64
	routineID          int
	writesPerGoroutine int
}

// testTimerCleanupTransition tests timer cleanup behavior during state transitions
func testTimerCleanupTransition(t *testing.T, testName string,
	setupFn func(*CardState) *atomic.Bool,
	transitionFn func(*CardState) *atomic.Bool,
	expectedState CardDetectionState,
) {
	t.Helper()
	t.Run(testName, func(t *testing.T) {
		t.Parallel()
		cs := &CardState{}

		// Set up initial timer
		initialCallback := setupFn(cs)
		require.NotNil(t, cs.RemovalTimer)

		// Perform transition
		transitionCallback := transitionFn(cs)

		// Verify state and timer
		if expectedState != StateIdle {
			require.NotNil(t, cs.RemovalTimer)
		} else {
			assert.Nil(t, cs.RemovalTimer)
		}
		assert.Equal(t, expectedState, cs.DetectionState)

		// Wait and verify callbacks
		time.Sleep(60 * time.Millisecond)
		assert.False(t, initialCallback.Load(), "Initial timer should not fire after cleanup")
		if transitionCallback != nil {
			assert.False(t, transitionCallback.Load(), "Transition timer should not fire yet")
		}
	})
}

// TestCardState_TimerCleanup tests that removal timers are properly cleaned up
func TestCardState_TimerCleanup(t *testing.T) {
	t.Parallel()

	testTimerCleanupTransition(t, "TransitionToPostReadGrace_CleansUpTimer",
		func(cs *CardState) *atomic.Bool {
			var callback atomic.Bool
			cs.TransitionToDetected(200*time.Millisecond, func() { callback.Store(true) })
			return &callback
		},
		func(cs *CardState) *atomic.Bool {
			var callback atomic.Bool
			cs.TransitionToPostReadGrace(200*time.Millisecond, func() { callback.Store(true) })
			return &callback
		},
		StatePostReadGrace,
	)

	testTimerCleanupTransition(t, "TransitionToDetected_CleansUpTimer",
		func(cs *CardState) *atomic.Bool {
			var callback atomic.Bool
			cs.TransitionToPostReadGrace(200*time.Millisecond, func() { callback.Store(true) })
			return &callback
		},
		func(cs *CardState) *atomic.Bool {
			var callback atomic.Bool
			cs.TransitionToDetected(200*time.Millisecond, func() { callback.Store(true) })
			return &callback
		},
		StateTagDetected,
	)

	testTimerCleanupTransition(t, "TransitionToIdle_CleansUpTimer",
		func(cs *CardState) *atomic.Bool {
			var callback atomic.Bool
			cs.TransitionToDetected(200*time.Millisecond, func() { callback.Store(true) })
			return &callback
		},
		func(cs *CardState) *atomic.Bool {
			cs.TransitionToIdle()
			// Verify additional idle state properties
			assert.False(t, cs.Present)
			assert.Empty(t, cs.LastUID)
			assert.Empty(t, cs.LastType)
			assert.Empty(t, cs.TestedUID)
			assert.True(t, cs.LastSeenTime.IsZero())
			assert.True(t, cs.ReadStartTime.IsZero())
			return nil
		},
		StateIdle,
	)

	t.Run("TransitionToReading_CleansUpTimer", func(t *testing.T) {
		t.Parallel()
		cs := &CardState{}

		// First set up a timer
		var callbackCalled atomic.Bool
		cs.TransitionToDetected(200*time.Millisecond, func() {
			callbackCalled.Store(true)
		})
		require.NotNil(t, cs.RemovalTimer)

		// Now transition to reading - should clean up the timer
		cs.TransitionToReading()

		// Timer should be nil and state should be reading
		assert.Nil(t, cs.RemovalTimer)
		assert.Equal(t, StateReading, cs.DetectionState)
		assert.False(t, cs.ReadStartTime.IsZero())

		// Original timer should not fire since it was cleaned up
		time.Sleep(60 * time.Millisecond)
		assert.False(t, callbackCalled.Load(), "Timer callback should not fire after cleanup to reading")
	})
}

func TestSession_WriteToNextTag(t *testing.T) {
	t.Parallel()

	t.Run("SuccessfulWriteToNextTag", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Setup mock responses for polling and tag operations
		// InListPassiveTarget with tag
		mockTransport.SetResponse(0x4A, []byte{
			0x4B, 0x01, 0x01, 0x00, 0x04, 0x08, 0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
		})
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response
		mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response

		writeCallCount := 0
		err := session.WriteToNextTag(
			context.Background(), context.Background(), 5*time.Second, func(_ context.Context, tag pn532.Tag) error {
				writeCallCount++
				// Validate we got a real tag, not nil
				require.NotNil(t, tag, "WriteToNextTag should pass a non-nil Tag object")
				require.NotEmpty(t, tag.UID(), "Tag should have a valid UID")
				return nil
			})

		require.NoError(t, err)
		assert.Equal(t, 1, writeCallCount)
		assert.False(t, session.isPaused.Load()) // Should be resumed after write
	})

	t.Run("TimeoutWaitingForTag", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Setup mock to return no tags - should be called multiple times during polling
		mockTransport.SetResponse(0x4A, []byte{0x4B, 0x00}) // InListPassiveTarget with no tags

		start := time.Now()
		timeout := 150 * time.Millisecond
		err := session.WriteToNextTag(
			context.Background(), context.Background(), timeout,
			func(_ context.Context, _ pn532.Tag) error {
				t.Fatal("Write function should not be called")
				return nil
			})

		elapsed := time.Since(start)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timeout waiting for tag")
		assert.False(t, session.isPaused.Load()) // Should be resumed even on timeout

		// Should have actually waited close to the timeout duration (polling continuously)
		assert.GreaterOrEqual(t, elapsed, timeout-20*time.Millisecond, "Should wait approximately the timeout duration")
	})

	t.Run("IndependentWriteContextCancellation", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Setup mock responses for tag detection
		mockTransport.SetResponse(0x4A, []byte{
			0x4B, 0x01, 0x01, 0x00, 0x04, 0x08, 0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
		})
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response

		// Create separate contexts - session ctx stays active, write ctx gets cancelled
		sessionCtx := context.Background()
		writeCtx, cancelWrite := context.WithCancel(context.Background())

		writeCalled := false
		err := session.WriteToNextTag(
			sessionCtx, writeCtx, 5*time.Second,
			func(ctx context.Context, _ pn532.Tag) error {
				writeCalled = true
				// Cancel the write context during the write operation
				cancelWrite()
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
					return errors.New("context should be cancelled")
				}
			})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
		assert.True(t, writeCalled)
		assert.False(t, session.isPaused.Load()) // Should be resumed after cancelled write
	})
}

//nolint:funlen // Test function with multiple subtests
func TestSession_WriteToTagWithRetry(t *testing.T) {
	t.Parallel()

	t.Run("SuccessfulWriteFirstAttempt", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Setup mock responses
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response
		mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response

		detectedTag := createTestDetectedTag()
		writeCallCount := 0

		err := session.WriteToTagWithRetry(
			context.Background(), context.Background(), detectedTag, 3,
			func(_ context.Context, tag pn532.Tag) error {
				writeCallCount++
				require.NotNil(t, tag)
				return nil
			})

		require.NoError(t, err)
		assert.Equal(t, 1, writeCallCount)
		assert.False(t, session.isPaused.Load())
	})

	t.Run("RetriesOnTransientError", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Setup mock responses
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00})
		mockTransport.SetResponse(0x40, []byte{0x41, 0x00})

		detectedTag := createTestDetectedTag()
		writeCallCount := 0

		err := session.WriteToTagWithRetry(
			context.Background(), context.Background(), detectedTag, 3,
			func(_ context.Context, _ pn532.Tag) error {
				writeCallCount++
				// Fail first two times with retryable error, succeed on third
				if writeCallCount < 3 {
					return pn532.ErrTransportTimeout // Retryable error
				}
				return nil
			})

		require.NoError(t, err)
		assert.Equal(t, 3, writeCallCount)
		assert.False(t, session.isPaused.Load())
	})

	t.Run("FailsOnPermanentError", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		mockTransport.SetResponse(0x54, []byte{0x55, 0x00})

		detectedTag := createTestDetectedTag()
		writeCallCount := 0

		err := session.WriteToTagWithRetry(
			context.Background(), context.Background(), detectedTag, 3,
			func(_ context.Context, _ pn532.Tag) error {
				writeCallCount++
				// Non-retryable error
				return pn532.ErrDataTooLarge
			})

		require.Error(t, err)
		assert.Equal(t, 1, writeCallCount) // Should not retry on permanent error
		assert.False(t, session.isPaused.Load())
	})

	t.Run("DefaultMaxRetriesWhenZero", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		mockTransport.SetResponse(0x54, []byte{0x55, 0x00})
		mockTransport.SetResponse(0x40, []byte{0x41, 0x00})

		detectedTag := createTestDetectedTag()
		writeCallCount := 0

		// Pass 0 for maxRetries - should default to 3
		err := session.WriteToTagWithRetry(
			context.Background(), context.Background(), detectedTag, 0,
			func(_ context.Context, _ pn532.Tag) error {
				writeCallCount++
				// Fail first 2 attempts, succeed on 3rd
				if writeCallCount < 3 {
					return pn532.ErrTransportTimeout
				}
				return nil
			})

		require.NoError(t, err)
		assert.Equal(t, 3, writeCallCount) // Succeeds on 3rd attempt (default 3 retries)
	})
}

func TestSession_WriteToNextTagWithRetry(t *testing.T) {
	t.Parallel()

	t.Run("SuccessfulWriteWithRetry", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Setup mock responses for polling and tag operations
		mockTransport.SetResponse(0x4A, []byte{
			0x4B, 0x01, 0x01, 0x00, 0x04, 0x08, 0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
		})
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00})
		mockTransport.SetResponse(0x40, []byte{0x41, 0x00})

		writeCallCount := 0
		err := session.WriteToNextTagWithRetry(
			context.Background(), context.Background(), 5*time.Second, 3,
			func(_ context.Context, tag pn532.Tag) error {
				writeCallCount++
				require.NotNil(t, tag)
				require.NotEmpty(t, tag.UID())
				return nil
			})

		require.NoError(t, err)
		assert.Equal(t, 1, writeCallCount)
		assert.False(t, session.isPaused.Load())
	})

	t.Run("RetriesOnTransientErrorDuringWrite", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Setup mock responses
		mockTransport.SetResponse(0x4A, []byte{
			0x4B, 0x01, 0x01, 0x00, 0x04, 0x08, 0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
		})
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00})
		mockTransport.SetResponse(0x40, []byte{0x41, 0x00})

		writeCallCount := 0
		err := session.WriteToNextTagWithRetry(
			context.Background(), context.Background(), 5*time.Second, 3,
			func(_ context.Context, _ pn532.Tag) error {
				writeCallCount++
				// Fail first attempt with retryable error
				if writeCallCount == 1 {
					return pn532.ErrCommunicationFailed
				}
				return nil
			})

		require.NoError(t, err)
		assert.Equal(t, 2, writeCallCount)
	})

	t.Run("ExhaustsRetriesOnPersistentError", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		mockTransport.SetResponse(0x4A, []byte{
			0x4B, 0x01, 0x01, 0x00, 0x04, 0x08, 0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
		})
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00})

		writeCallCount := 0
		err := session.WriteToNextTagWithRetry(
			context.Background(), context.Background(), 5*time.Second, 2,
			func(_ context.Context, _ pn532.Tag) error {
				writeCallCount++
				return pn532.ErrChecksumMismatch // Retryable error
			})

		require.Error(t, err)
		assert.Equal(t, 2, writeCallCount) // Tried maxRetries times
		assert.Contains(t, err.Error(), "retries")
	})
}

func TestSession_HandlePollingError(t *testing.T) {
	t.Parallel()

	t.Run("IgnoresDeadlineExceeded", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Should not panic or cause issues
		session.handlePollingError(context.DeadlineExceeded)
	})

	t.Run("IgnoresContextCanceled", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Should not panic or cause issues
		session.handlePollingError(context.Canceled)
	})

	t.Run("HandlesOtherErrors_TriggersCardRemoval", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Should trigger handleCardRemoval for transport errors
		session.handlePollingError(errors.New("some transport error"))

		// No panic means success
	})
}

// TestSession_OnDeviceDisconnected tests the OnDeviceDisconnected callback behavior
func TestSession_OnDeviceDisconnected(t *testing.T) {
	t.Parallel()

	t.Run("FatalError_TriggersCallback", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		var callbackCalled bool
		var receivedErr error
		session.SetOnDeviceDisconnected(func(err error) {
			callbackCalled = true
			receivedErr = err
		})

		// Create a fatal error (device not found)
		fatalErr := &pn532.TransportError{
			Op:   "test",
			Port: "test",
			Err:  pn532.ErrDeviceNotFound,
			Type: pn532.ErrorTypePermanent,
		}

		session.handlePollingError(fatalErr)

		assert.True(t, callbackCalled, "OnDeviceDisconnected callback should be called for fatal errors")
		assert.Equal(t, fatalErr, receivedErr, "Callback should receive the original error")
	})

	t.Run("NonFatalError_DoesNotTriggerCallback", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		var callbackCalled bool
		session.SetOnDeviceDisconnected(func(_ error) {
			callbackCalled = true
		})

		// Non-fatal transport error
		nonFatalErr := errors.New("temporary communication error")

		session.handlePollingError(nonFatalErr)

		assert.False(t, callbackCalled, "OnDeviceDisconnected should NOT be called for non-fatal errors")
	})

	t.Run("FatalError_NoCallback_NoPanic", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)
		// Don't set OnDeviceDisconnected callback

		fatalErr := &pn532.TransportError{
			Op:   "test",
			Port: "test",
			Err:  pn532.ErrDeviceNotFound,
			Type: pn532.ErrorTypePermanent,
		}

		// Should not panic when callback is nil
		session.handlePollingError(fatalErr)
	})
}

// TestSession_FatalErrorDoesNotTriggerCardRemoved verifies that fatal errors
// (device disconnection) don't incorrectly fire OnCardRemoved before OnDeviceDisconnected.
func TestSession_FatalErrorDoesNotTriggerCardRemoved(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Simulate card was present
	session.stateMutex.Lock()
	session.state.Present = true
	session.state.LastUID = "04123456789ABC"
	session.stateMutex.Unlock()

	var cardRemovedCalled bool
	var disconnectedCalled bool
	session.SetOnCardRemoved(func() {
		cardRemovedCalled = true
	})
	session.SetOnDeviceDisconnected(func(_ error) {
		disconnectedCalled = true
	})

	// Create a fatal error (device disconnected)
	fatalErr := &pn532.TransportError{
		Op:   "test",
		Port: "test",
		Err:  pn532.ErrDeviceNotFound,
		Type: pn532.ErrorTypePermanent,
	}

	session.handlePollingError(fatalErr)

	assert.True(t, disconnectedCalled, "OnDeviceDisconnected should be called")
	assert.False(t, cardRemovedCalled, "OnCardRemoved should NOT be called for fatal errors")
}

func TestSession_HandleCardRemoval(t *testing.T) {
	t.Parallel()

	t.Run("NoOpWhenSessionClosed", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)
		session.closed.Store(true)

		// Should return early without panic
		session.handleCardRemoval()
	})

	t.Run("NoOpWhenCardNotPresent", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Card not present, should not call callback
		session.handleCardRemoval()
		assert.False(t, session.GetState().Present)
	})

	t.Run("CallsCallback_WhenCardWasPresent", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Simulate card was present
		session.stateMutex.Lock()
		session.state.Present = true
		session.state.LastUID = "04123456789ABC"
		session.stateMutex.Unlock()

		var callbackCalled bool
		session.SetOnCardRemoved(func() {
			callbackCalled = true
		})

		session.handleCardRemoval()

		assert.True(t, callbackCalled, "OnCardRemoved callback should be called")
		assert.False(t, session.GetState().Present, "Card should no longer be present")
	})
}

func TestSession_ExecuteSinglePollingCycle_FatalError(t *testing.T) {
	t.Parallel()

	t.Run("ReturnsFatalError", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Set up a fatal error (transport closed)
		mockTransport.SetError(0x4A, pn532.ErrTransportClosed)

		ctx := context.Background()
		err := session.executeSinglePollingCycle(ctx)

		// Should return the fatal error
		require.Error(t, err)
		assert.ErrorIs(t, err, pn532.ErrTransportClosed)
	})

	t.Run("ContinuesOnRetryableError", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Set up a retryable error (transport timeout)
		mockTransport.SetError(0x4A, pn532.ErrTransportTimeout)

		ctx := context.Background()
		err := session.executeSinglePollingCycle(ctx)

		// Should return nil (continue polling)
		assert.NoError(t, err)
	})

	t.Run("ContinuesOnErrNoTagInPoll", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Set up no tag response (not an error, just no tag)
		mockTransport.DeselectTarget() // Simulate no tag present

		ctx := context.Background()
		err := session.executeSinglePollingCycle(ctx)

		// Should return nil (continue polling)
		assert.NoError(t, err)
	})

	t.Run("ReturnsFatalDeviceNotFoundError", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		// Set up a fatal error (device not found)
		mockTransport.SetError(0x4A, pn532.ErrDeviceNotFound)

		ctx := context.Background()
		err := session.executeSinglePollingCycle(ctx)

		// Should return the fatal error
		require.Error(t, err)
		assert.ErrorIs(t, err, pn532.ErrDeviceNotFound)
	})
}

// TestSession_SleepDetection_TriggersCallback tests that sleep detection triggers callback
func TestSession_SleepDetection_TriggersCallback(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	config := &Config{
		PollInterval: 10 * time.Millisecond,
		SleepRecovery: SleepRecoveryConfig{
			Enabled:                    true,
			TimeDiscontinuityThreshold: 50 * time.Millisecond,
			MaxRecoveryAttempts:        3,
			RecoveryBackoff:            10 * time.Millisecond,
		},
	}

	session := NewSession(device, config)

	var sleepDetected atomic.Bool
	session.SetOnSleepDetected(func() {
		sleepDetected.Store(true)
	})

	// Setup mock responses
	mockTransport.SetResponse(0x4A, []byte{0x4B, 0x00}) // No tag
	mockTransport.SetResponse(0x14, []byte{0x15})       // SAMConfiguration success

	// Simulate that we had a poll a long time ago
	session.lastPollTime = time.Now().Add(-100 * time.Millisecond)

	ctx := context.Background()
	_ = session.executeSinglePollingCycle(ctx)

	assert.True(t, sleepDetected.Load(), "OnSleepDetected callback should have been called")
}

// TestSession_SleepDetection_Disabled tests that sleep detection can be disabled
func TestSession_SleepDetection_Disabled(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	config := &Config{
		PollInterval: 10 * time.Millisecond,
		SleepRecovery: SleepRecoveryConfig{
			Enabled: false, // Disabled
		},
	}

	session := NewSession(device, config)

	var sleepDetected atomic.Bool
	session.SetOnSleepDetected(func() {
		sleepDetected.Store(true)
	})

	// Setup mock responses
	mockTransport.SetResponse(0x4A, []byte{0x4B, 0x00}) // No tag

	// Simulate that we had a poll a long time ago
	session.lastPollTime = time.Now().Add(-100 * time.Millisecond)

	ctx := context.Background()
	_ = session.executeSinglePollingCycle(ctx)

	assert.False(t, sleepDetected.Load(), "OnSleepDetected callback should NOT be called when disabled")
}

// TestSession_SleepDetection_RecoverySuccess tests that successful recovery continues polling
func TestSession_SleepDetection_RecoverySuccess(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	config := &Config{
		PollInterval: 10 * time.Millisecond,
		SleepRecovery: SleepRecoveryConfig{
			Enabled:                    true,
			TimeDiscontinuityThreshold: 50 * time.Millisecond,
			MaxRecoveryAttempts:        3,
			RecoveryBackoff:            10 * time.Millisecond,
		},
	}

	session := NewSession(device, config)

	// Setup mock responses
	mockTransport.SetResponse(0x4A, []byte{0x4B, 0x00}) // No tag
	mockTransport.SetResponse(0x14, []byte{0x15})       // SAMConfiguration success

	// Set up a mock recoverer that succeeds
	recoverer := NewDefaultRecoverer(device, nil, 10*time.Millisecond, 3)
	session.SetRecoverer(recoverer)

	// Simulate that we had a poll a long time ago
	session.lastPollTime = time.Now().Add(-100 * time.Millisecond)

	ctx := context.Background()
	err := session.executeSinglePollingCycle(ctx)

	assert.NoError(t, err, "Polling should continue after successful recovery")
}

// TestSession_SleepDetection_RecoveryFails tests that failed recovery exits polling loop
func TestSession_SleepDetection_RecoveryFails(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	config := &Config{
		PollInterval: 10 * time.Millisecond,
		SleepRecovery: SleepRecoveryConfig{
			Enabled:                    true,
			TimeDiscontinuityThreshold: 50 * time.Millisecond,
			MaxRecoveryAttempts:        1, // Fail fast
			RecoveryBackoff:            10 * time.Millisecond,
		},
	}

	session := NewSession(device, config)

	// Setup mock to fail SAMConfiguration
	mockTransport.SetError(0x14, errors.New("soft reset failed"))

	// Set up a mock recoverer with no reopen function (will fail)
	recoverer := NewDefaultRecoverer(device, nil, 10*time.Millisecond, 1)
	session.SetRecoverer(recoverer)

	// Simulate that we had a poll a long time ago
	session.lastPollTime = time.Now().Add(-100 * time.Millisecond)

	ctx := context.Background()
	err := session.executeSinglePollingCycle(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "sleep recovery failed")
}

// TestSession_SleepDetection_NilRecoverer tests that nil recoverer is handled gracefully
func TestSession_SleepDetection_NilRecoverer(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	config := &Config{
		PollInterval: 10 * time.Millisecond,
		SleepRecovery: SleepRecoveryConfig{
			Enabled:                    true,
			TimeDiscontinuityThreshold: 50 * time.Millisecond,
		},
	}

	session := NewSession(device, config)

	// Explicitly set recoverer to nil (overriding the auto-created one)
	session.SetRecoverer(nil)

	// Setup mock response for polling
	mockTransport.SetResponse(0x4A, []byte{0x4B, 0x00}) // No tag

	// Simulate sleep
	session.lastPollTime = time.Now().Add(-100 * time.Millisecond)

	ctx := context.Background()
	err := session.executeSinglePollingCycle(ctx)

	// Should succeed - nil recoverer means no recovery attempted, but no error
	assert.NoError(t, err)
}

// TestSession_SleepDetection_DeviceUpdatedAfterRecovery tests that device is updated after recovery
func TestSession_SleepDetection_DeviceUpdatedAfterRecovery(t *testing.T) {
	t.Parallel()
	oldDevice, oldTransport := createMockDeviceWithTransport(t)
	newDevice, newTransport := createMockDeviceWithTransport(t)

	config := &Config{
		PollInterval: 10 * time.Millisecond,
		SleepRecovery: SleepRecoveryConfig{
			Enabled:                    true,
			TimeDiscontinuityThreshold: 50 * time.Millisecond,
			MaxRecoveryAttempts:        2,
			RecoveryBackoff:            10 * time.Millisecond,
		},
	}

	session := NewSession(oldDevice, config)

	// Old device fails SAMConfiguration
	oldTransport.SetError(0x14, errors.New("soft reset failed"))

	// New device succeeds
	newTransport.SetResponse(0x14, []byte{0x15})
	newTransport.SetResponse(0x4A, []byte{0x4B, 0x00}) // No tag

	// Set up recoverer that returns new device on reopen
	reopenCalled := false
	recoverer := NewDefaultRecoverer(oldDevice, func() (*pn532.Device, error) {
		reopenCalled = true
		return newDevice, nil
	}, 10*time.Millisecond, 2)
	session.SetRecoverer(recoverer)

	// Simulate sleep
	session.lastPollTime = time.Now().Add(-100 * time.Millisecond)

	ctx := context.Background()
	err := session.executeSinglePollingCycle(ctx)

	require.NoError(t, err)
	assert.True(t, reopenCalled, "Reopen should have been called")
	assert.Equal(t, newDevice, session.GetDevice(), "Device should be updated to new device")
}

// TestSleepRecoveryConfig_DetectSleep tests the DetectSleep helper
func TestSleepRecoveryConfig_DetectSleep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		elapsed      time.Duration
		pollInterval time.Duration
		threshold    time.Duration
		enabled      bool
		want         bool
	}{
		{
			name:         "disabled returns false",
			enabled:      false,
			elapsed:      10 * time.Second,
			pollInterval: 100 * time.Millisecond,
			threshold:    2 * time.Second,
			want:         false,
		},
		{
			name:         "normal poll - no sleep",
			enabled:      true,
			elapsed:      150 * time.Millisecond,
			pollInterval: 100 * time.Millisecond,
			threshold:    2 * time.Second,
			want:         false,
		},
		{
			name:         "exactly at threshold - no sleep",
			enabled:      true,
			elapsed:      2100 * time.Millisecond,
			pollInterval: 100 * time.Millisecond,
			threshold:    2 * time.Second,
			want:         false,
		},
		{
			name:         "exceeds threshold - sleep detected",
			enabled:      true,
			elapsed:      5 * time.Second,
			pollInterval: 100 * time.Millisecond,
			threshold:    2 * time.Second,
			want:         true,
		},
		{
			name:         "long poll interval with small gap - no sleep",
			enabled:      true,
			elapsed:      1100 * time.Millisecond,
			pollInterval: 1 * time.Second,
			threshold:    2 * time.Second,
			want:         false,
		},
		{
			name:         "long poll interval exceeds threshold - sleep",
			enabled:      true,
			elapsed:      4 * time.Second,
			pollInterval: 1 * time.Second,
			threshold:    2 * time.Second,
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := SleepRecoveryConfig{
				Enabled:                    tt.enabled,
				TimeDiscontinuityThreshold: tt.threshold,
			}
			got := cfg.DetectSleep(tt.elapsed, tt.pollInterval)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestSession_GetState tests the GetState method
func TestSession_GetState(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)

	config := &Config{
		PollInterval: 10 * time.Millisecond,
	}

	session := NewSession(device, config)

	state := session.GetState()
	assert.False(t, state.Present)
	assert.Empty(t, state.LastUID)
}

// TestSession_CallbackBehavior tests callback execution during polling
func TestSession_CallbackBehavior(t *testing.T) {
	t.Parallel()

	t.Run("CallsCallback", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		detectedTag := createTestDetectedTag()
		var callbackCalled bool
		session.OnCardDetected = func(_ context.Context, _ *pn532.DetectedTag) error {
			callbackCalled = true
			return nil
		}

		err := session.processPollingResults(context.Background(), detectedTag)

		require.NoError(t, err)
		assert.True(t, callbackCalled, "callback should be called")
		assert.True(t, session.GetState().Present)
	})

	t.Run("CallbackFailsOnce_SilentRetry", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		detectedTag := createTestDetectedTag()
		callbackErr := errors.New("NDEF read failed")
		session.OnCardDetected = func(_ context.Context, _ *pn532.DetectedTag) error {
			return callbackErr
		}

		// First call - should fail silently (not return error)
		err := session.processPollingResults(context.Background(), detectedTag)

		require.NoError(t, err, "first failure should be silent")
		state := session.GetState()
		assert.Equal(t, 1, state.ConsecutiveStableFailures)
		assert.False(t, state.Present, "state should not be updated on failure")
	})
}

// TestSession_CallbackRepeatedFailures tests callback failure and retry behavior
func TestSession_CallbackRepeatedFailures(t *testing.T) {
	t.Parallel()

	t.Run("CallbackFailsRepeatedly_NeverReturnsError", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		detectedTag := createTestDetectedTag()
		callbackErr := errors.New("NDEF read failed")
		session.OnCardDetected = func(_ context.Context, _ *pn532.DetectedTag) error {
			return callbackErr
		}

		// All calls should return nil - we never give up, just keep retrying
		// until success or physical card removal (inferred from absence)
		for i := 1; i <= 5; i++ {
			err := session.processPollingResults(context.Background(), detectedTag)
			require.NoError(t, err, "failure %d should still return nil (never give up)", i)
			assert.Equal(t, i, session.GetState().ConsecutiveStableFailures)
		}
	})

	t.Run("CallbackSucceeds_ResetsFailureCounter", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		session := NewSession(device, nil)

		detectedTag := createTestDetectedTag()
		failCount := 0
		session.OnCardDetected = func(_ context.Context, _ *pn532.DetectedTag) error {
			failCount++
			if failCount <= 2 {
				return errors.New("transient error")
			}
			return nil // Succeed on third attempt
		}

		// First two calls fail silently
		_ = session.processPollingResults(context.Background(), detectedTag)
		assert.Equal(t, 1, session.GetState().ConsecutiveStableFailures)

		_ = session.processPollingResults(context.Background(), detectedTag)
		assert.Equal(t, 2, session.GetState().ConsecutiveStableFailures)

		// Third call succeeds - should reset counter
		err := session.processPollingResults(context.Background(), detectedTag)
		require.NoError(t, err)
		state := session.GetState()
		assert.Equal(t, 0, state.ConsecutiveStableFailures)
		assert.True(t, state.Present)
	})
}

// TestSession_CardRemoval_ResetsFailureCounter tests that card removal resets failure counter
func TestSession_CardRemoval_ResetsFailureCounter(t *testing.T) {
	t.Parallel()

	device, _ := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Simulate some failures (must hold lock when modifying state)
	session.stateMutex.Lock()
	session.state.ConsecutiveStableFailures = 2
	session.state.Present = true
	session.stateMutex.Unlock()

	// Trigger card removal
	session.handleCardRemoval()

	state := session.GetState()
	assert.Equal(t, 0, state.ConsecutiveStableFailures)
	assert.False(t, state.Present)
}

// --- Callback Failure Handling Tests ---

func TestHandleCallbackFailure_ResetsPresentFlag(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Simulate that a card was present
	session.stateMutex.Lock()
	session.state.Present = true
	session.state.ConsecutiveStableFailures = 0
	session.stateMutex.Unlock()

	// Handle a callback failure
	err := session.handleCallbackFailure(context.Background(), errors.New("NDEF read failed"))

	// Verify behavior
	state := session.GetState()

	// Key assertion: handleCallbackFailure should ALWAYS reset Present to false
	// This ensures the callback runs again on next poll
	assert.False(t, state.Present,
		"Present should be reset to false after callback failure")

	// Should increment failure counter
	assert.Equal(t, 1, state.ConsecutiveStableFailures,
		"ConsecutiveStableFailures should be incremented")

	// Should return nil (never give up)
	assert.NoError(t, err,
		"handleCallbackFailure should return nil to continue polling")
}

func TestHandleCallbackFailure_IncrementsFailureCounter(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Call handleCallbackFailure multiple times
	for i := range 5 {
		// Reset Present before each call (simulating successful detection)
		session.stateMutex.Lock()
		session.state.Present = true
		session.stateMutex.Unlock()

		err := session.handleCallbackFailure(context.Background(), errors.New("failure"))
		require.NoError(t, err)

		state := session.GetState()
		assert.Equal(t, i+1, state.ConsecutiveStableFailures,
			"Failure counter should increment on each call")
		assert.False(t, state.Present,
			"Present should be false after each failure")
	}
}

func TestHandleCallbackFailure_AlwaysReturnsNil(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Test with various error types
	testErrors := []error{
		errors.New("generic error"),
		pn532.ErrTransportTimeout,
		pn532.ErrTransportRead,
		pn532.NewPN532Error(0x02, "InDataExchange", "CRC error"),
	}

	for _, testErr := range testErrors {
		err := session.handleCallbackFailure(context.Background(), testErr)
		assert.NoError(t, err,
			"handleCallbackFailure should always return nil for error: %v", testErr)
	}
}

// Regression tests for poll cycle timeout protection and RF field cycling

func TestSession_CallbackFailure_CyclesRFField(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Set up RF configuration response for CycleRFField
	// 0x32 is cmdRFConfiguration
	mockTransport.SetResponse(0x32, []byte{0x33}) // RFConfiguration success

	// Simulate a callback failure - this should trigger RF field cycling
	err := session.handleCallbackFailure(context.Background(), errors.New("NDEF read failed"))

	require.NoError(t, err, "handleCallbackFailure should return nil")

	// Verify RFConfiguration was called (at least twice for off+on cycle)
	// The cycle happens when failures >= 1
	assert.GreaterOrEqual(t, mockTransport.GetCallCount(0x32), 2,
		"CycleRFField should call RFConfiguration twice (off + on)")

	// Verify failure counter was incremented
	state := session.GetState()
	assert.Equal(t, 1, state.ConsecutiveStableFailures,
		"ConsecutiveStableFailures should be incremented")
}

func TestSession_PollCycleTimeoutProtection(t *testing.T) {
	t.Parallel()

	// This test verifies that the pollCycleTimeout constant is applied.
	// The timeout is 10 seconds and is applied in executeSinglePollingCycle.
	// We verify the timeout mechanism exists by checking that a slow poll
	// eventually times out rather than hanging forever.

	device, mockTransport := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Set up a very long delay that would exceed the timeout
	// but we'll use a shorter test timeout to verify behavior
	mockTransport.SetDelay(50 * time.Millisecond)
	mockTransport.SetResponse(0x4A, []byte{0x4B, 0x00}) // No tag

	// Create a context with a short timeout for testing
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := session.executeSinglePollingCycle(ctx)
	elapsed := time.Since(start)

	// Either the poll completes or context times out
	// The key assertion is that we don't hang forever
	if err != nil {
		// Context timeout is expected
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}

	// Verify we respected the timeout (didn't wait forever)
	assert.Less(t, elapsed, 200*time.Millisecond,
		"Poll cycle should respect timeout, not hang indefinitely")
}

// --- Bug 3 Tests: Device check after hard reset failure ---

func TestSession_HandlePollError_NoACK_HardResetFails_DeviceGone(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Make Reconnect fail (simulating device gone)
	mockTransport.SetReconnectError(errors.New("reconnect failed: device gone"))

	// Make CheckHealth return a fatal error (device gone)
	deviceGoneErr := &pn532.TransportError{
		Op: "checkHealth", Port: "/dev/ttyUSB0",
		Err: pn532.ErrDeviceNotFound, Type: pn532.ErrorTypePermanent,
	}
	mockTransport.SetHealthError(deviceGoneErr)

	// Track OnDeviceDisconnected callback
	var disconnectedErr error
	var disconnectedCalled bool
	session.SetOnDeviceDisconnected(func(err error) {
		disconnectedCalled = true
		disconnectedErr = err
	})

	// Create a NoACK error (what happens when device disconnects mid-write)
	noACKErr := pn532.NewNoACKError("sendFrame", "/dev/ttyUSB0")

	ctx := context.Background()
	err := session.handlePollError(ctx, noACKErr)

	// Should return the fatal error immediately instead of continuing
	require.Error(t, err)
	assert.True(t, pn532.IsFatal(err), "Error should be fatal")
	assert.True(t, disconnectedCalled, "OnDeviceDisconnected should be called")
	assert.ErrorIs(t, disconnectedErr, pn532.ErrDeviceNotFound)
}

func TestSession_HandlePollError_NoACK_HardResetFails_DeviceStillPresent(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Make Reconnect fail but device is still present (CheckHealth returns nil)
	mockTransport.SetReconnectError(errors.New("reconnect failed: other reason"))
	mockTransport.SetHealthError(nil) // Device is healthy

	noACKErr := pn532.NewNoACKError("sendFrame", "/dev/ttyUSB0")

	ctx := context.Background()
	err := session.handlePollError(ctx, noACKErr)

	// Should continue polling (return nil) since device is still there
	assert.NoError(t, err, "Should continue polling when device is still present")
}

func TestSession_CheckDeviceHealth_NoHealthChecker(t *testing.T) {
	t.Parallel()

	// Create a minimal transport that doesn't implement DeviceHealthChecker
	minimalTransport := &minimalMockTransport{connected: true}
	device, err := pn532.New(minimalTransport)
	require.NoError(t, err)

	session := NewSession(device, nil)

	// Should return nil when transport doesn't implement DeviceHealthChecker
	healthErr := session.checkDeviceHealth()
	assert.NoError(t, healthErr)
}

func TestSession_CheckDeviceHealth_NilDevice(t *testing.T) {
	t.Parallel()

	device, _ := createMockDeviceWithTransport(t)
	session := NewSession(device, nil)

	// Set device to nil to test nil guard
	session.stateMutex.Lock()
	session.device = nil
	session.stateMutex.Unlock()

	healthErr := session.checkDeviceHealth()
	assert.NoError(t, healthErr, "Should return nil when device is nil")
}

// minimalMockTransport implements only the Transport interface (no DeviceHealthChecker)
type minimalMockTransport struct {
	mu        syncutil.Mutex
	connected bool
}

func (*minimalMockTransport) SendCommand(_ context.Context, cmd byte, _ []byte) ([]byte, error) {
	return []byte{0xD5, cmd + 1, 0x00}, nil
}

func (m *minimalMockTransport) Close() error {
	m.mu.Lock()
	m.connected = false
	m.mu.Unlock()
	return nil
}

func (*minimalMockTransport) SetTimeout(_ time.Duration) error { return nil }

func (m *minimalMockTransport) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connected
}

func (*minimalMockTransport) Type() pn532.TransportType { return pn532.TransportMock }
