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

//nolint:dupl // Test file - similar test patterns are acceptable
package pn532

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNTAGTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		device   *Device
		expected *NTAGTag
		name     string
		uid      []byte
		sak      byte
	}{
		{
			name:   "Valid_NTAG_Creation",
			device: createMockDevice(t),
			uid:    []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC},
			sak:    0x00,
		},
		{
			name:   "Empty_UID",
			device: createMockDevice(t),
			uid:    []byte{},
			sak:    0x00,
		},
		{
			name:   "Nil_UID",
			device: createMockDevice(t),
			uid:    nil,
			sak:    0x00,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := NewNTAGTag(tt.device, tt.uid, tt.sak)

			assert.NotNil(t, result)
			assert.Equal(t, TagTypeNTAG, result.Type())
			assert.Equal(t, tt.uid, result.UIDBytes())
			assert.Equal(t, tt.device, result.device)
			assert.Equal(t, tt.sak, result.sak)
		})
	}
}

// Helper function for testing read block error handling
func checkReadBlockError(t *testing.T, err error, errorContains string, data []byte) {
	t.Helper()
	require.Error(t, err)
	if errorContains != "" {
		assert.Contains(t, err.Error(), errorContains)
	}
	assert.Nil(t, data)
}

func checkReadBlockSuccess(t *testing.T, err error, data, expectedData []byte) {
	t.Helper()
	require.NoError(t, err)
	assert.Equal(t, expectedData, data)
}

func TestNTAGTag_ReadBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		expectedData  []byte
		block         uint8
		expectError   bool
	}{
		{
			name: "Successful_Block_Read",
			setupMock: func(mt *MockTransport) {
				// NTAG ReadBlock returns 16 bytes (4 blocks) but only first 4 are used
				// Response format: 0x41 (InDataExchange response), 0x00 (success status), 16 bytes of data
				mt.SetResponse(0x40, []byte{
					0x41, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06,
					0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
				})
			},
			block:        4,
			expectError:  false,
			expectedData: []byte{0x01, 0x02, 0x03, 0x04}, // Only first 4 bytes (1 block)
		},
		{
			name: "Transport_Error",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("transport error"))
			},
			block:         4,
			expectError:   true,
			errorContains: "tag read failed",
		},
		{
			name: "PN532_Error_Response",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x40, []byte{0x41, 0x01}) // Error status = 0x01
			},
			block:         4,
			expectError:   true,
			errorContains: "tag read failed",
		},
		{
			name: "Short_Response",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x40, []byte{0x41, 0x00, 0x01, 0x02}) // Only 2 bytes data (< 4 bytes required)
			},
			block:         4,
			expectError:   true,
			errorContains: "invalid response length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)

			data, err := tag.ReadBlock(context.Background(), tt.block)

			if tt.expectError {
				checkReadBlockError(t, err, tt.errorContains, data)
			} else {
				checkReadBlockSuccess(t, err, data, tt.expectedData)
			}
		})
	}
}

func TestNTAGTag_WriteBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		data          []byte
		block         uint8
		expectError   bool
	}{
		{
			name: "Successful_Block_Write",
			setupMock: func(mt *MockTransport) {
				// Mock response for InDataExchange with WRITE command
				mt.SetResponse(0x40, []byte{0x41, 0x00}) // Success status
			},
			block:       4,
			data:        []byte{0x01, 0x02, 0x03, 0x04},
			expectError: false,
		},
		{
			name: "Transport_Error",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("transport error"))
			},
			block:         4,
			data:          []byte{0x01, 0x02, 0x03, 0x04},
			expectError:   true,
			errorContains: "tag write failed",
		},
		{
			name: "PN532_Error_Response",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x40, []byte{0x41, 0x01}) // Error status = 0x01
			},
			block:         4,
			data:          []byte{0x01, 0x02, 0x03, 0x04},
			expectError:   true,
			errorContains: "tag write failed",
		},
		{
			name: "Data_Too_Large",
			setupMock: func(_ *MockTransport) {
				// No command expected as validation should fail early
			},
			block:         4,
			data:          []byte{0x01, 0x02, 0x03, 0x04, 0x05}, // 5 bytes > 4 byte max
			expectError:   true,
			errorContains: "invalid block size",
		},
		{
			name: "Data_Too_Small",
			setupMock: func(_ *MockTransport) {
				// No command expected as validation should fail early
			},
			block:         4,
			data:          []byte{0x01, 0x02, 0x03}, // 3 bytes < 4 byte requirement
			expectError:   true,
			errorContains: "invalid block size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)

			err := tag.WriteBlock(context.Background(), tt.block, tt.data)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

type getVersionTestCase struct {
	setupMock     func(*MockTransport)
	name          string
	errorContains string
	expectError   bool
	expectVersion bool
	expectedType  NTAGType
}

func getVersionTestCases() []getVersionTestCase {
	return []getVersionTestCase{
		{
			name: "NTAG213_Version",
			setupMock: func(mt *MockTransport) {
				// GET_VERSION response for NTAG213: 0x43 response, 0x00 success status, 8 bytes version
				mt.SetResponse(0x42, []byte{0x43, 0x00, 0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x0F, 0x03})
			},
			expectVersion: true,
			expectedType:  NTAGType213,
		},
		{
			name: "NTAG215_Version",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x42, []byte{0x43, 0x00, 0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x11, 0x03})
			},
			expectVersion: true,
			expectedType:  NTAGType215,
		},
		{
			name: "NTAG216_Version",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x42, []byte{0x43, 0x00, 0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x13, 0x03})
			},
			expectVersion: true,
			expectedType:  NTAGType216,
		},
		{
			name: "Transport_Error_With_Fallback",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x42, errors.New("transport error"))
			},
			expectError:   true,
			errorContains: "transport error",
			expectVersion: true,
		},
		{
			name: "Invalid_Response_Returns_Error",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x42, []byte{0x43, 0x00, 0x01, 0x02})
			},
			expectError:   true,
			errorContains: "too short",
			expectVersion: false,
		},
		{
			name: "Invalid_Vendor_Returns_Error",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x42, []byte{0x43, 0x00, 0x00, 0xFF, 0x04, 0x02, 0x01, 0x00, 0x0F, 0x03})
			},
			expectError:   true,
			errorContains: "invalid NTAG response",
			expectVersion: false,
		},
	}
}

func TestNTAGTag_GetVersion(t *testing.T) {
	t.Parallel()

	for _, tt := range getVersionTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)

			version, err := tag.GetVersion(context.Background())

			switch {
			case tt.expectError:
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			default:
				require.NoError(t, err)
			}

			switch {
			case tt.expectVersion && tt.expectedType != NTAGTypeUnknown:
				require.NotNil(t, version)
				assert.Equal(t, tt.expectedType, version.GetNTAGType())
			case tt.expectVersion:
				require.NotNil(t, version)
			}
		})
	}
}

