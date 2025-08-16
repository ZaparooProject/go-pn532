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

	testutil "github.com/ZaparooProject/go-pn532/internal/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to setup device with mock transport
func setupDeviceWithMock(t *testing.T, setupMock func(*MockTransport)) (*Device, *MockTransport) {
	t.Helper()
	mock := NewMockTransport()
	setupMock(mock)
	device, err := New(mock)
	require.NoError(t, err)
	return device, mock
}

// Helper function for standard error checking pattern
func checkTestError(t *testing.T, err error, errorSubstring string, result []byte) {
	t.Helper()
	require.Error(t, err)
	if errorSubstring != "" {
		assert.Contains(t, err.Error(), errorSubstring)
	}
	assert.Nil(t, result)
}

func checkTestSuccess(t *testing.T, err error, result, expectedData []byte) {
	t.Helper()
	require.NoError(t, err)
	assert.Equal(t, expectedData, result)
}

// Helper function for PowerDown test error checking
func checkPowerDownError(t *testing.T, err error, errorSubstring string) {
	t.Helper()
	require.Error(t, err)
	if errorSubstring != "" {
		assert.Contains(t, err.Error(), errorSubstring)
	}
}

func checkPowerDownSuccess(t *testing.T, err error, mock *MockTransport, cmdPowerDown byte) {
	t.Helper()
	require.NoError(t, err)
	assert.Equal(t, 1, mock.GetCallCount(cmdPowerDown))
}

func getDataExchangeTestCases() []struct {
	setupMock      func(*MockTransport)
	name           string
	errorSubstring string
	inputData      []byte
	expectedData   []byte
	expectError    bool
} {
	cases := []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		inputData      []byte
		expectedData   []byte
		expectError    bool
	}{}

	cases = append(cases, getDataExchangeSuccessCases()...)
	cases = append(cases, getDataExchangeErrorCases()...)

	return cases
}

func getDataExchangeSuccessCases() []struct {
	setupMock      func(*MockTransport)
	name           string
	errorSubstring string
	inputData      []byte
	expectedData   []byte
	expectError    bool
} {
	return []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		inputData      []byte
		expectedData   []byte
		expectError    bool
	}{
		{
			name: "Successful_Data_Exchange",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdInDataExchange, []byte{0x41, 0x00, 0xAA, 0xBB})
			},
			inputData:    []byte{0x00, 0x01, 0x02},
			expectedData: []byte{0xAA, 0xBB},
			expectError:  false,
		},
		{
			name: "Empty_Input_Data",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdInDataExchange, []byte{0x41, 0x00})
			},
			inputData:    []byte{},
			expectedData: []byte{},
			expectError:  false,
		},
		{
			name: "Large_Data_Exchange",
			setupMock: func(mock *MockTransport) {
				largeResponse := make([]byte, 200)
				for i := range largeResponse {
					largeResponse[i] = byte(i % 256)
				}
				response := []byte{0x41, 0x00}
				response = append(response, largeResponse...)
				mock.SetResponse(testutil.CmdInDataExchange, response)
			},
			inputData: make([]byte, 100),
			expectedData: func() []byte {
				data := make([]byte, 200)
				for i := range data {
					data[i] = byte(i % 256)
				}
				return data
			}(),
			expectError: false,
		},
	}
}

func getDataExchangeErrorCases() []struct {
	setupMock      func(*MockTransport)
	name           string
	errorSubstring string
	inputData      []byte
	expectedData   []byte
	expectError    bool
} {
	return []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		inputData      []byte
		expectedData   []byte
		expectError    bool
	}{
		{
			name: "Transport_Command_Error",
			setupMock: func(mock *MockTransport) {
				mock.SetError(testutil.CmdInDataExchange, errors.New("transport failure"))
			},
			inputData:      []byte{0x01, 0x02},
			expectError:    true,
			errorSubstring: "failed to send data exchange command",
		},
		{
			name: "PN532_Error_Frame",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdInDataExchange, []byte{0x7F, 0x01})
			},
			inputData:      []byte{0x01, 0x02},
			expectError:    true,
			errorSubstring: "PN532 error: 0x01",
		},
		{
			name: "Invalid_Response_Format",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdInDataExchange, []byte{0x99, 0x00})
			},
			inputData:      []byte{0x01, 0x02},
			expectError:    true,
			errorSubstring: "unexpected data exchange response",
		},
		{
			name: "Data_Exchange_Status_Error",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdInDataExchange, []byte{0x41, 0x01})
			},
			inputData:      []byte{0x01, 0x02},
			expectError:    true,
			errorSubstring: "data exchange error: 01",
		},
		{
			name: "Short_Response",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdInDataExchange, []byte{0x41})
			},
			inputData:      []byte{0x01, 0x02},
			expectError:    true,
			errorSubstring: "unexpected data exchange response",
		},
	}
}

