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

// ReadBlocks reads a range of blocks from the tag using the optimal method.
// For NTAG tags, it uses fast read when possible.
// For MIFARE tags, it handles authentication transparently.
// The startBlock and endBlock are inclusive.
func (t *TagOperations) ReadBlocks(ctx context.Context, startBlock, endBlock byte) ([]byte, error) {
	if t.tag == nil {
		return nil, ErrNoTag
	}

	switch t.tagType {
	case pn532.TagTypeNTAG:
		return t.readNTAGBlocks(ctx, startBlock, endBlock)
	case pn532.TagTypeMIFARE:
		return t.readMIFAREBlocks(ctx, startBlock, endBlock)
	case pn532.TagTypeUnknown, pn532.TagTypeFeliCa, pn532.TagTypeAny:
		return nil, ErrUnsupportedTag
	}
	return nil, ErrUnsupportedTag
}

// ReadAll reads all available data from the tag
func (t *TagOperations) ReadAll(ctx context.Context) ([]byte, error) {
	if t.tag == nil {
		return nil, ErrNoTag
	}

	switch t.tagType {
	case pn532.TagTypeNTAG:
		// Read from page 0 to last page
		return t.readNTAGBlocks(ctx, 0, byte(t.totalPages-1)) //nolint:gosec // totalPages <= 231 (NTAG216 max)
	case pn532.TagTypeMIFARE:
		if t.mifareInstance.IsMIFARE4K() {
			// MIFARE Classic 4K has 255 blocks (0-254)
			return t.readMIFAREBlocks(ctx, 0, 254)
		}
		// MIFARE Classic 1K has 64 blocks (0-63)
		return t.readMIFAREBlocks(ctx, 0, 63)
	case pn532.TagTypeUnknown, pn532.TagTypeFeliCa, pn532.TagTypeAny:
		return nil, ErrUnsupportedTag
	}
	return nil, ErrUnsupportedTag
}

// ReadNDEF reads and parses NDEF data from the tag
func (t *TagOperations) ReadNDEF(ctx context.Context) (*pn532.NDEFMessage, error) {
	if t.tag == nil {
		return nil, ErrNoTag
	}

	switch t.tagType {
	case pn532.TagTypeNTAG:
		ndefMsg, err := t.ntagInstance.ReadNDEFWithRetry(ctx)
		if err != nil {
			if errors.Is(err, pn532.ErrNoNDEF) {
				// No NDEF present is a valid state - return empty message
				return &pn532.NDEFMessage{}, nil
			}
			return nil, fmt.Errorf("failed to read NDEF from NTAG: %w", err)
		}
		return ndefMsg, nil
	case pn532.TagTypeMIFARE:
		// Unauthenticated MIFARE tags can't be read - return empty NDEF
		if !t.mifareInstance.IsAuthenticated() {
			return &pn532.NDEFMessage{}, nil
		}
		ndefMsg, err := t.mifareInstance.ReadNDEFWithRetry(ctx)
		if err != nil {
			if errors.Is(err, pn532.ErrNoNDEF) {
				// No NDEF present is a valid state - return empty message
				return &pn532.NDEFMessage{}, nil
			}
			return nil, fmt.Errorf("failed to read NDEF from MIFARE: %w", err)
		}
		return ndefMsg, nil
	case pn532.TagTypeUnknown, pn532.TagTypeFeliCa, pn532.TagTypeAny:
		return nil, ErrUnsupportedTag
	}
	return nil, ErrUnsupportedTag
}

// readNTAGBlocks reads blocks from NTAG using fast read when possible
func (t *TagOperations) readNTAGBlocks(ctx context.Context, startBlock, endBlock byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	startPage, endPage := t.validatePageRange(startBlock, endBlock)
	expectedBytes := (int(endPage - startPage + 1)) * 4

	var result []byte
	currentPage := startPage

	for currentPage <= endPage {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		chunkData, nextPage, err := t.readPageChunk(ctx, currentPage, endPage)
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
		endPage = byte(t.totalPages - 1) //nolint:gosec // totalPages <= 231 (NTAG216 max)
	}
	return startBlock, endPage
}

