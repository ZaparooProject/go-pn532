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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBaseTag_Type(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tagType  TagType
		expected TagType
	}{
		{
			name:     "NTAG_Type",
			tagType:  TagTypeNTAG,
			expected: TagTypeNTAG,
		},
		{
			name:     "MIFARE_Type",
			tagType:  TagTypeMIFARE,
			expected: TagTypeMIFARE,
		},
		{
			name:     "FeliCa_Type",
			tagType:  TagTypeFeliCa,
			expected: TagTypeFeliCa,
		},
		{
			name:     "Unknown_Type",
			tagType:  TagTypeUnknown,
			expected: TagTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag := &BaseTag{
				tagType: tt.tagType,
			}

			result := tag.Type()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBaseTag_UID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected string
		uid      []byte
	}{
		{
			name:     "4_byte_UID",
			uid:      []byte{0x04, 0x56, 0x78, 0x9A},
			expected: "0456789a",
		},
		{
			name:     "7_byte_UID_NTAG",
			uid:      []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC},
			expected: "04123456789abc",
		},
		{
			name:     "Empty_UID",
			uid:      []byte{},
			expected: "",
		},
		{
			name:     "Single_byte_UID",
			uid:      []byte{0xFF},
			expected: "ff",
		},
		{
			name:     "Zero_UID",
			uid:      []byte{0x00, 0x00, 0x00, 0x00},
			expected: "00000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag := &BaseTag{
				uid: tt.uid,
			}

			result := tag.UID()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBaseTag_UIDBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		uid      []byte
		expected []byte
	}{
		{
			name:     "4_byte_UID",
			uid:      []byte{0x04, 0x56, 0x78, 0x9A},
			expected: []byte{0x04, 0x56, 0x78, 0x9A},
		},
		{
			name:     "7_byte_UID_NTAG",
			uid:      []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC},
			expected: []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC},
		},
		{
			name:     "Empty_UID",
			uid:      []byte{},
			expected: []byte{},
		},
		{
			name:     "Nil_UID",
			uid:      nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag := &BaseTag{
				uid: tt.uid,
			}

			result := tag.UIDBytes()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBaseTag_IsMIFARE4K(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		sak      byte
		expected bool
	}{
		{
			name:     "MIFARE_Classic_4K",
			sak:      0x18,
			expected: true,
		},
		{
			name:     "MIFARE_Classic_1K",
			sak:      0x08,
			expected: false,
		},
		{
			name:     "NTAG_SAK",
			sak:      0x00,
			expected: false,
		},
		{
			name:     "Unknown_SAK",
			sak:      0xFF,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag := &BaseTag{
				sak: tt.sak,
			}

			result := tag.IsMIFARE4K()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBaseTag_ReadBlock(t *testing.T) {
	t.Parallel()

	tag := &BaseTag{}
	data, err := tag.ReadBlock(4)

	require.Error(t, err)
	assert.Nil(t, data)
	assert.Equal(t, ErrNotImplemented, err)
}

func TestBaseTag_WriteBlock(t *testing.T) {
	t.Parallel()

	tag := &BaseTag{}
	err := tag.WriteBlock(4, []byte{0x01, 0x02, 0x03, 0x04})

	require.Error(t, err)
	assert.Equal(t, ErrNotImplemented, err)
}

func TestBaseTag_ReadNDEF(t *testing.T) {
	t.Parallel()

	tag := &BaseTag{}
	data, err := tag.ReadNDEF()

	require.Error(t, err)
	assert.Nil(t, data)
	assert.Equal(t, ErrNotImplemented, err)
}

func TestBaseTag_WriteNDEF(t *testing.T) {
	t.Parallel()

	tag := &BaseTag{}
	message := &NDEFMessage{
		Records: []NDEFRecord{
			{Type: NDEFTypeText, Text: "Hello"},
		},
	}
	err := tag.WriteNDEF(message)

	require.Error(t, err)
	assert.Equal(t, ErrNotImplemented, err)
}

func TestBaseTag_ReadText(t *testing.T) {
	t.Parallel()

	// Create a BaseTag with mock device to test ReadText logic
	mockTransport := NewMockTransport()
	device, err := New(mockTransport)
	require.NoError(t, err)

	tag := &BaseTag{
		device:  device,
		tagType: TagTypeNTAG,
		uid:     []byte{0x04, 0x12, 0x34, 0x56},
	}

	// Test ReadText calls ReadNDEF (which will return ErrNotImplemented)
	text, err := tag.ReadText()
	require.Error(t, err)
	assert.Empty(t, text)
	assert.Equal(t, ErrNotImplemented, err)
}

func TestBaseTag_WriteText(t *testing.T) {
	t.Parallel()

	// Create a BaseTag with mock device
	mockTransport := NewMockTransport()
	device, err := New(mockTransport)
	require.NoError(t, err)

	tag := &BaseTag{
		device:  device,
		tagType: TagTypeNTAG,
		uid:     []byte{0x04, 0x12, 0x34, 0x56},
	}

	// Test WriteText calls WriteNDEF (which will return ErrNotImplemented)
	err = tag.WriteText("Hello, World!")
	require.Error(t, err)
	assert.Equal(t, ErrNotImplemented, err)
}

func TestBaseTag_Summary(t *testing.T) {
	t.Parallel()

	tag := &BaseTag{
		tagType: TagTypeNTAG,
		uid:     []byte{0x04, 0x12, 0x34, 0x56},
	}
	summary := tag.Summary()

	assert.NotEmpty(t, summary)
	assert.Contains(t, summary, "NTAG")
	assert.Contains(t, summary, "04123456")
}

func TestBaseTag_DebugInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tagType TagType
		uid     []byte
		sak     byte
	}{
		{
			name:    "NTAG_Tag",
			tagType: TagTypeNTAG,
			uid:     []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC},
			sak:     0x00,
		},
		{
			name:    "MIFARE_4K_Tag",
			tagType: TagTypeMIFARE,
			uid:     []byte{0x04, 0x56, 0x78, 0x9A},
			sak:     0x18,
		},
		{
			name:    "MIFARE_1K_Tag",
			tagType: TagTypeMIFARE,
			uid:     []byte{0x04, 0x56, 0x78, 0x9A},
			sak:     0x08,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag := &BaseTag{
				tagType: tt.tagType,
				uid:     tt.uid,
				sak:     tt.sak,
			}

			result := tag.DebugInfo()

			// Verify the debug info contains expected information
			assert.Contains(t, result, string(tt.tagType))
			assert.Contains(t, result, tag.UID())
			assert.Contains(t, result, "SAK:")
		})
	}
}

