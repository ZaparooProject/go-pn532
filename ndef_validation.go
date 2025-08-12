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
	"encoding/binary"
	"errors"
	"fmt"
)

// NDEF validation errors
var (
	ErrInvalidTLV           = errors.New("invalid TLV structure")
	ErrInvalidRecordHeader  = errors.New("invalid NDEF record header")
	ErrInvalidTNF           = errors.New("invalid TNF value")
	ErrInvalidTypeLength    = errors.New("invalid TYPE length")
	ErrInvalidPayloadLength = errors.New("invalid PAYLOAD length")
	ErrInvalidIDLength      = errors.New("invalid ID length")
	ErrIncompleteRecord     = errors.New("incomplete NDEF record")
	ErrInvalidMBME          = errors.New("invalid MB/ME flags")
	ErrInvalidChunking      = errors.New("invalid chunking")
	ErrTooManyRecords       = errors.New("too many NDEF records")
)

// TNF (Type Name Format) values
const (
	TNFEmpty        = 0x00
	TNFWellKnown    = 0x01
	TNFMediaType    = 0x02
	TNFAbsoluteURI  = 0x03
	TNFExternalType = 0x04
	TNFUnknown      = 0x05
	TNFUnchanged    = 0x06
	TNFReserved     = 0x07
)

// NDEF record header flags
const (
	flagMB  = 0x80 // Message Begin
	flagME  = 0x40 // Message End
	flagCF  = 0x20 // Chunk Flag
	flagSR  = 0x10 // Short Record
	flagIL  = 0x08 // ID Length present
	flagTNF = 0x07 // Type Name Format mask
)

// Maximum values for validation
const (
	maxRecordsPerMessage = 255
	maxTypeFieldLength   = 255
	maxIDFieldLength     = 255
)

// ValidateNDEFMessage validates a complete NDEF message including TLV wrapper
func ValidateNDEFMessage(data []byte) error {
	// Validate TLV structure
	payload, err := validateAndExtractTLV(data)
	if err != nil {
		return err
	}

	// Validate NDEF message structure
	return validateNDEFStructure(payload)
}

// parseTLVLength parses the length field of a TLV and returns the length and data start position
func parseTLVLength(data []byte, i int) (length, start int, err error) {
	if data[i+1] != 0xFF {
		// Short format (1 byte length)
		return int(data[i+1]), i + 2, nil
	}
	// Long format (3 bytes: 0xFF + 2 byte length)
	if i+3 >= len(data) {
		return 0, 0, fmt.Errorf("%w: incomplete long length", ErrInvalidTLV)
	}
	return int(binary.BigEndian.Uint16(data[i+2 : i+4])), i + 4, nil
}

// skipTLV skips over a TLV block and returns the new position
func skipTLV(data []byte, i int) int {
	if i+1 < len(data) {
		length := int(data[i+1])
		return i + 2 + length
	}
	return i + 1
}

// validateAndExtractTLV validates the TLV (Type-Length-Value) wrapper
func validateAndExtractTLV(data []byte) ([]byte, error) {
	if len(data) < 3 {
		return nil, fmt.Errorf("%w: data too short", ErrInvalidTLV)
	}

	ndefData, found, err := findNDEFTLV(data)
	if err != nil {
		return nil, err
	}

	if !found {
		return nil, fmt.Errorf("%w: no NDEF TLV found", ErrInvalidTLV)
	}

	return ndefData, nil
}

func findNDEFTLV(data []byte) (ndefData []byte, found bool, err error) {
	var ndefStart int
	var ndefLength int

	for i := 0; i < len(data)-2; {
		result, err := processTLVEntry(data, i, &tlvProcessingState{alreadyFound: found})
		if err != nil {
			return nil, false, err
		}

		if result.foundTLV {
			if found {
				return nil, false, fmt.Errorf("%w: multiple NDEF TLVs found", ErrInvalidTLV)
			}
			ndefStart = result.start
			ndefLength = result.length
			found = true
		}

		i = result.newIndex
	}

	if !found {
		return nil, false, nil
	}

	// Validate length
	if ndefStart+ndefLength > len(data) {
		return nil, false, fmt.Errorf("%w: NDEF length exceeds data size", ErrInvalidTLV)
	}

	return data[ndefStart : ndefStart+ndefLength], true, nil
}