func TestNTAGTag_FastRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		expectedData  []byte
		startBlock    uint8
		endBlock      uint8
		expectError   bool
	}{
		{
			name: "Successful_FastRead",
			setupMock: func(mt *MockTransport) {
				// Mock FAST_READ response for blocks 4-7 (4 blocks * 4 bytes = 16 bytes)
				// FastRead uses SendRawCommand (InCommunicateThru 0x42)
				// SendRawCommand strips the 0x43 header and 0x00 status, returning only data
				// So we need to provide: 0x43, 0x00, then 16 bytes of actual data
				data := make([]byte, 18) // Header + Status + 16 bytes data
				data[0] = 0x43           // InCommunicateThru response
				data[1] = 0x00           // Success status
				for i := 2; i < 18; i++ {
					data[i] = byte(i - 2) // Fill with test data (0x00 to 0x0F)
				}
				mt.SetResponse(0x42, data)
			},
			startBlock:  4,
			endBlock:    7,
			expectError: false,
			expectedData: func() []byte {
				// FastRead should return (7-4+1) * 4 = 16 bytes
				data := make([]byte, 16)
				for i := range 16 {
					data[i] = byte(i)
				}
				return data
			}(),
		},
		{
			name: "Transport_Error",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x42, errors.New("transport error"))
			},
			startBlock:    4,
			endBlock:      7,
			expectError:   true,
			errorContains: "transport error",
		},
		{
			name: "PN532_Error_Response",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x42, []byte{0x43, 0x01}) // Error status
			},
			startBlock:    4,
			endBlock:      7,
			expectError:   true,
			errorContains: "error 0x01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)

			data, err := tag.FastRead(context.Background(), tt.startBlock, tt.endBlock)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, data)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedData, data)
			}
		})
	}
}

// Helper functions for NTAG tests

func createMockDevice(t *testing.T) *Device {
	mockTransport := NewMockTransport()
	device, err := New(mockTransport)
	require.NoError(t, err)
	return device
}

// TestNTAG215LargeDataCrash reproduces the crash reported by user with large NDEF data
func TestNTAG215LargeDataCrash(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)
	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
	tag.tagType = NTAGType215 // Force NTAG215 type

	// Create large NDEF message that approaches NTAG215's 504-byte user memory limit
	largeText := make([]byte, 480) // Large text payload
	for i := range largeText {
		largeText[i] = byte('A' + (i % 26)) // Fill with repeating alphabet
	}

	// Mock the NDEF header read (block 4) with extended length format
	// Format: [0x03] [0xFF] [high_byte] [low_byte] for extended length
	headerData := []byte{
		0x03,       // NDEF TLV type
		0xFF,       // Extended length indicator
		0x02, 0x00, // Length = 512 bytes (0x0200) - exceeds NTAG215 capacity
		// Rest of block would contain start of NDEF data
	}
	// Pad to 16 bytes (4 blocks returned by READ command)
	headerBlock := make([]byte, 16)
	copy(headerBlock, headerData)

	// Mock the block read for header
	mockTransport.SetResponse(0x40, append([]byte{0x41, 0x00}, headerBlock...))

	// This should trigger the crash due to:
	// 1. Integer overflow when calculating totalBytes = headerSize + ndefLength + 1
	// 2. Attempting to read beyond the hardcoded 64-block limit in block-by-block fallback
	// 3. Potential buffer overflow when totalBytes exceeds expected bounds

	// With our fixes, this should now fail gracefully with proper error handling
	_, err := tag.ReadNDEF(context.Background())

	// Verify our fixes work:
	// 1. No crash occurs (test completes)
	// 2. Proper error handling for oversized NDEF data
	// 3. Error message indicates the specific problem
	require.Error(t, err, "Should reject oversized NDEF data")
	assert.Contains(t, err.Error(), "exceeds tag capacity", "Error should indicate capacity exceeded")
	assert.Contains(t, err.Error(), "NTAG215", "Error should identify tag type")

	t.Logf("✓ ReadNDEF properly rejected oversized data: %v", err)
}

// TestNTAG215BlockByBlockBufferOverflow tests the specific buffer overflow in block-by-block reading
func TestNTAG215BlockByBlockBufferOverflow(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)
	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
	tag.tagType = NTAGType215

	// Force FastRead to be disabled to trigger block-by-block fallback
	fastReadDisabled := false
	tag.fastReadSupported = &fastReadDisabled

	// Create NDEF header that indicates more data than the 64-block limit can handle
	headerData := []byte{
		0x03,       // NDEF TLV type
		0xFF,       // Extended length indicator
		0x02, 0x00, // Length = 512 bytes (more than 64*4=256 byte limit)
	}
	headerBlock := make([]byte, 16)
	copy(headerBlock, headerData)

	// Mock responses for block reads
	mockTransport.SetResponse(0x40, append([]byte{0x41, 0x00}, headerBlock...))

	// Mock subsequent block reads (blocks 5-67) with test data
	for block := uint8(5); block < 68; block++ {
		blockData := make([]byte, 18) // Response header + 16 bytes data
		blockData[0] = 0x41
		blockData[1] = 0x00
		for i := 2; i < 18; i++ {
			blockData[i] = block // Fill with block number for testing
		}
		mockTransport.SetResponse(0x40, blockData)
	}

	// With our fixes, this should now use proper tag capacity bounds instead of hardcoded limits
	_, err := tag.ReadNDEF(context.Background())

	// With our fixes, this could either:
	// 1. Succeed by properly reading within tag bounds (if mock data is valid)
	// 2. Fail gracefully with proper error handling (expected for this test setup)

	// The key verification is that it doesn't crash due to buffer overflow
	// Since we're forcing block-by-block reading with insufficient mock data,
	// we expect a graceful failure rather than success
	if err != nil {
		t.Logf("✓ Block-by-block reading failed gracefully (no crash): %v", err)
		// Verify it's a proper NDEF parsing error, not a crash/panic
		assert.NotContains(t, err.Error(), "panic", "Should not contain panic messages")
		assert.NotContains(t, err.Error(), "runtime error", "Should not contain runtime errors")
	} else {
		t.Log("✓ Block-by-block reading succeeded (proper bounds checking)")
	}
}

// TestNTAGFastReadConfigLimits tests the FastRead configuration limits
func TestNTAGFastReadConfigLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		maxFastReadPages int
		expectedResult   uint8
	}{
		{
			name:             "Custom config normal value",
			maxFastReadPages: 32,
			expectedResult:   32,
		},
		{
			name:             "Custom config bounds check - exceeds uint8",
			maxFastReadPages: 300,
			expectedResult:   255, // Capped to uint8 max
		},
		{
			name:             "Custom config at uint8 boundary",
			maxFastReadPages: 255,
			expectedResult:   255,
		},
		{
			name:             "Custom config small value",
			maxFastReadPages: 1,
			expectedResult:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create device with test config
			config := &DeviceConfig{
				MaxFastReadPages: tt.maxFastReadPages,
			}
			device := &Device{
				transport: &MockTransport{},
				config:    config,
			}

			// Create NTAG tag
			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)

			// Test the getMaxFastReadPages method
			maxPages := tag.getMaxFastReadPages()

			// Verify the result matches expected bounds checking
			assert.Equal(t, tt.expectedResult, maxPages,
				"MaxFastReadPages should respect config with proper bounds checking")
		})
	}
}

// TestNTAGFastReadDefaultLimits tests that default limits are reasonable
func TestNTAGFastReadDefaultLimits(t *testing.T) {
	t.Parallel()
	// Create device with default config (0 = use platform defaults)
	config := &DeviceConfig{
		MaxFastReadPages: 0,
	}
	device := &Device{
		transport: &MockTransport{},
		config:    config,
	}

	// Create NTAG tag
	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)

	// Test the getMaxFastReadPages method
	maxPages := tag.getMaxFastReadPages()

	// Verify the result is reasonable for any platform
	// (should be either Windows UART limit of 16 or default of 60)
	assert.True(t, maxPages == 16 || maxPages == 60,
		"Default MaxFastReadPages should be either Windows UART limit (16) or default (60), got %d", maxPages)
}