func TestBaseTag_DebugInfoWithNDEF(t *testing.T) {
	t.Parallel()

	// Create a BaseTag with mock device
	mockTransport := NewMockTransport()
	device, err := New(mockTransport)
	require.NoError(t, err)

	tag := &BaseTag{
		device:  device,
		tagType: TagTypeNTAG,
		uid:     []byte{0x04, 0x12, 0x34, 0x56},
		sak:     0x00,
	}

	// Pass the tag itself as the NDEF reader interface (it implements ReadNDEF)
	result := tag.DebugInfoWithNDEF(tag)

	// Should contain basic debug info
	assert.Contains(t, result, "NTAG")
	assert.Contains(t, result, tag.UID())
	// Should also indicate NDEF read failed (since BaseTag doesn't implement ReadNDEF)
	assert.Contains(t, result, "NDEF:")
}

func TestDetectedTag_Structure(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tag := DetectedTag{
		DetectedAt:     now,
		UID:            "04123456",
		Type:           TagTypeNTAG,
		UIDBytes:       []byte{0x04, 0x12, 0x34, 0x56},
		ATQ:            []byte{0x00, 0x44},
		TargetData:     []byte{0x04, 0x12, 0x34, 0x56, 0x78},
		SAK:            0x00,
		TargetNumber:   1,
		FromInAutoPoll: true,
	}

	// Verify all fields are properly set
	assert.Equal(t, now, tag.DetectedAt)
	assert.Equal(t, "04123456", tag.UID)
	assert.Equal(t, TagTypeNTAG, tag.Type)
	assert.Equal(t, []byte{0x04, 0x12, 0x34, 0x56}, tag.UIDBytes)
	assert.Equal(t, []byte{0x00, 0x44}, tag.ATQ)
	assert.Equal(t, []byte{0x04, 0x12, 0x34, 0x56, 0x78}, tag.TargetData)
	assert.Equal(t, byte(0x00), tag.SAK)
	assert.Equal(t, byte(1), tag.TargetNumber)
	assert.True(t, tag.FromInAutoPoll)
}

func TestTagType_Constants(t *testing.T) {
	t.Parallel()

	// Verify tag type constants are defined and unique
	assert.NotEmpty(t, TagTypeNTAG)
	assert.NotEmpty(t, TagTypeMIFARE)
	assert.NotEmpty(t, TagTypeFeliCa)
	assert.NotEmpty(t, TagTypeUnknown)
	assert.NotEmpty(t, TagTypeAny)

	// Verify they are all different
	types := []TagType{TagTypeNTAG, TagTypeMIFARE, TagTypeFeliCa, TagTypeUnknown, TagTypeAny}
	for i, t1 := range types {
		for j, t2 := range types {
			if i != j {
				assert.NotEqual(t, t1, t2, "Tag types should be unique: %s vs %s", t1, t2)
			}
		}
	}
}

// NTAG Operation Tests

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
			errorContains: "failed to read block",
		},
		{
			name: "PN532_Error_Response",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x40, []byte{0x41, 0x01}) // Error status = 0x01
			},
			block:         4,
			expectError:   true,
			errorContains: "failed to read block",
		},
		{
			name: "Short_Response",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x40, []byte{0x41, 0x00, 0x01, 0x02}) // Only 2 bytes data (< 4 bytes required)
			},
			block:         4,
			expectError:   true,
			errorContains: "invalid read response length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)

			data, err := tag.ReadBlock(tt.block)

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
			errorContains: "failed to write block",
		},
		{
			name: "PN532_Error_Response",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x40, []byte{0x41, 0x01}) // Error status = 0x01
			},
			block:         4,
			data:          []byte{0x01, 0x02, 0x03, 0x04},
			expectError:   true,
			errorContains: "failed to write block",
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

			err := tag.WriteBlock(tt.block, tt.data)

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

