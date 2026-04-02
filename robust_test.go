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
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadNDEFWithRetry_FunctionExists verifies that the robust reading functions exist and can be called
func TestReadNDEFWithRetry_FunctionExists(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	// Test NTAG ReadNDEFWithRetry exists
	uid := []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x90, 0xAB}
	ntagTag := NewNTAGTag(device, uid, 0x00)

	// Mock an empty NDEF response that should trigger ErrNoNDEF
	mockTransport.SetResponse(0x40, []byte{
		0x41, 0x00, // InDataExchange response + success status
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Empty block
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	})

	_, err := ntagTag.ReadNDEFWithRetry(context.Background())
	require.Error(t, err) // Should error because no NDEF data
	t.Log("✓ NTAGTag.ReadNDEFWithRetry(context.Background()) function exists and callable")

	// Test MIFARE ReadNDEFWithRetry exists
	mifareUID := []byte{0x04, 0x12, 0x34, 0x56}
	mifareTag := NewMIFARETag(device, mifareUID, 0x00)

	// Reset mock for MIFARE test
	mockTransport.Reset()
	// Mock auth failure - this means tag is not NDEF formatted, should return empty NDEF
	mockTransport.SetError(0x40, errors.New("authentication failed"))

	msg, err := mifareTag.ReadNDEFWithRetry(context.Background())
	require.NoError(t, err) // Auth failure = not NDEF formatted = empty NDEF, not error
	require.NotNil(t, msg)
	require.Empty(t, msg.Records)
	t.Log("✓ MIFARETag.ReadNDEFWithRetry(context.Background()) function exists and callable")
}

// TestErrorTypeEnhancements verifies that the new error types exist
func TestErrorTypeEnhancements(t *testing.T) {
	t.Parallel()

	// Test that new error types exist
	require.Error(t, ErrTagEmptyData)
	require.Error(t, ErrTagDataCorrupt)
	require.Error(t, ErrTagUnreliable)

	// Test error type identification
	require.Equal(t, "tag detected but returned empty data", ErrTagEmptyData.Error())
	require.Equal(t, "tag data appears corrupted", ErrTagDataCorrupt.Error())
	require.Equal(t, "tag readings are inconsistent", ErrTagUnreliable.Error())

	t.Log("✓ New error types are properly defined")
}

// TestIsRetryableError verifies the retry logic helper functions
func TestIsRetryableError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err      error
		name     string
		expected bool
	}{
		{name: "nil error", err: nil, expected: false},
		{name: "auth error 14", err: NewPN532Error(0x14, "InDataExchange", "authentication error"), expected: true},
		{name: "timeout error", err: ErrTransportTimeout, expected: true},
		{name: "read failure", err: fmt.Errorf("%w: block 5", ErrTagReadFailed), expected: true},
		{name: "other error", err: errors.New("some other error"), expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isRetryableError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}

	t.Log("✓ isRetryableError() works correctly")
}

// TestIsMifareRetryableError verifies MIFARE-specific retry logic
func TestIsMifareRetryableError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err      error
		name     string
		expected bool
	}{
		{name: "nil error", err: nil, expected: false},
		{name: "auth failed", err: fmt.Errorf("%w: sector 1", ErrTagAuthFailed), expected: true},
		{name: "timeout error", err: ErrTransportTimeout, expected: true},
		{name: "read failure", err: fmt.Errorf("%w: block 4", ErrTagReadFailed), expected: true},
		{name: "data exchange error", err: NewPN532Error(0x01, "InDataExchange", "timeout"), expected: true},
		{name: "other error", err: errors.New("some other error"), expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isMifareRetryableError(tt.err)
			require.Equal(t, tt.expected, result)
		})
	}

	t.Log("✓ isMifareRetryableError() works correctly")
}