// TestNTAG_GetVersionDoesNotBreakSubsequentReads is a regression test for the
// v0.8.1 → v0.8.3 NTAG read regression. The issue was that GetVersion() uses
// InCommunicateThru (0x42) which doesn't maintain the PN532's target selection
// state. Without re-selecting the target after GetVersion, subsequent
// InDataExchange calls would fail with timeout error 01.
//
// See PN532 User Manual §7.3.9: "The host controller has to take care of the
// selection of the target it wants to reach (whereas when using the
// InDataExchange command, it is done automatically)."
func TestNTAG_GetVersionDoesNotBreakSubsequentReads(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	// Setup mock responses for the full DetectType() + ReadBlock() flow:
	// 1. ReadBlock(3) for CC - InDataExchange (0x40)
	//    Response: valid NTAG CC with magic byte 0xE1
	mockTransport.SetResponse(0x40, []byte{
		0x41, 0x00, // InDataExchange response header + success status
		0xE1, 0x10, 0x3E, 0x00, // CC: magic, version, size (NTAG215), access
		0x00, 0x00, 0x00, 0x00, // Padding (NTAG returns 16 bytes)
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	})

	// 2. GetVersion() - InCommunicateThru (0x42)
	//    Response: valid NTAG215 version info
	mockTransport.SetResponse(0x42, []byte{
		0x43, 0x00, // InCommunicateThru response header + success status
		0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x11, 0x03, // NTAG215 version
	})

	// 3. InSelect (0x54) - re-select target after GetVersion
	//    This is the fix for the regression - without this, subsequent reads fail
	mockTransport.SetResponse(0x54, []byte{
		0x55, 0x00, // InSelect response header + success status
	})

	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}, 0x00)

	// Call DetectType which internally calls GetVersion (via InCommunicateThru)
	// and then InSelect to restore target selection state
	err := tag.DetectType(context.Background())
	require.NoError(t, err, "DetectType should succeed")
	assert.Equal(t, NTAGType215, tag.tagType, "Should detect NTAG215 from version info")

	// Now verify that subsequent reads work (this would fail without the InSelect fix)
	// ReadBlock uses InDataExchange which requires proper target selection
	data, err := tag.ReadBlock(context.Background(), 4)
	require.NoError(t, err, "ReadBlock after DetectType should succeed - "+
		"if this fails with timeout error 01, the InSelect fix is not working")
	assert.NotNil(t, data, "ReadBlock should return data")
}

// TestNTAG_GetVersionFailureStillAllowsReads verifies that even when GetVersion
// fails (e.g., clone device), subsequent reads still work because we call
// InSelect regardless of GetVersion success/failure.
func TestNTAG_GetVersionFailureStillAllowsReads(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	// Setup mock responses:
	// 1. ReadBlock(3) for CC - InDataExchange (0x40)
	mockTransport.SetResponse(0x40, []byte{
		0x41, 0x00,
		0xE1, 0x10, 0x12, 0x00, // CC: magic, version, size (NTAG213), access
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	})

	// 2. GetVersion() fails - InCommunicateThru (0x42) returns error
	mockTransport.SetError(0x42, errors.New("clone device: GET_VERSION not supported"))

	// 3. InSelect (0x54) - should still be called after GetVersion failure
	mockTransport.SetResponse(0x54, []byte{0x55, 0x00})

	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}, 0x00)

	// DetectType should succeed using CC-based fallback
	err := tag.DetectType(context.Background())
	require.NoError(t, err, "DetectType should succeed with CC-based fallback")
	assert.Equal(t, NTAGType213, tag.tagType, "Should detect NTAG213 from CC size field")

	// Subsequent reads should still work
	data, err := tag.ReadBlock(context.Background(), 4)
	require.NoError(t, err, "ReadBlock after failed GetVersion should succeed")
	assert.NotNil(t, data)
}

// TestNTAGTag_ContextCancellation tests that context cancellation is respected
func TestNTAGTag_ContextCancellation(t *testing.T) {
	t.Parallel()

	device, _ := createMockDeviceWithTransport(t)
	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	t.Run("ReadBlock_CancelledContext", func(t *testing.T) {
		t.Parallel()
		_, err := tag.ReadBlock(ctx, 4)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("WriteBlock_CancelledContext", func(t *testing.T) {
		t.Parallel()
		err := tag.WriteBlock(ctx, 4, []byte{0x01, 0x02, 0x03, 0x04})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("ReadNDEF_CancelledContext", func(t *testing.T) {
		t.Parallel()
		_, err := tag.ReadNDEF(ctx)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("FastRead_CancelledContext", func(t *testing.T) {
		t.Parallel()
		_, err := tag.FastRead(ctx, 4, 10)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("DetectType_CancelledContext", func(t *testing.T) {
		t.Parallel()
		err := tag.DetectType(ctx)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// TestNTAGTag_WriteNDEF tests NDEF writing functionality
func TestNTAGTag_WriteNDEF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		message       *NDEFMessage
		name          string
		errorContains string
		expectError   bool
	}{
		{
			name: "Empty_Message",
			setupMock: func(_ *MockTransport) {
				// No setup needed - validation happens before transport call
			},
			message: &NDEFMessage{
				Records: []NDEFRecord{},
			},
			expectError:   true,
			errorContains: "no NDEF records to write",
		},
		{
			name: "Write_Failure",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("write failed"))
			},
			message: &NDEFMessage{
				Records: []NDEFRecord{
					{Type: NDEFTypeText, Text: "Test"},
				},
			},
			expectError:   true,
			errorContains: "tag write failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
			tag.tagType = NTAGType215

			err := tag.WriteNDEF(context.Background(), tt.message)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestNTAGTag_WriteText tests the convenience WriteText method - error case
func TestNTAGTag_WriteText(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	// Mock write error
	mockTransport.SetError(0x40, errors.New("write failed"))

	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
	tag.tagType = NTAGType215

	err := tag.WriteText(context.Background(), "Hi")
	assert.Error(t, err)
}

// TestNTAGTag_FastRead_InvalidRange tests FastRead with invalid address range
func TestNTAGTag_FastRead_InvalidRange(t *testing.T) {
	t.Parallel()

	device, _ := createMockDeviceWithTransport(t)
	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)

	// Test invalid range (start > end)
	_, err := tag.FastRead(context.Background(), 10, 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid address range")
}

// TestNTAGTag_GetUserMemoryRange tests the memory range calculation for different tag types
func TestNTAGTag_GetUserMemoryRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		tagType       NTAGType
		expectedStart uint8
		expectedEnd   uint8
	}{
		{"NTAG213", NTAGType213, 4, 39},
		{"NTAG215", NTAGType215, 4, 129},
		{"NTAG216", NTAGType216, 4, 225},
		{"Unknown", NTAGTypeUnknown, 4, 39}, // Defaults to smallest
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, _ := createMockDeviceWithTransport(t)
			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
			tag.tagType = tt.tagType

			start, end := tag.GetUserMemoryRange()
			assert.Equal(t, tt.expectedStart, start)
			assert.Equal(t, tt.expectedEnd, end)
		})
	}
}

// TestNTAGTag_GetConfigPage tests configuration page addresses
func TestNTAGTag_GetConfigPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		tagType      NTAGType
		expectedPage uint8
	}{
		{"NTAG213", NTAGType213, 41},
		{"NTAG215", NTAGType215, 131},
		{"NTAG216", NTAGType216, 227},
		{"Unknown", NTAGTypeUnknown, 41}, // Defaults to NTAG213
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, _ := createMockDeviceWithTransport(t)
			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
			tag.tagType = tt.tagType

			page := tag.GetConfigPage()
			assert.Equal(t, tt.expectedPage, page)
		})
	}
}

// TestNTAGTag_GetPasswordPage tests password page addresses
func TestNTAGTag_GetPasswordPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		tagType      NTAGType
		expectedPage uint8
	}{
		{"NTAG213", NTAGType213, 43},
		{"NTAG215", NTAGType215, 133},
		{"NTAG216", NTAGType216, 229},
		{"Unknown", NTAGTypeUnknown, 43},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, _ := createMockDeviceWithTransport(t)
			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
			tag.tagType = tt.tagType

			page := tag.GetPasswordPage()
			assert.Equal(t, tt.expectedPage, page)
		})
	}
}

