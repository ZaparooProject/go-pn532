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

func TestParseRecordHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		want   *ndefRecord
		name   string
		header byte
	}{
		{
			name:   "all flags clear",
			header: 0x00,
			want: &ndefRecord{
				MB: false, ME: false, CF: false, SR: false, IL: false, TNF: 0x00,
			},
		},
		{
			name:   "MB flag set",
			header: 0x80, // flagMB
			want: &ndefRecord{
				MB: true, ME: false, CF: false, SR: false, IL: false, TNF: 0x00,
			},
		},
		{
			name:   "ME flag set",
			header: 0x40, // flagME
			want: &ndefRecord{
				MB: false, ME: true, CF: false, SR: false, IL: false, TNF: 0x00,
			},
		},
		{
			name:   "CF flag set",
			header: 0x20, // flagCF
			want: &ndefRecord{
				MB: false, ME: false, CF: true, SR: false, IL: false, TNF: 0x00,
			},
		},
		{
			name:   "SR flag set",
			header: 0x10, // flagSR
			want: &ndefRecord{
				MB: false, ME: false, CF: false, SR: true, IL: false, TNF: 0x00,
			},
		},
		{
			name:   "IL flag set",
			header: 0x08, // flagIL
			want: &ndefRecord{
				MB: false, ME: false, CF: false, SR: false, IL: true, TNF: 0x00,
			},
		},
		{
			name:   "TNF well-known",
			header: 0x01, // TNFWellKnown
			want: &ndefRecord{
				MB: false, ME: false, CF: false, SR: false, IL: false, TNF: 0x01,
			},
		},
		{
			name:   "all flags set with TNF",
			header: 0xFF, // All bits set
			want: &ndefRecord{
				MB: true, ME: true, CF: true, SR: true, IL: true, TNF: 0x07,
			},
		},
		{
			name:   "typical short record",
			header: 0xD1, // MB|ME|SR|TNFWellKnown
			want: &ndefRecord{
				MB: true, ME: true, CF: false, SR: true, IL: false, TNF: 0x01,
			},
		},
		{
			name:   "TNF mask test",
			header: 0xF7, // Most flags set, IL=false, TNF = 7 (reserved)
			want: &ndefRecord{
				MB: true, ME: true, CF: true, SR: true, IL: false, TNF: 0x07,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseRecordHeader(tt.header)
			compareRecordHeaders(t, got, tt.want)
		})
	}
}

func compareRecordHeaders(t *testing.T, got, want *ndefRecord) {
	if got.MB != want.MB {
		t.Errorf("MB: got %v, want %v", got.MB, want.MB)
	}
	if got.ME != want.ME {
		t.Errorf("ME: got %v, want %v", got.ME, want.ME)
	}
	if got.CF != want.CF {
		t.Errorf("CF: got %v, want %v", got.CF, want.CF)
	}
	if got.SR != want.SR {
		t.Errorf("SR: got %v, want %v", got.SR, want.SR)
	}
	if got.IL != want.IL {
		t.Errorf("IL: got %v, want %v", got.IL, want.IL)
	}
	if got.TNF != want.TNF {
		t.Errorf("TNF: got %v, want %v", got.TNF, want.TNF)
	}
}

func TestValidateTNF(t *testing.T) {
	t.Parallel()

	testValidateTNFValid(t)
	testValidateTNFInvalid(t)
}