type tlvProcessResult struct {
	newIndex int
	start    int
	length   int
	foundTLV bool
}

type tlvProcessingState struct {
	alreadyFound bool
}

func processTLVEntry(data []byte, i int, state *tlvProcessingState) (tlvProcessResult, error) {
	switch data[i] {
	case 0x00: // NULL TLV - padding
		return tlvProcessResult{newIndex: i + 1}, nil

	case 0x03: // NDEF Message TLV

		ndefLength, ndefStart, err := parseTLVLength(data, i)
		if err != nil {
			return tlvProcessResult{}, err
		}

		return tlvProcessResult{
			newIndex: ndefStart + ndefLength,
			start:    ndefStart,
			length:   ndefLength,
			foundTLV: true,
		}, nil

	case 0xFE: // Terminator TLV
		if !state.alreadyFound {
			return tlvProcessResult{}, fmt.Errorf("%w: terminator before NDEF TLV", ErrInvalidTLV)
		}
		return tlvProcessResult{newIndex: i + 1}, nil

	case 0x01, 0x02: // Lock Control TLV, Memory Control TLV
		return tlvProcessResult{newIndex: skipTLV(data, i)}, nil

	default:
		// Proprietary TLV (0x04-0xFD) - skip
		if data[i] >= 0x04 && data[i] <= 0xFD {
			return tlvProcessResult{newIndex: skipTLV(data, i)}, nil
		}
		return tlvProcessResult{newIndex: i + 1}, nil
	}
}

// validateNDEFStructure validates the structure of NDEF records
func validateNDEFStructure(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("%w: empty NDEF message", ErrInvalidNDEF)
	}

	state := &ndefValidationState{
		firstRecord:    true,
		lastRecordSeen: false,
		inChunk:        false,
	}

	pos := 0
	recordCount := 0

	for pos < len(data) {
		if recordCount > maxRecordsPerMessage {
			return ErrTooManyRecords
		}

		record, consumed, err := validateRecord(data[pos:], state.firstRecord, state.lastRecordSeen, state.inChunk)
		if err != nil {
			return fmt.Errorf("record %d: %w", recordCount, err)
		}

		if err := updateValidationState(state, record, recordCount); err != nil {
			return err
		}

		recordCount++
		pos += consumed
	}

	return validateMessageCompleteness(state)
}

type ndefValidationState struct {
	firstRecord    bool
	lastRecordSeen bool
	inChunk        bool
}

func updateValidationState(state *ndefValidationState, record *ndefRecord, recordCount int) error {
	if err := validateMBFlag(record, recordCount); err != nil {
		return err
	}

	if record.ME {
		state.lastRecordSeen = true
	}

	if err := validateChunkingFlags(record, recordCount); err != nil {
		return err
	}

	updateChunkingState(state, record)
	state.firstRecord = false
	return nil
}

func validateMBFlag(record *ndefRecord, recordCount int) error {
	isFirstRecord := recordCount == 0
	if record.MB && !isFirstRecord {
		return fmt.Errorf("record %d: %w: MB flag on non-first record", recordCount, ErrInvalidMBME)
	}
	return nil
}

func validateChunkingFlags(record *ndefRecord, recordCount int) error {
	if record.CF && record.ME {
		return fmt.Errorf("record %d: %w: CF and ME both set", recordCount, ErrInvalidChunking)
	}
	return nil
}

func updateChunkingState(state *ndefValidationState, record *ndefRecord) {
	if record.CF {
		state.inChunk = true
	} else {
		state.inChunk = false
	}
}