func (t *TagOperations) readPageChunk(
	ctx context.Context, currentPage, endPage byte,
) (data []byte, nextPage byte, err error) {
	chunkEnd := currentPage + 15
	if chunkEnd > endPage {
		chunkEnd = endPage
	}

	if data, nextPage := t.tryFastRead(ctx, currentPage, chunkEnd); data != nil {
		return data, nextPage, nil
	}

	return t.readPagesIndividually(ctx, currentPage, chunkEnd)
}

func (t *TagOperations) tryFastRead(ctx context.Context, currentPage, chunkEnd byte) (data []byte, nextPage byte) {
	if chunkEnd <= currentPage {
		return nil, 0
	}

	cmd := []byte{0x3A, currentPage, chunkEnd}
	data, err := t.device.SendRawCommand(ctx, cmd)

	// Re-select target after SendRawCommand to restore PN532 internal state
	// SendRawCommand uses InCommunicateThru which doesn't maintain target selection
	if selectErr := t.device.InSelect(ctx); selectErr != nil {
		pn532.Debugln("tagops tryFastRead: InSelect failed (non-fatal):", selectErr)
	}

	if err == nil {
		return data, chunkEnd + 1
	}

	return nil, 0
}

func (t *TagOperations) readPagesIndividually(
	ctx context.Context, currentPage, chunkEnd byte,
) (result []byte, nextPage byte, err error) {
	for page := currentPage; page <= chunkEnd; page++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		pageData, err := t.device.SendDataExchange(ctx, []byte{0x30, page})
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
	var result []byte

	for block := startBlock; block <= endBlock; block++ {
		// Skip trailer blocks (every 4th block in each sector)
		if (block+1)%4 == 0 && block != 0 {
			continue
		}

		// ReadBlockAuto handles authentication automatically
		data, err := t.mifareInstance.ReadBlockAuto(ctx, block)
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
	case pn532.TagTypeNTAG:
		totalBytes = t.totalPages * 4 // 4 bytes per page
		// Usable bytes exclude UID, lock bytes, etc. (typically first 4 pages)
		usableBytes = (t.totalPages - 4) * 4
		return totalBytes, usableBytes, nil

	case pn532.TagTypeMIFARE:
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

	case pn532.TagTypeUnknown, pn532.TagTypeFeliCa, pn532.TagTypeAny:
		return 0, 0, ErrUnsupportedTag
	}
	return 0, 0, ErrUnsupportedTag
}

// ProbeActualMemorySize probes the tag to find its actual memory size.
// This is useful for clone tags that may report incorrect capacity in their CC.
// The claimedBytes parameter (from CC) helps set a reasonable upper bound.
// Returns the last readable page and actual user memory in bytes.
func (t *TagOperations) ProbeActualMemorySize(
	ctx context.Context, claimedBytes int,
) (lastPage uint8, userMemory int, err error) {
	if t.tag == nil {
		return 0, 0, ErrNoTag
	}

	if t.tagType != pn532.TagTypeNTAG || t.ntagInstance == nil {
		return 0, 0, ErrUnsupportedTag
	}

	lastPage, userMemory = t.ntagInstance.ProbeActualMemorySize(ctx, claimedBytes)
	return lastPage, userMemory, nil
}

// ReadCapabilityContainer reads the capability container (CC) from the tag.
// For NTAG, this is page 3. Returns the raw CC bytes.
func (t *TagOperations) ReadCapabilityContainer(ctx context.Context) ([]byte, error) {
	if t.tag == nil {
		return nil, ErrNoTag
	}

	if t.tagType != pn532.TagTypeNTAG || t.ntagInstance == nil {
		return nil, ErrUnsupportedTag
	}

	data, err := t.ntagInstance.ReadCapabilityContainer(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read capability container: %w", err)
	}
	return data, nil
}

// GetClaimedSizeFromCC returns the claimed memory size from CC data.
func GetClaimedSizeFromCC(ccData []byte) int {
	return pn532.GetClaimedSizeFromCC(ccData)
}
