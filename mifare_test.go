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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestMIFARETag creates a MIFARE tag with fast test configuration
func newTestMIFARETag(device *Device, uid []byte, sak byte) *MIFARETag {
	tag := NewMIFARETag(device, uid, sak)
	tag.SetConfig(testMIFAREConfig()) // Apply fast test timing
	return tag
}

// createMockDeviceWithTransport creates a device with a mock transport for testing
// Moved to test_common.go to share across test files

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
			errorContains: "tag read failed",
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
			errorContains: "invalid response length",
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

			data, err := tag.ReadBlock(context.Background(), tt.block)

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

func getMIFAREWriteBlockTestCases() []struct {
	setupMock     func(*MockTransport)
	setupAuth     func(*MIFARETag)
	name          string
	errorContains string
	data          []byte
	block         uint8
	expectError   bool
} {
	successCases := getMIFAREWriteBlockSuccessCases()
	errorCases := getMIFAREWriteBlockErrorCases()
	cases := make([]struct {
		setupMock     func(*MockTransport)
		setupAuth     func(*MIFARETag)
		name          string
		errorContains string
		data          []byte
		block         uint8
		expectError   bool
	}, 0, len(successCases)+len(errorCases))

	cases = append(cases, successCases...)
	cases = append(cases, errorCases...)

	return cases
}

func getMIFAREWriteBlockSuccessCases() []struct {
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
		// Success case
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

func getMIFAREWriteBlockErrorCases() []struct {
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
			errorContains: "tag write failed",
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

			err := tag.Authenticate(context.Background(), tt.sector, tt.keyType, tt.key)

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
			errorContains: "tag authentication failed",
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
			errorContains: "tag authentication failed",
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
				// First call (InDataExchange) fails with PN532 error 01 (timeout),
				// second call (InCommunicateThru) succeeds
				mt.SetResponse(0x40, []byte{0x7F, 0x01})

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
			errorContains: "tag read failed",
		},
		{
			name: "Short_Response",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x40, []byte{0x41, 0x00, 0x01, 0x02}) // Only 2 bytes data
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

			tag := newTestMIFARETag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x08)

			data, err := tag.ReadBlockDirect(context.Background(), tt.block)

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
			mt.SelectTarget() // Tag operations require a target to be selected
			tt.setupMock(mt)

			device := &Device{transport: mt}

			// Create MIFARE tag
			uid := []byte{0x04, 0x56, 0x78, 0x9A}
			tag := newTestMIFARETag(device, uid, 0x08) // MIFARE Classic 1K SAK

			// Test WriteBlockDirect
			err := tag.WriteBlockDirect(context.Background(), tt.block, tt.data)

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
				// Simulate timeout errors for all InDataExchange calls to trigger fallback
				mt.SetResponse(0x40, []byte{0x7F, 0x01})

				// Setup successful response for SendRawCommand (both read and write will use this)
				// For read: return 16 bytes of data
				readData := make([]byte, 18)
				readData[0] = 0x43 // InCommunicateThru response
				readData[1] = 0x00 // Success status
				for i := 2; i < 18; i++ {
					readData[i] = byte(i - 2)
				}
				mt.SetResponse(0x42, readData)

				// For write: when SendRawCommand is called for write, return success
				// The mock will use the same response for both read and write SendRawCommand calls
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
		expectEmpty   bool // When true, verify message has empty Records
	}{
		{
			name: "Authentication_Failure",
			setupMock: func(mt *MockTransport) {
				// Setup authentication failure for sector 1
				mt.SetError(0x40, errors.New("authentication failed"))
			},
			expectError: false, // Auth failure = not NDEF formatted = empty NDEF, not error
			expectEmpty: true,  // Regression: auth failure should return empty NDEF, not nil
		},
		{
			name: "Empty_NDEF_Data",
			setupMock: func(mt *MockTransport) {
				// Setup authentication success
				authData := []byte{0x41, 0x00}
				mt.SetResponse(0x40, authData)

				// Setup empty response for reads - no NDEF TLV present
				emptyResponse := make([]byte, 18)
				emptyResponse[0] = 0x41
				emptyResponse[1] = 0x00
				// All data bytes remain 0x00
				mt.SetResponse(0x40, emptyResponse)
			},
			expectError:   true,
			errorContains: "no NDEF", // No NDEF TLV found in empty data
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
			message, err := tag.ReadNDEF(context.Background())

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, message)
			} else {
				require.NoError(t, err)
				require.NotNil(t, message, "NDEF message should not be nil")
				if tt.expectEmpty {
					assert.Empty(t, message.Records,
						"Auth failure should return empty NDEF message, not records")
				}
			}
		})
	}
}

