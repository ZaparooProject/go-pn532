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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test FeliCa tag UIDs for consistent testing
var (
	testFeliCaIDm = []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	testFeliCaPMm = []byte{0x00, 0xF0, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
)

// buildFeliCaPollingResponse builds a valid FeliCa polling response for testing
func buildFeliCaPollingResponse(idm, pmm []byte, systemCode uint16) []byte {
	response := make([]byte, 19)
	response[0] = 0x01 // Polling response code
	copy(response[1:9], idm)
	copy(response[9:17], pmm)
	response[17] = byte((systemCode >> 8) & 0xFF)
	response[18] = byte(systemCode & 0xFF)
	return response
}

// buildFeliCaReadResponse builds a FeliCa read response wrapped in DataExchange format
// Response: 0x41 (DataExchange response) + 0x00 (success) + FeliCa data
func buildFeliCaReadResponse(idm, blockData []byte) []byte {
	// FeliCa read response: ResponseCode(0x07) + IDm(8) + StatusFlag1 + StatusFlag2 + BlockData
	feliCaResponse := make([]byte, 11+len(blockData))
	feliCaResponse[0] = 0x07 // Read response code
	copy(feliCaResponse[1:9], idm)
	feliCaResponse[9] = 0x00  // Status Flag 1 (success)
	feliCaResponse[10] = 0x00 // Status Flag 2 (success)
	copy(feliCaResponse[11:], blockData)

	// Wrap in DataExchange response format
	return append([]byte{0x41, 0x00}, feliCaResponse...)
}

// buildFeliCaWriteSuccessResponse builds a successful FeliCa write response wrapped in DataExchange format
func buildFeliCaWriteSuccessResponse(idm []byte) []byte {
	// FeliCa write response: ResponseCode(0x09) + IDm(8) + StatusFlag1 + StatusFlag2
	feliCaResponse := make([]byte, 11)
	feliCaResponse[0] = 0x09 // Write response code
	copy(feliCaResponse[1:9], idm)
	feliCaResponse[9] = 0x00  // Status Flag 1 (success)
	feliCaResponse[10] = 0x00 // Status Flag 2 (success)
	return append([]byte{0x41, 0x00}, feliCaResponse...)
}

// buildFeliCaWriteErrorResponse builds an error FeliCa write response wrapped in DataExchange format
func buildFeliCaWriteErrorResponse(idm []byte) []byte {
	// FeliCa write response: ResponseCode(0x09) + IDm(8) + StatusFlag1 + StatusFlag2
	feliCaResponse := make([]byte, 11)
	feliCaResponse[0] = 0x09 // Write response code
	copy(feliCaResponse[1:9], idm)
	feliCaResponse[9] = 0x01  // Status Flag 1 (error)
	feliCaResponse[10] = 0x02 // Status Flag 2 (error)
	return append([]byte{0x41, 0x00}, feliCaResponse...)
}

// buildFeliCaRequestServiceResponse builds a RequestService response wrapped in DataExchange format
func buildFeliCaRequestServiceResponse(idm []byte, nodeCount int) []byte {
	// FeliCa RequestService response: ResponseCode(0x03) + IDm(8) + NodeCount + NodeKeyVersions
	feliCaResponse := make([]byte, 10+nodeCount*2)
	feliCaResponse[0] = 0x03 // Request Service response code
	copy(feliCaResponse[1:9], idm)
	feliCaResponse[9] = byte(nodeCount) //nolint:gosec // test helper with small values
	// Node key versions (2 bytes each) - fill with zeros

	return append([]byte{0x41, 0x00}, feliCaResponse...)
}

type newFeliCaTagTestCase struct {
	name            string
	errorContains   string
	targetData      []byte
	expectedIDm     []byte
	expectedPMm     []byte
	expectedSysCode uint16
	expectError     bool
}

func getNewFeliCaTagTestCases() []newFeliCaTagTestCase {
	return []newFeliCaTagTestCase{
		{
			name:            "valid_polling_response_with_system_code",
			targetData:      buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC),
			expectedIDm:     testFeliCaIDm,
			expectedPMm:     testFeliCaPMm,
			expectedSysCode: 0x12FC,
		},
		{
			name:            "valid_polling_response_minimal",
			targetData:      buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0xFFFF)[:18],
			expectedIDm:     testFeliCaIDm,
			expectedPMm:     testFeliCaPMm,
			expectedSysCode: 0xFFFF, // Default wildcard when no system code in response
		},
		{
			name:          "target_data_too_short",
			targetData:    []byte{0x01, 0x02, 0x03},
			expectError:   true,
			errorContains: "too short",
		},
		{
			name:          "empty_target_data",
			targetData:    []byte{},
			expectError:   true,
			errorContains: "too short",
		},
	}
}

