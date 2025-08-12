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
	"fmt"
	"testing"
)

func TestParseTextPayload(t *testing.T) {
	t.Parallel()

	testParseTextPayloadBasic(t)
	testParseTextPayloadLanguage(t)
	testParseTextPayloadEdgeCases(t)
}

func testParseTextPayloadBasic(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		payload []byte
		wantErr bool
	}{
		{
			name:    "empty payload",
			payload: []byte{},
			want:    "",
			wantErr: true,
		},
		{
			name:    "nil payload",
			payload: nil,
			want:    "",
			wantErr: true,
		},
		{
			name:    "zero language length",
			payload: []byte{0x00, 'H', 'e', 'l', 'l', 'o'},
			want:    "Hello",
			wantErr: false,
		},
		{
			name:    "UTF-8 text",
			payload: []byte{0x02, 'e', 'n', 'H', 'e', 'l', 'l', 'o', ' ', 0xF0, 0x9F, 0x98, 0x80}, // Hello 😀
			want:    "Hello 😀",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseTextPayload(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTextPayload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseTextPayload() = %q, want %q", got, tt.want)
			}
		})
	}
}

func testParseTextPayloadLanguage(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		payload []byte
		wantErr bool
	}{
		{
			name:    "language length 2 (en)",
			payload: []byte{0x02, 'e', 'n', 'H', 'e', 'l', 'l', 'o'},
			want:    "Hello",
			wantErr: false,
		},
		{
			name:    "language length 5 (en-US)",
			payload: []byte{0x05, 'e', 'n', '-', 'U', 'S', 'W', 'o', 'r', 'l', 'd'},
			want:    "World",
			wantErr: false,
		},
		{
			name:    "maximum language length (63)",
			payload: append([]byte{0x3F}, append(make([]byte, 63), []byte("Text")...)...),
			want:    "Text",
			wantErr: false,
		},
		{
			name:    "empty text after language code",
			payload: []byte{0x02, 'e', 'n'},
			want:    "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseTextPayload(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTextPayload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseTextPayload() = %q, want %q", got, tt.want)
			}
		})
	}
}

func testParseTextPayloadEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		payload []byte
		wantErr bool
	}{
		{
			name:    "language length exceeds payload",
			payload: []byte{0x05, 'e', 'n'},
			want:    "",
			wantErr: true,
		},
		{
			name:    "language code at boundary",
			payload: []byte{0x03, 'e', 'n', 'g'},
			want:    "",
			wantErr: false,
		},
		{
			name:    "language length with high bit set",
			payload: []byte{0x82, 'e', 'n', 'T', 'e', 's', 't'}, // Status byte with encoding bit
			want:    "Test",
			wantErr: false, // langLen = 0x82 & 0x3F = 2
		},
		{
			name:    "language length mask edge case",
			payload: []byte{0xC0, 'T', 'e', 'x', 't'}, // langLen = 0xC0 & 0x3F = 0
			want:    "Text",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseTextPayload(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTextPayload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseTextPayload() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseURIPayload(t *testing.T) {
	t.Parallel()

	testParseURIPayloadBasic(t)
	testParseURIPayloadPrefixes(t)
	testParseURIPayloadSpecial(t)
}

func testParseURIPayloadBasic(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		payload []byte
		wantErr bool
	}{
		{
			name:    "empty payload",
			payload: []byte{},
			want:    "",
			wantErr: true,
		},
		{
			name:    "nil payload",
			payload: nil,
			want:    "",
			wantErr: true,
		},
		{
			name:    "no prefix (code 0)",
			payload: []byte{0x00, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'},
			want:    "example.com",
			wantErr: false,
		},
		{
			name:    "empty URI remainder",
			payload: []byte{0x04}, // https:// with no remainder
			want:    "https://",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseURIPayload(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseURIPayload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseURIPayload() = %q, want %q", got, tt.want)
			}
		})
	}
}

func testParseURIPayloadPrefixes(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		payload []byte
		wantErr bool
	}{
		{
			name:    "http://www. (code 1)",
			payload: []byte{0x01, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'},
			want:    "http://www.example.com",
			wantErr: false,
		},
		{
			name:    "https://www. (code 2)",
			payload: []byte{0x02, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'},
			want:    "https://www.example.com",
			wantErr: false,
		},
		{
			name:    "http:// (code 3)",
			payload: []byte{0x03, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'},
			want:    "http://example.com",
			wantErr: false,
		},
		{
			name:    "https:// (code 4)",
			payload: []byte{0x04, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'},
			want:    "https://example.com",
			wantErr: false,
		},
		{
			name:    "tel: (code 5)",
			payload: []byte{0x05, '+', '1', '2', '3', '4', '5', '6', '7', '8', '9', '0'},
			want:    "tel:+1234567890",
			wantErr: false,
		},
		{
			name:    "mailto: (code 6)",
			payload: []byte{0x06, 'u', 's', 'e', 'r', '@', 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'},
			want:    "mailto:user@example.com",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseURIPayload(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseURIPayload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseURIPayload() = %q, want %q", got, tt.want)
			}
		})
	}
}

func testParseURIPayloadSpecial(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		payload []byte
		wantErr bool
	}{
		{
			name:    "ftp://ftp. (code 8)",
			payload: []byte{0x08, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'},
			want:    "ftp://ftp.example.com",
			wantErr: false,
		},
		{
			name:    "file:// (code 29)",
			payload: []byte{0x1D, 'p', 'a', 't', 'h', '/', 't', 'o', '/', 'f', 'i', 'l', 'e'},
			want:    "file://path/to/file",
			wantErr: false,
		},
		{
			name:    "urn:nfc: (code 35)",
			payload: []byte{0x23, 't', 'a', 'g'},
			want:    "urn:nfc:tag",
			wantErr: false,
		},
		{
			name:    "invalid prefix code 36",
			payload: []byte{0x24, 'e', 'x', 'a', 'm', 'p', 'l', 'e'},
			want:    "",
			wantErr: true,
		},
		{
			name:    "invalid prefix code 255",
			payload: []byte{0xFF, 'e', 'x', 'a', 'm', 'p', 'l', 'e'},
			want:    "",
			wantErr: true,
		},
		{
			name: "URI with UTF-8 characters",
			// https://example.com/ä
			payload: []byte{0x04, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm', '/', 0xC3, 0xA4},
			want:    "https://example.com/ä",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseURIPayload(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseURIPayload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseURIPayload() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestParseURIPayload_AllPrefixes tests all remaining URI prefix codes
func TestParseURIPayload_AllPrefixes(t *testing.T) {
	t.Parallel()
	// Test remaining URI prefixes 7, 9-28, 30-34
	prefixTests := []struct {
		prefix string
		code   byte
	}{
		{prefix: "ftp://anonymous:anonymous@", code: 7},
		{prefix: "ftps://", code: 9},
		{prefix: "sftp://", code: 10},
		{prefix: "smb://", code: 11},
		{prefix: "nfs://", code: 12},
		{prefix: "ftp://", code: 13},
		{prefix: "dav://", code: 14},
		{prefix: "news:", code: 15},
		{prefix: "telnet://", code: 16},
		{prefix: "imap:", code: 17},
		{prefix: "rtsp://", code: 18},
		{prefix: "urn:", code: 19},
		{prefix: "pop:", code: 20},
		{prefix: "sip:", code: 21},
		{prefix: "sips:", code: 22},
		{prefix: "tftp:", code: 23},
		{prefix: "btspp://", code: 24},
		{prefix: "btl2cap://", code: 25},
		{prefix: "btgoep://", code: 26},
		{prefix: "tcpobex://", code: 27},
		{prefix: "irdaobex://", code: 28},
		{prefix: "urn:epc:id:", code: 30},
		{prefix: "urn:epc:tag:", code: 31},
		{prefix: "urn:epc:pat:", code: 32},
		{prefix: "urn:epc:raw:", code: 33},
		{prefix: "urn:epc:", code: 34},
	}

	for _, tt := range prefixTests {
		t.Run(fmt.Sprintf("prefix_code_%d", tt.code), func(t *testing.T) {
			t.Parallel()
			payload := []byte{tt.code, 't', 'e', 's', 't'}
			got, err := parseURIPayload(payload)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			expected := tt.prefix + "test"
			if got != expected {
				t.Errorf("Expected %q, got %q", expected, got)
			}
		})
	}
}

func TestBuildTextRecord(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
	}{
		{"empty text", ""},
		{"simple text", "Hello"},
		{"text with spaces", "Hello World"},
		{"UTF-8 text", "Hello \u4e16\u754c"},
		{"long text", "This is a longer text message for testing purposes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			record := buildTextRecord(tt.text)
			if record == nil {
				t.Fatal("buildTextRecord() returned nil")
			}

			// Verify it's a text record with correct flags
			if record.TNF() != 0x01 { // Well-known type
				t.Errorf("Expected TNF 0x01, got 0x%02X", record.TNF())
			}
			if record.Type() != "T" {
				t.Errorf("Expected type 'T', got %q", record.Type())
			}

			// Verify MB and ME flags are cleared (will be set by message builder)
			if record.MB() || record.ME() {
				t.Error("MB and ME flags should be cleared")
			}
		})
	}
}

func TestBuildURIRecord(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		uri  string
	}{
		{"http URL", "http://example.com"},
		{"https URL", "https://example.com"},
		{"mailto", "mailto:user@example.com"},
		{"tel", "tel:+1234567890"},
		{"file", "file:///path/to/file"},
		{"custom scheme", "myscheme://data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			record := buildURIRecord(tt.uri)
			if record == nil {
				t.Fatal("buildURIRecord() returned nil")
			}

			// Verify it's a URI record with correct flags
			if record.TNF() != 0x01 { // Well-known type
				t.Errorf("Expected TNF 0x01, got 0x%02X", record.TNF())
			}
			if record.Type() != "U" {
				t.Errorf("Expected type 'U', got %q", record.Type())
			}

			// Verify MB and ME flags are cleared
			if record.MB() || record.ME() {
				t.Error("MB and ME flags should be cleared")
			}
		})
	}
}

func TestBuildTextMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		wantErr bool
	}{
		{"empty text", "", false},
		{"simple text", "Hello", false},
		{"UTF-8 text", "Hello \u4e16\u754c", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := BuildTextMessage(tt.text)
			validateBuildMessageResult(t, buildMessageValidation{
				data:     data,
				err:      err,
				wantErr:  tt.wantErr,
				funcName: "BuildTextMessage",
			})
		})
	}
}

func TestBuildURIMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		uri     string
		wantErr bool
	}{
		{"http URL", "http://example.com", false},
		{"https URL", "https://example.com", false},
		{"mailto", "mailto:user@example.com", false},
		{"empty URI", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := BuildURIMessage(tt.uri)
			validateBuildMessageResult(t, buildMessageValidation{
				data:     data,
				err:      err,
				wantErr:  tt.wantErr,
				funcName: "BuildURIMessage",
			})
		})
	}
}

type buildMessageValidation struct {
	err      error
	funcName string
	data     []byte
	wantErr  bool
}

func validateBuildMessageResult(t *testing.T, validation buildMessageValidation) {
	t.Helper()

	if (validation.err != nil) != validation.wantErr {
		t.Errorf("%s() error = %v, wantErr %v", validation.funcName, validation.err, validation.wantErr)
		return
	}

	if validation.wantErr {
		return
	}

	if len(validation.data) == 0 {
		t.Errorf("%s() returned empty data", validation.funcName)
		return
	}

	if err := ValidateNDEFMessage(validation.data); err != nil {
		t.Errorf("Generated invalid NDEF message: %v", err)
	}
}