// Helper function for MIFARE tag error testing
// Helper function for MIFARE tag setup and error checking
func setupMIFARETagTest(t *testing.T, setupMock func(*MockTransport)) (*MIFARETag, *MockTransport) {
	t.Helper()
	mt := NewMockTransport()
	// Select target by default - tag operations require a target to be selected
	mt.SelectTarget()
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
				// NDEF "Hi" encodes to: 03 09 D1 01 05 54 02 65 6E 48 69 FE (12 bytes, padded to 16)
				ndefBlock := []byte{
					0x03, 0x09, 0xD1, 0x01, 0x05, 0x54, 0x02, 0x65,
					0x6E, 0x48, 0x69, 0xFE, 0x00, 0x00, 0x00, 0x00,
				}
				// Queue auth responses (success)
				writeSuccess := []byte{0x41, 0x00}
				mt.QueueResponses(0x40,
					writeSuccess, // Auth
					writeSuccess, // Write block
				)
				// Set fallback for verification reads - returns the NDEF data
				readResponse := append([]byte{0x41, 0x00}, ndefBlock...)
				mt.SetResponse(0x40, readResponse)
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
			err := tag.WriteNDEF(context.Background(), tt.message)
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
			setupMock: func(_ *MockTransport) {
				// Default responses from mock are sufficient
			},
		},
		{
			name: "Reset_Communication_Error",
			setupMock: func(mt *MockTransport) {
				// InDeselect succeeds but InSelect fails with communication error
				mt.SetError(0x54, errors.New("communication error"))
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
			// Select target by default - tag operations require a target to be selected
			mt.SelectTarget()
			tt.setupMock(mt)

			device := &Device{transport: mt}

			// Create MIFARE tag and set some auth state
			uid := []byte{0x04, 0x56, 0x78, 0x9A}
			tag := newTestMIFARETag(device, uid, 0x08)

			// Simulate previous auth state
			tag.lastAuthSector = 5
			tag.lastAuthKeyType = 0x01

			// Test ResetAuthState
			err := tag.ResetAuthState(context.Background())

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
				// NDEF "Hello World" is 21 bytes, spans 2 blocks
				// Block 4: 03 12 D1 01 0E 54 02 65 6E 48 65 6C 6C 6F 20 57
				// Block 5: 6F 72 6C 64 FE + padding
				ndefBlock4 := []byte{
					0x03, 0x12, 0xD1, 0x01, 0x0E, 0x54, 0x02, 0x65,
					0x6E, 0x48, 0x65, 0x6C, 0x6C, 0x6F, 0x20, 0x57,
				}
				ndefBlock5 := []byte{
					0x6F, 0x72, 0x6C, 0x64, 0xFE, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				}
				writeSuccess := []byte{0x41, 0x00}

				// Queue writeSuccess responses for:
				// - 1 auth (authenticateWithKeyFallback with NDEF key succeeds)
				// - 2 writes (blocks 4, 5)
				// - 57 clearRemainingBlocks ops: block 6 + sectors 2-15 (1 + 14*4)
				// - 1 verification auth
				// Total: 61 writeSuccess, then 2 reads
				for range 61 {
					mt.QueueResponse(0x40, writeSuccess)
				}

				// Queue read responses for verification (block 4, then block 5)
				readResponse4 := append([]byte{0x41, 0x00}, ndefBlock4...)
				readResponse5 := append([]byte{0x41, 0x00}, ndefBlock5...)
				mt.QueueResponses(0x40, readResponse4, readResponse5)
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
				// Empty NDEF "" encodes to: 03 07 D1 01 03 54 02 65 6E FE (10 bytes, padded to 16)
				ndefBlock := []byte{
					0x03, 0x07, 0xD1, 0x01, 0x03, 0x54, 0x02, 0x65,
					0x6E, 0xFE, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				}
				writeSuccess := []byte{0x41, 0x00}

				// Queue 61 writeSuccess for auth + MAD writes + NDEF write + clearRemainingBlocks
				for range 61 {
					mt.QueueResponse(0x40, writeSuccess)
				}

				// Queue read response for verification
				readResponse := append([]byte{0x41, 0x00}, ndefBlock...)
				mt.QueueResponse(0x40, readResponse)
			},
			text: "", // Empty text should still work
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag, _ := setupMIFARETagTest(t, tt.setupMock)

			// Test WriteText
			err := tag.WriteText(context.Background(), tt.text)
			checkMIFARETagError(t, err, tt.expectError, tt.errorContains)
		})
	}
}