func TestNewFeliCaTag(t *testing.T) {
	t.Parallel()

	for _, tt := range getNewFeliCaTagTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device := createMockDevice(t)
			tag, err := NewFeliCaTag(device, tt.targetData)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, tag)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, tag)
			assert.Equal(t, TagTypeFeliCa, tag.Type())
			assert.Equal(t, tt.expectedIDm, tag.GetIDm())
			assert.Equal(t, tt.expectedPMm, tag.GetPMm())
			assert.Equal(t, tt.expectedSysCode, tag.GetSystemCode())
			assert.Equal(t, tt.expectedIDm, tag.UIDBytes()) // IDm is used as UID
		})
	}
}

func TestFeliCaTag_Accessors(t *testing.T) {
	t.Parallel()

	device := createMockDevice(t)
	targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)

	tag, err := NewFeliCaTag(device, targetData)
	require.NoError(t, err)

	t.Run("GetIDm", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, testFeliCaIDm, tag.GetIDm())
	})

	t.Run("GetPMm", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, testFeliCaPMm, tag.GetPMm())
	})

	t.Run("GetSetSystemCode", func(t *testing.T) {
		t.Parallel()
		// Create a separate tag for this test to avoid race conditions
		tag2, _ := NewFeliCaTag(device, targetData)

		assert.Equal(t, uint16(0x12FC), tag2.GetSystemCode())
		tag2.SetSystemCode(0xFFFF)
		assert.Equal(t, uint16(0xFFFF), tag2.GetSystemCode())
	})

	t.Run("GetSetServiceCode", func(t *testing.T) {
		t.Parallel()
		// Create a separate tag for this test to avoid race conditions
		tag3, _ := NewFeliCaTag(device, targetData)

		assert.Equal(t, uint16(feliCaServiceCodeNDEFRead), tag3.GetServiceCode())
		tag3.SetServiceCode(0x0009)
		assert.Equal(t, uint16(0x0009), tag3.GetServiceCode())
	})
}

func TestFeliCaTag_ReadBlockExtended(t *testing.T) {
	t.Parallel()

	// Standard 16-byte block data for testing
	testBlockData := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		expectedData  []byte
		block         uint16
		expectError   bool
	}{
		{
			name: "successful_read",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(0x40, buildFeliCaReadResponse(testFeliCaIDm, testBlockData))
			},
			block:        0,
			expectError:  false,
			expectedData: testBlockData,
		},
		{
			name: "read_error_status",
			setupMock: func(mock *MockTransport) {
				// Build response with error status flags
				feliCaResponse := make([]byte, 27)
				feliCaResponse[0] = 0x07 // Read response code
				copy(feliCaResponse[1:9], testFeliCaIDm)
				feliCaResponse[9] = 0x01  // Status Flag 1 (error)
				feliCaResponse[10] = 0x02 // Status Flag 2 (error)
				mock.SetResponse(0x40, append([]byte{0x41, 0x00}, feliCaResponse...))
			},
			block:         0,
			expectError:   true,
			errorContains: "failed with status",
		},
		{
			name: "invalid_response_code",
			setupMock: func(mock *MockTransport) {
				feliCaResponse := make([]byte, 27)
				feliCaResponse[0] = 0x99 // Invalid response code
				mock.SetResponse(0x40, append([]byte{0x41, 0x00}, feliCaResponse...))
			},
			block:         0,
			expectError:   true,
			errorContains: "invalid FeliCa read response code",
		},
		{
			name: "response_too_short",
			setupMock: func(mock *MockTransport) {
				// Short FeliCa response (only 5 bytes instead of minimum 12)
				mock.SetResponse(0x40, []byte{0x41, 0x00, 0x07, 0x01, 0x02})
			},
			block:         0,
			expectError:   true,
			errorContains: "too short",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
			tag, err := NewFeliCaTag(device, targetData)
			require.NoError(t, err)

			data, err := tag.ReadBlockExtended(context.Background(), tt.block)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedData, data)
			}
		})
	}
}