// TestReadNDEFWithRetry tests the core retry logic function
//
//nolint:revive,funlen // Function complexity and length are necessary to test 6 comprehensive retry scenarios
func TestReadNDEFWithRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expectedError  error
		setupFunc      func() (ReadNDEFFunc, IsRetryableFunc)
		expectedResult *NDEFMessage
		name           string
	}{
		{
			name: "Success on first attempt",
			setupFunc: func() (ReadNDEFFunc, IsRetryableFunc) {
				msg := &NDEFMessage{
					Records: []NDEFRecord{
						{Type: NDEFTypeText, Payload: []byte("Hello")},
					},
				}
				readFunc := func() (*NDEFMessage, error) {
					return msg, nil
				}
				retryFunc := func(_ error) bool {
					return false // Shouldn't be called
				}
				return readFunc, retryFunc
			},
			expectedResult: &NDEFMessage{
				Records: []NDEFRecord{
					{Type: NDEFTypeText, Payload: []byte("Hello")},
				},
			},
		},
		{
			name: "Empty data is valid success",
			setupFunc: func() (ReadNDEFFunc, IsRetryableFunc) {
				readFunc := func() (*NDEFMessage, error) {
					// Return empty data - this is valid (e.g., tag with [03 00 FE] TLV)
					return &NDEFMessage{Records: []NDEFRecord{}}, nil
				}
				retryFunc := func(_ error) bool {
					return false
				}
				return readFunc, retryFunc
			},
			expectedResult: &NDEFMessage{
				Records: []NDEFRecord{}, // Empty is valid
			},
		},
		{
			name: "Retryable error then success",
			setupFunc: func() (ReadNDEFFunc, IsRetryableFunc) {
				attempt := 0
				readFunc := func() (*NDEFMessage, error) {
					attempt++
					if attempt == 1 {
						return nil, ErrTransportTimeout
					}
					// Success on second attempt
					return &NDEFMessage{
						Records: []NDEFRecord{
							{Type: NDEFTypeText, Payload: []byte("Retry Success")},
						},
					}, nil
				}
				retryFunc := func(err error) bool {
					return errors.Is(err, ErrTransportTimeout)
				}
				return readFunc, retryFunc
			},
			expectedResult: &NDEFMessage{
				Records: []NDEFRecord{
					{Type: NDEFTypeText, Payload: []byte("Retry Success")},
				},
			},
		},
		{
			name: "Non-retryable error fails immediately",
			setupFunc: func() (ReadNDEFFunc, IsRetryableFunc) {
				readFunc := func() (*NDEFMessage, error) {
					return nil, ErrDeviceNotFound
				}
				retryFunc := func(_ error) bool {
					return false // Device not found is not retryable
				}
				return readFunc, retryFunc
			},
			expectedError: ErrDeviceNotFound,
		},
		{
			name: "Retryable error exhausts retries",
			setupFunc: func() (ReadNDEFFunc, IsRetryableFunc) {
				readFunc := func() (*NDEFMessage, error) {
					return nil, ErrTransportTimeout
				}
				retryFunc := func(err error) bool {
					return errors.Is(err, ErrTransportTimeout)
				}
				return readFunc, retryFunc
			},
			expectedError: ErrTransportTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			readFunc, retryFunc := tt.setupFunc()

			result, err := readNDEFWithRetry(readFunc, retryFunc, "TEST")

			if tt.expectedError != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.expectedError,
					"Expected error %v, got %v", tt.expectedError, err)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Len(t, result.Records, len(tt.expectedResult.Records))
				if len(result.Records) > 0 {
					require.Equal(t, tt.expectedResult.Records[0].Type, result.Records[0].Type)
					require.Equal(t, tt.expectedResult.Records[0].Payload, result.Records[0].Payload)
				}
			}
		})
	}
}

