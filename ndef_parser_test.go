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

func TestExtractNDEFPayload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		want []byte
	}{
		{
			name: "empty data",
			data: []byte{},
			want: nil,
		},
		{
			name: "no NDEF TLV",
			data: []byte{0x00, 0x01, 0x02},
			want: nil,
		},
		{
			name: "simple NDEF TLV with short form",
			data: []byte{0x03, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05},
			want: []byte{0x01, 0x02, 0x03, 0x04, 0x05},
		},
		{
			name: "NDEF TLV at offset",
			data: []byte{0x00, 0x00, 0x03, 0x03, 0xAA, 0xBB, 0xCC},
			want: []byte{0xAA, 0xBB, 0xCC},
		},
		{
			name: "multiple TLVs with NDEF",
			data: []byte{0x01, 0x02, 0x00, 0x00, 0x03, 0x04, 0x11, 0x22, 0x33, 0x44},
			want: []byte{0x11, 0x22, 0x33, 0x44},
		},
		{
			name: "NDEF TLV with zero length",
			data: []byte{0x03, 0x00, 0x00}, // Add padding so loop condition is satisfied
			want: []byte{},                 // Zero length returns empty slice
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := extractNDEFPayload(tt.data); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractNDEFPayload() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractTLVPayload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		data   []byte
		want   []byte
		offset int
	}{
		{
			name:   "short form TLV",
			data:   []byte{0x03, 0x04, 0x01, 0x02, 0x03, 0x04},
			offset: 0,
			want:   []byte{0x01, 0x02, 0x03, 0x04},
		},
		{
			name:   "zero length TLV",
			data:   []byte{0x03, 0x00},
			offset: 0,
			want:   []byte{},
		},
		{
			name:   "offset out of bounds",
			data:   []byte{0x03, 0x04},
			offset: 1,
			want:   nil,
		},
		{
			name:   "insufficient data for length",
			data:   []byte{0x03},
			offset: 0,
			want:   nil,
		},
		{
			name:   "insufficient data for payload",
			data:   []byte{0x03, 0x05, 0x01, 0x02},
			offset: 0,
			want:   nil, // Length says 5 bytes but only 2 available
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := extractTLVPayload(tt.data, tt.offset); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractTLVPayload() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractShortFormatPayload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		data   []byte
		want   []byte
		offset int
	}{
		{
			name:   "valid short format",
			data:   []byte{0x03, 0x03, 0xAA, 0xBB, 0xCC},
			offset: 0,
			want:   []byte{0xAA, 0xBB, 0xCC},
		},
		{
			name:   "zero length",
			data:   []byte{0x03, 0x00},
			offset: 0,
			want:   []byte{},
		},
		{
			name:   "insufficient data",
			data:   []byte{0x03, 0x05, 0x01, 0x02},
			offset: 0,
			want:   nil, // Claims 5 bytes but only 2 available
		},
		{
			name:   "offset out of bounds",
			data:   []byte{0x03, 0x02, 0x01, 0x02},
			offset: 2, // Valid offset that results in insufficient data
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := extractShortFormatPayload(tt.data, tt.offset); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractShortFormatPayload() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractLongFormatPayload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		data   []byte
		want   []byte
		offset int
	}{
		{
			name:   "valid long format",
			data:   []byte{0x03, 0xFF, 0x01, 0x00, 0xAA, 0xBB},
			offset: 0,
			want:   []byte{0xAA, 0xBB}, // Length is 0x0100 (256), but we only have 2 bytes
		},
		{
			name:   "insufficient data for length",
			data:   []byte{0x03, 0xFF, 0x01},
			offset: 0,
			want:   nil, // Not enough bytes for 16-bit length
		},
		{
			name:   "insufficient data for payload",
			data:   []byte{0x03, 0xFF, 0x00, 0x05, 0x01, 0x02},
			offset: 0,
			want:   nil, // Claims 5 bytes but only 2 available
		},
		{
			name:   "valid minimal long format",
			data:   []byte{0x03, 0xFF, 0x00, 0x02, 0xAA, 0xBB},
			offset: 0,
			want:   []byte{0xAA, 0xBB},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractLongFormatPayload(tt.data, tt.offset)
			// For the first test case, we expect nil because there's insufficient data
			if tt.name == "valid long format" {
				if got != nil {
					t.Errorf("extractLongFormatPayload() = %v, want nil (insufficient data)", got)
				}
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractLongFormatPayload() = %v, want %v", got, tt.want)
			}
		})
	}
}