// Tests for mifare_extra.go functionality

func TestMIFARETag_IsNDEFFormatted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock func(*MockTransport)
		name      string
		expected  bool
	}{
		{
			name: "Not_NDEF_Formatted",
			setupMock: func(mt *MockTransport) {
				// MAD key authentication fails
				mt.SetError(0x40, errors.New("authentication failed"))
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag, _ := setupMIFARETagTest(t, tt.setupMock)

			result := tag.IsNDEFFormatted(context.Background())
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMIFARETag_FormatForNDEF(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		expectError   bool
	}{
		{
			name: "Sector0_Auth_Failure",
			setupMock: func(mt *MockTransport) {
				// First auth attempt for sector 0 fails
				mt.SetError(0x40, errors.New("authentication failed"))
			},
			expectError:   true,
			errorContains: "failed to authenticate sector 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag, _ := setupMIFARETagTest(t, tt.setupMock)
			tag.SetConfig(testMIFAREConfig()) // Use fast test timing

			err := tag.FormatForNDEF(context.Background())
			checkMIFARETagError(t, err, tt.expectError, tt.errorContains)
		})
	}
}

// --- Issue #49 regression tests: Mifare Classic partial-format ---

func TestClassifyPN532Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		err  error
		want error
		name string
	}{
		{
			name: "timeout_0x01",
			err:  &PN532Error{Command: "InDataExchange", ErrorCode: 0x01},
			want: ErrTagTimeout,
		},
		{
			name: "dataformat_mismatch_0x13",
			err:  &PN532Error{Command: "InDataExchange", ErrorCode: 0x13},
			want: ErrTagDataMismatch,
		},
		{
			name: "auth_failed_0x14",
			err:  &PN532Error{Command: "InDataExchange", ErrorCode: 0x14},
			want: ErrTagAuthFailed,
		},
		{
			name: "wrong_context_0x27",
			err:  &PN532Error{Command: "InDataExchange", ErrorCode: 0x27},
			want: ErrTagInvalidState,
		},
		{
			name: "unclassified_pn532_code",
			err:  &PN532Error{Command: "InDataExchange", ErrorCode: 0x99},
			want: nil,
		},
		{
			name: "wrapped_pn532_error_still_classifies",
			err: fmt.Errorf("block 7 write failed: %w",
				&PN532Error{Command: "InDataExchange", ErrorCode: 0x13}),
			want: ErrTagDataMismatch,
		},
		{
			name: "non_pn532_error",
			err:  errors.New("something else"),
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := classifyPN532Error(tt.err)
			assert.Equal(t, tt.want, got,
				"classifyPN532Error should return the matching sentinel")
		})
	}
}

