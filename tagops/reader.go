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
	"fmt"

	"github.com/ZaparooProject/go-pn532"
	"github.com/hsanjuan/go-ndef"
)

// ReadBlocks reads a range of blocks from the tag using the optimal method.
// For NTAG tags, it uses fast read when possible.
// For MIFARE tags, it handles authentication transparently.
// The startBlock and endBlock are inclusive.
func (t *TagOperations) ReadBlocks(ctx context.Context, startBlock, endBlock byte) ([]byte, error) {
	if t.tag == nil {
		return nil, ErrNoTag
	}

	switch t.tagType {
	case TagTypeNTAG:
		return t.readNTAGBlocks(ctx, startBlock, endBlock)
	case TagTypeMIFARE:
		return t.readMIFAREBlocks(ctx, startBlock, endBlock)
	case TagTypeUnknown:
		return nil, ErrUnsupportedTag
	default:
		return nil, ErrUnsupportedTag
	}
}

// ReadAll reads all available data from the tag
func (t *TagOperations) ReadAll(ctx context.Context) ([]byte, error) {
	if t.tag == nil {
		return nil, ErrNoTag
	}

	switch t.tagType {
	case TagTypeNTAG:
		// Read from page 0 to last page
		return t.readNTAGBlocks(ctx, 0, byte(t.totalPages-1))
	case TagTypeMIFARE:
		if t.mifareInstance.IsMIFARE4K() {
			// MIFARE Classic 4K has 255 blocks (0-254)
			return t.readMIFAREBlocks(ctx, 0, 254)
		}
		// MIFARE Classic 1K has 64 blocks (0-63)
		return t.readMIFAREBlocks(ctx, 0, 63)
	case TagTypeUnknown:
		return nil, ErrUnsupportedTag
	default:
		return nil, ErrUnsupportedTag
	}
}

// ReadNDEF reads and parses NDEF data from the tag
func (t *TagOperations) ReadNDEF(_ context.Context) (*ndef.Message, error) {
	if t.tag == nil {
		return nil, ErrNoTag
	}

	switch t.tagType {
	case TagTypeNTAG:
		ndefMsg, err := t.ntagInstance.ReadNDEF()
		if err != nil {
			return nil, fmt.Errorf("failed to read NDEF from NTAG: %w", err)
		}
		// Convert from pn532.NDEFMessage to ndef.Message
		return convertNDEFMessage(ndefMsg), nil
	case TagTypeMIFARE:
		ndefMsg, err := t.mifareInstance.ReadNDEF()
		if err != nil {
			return nil, fmt.Errorf("failed to read NDEF from MIFARE: %w", err)
		}
		// Convert from pn532.NDEFMessage to ndef.Message
		return convertNDEFMessage(ndefMsg), nil
	case TagTypeUnknown:
		return nil, ErrUnsupportedTag
	default:
		return nil, ErrUnsupportedTag
	}
}

// readNTAGBlocks reads blocks from NTAG using fast read when possible
func (t *TagOperations) readNTAGBlocks(ctx context.Context, startBlock, endBlock byte) ([]byte, error) {
	_ = ctx // Reserved for future timeout/cancellation support

	startPage, endPage := t.validatePageRange(startBlock, endBlock)
	expectedBytes := (int(endPage - startPage + 1)) * 4

	var result []byte
	currentPage := startPage

	for currentPage <= endPage {
		chunkData, nextPage, err := t.readPageChunk(currentPage, endPage)
		if err != nil {
			return nil, err
		}
		result = append(result, chunkData...)
		currentPage = nextPage
	}

	return t.trimToExpectedSize(result, expectedBytes), nil
}

func (t *TagOperations) validatePageRange(startBlock, endBlock byte) (validStart, validEnd byte) {
	endPage := endBlock
	if int(endPage) >= t.totalPages {
		endPage = byte(t.totalPages - 1)
	}
	return startBlock, endPage
}

func (t *TagOperations) readPageChunk(currentPage, endPage byte) (data []byte, nextPage byte, err error) {
	chunkEnd := currentPage + 15
	if chunkEnd > endPage {
		chunkEnd = endPage
	}

	if data, nextPage := t.tryFastRead(currentPage, chunkEnd); data != nil {
		return data, nextPage, nil
	}

	return t.readPagesIndividually(currentPage, chunkEnd)
}

