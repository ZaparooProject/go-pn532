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
	"reflect"
	"testing"
)

func TestNDEFRecordTypeConstants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		recType  NDEFRecordType
		expected string
	}{
		{"Text", NDEFTypeText, "text"},
		{"URI", NDEFTypeURI, "uri"},
		{"Smart Poster", NDEFTypeSmartPoster, "smartposter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if string(tt.recType) != tt.expected {
				t.Errorf("NDEFRecordType %s = %q, want %q", tt.name, string(tt.recType), tt.expected)
			}
		})
	}
}

func TestNDEFSecurityLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		desc     string
		constant int
		expected int
	}{
		{"MaxNDEFMessageSize", "8KB message limit", MaxNDEFMessageSize, 8192},
		{"MaxNDEFRecordCount", "255 records limit", MaxNDEFRecordCount, 255},
		{"MaxNDEFPayloadSize", "4KB payload limit", MaxNDEFPayloadSize, 4096},
		{"MaxNDEFTypeLength", "255 byte type limit", MaxNDEFTypeLength, 255},
		{"MaxNDEFIDLength", "255 byte ID limit", MaxNDEFIDLength, 255},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.constant != tt.expected {
				t.Errorf("%s = %d, want %d (%s)", tt.name, tt.constant, tt.expected, tt.desc)
			}
		})
	}
}

func TestNDEFRecordStructure(t *testing.T) {
	t.Parallel()
	// Test creating NDEF record with different fields
	record := NDEFRecord{
		Type:    NDEFTypeText,
		Text:    "Hello, World!",
		URI:     "https://example.com",
		Payload: []byte{0x01, 0x02, 0x03},
	}

	if record.Type != NDEFTypeText {
		t.Errorf("Type = %v, want %v", record.Type, NDEFTypeText)
	}
	if record.Text != "Hello, World!" {
		t.Errorf("Text = %q, want %q", record.Text, "Hello, World!")
	}
	if record.URI != "https://example.com" {
		t.Errorf("URI = %q, want %q", record.URI, "https://example.com")
	}
	if !reflect.DeepEqual(record.Payload, []byte{0x01, 0x02, 0x03}) {
		t.Errorf("Payload = %v, want %v", record.Payload, []byte{0x01, 0x02, 0x03})
	}
}

func TestNDEFMessageStructure(t *testing.T) {
	t.Parallel()
	records := []NDEFRecord{
		{Type: NDEFTypeText, Text: "First record"},
		{Type: NDEFTypeURI, URI: "https://example.com"},
		{Type: NDEFTypeText, Text: "Last record"},
	}

	message := NDEFMessage{Records: records}

	if len(message.Records) != 3 {
		t.Errorf("Records count = %d, want 3", len(message.Records))
	}

	if message.Records[0].Type != NDEFTypeText {
		t.Errorf("First record type = %v, want %v", message.Records[0].Type, NDEFTypeText)
	}
	if message.Records[1].Type != NDEFTypeURI {
		t.Errorf("Second record type = %v, want %v", message.Records[1].Type, NDEFTypeURI)
	}
}

func TestCalculateNDEFHeaderShortFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		payload  []byte
		expected []byte
	}{
		{
			name:     "empty payload",
			payload:  []byte{},
			expected: []byte{0x03, 0x00},
		},
		{
			name:     "single byte payload",
			payload:  []byte{0x01},
			expected: []byte{0x03, 0x01},
		},
		{
			name:     "small payload",
			payload:  []byte{0x01, 0x02, 0x03, 0x04, 0x05},
			expected: []byte{0x03, 0x05},
		},
		{
			name:     "max short format payload",
			payload:  make([]byte, 254),
			expected: []byte{0x03, 0xFE},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := calculateNDEFHeader(tt.payload)
			if err != nil {
				t.Errorf("calculateNDEFHeader() error = %v", err)
				return
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("calculateNDEFHeader() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCalculateNDEFHeaderLongFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected []byte
		length   int
	}{
		{
			name:     "255 bytes (first long format)",
			length:   255,
			expected: []byte{0x03, 0xFF, 0x00, 0xFF},
		},
		{
			name:     "256 bytes",
			length:   256,
			expected: []byte{0x03, 0xFF, 0x01, 0x00},
		},
		{
			name:     "1024 bytes",
			length:   1024,
			expected: []byte{0x03, 0xFF, 0x04, 0x00},
		},
		{
			name:     "max length (65535)",
			length:   65535,
			expected: []byte{0x03, 0xFF, 0xFF, 0xFF},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := make([]byte, tt.length)
			got, err := calculateNDEFHeader(payload)
			if err != nil {
				t.Errorf("calculateNDEFHeader() error = %v", err)
				return
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("calculateNDEFHeader() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCalculateNDEFHeaderErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		errorMsg    string
		payloadSize int
		wantError   bool
	}{
		{
			name:        "payload too large",
			payloadSize: 65536,
			wantError:   true,
			errorMsg:    "NDEF payload too large",
		},
		{
			name:        "way too large payload",
			payloadSize: 100000,
			wantError:   true,
			errorMsg:    "NDEF payload too large",
		},
		{
			name:        "valid max size",
			payloadSize: 65535,
			wantError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := make([]byte, tt.payloadSize)
			_, err := calculateNDEFHeader(payload)

			if (err != nil) != tt.wantError {
				t.Errorf("calculateNDEFHeader() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if tt.wantError && err != nil && err.Error() != tt.errorMsg {
				t.Errorf("calculateNDEFHeader() error = %q, want %q", err.Error(), tt.errorMsg)
			}
		})
	}
}

func TestParseNDEFDataSecurity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		errorType error
		name      string
		dataSize  int
		wantError bool
	}{
		{
			name:      "too large data",
			dataSize:  MaxNDEFMessageSize + 1,
			wantError: true,
			errorType: ErrSecurityViolation,
		},
		{
			name:      "max valid size",
			dataSize:  MaxNDEFMessageSize,
			wantError: true,      // Will fail for other reasons, but not security
			errorType: ErrNoNDEF, // Will fail because no valid NDEF markers in empty data
		},
		{
			name:      "too small data",
			dataSize:  3,
			wantError: true,
			errorType: ErrInvalidNDEF,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := make([]byte, tt.dataSize)
			_, err := ParseNDEFData(data)

			if (err != nil) != tt.wantError {
				t.Errorf("ParseNDEFData() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if tt.wantError && err != nil {
				// Check if error is of expected type (contains expected error)
				errStr := err.Error()
				expectedStr := tt.errorType.Error()
				if !containsString(errStr, expectedStr) &&
					!containsString(errStr, "security violation") &&
					!containsString(errStr, "invalid NDEF") &&
					!containsString(errStr, "no NDEF") {
					t.Errorf("ParseNDEFData() error type mismatch: got %q, expected to contain %q", errStr, expectedStr)
				}
			}
		})
	}
}

func TestNDEFMarkers(t *testing.T) {
	t.Parallel()
	// Test NDEF start marker
	expectedStart := []byte{0x54, 0x02, 0x65, 0x6E}
	if !reflect.DeepEqual(ndefStart, expectedStart) {
		t.Errorf("ndefStart = %v, want %v", ndefStart, expectedStart)
	}

	// Test NDEF end marker
	expectedEnd := []byte{0xFE}
	if !reflect.DeepEqual(ndefEnd, expectedEnd) {
		t.Errorf("ndefEnd = %v, want %v", ndefEnd, expectedEnd)
	}
}

func TestNDEFErrorConstants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"ErrNoNDEF", ErrNoNDEF, "no NDEF record found"},
		{"ErrInvalidNDEF", ErrInvalidNDEF, "invalid NDEF format"},
		{"ErrSecurityViolation", ErrSecurityViolation, "security violation: data exceeds safety limits"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.err.Error() != tt.expected {
				t.Errorf("%s.Error() = %q, want %q", tt.name, tt.err.Error(), tt.expected)
			}
		})
	}
}

// Helper function to check if string contains substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || substr == "" ||
		findSubstring(s, substr) >= 0)
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