func getRawCommandTestCases() []struct {
	setupMock      func(*MockTransport)
	name           string
	errorSubstring string
	inputData      []byte
	expectedData   []byte
	expectError    bool
} {
	cases := []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		inputData      []byte
		expectedData   []byte
		expectError    bool
	}{}

	cases = append(cases, getRawCommandSuccessCases()...)
	cases = append(cases, getRawCommandErrorCases()...)

	return cases
}

func getRawCommandSuccessCases() []struct {
	setupMock      func(*MockTransport)
	name           string
	errorSubstring string
	inputData      []byte
	expectedData   []byte
	expectError    bool
} {
	const cmdInCommunicateThru = 0x42

	return []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		inputData      []byte
		expectedData   []byte
		expectError    bool
	}{
		{
			name: "Successful_Raw_Command",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdInCommunicateThru, []byte{0x43, 0x00, 0xDE, 0xAD, 0xBE, 0xEF})
			},
			inputData:    []byte{0x30, 0x00},
			expectedData: []byte{0xDE, 0xAD, 0xBE, 0xEF},
			expectError:  false,
		},
		{
			name: "Empty_Raw_Command",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdInCommunicateThru, []byte{0x43, 0x00})
			},
			inputData:    []byte{},
			expectedData: []byte{},
			expectError:  false,
		},
		{
			name: "Complex_Raw_Command",
			setupMock: func(mock *MockTransport) {
				complexResponse := []byte{0x43, 0x00}
				versionData := []byte{0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x11, 0x03}
				complexResponse = append(complexResponse, versionData...)
				mock.SetResponse(cmdInCommunicateThru, complexResponse)
			},
			inputData:    []byte{0x60},
			expectedData: []byte{0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x11, 0x03},
			expectError:  false,
		},
	}
}

func getRawCommandErrorCases() []struct {
	setupMock      func(*MockTransport)
	name           string
	errorSubstring string
	inputData      []byte
	expectedData   []byte
	expectError    bool
} {
	const cmdInCommunicateThru = 0x42

	return []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		inputData      []byte
		expectedData   []byte
		expectError    bool
	}{
		{
			name: "Transport_Command_Error",
			setupMock: func(mock *MockTransport) {
				mock.SetError(cmdInCommunicateThru, errors.New("communicate through failed"))
			},
			inputData:      []byte{0x30, 0x00},
			expectError:    true,
			errorSubstring: "failed to send communicate through command",
		},
		{
			name: "PN532_Error_Frame",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdInCommunicateThru, []byte{0x7F, 0x02})
			},
			inputData:      []byte{0x30, 0x00},
			expectError:    true,
			errorSubstring: "PN532 error: 0x02",
		},
		{
			name: "Invalid_Response_Format",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdInCommunicateThru, []byte{0x99, 0x00})
			},
			inputData:      []byte{0x30, 0x00},
			expectError:    true,
			errorSubstring: "unexpected InCommunicateThru response",
		},
		{
			name: "InCommunicateThru_Status_Error",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdInCommunicateThru, []byte{0x43, 0x01})
			},
			inputData:      []byte{0x30, 0x00},
			expectError:    true,
			errorSubstring: "InCommunicateThru error: 01",
		},
		{
			name: "Short_Response",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdInCommunicateThru, []byte{0x43})
			},
			inputData:      []byte{0x30, 0x00},
			expectError:    true,
			errorSubstring: "unexpected InCommunicateThru response",
		},
	}
}

func getPowerDownTestCases() []struct {
	setupMock      func(*MockTransport)
	name           string
	errorSubstring string
	description    string
	wakeupEnable   byte
	irqEnable      byte
	expectError    bool
} {
	cases := []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		description    string
		wakeupEnable   byte
		irqEnable      byte
		expectError    bool
	}{}

	cases = append(cases, getPowerDownSuccessCases()...)
	cases = append(cases, getPowerDownErrorCases()...)

	return cases
}