func validateMessageCompleteness(state *ndefValidationState) error {
	if !state.lastRecordSeen {
		return fmt.Errorf("%w: no record with ME flag", ErrInvalidMBME)
	}
	return nil
}

// ndefRecord represents a parsed NDEF record header
type ndefRecord struct {
	PayloadLength uint32
	TNF           uint8
	TypeLength    uint8
	IDLength      uint8
	MB            bool
	ME            bool
	CF            bool
	SR            bool
	IL            bool
}

// validateRecord validates a single NDEF record
func validateRecord(data []byte, _, afterME, inChunk bool) (*ndefRecord, int, error) {
	if len(data) < 3 {
		return nil, 0, ErrIncompleteRecord
	}

	record := parseRecordHeader(data[0])

	// Validate flags and TNF
	if err := validateRecordFlags(record, validationContext{afterME: afterME, inChunk: inChunk}); err != nil {
		return nil, 0, err
	}

	// Parse record structure
	pos, err := parseRecordStructure(data, record)
	if err != nil {
		return nil, 0, err
	}

	// Validate record completeness and type field
	totalSize, err := validateRecordCompleteness(data, record, pos)
	if err != nil {
		return nil, 0, err
	}

	return record, totalSize, nil
}

// parseRecordHeader parses the NDEF record header byte
func parseRecordHeader(header byte) *ndefRecord {
	return &ndefRecord{
		MB:  (header & flagMB) != 0,
		ME:  (header & flagME) != 0,
		CF:  (header & flagCF) != 0,
		SR:  (header & flagSR) != 0,
		IL:  (header & flagIL) != 0,
		TNF: header & flagTNF,
	}
}

type validationContext struct {
	afterME bool
	inChunk bool
}

// validateRecordFlags validates record flags and TNF
func validateRecordFlags(record *ndefRecord, ctx validationContext) error {
	// Validate flags
	if ctx.afterME {
		return fmt.Errorf("%w: record after ME flag", ErrInvalidMBME)
	}

	// Validate TNF
	return validateTNF(record.TNF, ctx)
}

// parseRecordStructure parses the record's type length, payload length, and ID length
func parseRecordStructure(data []byte, record *ndefRecord) (int, error) {
	// Parse type length
	record.TypeLength = data[1]
	pos := 2

	// Parse payload length
	var err error
	pos, err = parsePayloadLength(data, record, pos)
	if err != nil {
		return 0, err
	}

	// Parse ID length if present
	return parseIDLength(data, record, pos)
}

// parsePayloadLength parses the payload length field
func parsePayloadLength(data []byte, record *ndefRecord, pos int) (int, error) {
	if record.SR {
		// Short record - 1 byte payload length
		if pos >= len(data) {
			return 0, ErrIncompleteRecord
		}
		record.PayloadLength = uint32(data[pos])
		return pos + 1, nil
	}

	// Normal record - 4 byte payload length
	if pos+4 > len(data) {
		return 0, ErrIncompleteRecord
	}
	record.PayloadLength = binary.BigEndian.Uint32(data[pos : pos+4])
	return pos + 4, nil
}

// parseIDLength parses the ID length field if present
func parseIDLength(data []byte, record *ndefRecord, pos int) (int, error) {
	if record.IL {
		if pos >= len(data) {
			return 0, ErrIncompleteRecord
		}
		record.IDLength = data[pos]
		return pos + 1, nil
	}
	return pos, nil
}

// validateRecordCompleteness validates record lengths and type field
func validateRecordCompleteness(data []byte, record *ndefRecord, pos int) (int, error) {
	// Validate lengths
	if err := validateLengths(record); err != nil {
		return 0, err
	}

	// Calculate total record size
	totalSize := pos + int(record.TypeLength) + int(record.PayloadLength) + int(record.IDLength)
	if totalSize > len(data) {
		return 0, ErrIncompleteRecord
	}

	// Validate TYPE field format based on TNF
	if record.TypeLength > 0 {
		typeField := data[pos : pos+int(record.TypeLength)]
		if err := validateTypeField(record.TNF, typeField); err != nil {
			return 0, err
		}
	}

	return totalSize, nil
}