// TestNTAGTag_GetTotalPages tests total page count
func TestNTAGTag_GetTotalPages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		tagType       NTAGType
		expectedPages uint8
	}{
		{"NTAG213", NTAGType213, 45},
		{"NTAG215", NTAGType215, 135},
		{"NTAG216", NTAGType216, 231},
		{"Unknown", NTAGTypeUnknown, 45},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, _ := createMockDeviceWithTransport(t)
			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
			tag.tagType = tt.tagType

			pages := tag.GetTotalPages()
			assert.Equal(t, tt.expectedPages, pages)
		})
	}
}

// TestNTAGVersion_GetStorageSize tests storage size calculation
func TestNTAGVersion_GetStorageSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		storageSize  uint8
		expectedSize int
	}{
		{"NTAG213", 0x0F, 144},
		{"NTAG215", 0x11, 504},
		{"NTAG216", 0x13, 888},
		{"Unknown_Even", 0x10, 256}, // 2^8
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			version := &NTAGVersion{StorageSize: tt.storageSize}
			size := version.GetStorageSize()
			assert.Equal(t, tt.expectedSize, size)
		})
	}
}

// TestNTAGVersion_GetNTAGType tests NTAG type detection from version
func TestNTAGVersion_GetNTAGType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version      *NTAGVersion
		name         string
		expectedType NTAGType
	}{
		{
			name:         "NTAG213",
			version:      &NTAGVersion{VendorID: 0x04, ProductType: 0x04, StorageSize: 0x0F},
			expectedType: NTAGType213,
		},
		{
			name:         "NTAG215",
			version:      &NTAGVersion{VendorID: 0x04, ProductType: 0x04, StorageSize: 0x11},
			expectedType: NTAGType215,
		},
		{
			name:         "NTAG216",
			version:      &NTAGVersion{VendorID: 0x04, ProductType: 0x04, StorageSize: 0x13},
			expectedType: NTAGType216,
		},
		{
			name:         "Invalid_Vendor",
			version:      &NTAGVersion{VendorID: 0x00, ProductType: 0x04, StorageSize: 0x11},
			expectedType: NTAGTypeUnknown,
		},
		{
			name:         "Invalid_Product",
			version:      &NTAGVersion{VendorID: 0x04, ProductType: 0x00, StorageSize: 0x11},
			expectedType: NTAGTypeUnknown,
		},
		{
			name:         "Unknown_Storage",
			version:      &NTAGVersion{VendorID: 0x04, ProductType: 0x04, StorageSize: 0x20},
			expectedType: NTAGTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ntagType := tt.version.GetNTAGType()
			assert.Equal(t, tt.expectedType, ntagType)
		})
	}
}

// TestNTAGTag_PwdAuth tests password authentication
func TestNTAGTag_PwdAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		password      []byte
		expectError   bool
	}{
		{
			name: "Successful_Auth",
			setupMock: func(mt *MockTransport) {
				// PWD_AUTH returns 2-byte PACK on success
				mt.SetResponse(0x40, []byte{0x41, 0x00, 0xAB, 0xCD})
			},
			password:    []byte{0x01, 0x02, 0x03, 0x04},
			expectError: false,
		},
		{
			name: "Invalid_Password_Length",
			setupMock: func(_ *MockTransport) {
				// No setup needed
			},
			password:      []byte{0x01, 0x02, 0x03}, // Only 3 bytes
			expectError:   true,
			errorContains: "password must be 4 bytes",
		},
		{
			name: "Auth_Failure",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("authentication failed"))
			},
			password:      []byte{0x01, 0x02, 0x03, 0x04},
			expectError:   true,
			errorContains: "PWD_AUTH failed",
		},
		{
			name: "Invalid_PACK_Response",
			setupMock: func(mt *MockTransport) {
				// Response with only 1 byte PACK (should be 2)
				mt.SetResponse(0x40, []byte{0x41, 0x00, 0xAB})
			},
			password:      []byte{0x01, 0x02, 0x03, 0x04},
			expectError:   true,
			errorContains: "invalid PACK response length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)

			pack, err := tag.PwdAuth(context.Background(), tt.password)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, pack)
			} else {
				require.NoError(t, err)
				assert.Len(t, pack, 2)
			}
		})
	}
}

// TestNTAGTag_SetPasswordProtection tests password protection configuration
func TestNTAGTag_SetPasswordProtection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		password      []byte
		pack          []byte
		tagType       NTAGType
		auth0         uint8
		expectError   bool
	}{
		{
			name: "Invalid_Password_Length",
			setupMock: func(_ *MockTransport) {
				// No setup needed
			},
			password:      []byte{0x01, 0x02, 0x03}, // Only 3 bytes
			pack:          []byte{0x00, 0x00},
			auth0:         0x04,
			tagType:       NTAGType215,
			expectError:   true,
			errorContains: "password must be 4 bytes",
		},
		{
			name: "Invalid_PACK_Length",
			setupMock: func(_ *MockTransport) {
				// No setup needed
			},
			password:      []byte{0x01, 0x02, 0x03, 0x04},
			pack:          []byte{0x00}, // Only 1 byte
			auth0:         0x04,
			tagType:       NTAGType215,
			expectError:   true,
			errorContains: "pack must be 2 bytes",
		},
		{
			name: "Unknown_Tag_Type",
			setupMock: func(_ *MockTransport) {
				// No setup needed
			},
			password:      []byte{0x01, 0x02, 0x03, 0x04},
			pack:          []byte{0x00, 0x00},
			auth0:         0x04,
			tagType:       NTAGTypeUnknown,
			expectError:   true,
			errorContains: "unknown NTAG type",
		},
		{
			name: "Write_Password_Fails",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("write failed"))
			},
			password:      []byte{0x01, 0x02, 0x03, 0x04},
			pack:          []byte{0xAB, 0xCD},
			auth0:         0x04,
			tagType:       NTAGType213, // Use NTAG213 for smaller address space
			expectError:   true,
			errorContains: "failed to set password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
			tag.tagType = tt.tagType

			err := tag.SetPasswordProtection(context.Background(), tt.password, tt.pack, tt.auth0)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestNTAGTag_LockPage tests page locking