func TestNTAGTag_GetVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		expectError   bool
		expectedType  NTAGType
	}{
		{
			name: "NTAG213_Version",
			setupMock: func(mt *MockTransport) {
				// Mock GET_VERSION response for NTAG213 using InCommunicateThru (0x42)
				// Response format: 0x43 (InCommunicateThru response), 0x00 (success status), 8 bytes version data
				mt.SetResponse(0x42, []byte{0x43, 0x00, 0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x0F, 0x03})
			},
			expectError:  false,
			expectedType: NTAGType213,
		},
		{
			name: "NTAG215_Version",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x42, []byte{0x43, 0x00, 0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x11, 0x03})
			},
			expectError:  false,
			expectedType: NTAGType215,
		},
		{
			name: "NTAG216_Version",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x42, []byte{0x43, 0x00, 0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x13, 0x03})
			},
			expectError:  false,
			expectedType: NTAGType216,
		},
		{
			name: "Transport_Error_With_Fallback",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x42, errors.New("transport error"))
			},
			expectError:   true, // Error returned but with fallback version
			errorContains: "transport error",
		},
		{
			name: "Invalid_Response_With_Fallback",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x42, []byte{0x43, 0x00, 0x01, 0x02}) // Invalid short response
			},
			expectError: false, // Should succeed with fallback version
		},
		{
			name: "Invalid_Vendor_With_Fallback",
			setupMock: func(mt *MockTransport) {
				// Invalid vendor ID (not 0x04) - should use fallback
				mt.SetResponse(0x42, []byte{0x43, 0x00, 0x00, 0xFF, 0x04, 0x02, 0x01, 0x00, 0x0F, 0x03})
			},
			expectError: false, // Should succeed with fallback version
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)

			version, err := tag.GetVersion()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				// Even with error, should still return a fallback version
				assert.NotNil(t, version)
			} else {
				// For successful cases or fallback cases
				assert.NotNil(t, version)
				if tt.expectedType != NTAGTypeUnknown {
					assert.Equal(t, tt.expectedType, version.GetNTAGType())
				}
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
				for i := 0; i < 16; i++ {
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
			errorContains: "error: 01",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)

			data, err := tag.FastRead(tt.startBlock, tt.endBlock)

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

func createMockDeviceWithTransport(t *testing.T) (*Device, *MockTransport) {
	mockTransport := NewMockTransport()
	device, err := New(mockTransport)
	require.NoError(t, err)
	return device, mockTransport
}

// NDEF Message Tests

func TestNDEFMessage_Structure(t *testing.T) {
	t.Parallel()

	message := &NDEFMessage{
		Records: []NDEFRecord{
			{Type: NDEFTypeText, Text: "Hello, World!"},
			{Type: NDEFTypeURI, URI: "https://example.com"},
		},
	}

	assert.Len(t, message.Records, 2)
	assert.Equal(t, NDEFTypeText, message.Records[0].Type)
	assert.Equal(t, "Hello, World!", message.Records[0].Text)
	assert.Equal(t, NDEFTypeURI, message.Records[1].Type)
	assert.Equal(t, "https://example.com", message.Records[1].URI)
}

func TestNDEFRecord_Structure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		record NDEFRecord
	}{
		{
			name: "Text_Record",
			record: NDEFRecord{
				Type: NDEFTypeText,
				Text: "Hello, World!",
			},
		},
		{
			name: "URI_Record",
			record: NDEFRecord{
				Type: NDEFTypeURI,
				URI:  "https://example.com",
			},
		},
		{
			name: "Payload_Record",
			record: NDEFRecord{
				Type:    NDEFTypeText,
				Payload: []byte{0x01, 0x02, 0x03, 0x04},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Verify record structure is properly set
			assert.NotEmpty(t, tt.record.Type)
			if tt.record.Text != "" {
				assert.NotEmpty(t, tt.record.Text)
			}
			if tt.record.URI != "" {
				assert.NotEmpty(t, tt.record.URI)
			}
			if tt.record.Payload != nil {
				assert.NotEmpty(t, tt.record.Payload)
			}
		})
	}
}

func TestBuildNDEFMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		text        string
		expectError bool
	}{
		{
			name:        "Valid_Text",
			text:        "Hello, World!",
			expectError: false,
		},
		{
			name:        "Empty_Text",
			text:        "",
			expectError: false,
		},
		{
			name:        "Long_Text",
			text:        "This is a longer text message to test NDEF encoding with more content.",
			expectError: false,
		},
		{
			name:        "Unicode_Text",
			text:        "Hello, Café! 🌍",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := BuildNDEFMessage(tt.text)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, data)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, data)
				assert.NotEmpty(t, data)

				// Verify the data starts with NDEF header (0x03)
				assert.Equal(t, byte(0x03), data[0])
			}
		})
	}
}

func TestBuildNDEFMessageEx(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		errorContains string
		records       []NDEFRecord
		expectError   bool
	}{
		{
			name: "Single_Text_Record",
			records: []NDEFRecord{
				{Type: NDEFTypeText, Text: "Hello, World!"},
			},
			expectError: false,
		},
		{
			name: "Multiple_Records",
			records: []NDEFRecord{
				{Type: NDEFTypeText, Text: "Hello, World!"},
				{Type: NDEFTypeURI, URI: "https://example.com"},
			},
			expectError: false,
		},
		{
			name: "Single_URI_Record",
			records: []NDEFRecord{
				{Type: NDEFTypeURI, URI: "https://example.com"},
			},
			expectError: false,
		},
		{
			name:          "Empty_Records",
			records:       []NDEFRecord{},
			expectError:   true,
			errorContains: "no records to build",
		},
		{
			name: "Large_Payload_Record",
			records: []NDEFRecord{
				{Type: NDEFTypeText, Payload: make([]byte, MaxNDEFPayloadSize+1)},
			},
			expectError:   true,
			errorContains: "payload size",
		},
		{
			name: "Too_Many_Records",
			records: func() []NDEFRecord {
				records := make([]NDEFRecord, MaxNDEFRecordCount+1)
				for i := range records {
					records[i] = NDEFRecord{Type: NDEFTypeText, Text: "Test"}
				}
				return records
			}(),
			expectError:   true,
			errorContains: "record count",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := BuildNDEFMessageEx(tt.records)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, data)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, data)
				assert.NotEmpty(t, data)

				// Verify the data starts with NDEF header (0x03)
				assert.Equal(t, byte(0x03), data[0])
			}
		})
	}
}

func TestParseNDEFMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupData     func() []byte
		name          string
		errorContains string
		expectedCount int
		expectError   bool
	}{
		{
			name: "Valid_Text_Message",
			setupData: func() []byte {
				// Build a simple text message and return the data
				data, err := BuildNDEFMessage("Hello, World!")
				require.NoError(t, err)
				return data
			},
			expectError:   false,
			expectedCount: 1,
		},
		{
			name: "Valid_Multiple_Records",
			setupData: func() []byte {
				records := []NDEFRecord{
					{Type: NDEFTypeText, Text: "Hello"},
					{Type: NDEFTypeURI, URI: "https://example.com"},
				}
				data, err := BuildNDEFMessageEx(records)
				require.NoError(t, err)
				return data
			},
			expectError:   false,
			expectedCount: 2,
		},
		{
			name: "Empty_Data",
			setupData: func() []byte {
				return []byte{}
			},
			expectError:   true,
			errorContains: "invalid NDEF message",
		},
		{
			name: "Invalid_Header",
			setupData: func() []byte {
				return []byte{0x01, 0x02, 0x03} // Invalid NDEF header
			},
			expectError:   true,
			errorContains: "invalid NDEF message",
		},
		{
			name: "Truncated_Data",
			setupData: func() []byte {
				return []byte{0x03, 0x05} // Valid header but truncated
			},
			expectError:   true,
			errorContains: "invalid NDEF message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := tt.setupData()
			message, err := ParseNDEFMessage(data)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, message)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, message)
				assert.Len(t, message.Records, tt.expectedCount)
			}
		})
	}
}

func TestNDEF_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		records []NDEFRecord
	}{
		{
			name: "Text_Record",
			records: []NDEFRecord{
				{Type: NDEFTypeText, Text: "Hello, World!"},
			},
		},
		{
			name: "URI_Record",
			records: []NDEFRecord{
				{Type: NDEFTypeURI, URI: "https://example.com"},
			},
		},
		{
			name: "Multiple_Records",
			records: []NDEFRecord{
				{Type: NDEFTypeText, Text: "Test Message"},
				{Type: NDEFTypeURI, URI: "https://test.example"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Build NDEF message
			data, err := BuildNDEFMessageEx(tt.records)
			require.NoError(t, err)
			require.NotNil(t, data)

			// Parse it back
			message, err := ParseNDEFMessage(data)
			require.NoError(t, err)
			require.NotNil(t, message)

			// Verify record count matches
			assert.Len(t, message.Records, len(tt.records))

			// Verify record content
			verifyNDEFRecords(t, tt.records, message.Records)
		})
	}
}

func verifyNDEFRecords(t *testing.T, original, parsed []NDEFRecord) {
	t.Helper()

	// Verify record content (for simple cases)
	for i, originalRecord := range original {
		if i < len(parsed) {
			parsedRecord := parsed[i]
			assert.Equal(t, originalRecord.Type, parsedRecord.Type)
			if originalRecord.Text != "" {
				assert.Equal(t, originalRecord.Text, parsedRecord.Text)
			}
			if originalRecord.URI != "" {
				assert.Equal(t, originalRecord.URI, parsedRecord.URI)
			}
		}
	}
}

// MIFARE Classic Tests

// TestMIFAREConfig returns test-friendly MIFARE configuration with minimal delays
// This should only be used in tests to speed up test execution
// testMIFAREConfig returns test-friendly MIFARE configuration with minimal delays
// This should only be used in tests to speed up test execution
func testMIFAREConfig() *MIFAREConfig {
	return &MIFAREConfig{
		RetryConfig: &RetryConfig{
			MaxAttempts:       5,
			InitialBackoff:    1 * time.Microsecond,
			MaxBackoff:        10 * time.Microsecond,
			BackoffMultiplier: 2.0,
			Jitter:            0.1,
			RetryTimeout:      1 * time.Second,
		},
		HardwareDelay: 1 * time.Microsecond, // Minimal hardware timing for tests
	}
}

// newTestMIFARETag creates a MIFARE tag with fast test configuration
func newTestMIFARETag(device *Device, uid []byte, sak byte) *MIFARETag {
	tag := NewMIFARETag(device, uid, sak)
	tag.SetConfig(testMIFAREConfig()) // Apply fast test timing
	return tag
}

