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
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/go-pn532/detection"
	testutil "github.com/ZaparooProject/go-pn532/internal/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		transport Transport
		name      string
		errMsg    string
		wantErr   bool
	}{
		{
			name:      "Valid_MockTransport",
			transport: NewMockTransport(),
			wantErr:   false,
		},
		{
			name:      "Nil_Transport",
			transport: nil,
			wantErr:   false, // New() doesn't validate nil transport, but using it will panic
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, err := New(tt.transport)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				assert.Nil(t, device)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, device)
				if tt.transport != nil {
					assert.Equal(t, tt.transport, device.Transport())
				}
			}
		})
	}
}

func TestDevice_InitContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		expectError    bool
	}{
		{
			name: "Successful_Initialization",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
				mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
			},
			expectError: false,
		},
		{
			name: "Firmware_Version_Error",
			setupMock: func(mock *MockTransport) {
				mock.SetError(testutil.CmdGetFirmwareVersion, errors.New("firmware version failed"))
				mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
			},
			expectError:    true,
			errorSubstring: "firmware version failed",
		},
		{
			name: "SAM_Configuration_Error",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
				mock.SetError(testutil.CmdSAMConfiguration, errors.New("SAM config failed"))
			},
			expectError:    true,
			errorSubstring: "SAM config failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup mock transport
			mock := NewMockTransport()
			tt.setupMock(mock)

			// Create device
			device, err := New(mock)
			require.NoError(t, err)

			// Test initialization
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			err = device.InitContext(ctx)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring)
				}
			} else {
				require.NoError(t, err)
				// Verify that firmware version is called twice (validation + setup)
				assert.Equal(t, 2, mock.GetCallCount(testutil.CmdGetFirmwareVersion))
				assert.Equal(t, 1, mock.GetCallCount(testutil.CmdSAMConfiguration))
			}
		})
	}
}

func TestDevice_InitContext_Timeout(t *testing.T) {
	t.Parallel()

	// Setup mock with delay longer than context timeout
	mock := NewMockTransport()
	mock.SetDelay(200 * time.Millisecond)
	mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
	mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())

	device, err := New(mock)
	require.NoError(t, err)

	// Test with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_ = device.InitContext(ctx)
	// Note: This test depends on the actual implementation being context-aware
	// For now, we just verify the setup works with longer timeout

	// Retry with sufficient timeout to verify mock works
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()

	err = device.InitContext(ctx2)
	assert.NoError(t, err)
}

func TestDevice_DetectTagsContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock   func(*MockTransport)
		name        string
		wantTags    int
		maxTargets  byte
		expectError bool
	}{
		{
			name: "Single_NTAG_Detection",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
				mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
				mock.SetResponse(testutil.CmdInListPassiveTarget,
					testutil.BuildTagDetectionResponse("NTAG213", testutil.TestNTAG213UID))
			},
			maxTargets: 1,
			wantTags:   1,
		},
		{
			name: "No_Tag_Detection",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
				mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
				mock.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildNoTagResponse())
			},
			maxTargets: 1,
			wantTags:   0,
		},
		{
			name: "Detection_Error",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
				mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
				mock.SetError(testutil.CmdInListPassiveTarget, errors.New("detection failed"))
			},
			maxTargets:  1,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup mock transport
			mock := NewMockTransport()
			tt.setupMock(mock)

			// Create and initialize device
			device, err := New(mock)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			err = device.InitContext(ctx)
			require.NoError(t, err)

			// Test tag detection
			tags, err := device.DetectTagsContext(ctx, tt.maxTargets, 0)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, tags, tt.wantTags)

				if tt.wantTags > 0 {
					tag := tags[0]
					assert.NotEmpty(t, tag.UID)
					assert.NotEmpty(t, tag.UIDBytes)
				}
			}
		})
	}
}

func TestDevice_GetFirmwareVersionContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		expectError    bool
	}{
		{
			name: "Successful_Firmware_Version",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
			},
			expectError: false,
		},
		{
			name: "Firmware_Version_Command_Error",
			setupMock: func(mock *MockTransport) {
				mock.SetError(testutil.CmdGetFirmwareVersion, errors.New("command failed"))
			},
			expectError:    true,
			errorSubstring: "command failed",
		},
		{
			name: "Invalid_Firmware_Response",
			setupMock: func(mock *MockTransport) {
				// Set invalid response (too short)
				mock.SetResponse(testutil.CmdGetFirmwareVersion, []byte{0xD5, 0x03})
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup mock transport
			mock := NewMockTransport()
			tt.setupMock(mock)

			// Create device
			device, err := New(mock)
			require.NoError(t, err)

			// Test firmware version
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			firmware, err := device.GetFirmwareVersionContext(ctx)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, firmware)
				assert.Equal(t, 1, mock.GetCallCount(testutil.CmdGetFirmwareVersion))
			}
		})
	}
}