func TestNTAGTag_LockPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		tagType       NTAGType
		page          uint8
		expectError   bool
	}{
		{
			name:          "Cannot_Lock_System_Page",
			setupMock:     func(_ *MockTransport) {},
			page:          2, // System page
			tagType:       NTAGType213,
			expectError:   true,
			errorContains: "cannot lock system pages",
		},
		{
			name: "Lock_Static_Page_Read_Fails",
			setupMock: func(mt *MockTransport) {
				// Read current lock bytes fails
				mt.SetError(0x40, errors.New("read failed"))
			},
			page:          5,
			tagType:       NTAGType213,
			expectError:   true,
			errorContains: "tag read failed",
		},
		{
			name:          "Unknown_Tag_Type_Dynamic",
			setupMock:     func(_ *MockTransport) {},
			page:          20,
			tagType:       NTAGTypeUnknown,
			expectError:   true,
			errorContains: "unknown NTAG type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
			tag.tagType = tt.tagType

			err := tag.LockPage(context.Background(), tt.page)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestNTAGTag_SetAccessControl tests access control configuration
func TestNTAGTag_SetAccessControl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		config        AccessControlConfig
		tagType       NTAGType
		expectError   bool
	}{
		{
			name:    "Invalid_AuthFailureLimit",
			config:  AccessControlConfig{AuthFailureLimit: 10},
			tagType: NTAGType213,
			setupMock: func(_ *MockTransport) {
				// No setup needed
			},
			expectError:   true,
			errorContains: "authFailureLimit must be 0-7",
		},
		{
			name:    "Unknown_Tag_Type",
			config:  AccessControlConfig{},
			tagType: NTAGTypeUnknown,
			setupMock: func(_ *MockTransport) {
				// No setup needed
			},
			expectError:   true,
			errorContains: "unknown NTAG type",
		},
		{
			name: "Read_CFG0_Fails",
			config: AccessControlConfig{
				Protection:       true,
				ConfigLock:       true,
				AuthFailureLimit: 3,
			},
			tagType: NTAGType213,
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("read failed"))
			},
			expectError:   true,
			errorContains: "tag read failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
			tag.tagType = tt.tagType

			err := tag.SetAccessControl(context.Background(), tt.config)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestNTAGTag_detectTypeFromCapabilityContainer tests CC-based type detection
func TestNTAGTag_detectTypeFromCapabilityContainer(t *testing.T) {
	t.Parallel()

	device, _ := createMockDeviceWithTransport(t)
	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)

	tests := []struct {
		name         string
		ccData       []byte
		expectedType NTAGType
	}{
		{"NTAG213_CC", []byte{0xE1, 0x10, 0x12, 0x00}, NTAGType213},
		{"NTAG215_CC", []byte{0xE1, 0x10, 0x3E, 0x00}, NTAGType215},
		{"NTAG216_CC", []byte{0xE1, 0x10, 0x6D, 0x00}, NTAGType216},
		{"Unknown_Small", []byte{0xE1, 0x10, 0x15, 0x00}, NTAGType213},  // <= 0x20
		{"Unknown_Medium", []byte{0xE1, 0x10, 0x40, 0x00}, NTAGType215}, // 0x20 < x <= 0x50
		{"Unknown_Large", []byte{0xE1, 0x10, 0x70, 0x00}, NTAGType216},  // > 0x50
		{"Too_Short", []byte{0xE1, 0x10}, NTAGTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tag.detectTypeFromCapabilityContainer(tt.ccData)
			assert.Equal(t, tt.expectedType, result)
		})
	}
}

// TestNTAGTag_DebugInfo tests debug info generation
func TestNTAGTag_DebugInfo(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	// Mock for ReadNDEFWithRetry called by DebugInfoWithNDEF
	mockTransport.SetError(0x40, errors.New("read error"))

	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
	tag.tagType = NTAGType215

	info := tag.DebugInfo(context.Background())
	assert.NotEmpty(t, info)
	assert.Contains(t, info, "NTAG")
}

// TestNTAGTag_calculateReadRange tests read range calculation edge cases
func TestNTAGTag_calculateReadRange(t *testing.T) {
	t.Parallel()

	device, _ := createMockDeviceWithTransport(t)
	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
	tag.tagType = NTAGType215

	tests := []struct {
		name          string
		totalBytes    int
		expectedStart uint8
	}{
		{"Normal_Range", 100, 4},
		{"Negative_Bytes", -10, 4},
		{"Excessive_Bytes", 20000, 4},
		{"Zero_Bytes", 0, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tag.calculateReadRange(tt.totalBytes)
			assert.Equal(t, tt.expectedStart, result.startPage)
			// Just verify it doesn't panic and returns reasonable values
			assert.True(t, result.endPage >= result.startPage || tt.totalBytes <= 0)
		})
	}
}

// TestNTAGTag_isRetryableError tests retry logic
func TestNTAGTag_isRetryableError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err           error
		name          string
		expectedRetry bool
	}{
		{name: "Nil_Error", err: nil, expectedRetry: false},
		{name: "TagReadFailed", err: ErrTagReadFailed, expectedRetry: true},
		{name: "Generic_Error", err: errors.New("some error"), expectedRetry: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isRetryableError(tt.err)
			assert.Equal(t, tt.expectedRetry, result)
		})
	}
}

// TestCalculateStaticLockPosition tests lock bit position calculation
func TestCalculateStaticLockPosition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		page         uint8
		expectedByte byte
		expectedBit  byte
	}{
		{"Page_3", 3, 2, 0},
		{"Page_9", 9, 2, 6},
		{"Page_15", 15, 2, 7},
		{"Page_10", 10, 3, 0},
		{"Page_14", 14, 3, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lockByte, lockBit := calculateStaticLockPosition(tt.page)
			assert.Equal(t, tt.expectedByte, lockByte)
			assert.Equal(t, tt.expectedBit, lockBit)
		})
	}
}

// GetClaimedSizeFromCC Tests

func TestGetClaimedSizeFromCC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ccData   []byte
		expected int
	}{
		{
			name:     "NTAG213_CC",
			ccData:   []byte{0xE1, 0x10, 0x12, 0x00}, // 0x12 * 8 = 144 bytes
			expected: 144,
		},
		{
			name:     "NTAG215_CC",
			ccData:   []byte{0xE1, 0x10, 0x3E, 0x00}, // 0x3E * 8 = 496 bytes
			expected: 496,
		},
		{
			name:     "NTAG216_CC",
			ccData:   []byte{0xE1, 0x10, 0x6D, 0x00}, // 0x6D * 8 = 872 bytes
			expected: 872,
		},
		{
			name:     "Zero_Size",
			ccData:   []byte{0xE1, 0x10, 0x00, 0x00},
			expected: 0,
		},
		{
			name:     "Max_Size",
			ccData:   []byte{0xE1, 0x10, 0xFF, 0x00}, // 0xFF * 8 = 2040 bytes
			expected: 2040,
		},
		{
			name:     "Short_CC_Data",
			ccData:   []byte{0xE1, 0x10},
			expected: 0,
		},
		{
			name:     "Empty_CC_Data",
			ccData:   []byte{},
			expected: 0,
		},
		{
			name:     "Nil_CC_Data",
			ccData:   nil,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := GetClaimedSizeFromCC(tt.ccData)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// NTAG DetectType UID Validation Tests

func TestNTAGDetectType_UIDLengthValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		errorMsg    string
		uid         []byte
		expectError bool
	}{
		{
			name:        "Valid_7_Byte_UID",
			uid:         []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC},
			expectError: false,
		},
		{
			name:        "Invalid_4_Byte_UID",
			uid:         []byte{0x04, 0x12, 0x34, 0x56},
			expectError: true,
			errorMsg:    "UID must be 7 bytes",
		},
		{
			name:        "Invalid_Empty_UID",
			uid:         []byte{},
			expectError: true,
			errorMsg:    "UID must be 7 bytes",
		},
		{
			name:        "Invalid_10_Byte_UID",
			uid:         []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0, 0x12},
			expectError: true,
			errorMsg:    "UID must be 7 bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tag := NewNTAGTag(device, tt.uid, 0x00)

			if !tt.expectError {
				// Mock valid CC read for valid UIDs
				ccData := []byte{0xE1, 0x10, 0x12, 0x00} // Valid NTAG CC
				mockTransport.SetResponse(0x40, append([]byte{0x41, 0x00}, ccData...))
				// Mock GET_VERSION response
				versionResp := []byte{0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x11, 0x03}
				mockTransport.SetResponse(0x40, append([]byte{0x41, 0x00}, versionResp...))
			}

			err := tag.DetectType(context.Background())

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else if err != nil {
				// For valid UIDs, we just check no UID-related error
				// (may still fail on other things in mock, that's ok)
				assert.NotContains(t, err.Error(), "UID must be 7 bytes")
			}
		})
	}
}