func TestMIFARETag_ValidatePartialFormatCapacity(t *testing.T) {
	t.Parallel()

	tag, _ := setupMIFARETagTest(t, func(_ *MockTransport) {})

	tests := []struct {
		name          string
		dataLen       int
		usableSectors uint8
		wantErr       bool
	}{
		{
			name:          "no_cap_means_always_ok",
			dataLen:       4096,
			usableSectors: 0,
			wantErr:       false,
		},
		{
			name:          "one_sector_fits_small_ndef",
			dataLen:       40, // fits in 3 data blocks (48 bytes)
			usableSectors: 1,
			wantErr:       false,
		},
		{
			name:          "one_sector_exactly_48_bytes",
			dataLen:       48,
			usableSectors: 1,
			wantErr:       false,
		},
		{
			name:          "one_sector_overflow",
			dataLen:       49,
			usableSectors: 1,
			wantErr:       true,
		},
		{
			name:          "two_sectors_fits_96_bytes",
			dataLen:       96,
			usableSectors: 2,
			wantErr:       false,
		},
		{
			name:          "two_sectors_overflow",
			dataLen:       97,
			usableSectors: 2,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := make([]byte, tt.dataLen)
			err := tag.validatePartialFormatCapacity(data, tt.usableSectors)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrTagIncompatible,
					"over-capacity payload must wrap ErrTagIncompatible")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestMIFARETag_MaxWritableBlocks verifies the block-cap helper used by both
// writeNDEFData and clearRemainingBlocks. The two call sites must agree on the
// exclusive upper bound so partial-format cleanup never walks past the sectors
// that formatForNDEFWithKey actually rekeyed.
func TestMIFARETag_MaxWritableBlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		usableSectors uint8
		is4K          bool
		want          uint8
	}{
		{
			name:          "1K_no_cap",
			is4K:          false,
			usableSectors: 0,
			want:          64,
		},
		{
			name:          "1K_cap_1_sector",
			is4K:          false,
			usableSectors: 1,
			want:          8, // sector 1 occupies blocks 4..7
		},
		{
			name:          "1K_cap_2_sectors",
			is4K:          false,
			usableSectors: 2,
			want:          12, // sector 2 occupies blocks 8..11
		},
		{
			name:          "1K_cap_larger_than_tag_clamps_to_64",
			is4K:          false,
			usableSectors: 20,
			want:          64,
		},
		{
			name:          "4K_no_cap",
			is4K:          true,
			usableSectors: 0,
			want:          255,
		},
		{
			name:          "4K_cap_1_sector",
			is4K:          true,
			usableSectors: 1,
			want:          8,
		},
		{
			name:          "4K_cap_31_sectors",
			is4K:          true,
			usableSectors: 31,
			want:          128, // last of the small-sector region
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			device := createMockDevice(t)
			sak := byte(0x08)
			if tt.is4K {
				sak = 0x18
			}
			tag := newTestMIFARETag(device, []byte{0x04, 0x12, 0x34, 0x56}, sak)
			got := tag.maxWritableBlocks(tt.usableSectors)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestMIFARETag_FormatForNDEFWithKey_NoAccessibleSector1 verifies that when
// sector 1 cannot be authenticated with either the blank key or the NDEF key
// (because the mock rejects every InDataExchange command), formatForNDEFWithKey
// returns (0, ErrTagIncompatible wrapped) rather than a silent success or an
// opaque auth error.
func TestMIFARETag_FormatForNDEFWithKey_NoAccessibleSector1(t *testing.T) {
	t.Parallel()

	tag, mt := setupMIFARETagTest(t, func(_ *MockTransport) {})
	tag.SetConfig(testMIFAREConfig())
	// Reject every InDataExchange command so blank-key and NDEF-key auth both
	// fail on sector 1. classifyFormatError should fall through to "unknown
	// format error" because the inner error is not a *PN532Error.
	mt.SetError(0x40, errors.New("auth refused"))

	blankKey := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	contiguous, err := tag.formatForNDEFWithKey(context.Background(), blankKey)

	require.Error(t, err)
	assert.Equal(t, uint8(0), contiguous,
		"no sectors should have been counted as formatted")
	assert.ErrorIs(t, err, ErrTagIncompatible,
		"sector 1 failure must surface as ErrTagIncompatible")
}

// TestMIFARETag_FormatForNDEFWithKey_Pn532ErrorChainsSentinel asserts that
// when sector 1 fails with a *PN532Error (0x13 dataformat mismatch, the
// failure mode reported in issue #49), the returned error chain contains
// both ErrTagIncompatible (the operation-level sentinel) and ErrTagDataMismatch
// (the classification sentinel) so callers can branch on cause with
// errors.Is, and the raw *PN532Error is still reachable via errors.As.
func TestMIFARETag_FormatForNDEFWithKey_Pn532ErrorChainsSentinel(t *testing.T) {
	t.Parallel()

	tag, mt := setupMIFARETagTest(t, func(_ *MockTransport) {})
	tag.SetConfig(testMIFAREConfig())
	mt.SetError(0x40, &PN532Error{Command: "InDataExchange", ErrorCode: 0x13})

	blankKey := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	_, err := tag.formatForNDEFWithKey(context.Background(), blankKey)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrTagIncompatible,
		"error chain must include the operation-level sentinel")
	require.ErrorIs(t, err, ErrTagDataMismatch,
		"error chain must include the classification sentinel")

	var pn532Err *PN532Error
	require.ErrorAs(t, err, &pn532Err,
		"underlying *PN532Error must remain unwrappable through the error chain")
	assert.Equal(t, uint8(0x13), pn532Err.ErrorCode)
}

func TestMIFARETag_WriteNDEFAlternative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		message       *NDEFMessage
		name          string
		errorContains string
		expectError   bool
	}{
		{
			name:      "Empty_Records",
			setupMock: func(_ *MockTransport) {},
			message: &NDEFMessage{
				Records: []NDEFRecord{},
			},
			expectError:   true,
			errorContains: "no NDEF records to write",
		},
		{
			name: "Successful_Write",
			setupMock: func(mt *MockTransport) {
				writeSuccess := []byte{0x41, 0x00}
				// Auth for sector 1 + writes for NDEF data
				for range 10 {
					mt.QueueResponse(0x40, writeSuccess)
				}
			},
			message: &NDEFMessage{
				Records: []NDEFRecord{
					{Type: NDEFTypeText, Text: "Test"},
				},
			},
			expectError: false,
		},
		{
			name: "Data_Exceeds_Capacity",
			setupMock: func(mt *MockTransport) {
				writeSuccess := []byte{0x41, 0x00}
				// Keep returning success so we hit the capacity limit
				for range 100 {
					mt.QueueResponse(0x40, writeSuccess)
				}
			},
			message: &NDEFMessage{
				Records: []NDEFRecord{
					{Type: NDEFTypeText, Text: string(make([]byte, 2000))}, // Large message
				},
			},
			expectError:   true,
			errorContains: "exceeds tag capacity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag, _ := setupMIFARETagTest(t, tt.setupMock)
			tag.SetConfig(testMIFAREConfig())

			err := tag.WriteNDEFAlternative(context.Background(), tt.message)
			checkMIFARETagError(t, err, tt.expectError, tt.errorContains)
		})
	}
}

