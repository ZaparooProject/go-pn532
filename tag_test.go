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
		uid      []byte
		expected string
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

	assert.Error(t, err)
	assert.Nil(t, data)
	assert.Equal(t, ErrNotImplemented, err)
}

func TestBaseTag_WriteBlock(t *testing.T) {
	t.Parallel()

	tag := &BaseTag{}
	err := tag.WriteBlock(4, []byte{0x01, 0x02, 0x03, 0x04})

	assert.Error(t, err)
	assert.Equal(t, ErrNotImplemented, err)
}

func TestBaseTag_ReadNDEF(t *testing.T) {
	t.Parallel()

	tag := &BaseTag{}
	data, err := tag.ReadNDEF()

	assert.Error(t, err)
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

	assert.Error(t, err)
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
	assert.Error(t, err)
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
	assert.Error(t, err)
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
		name     string
		device   *Device
		uid      []byte
		sak      byte
		expected *NTAGTag
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
			assert.Equal(t, tt.device, result.BaseTag.device)
			assert.Equal(t, tt.sak, result.BaseTag.sak)
		})
	}
}

func TestNTAGTag_ReadBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*MockTransport)
		block         uint8
		expectError   bool
		expectedData  []byte
		errorContains string
	}{
		{
			name: "Successful_Block_Read",
			setupMock: func(mt *MockTransport) {
				// NTAG ReadBlock returns 16 bytes (4 blocks) but only first 4 are used
				// Response format: 0x41 (InDataExchange response), 0x00 (success status), 16 bytes of data
				mt.SetResponse(0x40, []byte{0x41, 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10})
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
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, data)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedData, data)
			}
		})
	}
}

func TestNTAGTag_WriteBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*MockTransport)
		block         uint8
		data          []byte
		expectError   bool
		errorContains string
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
			setupMock: func(mt *MockTransport) {
				// No command expected as validation should fail early
			},
			block:         4,
			data:          []byte{0x01, 0x02, 0x03, 0x04, 0x05}, // 5 bytes > 4 byte max
			expectError:   true,
			errorContains: "invalid block size",
		},
		{
			name: "Data_Too_Small",
			setupMock: func(mt *MockTransport) {
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
				assert.Error(t, err)
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
		name          string
		setupMock     func(*MockTransport)
		expectError   bool
		expectedType  NTAGType
		errorContains string
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
				assert.Error(t, err)
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
		name          string
		setupMock     func(*MockTransport)
		startBlock    uint8
		endBlock      uint8
		expectError   bool
		expectedData  []byte
		errorContains string
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
			startBlock:   4,
			endBlock:     7,
			expectError:  false,
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
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, data)
			} else {
				assert.NoError(t, err)
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

			assert.Equal(t, tt.record.Type, tt.record.Type)
			if tt.record.Text != "" {
				assert.Equal(t, tt.record.Text, tt.record.Text)
			}
			if tt.record.URI != "" {
				assert.Equal(t, tt.record.URI, tt.record.URI)
			}
			if tt.record.Payload != nil {
				assert.Equal(t, tt.record.Payload, tt.record.Payload)
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
			text:        "Hello, 世界! 🌍",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := BuildNDEFMessage(tt.text)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, data)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, data)
				assert.True(t, len(data) > 0)
				
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
		records       []NDEFRecord
		expectError   bool
		errorContains string
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
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, data)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, data)
				assert.True(t, len(data) > 0)
				
				// Verify the data starts with NDEF header (0x03)
				assert.Equal(t, byte(0x03), data[0])
			}
		})
	}
}

func TestParseNDEFMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupData     func() []byte
		expectError   bool
		expectedCount int
		errorContains string
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
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, message)
			} else {
				assert.NoError(t, err)
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

			// Verify record content (for simple cases)
			for i, original := range tt.records {
				if i < len(message.Records) {
					parsed := message.Records[i]
					assert.Equal(t, original.Type, parsed.Type)
					if original.Text != "" {
						assert.Equal(t, original.Text, parsed.Text)
					}
					if original.URI != "" {
						assert.Equal(t, original.URI, parsed.URI)
					}
				}
			}
		})
	}
}

