// communication_test.go - tests for communication layer functions
// This file tests SendDataExchange, SendRawCommand, and PowerDown functions
// using the existing MockTransport infrastructure for comprehensive coverage.

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

func TestDevice_SendDataExchange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupMock      func(*MockTransport)
		inputData      []byte
		expectedData   []byte
		expectError    bool
		errorSubstring string
	}{
		{
			name: "Successful_Data_Exchange",
			setupMock: func(mock *MockTransport) {
				// Response format: 0x41 (InDataExchange response), 0x00 (success status), data
				mock.SetResponse(testutil.CmdInDataExchange, []byte{0x41, 0x00, 0xAA, 0xBB})
			},
			inputData:    []byte{0x00, 0x01, 0x02},
			expectedData: []byte{0xAA, 0xBB},
			expectError:  false,
		},
		{
			name: "Empty_Input_Data",
			setupMock: func(mock *MockTransport) {
				// Response format: 0x41 (InDataExchange response), 0x00 (success status), no data
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
				// Response format: 0x41 (InDataExchange response), 0x00 (success status), data
				response := []byte{0x41, 0x00}
				response = append(response, largeResponse...)
				mock.SetResponse(testutil.CmdInDataExchange, response)
			},
			inputData:    make([]byte, 100), // Large input
			expectedData: func() []byte {
				data := make([]byte, 200)
				for i := range data {
					data[i] = byte(i % 256)
				}
				return data
			}(),
			expectError: false,
		},
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
				// Error frame: TFI = 0x7F, error code = 0x01
				mock.SetResponse(testutil.CmdInDataExchange, []byte{0x7F, 0x01})
			},
			inputData:      []byte{0x01, 0x02},
			expectError:    true,
			errorSubstring: "PN532 error: 0x01",
		},
		{
			name: "Invalid_Response_Format",
			setupMock: func(mock *MockTransport) {
				// Invalid response (wrong command code)
				mock.SetResponse(testutil.CmdInDataExchange, []byte{0x99, 0x00})
			},
			inputData:      []byte{0x01, 0x02},
			expectError:    true,
			errorSubstring: "unexpected data exchange response",
		},
		{
			name: "Data_Exchange_Status_Error",
			setupMock: func(mock *MockTransport) {
				// Valid format but error status
				mock.SetResponse(testutil.CmdInDataExchange, []byte{0x41, 0x01}) // Status = 0x01 (error)
			},
			inputData:      []byte{0x01, 0x02},
			expectError:    true,
			errorSubstring: "data exchange error: 01",
		},
		{
			name: "Short_Response",
			setupMock: func(mock *MockTransport) {
				// Response too short (missing status)
				mock.SetResponse(testutil.CmdInDataExchange, []byte{0x41})
			},
			inputData:      []byte{0x01, 0x02},
			expectError:    true,
			errorSubstring: "unexpected data exchange response",
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

			// Test data exchange
			result, err := device.SendDataExchange(tt.inputData)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedData, result)
				assert.Equal(t, 1, mock.GetCallCount(testutil.CmdInDataExchange))
			}
		})
	}
}

func TestDevice_SendRawCommand(t *testing.T) {
	t.Parallel()

	// Define the command constant for InCommunicateThru (0x42)
	const cmdInCommunicateThru = 0x42

	tests := []struct {
		name           string
		setupMock      func(*MockTransport)
		inputData      []byte
		expectedData   []byte
		expectError    bool
		errorSubstring string
	}{
		{
			name: "Successful_Raw_Command",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdInCommunicateThru, []byte{0x43, 0x00, 0xDE, 0xAD, 0xBE, 0xEF})
			},
			inputData:    []byte{0x30, 0x00}, // READ command for NTAG
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
				// Simulate NTAG GET_VERSION response
				versionData := []byte{0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x11, 0x03}
				complexResponse = append(complexResponse, versionData...)
				mock.SetResponse(cmdInCommunicateThru, complexResponse)
			},
			inputData:    []byte{0x60}, // GET_VERSION command
			expectedData: []byte{0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x11, 0x03},
			expectError:  false,
		},
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
				// Error frame: TFI = 0x7F, error code = 0x02
				mock.SetResponse(cmdInCommunicateThru, []byte{0x7F, 0x02})
			},
			inputData:      []byte{0x30, 0x00},
			expectError:    true,
			errorSubstring: "PN532 error: 0x02",
		},
		{
			name: "Invalid_Response_Format",
			setupMock: func(mock *MockTransport) {
				// Invalid response (wrong command code)
				mock.SetResponse(cmdInCommunicateThru, []byte{0x99, 0x00})
			},
			inputData:      []byte{0x30, 0x00},
			expectError:    true,
			errorSubstring: "unexpected InCommunicateThru response",
		},
		{
			name: "InCommunicateThru_Status_Error",
			setupMock: func(mock *MockTransport) {
				// Valid format but error status
				mock.SetResponse(cmdInCommunicateThru, []byte{0x43, 0x01}) // Status = 0x01 (error)
			},
			inputData:      []byte{0x30, 0x00},
			expectError:    true,
			errorSubstring: "InCommunicateThru error: 01",
		},
		{
			name: "Short_Response",
			setupMock: func(mock *MockTransport) {
				// Response too short
				mock.SetResponse(cmdInCommunicateThru, []byte{0x43})
			},
			inputData:      []byte{0x30, 0x00},
			expectError:    true,
			errorSubstring: "unexpected InCommunicateThru response",
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

			// Test raw command
			result, err := device.SendRawCommand(tt.inputData)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedData, result)
				assert.Equal(t, 1, mock.GetCallCount(cmdInCommunicateThru))
			}
		})
	}
}