func TestMIFARETag_WriteBlockAutoAlternative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		setupTag      func(*MIFARETag)
		name          string
		errorContains string
		data          []byte
		block         uint8
		expectError   bool
	}{
		{
			name: "Auth_Required_KeyB_Succeeds",
			setupMock: func(mt *MockTransport) {
				// Key B auth succeeds, then write succeeds
				mt.QueueResponses(0x40, []byte{0x41, 0x00}, []byte{0x41, 0x00})
			},
			setupTag:    func(_ *MIFARETag) {},
			block:       4,
			data:        make([]byte, 16),
			expectError: false,
		},
		{
			name: "Auth_Required_KeyB_Fails_KeyA_Succeeds",
			setupMock: func(mt *MockTransport) {
				// Key B fails, Key A succeeds, write succeeds
				mt.QueueResponses(0x40,
					[]byte{0x41, 0x14}, // Key B auth fails
					[]byte{0x41, 0x00}, // Key A auth succeeds
					[]byte{0x41, 0x00}, // Write succeeds
				)
			},
			setupTag:    func(_ *MIFARETag) {},
			block:       4,
			data:        make([]byte, 16),
			expectError: false,
		},
		{
			name: "Both_Keys_Fail",
			setupMock: func(mt *MockTransport) {
				// Both keys fail
				mt.SetError(0x40, errors.New("authentication failed"))
			},
			setupTag:      func(_ *MIFARETag) {},
			block:         4,
			data:          make([]byte, 16),
			expectError:   true,
			errorContains: "failed to authenticate to sector",
		},
		{
			name: "Auth_Fails_For_Alt",
			setupMock: func(mt *MockTransport) {
				// Auth fails
				mt.SetError(0x40, errors.New("auth failed"))
			},
			setupTag:      func(_ *MIFARETag) {},
			block:         4,
			data:          make([]byte, 16),
			expectError:   true,
			errorContains: "failed to authenticate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag, _ := setupMIFARETagTest(t, tt.setupMock)
			tag.SetConfig(testMIFAREConfig())
			tt.setupTag(tag)

			err := tag.WriteBlockAutoAlternative(context.Background(), tt.block, tt.data)
			checkMIFARETagError(t, err, tt.expectError, tt.errorContains)
		})
	}
}

