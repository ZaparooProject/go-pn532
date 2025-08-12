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

package tagops

import (
	"context"
	"errors"
	"fmt"

	"github.com/ZaparooProject/go-pn532"
	"github.com/hsanjuan/go-ndef"
)

// WriteBlocks writes data to the tag starting at the specified block.
// For NTAG tags, it writes directly.
// For MIFARE tags, it handles authentication transparently.
// The data will be written in chunks appropriate to the tag type.
func (t *TagOperations) WriteBlocks(ctx context.Context, startBlock byte, data []byte) error {
	if t.tag == nil {
		return ErrNoTag
	}

	switch t.tagType {
	case TagTypeNTAG:
		return t.writeNTAGBlocks(ctx, startBlock, data)
	case TagTypeMIFARE:
		return t.writeMIFAREBlocks(ctx, startBlock, data)
	case TagTypeUnknown:
		return ErrUnsupportedTag
	default:
		return ErrUnsupportedTag
	}
}

// WriteNDEF writes an NDEF message to the tag
func (t *TagOperations) WriteNDEF(_ context.Context, msg *ndef.Message) error {
	if t.tag == nil {
		return ErrNoTag
	}

	switch t.tagType {
	case TagTypeNTAG:
		pn532Msg := convertToPN532Message(msg)
		if err := t.ntagInstance.WriteNDEF(pn532Msg); err != nil {
			return fmt.Errorf("failed to write NDEF to NTAG: %w", err)
		}
		return nil
	case TagTypeMIFARE:
		pn532Msg := convertToPN532Message(msg)
		if err := t.mifareInstance.WriteNDEF(pn532Msg); err != nil {
			return fmt.Errorf("failed to write NDEF to MIFARE: %w", err)
		}
		return nil
	case TagTypeUnknown:
		return ErrUnsupportedTag
	default:
		return ErrUnsupportedTag
	}
}

// writeNTAGBlocks writes blocks to NTAG
func (t *TagOperations) writeNTAGBlocks(_ context.Context, startBlock byte, data []byte) error {
	// Convert block to page
	startPage := startBlock

	// Validate we're not writing to restricted pages
	if startPage < 4 {
		return errors.New("cannot write to restricted pages (0-3)")
	}

	// Calculate how many pages we need to write
	numPages := (len(data) + 3) / 4 // Round up to nearest page

	// Validate we don't exceed tag capacity
	if int(startPage)+numPages > t.totalPages {
		return errors.New("write would exceed tag capacity")
	}

	// Write page by page (NTAG doesn't support multi-page write)
	for i := 0; i < numPages; i++ {
		page := startPage + byte(i)

		// Get 4 bytes for this page (pad with zeros if necessary)
		pageData := make([]byte, 4)
		dataStart := i * 4
		dataEnd := dataStart + 4
		if dataEnd > len(data) {
			dataEnd = len(data)
		}
		copy(pageData, data[dataStart:dataEnd])

		// Write command: 0xA2 page data[4]
		cmd := append([]byte{0xA2, page}, pageData...)
		_, err := t.device.SendDataExchange(cmd)
		if err != nil {
			return fmt.Errorf("failed to write page %d: %w", page, err)
		}
	}

	return nil
}

// writeMIFAREBlocks writes blocks to MIFARE Classic with automatic authentication
func (t *TagOperations) writeMIFAREBlocks(_ context.Context, startBlock byte, data []byte) error {
	// Authentication is handled automatically by WriteBlockAuto

	// Ensure key provider is set

	// Validate we're not writing to restricted blocks
	if startBlock == 0 {
		return errors.New("cannot write to manufacturer block (0)")
	}

	// Calculate how many blocks we need to write
	numBlocks := (len(data) + 15) / 16 // Round up to nearest block

	// Write block by block
	for i := 0; i < numBlocks; i++ {
		block := startBlock + byte(i)

		// Skip trailer blocks (every 4th block in each sector)
		if (block+1)%4 == 0 && block != 0 {
			continue
		}

		// Get 16 bytes for this block (pad with zeros if necessary)
		blockData := make([]byte, 16)
		dataStart := i * 16
		dataEnd := dataStart + 16
		if dataEnd > len(data) {
			dataEnd = len(data)
		}
		copy(blockData, data[dataStart:dataEnd])

		// WriteBlockAuto handles authentication automatically
		err := t.mifareInstance.WriteBlockAuto(block, blockData)
		if err != nil {
			return fmt.Errorf("failed to write block %d: %w", block, err)
		}
	}

	return nil
}