func TestDevice_PowerDown(t *testing.T) {
	t.Parallel()

	// Define the command constant for PowerDown (0x16)
	const cmdPowerDown = 0x16

	tests := []struct {
		name           string
		setupMock      func(*MockTransport)
		wakeupEnable   byte
		irqEnable      byte
		expectError    bool
		errorSubstring string
		description    string
	}{
		{
			name: "Successful_PowerDown_HSU_Wake",
			setupMock: func(mock *MockTransport) {
				// PowerDown response: 0x17
				mock.SetResponse(cmdPowerDown, []byte{0x17})
			},
			wakeupEnable: 0x01, // Enable HSU wake-up (bit 0)
			irqEnable:    0x01, // Generate IRQ on wake-up
			expectError:  false,
			description:  "HSU wake-up enabled with IRQ",
		},
		{
			name: "Successful_PowerDown_RF_Wake",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{0x17})
			},
			wakeupEnable: 0x20, // Enable RF wake-up (bit 5)
			irqEnable:    0x00, // No IRQ on wake-up
			expectError:  false,
			description:  "RF wake-up enabled without IRQ",
		},
		{
			name: "Successful_PowerDown_Multiple_Wake",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{0x17})
			},
			wakeupEnable: 0x27, // HSU + SPI + I2C + RF (bits 0,1,2,5)
			irqEnable:    0x01, // Generate IRQ on wake-up
			expectError:  false,
			description:  "Multiple wake-up sources enabled",
		},
		{
			name: "Successful_PowerDown_GPIO_Wake",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{0x17})
			},
			wakeupEnable: 0x98, // GPIO P32, P34, INT1 (bits 3,4,7)
			irqEnable:    0x01, // Generate IRQ on wake-up
			expectError:  false,
			description:  "GPIO wake-up sources enabled",
		},
		{
			name: "PowerDown_No_Wake_Sources",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(cmdPowerDown, []byte{0x17})
			},
			wakeupEnable: 0x00, // No wake-up sources
			irqEnable:    0x00, // No IRQ
			expectError:  false,
			description:  "No wake-up sources (deep sleep)",
		},
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
				// Wrong response code
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
				// Empty response
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
				// Response too long
				mock.SetResponse(cmdPowerDown, []byte{0x17, 0x00, 0x01})
			},
			wakeupEnable:   0x01,
			irqEnable:      0x01,
			expectError:    true,
			errorSubstring: "unexpected PowerDown response",
			description:    "Response too long",
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

			// Test power down
			err = device.PowerDown(tt.wakeupEnable, tt.irqEnable)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, 1, mock.GetCallCount(cmdPowerDown))
			}
		})
	}
}

func TestDevice_SendDataExchangeContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupMock      func(*MockTransport)
		contextTimeout time.Duration
		inputData      []byte
		expectedData   []byte
		expectError    bool
		errorSubstring string
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

			// Setup mock transport
			mock := NewMockTransport()
			tt.setupMock(mock)

			// Create device
			device, err := New(mock)
			require.NoError(t, err)

			// Test with context
			ctx, cancel := context.WithTimeout(context.Background(), tt.contextTimeout)
			defer cancel()

			result, err := device.SendDataExchangeContext(ctx, tt.inputData)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedData, result)
			}
		})
	}
}

func TestDevice_SendRawCommandContext(t *testing.T) {
	t.Parallel()

	// Define the command constant for InCommunicateThru (0x42)
	const cmdInCommunicateThru = 0x42

	tests := []struct {
		name           string
		setupMock      func(*MockTransport)
		contextTimeout time.Duration
		inputData      []byte
		expectedData   []byte
		expectError    bool
		errorSubstring string
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

			// Setup mock transport
			mock := NewMockTransport()
			tt.setupMock(mock)

			// Create device
			device, err := New(mock)
			require.NoError(t, err)

			// Test with context
			ctx, cancel := context.WithTimeout(context.Background(), tt.contextTimeout)
			defer cancel()

			result, err := device.SendRawCommandContext(ctx, tt.inputData)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedData, result)
			}
		})
	}
}

func TestDevice_PowerDownContext(t *testing.T) {
	t.Parallel()

	// Define the command constant for PowerDown (0x16)
	const cmdPowerDown = 0x16

	tests := []struct {
		name           string
		setupMock      func(*MockTransport)
		contextTimeout time.Duration
		wakeupEnable   byte
		irqEnable      byte
		expectError    bool
		errorSubstring string
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
				assert.Error(t, err)
				if tt.errorSubstring != "" {
					assert.Contains(t, err.Error(), tt.errorSubstring)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}