func TestNTAGDetectType_SkipsGetVersionForCloneTags(t *testing.T) {
	t.Parallel()

	// This test verifies that for non-NXP UIDs (not starting with 0x04),
	// the code uses CC-based detection instead of GET_VERSION to avoid
	// putting clone tags into IDLE state.

	device, mockTransport := createMockDeviceWithTransport(t)

	// Use a clone tag UID (starts with 0x08, not 0x04)
	cloneUID := []byte{0x08, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}
	tag := NewNTAGTag(device, cloneUID, 0x00)

	// Mock valid CC read - this should be enough for clone tag detection
	// CC format: [Magic 0xE1] [Version] [Size] [Access]
	// Size 0x12 = 18 * 8 = 144 bytes (NTAG213-like)
	ccData := make([]byte, 16) // Read returns 4 pages = 16 bytes
	ccData[0] = 0xE1           // NDEF magic
	ccData[1] = 0x10           // Version 1.0
	ccData[2] = 0x12           // Size (144 bytes)
	ccData[3] = 0x00           // Access conditions

	mockTransport.SetResponse(0x40, append([]byte{0x41, 0x00}, ccData...))

	err := tag.DetectType(context.Background())

	// Should succeed without calling GET_VERSION
	require.NoError(t, err)
	// Tag type should be detected from CC
	assert.NotEqual(t, NTAGTypeUnknown, tag.tagType)
}

// TestNTAGDetectType_UsesCCOnlyForNXPTags verifies that even genuine NXP tags
// (UID starting with 0x04) use CC-based detection and never call GET_VERSION.
// This is a regression test for the fix that removed GET_VERSION to avoid
// timeout errors on marginal RF connections. See issue: NDEF read timeout 0x01.
func TestNTAGDetectType_UsesCCOnlyForNXPTags(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	// Use a genuine NXP tag UID (starts with 0x04)
	nxpUID := []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}
	tag := NewNTAGTag(device, nxpUID, 0x00)

	// Mock valid CC read for NTAG215 (size 0x3E)
	// CC format: [Magic 0xE1] [Version] [Size] [Access]
	ccData := make([]byte, 16) // Read returns 4 pages = 16 bytes
	ccData[0] = 0xE1           // NDEF magic
	ccData[1] = 0x10           // Version 1.0
	ccData[2] = 0x3E           // Size field for NTAG215 (504 bytes)
	ccData[3] = 0x00           // Access conditions

	// Only set up response for InDataExchange (0x40), NOT for InCommunicateThru (0x42)
	// If GetVersion was being called, it would use InCommunicateThru and fail
	mockTransport.SetResponse(0x40, append([]byte{0x41, 0x00}, ccData...))

	err := tag.DetectType(context.Background())

	require.NoError(t, err, "DetectType should succeed with CC-based detection only")
	assert.Equal(t, NTAGType215, tag.tagType, "Should detect NTAG215 from CC size field")

	// Verify InCommunicateThru was NOT called (GetVersion uses InCommunicateThru)
	assert.Equal(t, 0, mockTransport.GetCallCount(0x42),
		"InCommunicateThru should NOT be called - GetVersion is no longer used")

	// Verify InDataExchange WAS called (for CC read)
	assert.GreaterOrEqual(t, mockTransport.GetCallCount(0x40), 1,
		"InDataExchange should be called for CC read")
}

// TestNTAGTag_ReadNDEF_FudanClone tests that Fudan clone tags (UID prefix 0x1D)
// use block-by-block reading instead of FAST_READ, since Fudan FM11NT021 clones
// don't support FAST_READ (0x3A) and may return garbage or corrupt tag state.
// See: https://github.com/RfidResearchGroup/proxmark3/issues/2457
func TestNTAGTag_ReadNDEF_FudanClone(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	// Use a Fudan clone UID (starts with 0x1D)
	fudanUID := []byte{0x1D, 0x20, 0xBD, 0xC9, 0x07, 0x10, 0x80}
	tag := NewNTAGTag(device, fudanUID, 0x00)
	tag.tagType = NTAGType213

	// NDEF data split by 4-byte pages (NTAG page size)
	// ReadBlock returns only first 4 bytes of the 16-byte NTAG response
	page4 := []byte{0x03, 0x0D, 0xD1, 0x01} // TLV header + start of NDEF record
	page5 := []byte{0x09, 0x54, 0x02, 0x65} // payload len=9, type='T', UTF-8, 'e'
	page6 := []byte{0x6E, 0x48, 0x65, 0x6C} // 'n', "Hel"
	page7 := []byte{0x6C, 0x6F, 0x21, 0xFE} // "lo!", terminator

	// Helper to build 16-byte response (NTAG READ returns 4 pages)
	makeResp := func(firstPage []byte) []byte {
		resp := make([]byte, 16)
		copy(resp, firstPage)
		return resp
	}

	// Queue responses in order:
	// 1. InSelect (0x54) for target re-selection
	mockTransport.QueueResponse(0x54, []byte{0x55, 0x00})

	// 2. ReadBlock for NDEF header (page 4)
	mockTransport.QueueResponse(0x40, append([]byte{0x41, 0x00}, makeResp(page4)...))

	// 3. Block-by-block reading - queue each page's response
	mockTransport.QueueResponse(0x40, append([]byte{0x41, 0x00}, makeResp(page4)...)) // page 4
	mockTransport.QueueResponse(0x40, append([]byte{0x41, 0x00}, makeResp(page5)...)) // page 5
	mockTransport.QueueResponse(0x40, append([]byte{0x41, 0x00}, makeResp(page6)...)) // page 6
	mockTransport.QueueResponse(0x40, append([]byte{0x41, 0x00}, makeResp(page7)...)) // page 7 (has terminator)

	msg, err := tag.ReadNDEF(context.Background())

	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Len(t, msg.Records, 1)
	assert.Equal(t, NDEFTypeText, msg.Records[0].Type)
}

