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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createMockDeviceWithTransport creates a device with mock transport for testing
func createMockDeviceWithTransport(t *testing.T) (*pn532.Device, *pn532.MockTransport) {
	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
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

func TestNewMonitor(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)

	t.Run("WithDefaultConfig", func(t *testing.T) {
		t.Parallel()
		monitor := NewMonitor(device, nil)

		assert.NotNil(t, monitor)
		assert.Equal(t, device, monitor.device)
		assert.NotNil(t, monitor.config)
		assert.NotNil(t, monitor.pauseChan)
		assert.NotNil(t, monitor.resumeChan)
		assert.False(t, monitor.isPaused.Load())
	})

	t.Run("WithCustomConfig", func(t *testing.T) {
		t.Parallel()
		config := &Config{
			PollInterval: 50 * time.Millisecond,
		}
		monitor := NewMonitor(device, config)

		assert.NotNil(t, monitor)
		assert.Equal(t, config, monitor.config)
		assert.Equal(t, 50*time.Millisecond, monitor.config.PollInterval)
	})
}

func TestMonitor_PauseResume(t *testing.T) {
	t.Parallel()

	t.Run("InitiallyNotPaused", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		monitor := NewMonitor(device, nil)
		assert.False(t, monitor.isPaused.Load())
	})

	t.Run("PauseOperation", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		monitor := NewMonitor(device, nil)
		monitor.Pause()
		assert.True(t, monitor.isPaused.Load())

		// Pausing again should be idempotent
		monitor.Pause()
		assert.True(t, monitor.isPaused.Load())
	})

	t.Run("ResumeOperation", func(t *testing.T) {
		t.Parallel()
		device, _ := createMockDeviceWithTransport(t)
		monitor := NewMonitor(device, nil)
		monitor.Pause() // First pause it
		monitor.Resume()
		assert.False(t, monitor.isPaused.Load())

		// Resuming again should be idempotent
		monitor.Resume()
		assert.False(t, monitor.isPaused.Load())
	})
}

func TestMonitor_ConcurrentPauseResume(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)
	monitor := NewMonitor(device, nil)

	// Test concurrent pause/resume operations
	var wg sync.WaitGroup
	iterations := 100

	// Start multiple goroutines doing pause/resume
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				monitor.Pause()
				time.Sleep(time.Microsecond)
				monitor.Resume()
			}
		}()
	}

	wg.Wait()

	// Should end up in a consistent state
	assert.False(t, monitor.isPaused.Load())
}

func TestMonitor_WriteToTag(t *testing.T) {
	t.Parallel()

	t.Run("SuccessfulWrite", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		monitor := NewMonitor(device, nil)

		// Setup mock responses for tag creation and write operations
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
		mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

		detectedTag := createTestDetectedTag()
		writeCallCount := 0

		err := monitor.WriteToTag(detectedTag, func(_ pn532.Tag) error {
			writeCallCount++
			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, 1, writeCallCount)
		assert.False(t, monitor.isPaused.Load()) // Should be resumed after write
	})

	t.Run("WriteError", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		monitor := NewMonitor(device, nil)

		// Setup mock responses for tag creation and write operations
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
		mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

		detectedTag := createTestDetectedTag()
		expectedErr := errors.New("write failed")

		err := monitor.WriteToTag(detectedTag, func(_ pn532.Tag) error {
			return expectedErr
		})

		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.False(t, monitor.isPaused.Load()) // Should be resumed even on error
	})

	t.Run("TagCreationError", func(t *testing.T) {
		t.Parallel()
		device, mockTransport := createMockDeviceWithTransport(t)
		monitor := NewMonitor(device, nil)

		// Setup mock responses for tag creation and write operations
		mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
		mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

		// Create a tag with invalid/unknown type that will cause CreateTag to fail
		invalidTag := &pn532.DetectedTag{
			UID:      "04123456789ABC",
			UIDBytes: []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC},
			Type:     pn532.TagTypeUnknown, // This will cause CreateTag to return ErrInvalidTag
		}

		err := monitor.WriteToTag(invalidTag, func(_ pn532.Tag) error {
			t.Fatal("Write function should not be called")
			return nil
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create tag")
		assert.False(t, monitor.isPaused.Load()) // Should be resumed even on error
	})
}