func TestFeliCaTag_WriteBlockExtended(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		data          []byte
		block         uint16
		expectError   bool
	}{
		{
			name: "successful_write",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(0x40, buildFeliCaWriteSuccessResponse(testFeliCaIDm))
			},
			block:       1,
			data:        make([]byte, 16),
			expectError: false,
		},
		{
			name: "write_error_status",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(0x40, buildFeliCaWriteErrorResponse(testFeliCaIDm))
			},
			block:         1,
			data:          make([]byte, 16),
			expectError:   true,
			errorContains: "failed with status",
		},
		{
			name:          "invalid_data_length_short",
			setupMock:     func(*MockTransport) {},
			block:         1,
			data:          []byte{0x01, 0x02, 0x03}, // Too short
			expectError:   true,
			errorContains: "must be exactly 16 bytes",
		},
		{
			name:          "invalid_data_length_long",
			setupMock:     func(*MockTransport) {},
			block:         1,
			data:          make([]byte, 32), // Too long
			expectError:   true,
			errorContains: "must be exactly 16 bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
			tag, err := NewFeliCaTag(device, targetData)
			require.NoError(t, err)

			err = tag.WriteBlockExtended(context.Background(), tt.block, tt.data)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFeliCaTag_RequestService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		serviceCodes  []uint16
		expectError   bool
	}{
		{
			name: "successful_request_single_service",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(0x40, buildFeliCaRequestServiceResponse(testFeliCaIDm, 1))
			},
			serviceCodes: []uint16{0x000B},
			expectError:  false,
		},
		{
			name: "successful_request_multiple_services",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(0x40, buildFeliCaRequestServiceResponse(testFeliCaIDm, 2))
			},
			serviceCodes: []uint16{0x000B, 0x0009},
			expectError:  false,
		},
		{
			name:          "empty_service_codes",
			setupMock:     func(*MockTransport) {},
			serviceCodes:  []uint16{},
			expectError:   true,
			errorContains: "invalid service code count",
		},
		{
			name:          "too_many_service_codes",
			setupMock:     func(*MockTransport) {},
			serviceCodes:  make([]uint16, 33), // Max is 32
			expectError:   true,
			errorContains: "invalid service code count",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
			tag, err := NewFeliCaTag(device, targetData)
			require.NoError(t, err)

			_, err = tag.RequestService(context.Background(), tt.serviceCodes)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFeliCaTag_ValidateAIB(t *testing.T) {
	t.Parallel()

	device := createMockDevice(t)
	targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
	tag, err := NewFeliCaTag(device, targetData)
	require.NoError(t, err)

	tests := []struct {
		name     string
		aib      []byte
		expected bool
	}{
		{
			name: "valid_aib",
			aib: func() []byte {
				aib := make([]byte, 16)
				aib[0] = 0x10 // Version
				// Calculate checksum
				var sum uint16
				for i := range 14 {
					sum += uint16(aib[i])
				}
				aib[14] = byte((sum >> 8) & 0xFF)
				aib[15] = byte(sum & 0xFF)
				return aib
			}(),
			expected: true,
		},
		{
			name: "invalid_checksum",
			aib: func() []byte {
				aib := make([]byte, 16)
				aib[0] = 0x10
				aib[14] = 0xFF // Wrong checksum
				aib[15] = 0xFF
				return aib
			}(),
			expected: false,
		},
		{
			name:     "aib_too_short",
			aib:      []byte{0x10, 0x00, 0x00},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tag.validateAIB(tt.aib)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFeliCaTag_UpdateAIBWithLength(t *testing.T) {
	t.Parallel()

	device := createMockDevice(t)
	targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
	tag, err := NewFeliCaTag(device, targetData)
	require.NoError(t, err)

	// Create a valid AIB
	originalAIB := make([]byte, 16)
	originalAIB[0] = 0x10 // Version

	tests := []struct {
		name          string
		expectedBytes []byte
		ndefLength    uint32
	}{
		{
			name:          "zero_length",
			ndefLength:    0,
			expectedBytes: []byte{0x00, 0x00, 0x00},
		},
		{
			name:          "small_length",
			ndefLength:    0x123,
			expectedBytes: []byte{0x00, 0x01, 0x23},
		},
		{
			name:          "large_length",
			ndefLength:    0xABCDEF,
			expectedBytes: []byte{0xAB, 0xCD, 0xEF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			newAIB := tag.updateAIBWithLength(originalAIB, tt.ndefLength)

			// Check NDEF length bytes
			assert.Equal(t, tt.expectedBytes[0], newAIB[11])
			assert.Equal(t, tt.expectedBytes[1], newAIB[12])
			assert.Equal(t, tt.expectedBytes[2], newAIB[13])

			// Verify checksum is valid
			assert.True(t, tag.validateAIB(newAIB), "AIB checksum should be valid")
		})
	}
}

func TestFeliCaTag_ValidateWritePermissions(t *testing.T) {
	t.Parallel()

	device := createMockDevice(t)
	targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
	tag, err := NewFeliCaTag(device, targetData)
	require.NoError(t, err)

	tests := []struct {
		name          string
		errorContains string
		aib           []byte
		expectError   bool
	}{
		{
			name: "writable",
			aib: func() []byte {
				aib := make([]byte, 16)
				aib[9] = 0x00  // Write flag (writable)
				aib[10] = 0x00 // RW flag (read-write)
				return aib
			}(),
			expectError: false,
		},
		{
			name: "write_protected",
			aib: func() []byte {
				aib := make([]byte, 16)
				aib[9] = 0x0F // Write flag (write-protected)
				aib[10] = 0x00
				return aib
			}(),
			expectError:   true,
			errorContains: "write-protected",
		},
		{
			name: "read_only",
			aib: func() []byte {
				aib := make([]byte, 16)
				aib[9] = 0x00
				aib[10] = 0x01 // RW flag (read-only)
				return aib
			}(),
			expectError:   true,
			errorContains: "read-only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tag.validateWritePermissions(tt.aib)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFeliCaTag_ValidateDataSize(t *testing.T) {
	t.Parallel()

	device := createMockDevice(t)
	targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
	tag, err := NewFeliCaTag(device, targetData)
	require.NoError(t, err)

	tests := []struct {
		name        string
		data        []byte
		maxBytes    uint32
		expectError bool
	}{
		{
			name:        "within_capacity",
			data:        make([]byte, 100),
			maxBytes:    1000,
			expectError: false,
		},
		{
			name:        "at_capacity",
			data:        make([]byte, 1000),
			maxBytes:    1000,
			expectError: false,
		},
		{
			name:        "exceeds_capacity",
			data:        make([]byte, 1001),
			maxBytes:    1000,
			expectError: true,
		},
		{
			name:        "empty_data",
			data:        []byte{},
			maxBytes:    1000,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tag.validateDataSize(tt.data, tt.maxBytes)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFeliCaTag_ReadBlock_UsesReadBlockExtended(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	testBlockData := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	mockTransport.SetResponse(0x40, buildFeliCaReadResponse(testFeliCaIDm, testBlockData))

	targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
	tag, err := NewFeliCaTag(device, targetData)
	require.NoError(t, err)

	// ReadBlock should delegate to ReadBlockExtended
	data, err := tag.ReadBlock(context.Background(), 5)
	require.NoError(t, err)
	assert.Len(t, data, 16)
}

func TestFeliCaTag_WriteBlock_UsesWriteBlockExtended(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	mockTransport.SetResponse(0x40, buildFeliCaWriteSuccessResponse(testFeliCaIDm))

	targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
	tag, err := NewFeliCaTag(device, targetData)
	require.NoError(t, err)

	// WriteBlock should delegate to WriteBlockExtended
	err = tag.WriteBlock(context.Background(), 5, make([]byte, 16))
	require.NoError(t, err)
}

func TestFeliCaTag_Constants(t *testing.T) {
	t.Parallel()

	// Verify FeliCa constants match JIS X 6319-4 specification
	assert.Equal(t, byte(0x00), byte(feliCaCmdPolling))
	assert.Equal(t, byte(0x02), byte(feliCaCmdRequestService))
	assert.Equal(t, byte(0x04), byte(feliCaCmdRequestResponse))
	assert.Equal(t, byte(0x06), byte(feliCaCmdReadWithoutEncryption))
	assert.Equal(t, byte(0x08), byte(feliCaCmdWriteWithoutEncryption))
	assert.Equal(t, byte(0x0A), byte(feliCaCmdAuthentication))

	assert.Equal(t, uint16(0x12FC), uint16(feliCaSystemCodeNDEF))
	assert.Equal(t, uint16(0xFFFF), uint16(feliCaSystemCodeCommon))

	assert.Equal(t, uint16(0x000B), uint16(feliCaServiceCodeNDEFRead))
	assert.Equal(t, uint16(0x0009), uint16(feliCaServiceCodeNDEFWrite))

	assert.Equal(t, 16, feliCaBlockSize)
	assert.Equal(t, 8, feliCaIDmLength)
	assert.Equal(t, 8, feliCaPMmLength)
}

// buildFeliCaPollingSuccessResponse builds a successful FeliCa polling response
func buildFeliCaPollingSuccessResponse(idm, pmm []byte, systemCode uint16) []byte {
	// Polling response: ResponseCode(0x01) + IDm(8) + PMm(8) + SystemCode(2)
	feliCaResponse := make([]byte, 19)
	feliCaResponse[0] = 0x01 // Polling response code
	copy(feliCaResponse[1:9], idm)
	copy(feliCaResponse[9:17], pmm)
	feliCaResponse[17] = byte((systemCode >> 8) & 0xFF)
	feliCaResponse[18] = byte(systemCode & 0xFF)
	return append([]byte{0x41, 0x00}, feliCaResponse...)
}

func TestFeliCaTag_Polling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		systemCode    uint16
		expectError   bool
	}{
		{
			name: "successful_polling",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(0x40, buildFeliCaPollingSuccessResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC))
			},
			systemCode:  0x12FC,
			expectError: false,
		},
		{
			name: "polling_error_response_too_short",
			setupMock: func(mock *MockTransport) {
				// Short response
				mock.SetResponse(0x40, []byte{0x41, 0x00, 0x01, 0x02, 0x03})
			},
			systemCode:    0x12FC,
			expectError:   true,
			errorContains: "too short",
		},
		{
			name: "polling_invalid_response_code",
			setupMock: func(mock *MockTransport) {
				feliCaResponse := make([]byte, 19)
				feliCaResponse[0] = 0x99 // Invalid response code
				mock.SetResponse(0x40, append([]byte{0x41, 0x00}, feliCaResponse...))
			},
			systemCode:    0x12FC,
			expectError:   true,
			errorContains: "invalid FeliCa polling response code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
			tag, err := NewFeliCaTag(device, targetData)
			require.NoError(t, err)

			err = tag.Polling(context.Background(), tt.systemCode)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				// Verify system code was updated
				assert.Equal(t, uint16(0x12FC), tag.GetSystemCode())
			}
		})
	}
}

// buildValidAIB creates a valid AIB with proper checksum for testing
func buildValidAIB(version byte, maxBlocks uint16, ndefLength uint32, rwFlag byte) []byte {
	aib := make([]byte, 16)
	aib[0] = version
	aib[3] = byte((maxBlocks >> 8) & 0xFF)
	aib[4] = byte(maxBlocks & 0xFF)
	aib[9] = 0x00 // Write flag (writable)
	aib[10] = rwFlag
	aib[11] = byte((ndefLength >> 16) & 0xFF)
	aib[12] = byte((ndefLength >> 8) & 0xFF)
	aib[13] = byte(ndefLength & 0xFF)
	// Calculate checksum
	var sum uint16
	for i := range 14 {
		sum += uint16(aib[i])
	}
	aib[14] = byte((sum >> 8) & 0xFF)
	aib[15] = byte(sum & 0xFF)
	return aib
}

func TestFeliCaTag_WriteNDEF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		message       *NDEFMessage
		name          string
		errorContains string
		expectError   bool
	}{
		{
			name:          "nil_message",
			setupMock:     func(_ *MockTransport) {},
			message:       nil,
			expectError:   true,
			errorContains: "cannot be nil",
		},
		{
			name: "aib_read_failure",
			setupMock: func(mock *MockTransport) {
				// Return error status on read
				feliCaResponse := make([]byte, 27)
				feliCaResponse[0] = 0x07
				copy(feliCaResponse[1:9], testFeliCaIDm)
				feliCaResponse[9] = 0x01 // Error status
				feliCaResponse[10] = 0x01
				mock.SetResponse(0x40, append([]byte{0x41, 0x00}, feliCaResponse...))
			},
			message: &NDEFMessage{
				Records: []NDEFRecord{
					{Type: NDEFTypeText, Text: "Test"},
				},
			},
			expectError:   true,
			errorContains: "failed to read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
			tag, err := NewFeliCaTag(device, targetData)
			require.NoError(t, err)

			err = tag.WriteNDEF(context.Background(), tt.message)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestFeliCaTag_WriteText(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	// Setup for successful write
	validAIB := buildValidAIB(0x10, 100, 0, 0x00)
	mockTransport.QueueResponse(0x40, buildFeliCaReadResponse(testFeliCaIDm, validAIB))
	// Multiple write responses for AIB + data blocks
	for range 5 {
		mockTransport.QueueResponse(0x40, buildFeliCaWriteSuccessResponse(testFeliCaIDm))
	}

	targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
	tag, err := NewFeliCaTag(device, targetData)
	require.NoError(t, err)

	err = tag.WriteText(context.Background(), "Hello")
	require.NoError(t, err)
}

func TestFeliCaTag_ReadNDEF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		expectError   bool
	}{
		{
			name: "invalid_aib_checksum",
			setupMock: func(mock *MockTransport) {
				// Invalid AIB (bad checksum)
				invalidAIB := make([]byte, 16)
				invalidAIB[0] = 0x10
				invalidAIB[14] = 0xFF // Wrong checksum
				invalidAIB[15] = 0xFF
				mock.SetResponse(0x40, buildFeliCaReadResponse(testFeliCaIDm, invalidAIB))
			},
			expectError:   true,
			errorContains: "invalid attribute information block",
		},
		{
			name: "unsupported_version",
			setupMock: func(mock *MockTransport) {
				validAIB := buildValidAIB(0x20, 100, 0, 0x00) // Wrong version
				mock.SetResponse(0x40, buildFeliCaReadResponse(testFeliCaIDm, validAIB))
			},
			expectError:   true,
			errorContains: "unsupported NDEF version",
		},
		{
			name: "empty_ndef_message",
			setupMock: func(mock *MockTransport) {
				validAIB := buildValidAIB(0x10, 100, 0, 0x00) // Zero NDEF length
				mock.SetResponse(0x40, buildFeliCaReadResponse(testFeliCaIDm, validAIB))
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
			tag, err := NewFeliCaTag(device, targetData)
			require.NoError(t, err)

			message, err := tag.ReadNDEF(context.Background())

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, message)
			}
		})
	}
}

func TestFeliCaTag_DebugInfo(t *testing.T) {
	t.Parallel()

	device, mockTransport := createMockDeviceWithTransport(t)

	// Mock failed read for NDEF (to test debug info generation)
	mockTransport.SetError(0x40, assert.AnError)

	targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
	tag, err := NewFeliCaTag(device, targetData)
	require.NoError(t, err)

	info := tag.DebugInfo(context.Background())
	assert.NotEmpty(t, info)
	assert.Contains(t, info, "FeliCa")
}

func TestFeliCaTag_RestoreSystemCodes(t *testing.T) {
	t.Parallel()

	device := createMockDevice(t)
	targetData := buildFeliCaPollingResponse(testFeliCaIDm, testFeliCaPMm, 0x12FC)
	tag, err := NewFeliCaTag(device, targetData)
	require.NoError(t, err)

	// Set initial values
	tag.SetSystemCode(0x1234)
	tag.SetServiceCode(0x5678)

	// Call restoreSystemCodes (internal method test)
	tag.restoreSystemCodes(0xAAAA, 0xBBBB)

	assert.Equal(t, uint16(0xAAAA), tag.GetSystemCode())
	assert.Equal(t, uint16(0xBBBB), tag.GetServiceCode())
}
