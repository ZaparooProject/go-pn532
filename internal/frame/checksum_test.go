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

package frame

import "testing"

func TestCalculateChecksum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		want byte
	}{
		{
			name: "empty data",
			data: []byte{},
			want: 0,
		},
		{
			name: "single byte",
			data: []byte{0x42},
			want: 0x42,
		},
		{
			name: "two bytes",
			data: []byte{0x10, 0x20},
			want: 0x30,
		},
		{
			name: "overflow handling",
			data: []byte{0xFF, 0x01},
			want: 0x00, // 255 + 1 = 256, truncated to 0
		},
		{
			name: "multiple bytes",
			data: []byte{0x01, 0x02, 0x03, 0x04},
			want: 0x0A,
		},
		{
			name: "real frame data",
			data: []byte{0xD4, 0x03, 0x32, 0x01, 0x00, 0x6B, 0x02, 0x4A, 0x65, 0x6C, 0x6C, 0x6F},
			want: 0x6D, // Sum of all bytes (corrected value)
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := CalculateChecksum(tt.data); got != tt.want {
				t.Errorf("CalculateChecksum() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateChecksum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		data     []byte
		wantNack bool // true if checksum invalid (should NACK)
	}{
		{
			name:     "valid checksum (zero sum)",
			data:     []byte{0x10, 0xF0}, // 0x10 + 0xF0 = 0x00 (valid)
			wantNack: false,
		},
		{
			name:     "invalid checksum",
			data:     []byte{0x10, 0x20}, // 0x10 + 0x20 = 0x30 (invalid)
			wantNack: true,
		},
		{
			name:     "empty data",
			data:     []byte{},
			wantNack: false, // Empty data has checksum 0 (valid)
		},
		{
			name:     "valid frame with correct DCS",
			data:     []byte{0xD4, 0x03, 0x29},
			wantNack: false,
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ValidateChecksum(tt.data); got != tt.wantNack {
				t.Errorf("ValidateChecksum() = %v, want %v", got, tt.wantNack)
			}
		})
	}
}

func TestCalculateDataChecksum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		tfi  byte
		want byte
	}{
		{
			name: "simple case",
			tfi:  0xD4,
			data: []byte{0x02},
			want: 0x2A, // Two's complement of (0xD4 + 0x02)
		},
		{
			name: "empty data",
			tfi:  0xD4,
			data: []byte{},
			want: 0x2C, // Two's complement of 0xD4
		},
		{
			name: "multiple bytes",
			tfi:  0xD4,
			data: []byte{0x02, 0x01, 0x03},
			want: 0x26, // Two's complement of (0xD4 + 0x02 + 0x01 + 0x03)
		},
		{
			name: "real GetFirmwareVersion command",
			tfi:  0xD4,
			data: []byte{0x02},
			want: 0x2A,
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := CalculateDataChecksum(tt.tfi, tt.data); got != tt.want {
				t.Errorf("CalculateDataChecksum() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateLengthChecksum(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		length byte
		want   byte
	}{
		{
			name:   "length 2",
			length: 0x02,
			want:   0xFE, // Two's complement of 0x02
		},
		{
			name:   "length 1",
			length: 0x01,
			want:   0xFF, // Two's complement of 0x01
		},
		{
			name:   "length 255",
			length: 0xFF,
			want:   0x01, // Two's complement of 0xFF
		},
		{
			name:   "length 0",
			length: 0x00,
			want:   0x00, // Two's complement of 0x00
		},
		{
			name:   "length 16",
			length: 0x10,
			want:   0xF0, // Two's complement of 0x10
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := CalculateLengthChecksum(tt.length); got != tt.want {
				t.Errorf("CalculateLengthChecksum() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestChecksumProperty verifies the mathematical property that
// length + LCS should always equal 0 (mod 256)
func TestChecksumProperty(t *testing.T) {
	t.Parallel()
	for i := 0; i < 256; i++ {
		length := byte(i)
		lcs := CalculateLengthChecksum(length)
		sum := length + lcs
		if sum != 0 {
			t.Errorf("Property violation: length=%d + LCS=%d = %d, expected 0", length, lcs, sum)
		}
	}
}