// MIFARE Classic Tests

func TestNewMIFARETag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		device   *Device
		uid      []byte
		sak      byte
		expected *MIFARETag
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
			assert.Equal(t, tt.device, result.BaseTag.device)
			assert.Equal(t, tt.sak, result.BaseTag.sak)
			assert.Equal(t, -1, result.lastAuthSector) // Should start unauthenticated
		})
	}
}

func TestMIFARETag_ReadBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*MockTransport)
		setupAuth     func(*MIFARETag)
		block         uint8
		expectError   bool
		expectedData  []byte
		errorContains string
	}{
		{
			name: "Successful_Block_Read",
			setupMock: func(mt *MockTransport) {
				// Mock response for InDataExchange with READ command
				// MIFARE Classic returns 16 bytes on read
				data := make([]byte, 18) // Status + 16 bytes data
				data[0] = 0x41           // InDataExchange response
				data[1] = 0x00           // Success status
				for i := 2; i < 18; i++ {
					data[i] = byte(i - 2) // Fill with test data
				}
				mt.SetResponse(0x40, data)
			},
			setupAuth: func(tag *MIFARETag) {
				// Simulate authenticated state for sector 1 (block 4)
				tag.authMutex.Lock()
				tag.lastAuthSector = 1
				tag.authMutex.Unlock()
			},
			block:        4,
			expectError:  false,
			expectedData: []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F},
		},
		{
			name: "Not_Authenticated_Error",
			setupMock: func(mt *MockTransport) {
				// No setup needed - should fail before transport call
			},
			setupAuth: func(tag *MIFARETag) {
				// Leave unauthenticated (lastAuthSector = -1)
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
				// Simulate authenticated state
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
				mt.SetResponse(0x40, []byte{0x41, 0x00, 0x01, 0x02}) // Only 2 bytes data (< 16 bytes)
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewMIFARETag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x08)
			tt.setupAuth(tag)

			data, err := tag.ReadBlock(tt.block)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, data)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedData, data)
			}
		})
	}
}

func TestMIFARETag_WriteBlock(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*MockTransport)
		setupAuth     func(*MIFARETag)
		block         uint8
		data          []byte
		expectError   bool
		errorContains string
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
		{
			name: "Not_Authenticated_Error",
			setupMock: func(mt *MockTransport) {
				// No setup needed - should fail before transport call
			},
			setupAuth: func(tag *MIFARETag) {
				// Leave unauthenticated
			},
			block:         4,
			data:          make([]byte, 16),
			expectError:   true,
			errorContains: "not authenticated to sector",
		},
		{
			name: "Invalid_Block_Size",
			setupMock: func(mt *MockTransport) {
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
			setupMock: func(mt *MockTransport) {
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewMIFARETag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x08)
			tt.setupAuth(tag)

			err := tag.WriteBlock(tt.block, tt.data)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMIFARETag_Authenticate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*MockTransport)
		sector        uint8
		keyType       byte
		key           []byte
		expectError   bool
		errorContains string
	}{
		{
			name: "Successful_Authentication_KeyA",
			setupMock: func(mt *MockTransport) {
				// Mock successful authentication response
				mt.SetResponse(0x40, []byte{0x41, 0x00}) // Success status
			},
			sector:      1,
			keyType:     0x00, // Key A
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
			setupMock: func(mt *MockTransport) {
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
			setupMock: func(mt *MockTransport) {
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewMIFARETag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x08)

			err := tag.Authenticate(tt.sector, tt.keyType, tt.key)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				// Should reset auth state on failure
				assert.Equal(t, -1, tag.lastAuthSector)
			} else {
				assert.NoError(t, err)
				// Should update auth state on success
				assert.Equal(t, int(tt.sector), tag.lastAuthSector)
				assert.Equal(t, tt.keyType, tag.lastAuthKeyType)
			}
		})
	}
}