func TestNewMIFARETag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		device   *Device
		expected *MIFARETag
		name     string
		uid      []byte
		sak      byte
	}{
		{
			name:   "Valid_MIFARE_Creation",
			device: createMockDevice(t),
			uid:    []byte{0x04, 0x56, 0x78, 0x9A},
			sak:    0x08, // MIFARE Classic 1K
		},
		{
			name:   "MIFARE_4K_Creation",
			device: createMockDevice(t),
			uid:    []byte{0x04, 0x12, 0x34, 0x56},
			sak:    0x18, // MIFARE Classic 4K
		},
		{
			name:   "Empty_UID",
			device: createMockDevice(t),
			uid:    []byte{},
			sak:    0x08,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := NewMIFARETag(tt.device, tt.uid, tt.sak)

			assert.NotNil(t, result)
			assert.Equal(t, TagTypeMIFARE, result.Type())
			assert.Equal(t, tt.uid, result.UIDBytes())
			assert.Equal(t, tt.device, result.device)
			assert.Equal(t, tt.sak, result.sak)
			assert.Equal(t, -1, result.lastAuthSector) // Should start unauthenticated
		})
	}
}

func getMIFAREReadBlockTestCases() []struct {
	setupMock     func(*MockTransport)
	setupAuth     func(*MIFARETag)
	name          string
	errorContains string
	expectedData  []byte
	block         uint8
	expectError   bool
} {
	return []struct {
		setupMock     func(*MockTransport)
		setupAuth     func(*MIFARETag)
		name          string
		errorContains string
		expectedData  []byte
		block         uint8
		expectError   bool
	}{
		{
			name: "Successful_Block_Read",
			setupMock: func(mt *MockTransport) {
				data := make([]byte, 18)
				data[0] = 0x41
				data[1] = 0x00
				for i := 2; i < 18; i++ {
					data[i] = byte(i - 2)
				}
				mt.SetResponse(0x40, data)
			},
			setupAuth: func(tag *MIFARETag) {
				tag.authMutex.Lock()
				tag.lastAuthSector = 1
				tag.authMutex.Unlock()
			},
			block:       4,
			expectError: false,
			expectedData: []byte{
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
			},
		},
		{
			name: "Not_Authenticated_Error",
			setupMock: func(_ *MockTransport) {
				// No setup needed
			},
			setupAuth: func(_ *MIFARETag) {
				// Leave unauthenticated
			},
			block:         4,
			expectError:   true,
			errorContains: "not authenticated to sector",
		},
		{
			name: "Transport_Error",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("transport error"))
			},
			setupAuth: func(tag *MIFARETag) {
				tag.authMutex.Lock()
				tag.lastAuthSector = 1
				tag.authMutex.Unlock()
			},
			block:         4,
			expectError:   true,
			errorContains: "failed to read block",
		},
		{
			name: "Short_Response",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x40, []byte{0x41, 0x00, 0x01, 0x02})
			},
			setupAuth: func(tag *MIFARETag) {
				tag.authMutex.Lock()
				tag.lastAuthSector = 1
				tag.authMutex.Unlock()
			},
			block:         4,
			expectError:   true,
			errorContains: "invalid read response length",
		},
	}
}

func TestMIFARETag_ReadBlock(t *testing.T) {
	t.Parallel()

	tests := getMIFAREReadBlockTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := newTestMIFARETag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x08)
			tt.setupAuth(tag)

			data, err := tag.ReadBlock(tt.block)

			if tt.expectError {
				checkReadBlockError(t, err, tt.errorContains, data)
			} else {
				checkReadBlockSuccess(t, err, data, tt.expectedData)
			}
		})
	}
}

func TestMIFARETag_WriteBlock(t *testing.T) {
	t.Parallel()

	tests := getMIFAREWriteBlockTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := newTestMIFARETag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x08)
			tt.setupAuth(tag)

			err := tag.WriteBlock(tt.block, tt.data)

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

func getMIFAREWriteBlockTestCases() []struct {
	setupMock     func(*MockTransport)
	setupAuth     func(*MIFARETag)
	name          string
	errorContains string
	data          []byte
	block         uint8
	expectError   bool
} {
	cases := []struct {
		setupMock     func(*MockTransport)
		setupAuth     func(*MIFARETag)
		name          string
		errorContains string
		data          []byte
		block         uint8
		expectError   bool
	}{}

	cases = append(cases, getMIFAREWriteSuccessCases()...)
	cases = append(cases, getMIFAREWriteErrorCases()...)

	return cases
}

func getMIFAREWriteSuccessCases() []struct {
	setupMock     func(*MockTransport)
	setupAuth     func(*MIFARETag)
	name          string
	errorContains string
	data          []byte
	block         uint8
	expectError   bool
} {
	return []struct {
		setupMock     func(*MockTransport)
		setupAuth     func(*MIFARETag)
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
			setupAuth: func(tag *MIFARETag) {
				// Simulate authenticated state for sector 1 (block 4)
				tag.authMutex.Lock()
				tag.lastAuthSector = 1
				tag.authMutex.Unlock()
			},
			block:       4,
			data:        make([]byte, 16), // 16 bytes for MIFARE Classic
			expectError: false,
		},
	}
}