func getPowerDownSuccessCases() []struct {
	setupMock      func(*MockTransport)
	name           string
	errorSubstring string
	description    string
	wakeupEnable   byte
	irqEnable      byte
	expectError    bool
} {
	const cmdPowerDown = 0x16

	return []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		description    string
		wakeupEnable   byte
		irqEnable      byte
		expectError    bool
	}{
		{
			name: "Successful_PowerDown_HSU_Wake",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{0x17})
			},
			wakeupEnable: 0x01,
			irqEnable:    0x01,
			expectError:  false,
			description:  "HSU wake-up enabled with IRQ",
		},
		{
			name: "Successful_PowerDown_RF_Wake",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{0x17})
			},
			wakeupEnable: 0x20,
			irqEnable:    0x00,
			expectError:  false,
			description:  "RF wake-up enabled without IRQ",
		},
		{
			name: "Successful_PowerDown_Multiple_Wake",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{0x17})
			},
			wakeupEnable: 0x27,
			irqEnable:    0x01,
			expectError:  false,
			description:  "Multiple wake-up sources enabled",
		},
		{
			name: "Successful_PowerDown_GPIO_Wake",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{0x17})
			},
			wakeupEnable: 0x98,
			irqEnable:    0x01,
			expectError:  false,
			description:  "GPIO wake-up sources enabled",
		},
		{
			name: "PowerDown_No_Wake_Sources",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{0x17})
			},
			wakeupEnable: 0x00,
			irqEnable:    0x00,
			expectError:  false,
			description:  "No wake-up sources (deep sleep)",
		},
	}
}

func getPowerDownErrorCases() []struct {
	setupMock      func(*MockTransport)
	name           string
	errorSubstring string
	description    string
	wakeupEnable   byte
	irqEnable      byte
	expectError    bool
} {
	const cmdPowerDown = 0x16

	return []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		description    string
		wakeupEnable   byte
		irqEnable      byte
		expectError    bool
	}{
		{
			name: "Transport_Command_Error",
			setupMock: func(mock *MockTransport) {
				mock.SetError(cmdPowerDown, errors.New("power down transport error"))
			},
			wakeupEnable:   0x01,
			irqEnable:      0x01,
			expectError:    true,
			errorSubstring: "PowerDown command failed",
			description:    "Transport layer error",
		},
		{
			name: "Invalid_PowerDown_Response",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{0x99})
			},
			wakeupEnable:   0x01,
			irqEnable:      0x01,
			expectError:    true,
			errorSubstring: "unexpected PowerDown response",
			description:    "Invalid response code",
		},
		{
			name: "Empty_PowerDown_Response",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{})
			},
			wakeupEnable:   0x01,
			irqEnable:      0x01,
			expectError:    true,
			errorSubstring: "unexpected PowerDown response",
			description:    "Empty response",
		},
		{
			name: "Long_PowerDown_Response",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{0x17, 0x00, 0x01})
			},
			wakeupEnable:   0x01,
			irqEnable:      0x01,
			expectError:    true,
			errorSubstring: "unexpected PowerDown response",
			description:    "Response too long",
		},
	}
}

func TestDevice_SendDataExchange(t *testing.T) {
	t.Parallel()

	tests := getDataExchangeTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mock := setupDeviceWithMock(t, tt.setupMock)

			// Test data exchange
			result, err := device.SendDataExchange(tt.inputData)

			if tt.expectError {
				checkTestError(t, err, tt.errorSubstring, result)
			} else {
				checkTestSuccess(t, err, result, tt.expectedData)
				assert.Equal(t, 1, mock.GetCallCount(testutil.CmdInDataExchange))
			}
		})
	}
}

func TestDevice_SendRawCommand(t *testing.T) {
	t.Parallel()

	const cmdInCommunicateThru = 0x42
	tests := getRawCommandTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mock := setupDeviceWithMock(t, tt.setupMock)

			// Test raw command
			result, err := device.SendRawCommand(tt.inputData)

			if tt.expectError {
				checkTestError(t, err, tt.errorSubstring, result)
			} else {
				checkTestSuccess(t, err, result, tt.expectedData)
				assert.Equal(t, 1, mock.GetCallCount(cmdInCommunicateThru))
			}
		})
	}
}