func testValidateTNFValid(t *testing.T) {
	tests := []struct {
		name    string
		tnf     uint8
		ctx     validationContext
		wantErr bool
	}{
		{
			name:    "TNF Empty",
			tnf:     TNFEmpty,
			ctx:     validationContext{},
			wantErr: false,
		},
		{
			name:    "TNF Well-known",
			tnf:     TNFWellKnown,
			ctx:     validationContext{},
			wantErr: false,
		},
		{
			name:    "TNF Media Type",
			tnf:     TNFMediaType,
			ctx:     validationContext{},
			wantErr: false,
		},
		{
			name:    "TNF Absolute URI",
			tnf:     TNFAbsoluteURI,
			ctx:     validationContext{},
			wantErr: false,
		},
		{
			name:    "TNF External Type",
			tnf:     TNFExternalType,
			ctx:     validationContext{},
			wantErr: false,
		},
		{
			name:    "TNF Unknown",
			tnf:     TNFUnknown,
			ctx:     validationContext{},
			wantErr: false,
		},
		{
			name:    "TNF Unchanged in chunk",
			tnf:     TNFUnchanged,
			ctx:     validationContext{inChunk: true},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateTNF(tt.tnf, tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTNF() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func testValidateTNFInvalid(t *testing.T) {
	tests := []struct {
		name    string
		tnf     uint8
		ctx     validationContext
		wantErr bool
	}{
		{
			name:    "TNF Unchanged without chunk",
			tnf:     TNFUnchanged,
			ctx:     validationContext{inChunk: false},
			wantErr: true,
		},
		{
			name:    "TNF Reserved",
			tnf:     TNFReserved,
			ctx:     validationContext{},
			wantErr: true,
		},
		{
			name:    "Invalid TNF value 8",
			tnf:     8,
			ctx:     validationContext{},
			wantErr: true,
		},
		{
			name:    "Invalid TNF value 255",
			tnf:     255,
			ctx:     validationContext{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateTNF(tt.tnf, tt.ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTNF() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateLengths(t *testing.T) {
	t.Parallel()

	testValidateLengthsEmpty(t)
	testValidateLengthsSpecial(t)
	testValidateLengthsBoundaries(t)
}

func testValidateLengthsEmpty(t *testing.T) {
	runValidateLengthsTests(t, []validateLengthsTestCase{
		createValidLengthsTestCase("empty record valid", TNFEmpty, [2]uint8{0, 0}, 0, false),
		createValidLengthsTestCase("empty record with non-zero type length", TNFEmpty, [2]uint8{1, 0}, 0, true),
		createValidLengthsTestCase("empty record with non-zero payload length", TNFEmpty, [2]uint8{0, 0}, 1, true),
		createValidLengthsTestCase("empty record with non-zero ID length", TNFEmpty, [2]uint8{0, 1}, 0, true),
	})
}

func testValidateLengthsSpecial(t *testing.T) {
	runValidateLengthsTests(t, []validateLengthsTestCase{
		createValidLengthsTestCase("unknown record valid", TNFUnknown, [2]uint8{0, 0}, 10, false),
		createValidLengthsTestCase("unknown record with type", TNFUnknown, [2]uint8{1, 0}, 10, true),
		createValidLengthsTestCase("unchanged record valid", TNFUnchanged, [2]uint8{0, 0}, 10, false),
		createValidLengthsTestCase("unchanged record with type", TNFUnchanged, [2]uint8{1, 0}, 10, true),
	})
}

func createValidLengthsTestCase(
	name string, tnf uint8, lengths [2]uint8, payloadLen uint32, wantErr bool,
) validateLengthsTestCase {
	return validateLengthsTestCase{
		name: name,
		record: &ndefRecord{
			TNF:           tnf,
			TypeLength:    lengths[0],
			PayloadLength: payloadLen,
			IDLength:      lengths[1],
		},
		wantErr: wantErr,
	}
}

func testValidateLengthsBoundaries(t *testing.T) {
	tests := []validateLengthsTestCase{
		{
			name: "type length at maximum",
			record: &ndefRecord{
				TNF:           TNFWellKnown,
				TypeLength:    255,
				PayloadLength: 0,
				IDLength:      0,
			},
			wantErr: false,
		},
		{
			name: "ID length at maximum",
			record: &ndefRecord{
				TNF:           TNFWellKnown,
				TypeLength:    1,
				PayloadLength: 0,
				IDLength:      255,
			},
			wantErr: false,
		},
		{
			name: "well-known record valid",
			record: &ndefRecord{
				TNF:           TNFWellKnown,
				TypeLength:    1,
				PayloadLength: 100,
				IDLength:      5,
			},
			wantErr: false,
		},
	}

	runValidateLengthsTests(t, tests)
}

func runValidateLengthsTests(t *testing.T, tests []validateLengthsTestCase) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateLengths(tt.record)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateLengths() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type validateLengthsTestCase struct {
	record  *ndefRecord
	name    string
	wantErr bool
}

func TestValidateTypeField(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		typeField []byte
		tnf       uint8
		wantErr   bool
	}{
		{
			name:      "well-known type valid",
			tnf:       TNFWellKnown,
			typeField: []byte("T"),
			wantErr:   false,
		},
		{
			name:      "well-known type empty",
			tnf:       TNFWellKnown,
			typeField: []byte{},
			wantErr:   true,
		},
		{
			name:      "media type valid",
			tnf:       TNFMediaType,
			typeField: []byte("text/plain"),
			wantErr:   false,
		},
		{
			name:      "media type empty",
			tnf:       TNFMediaType,
			typeField: []byte{},
			wantErr:   true,
		},
		{
			name:      "absolute URI valid",
			tnf:       TNFAbsoluteURI,
			typeField: []byte("http://example.com"),
			wantErr:   false,
		},
		{
			name:      "absolute URI empty",
			tnf:       TNFAbsoluteURI,
			typeField: []byte{},
			wantErr:   true,
		},
		{
			name:      "external type valid",
			tnf:       TNFExternalType,
			typeField: []byte("example.com:mytype"),
			wantErr:   false,
		},
		{
			name:      "external type empty",
			tnf:       TNFExternalType,
			typeField: []byte{},
			wantErr:   true,
		},
		{
			name:      "empty record type field",
			tnf:       TNFEmpty,
			typeField: []byte{},
			wantErr:   false,
		},
		{
			name:      "unknown record type field",
			tnf:       TNFUnknown,
			typeField: []byte{},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateTypeField(tt.tnf, tt.typeField)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateTypeField() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsValidNDEFMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{
			name: "empty data",
			data: []byte{},
			want: false,
		},
		{
			name: "valid minimal NDEF",
			data: []byte{0x03, 0x05, 0xD1, 0x01, 0x01, 0x54, 0x02},
			want: true,
		},
		{
			name: "no NDEF TLV",
			data: []byte{0x00, 0x01, 0x02},
			want: false,
		},
		{
			name: "invalid TLV structure",
			data: []byte{0x03, 0x10, 0x01, 0x02}, // Claims 16 bytes but only 2 provided
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsValidNDEFMessage(tt.data)
			if got != tt.want {
				t.Errorf("IsValidNDEFMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidNDEFPayload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload []byte
		want    bool
	}{
		{
			name:    "empty payload",
			payload: []byte{},
			want:    false,
		},
		{
			name: "valid short record",
			// MB|ME|SR|TNFWellKnown, type='T', payload=1 byte, payload="02"
			payload: []byte{0xD1, 0x01, 0x01, 0x54, 0x02},
			want:    true,
		},
		{
			name:    "invalid record structure",
			payload: []byte{0xD1, 0x01}, // Incomplete record
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsValidNDEFPayload(tt.payload)
			if got != tt.want {
				t.Errorf("IsValidNDEFPayload() = %v, want %v", got, tt.want)
			}
		})
	}
}