func getMIFAREWriteErrorCases() []struct {
	setupMock     func(*MockTransport)
	setupAuth     func(*MIFARETag)
	name          string
	errorContains string
	data          []byte
	block         uint8
	expectError   bool
} {
	return []struct {
		setupMock     func(*MockTransport)
		setupAuth     func(*MIFARETag)
		name          string
		errorContains string
		data          []byte
		block         uint8
		expectError   bool
	}{
		{
			name: "Not_Authenticated_Error",
			setupMock: func(_ *MockTransport) {
				// No setup needed - should fail before transport call
			},
			setupAuth: func(_ *MIFARETag) {
				// Leave unauthenticated
			},
			block:         4,
			data:          make([]byte, 16),
			expectError:   true,
			errorContains: "not authenticated to sector",
		},
		{
			name: "Invalid_Block_Size",
			setupMock: func(_ *MockTransport) {
				// No setup needed - should fail validation
			},
			setupAuth: func(tag *MIFARETag) {
				tag.authMutex.Lock()
				tag.lastAuthSector = 1
				tag.authMutex.Unlock()
			},
			block:         4,
			data:          []byte{0x01, 0x02, 0x03}, // Wrong size (< 16 bytes)
			expectError:   true,
			errorContains: "invalid block size",
		},
		{
			name: "Manufacturer_Block_Protection",
			setupMock: func(_ *MockTransport) {
				// No setup needed - should fail validation
			},
			setupAuth: func(tag *MIFARETag) {
				tag.authMutex.Lock()
				tag.lastAuthSector = 0
				tag.authMutex.Unlock()
			},
			block:         0, // Manufacturer block
			data:          make([]byte, 16),
			expectError:   true,
			errorContains: "cannot write to manufacturer block",
		},
		{
			name: "Transport_Error",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("transport error"))
			},
			setupAuth: func(tag *MIFARETag) {
				tag.authMutex.Lock()
				tag.lastAuthSector = 1
				tag.authMutex.Unlock()
			},
			block:         4,
			data:          make([]byte, 16),
			expectError:   true,
			errorContains: "failed to write block",
		},
	}
}

func TestMIFARETag_Authenticate(t *testing.T) {
	t.Parallel()

	tests := getMIFAREAuthenticateTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := newTestMIFARETag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x08)

			err := tag.Authenticate(tt.sector, tt.keyType, tt.key)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				// Should reset auth state on failure
				assert.Equal(t, -1, tag.lastAuthSector)
			} else {
				require.NoError(t, err)
				// Should update auth state on success
				assert.Equal(t, int(tt.sector), tag.lastAuthSector)
				assert.Equal(t, tt.keyType, tag.lastAuthKeyType)
			}
		})
	}
}

func getMIFAREAuthenticateTestCases() []struct {
	setupMock     func(*MockTransport)
	name          string
	errorContains string
	key           []byte
	sector        uint8
	keyType       byte
	expectError   bool
} {
	return []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		key           []byte
		sector        uint8
		keyType       byte
		expectError   bool
	}{
		{
			name: "Successful_Authentication_KeyA",
			setupMock: func(mt *MockTransport) {
				// Mock successful authentication response
				mt.SetResponse(0x40, []byte{0x41, 0x00}) // Success status
			},
			sector:      1,
			keyType:     0x00,                                       // Key A
			key:         []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, // Default key
			expectError: false,
		},
		{
			name: "Successful_Authentication_KeyB",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x40, []byte{0x41, 0x00}) // Success status
			},
			sector:      1,
			keyType:     0x01, // Key B
			key:         []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			expectError: false,
		},
		{
			name: "Invalid_Key_Length",
			setupMock: func(_ *MockTransport) {
				// No setup needed - should fail validation
			},
			sector:        1,
			keyType:       0x00,
			key:           []byte{0xFF, 0xFF, 0xFF}, // Wrong length (< 6 bytes)
			expectError:   true,
			errorContains: "MIFARE key must be 6 bytes",
		},
		{
			name: "Invalid_Key_Type",
			setupMock: func(_ *MockTransport) {
				// No setup needed - should fail validation
			},
			sector:        1,
			keyType:       0x02, // Invalid key type
			key:           []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			expectError:   true,
			errorContains: "invalid key type",
		},
		{
			name: "Authentication_Failed",
			setupMock: func(mt *MockTransport) {
				// Mock authentication failure (error 0x14 = wrong key)
				mt.SetResponse(0x40, []byte{0x41, 0x14}) // Error status
			},
			sector:        1,
			keyType:       0x00,
			key:           []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // Wrong key
			expectError:   true,
			errorContains: "authentication failed",
		},
		{
			name: "Transport_Error",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("transport error"))
			},
			sector:        1,
			keyType:       0x00,
			key:           []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
			expectError:   true,
			errorContains: "authentication failed",
		},
	}
}

func TestMIFARETag_ReadBlockDirect(t *testing.T) {
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
			name: "Successful_Direct_Read",
			setupMock: func(mt *MockTransport) {
				// Mock successful direct read (no authentication required)
				data := make([]byte, 18) // Status + 16 bytes data
				data[0] = 0x41           // InDataExchange response
				data[1] = 0x00           // Success status
				for i := 2; i < 18; i++ {
					data[i] = byte(i - 2) // Fill with test data
				}
				mt.SetResponse(0x40, data)
			},
			block:       4,
			expectError: false,
			expectedData: []byte{
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
			},
		},
		{
			name: "Fallback_To_CommunicateThru",
			setupMock: func(mt *MockTransport) {
				// First call (InDataExchange) fails with error 01, second call (InCommunicateThru) succeeds
				mt.SetError(0x40, errors.New("data exchange error: 01"))

				// Setup InCommunicateThru response
				data := make([]byte, 18) // Header + Status + 16 bytes data
				data[0] = 0x43           // InCommunicateThru response
				data[1] = 0x00           // Success status
				for i := 2; i < 18; i++ {
					data[i] = byte(i - 2) // Fill with test data
				}
				mt.SetResponse(0x42, data)
			},
			block:       4,
			expectError: false,
			expectedData: []byte{
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F,
			},
		},
		{
			name: "Transport_Error",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("transport error"))
			},
			block:         4,
			expectError:   true,
			errorContains: "failed to read block",
		},
		{
			name: "Short_Response",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x40, []byte{0x41, 0x00, 0x01, 0x02}) // Only 2 bytes data
			},
			block:         4,
			expectError:   true,
			errorContains: "invalid read response length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := newTestMIFARETag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x08)

			data, err := tag.ReadBlockDirect(tt.block)

			if tt.expectError {
				checkReadBlockError(t, err, tt.errorContains, data)
			} else {
				checkReadBlockSuccess(t, err, data, tt.expectedData)
			}
		})
	}
}

