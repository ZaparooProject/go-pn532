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
	var cases []struct {
		setupMock     func(*MockTransport)
		setupAuth     func(*MIFARETag)
		name          string
		errorContains string
		data          []byte
		block         uint8
		expectError   bool
	}

	cases = append(cases, getMIFAREWriteBlockSuccessCases()...)
	cases = append(cases, getMIFAREWriteBlockErrorCases()...)

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
			errorContains: "failed to write block",
		},
	}
}

// Removed - test cases consolidated into getMIFAREWriteBlockTestCases

// Removed - test cases consolidated into getMIFAREWriteBlockTestCases

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