func TestMIFARETag_authenticateWithKeyFallbackAlt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock             func(*MockTransport)
		name                  string
		expectedNDEFFormatted bool
	}{
		{
			name: "NDEF_Formatted",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x40, []byte{0x41, 0x00})
			},
			expectedNDEFFormatted: true,
		},
		{
			name: "Not_NDEF_Formatted",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("auth failed"))
			},
			expectedNDEFFormatted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag, _ := setupMIFARETagTest(t, tt.setupMock)

			result, err := tag.authenticateWithKeyFallbackAlt(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.expectedNDEFFormatted, result.isNDEFFormatted)
		})
	}
}

func TestMIFARETag_authenticateWithNDEFKeyAlt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		keyType       byte
		sector        uint8
		expectError   bool
	}{
		{
			name: "KeyA_Success",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x40, []byte{0x41, 0x00})
			},
			sector:      1,
			keyType:     MIFAREKeyA,
			expectError: false,
		},
		{
			name: "KeyB_Success",
			setupMock: func(mt *MockTransport) {
				mt.SetResponse(0x40, []byte{0x41, 0x00})
			},
			sector:      1,
			keyType:     MIFAREKeyB,
			expectError: false,
		},
		{
			name: "KeyA_Failure",
			setupMock: func(mt *MockTransport) {
				mt.SetError(0x40, errors.New("auth failed"))
			},
			sector:        1,
			keyType:       MIFAREKeyA,
			expectError:   true,
			errorContains: "auth failed",
		},
		{
			name: "Invalid_KeyType_NoOp",
			setupMock: func(_ *MockTransport) {
				// No setup needed - invalid key type returns nil
			},
			sector:      1,
			keyType:     0x02, // Invalid key type
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag, _ := setupMIFARETagTest(t, tt.setupMock)
			tag.SetConfig(testMIFAREConfig())

			err := tag.authenticateWithNDEFKeyAlt(context.Background(), tt.sector, tt.keyType)
			checkMIFARETagError(t, err, tt.expectError, tt.errorContains)
		})
	}
}

// Regression Tests - Key Fallback with Re-Select
//
// These tests verify the fix for MIFARE key fallback.
// When the first key fails, the code should:
// 1. Call InDeselect + InSelect to re-select the tag (failed auth puts tag in HALT)
// 2. Try the alternative key
// 3. Proceed with the read/write operation

func TestMIFARETag_ReadBlockAuto_KeyFallbackSucceeds(t *testing.T) {
	t.Parallel()

	// Test that ReadBlockAuto successfully falls back from Key A to Key B
	// This implicitly tests the re-select fix - without it, Key B would also fail

	tag, mockTransport := setupMIFARETagTest(t, func(mt *MockTransport) {
		// Auth with Key A fails, auth with Key B succeeds, read succeeds
		mt.QueueResponses(0x40,
			[]byte{0x41, 0x14}, // Key A auth fails (status 0x14)
			[]byte{0x41, 0x00}, // Key B auth succeeds
			append([]byte{0x41, 0x00}, make([]byte, 16)...), // Read returns 16 bytes
		)
		// Default mock responses for InDeselect (0x44) and InSelect (0x54) are sufficient
	})
	tag.SetConfig(testMIFAREConfig())

	data, err := tag.ReadBlockAuto(context.Background(), 4)

	require.NoError(t, err, "ReadBlockAuto should succeed with Key B fallback")
	assert.Len(t, data, 16)
	// The fact that this succeeds proves the re-select is working
	// (without re-select, Key B auth would fail due to HALT state)
	assert.GreaterOrEqual(t, mockTransport.GetCallCount(0x44), 1,
		"InDeselect should be called for re-select")
	assert.GreaterOrEqual(t, mockTransport.GetCallCount(0x54), 1,
		"InSelect should be called for re-select")
}

