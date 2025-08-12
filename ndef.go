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
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// NDEFRecordType represents the type of an NDEF record
type NDEFRecordType string

const (
	// NDEFTypeText represents a text record type
	NDEFTypeText NDEFRecordType = "text"
	// NDEFTypeURI represents a URI record type
	NDEFTypeURI NDEFRecordType = "uri"
	// NDEFTypeSmartPoster represents a smart poster record type
	NDEFTypeSmartPoster NDEFRecordType = "smartposter"
)

var (
	// Security constants for memory protection
	MaxNDEFMessageSize = 8192 // Maximum NDEF message size (8KB)
	MaxNDEFRecordCount = 255  // Maximum records per message
	MaxNDEFPayloadSize = 4096 // Maximum payload size per record
	MaxNDEFTypeLength  = 255  // Maximum type field length
	MaxNDEFIDLength    = 255  // Maximum ID field length

	// Error for security violations
	ErrSecurityViolation = errors.New("security violation: data exceeds safety limits")

	// NDEF markers
	ndefEnd   = []byte{0xFE}
	ndefStart = []byte{0x54, 0x02, 0x65, 0x6E} // Text record with "en" language

	// ErrNoNDEF is returned when no NDEF record is found.
	ErrNoNDEF = errors.New("no NDEF record found")
	// ErrInvalidNDEF is returned when the NDEF format is invalid.
	ErrInvalidNDEF = errors.New("invalid NDEF format")
)

// NDEFMessage represents an NDEF message
type NDEFMessage struct {
	Records []NDEFRecord
}

// NDEFRecord represents a single NDEF record
type NDEFRecord struct {
	WiFi    *WiFiCredential
	VCard   *VCardContact
	Text    string
	URI     string
	Type    NDEFRecordType
	Payload []byte
}

// ParseNDEFData parses raw NDEF data into an NDEFMessage
func ParseNDEFData(data []byte) (*NDEFMessage, error) {
	// SECURITY: Validate total data size before processing
	if len(data) > MaxNDEFMessageSize {
		return nil, fmt.Errorf("%w: data size %d exceeds maximum %d",
			ErrSecurityViolation, len(data), MaxNDEFMessageSize)
	}

	if len(data) < 4 {
		return nil, fmt.Errorf("%w: data too short for NDEF", ErrInvalidNDEF)
	}

	// Find NDEF start marker
	startIndex := bytes.LastIndex(data, ndefStart)
	if startIndex == -1 {
		return nil, ErrNoNDEF
	}

	// Check for corrupted data with multiple NDEF starts
	if len(data) > startIndex+8 {
		nextStart := bytes.Index(data[startIndex+4:], ndefStart)
		if nextStart != -1 {
			startIndex += nextStart + 4
		}
	}

	// Find NDEF end marker
	endIndex := bytes.Index(data[startIndex:], ndefEnd)
	if endIndex == -1 {
		return nil, fmt.Errorf("%w: end marker not found", ErrInvalidNDEF)
	}
	endIndex += startIndex

	// Validate indices
	if startIndex >= endIndex {
		return nil, fmt.Errorf("%w: invalid start index", ErrInvalidNDEF)
	}

	// SECURITY: Comprehensive bounds checking
	if startIndex+4 > len(data) {
		return nil, fmt.Errorf("%w: insufficient data for NDEF header", ErrInvalidNDEF)
	}

	if endIndex > len(data) {
		return nil, fmt.Errorf("%w: invalid start index", ErrInvalidNDEF)
	}

	// SECURITY: Validate text length before extraction
	textLength := endIndex - (startIndex + 4)
	if textLength > MaxNDEFPayloadSize {
		return nil, fmt.Errorf("%w: text payload %d exceeds maximum %d",
			ErrSecurityViolation, textLength, MaxNDEFPayloadSize)
	}

	// Extract text from simple text record
	// TODO: Properly parse full NDEF structure
	text := string(data[startIndex+4 : endIndex])

	return &NDEFMessage{
		Records: []NDEFRecord{
			{
				Type:    NDEFTypeText,
				Text:    text,
				Payload: data[startIndex:endIndex],
			},
		},
	}, nil
}

// BuildNDEFMessage creates NDEF data from text
// Deprecated: Use BuildTextMessage or BuildNDEFMessageEx for more flexibility
func BuildNDEFMessage(text string) ([]byte, error) {
	return BuildTextMessage(text)
}

// calculateNDEFHeader calculates the NDEF TLV header
func calculateNDEFHeader(payload []byte) ([]byte, error) {
	length := len(payload)

	// Short format (length < 255)
	if length < 255 {
		return []byte{0x03, byte(length)}, nil
	}

	// Long format (length >= 255)
	// NFCForum-TS-Type-2-Tag_1.1.pdf Page 9
	if length > 0xFFFF {
		return nil, errors.New("NDEF payload too large")
	}

	header := []byte{0x03, 0xFF}
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.BigEndian, uint16(length)); err != nil {
		return nil, fmt.Errorf("failed to write NDEF length header: %w", err)
	}

	return append(header, buf.Bytes()...), nil
}
