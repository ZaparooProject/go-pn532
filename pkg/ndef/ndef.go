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

package ndef

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// TNF (Type Name Format) values as defined by NFC Forum.
const (
	TNFEmpty          byte = 0x00 // Empty record
	TNFWellKnown      byte = 0x01 // NFC Forum well-known type
	TNFMedia          byte = 0x02 // Media-type (RFC 2046)
	TNFAbsoluteURI    byte = 0x03 // Absolute URI (RFC 3986)
	TNFExternal       byte = 0x04 // NFC Forum external type
	TNFUnknown        byte = 0x05 // Unknown
	TNFUnchanged      byte = 0x06 // Unchanged (for chunked records)
	TNFReserved       byte = 0x07 // Reserved
	tnfMask           byte = 0x07
	flagMB            byte = 0x80
	flagME            byte = 0x40
	flagCF            byte = 0x20
	flagSR            byte = 0x10
	flagIL            byte = 0x08
	shortRecordMaxLen      = 255
)

// Common errors.
var (
	ErrEmptyMessage    = errors.New("ndef: empty message")
	ErrInvalidRecord   = errors.New("ndef: invalid record")
	ErrTruncatedRecord = errors.New("ndef: truncated record data")
	ErrInvalidTNF      = errors.New("ndef: invalid TNF value")
	ErrChunkedRecord   = errors.New("ndef: chunked records not supported")
)

// Record represents a single NDEF record.
type Record struct {
	Type    string
	ID      string
	Payload []byte
	TNF     byte
	mb      bool
	me      bool
}

// MB returns true if this record is the first in a message.
func (r *Record) MB() bool { return r.mb }

// ME returns true if this record is the last in a message.
func (r *Record) ME() bool { return r.me }

// SetMB sets the Message Begin flag.
func (r *Record) SetMB(v bool) { r.mb = v }

// SetME sets the Message End flag.
func (r *Record) SetME(v bool) { r.me = v }

// Message represents an NDEF message containing one or more records.
type Message struct {
	Records []*Record
}

// Marshal serializes the NDEF message to bytes.
func (m *Message) Marshal() ([]byte, error) {
	if len(m.Records) == 0 {
		return nil, ErrEmptyMessage
	}

	var result []byte
	for i, rec := range m.Records {
		rec.mb = (i == 0)
		rec.me = (i == len(m.Records)-1)

		data, err := rec.Marshal()
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", i, err)
		}
		result = append(result, data...)
	}
	return result, nil
}

// Unmarshal parses NDEF message data and returns the number of bytes consumed.
func (m *Message) Unmarshal(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, ErrEmptyMessage
	}

	m.Records = nil
	offset := 0
	seenMB := false
	seenME := false

	for offset < len(data) && !seenME {
		rec := &Record{}
		bytesRead, err := rec.Unmarshal(data[offset:])
		if err != nil {
			return offset, fmt.Errorf("record at offset %d: %w", offset, err)
		}

		if rec.mb {
			if seenMB && len(m.Records) > 0 {
				// New message starting, stop here
				break
			}
			seenMB = true
		}
		seenME = rec.me

		m.Records = append(m.Records, rec)
		offset += bytesRead
	}

	if len(m.Records) == 0 {
		return 0, ErrEmptyMessage
	}

	return offset, nil
}

// Marshal serializes a single NDEF record to bytes.
func (r *Record) Marshal() ([]byte, error) {
	if r.TNF > TNFReserved {
		return nil, ErrInvalidTNF
	}

	typeBytes := []byte(r.Type)
	idBytes := []byte(r.ID)
	payloadLen := len(r.Payload)

	// Calculate flags
	flags := r.TNF & tnfMask
	if r.mb {
		flags |= flagMB
	}
	if r.me {
		flags |= flagME
	}
	if payloadLen <= shortRecordMaxLen {
		flags |= flagSR
	}
	if len(idBytes) > 0 {
		flags |= flagIL
	}

	// Build header
	header := []byte{flags, byte(len(typeBytes))} //nolint:gosec // NDEF type field is max 255 bytes per spec

	// Payload length
	if payloadLen <= shortRecordMaxLen {
		header = append(header, byte(payloadLen))
	} else {
		lenBytes := make([]byte, 4)
		//nolint:gosec // payloadLen is guaranteed non-negative (from len()) and checked > 255
		binary.BigEndian.PutUint32(lenBytes, uint32(payloadLen))
		header = append(header, lenBytes...)
	}

	// ID length (if present)
	if len(idBytes) > 0 {
		header = append(header, byte(len(idBytes))) //nolint:gosec // NDEF ID field is max 255 bytes per spec
	}

	// Assemble record
	result := make([]byte, 0, len(header)+len(typeBytes)+len(idBytes)+payloadLen)
	result = append(result, header...)
	result = append(result, typeBytes...)
	result = append(result, idBytes...)
	result = append(result, r.Payload...)

	return result, nil
}

// Unmarshal parses a single NDEF record and returns the number of bytes consumed.
func (r *Record) Unmarshal(data []byte) (int, error) {
	if len(data) < 3 {
		return 0, ErrTruncatedRecord
	}

	flags := data[0]
	r.TNF = flags & tnfMask
	r.mb = (flags & flagMB) != 0
	r.me = (flags & flagME) != 0
	isShort := (flags & flagSR) != 0
	hasID := (flags & flagIL) != 0
	isChunked := (flags & flagCF) != 0

	if isChunked {
		return 0, ErrChunkedRecord
	}

	if r.TNF > TNFUnchanged {
		return 0, ErrInvalidTNF
	}

	typeLen := int(data[1])
	offset := 2

	// Payload length
	var payloadLen int
	if isShort {
		if offset >= len(data) {
			return 0, ErrTruncatedRecord
		}
		payloadLen = int(data[offset])
		offset++
	} else {
		if offset+4 > len(data) {
			return 0, ErrTruncatedRecord
		}
		payloadLen = int(binary.BigEndian.Uint32(data[offset : offset+4]))
		offset += 4
	}

	// ID length
	var idLen int
	if hasID {
		if offset >= len(data) {
			return 0, ErrTruncatedRecord
		}
		idLen = int(data[offset]) //nolint:gosec // bounds checked at line 234
		offset++
	}

	// Check we have enough data
	totalLen := offset + typeLen + idLen + payloadLen
	if totalLen > len(data) {
		return 0, ErrTruncatedRecord
	}

	// Extract fields
	if typeLen > 0 {
		r.Type = string(data[offset : offset+typeLen])
		offset += typeLen
	}
	if idLen > 0 {
		r.ID = string(data[offset : offset+idLen])
		offset += idLen
	}
	if payloadLen > 0 {
		r.Payload = make([]byte, payloadLen)
		copy(r.Payload, data[offset:offset+payloadLen])
		offset += payloadLen
	}

	return offset, nil
}