func TestMIFARETag_WriteBlockDirect(t *testing.T) {
	t.Parallel()

	tests := getMIFAREWriteBlockDirectTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create mock transport and device
			mt := NewMockTransport()
			tt.setupMock(mt)

			device := &Device{transport: mt}

			// Create MIFARE tag
			uid := []byte{0x04, 0x56, 0x78, 0x9A}
			tag := newTestMIFARETag(device, uid, 0x08) // MIFARE Classic 1K SAK

			// Test WriteBlockDirect
			err := tag.WriteBlockDirect(tt.block, tt.data)

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

func getMIFAREWriteBlockDirectTestCases() []struct {
	setupMock     func(*MockTransport)
	name          string
	errorContains string
	data          []byte
	block         uint8
	expectError   bool
} {
	return []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		data          []byte
		block         uint8
		expectError   bool
	}{
		{
			name: "Successful_Direct_Write_via_Fallback",
			setupMock: func(mt *MockTransport) {
				// Simulate read timeout to trigger SendRawCommand fallback
				mt.SetError(0x40, errors.New("data exchange error: 01"))

				// Setup response for readBlockCommunicateThru fallback (SendRawCommand)
				readData := make([]byte, 18)
				readData[0] = 0x43 // InCommunicateThru response
				readData[1] = 0x00 // Success status
				for i := 2; i < 18; i++ {
					readData[i] = byte(i - 2)
				}
				mt.SetResponse(0x42, readData) // SendRawCommand for read validation

				// Note: The write will also fail with timeout error and use writeBlockDirectAlternative
				// which eventually calls writeBlockCommunicateThru via SendRawCommand
				// This tests the full fallback chain for clone tags
			},
			block: 4,
			data: []byte{
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
			},
		},
		{
			name: "Successful_Direct_Write_Normal_Path",
			setupMock: func(mt *MockTransport) {
				// For read validation (first call to SendDataExchange)
				readData := make([]byte, 18)
				readData[0] = 0x41 // InDataExchange response
				readData[1] = 0x00 // Success status
				for i := 2; i < 18; i++ {
					readData[i] = byte(i - 2)
				}
				mt.SetResponse(0x40, readData)

				// The write will also use SendDataExchange but MockTransport
				// will return the same response (which is fine for success case)
			},
			block: 4,
			data: []byte{
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
			},
		},
		{
			name: "Invalid_Block_Size",
			setupMock: func(_ *MockTransport) {
				// No setup needed - validation happens before transport call
			},
			block:         4,
			data:          []byte{0x01, 0x02, 0x03}, // Too short
			expectError:   true,
			errorContains: "invalid block size",
		},
		{
			name: "Manufacturer_Block_Protection",
			setupMock: func(_ *MockTransport) {
				// No setup needed - validation happens before transport call
			},
			block: 0, // Manufacturer block
			data: []byte{
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
			},
			expectError:   true,
			errorContains: "cannot write to manufacturer block",
		},
		{
			name: "Read_Validation_Failure",
			setupMock: func(mt *MockTransport) {
				// Set error for both SendDataExchange and SendRawCommand
				mt.SetError(0x40, errors.New("data exchange error: 14"))
				mt.SetError(0x42, errors.New("raw read command failed"))
			},
			block: 4,
			data: []byte{
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10,
			},
			expectError:   true,
			errorContains: "clone tag does not support direct block access",
		},
	}
}

func TestMIFARETag_ReadNDEF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		expectError   bool
	}{
		{
			name: "Authentication_Failure",
			setupMock: func(mt *MockTransport) {
				// Setup authentication failure for sector 1
				mt.SetError(0x40, errors.New("authentication failed"))
			},
			expectError:   true,
			errorContains: "failed to read NDEF data",
		},
		{
			name: "Empty_NDEF_Data",
			setupMock: func(mt *MockTransport) {
				// Setup authentication success
				authData := []byte{0x41, 0x00}
				mt.SetResponse(0x40, authData)

				// Setup empty response for reads - this will trigger TLV parsing error
				emptyResponse := make([]byte, 18)
				emptyResponse[0] = 0x41
				emptyResponse[1] = 0x00
				// All data bytes remain 0x00
				mt.SetResponse(0x40, emptyResponse)
			},
			expectError:   true,
			errorContains: "invalid NDEF message", // Updated to match actual error
		},
		{
			name: "Communication_Error_During_Read",
			setupMock: func(mt *MockTransport) {
				// Setup authentication success first
				authData := []byte{0x41, 0x00}
				mt.SetResponse(0x40, authData)

				// Then setup error for subsequent read operations
				mt.SetError(0x40, errors.New("communication error"))
			},
			expectError:   true,
			errorContains: "communication error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create mock transport and device
			mt := NewMockTransport()
			tt.setupMock(mt)

			device := &Device{transport: mt}

			// Create MIFARE tag
			uid := []byte{0x04, 0x56, 0x78, 0x9A}
			tag := newTestMIFARETag(device, uid, 0x08)

			// Test ReadNDEF
			message, err := tag.ReadNDEF()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, message)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, message)
			}
		})
	}
}