// TestNTAGTag_ReadNDEF_GenuineNXP tests that genuine NXP tags (UID prefix 0x04)
// still use FastRead for optimal performance.
func TestNTAGTag_ReadNDEF_GenuineNXP(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	// Use a genuine NXP UID (starts with 0x04)
	nxpUID := []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}
	tag := NewNTAGTag(device, nxpUID, 0x00)
	tag.tagType = NTAGType213

	// NDEF data for pages 4-7 (16 bytes total)
	ndefData := []byte{
		0x03, 0x0D, // NDEF TLV: type 0x03, length 13
		0xD1, 0x01, 0x09, 0x54, // NDEF record header
		0x02, 0x65, 0x6E, // Text record: UTF-8, "en"
		0x48, 0x65, 0x6C, 0x6C, 0x6F, 0x21, // "Hello!"
		0xFE, // Terminator TLV
	}

	// Pad to 16 bytes for header read response
	headerResponse := make([]byte, 16)
	copy(headerResponse, ndefData)

	// Queue responses in order:
	// 1. InSelect (0x54) for target re-selection
	mockTransport.QueueResponse(0x54, []byte{0x55, 0x00})

	// 2. ReadBlock for NDEF header (page 4) - returns first 4 bytes
	mockTransport.QueueResponse(0x40, append([]byte{0x41, 0x00}, headerResponse...))

	// 3. FastRead response (InCommunicateThru 0x42)
	// Response format: [0x43, 0x00, data...] (0x43=response, 0x00=success)
	// FastRead pages 4-7 = 16 bytes of NDEF data
	fastReadData := make([]byte, 16)
	copy(fastReadData, ndefData)
	mockTransport.QueueResponse(0x42, append([]byte{0x43, 0x00}, fastReadData...))

	// 4. InSelect (0x54) after FastRead to restore state
	mockTransport.QueueResponse(0x54, []byte{0x55, 0x00})

	msg, err := tag.ReadNDEF(context.Background())

	require.NoError(t, err)
	require.NotNil(t, msg)
	require.Len(t, msg.Records, 1)
	assert.Equal(t, NDEFTypeText, msg.Records[0].Type)
}

// --- Zero UID Detection Tests ---
// These tests verify the new behavior that distinguishes between:
// - Zero 4-byte UID (from corrupt AutoPoll data) - retryable
// - Real 4-byte UID (MIFARE Classic) - not retryable

func TestNTAGDetectType_ZeroUIDIsRetryable(t *testing.T) {
	t.Parallel()

	device, _ := createMockDeviceWithTransport(t)

	// Zero UID - this happens when AutoPoll gets corrupt data during card slide
	zeroUID := []byte{0x00, 0x00, 0x00, 0x00}
	tag := NewNTAGTag(device, zeroUID, 0x00)

	err := tag.DetectType(context.Background())

	// Should wrap with ErrTagDataCorrupt which is retryable
	require.ErrorIs(t, err, ErrTagDataCorrupt,
		"Zero UID error should be ErrTagDataCorrupt (retryable)")
	assert.Contains(t, err.Error(), "parse failed",
		"Error should indicate parse failure")
}

func TestNTAGDetectType_Real4ByteUIDIsNotRetryable(t *testing.T) {
	t.Parallel()

	device, _ := createMockDeviceWithTransport(t)

	// Real 4-byte UID - this is a MIFARE Classic tag
	realUID := []byte{0x01, 0x02, 0x03, 0x04}
	tag := NewNTAGTag(device, realUID, 0x00)

	err := tag.DetectType(context.Background())

	// Should NOT wrap with ErrTagDataCorrupt - this is a definitive "not NTAG"
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrTagDataCorrupt,
		"Real 4-byte UID error should NOT be ErrTagDataCorrupt")
	assert.Contains(t, err.Error(), "UID must be 7 bytes",
		"Error should indicate wrong UID length")
}

func TestNTAGDetectType_NonNDEFTag_DefaultsToNTAG215(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	// Valid 7-byte UID
	validUID := []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	tag := NewNTAGTag(device, validUID, 0x00)

	// Mock CC read - returns non-NDEF data (like Amiibo uses page 3 for proprietary data)
	amiiboPage3 := []byte{0xA5, 0x00, 0x00, 0x00} // No 0xE1 magic byte
	// Mock page 4 read for user data caching
	page4Data := []byte{0xA5, 0x00, 0x00, 0x04}

	mockTransport.QueueResponses(0x40,
		append([]byte{0x41, 0x00}, amiiboPage3...), // CC read - non-NDEF
		append([]byte{0x41, 0x00}, page4Data...),   // page 4 read
	)

	err := tag.DetectType(context.Background())

	// Non-NDEF tags default to NTAG215 (most common for Amiibo/Lego Dimensions)
	// We skip probing to avoid NAK errors that break subsequent reads
	require.NoError(t, err, "Non-NDEF tag detection should succeed")
	assert.Equal(t, NTAGType215, tag.tagType,
		"Non-NDEF tag should default to NTAG215")

	// Non-NDEF tag should have hasNDEF = false
	assert.False(t, tag.HasNDEF(), "Non-NDEF tag should have HasNDEF() = false")

	// ReadNDEF should return empty message without error for non-NDEF tags
	msg, err := tag.ReadNDEF(context.Background())
	require.NoError(t, err, "ReadNDEF should not error for non-NDEF tags")
	assert.Empty(t, msg.Records, "ReadNDEF should return empty message for non-NDEF tags")

	// Verify user data page was cached
	cachedPage := tag.GetCachedUserDataPage()
	assert.Equal(t, page4Data, cachedPage, "User data page should be cached")
}

func TestNTAGReadBlock_ShortResponseIsRetryable(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	tag := NewNTAGTag(device, []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}, 0x00)

	// Mock short response (only 2 bytes instead of 4+)
	mockTransport.SetResponse(0x40, []byte{0x41, 0x00, 0x01, 0x02})

	_, err := tag.ReadBlock(context.Background(), 4)

	// Should wrap with ErrTagReadFailed which is retryable
	require.ErrorIs(t, err, ErrTagReadFailed,
		"Short response error should wrap ErrTagReadFailed")
	assert.Contains(t, err.Error(), "invalid response length",
		"Error should mention invalid response length")
}

// --- Capability Container Caching Tests ---
// These tests verify the CC caching feature for Amiibo/LEGO Dimensions identification.

func TestNTAGTag_GetCachedCapabilityContainer_NilBeforeDetectType(t *testing.T) {
	t.Parallel()

	device, _ := createMockDeviceWithTransport(t)
	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}, 0x00)

	// Before DetectType is called, GetCachedCapabilityContainer should return nil
	cc := tag.GetCachedCapabilityContainer()
	assert.Nil(t, cc, "GetCachedCapabilityContainer should return nil before DetectType")
}

func TestNTAGTag_GetCachedCapabilityContainer_ReturnsCC(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)
	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}, 0x00)

	// Expected CC for NTAG215: [0xE1, 0x10, 0x3E, 0x00]
	expectedCC := []byte{0xE1, 0x10, 0x3E, 0x00}
	ccResponse := make([]byte, 16) // NTAG read returns 16 bytes
	copy(ccResponse, expectedCC)

	// Mock CC read response
	mockTransport.SetResponse(0x40, append([]byte{0x41, 0x00}, ccResponse...))

	err := tag.DetectType(context.Background())
	require.NoError(t, err)

	// After DetectType, GetCachedCapabilityContainer should return the CC
	cc := tag.GetCachedCapabilityContainer()
	require.NotNil(t, cc, "GetCachedCapabilityContainer should return cached CC after DetectType")
	assert.Equal(t, expectedCC, cc, "Cached CC should match the CC read during DetectType")
}

