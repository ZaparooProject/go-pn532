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

package tagops

import (
	"context"
	"errors"
	"fmt"

	"github.com/ZaparooProject/go-pn532"
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
	case pn532.TagTypeNTAG:
		return t.writeNTAGBlocks(ctx, startBlock, data)
	case pn532.TagTypeMIFARE:
		return t.writeMIFAREBlocks(ctx, startBlock, data)
	case pn532.TagTypeUnknown, pn532.TagTypeFeliCa, pn532.TagTypeAny:
		return ErrUnsupportedTag
	}
	return ErrUnsupportedTag
}

// WriteNDEF writes an NDEF message to the tag with retry logic for transient errors.
// This provides consistent retry behavior with ReadNDEF, especially important for
// card sliding scenarios where RF communication may be unstable.
func (t *TagOperations) WriteNDEF(ctx context.Context, msg *pn532.NDEFMessage) error {
	if t.tag == nil {
		return ErrNoTag
	}

	const maxRetries = 3

	switch t.tagType {
	case pn532.TagTypeNTAG:
		err := pn532.WriteNDEFWithRetry(ctx, func(ctx context.Context) error {
			return t.ntagInstance.WriteNDEF(ctx, msg)
		}, maxRetries, "NTAG")
		if err != nil {
			return fmt.Errorf("failed to write NDEF to NTAG: %w", err)
		}
		return nil
	case pn532.TagTypeMIFARE:
		err := pn532.WriteNDEFWithRetry(ctx, func(ctx context.Context) error {
			return t.mifareInstance.WriteNDEF(ctx, msg)
		}, maxRetries, "MIFARE")
		if err != nil {
			return fmt.Errorf("failed to write NDEF to MIFARE: %w", err)
		}
		return nil
	case pn532.TagTypeUnknown, pn532.TagTypeFeliCa, pn532.TagTypeAny:
		return ErrUnsupportedTag
	}
	return ErrUnsupportedTag
}

// writeNTAGBlocks writes blocks to NTAG
func (t *TagOperations) writeNTAGBlocks(ctx context.Context, startBlock byte, data []byte) error {
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
	for i := range numPages {
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
		_, err := t.device.SendDataExchange(ctx, cmd)
		if err != nil {
			return fmt.Errorf("failed to write page %d: %w", page, err)
		}
	}

	return nil
}

// writeMIFAREBlocks writes blocks to MIFARE Classic with automatic authentication
func (t *TagOperations) writeMIFAREBlocks(ctx context.Context, startBlock byte, data []byte) error {
	// Authentication is handled automatically by WriteBlockAuto

	// Validate we're not writing to restricted blocks
	if startBlock == 0 {
		return errors.New("cannot write to manufacturer block (0)")
	}

	// Calculate how many blocks we need to write
	numBlocks := (len(data) + 15) / 16 // Round up to nearest block

	// Write block by block
	for i := range numBlocks {
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
		err := t.mifareInstance.WriteBlockAuto(ctx, block, blockData)
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

	if t.tagType == pn532.TagTypeNTAG {
		blockSize = 4
	}

	zeros := make([]byte, numBlocks*blockSize)
	return t.WriteBlocks(ctx, startBlock, zeros)
}

// ErrLockNotSupported indicates the tag type does not support locking.
var ErrLockNotSupported = errors.New("locking not supported for this tag type")

// MakeReadOnly permanently locks the tag, preventing any future writes.
// This is IRREVERSIBLE for NTAG tags.
// Returns ErrLockNotSupported for tag types that don't support locking (e.g., MIFARE).
func (t *TagOperations) MakeReadOnly(ctx context.Context) error {
	if t.tag == nil {
		return ErrNoTag
	}

	switch t.tagType {
	case pn532.TagTypeNTAG:
		if err := t.ntagInstance.MakeReadOnly(ctx); err != nil {
			return fmt.Errorf("failed to lock NTAG: %w", err)
		}
		return nil
	case pn532.TagTypeMIFARE:
		return ErrLockNotSupported
	case pn532.TagTypeUnknown, pn532.TagTypeFeliCa, pn532.TagTypeAny:
		return ErrUnsupportedTag
	}
	return ErrUnsupportedTag
}

// Format prepares the tag for NDEF use
func (t *TagOperations) Format(ctx context.Context) error {
	if t.tag == nil {
		return ErrNoTag
	}

	// Create an empty NDEF message for formatting
	emptyNDEF := &pn532.NDEFMessage{
		Records: []pn532.NDEFRecord{},
	}

	switch t.tagType {
	case pn532.TagTypeNTAG:
		// NTAG tags don't need explicit formatting
		// Just write an empty NDEF message
		return t.WriteNDEF(ctx, emptyNDEF)

	case pn532.TagTypeMIFARE:
		// For MIFARE, formatting means writing an empty NDEF message
		// The WriteNDEF method handles formatting if needed
		return t.WriteNDEF(ctx, emptyNDEF)

	case pn532.TagTypeUnknown, pn532.TagTypeFeliCa, pn532.TagTypeAny:
		return ErrUnsupportedTag
	}
	return ErrUnsupportedTag
}