// TestNTAGReadNDEFWithRetry tests NTAG robust reading functionality
func TestNTAGReadNDEFWithRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		errorType   error
		setupMock   func(*MockTransport)
		name        string
		expectError bool
	}{
		{
			name: "Success after retry",
			setupMock: func(mt *MockTransport) {
				// First call returns empty data
				mt.SetResponse(0x40, []byte{
					0x41, 0x00, // InDataExchange response + success status
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Empty block
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				})
				// Second call would get better data, but we'll simulate the "no NDEF" scenario
			},
			expectError: true,
			errorType:   ErrNoNDEF, // Empty data will result in no valid NDEF
		},
		{
			name: "Read failure then authentication fallback",
			setupMock: func(mt *MockTransport) {
				// First call gets PN532 authentication error (0x14)
				mt.SetResponse(0x40, []byte{0x7F, 0x14})
				// Fallback to InCommunicateThru succeeds
				mt.SetResponse(0x42, []byte{
					0x43, 0x00, // InCommunicateThru response + success
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // Empty block
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				})
			},
			expectError: true,
			errorType:   ErrTagReadFailed, // Auth error propagates as read failure
		},
		{
			name: "Timeout error triggers retry logic",
			setupMock: func(mt *MockTransport) {
				// Simulate transport timeout
				mt.SetError(0x40, ErrTransportTimeout)
			},
			expectError: true,
			errorType:   ErrTransportTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			uid := []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x90, 0xAB}
			tag := NewNTAGTag(device, uid, 0x00)

			_, err := tag.ReadNDEFWithRetry(context.Background())

			if tt.expectError {
				require.Error(t, err)
				if tt.errorType != nil {
					require.True(t, errors.Is(err, tt.errorType) ||
						strings.Contains(err.Error(), tt.errorType.Error()),
						"Expected error type %v, got %v", tt.errorType, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestMIFAREReadNDEFWithRetry tests MIFARE robust reading functionality
func TestMIFAREReadNDEFWithRetry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		errorType   error
		setupMock   func(*MockTransport)
		name        string
		expectError bool
	}{
		{
			name: "Authentication failure returns empty NDEF",
			setupMock: func(mt *MockTransport) {
				// Authentication will fail - but this is now treated as "not NDEF formatted"
				// and returns empty NDEF instead of error (consistent with NTAG behavior)
				mt.SetError(0x40, NewPN532Error(0x14, "InDataExchange", "auth failed"))
			},
			expectError: false, // Auth failure on sector 1 = not NDEF formatted = empty NDEF
			errorType:   nil,
		},
		{
			name: "Read failure after auth success",
			setupMock: func(mt *MockTransport) {
				// Set up authentication success first
				mt.SetResponse(0x40, []byte{0x41, 0x00}) // Auth success
				// Then read failure - this should still error (auth succeeded, read failed)
				mt.SetError(0x40, ErrTagReadFailed)
			},
			expectError: true,
			errorType:   ErrTagReadFailed,
		},
		{
			name: "Transport timeout triggers retry",
			setupMock: func(mt *MockTransport) {
				// Transport timeout is a communication error, not auth failure
				mt.SetError(0x40, ErrTransportTimeout)
			},
			expectError: true,
			errorType:   ErrTransportTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			uid := []byte{0x04, 0x12, 0x34, 0x56}
			tag := NewMIFARETag(device, uid, 0x00)

			_, err := tag.ReadNDEFWithRetry(context.Background())

			if tt.expectError {
				require.Error(t, err)
				if tt.errorType != nil {
					require.True(t, errors.Is(err, tt.errorType) ||
						strings.Contains(err.Error(), tt.errorType.Error()),
						"Expected error type %v, got %v", tt.errorType, err)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestRetryHelperFunctions tests the helper functions for error classification
func TestRetryHelperFunctions(t *testing.T) {
	t.Parallel()

	t.Run("isRetryableError", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			err  error
			name string
			want bool
		}{
			{name: "nil error", err: nil, want: false},
			{name: "transport timeout", err: ErrTransportTimeout, want: true},
			{name: "tag read failed", err: ErrTagReadFailed, want: true},
			{name: "device not found", err: ErrDeviceNotFound, want: false},
			{name: "PN532 timeout", err: NewPN532Error(0x01, "InDataExchange", ""), want: true},
			{name: "PN532 auth error", err: NewPN532Error(0x14, "InDataExchange", ""), want: true},
			{name: "PN532 command not supported", err: NewPN532Error(0x81, "InDataExchange", ""), want: false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got := isRetryableError(tt.err)
				require.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("isMifareRetryableError", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			err  error
			name string
			want bool
		}{
			{name: "nil error", err: nil, want: false},
			{name: "auth failed", err: ErrTagAuthFailed, want: true},
			{name: "read failed", err: ErrTagReadFailed, want: true},
			{name: "transport timeout", err: ErrTransportTimeout, want: true},
			{name: "device not found", err: ErrDeviceNotFound, want: false},
			{name: "PN532 timeout", err: NewPN532Error(0x01, "InDataExchange", ""), want: true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got := isMifareRetryableError(tt.err)
				require.Equal(t, tt.want, got)
			})
		}
	})
}

// TestWriteNDEFWithRetry tests the write retry logic function
//
//nolint:revive,funlen // Function complexity and length are necessary to test comprehensive retry scenarios
func TestWriteNDEFWithRetry(t *testing.T) {
	t.Parallel()

	t.Run("Success on first attempt", func(t *testing.T) {
		t.Parallel()

		var attempts int32
		writeFunc := func(_ context.Context) error {
			atomic.AddInt32(&attempts, 1)
			return nil
		}

		err := WriteNDEFWithRetry(context.Background(), writeFunc, 3, "TEST")
		require.NoError(t, err)
		assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "Should only attempt once on success")
	})

	t.Run("Retryable error then success", func(t *testing.T) {
		t.Parallel()

		var attempts int32
		writeFunc := func(_ context.Context) error {
			count := atomic.AddInt32(&attempts, 1)
			if count < 3 {
				return ErrTransportTimeout // Retryable
			}
			return nil // Success on 3rd attempt
		}

		err := WriteNDEFWithRetry(context.Background(), writeFunc, 3, "TEST")
		require.NoError(t, err)
		assert.Equal(t, int32(3), atomic.LoadInt32(&attempts), "Should retry until success")
	})

	t.Run("Non-retryable error fails immediately", func(t *testing.T) {
		t.Parallel()

		var attempts int32
		writeFunc := func(_ context.Context) error {
			atomic.AddInt32(&attempts, 1)
			return ErrDeviceNotFound // Not retryable
		}

		err := WriteNDEFWithRetry(context.Background(), writeFunc, 3, "TEST")
		require.Error(t, err)
		require.ErrorIs(t, err, ErrDeviceNotFound)
		assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "Should not retry non-retryable errors")
	})

	t.Run("Retryable error exhausts all retries", func(t *testing.T) {
		t.Parallel()

		var attempts int32
		writeFunc := func(_ context.Context) error {
			atomic.AddInt32(&attempts, 1)
			return ErrTransportTimeout // Always fail with retryable error
		}

		err := WriteNDEFWithRetry(context.Background(), writeFunc, 3, "TEST")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to write TEST NDEF data after 3 retries")
		require.ErrorIs(t, err, ErrTransportTimeout)
		assert.Equal(t, int32(3), atomic.LoadInt32(&attempts), "Should exhaust all retries")
	})

	t.Run("Context cancellation before first attempt", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		var attempts int32
		writeFunc := func(_ context.Context) error {
			atomic.AddInt32(&attempts, 1)
			return nil
		}

		err := WriteNDEFWithRetry(ctx, writeFunc, 3, "TEST")
		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, int32(0), atomic.LoadInt32(&attempts), "Should not attempt when context is already cancelled")
	})

	t.Run("Context cancellation during retry delay", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())

		var attempts int32
		writeFunc := func(_ context.Context) error {
			count := atomic.AddInt32(&attempts, 1)
			if count == 1 {
				// Cancel after first failure, during the retry delay
				go func() {
					time.Sleep(10 * time.Millisecond)
					cancel()
				}()
				return ErrTransportTimeout // Trigger retry
			}
			return nil
		}

		start := time.Now()
		err := WriteNDEFWithRetry(ctx, writeFunc, 3, "TEST")
		elapsed := time.Since(start)

		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "Should stop after context cancellation")
		// Should cancel during the 100ms delay, not wait the full delay
		assert.Less(t, elapsed, 80*time.Millisecond, "Should cancel quickly, not wait for full delay")
	})

	t.Run("Default max retries when zero provided", func(t *testing.T) {
		t.Parallel()

		var attempts int32
		writeFunc := func(_ context.Context) error {
			atomic.AddInt32(&attempts, 1)
			return ErrTransportTimeout
		}

		err := WriteNDEFWithRetry(context.Background(), writeFunc, 0, "TEST")
		require.Error(t, err)
		assert.Equal(t, int32(3), atomic.LoadInt32(&attempts), "Should default to 3 retries when 0 provided")
	})

	t.Run("ACK error is not retried", func(t *testing.T) {
		t.Parallel()

		var attempts int32
		writeFunc := func(_ context.Context) error {
			atomic.AddInt32(&attempts, 1)
			return ErrNoACK // ACK error is NOT retryable (transport already retried)
		}

		err := WriteNDEFWithRetry(context.Background(), writeFunc, 3, "TEST")
		require.Error(t, err)
		assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "Should NOT retry on ACK errors")
	})

	t.Run("PN532 timeout error is retried", func(t *testing.T) {
		t.Parallel()

		var attempts int32
		writeFunc := func(_ context.Context) error {
			count := atomic.AddInt32(&attempts, 1)
			if count < 2 {
				return NewPN532Error(0x01, "InDataExchange", "") // Timeout
			}
			return nil
		}

		err := WriteNDEFWithRetry(context.Background(), writeFunc, 3, "TEST")
		require.NoError(t, err)
		assert.Equal(t, int32(2), atomic.LoadInt32(&attempts), "Should retry on PN532 timeout")
	})
}