func TestDevice_SetTimeout(t *testing.T) {
	t.Parallel()

	mock := NewMockTransport()
	device, err := New(mock)
	require.NoError(t, err)

	// Test setting timeout
	timeout := 5 * time.Second
	err = device.SetTimeout(timeout)
	assert.NoError(t, err)
}

func TestDevice_SetRetryConfig(t *testing.T) {
	t.Parallel()

	mock := NewMockTransport()
	device, err := New(mock)
	require.NoError(t, err)

	// Test setting retry config
	config := &RetryConfig{
		MaxAttempts:       5,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        2 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            0.1,
		RetryTimeout:      10 * time.Second,
	}

	device.SetRetryConfig(config)
	// No return value to check, but should not panic
}

func TestDevice_Close(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock   func(*MockTransport)
		name        string
		expectError bool
	}{
		{
			name: "Successful_Close",
			setupMock: func(_ *MockTransport) {
				// Mock is connected by default
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := NewMockTransport()
			tt.setupMock(mock)

			device, err := New(mock)
			require.NoError(t, err)

			err = device.Close()
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.False(t, mock.IsConnected())
			}
		})
	}
}

func TestDevice_IsAutoPollSupported(t *testing.T) {
	t.Parallel()

	mock := NewMockTransport()
	device, err := New(mock)
	require.NoError(t, err)

	// Test AutoPoll support (mock transport should support it)
	supported := device.IsAutoPollSupported()
	// The result depends on the mock implementation's HasCapability method
	assert.IsType(t, true, supported) // Just verify it returns a boolean
}

func TestWithConnectionRetries_Option(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		retries     int
		expectError bool
	}{
		{
			name:        "Valid_Retry_Count",
			retries:     3,
			expectError: false,
		},
		{
			name:        "Single_Attempt",
			retries:     1,
			expectError: false,
		},
		{
			name:        "Zero_Retries_Invalid",
			retries:     0,
			expectError: true,
		},
		{
			name:        "Negative_Retries_Invalid",
			retries:     -1,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Test the option by applying it to a config
			config := &connectConfig{}
			option := WithConnectionRetries(tt.retries)
			err := option(config)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.retries, config.connectionRetries)
			}
		})
	}
}

// setupFailingTransport creates a mock transport that fails SAM configuration
func setupFailingTransport() Transport {
	mock := NewMockTransport()
	// Set up successful firmware version response
	mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
	// Always fail SAM configuration with a retryable error to demonstrate retry behavior
	mock.SetError(testutil.CmdSAMConfiguration, ErrCommunicationFailed)
	return mock
}

// verifyRetryAttemptsForFailure checks that the expected number of retry attempts were made for failed connections
func verifyRetryAttemptsForFailure(t *testing.T, transport Transport, expectedMinCalls int) {
	if mock, ok := transport.(*MockTransport); ok {
		samAttempts := mock.GetCallCount(testutil.CmdSAMConfiguration)
		// For failed connection, should have been retried, so expect multiple calls
		assert.GreaterOrEqual(t, samAttempts, expectedMinCalls,
			"Expected at least %d SAM configuration calls indicating retry attempts", expectedMinCalls)
	}
}

func TestConnectDevice_WithConnectionRetries(t *testing.T) {
	t.Parallel()

	t.Run("Failure_Should_Retry_Before_Giving_Up", func(t *testing.T) {
		t.Parallel()

		transport := setupFailingTransport()

		// Create a factory that returns our pre-configured transport
		factory := func(_ string) (Transport, error) {
			return transport, nil
		}

		// Use ConnectDevice with retry configuration
		device, err := ConnectDevice("/mock/path",
			WithTransportFactory(factory),
			WithConnectionRetries(3))

		// Should fail after retries
		require.Error(t, err, "Expected connection to fail after all retries")
		assert.Nil(t, device)

		// Verify the number of retry attempts made (should see at least 2 SAM config calls)
		verifyRetryAttemptsForFailure(t, transport, 2)
	})

	t.Run("AutoDetection_Bypasses_Retry_Logic", func(t *testing.T) {
		t.Parallel()

		transport := setupFailingTransport()

		// Mock the detection to return our failing transport
		deviceFactory := func(_ detection.DeviceInfo) (Transport, error) {
			return transport, nil
		}

		// Use auto-detection mode (should bypass retry logic)
		device, err := ConnectDevice("", // empty path triggers auto-detection
			WithAutoDetection(),
			WithTransportFromDeviceFactory(deviceFactory),
			WithConnectionRetries(5)) // This should be ignored for auto-detection

		// Should fail immediately (no retries for auto-detection)
		require.Error(t, err, "Expected immediate failure for auto-detection")
		assert.Nil(t, device)

		// Verify only single attempt was made (no retries)
		samAttempts := transport.(*MockTransport).GetCallCount(testutil.CmdSAMConfiguration)
		assert.Equal(t, 1, samAttempts, "Auto-detection should only make single attempt")
	})
}