func TestNTAGTag_GetCachedCapabilityContainer_ReturnsNonNDEFCC(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)
	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}, 0x00)

	// Non-NDEF CC (e.g., Amiibo) - magic byte is NOT 0xE1
	nonNDEFCC := []byte{0xA5, 0x00, 0x00, 0x00}
	ccResponse := make([]byte, 16)
	copy(ccResponse, nonNDEFCC)

	// Mock responses: CC read, then probe reads for type detection
	mockTransport.QueueResponses(0x40,
		append([]byte{0x41, 0x00}, ccResponse...), // CC read
		[]byte{0x41, 0x00},                        // page 45 probe - short response = inaccessible
	)

	err := tag.DetectType(context.Background())
	require.NoError(t, err)

	// CC should still be cached even for non-NDEF tags
	cc := tag.GetCachedCapabilityContainer()
	require.NotNil(t, cc, "GetCachedCapabilityContainer should return cached CC for non-NDEF tags")
	assert.Equal(t, nonNDEFCC, cc, "Cached CC should match the CC read during DetectType")
}

func TestNTAGTag_GetCachedCapabilityContainer_ReturnsCopy(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)
	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}, 0x00)

	expectedCC := []byte{0xE1, 0x10, 0x3E, 0x00}
	ccResponse := make([]byte, 16)
	copy(ccResponse, expectedCC)

	mockTransport.SetResponse(0x40, append([]byte{0x41, 0x00}, ccResponse...))

	err := tag.DetectType(context.Background())
	require.NoError(t, err)

	// Get the cached CC
	cc1 := tag.GetCachedCapabilityContainer()
	require.NotNil(t, cc1)

	// Modify the returned slice
	cc1[0] = 0xFF

	// Get it again - should be unchanged (proves it's a copy, not reference)
	cc2 := tag.GetCachedCapabilityContainer()
	require.NotNil(t, cc2)

	assert.Equal(t, expectedCC, cc2,
		"GetCachedCapabilityContainer should return a copy, not a reference to internal state")
	assert.NotEqual(t, cc1[0], cc2[0],
		"Modifying returned CC should not affect subsequent calls")
}

func TestNTAGTag_MakeReadOnly(t *testing.T) { //nolint:funlen // table-driven test with many cases
	t.Parallel()

	// newReadSuccess creates a fresh success response for InDataExchange reads.
	// Each call allocates a new slice to prevent race conditions between parallel tests.
	newReadSuccess := func() []byte {
		return []byte{
			0x41, 0x00, // header + success
			0x00, 0x00, 0x00, 0x00, // 4 bytes data (all zeros)
			0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
			0x00, 0x00, 0x00, 0x00,
		}
	}
	// writeSuccess is safe to share (never modified by code under test)
	writeSuccess := []byte{0x41, 0x00}
	// errorResp triggers a PN532 error (status byte 0x01 = timeout)
	errorResp := []byte{0x41, 0x01}

	// queueMakeReadOnlySuccess queues responses for the full MakeReadOnly flow.
	// Call sequence: static read+write, dynamic read+write, CC read+write
	// For NTAG215/216: extra canAccessPage probe read before dynamic write (page >= 45)
	queueMakeReadOnlySuccess := func(mt *MockTransport, tagType NTAGType) {
		mt.QueueResponses(0x40,
			newReadSuccess(), writeSuccess, // static lock read + write
			newReadSuccess(), // dynamic lock read
		)
		if tagType == NTAGType215 || tagType == NTAGType216 {
			mt.QueueResponse(0x40, newReadSuccess()) // canAccessPage probe
		}
		mt.QueueResponses(0x40,
			writeSuccess,                   // dynamic lock write
			newReadSuccess(), writeSuccess, // CC read + write (last)
		)
	}

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		tagType       NTAGType
		expectError   bool
	}{
		{
			name:    "Success_NTAG213",
			tagType: NTAGType213,
			setupMock: func(mt *MockTransport) {
				queueMakeReadOnlySuccess(mt, NTAGType213)
			},
		},
		{
			name:    "Success_NTAG215",
			tagType: NTAGType215,
			setupMock: func(mt *MockTransport) {
				queueMakeReadOnlySuccess(mt, NTAGType215)
			},
		},
		{
			name:    "Success_NTAG216",
			tagType: NTAGType216,
			setupMock: func(mt *MockTransport) {
				queueMakeReadOnlySuccess(mt, NTAGType216)
			},
		},
		{
			name:    "Static_Lock_Read_Fails",
			tagType: NTAGType213,
			setupMock: func(mt *MockTransport) {
				mt.QueueResponse(0x40, errorResp) // static lock read fails (first operation)
			},
			expectError:   true,
			errorContains: "static lock bytes",
		},
		{
			name:    "Static_Lock_Write_Fails",
			tagType: NTAGType213,
			setupMock: func(mt *MockTransport) {
				mt.QueueResponses(0x40,
					newReadSuccess(), // static lock read succeeds
					errorResp,        // static lock write fails
				)
			},
			expectError:   true,
			errorContains: "static lock bytes",
		},
		{
			name:    "Dynamic_Lock_Read_Fails",
			tagType: NTAGType213,
			setupMock: func(mt *MockTransport) {
				mt.QueueResponses(0x40,
					newReadSuccess(), writeSuccess, // static lock read + write
					errorResp, // dynamic lock read fails
				)
			},
			expectError:   true,
			errorContains: "dynamic lock bytes",
		},
		{
			name:    "Dynamic_Lock_Write_Fails",
			tagType: NTAGType213,
			setupMock: func(mt *MockTransport) {
				mt.QueueResponses(0x40,
					newReadSuccess(), writeSuccess, // static lock read + write
					newReadSuccess(), // dynamic lock read succeeds
					errorResp,        // dynamic lock write fails
				)
			},
			expectError:   true,
			errorContains: "dynamic lock bytes",
		},
		{
			name:    "CC_Read_Fails",
			tagType: NTAGType213,
			setupMock: func(mt *MockTransport) {
				mt.QueueResponses(0x40,
					newReadSuccess(), writeSuccess, // static lock read + write
					newReadSuccess(), writeSuccess, // dynamic lock read + write
					errorResp, // CC read fails (last operation)
				)
			},
			expectError:   true,
			errorContains: "capability container",
		},
		{
			name:    "CC_Write_Fails",
			tagType: NTAGType213,
			setupMock: func(mt *MockTransport) {
				mt.QueueResponses(0x40,
					newReadSuccess(), writeSuccess, // static lock read + write
					newReadSuccess(), writeSuccess, // dynamic lock read + write
					newReadSuccess(), // CC read succeeds
					errorResp,        // CC write fails
				)
			},
			expectError:   true,
			errorContains: "capability container",
		},
		// Note: NTAGTypeUnknown is not tested here because validateWriteBoundary
		// (called by WriteBlock) auto-detects the type via DetectType, making the
		// "unknown NTAG type" error path in MakeReadOnly unreachable in practice.
		{
			name:          "Context_Cancelled",
			tagType:       NTAGType213,
			setupMock:     func(_ *MockTransport) {},
			expectError:   true,
			errorContains: "context canceled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}, 0x00)
			tag.tagType = tt.tagType

			ctx := context.Background()
			if tt.name == "Context_Cancelled" {
				cancelledCtx, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelledCtx
			}

			err := tag.MakeReadOnly(ctx)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