func TestMIFARETag_WriteBlockAuto_KeyFallbackSucceeds(t *testing.T) {
	t.Parallel()

	// Test that WriteBlockAuto successfully falls back from Key B to Key A

	tag, mockTransport := setupMIFARETagTest(t, func(mt *MockTransport) {
		// Auth with Key B fails, auth with Key A succeeds, write succeeds
		mt.QueueResponses(0x40,
			[]byte{0x41, 0x14}, // Key B auth fails (status 0x14)
			[]byte{0x41, 0x00}, // Key A auth succeeds
			[]byte{0x41, 0x00}, // Write succeeds
		)
		// Default mock responses for InDeselect (0x44) and InSelect (0x54) are sufficient
	})
	tag.SetConfig(testMIFAREConfig())

	err := tag.WriteBlockAuto(context.Background(), 4, make([]byte, 16))

	require.NoError(t, err, "WriteBlockAuto should succeed with Key A fallback")
	assert.GreaterOrEqual(t, mockTransport.GetCallCount(0x44), 1,
		"InDeselect should be called for re-select")
	assert.GreaterOrEqual(t, mockTransport.GetCallCount(0x54), 1,
		"InSelect should be called for re-select")
}

// Regression test: TryAuthenticate sets authenticated field

func TestMIFARETag_TryAuthenticate_SetsAuthenticatedField(t *testing.T) {
	t.Parallel()

	t.Run("Success_SetsAuthenticated", func(t *testing.T) {
		t.Parallel()

		tag, _ := setupMIFARETagTest(t, func(mt *MockTransport) {
			// Authentication succeeds
			mt.SetResponse(0x40, []byte{0x41, 0x00})
		})
		tag.SetConfig(testMIFAREConfig())

		// Initially not authenticated
		assert.False(t, tag.IsAuthenticated(), "tag should not be authenticated before TryAuthenticate")

		err := tag.TryAuthenticate(context.Background())

		require.NoError(t, err)
		assert.True(t, tag.IsAuthenticated(),
			"tag.authenticated should be true after successful TryAuthenticate")
	})

	t.Run("Failure_LeavesUnauthenticated", func(t *testing.T) {
		t.Parallel()

		tag, _ := setupMIFARETagTest(t, func(mt *MockTransport) {
			// All authentication attempts fail
			mt.SetError(0x40, errors.New("authentication failed"))
		})
		tag.SetConfig(testMIFAREConfig())

		// Initially not authenticated
		assert.False(t, tag.IsAuthenticated())

		err := tag.TryAuthenticate(context.Background())

		require.Error(t, err)
		assert.False(t, tag.IsAuthenticated(),
			"tag.authenticated should remain false after failed TryAuthenticate")
	})
}

// Regression test: DefaultMIFAREConfig has correct defaults

func TestMIFAREConfig_DefaultMaxAttempts(t *testing.T) {
	t.Parallel()

	config := DefaultMIFAREConfig()

	require.NotNil(t, config, "DefaultMIFAREConfig should not return nil")
	require.NotNil(t, config.RetryConfig, "RetryConfig should not be nil")

	// Verify MaxAttempts is 3 (default for production)
	assert.Equal(t, 3, config.RetryConfig.MaxAttempts,
		"DefaultMIFAREConfig.RetryConfig.MaxAttempts should be 3")

	// Also verify other retry config defaults are sensible
	assert.Greater(t, config.RetryConfig.InitialBackoff, time.Duration(0),
		"InitialBackoff should be positive")
	assert.Greater(t, config.RetryConfig.MaxBackoff, config.RetryConfig.InitialBackoff,
		"MaxBackoff should be greater than InitialBackoff")
	assert.Greater(t, config.RetryConfig.BackoffMultiplier, float64(1),
		"BackoffMultiplier should be greater than 1")
	assert.Greater(t, config.HardwareDelay, time.Duration(0),
		"HardwareDelay should be positive")
}
