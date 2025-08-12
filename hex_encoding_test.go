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
	"encoding/hex"
	"strings"
	"testing"
)

func TestHexEncodingDecoding(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		expected string
		input    []byte
	}{
		{
			name:     "empty bytes",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "single byte",
			input:    []byte{0xFF},
			expected: "ff",
		},
		{
			name:     "multiple bytes",
			input:    []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE},
			expected: "04123456789abcde",
		},
		{
			name:     "all zeros",
			input:    []byte{0x00, 0x00, 0x00, 0x00},
			expected: "00000000",
		},
		{
			name:     "all 0xFF",
			input:    []byte{0xFF, 0xFF, 0xFF, 0xFF},
			expected: "ffffffff",
		},
		{
			name:     "mixed values",
			input:    []byte{0x00, 0x0F, 0xF0, 0xFF},
			expected: "000ff0ff",
		},
		{
			name:     "NTAG UID example",
			input:    []byte{0x04, 0x6A, 0x18, 0x42, 0x2E, 0x2D, 0x80},
			expected: "046a18422e2d80",
		},
		{
			name:     "MIFARE UID example",
			input:    []byte{0x12, 0x34, 0x56, 0x78},
			expected: "12345678",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Test encoding
			encoded := hex.EncodeToString(tt.input)
			if encoded != tt.expected {
				t.Errorf("hex.EncodeToString() = %q, want %q", encoded, tt.expected)
			}

			// Test decoding back
			decoded, err := hex.DecodeString(encoded)
			if err != nil {
				t.Errorf("hex.DecodeString() failed: %v", err)
			}

			if !equalByteSlices(decoded, tt.input) {
				t.Errorf("Round trip failed: decoded = %v, want %v", decoded, tt.input)
			}
		})
	}
}

func TestHexDecodingErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     string
		errorType string
		wantError bool
	}{
		{
			name:      "invalid hex character",
			input:     "04123456g",
			wantError: true,
			errorType: "invalid character",
		},
		{
			name:      "odd length string",
			input:     "0412345",
			wantError: true,
			errorType: "odd length",
		},
		{
			name:      "valid empty string",
			input:     "",
			wantError: false,
		},
		{
			name:      "valid hex string",
			input:     "04123456",
			wantError: false,
		},
		{
			name:      "space in hex string",
			input:     "04 12 34 56",
			wantError: true,
			errorType: "invalid character",
		},
		{
			name:      "uppercase hex valid",
			input:     "04123456ABCDEF",
			wantError: false,
		},
		{
			name:      "mixed case hex valid",
			input:     "04123456AbCdEf",
			wantError: false,
		},
		{
			name:      "non-ascii character",
			input:     "04123456ñ",
			wantError: true,
			errorType: "invalid character",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := hex.DecodeString(tt.input)
			gotError := err != nil

			if gotError != tt.wantError {
				t.Errorf("hex.DecodeString() error = %v, wantError %v", err, tt.wantError)
			}

			if tt.wantError && err != nil && tt.errorType != "" {
				errStr := err.Error()
				if !strings.Contains(errStr, "invalid") && !strings.Contains(errStr, "odd") {
					t.Errorf("Expected error containing %q, got %q", tt.errorType, errStr)
				}
			}
		})
	}
}

func TestHexEncodingConsistency(t *testing.T) {
	t.Parallel()
	// Test that BaseTag.UID() produces valid hex strings
	testCases := [][]byte{
		{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC},
		{0x12, 0x34, 0x56, 0x78},
		{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66},
		{0xFF, 0xEE, 0xDD, 0xCC},
		{},
		{0x00},
		{0xFF},
	}

	for i, uid := range testCases {
		t.Run(hex.EncodeToString(uid), func(t *testing.T) {
			t.Parallel()
			tag := &BaseTag{
				uid:     uid,
				tagType: TagTypeUnknown,
			}

			hexString := tag.UID()

			// Verify it's valid hex
			decoded, err := hex.DecodeString(hexString)
			if err != nil {
				t.Errorf("BaseTag.UID() produced invalid hex %q: %v", hexString, err)
			}

			// Verify round-trip consistency
			if !equalByteSlices(decoded, uid) {
				t.Errorf("Round trip failed for case %d: got %v, want %v", i, decoded, uid)
			}

			// Verify lowercase
			if hexString != strings.ToLower(hexString) {
				t.Errorf("BaseTag.UID() should produce lowercase hex, got %q", hexString)
			}
		})
	}
}

func TestHexPaddingBehavior(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		expected string
		desc     string
		input    []byte
	}{
		{
			name:     "single digit values",
			input:    []byte{0x01, 0x02, 0x03, 0x04, 0x05},
			expected: "0102030405",
			desc:     "single digit values should be zero-padded",
		},
		{
			name:     "zero bytes",
			input:    []byte{0x00, 0x00, 0x00},
			expected: "000000",
			desc:     "zero bytes should be properly represented",
		},
		{
			name:     "mixed single and double digit",
			input:    []byte{0x0A, 0x01, 0xFF, 0x00},
			expected: "0a01ff00",
			desc:     "mixed values should maintain proper padding",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := hex.EncodeToString(tt.input)
			if result != tt.expected {
				t.Errorf("hex.EncodeToString() = %q, want %q (%s)", result, tt.expected, tt.desc)
			}
		})
	}
}

func TestHexCaseInsensitivity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		lowercase string
		uppercase string
	}{
		{"04123456789abcde", "04123456789ABCDE"},
		{"ff00ff00", "FF00FF00"},
		{"deadbeef", "DEADBEEF"},
		{"0a0b0c0d0e0f", "0A0B0C0D0E0F"},
	}

	for _, tt := range tests {
		t.Run(tt.lowercase, func(t *testing.T) {
			t.Parallel()
			lowerDecoded, err1 := hex.DecodeString(tt.lowercase)
			upperDecoded, err2 := hex.DecodeString(tt.uppercase)

			if err1 != nil || err2 != nil {
				t.Fatalf("Decoding failed: lower=%v, upper=%v", err1, err2)
			}

			if !equalByteSlices(lowerDecoded, upperDecoded) {
				t.Errorf("Case insensitive decoding failed: %v != %v", lowerDecoded, upperDecoded)
			}
		})
	}
}

// Helper function to compare byte slices for equality
func equalByteSlices(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}
