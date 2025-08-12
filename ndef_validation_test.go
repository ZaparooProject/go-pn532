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
	"testing"
)

func TestValidateNDEFMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		data    []byte
		wantErr bool
	}{
		{
			name:    "empty data",
			data:    []byte{},
			wantErr: true,
		},
		{
			name:    "valid simple NDEF",
			data:    []byte{0x03, 0x05, 0xD1, 0x01, 0x01, 0x54, 0x02},
			wantErr: false,
		},
		{
			name:    "no NDEF TLV found",
			data:    []byte{0x00, 0x01, 0x02, 0x04},
			wantErr: true,
		},
		{
			name:    "valid NDEF with padding",
			data:    []byte{0x00, 0x00, 0x03, 0x05, 0xD1, 0x01, 0x01, 0x54, 0x02, 0xFE},
			wantErr: false,
		},
		{
			name:    "truncated TLV",
			data:    []byte{0x03, 0x10, 0x01, 0x02}, // Claims 16 bytes but only 2 provided
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateNDEFMessage(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateNDEFMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseTLVLength(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		data       []byte
		i          int
		wantLength int
		wantStart  int
		wantErr    bool
	}{
		{
			name:       "short form length",
			data:       []byte{0x03, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05},
			i:          0,
			wantLength: 5,
			wantStart:  2,
			wantErr:    false,
		},
		{
			name:       "zero length",
			data:       []byte{0x03, 0x00},
			i:          0,
			wantLength: 0,
			wantStart:  2,
			wantErr:    false,
		},
		{
			name:       "long form marker without length bytes",
			data:       []byte{0x03, 0xFF},
			i:          0,
			wantLength: 0,
			wantStart:  0,
			wantErr:    true,
		},
		{
			name:       "long form with 16-bit length",
			data:       []byte{0x03, 0xFF, 0x01, 0x00},
			i:          0,
			wantLength: 256,
			wantStart:  4,
			wantErr:    false,
		},
		{
			name:       "valid boundary condition",
			data:       []byte{0x03, 0x05, 0x02},
			i:          1, // Tag is 0x05, length is 0x02
			wantLength: 2,
			wantStart:  3,
			wantErr:    false,
		},
		{
			name:       "incomplete long form length",
			data:       []byte{0x03, 0xFF, 0x01},
			i:          0,
			wantLength: 0,
			wantStart:  0,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotLength, gotStart, err := parseTLVLength(tt.data, tt.i)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTLVLength() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotLength != tt.wantLength {
				t.Errorf("parseTLVLength() gotLength = %v, want %v", gotLength, tt.wantLength)
			}
			if gotStart != tt.wantStart {
				t.Errorf("parseTLVLength() gotStart = %v, want %v", gotStart, tt.wantStart)
			}
		})
	}
}

func TestSkipTLV(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		i    int
		want int
	}{
		{
			name: "short form TLV",
			data: []byte{0x01, 0x03, 0xAA, 0xBB, 0xCC, 0x02, 0x01, 0xFF},
			i:    0,
			want: 5, // Skip T(1) + L(1) + V(3) = 5 bytes
		},
		{
			name: "zero length TLV",
			data: []byte{0x01, 0x00, 0x02, 0x01, 0xFF},
			i:    0,
			want: 2, // Skip T(1) + L(1) = 2 bytes
		},
		{
			name: "invalid TLV at end",
			data: []byte{0x01, 0x05, 0xAA}, // Claims 5 bytes but only 1 available
			i:    0,
			want: 7, // i + 2 + length = 0 + 2 + 5 = 7 (goes beyond data)
		},
		{
			name: "long form marker treated as length",
			data: []byte{0x01, 0xFF, 0x00, 0x02, 0xAA, 0xBB, 0x03},
			i:    0,
			want: 257, // i + 2 + 0xFF = 0 + 2 + 255 = 257 (function doesn't handle long form)
		},
		{
			name: "offset at end",
			data: []byte{0x01, 0x02, 0xAA, 0xBB},
			i:    4,
			want: 5, // i + 1 = 4 + 1 = 5 (boundary condition)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := skipTLV(tt.data, tt.i); got != tt.want {
				t.Errorf("skipTLV() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindNDEFTLV(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		data         []byte
		wantNdefData []byte
		wantFound    bool
		wantErr      bool
	}{
		{
			name:         "simple NDEF TLV",
			data:         []byte{0x03, 0x03, 0xD1, 0x01, 0x01},
			wantNdefData: []byte{0xD1, 0x01, 0x01},
			wantFound:    true,
			wantErr:      false,
		},
		{
			name:         "no NDEF TLV",
			data:         []byte{0x01, 0x02, 0xAA, 0xBB, 0x02, 0x01, 0xCC},
			wantNdefData: nil,
			wantFound:    false,
			wantErr:      false,
		},
		{
			name:         "NDEF TLV with other TLVs",
			data:         []byte{0x01, 0x01, 0xAA, 0x03, 0x02, 0xBB, 0xCC, 0x02, 0x01, 0xDD},
			wantNdefData: []byte{0xBB, 0xCC},
			wantFound:    true,
			wantErr:      false,
		},
		{
			name:         "empty data",
			data:         []byte{},
			wantNdefData: nil,
			wantFound:    false,
			wantErr:      false,
		},
		{
			name:         "truncated NDEF TLV",
			data:         []byte{0x03, 0x05, 0x01, 0x02}, // Claims 5 bytes but only 2 available
			wantNdefData: nil,
			wantFound:    false,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotNdefData, gotFound, err := findNDEFTLV(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("findNDEFTLV() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !equal(gotNdefData, tt.wantNdefData) {
				t.Errorf("findNDEFTLV() gotNdefData = %v, want %v", gotNdefData, tt.wantNdefData)
			}
			if gotFound != tt.wantFound {
				t.Errorf("findNDEFTLV() gotFound = %v, want %v", gotFound, tt.wantFound)
			}
		})
	}
}

// equal compares two byte slices for equality, handling nil cases
func equal(data1, data2 []byte) bool {
	if data1 == nil && data2 == nil {
		return true
	}
	if data1 == nil || data2 == nil {
		return false
	}
	if len(data1) != len(data2) {
		return false
	}
	for i, v := range data1 {
		if v != data2[i] {
			return false
		}
	}
	return true
}
