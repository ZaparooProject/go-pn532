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
	"encoding/binary"
	"errors"
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

	header := make([]byte, 4)
	header[0] = 0x03
	header[1] = 0xFF
	binary.BigEndian.PutUint16(header[2:], uint16(length))

	return header, nil
}