// EraseBlocks writes zeros to the specified block range
func (t *TagOperations) EraseBlocks(ctx context.Context, startBlock, endBlock byte) error {
	numBlocks := int(endBlock - startBlock + 1)
	blockSize := 16 // MIFARE block size, NTAG pages are smaller but we'll use max

	if t.tagType == TagTypeNTAG {
		blockSize = 4
	}

	zeros := make([]byte, numBlocks*blockSize)
	return t.WriteBlocks(ctx, startBlock, zeros)
}

// Format prepares the tag for NDEF use
func (t *TagOperations) Format(ctx context.Context) error {
	if t.tag == nil {
		return ErrNoTag
	}

	switch t.tagType {
	case TagTypeNTAG:
		// NTAG tags don't need explicit formatting
		// Just write an empty NDEF message
		emptyNDEF := ndef.NewMessage(ndef.Empty, "", "", nil)
		return t.WriteNDEF(ctx, emptyNDEF)

	case TagTypeMIFARE:
		// For MIFARE, formatting means writing an empty NDEF message
		// The WriteNDEF method handles formatting if needed
		emptyNDEF := ndef.NewMessage(ndef.Empty, "", "", nil)
		return t.WriteNDEF(ctx, emptyNDEF)

	case TagTypeUnknown:
		return ErrUnsupportedTag

	default:
		return ErrUnsupportedTag
	}
}

// convertToPN532Message converts from ndef.Message to pn532.NDEFMessage
func convertToPN532Message(ndefMsg *ndef.Message) *pn532.NDEFMessage {
	if ndefMsg == nil {
		return nil
	}

	pn532Msg := &pn532.NDEFMessage{
		Records: make([]pn532.NDEFRecord, 0, len(ndefMsg.Records)),
	}

	for _, rec := range ndefMsg.Records {
		pn532Rec := convertRecord(*rec)
		pn532Msg.Records = append(pn532Msg.Records, pn532Rec)
	}

	return pn532Msg
}

// convertRecord converts a single ndef.Record to pn532.NDEFRecord
func convertRecord(rec ndef.Record) pn532.NDEFRecord {
	pn532Rec := pn532.NDEFRecord{}

	// Determine type from TNF and type field
	tnf := rec.TNF()
	typeField := rec.Type()

	if tnf == ndef.NFCForumWellKnownType {
		if typeField != "" && typeField[0] == 'T' {
			convertTextRecord(rec, &pn532Rec)
		} else if typeField != "" && typeField[0] == 'U' {
			convertURIRecord(rec, &pn532Rec)
		}
	}

	// Always store raw payload
	setRawPayload(rec, &pn532Rec)

	return pn532Rec
}

// convertTextRecord handles text record conversion
func convertTextRecord(rec ndef.Record, pn532Rec *pn532.NDEFRecord) {
	pn532Rec.Type = pn532.NDEFTypeText
	payload, err := rec.Payload()
	if err == nil && payload != nil {
		extractTextFromPayload(payload.Marshal(), pn532Rec)
	}
}

// extractTextFromPayload extracts text content from text record payload
func extractTextFromPayload(payloadBytes []byte, pn532Rec *pn532.NDEFRecord) {
	if len(payloadBytes) > 3 {
		// Skip encoding and language code
		langLen := int(payloadBytes[0] & 0x3F)
		if len(payloadBytes) > langLen+1 {
			pn532Rec.Text = string(payloadBytes[langLen+1:])
		}
	}
}

// convertURIRecord handles URI record conversion
func convertURIRecord(rec ndef.Record, pn532Rec *pn532.NDEFRecord) {
	pn532Rec.Type = pn532.NDEFTypeURI
	payload, err := rec.Payload()
	if err == nil && payload != nil {
		pn532Rec.URI = string(payload.Marshal())
	}
}

// setRawPayload sets the raw payload data
func setRawPayload(rec ndef.Record, pn532Rec *pn532.NDEFRecord) {
	payload, err := rec.Payload()
	if err == nil && payload != nil {
		pn532Rec.Payload = payload.Marshal()
	}
}