// Helper function for MIFARE tag error testing
// Helper function for MIFARE tag setup and error checking
func setupMIFARETagTest(t *testing.T, setupMock func(*MockTransport)) (*MIFARETag, *MockTransport) {
	t.Helper()
	mt := NewMockTransport()
	setupMock(mt)
	device := &Device{transport: mt}
	uid := []byte{0x04, 0x56, 0x78, 0x9A}
	tag := newTestMIFARETag(device, uid, 0x08)
	return tag, mt
}

// Helper function for MIFARE tag error checking
func checkMIFARETagError(t *testing.T, err error, expectError bool, errorContains string) {
	t.Helper()
	switch expectError {
	case true:
		require.Error(t, err)
		if errorContains != "" {
			assert.Contains(t, err.Error(), errorContains)
		}
	case false:
		assert.NoError(t, err)
	}
}

func getMIFAREWriteNDEFTestCases() []struct {
	setupMock     func(*MockTransport)
	message       *NDEFMessage
	name          string
	errorContains string
	expectError   bool
} {
	return []struct {
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
			name: "Authentication_Failure",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("authentication failed"))
			},
			message: &NDEFMessage{
				Records: []NDEFRecord{
					{
						Type: NDEFTypeText,
						Text: "Test",
					},
				},
			},
			expectError:   true,
			errorContains: "cannot authenticate to tag",
		},
		{
			name: "Message_Too_Large",
			setupMock: func(mt *MockTransport) {
				authData := []byte{0x41, 0x00}
				mt.SetResponse(0x40, authData)
			},
			message: &NDEFMessage{
				Records: []NDEFRecord{
					{
						Type: NDEFTypeText,
						Text: string(make([]byte, 2000)),
					},
				},
			},
			expectError:   true,
			errorContains: "NDEF message too large",
		},
		{
			name: "Valid_Small_Message",
			setupMock: func(mt *MockTransport) {
				authData := []byte{0x41, 0x00}
				mt.SetResponse(0x40, authData)
				writeData := []byte{0x41, 0x00}
				mt.SetResponse(0x40, writeData)
			},
			message: &NDEFMessage{
				Records: []NDEFRecord{
					{
						Type: NDEFTypeText,
						Text: "Hi",
					},
				},
			},
			expectError: false,
		},
	}
}

func TestMIFARETag_WriteNDEF(t *testing.T) {
	t.Parallel()

	tests := getMIFAREWriteNDEFTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag, _ := setupMIFARETagTest(t, tt.setupMock)

			// Test WriteNDEF
			err := tag.WriteNDEF(tt.message)
			checkMIFARETagError(t, err, tt.expectError, tt.errorContains)
		})
	}
}

func TestMIFARETag_ResetAuthState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		expectError   bool
	}{
		{
			name: "Successful_Reset",
			setupMock: func(mt *MockTransport) {
				// Setup successful InListPassiveTarget response
				resetData := []byte{0xD5, 0x4B, 0x01, 0x01, 0x00, 0x04, 0x08, 0x04, 0x56, 0x78, 0x9A}
				mt.SetResponse(0x4A, resetData) // InListPassiveTarget
			},
		},
		{
			name: "Reset_Communication_Error",
			setupMock: func(mt *MockTransport) {
				// Setup communication error
				mt.SetError(0x4A, errors.New("communication error"))
			},
			expectError:   true,
			errorContains: "communication error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create mock transport and device
			mt := NewMockTransport()
			tt.setupMock(mt)

			device := &Device{transport: mt}

			// Create MIFARE tag and set some auth state
			uid := []byte{0x04, 0x56, 0x78, 0x9A}
			tag := newTestMIFARETag(device, uid, 0x08)

			// Simulate previous auth state
			tag.lastAuthSector = 5
			tag.lastAuthKeyType = 0x01

			// Test ResetAuthState
			err := tag.ResetAuthState()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				// Verify auth state was cleared
				assert.Equal(t, -1, tag.lastAuthSector)
				assert.Equal(t, byte(0), tag.lastAuthKeyType)
			}
		})
	}
}

func TestMIFARETag_WriteText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		text          string
		errorContains string
		expectError   bool
	}{
		{
			name: "Successful_Text_Write",
			setupMock: func(mt *MockTransport) {
				// Setup authentication and write success
				successData := []byte{0x41, 0x00}
				mt.SetResponse(0x40, successData)
			},
			text: "Hello World",
		},
		{
			name: "Authentication_Failure",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("authentication failed"))
			},
			text:          "Test",
			expectError:   true,
			errorContains: "cannot authenticate to tag",
		},
		{
			name: "Empty_Text",
			setupMock: func(mt *MockTransport) {
				successData := []byte{0x41, 0x00}
				mt.SetResponse(0x40, successData)
			},
			text: "", // Empty text should still work
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag, _ := setupMIFARETagTest(t, tt.setupMock)

			// Test WriteText
			err := tag.WriteText(tt.text)
			checkMIFARETagError(t, err, tt.expectError, tt.errorContains)
		})
	}
}