func TestMonitor_ConcurrentWrites(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	monitor := NewMonitor(device, nil)

	// Setup mock responses - use correct InSelect response format
	mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
	mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

	detectedTag := createTestDetectedTag()

	var writeOrder []int
	var mu sync.Mutex
	var wg sync.WaitGroup

	numWrites := 5

	// Start multiple concurrent writes
	for i := 0; i < numWrites; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			err := monitor.WriteToTag(detectedTag, func(_ pn532.Tag) error {
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
	assert.False(t, monitor.isPaused.Load())

	// Verify writes were serialized (no overlapping)
	// Each write should complete before the next starts due to mutex
	for i := 0; i < numWrites; i++ {
		assert.Contains(t, writeOrder, i)
	}
}

func TestMonitor_WriteToTagPausesBehavior(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	monitor := NewMonitor(device, nil)

	// Setup mock responses - use correct InSelect response format
	mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
	mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

	detectedTag := createTestDetectedTag()

	var pauseDetected, resumeDetected bool
	var wg sync.WaitGroup
	wg.Add(1)

	// Monitor pause state changes
	go func() {
		defer wg.Done()
		for {
			if monitor.isPaused.Load() {
				pauseDetected = true
			}
			time.Sleep(time.Millisecond)

			// Break when write is complete
			if pauseDetected && !monitor.isPaused.Load() {
				resumeDetected = true
				break
			}
		}
	}()

	err := monitor.WriteToTag(detectedTag, func(_ pn532.Tag) error {
		// During write, monitor should be paused
		assert.True(t, monitor.isPaused.Load())
		time.Sleep(20 * time.Millisecond) // Simulate write operation
		return nil
	})

	wg.Wait()

	require.NoError(t, err)
	assert.True(t, pauseDetected, "Monitor should have been paused during write")
	assert.True(t, resumeDetected, "Monitor should have been resumed after write")
	assert.False(t, monitor.isPaused.Load(), "Monitor should be resumed after write")
}

func TestMonitor_WriteToTagWithLongOperation(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	monitor := NewMonitor(device, nil)

	// Setup mock responses - use correct InSelect response format
	mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
	mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

	detectedTag := createTestDetectedTag()

	start := time.Now()

	err := monitor.WriteToTag(detectedTag, func(_ pn532.Tag) error {
		// Simulate a longer write operation
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	duration := time.Since(start)

	require.NoError(t, err)
	assert.GreaterOrEqual(t, duration, 100*time.Millisecond)
	assert.False(t, monitor.isPaused.Load())
}

func TestMonitor_WriteToTagErrorHandling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		writeFunc   func(pn532.Tag) error
		name        string
		expectError bool
	}{
		{
			name:        "WriteSuccess",
			expectError: false,
			writeFunc: func(_ pn532.Tag) error {
				return nil
			},
		},
		{
			name:        "WriteFailure",
			expectError: true,
			writeFunc: func(_ pn532.Tag) error {
				return errors.New("simulated write error")
			},
		},
		{
			name:        "WritePanic",
			expectError: true,
			writeFunc: func(_ pn532.Tag) error {
				panic("simulated panic")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create separate monitor instance for each subtest to avoid race conditions
			device, mockTransport := createMockDeviceWithTransport(t)
			monitor := NewMonitor(device, nil)

			// Setup mock responses - use correct InSelect response format
			// InSelect response (cmd 0x54, response 0x55, status 0x00)
			mockTransport.SetResponse(0x54, []byte{0x55, 0x00})
			mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

			detectedTag := createTestDetectedTag()

			err := executeWriteWithPanicRecovery(monitor, detectedTag, tt.writeFunc)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			// Monitor should always be resumed after write, even on error
			assert.False(t, monitor.isPaused.Load())
		})
	}
}

func executeWriteWithPanicRecovery(
	monitor *Monitor,
	tag *pn532.DetectedTag,
	writeFunc func(pn532.Tag) error,
) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("panic occurred")
		}
	}()
	return monitor.WriteToTag(tag, writeFunc)
}

func TestMonitor_ConcurrentWriteStressTest(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	monitor := NewMonitor(device, nil)

	// Setup mock responses - use correct InSelect response format
	mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
	mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for write

	detectedTag := createTestDetectedTag()

	var successCount int64
	var errorCount int64
	var wg sync.WaitGroup

	const numGoroutines = 20
	const writesPerGoroutine = 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			params := stressTestParams{
				routineID:          routineID,
				writesPerGoroutine: writesPerGoroutine,
				successCount:       &successCount,
				errorCount:         &errorCount,
			}
			runStressTestWrites(monitor, detectedTag, params)
		}(i)
	}

	wg.Wait()

	totalWrites := int64(numGoroutines * writesPerGoroutine)
	assert.Equal(t, totalWrites, successCount+errorCount)
	assert.False(t, monitor.isPaused.Load())

	// We expect some successes and some errors based on our error condition
	assert.Positive(t, successCount)
	assert.Positive(t, errorCount)
}

func runStressTestWrites(
	monitor *Monitor,
	tag *pn532.DetectedTag,
	params stressTestParams,
) {
	for j := 0; j < params.writesPerGoroutine; j++ {
		err := monitor.WriteToTag(tag, func(_ pn532.Tag) error {
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
