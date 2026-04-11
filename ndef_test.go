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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

//nolint:funlen // Table-driven test with multiple test cases
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
				data, err := BuildNDEFMessageEx([]NDEFRecord{
					{Type: NDEFTypeText, Text: "Hello, World!"},
				})
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
			errorContains: "no NDEF", // No TLV data at all
		},
		{
			name: "Invalid_Header",
			setupData: func() []byte {
				return []byte{0x01, 0x02, 0x03} // No NDEF TLV marker
			},
			expectError:   true,
			errorContains: "no NDEF", // No NDEF TLV found
		},
		{
			name: "Truncated_Data",
			setupData: func() []byte {
				return []byte{0x03, 0x05} // NDEF TLV but truncated
			},
			expectError:   true,
			errorContains: "no NDEF", // TLV extraction fails
		},
		{
			name: "Empty_TLV_Payload",
			setupData: func() []byte {
				// NDEF TLV with 0-length payload: 03 00 FE
				// 0x03 = NDEF TLV type
				// 0x00 = length (zero)
				// 0xFE = Terminator TLV
				return []byte{0x03, 0x00, 0xFE}
			},
			expectError:   false,
			expectedCount: 0, // Empty NDEF = no records
		},
		{
			// Regression for issue #51: tags written by other NFC tools or with
			// leftover data from prior writes commonly have padding past ME.
			// The parser must stop at ME and return the single valid record.
			name: "Trailing_Padding_After_ME",
			setupData: func() []byte {
				return []byte{
					0x03, 0x07,
					0xD1, 0x01, 0x01, 0x54, 0x02,
					0x00, 0x00,
					0xFE,
				}
			},
			expectError:   false,
			expectedCount: 1,
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