// validateTNF validates the Type Name Format value
func validateTNF(tnf uint8, ctx validationContext) error {
	switch tnf {
	case TNFEmpty:
		// Empty record - valid
		return nil

	case TNFWellKnown, TNFMediaType, TNFAbsoluteURI, TNFExternalType:
		// Valid TNF values
		return nil

	case TNFUnknown:
		// Unknown type - valid but type should not be present
		return nil

	case TNFUnchanged:
		// Only valid for middle and terminating chunks
		if !ctx.inChunk {
			return fmt.Errorf("%w: TNF Unchanged without chunking", ErrInvalidTNF)
		}
		return nil

	case TNFReserved:
		return fmt.Errorf("%w: TNF Reserved value", ErrInvalidTNF)

	default:
		return fmt.Errorf("%w: TNF value %d", ErrInvalidTNF, tnf)
	}
}

// validateLengths validates field lengths
func validateLengths(record *ndefRecord) error {
	// Empty record validation
	if record.TNF == TNFEmpty {
		if record.TypeLength != 0 || record.PayloadLength != 0 || record.IDLength != 0 {
			return fmt.Errorf("%w: empty record with non-zero lengths", ErrInvalidTNF)
		}
		return nil
	}

	// Unknown record validation
	if record.TNF == TNFUnknown {
		if record.TypeLength != 0 {
			return fmt.Errorf("%w: unknown record with type", ErrInvalidTypeLength)
		}
	}

	// Unchanged record validation (for chunks)
	if record.TNF == TNFUnchanged {
		if record.TypeLength != 0 {
			return fmt.Errorf("%w: unchanged record with type", ErrInvalidTypeLength)
		}
	}

	// Type length validation
	if record.TypeLength > maxTypeFieldLength {
		return fmt.Errorf("%w: type length %d exceeds maximum", ErrInvalidTypeLength, record.TypeLength)
	}

	// ID length validation
	if record.IDLength > maxIDFieldLength {
		return fmt.Errorf("%w: ID length %d exceeds maximum", ErrInvalidIDLength, record.IDLength)
	}

	return nil
}

// validateTypeField validates the TYPE field format based on TNF
func validateTypeField(tnf uint8, typeField []byte) error {
	switch tnf {
	case TNFWellKnown:
		// Should be a well-known type (e.g., "T", "U", "Sp")
		if len(typeField) == 0 {
			return fmt.Errorf("%w: empty well-known type", ErrInvalidTypeLength)
		}
		// Additional validation could check against known RTD types

	case TNFMediaType:
		// Should be a valid MIME type
		if len(typeField) == 0 {
			return fmt.Errorf("%w: empty media type", ErrInvalidTypeLength)
		}
		// Could validate MIME type format

	case TNFAbsoluteURI:
		// Should be a valid URI
		if len(typeField) == 0 {
			return fmt.Errorf("%w: empty URI type", ErrInvalidTypeLength)
		}
		// Could validate URI format

	case TNFExternalType:
		// Should follow external type format (domain:type)
		if len(typeField) == 0 {
			return fmt.Errorf("%w: empty external type", ErrInvalidTypeLength)
		}
		// Could validate external type format
	}

	return nil
}

// ValidateNDEFPayload validates just the NDEF payload without TLV wrapper
func ValidateNDEFPayload(payload []byte) error {
	return validateNDEFStructure(payload)
}

// IsValidNDEFMessage checks if data contains a valid NDEF message
func IsValidNDEFMessage(data []byte) bool {
	return ValidateNDEFMessage(data) == nil
}

// IsValidNDEFPayload checks if payload is a valid NDEF structure
func IsValidNDEFPayload(payload []byte) bool {
	return ValidateNDEFPayload(payload) == nil
}