func (t *TagOperations) tryFastRead(currentPage, chunkEnd byte) (data []byte, nextPage byte) {
	if chunkEnd <= currentPage {
		return nil, 0
	}

	cmd := []byte{0x3A, currentPage, chunkEnd}
	if data, err := t.device.SendRawCommand(cmd); err == nil {
		return data, chunkEnd + 1
	}

	return nil, 0
}

func (t *TagOperations) readPagesIndividually(currentPage, chunkEnd byte) (result []byte, nextPage byte, err error) {
	for page := currentPage; page <= chunkEnd; page++ {
		pageData, err := t.device.SendDataExchange([]byte{0x30, page})
		if err != nil {
			return nil, 0, fmt.Errorf("failed to read page %d: %w", page, err)
		}

		if len(pageData) >= 4 {
			result = append(result, pageData[:4]...)
		} else {
			result = append(result, pageData...)
		}
	}

	return result, chunkEnd + 1, nil
}

func (*TagOperations) trimToExpectedSize(result []byte, expectedBytes int) []byte {
	if len(result) > expectedBytes {
		return result[:expectedBytes]
	}
	return result
}

// readMIFAREBlocks reads blocks from MIFARE Classic with automatic authentication
func (t *TagOperations) readMIFAREBlocks(ctx context.Context, startBlock, endBlock byte) ([]byte, error) {
	_ = ctx // Reserved for future timeout/cancellation support
	// Authentication is handled automatically by ReadBlockAuto

	// Ensure key provider is set

	var result []byte

	for block := startBlock; block <= endBlock; block++ {
		// Skip trailer blocks (every 4th block in each sector)
		if (block+1)%4 == 0 && block != 0 {
			continue
		}

		// ReadBlockAuto handles authentication automatically
		data, err := t.mifareInstance.ReadBlockAuto(block)
		if err != nil {
			return nil, fmt.Errorf("failed to read block %d: %w", block, err)
		}

		result = append(result, data...)
	}

	return result, nil
}

// GetCapacityInfo returns information about the tag's storage capacity
func (t *TagOperations) GetCapacityInfo() (totalBytes, usableBytes int, err error) {
	if t.tag == nil {
		return 0, 0, ErrNoTag
	}

	switch t.tagType {
	case TagTypeNTAG:
		totalBytes = t.totalPages * 4 // 4 bytes per page
		// Usable bytes exclude UID, lock bytes, etc. (typically first 4 pages)
		usableBytes = (t.totalPages - 4) * 4
		return totalBytes, usableBytes, nil

	case TagTypeMIFARE:
		if t.mifareInstance.IsMIFARE4K() {
			// MIFARE Classic 4K: 40 sectors, 255 blocks total
			totalBytes = 4096
			// First 32 sectors: 3 data blocks each = 32 * 3 * 16 = 1536 bytes
			// Last 8 sectors: 15 data blocks each = 8 * 15 * 16 = 1920 bytes
			// Total usable: 1536 + 1920 = 3456 bytes
			usableBytes = 3456
		} else {
			// MIFARE Classic 1K: 16 sectors, 64 blocks total
			totalBytes = 1024
			// Each sector has 3 data blocks (16 bytes each) + 1 trailer
			// 16 sectors * 3 blocks * 16 bytes = 768 usable bytes
			usableBytes = 768
		}
		return totalBytes, usableBytes, nil

	case TagTypeUnknown:
		return 0, 0, ErrUnsupportedTag
	default:
		return 0, 0, ErrUnsupportedTag
	}
}

// convertNDEFMessage converts from pn532.NDEFMessage to ndef.Message
func convertNDEFMessage(pn532Msg *pn532.NDEFMessage) *ndef.Message {
	if pn532Msg == nil {
		return nil
	}

	records := make([]*ndef.Record, 0, len(pn532Msg.Records))

	for _, pn532Rec := range pn532Msg.Records {
		var rec *ndef.Record

		// Convert based on record type
		switch {
		case pn532Rec.Type == pn532.NDEFTypeText && pn532Rec.Text != "":
			// Create text record
			rec = ndef.NewTextRecord(pn532Rec.Text, "en")
		case pn532Rec.Type == pn532.NDEFTypeURI && pn532Rec.URI != "":
			// Create URI record
			rec = ndef.NewURIRecord(pn532Rec.URI)
		case len(pn532Rec.Payload) > 0:
			// Create a generic record with raw payload
			// For generic records, we'll create a simple text record
			rec = ndef.NewTextRecord(string(pn532Rec.Payload), "en")
		}

		if rec != nil {
			records = append(records, rec)
		}
	}

	return ndef.NewMessageFromRecords(records...)
}