func TestDevice_PowerDown(t *testing.T) {
	t.Parallel()

	const cmdPowerDown = 0x16
	tests := getPowerDownTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mock := setupDeviceWithMock(t, tt.setupMock)

			// Test power down
			err := device.PowerDown(tt.wakeupEnable, tt.irqEnable)

			if tt.expectError {
				checkPowerDownError(t, err, tt.errorSubstring)
			} else {
				checkPowerDownSuccess(t, err, mock, cmdPowerDown)
			}
		})
	}
}

func TestDevice_SendDataExchangeContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		inputData      []byte
		expectedData   []byte
		contextTimeout time.Duration
		expectError    bool
	}{
		{
			name: "Successful_With_Context",
			setupMock: func(mock *MockTransport) {
				// Response format: 0x41 (InDataExchange response), 0x00 (success status), data
				mock.SetResponse(testutil.CmdInDataExchange, []byte{0x41, 0x00, 0xFF, 0xEE})
			},
			contextTimeout: time.Second,
			inputData:      []byte{0x30, 0x04}, // READ block 1
			expectedData:   []byte{0xFF, 0xEE},
			expectError:    false,
		},
		{
			name: "Context_With_Nil_Data",
			setupMock: func(mock *MockTransport) {
				// Response format: 0x41 (InDataExchange response), 0x00 (success status), no data
				mock.SetResponse(testutil.CmdInDataExchange, []byte{0x41, 0x00})
			},
			contextTimeout: time.Second,
			inputData:      nil,
			expectedData:   []byte{},
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, _ := setupDeviceWithMock(t, tt.setupMock)

			// Test with context
			ctx, cancel := context.WithTimeout(context.Background(), tt.contextTimeout)
			defer cancel()

			result, err := device.SendDataExchangeContext(ctx, tt.inputData)

			if tt.expectError {
				checkTestError(t, err, tt.errorSubstring, result)
			} else {
				checkTestSuccess(t, err, result, tt.expectedData)
			}
		})
	}
}

func TestDevice_SendRawCommandContext(t *testing.T) {
	t.Parallel()

	// Define the command constant for InCommunicateThru (0x42)
	const cmdInCommunicateThru = 0x42

	tests := []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		inputData      []byte
		expectedData   []byte
		contextTimeout time.Duration
		expectError    bool
	}{
		{
			name: "Successful_With_Context",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdInCommunicateThru, []byte{0x43, 0x00, 0x01, 0x02, 0x03, 0x04})
			},
			contextTimeout: time.Second,
			inputData:      []byte{0x60}, // GET_VERSION
			expectedData:   []byte{0x01, 0x02, 0x03, 0x04},
			expectError:    false,
		},
		{
			name: "Context_With_Nil_Data",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdInCommunicateThru, []byte{0x43, 0x00})
			},
			contextTimeout: time.Second,
			inputData:      nil,
			expectedData:   []byte{},
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, _ := setupDeviceWithMock(t, tt.setupMock)

			// Test with context
			ctx, cancel := context.WithTimeout(context.Background(), tt.contextTimeout)
			defer cancel()

			result, err := device.SendRawCommandContext(ctx, tt.inputData)

			if tt.expectError {
				checkTestError(t, err, tt.errorSubstring, result)
			} else {
				checkTestSuccess(t, err, result, tt.expectedData)
			}
		})
	}
}

func TestDevice_PowerDownContext(t *testing.T) {
	t.Parallel()

	// Define the command constant for PowerDown (0x16)
	const cmdPowerDown = 0x16

	tests := []struct {
		setupMock      func(*MockTransport)
		name           string
		errorSubstring string
		contextTimeout time.Duration
		wakeupEnable   byte
		irqEnable      byte
		expectError    bool
	}{
		{
			name: "Successful_With_Context",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{0x17})
			},
			contextTimeout: time.Second,
			wakeupEnable:   0x21, // HSU + RF wake-up
			irqEnable:      0x01,
			expectError:    false,
		},
		{
			name: "Context_With_All_Wakeup_Sources",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{0x17})
			},
			contextTimeout: time.Second,
			wakeupEnable:   0xFF, // All wake-up sources
			irqEnable:      0x01,
			expectError:    false,
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

			// Test with context
			ctx, cancel := context.WithTimeout(context.Background(), tt.contextTimeout)
			defer cancel()

			err = device.PowerDownContext(ctx, tt.wakeupEnable, tt.irqEnable)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