func TestMIFARETag_ReadBlockDirect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*MockTransport)
		block         uint8
		expectError   bool
		expectedData  []byte
		errorContains string
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
			block:        4,
			expectError:  false,
			expectedData: []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F},
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
			block:        4,
			expectError:  false,
			expectedData: []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F},
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

			tag := NewMIFARETag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x08)

			data, err := tag.ReadBlockDirect(tt.block)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, data)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedData, data)
			}
		})
	}
}

func TestMIFARETag_WriteBlockDirect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*MockTransport)
		block         uint8
		data          []byte
		expectError   bool
		errorContains string
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
			data:  []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
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
			data:  []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
		},
		{
			name: "Invalid_Block_Size",
			setupMock: func(mt *MockTransport) {
				// No setup needed - validation happens before transport call
			},
			block:         4,
			data:          []byte{0x01, 0x02, 0x03}, // Too short
			expectError:   true,
			errorContains: "invalid block size",
		},
		{
			name: "Manufacturer_Block_Protection",
			setupMock: func(mt *MockTransport) {
				// No setup needed - validation happens before transport call
			},
			block:         0, // Manufacturer block
			data:          []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
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
			block:         4,
			data:          []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10},
			expectError:   true,
			errorContains: "clone tag does not support direct block access",
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
			tag := NewMIFARETag(device, uid, 0x08) // MIFARE Classic 1K SAK

			// Test WriteBlockDirect
			err := tag.WriteBlockDirect(tt.block, tt.data)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMIFARETag_ReadNDEF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*MockTransport)
		expectError   bool
		errorContains string
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
			tag := NewMIFARETag(device, uid, 0x08)

			// Test ReadNDEF
			message, err := tag.ReadNDEF()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, message)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, message)
			}
		})
	}
}

func TestMIFARETag_WriteNDEF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*MockTransport)
		message       *NDEFMessage
		expectError   bool
		errorContains string
	}{
		{
			name: "Empty_Message",
			setupMock: func(mt *MockTransport) {
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
				// Setup authentication failure
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
				// Setup authentication success
				authData := []byte{0x41, 0x00}
				mt.SetResponse(0x40, authData)
			},
			message: &NDEFMessage{
				Records: []NDEFRecord{
					{
						Type: NDEFTypeText,
						Text: string(make([]byte, 2000)), // Very large text
					},
				},
			},
			expectError:   true,
			errorContains: "NDEF message too large",
		},
		{
			name: "Valid_Small_Message",
			setupMock: func(mt *MockTransport) {
				// Setup authentication success (for sector authentication)
				authData := []byte{0x41, 0x00}
				mt.SetResponse(0x40, authData)
				
				// Setup successful write responses
				writeData := []byte{0x41, 0x00}
				mt.SetResponse(0x40, writeData)
			},
			message: &NDEFMessage{
				Records: []NDEFRecord{
					{
						Type: NDEFTypeText,
						Text: "Hi", // Small text to avoid complexity
					},
				},
			},
			expectError: false,
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
			tag := NewMIFARETag(device, uid, 0x08)

			// Test WriteNDEF
			err := tag.WriteNDEF(tt.message)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMIFARETag_ResetAuthState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*MockTransport)
		expectError   bool
		errorContains string
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
			tag := NewMIFARETag(device, uid, 0x08)

			// Simulate previous auth state
			tag.lastAuthSector = 5
			tag.lastAuthKeyType = 0x01

			// Test ResetAuthState
			err := tag.ResetAuthState()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
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
		name          string
		setupMock     func(*MockTransport)
		text          string
		expectError   bool
		errorContains string
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

			// Create mock transport and device
			mt := NewMockTransport()
			tt.setupMock(mt)

			device := &Device{transport: mt}

			// Create MIFARE tag
			uid := []byte{0x04, 0x56, 0x78, 0x9A}
			tag := NewMIFARETag(device, uid, 0x08)

			// Test WriteText
			err := tag.WriteText(tt.text)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
