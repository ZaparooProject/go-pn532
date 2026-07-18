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
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"
)

// NTAG commands
const (
	ntagCmdRead       = 0x30
	ntagCmdWrite      = 0xA2
	ntagCmdFastRead   = 0x3A
	ntagCmdPwdAuth    = 0x1B
	ntagCmdGetVersion = 0x60
)

// NTAG memory structure
const (
	ntagBlockSize    = 4  // 4 bytes per block
	ntagUserMemStart = 4  // User memory starts at block 4
	ntagMaxBlocks    = 64 // Maximum blocks to read

	// NTAG variant-specific constants
	ntag213TotalPages = 45 // 180 bytes total
	ntag213UserPages  = 36 // 144 bytes user memory
	ntag213UserStart  = 4  // User memory starts at page 4
	ntag213UserEnd    = 39 // User memory ends at page 39

	ntag215TotalPages = 135 // 540 bytes total
	ntag215UserPages  = 126 // 504 bytes user memory
	ntag215UserStart  = 4   // User memory starts at page 4
	ntag215UserEnd    = 129 // User memory ends at page 129

	ntag216TotalPages = 231 // 924 bytes total
	ntag216UserPages  = 222 // 888 bytes user memory
	ntag216UserStart  = 4   // User memory starts at page 4
	ntag216UserEnd    = 225 // User memory ends at page 225

	// System memory layout (common to all NTAG21x)
	ntagPageSerial0    = 0 // UID bytes 0-2 + BCC0
	ntagPageSerial1    = 1 // UID bytes 3-6
	ntagPageSerial2    = 2 // BCC1 + Internal + Lock bytes
	ntagPageCC         = 3 // Capability Container
	ntagPageStaticLock = 2 // Static lock bytes (page 2, bytes 2-3)

	// Configuration pages by NTAG type
	ntag213DynLock = 0x28 // Dynamic lock bytes (NTAG213)
	ntag213Cfg0    = 0x29 // Config 0 (NTAG213)
	ntag213Cfg1    = 0x2A // Config 1 (NTAG213)
	ntag213Pwd     = 0x2B // Password (NTAG213)
	ntag213Pack    = 0x2C // PACK (NTAG213)

	ntag215DynLock = 0x82 // Dynamic lock bytes (NTAG215)
	ntag215Cfg0    = 0x83 // Config 0 (NTAG215)
	ntag215Cfg1    = 0x84 // Config 1 (NTAG215)
	ntag215Pwd     = 0x85 // Password (NTAG215)
	ntag215Pack    = 0x86 // PACK (NTAG215)

	ntag216DynLock = 0xE2 // Dynamic lock bytes (NTAG216)
	ntag216Cfg0    = 0xE3 // Config 0 (NTAG216)
	ntag216Cfg1    = 0xE4 // Config 1 (NTAG216)
	ntag216Pwd     = 0xE5 // Password (NTAG216)
	ntag216Pack    = 0xE6 // PACK (NTAG216)

	// Lock bytes positions in page 2
	// TODO: Use these when implementing lock/unlock features
	// ntagLockBytesOffset = 2 // Lock bytes start at byte 2 of page 2
	// ntagLockBytesLen    = 2 // 2 lock bytes

	// Dynamic lock bytes (variant-specific)
	// TODO: Use these when implementing lock/unlock features
	// ntag213DynLockPage = 40  // Dynamic lock bytes page
	// ntag215DynLockPage = 130 // Dynamic lock bytes page
	// ntag216DynLockPage = 226 // Dynamic lock bytes page

	// Configuration pages (variant-specific)
	ntag213ConfigPage = 41  // Configuration page start
	ntag215ConfigPage = 131 // Configuration page start
	ntag216ConfigPage = 227 // Configuration page start

	// Password and PACK pages (variant-specific)
	ntag213PwdPage  = 43  // Password page
	ntag213PackPage = 44  // PACK page
	ntag215PwdPage  = 133 // Password page
	ntag215PackPage = 134 // PACK page
	ntag216PwdPage  = 229 // Password page
	ntag216PackPage = 230 // PACK page
)

// NTAGType represents different NTAG variants
type NTAGType uint8

const (
	// NTAGTypeUnknown represents an unknown NTAG type.
	NTAGTypeUnknown NTAGType = iota
	// NTAGType213 represents an NTAG213 chip.
	NTAGType213
	// NTAGType215 represents an NTAG215 chip.
	NTAGType215
	// NTAGType216 represents an NTAG216 chip.
	NTAGType216
)

// NTAGVersion holds the version information from GET_VERSION command
type NTAGVersion struct {
	FixedHeader    uint8 // Should be 0x00
	VendorID       uint8 // 0x04 = NXP Semiconductors
	ProductType    uint8 // 0x04 = NTAG
	ProductSubtype uint8 // 0x02 = 50 pF
	MajorVersion   uint8 // Major product version
	MinorVersion   uint8 // Minor product version
	StorageSize    uint8 // Storage size (encoded)
	ProtocolType   uint8 // 0x03 = ISO/IEC 14443-3
}

// NTAGTag represents an NTAG21X tag
type NTAGTag struct {
	fastReadSupported   *bool
	capabilityContainer []byte // cached CC from page 3 (set during DetectType)
	userDataPage        []byte // cached page 4 (first user data page, set during DetectType)
	BaseTag
	tagType NTAGType
	hasNDEF bool // true if tag has valid NDEF CC (magic byte 0xE1)
}

// AccessControlConfig holds the access control settings for NTAG tags
type AccessControlConfig struct {
	Protection       bool  // false = write protection, true = read/write protection
	ConfigLock       bool  // lock configuration pages
	AuthFailureLimit uint8 // limit failed authentication attempts (0 = disabled, 1-7 = limit)
}

// NewNTAGTag creates a new NTAG tag instance
func NewNTAGTag(device *Device, uid []byte, sak byte) *NTAGTag {
	return &NTAGTag{
		BaseTag: BaseTag{
			tagType: TagTypeNTAG,
			uid:     uid,
			device:  device,
			sak:     sak,
		},
		hasNDEF: true, // Assume NDEF capability; Init will set false if no valid CC
	}
}

// HasNDEF returns true if the tag has a valid NDEF capability container.
// Non-NDEF tags (Amiibo, Lego Dimensions, etc.) return false but are still
// valid for UID reading and raw data access via ReadBlock/ReadAll.
func (t *NTAGTag) HasNDEF() bool {
	return t.hasNDEF
}

// ReadBlock reads a block from the NTAG tag.
func (t *NTAGTag) ReadBlock(ctx context.Context, block uint8) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if t.requiresRawType2Commands() {
		data, err := t.readRawType2Pages(ctx, block, ntagBlockSize)
		if err != nil {
			return nil, fmt.Errorf("%w (block %d): %w", ErrTagReadFailed, block, err)
		}
		return data, nil
	}

	data, err := t.device.SendDataExchangeWithRetry(ctx, []byte{ntagCmdRead, block})
	if err != nil {
		// If we get authentication error 14, try InCommunicateThru as fallback for clone devices
		if IsPN532AuthenticationError(err) {
			Debugf("NTAG ReadBlock: page %d auth error, trying InCommunicateThru fallback", block)
			return t.readBlockCommunicateThru(ctx, block)
		}
		return nil, fmt.Errorf("%w (block %d): %w", ErrTagReadFailed, block, err)
	}

	Debugf("NTAG ReadBlock: page %d raw response = [% 02X] (len=%d)", block, data, len(data))

	// NTAG returns 16 bytes (4 blocks) on read
	if len(data) < ntagBlockSize {
		return nil, fmt.Errorf("%w: invalid response length %d (expected at least %d)",
			ErrTagReadFailed, len(data), ntagBlockSize)
	}

	// Return only the requested block
	return data[:ntagBlockSize], nil
}

// WriteBlock writes a block to the NTAG tag
func (t *NTAGTag) WriteBlock(ctx context.Context, block uint8, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(data) != ntagBlockSize {
		return fmt.Errorf("invalid block size: expected %d, got %d", ntagBlockSize, len(data))
	}

	// Validate write boundary to catch counterfeit tags
	if err := t.validateWriteBoundary(ctx, block); err != nil {
		return err
	}

	cmd := make([]byte, 0, 2+len(data))
	cmd = append(cmd, ntagCmdWrite, block)
	cmd = append(cmd, data...)
	if t.requiresRawType2Commands() {
		response, err := t.sendRawType2Command(ctx, cmd)
		if err != nil {
			// Some raw transports surface the tag's four-bit write ACK as PN532
			// framing status 0x05. Treat it as ambiguous and require exact readback.
			if isRawType2WriteFramingError(err) {
				return t.verifyRawType2WriteAfterFramingError(ctx, block, data, err)
			}
			return fmt.Errorf("%w (block %d): %w", ErrTagWriteFailed, block, err)
		}
		if len(response) == 0 {
			return fmt.Errorf("%w (block %d): missing Type 2 ACK", ErrTagWriteFailed, block)
		}
		if response[0]&0x0F != 0x0A {
			return fmt.Errorf("%w (block %d): Type 2 NAK 0x%02X", ErrTagWriteFailed, block, response[0])
		}
		return nil
	}

	_, err := t.device.SendDataExchangeWithRetry(ctx, cmd)
	if err != nil {
		return fmt.Errorf("%w (block %d): %w", ErrTagWriteFailed, block, err)
	}

	return nil
}

// ReadNDEFWithRetry reads NDEF data with retry logic to handle intermittent empty data issues
// This addresses the "empty valid tag" problem where tags are detected but return no data
func (t *NTAGTag) ReadNDEFWithRetry(ctx context.Context) (*NDEFMessage, error) {
	return readNDEFWithRetry(func() (*NDEFMessage, error) {
		return t.ReadNDEF(ctx)
	}, isRetryableError, "NDEF")
}

// isRetryableError determines if an error is worth retrying
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Never retry on transport lockup - device needs hard reset
	if errors.Is(err, ErrNoACK) {
		return false
	}

	// Use the centralized retry logic from errors.go
	// This handles timeouts, read failures, and communication errors including data exchange error 14
	return IsRetryable(err) ||
		errors.Is(err, ErrTagReadFailed)
}

// ReadNDEF reads NDEF data from the NTAG tag using FastRead for optimal performance
//
//nolint:gocognit,revive // Complexity from necessary error handling and NDEF capability check
func (t *NTAGTag) ReadNDEF(ctx context.Context) (*NDEFMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Non-NDEF tags (Amiibo, Lego Dimensions, etc.) don't have valid NDEF CC
	// Return empty message immediately - the tag is still valid for UID/raw data
	if !t.hasNDEF {
		Debugf("NTAG ReadNDEF: tag has no NDEF CC, returning empty message")
		return &NDEFMessage{}, nil
	}

	// Ensure target is selected before reading on standard transports. Raw Type 2
	// transports preserve selection themselves and may not return an InSelect response.
	if !t.requiresRawType2Commands() {
		if err := t.device.InSelect(ctx); err != nil {
			Debugln("NTAG ReadNDEF: InSelect failed, continuing anyway:", err)
		}
	}

	header, err := t.readNDEFHeader(ctx)
	if err != nil {
		return nil, err
	}

	// Calculate totalBytes with overflow protection
	totalBytes := header.headerSize + header.ndefLength + 1

	// Additional bounds checking for totalBytes calculation
	if totalBytes < 0 || totalBytes > 1000000 { // Sanity check against unreasonable values
		return nil, fmt.Errorf("invalid total bytes calculation: %d (header: %d, length: %d)",
			totalBytes, header.headerSize, header.ndefLength)
	}

	// Try to detect tag type for optimal read strategy; failure is non-fatal
	// as we fall back to conservative memory bounds
	if detectErr := t.ensureTagTypeDetected(ctx); detectErr != nil {
		Debugf("NTAG ReadNDEF: tag type detection failed (using conservative bounds): %v", detectErr)
	}

	// Clone tags (like Fudan FM11NT021 with UID prefix 0x1D) often don't support FAST_READ (0x3A).
	// Sending FAST_READ to these clones can return garbage data or corrupt the tag state,
	// causing "no NDEF record found" errors. Skip directly to block-by-block reading for non-NXP tags.
	// NXP genuine tags have UID prefix 0x04.
	if len(t.uid) > 0 && t.uid[0] != 0x04 {
		Debugf("NTAG ReadNDEF: non-NXP UID prefix 0x%02X, skipping FAST_READ for clone compatibility", t.uid[0])
		return t.readNDEFBlockByBlock(ctx)
	}
	// Raw Type 2 transports can reject FAST_READ with a card framing error even
	// while standard READ works. Prefer the universally supported command for NDEF.
	if t.requiresRawType2Commands() {
		return t.readNDEFBlockByBlock(ctx)
	}

	data, err := t.readNDEFDataWithFastRead(ctx, header, totalBytes)
	if err != nil {
		// CRITICAL: Clear transport state after failed InCommunicateThru operation
		// This prevents firmware lockup when switching to InDataExchange protocol
		fallbackReason := "FastRead failed"
		if runtime.GOOS == "windows" && t.isUARTTransport() {
			fallbackReason = "FastRead failed (Windows UART has size limits to prevent PN532 firmware lockup)"
		}
		Debugf("NTAG %s, clearing transport state before fallback: %v", fallbackReason, err)
		if clearErr := t.device.ClearTransportState(); clearErr != nil {
			Debugf("NTAG transport state clearing failed: %v", clearErr)
		}
		return t.readNDEFBlockByBlock(ctx)
	}

	if len(data) > totalBytes {
		data = data[:totalBytes]
	}

	return ParseNDEFMessage(data)
}

type ndefHeader struct {
	ndefLength int
	headerSize int
	// tlvOffset is the byte offset within user memory where NDEF TLV starts
	// This is needed when there are Lock/Memory Control TLVs before NDEF
	tlvOffset int
}

func (t *NTAGTag) readNDEFHeader(ctx context.Context) (*ndefHeader, error) {
	// Read user memory pages to scan for NDEF TLV
	// Per NFC Forum Type 2 Tag spec, the data area can contain:
	// - NULL TLV (0x00): single padding byte
	// - Lock Control TLV (0x01): defines lock bit positions
	// - Memory Control TLV (0x02): defines reserved memory
	// - NDEF Message TLV (0x03): the actual NDEF data
	// - Terminator TLV (0xFE): end marker
	//
	// We must scan through TLVs to find NDEF, not assume it's at byte 0

	// Read initial pages (16 bytes = 4 pages) for TLV scanning
	data, err := t.readRawPages(ctx, ntagUserMemStart, 4)
	if err != nil {
		return nil, fmt.Errorf("%w (NDEF header): %w", ErrTagReadFailed, err)
	}

	Debugf("NTAG readNDEFHeader: user data = [% 02X] (len=%d)", data, len(data))

	// Scan for NDEF TLV
	loc, err := ScanForNDEFTLV(data)
	if err != nil {
		// For errors other than "not found", fail immediately
		if !errors.Is(err, ErrTLVNDEFNotFound) {
			Debugf("NTAG readNDEFHeader: TLV scan error: %v", err)
			return nil, ErrNoNDEF
		}
		// If we didn't find NDEF in the first 16 bytes and didn't hit terminator,
		// try reading more pages
		loc, err = t.scanMorePagesForNDEF(ctx, data)
		if err != nil {
			Debugf("NTAG readNDEFHeader: NDEF TLV not found: %v", err)
			return nil, ErrNoNDEF
		}
	}

	Debugf("NTAG readNDEFHeader: found NDEF TLV at offset %d, length %d, headerSize %d",
		loc.Offset-loc.HeaderSize, loc.Length, loc.HeaderSize)

	header := &ndefHeader{
		ndefLength: loc.Length,
		headerSize: loc.HeaderSize,
		tlvOffset:  loc.Offset - loc.HeaderSize,
	}

	time.Sleep(5 * time.Millisecond)

	// Validate NDEF length against reasonable bounds
	if header.ndefLength < 0 || header.ndefLength > 65535 {
		return nil, fmt.Errorf("invalid NDEF length: %d (must be 0-65535)", header.ndefLength)
	}

	// Validate against tag capacity if we know the tag type
	if err := t.validateNDEFLengthAgainstCapacity(ctx, header.ndefLength); err != nil {
		return nil, err
	}

	return header, nil
}

// readRawPages reads multiple consecutive pages and returns the raw data.
// Unlike ReadBlock which returns only 4 bytes, this returns all data from the READ command.
func (t *NTAGTag) readRawPages(ctx context.Context, startPage uint8, pageCount int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expectedBytes := pageCount * ntagBlockSize
	if t.requiresRawType2Commands() {
		return t.readRawType2Pages(ctx, startPage, expectedBytes)
	}

	// NTAG READ command returns 16 bytes (4 pages) starting from the specified page
	data, err := t.device.SendDataExchangeWithRetry(ctx, []byte{ntagCmdRead, startPage})
	if err != nil {
		return nil, err
	}

	// The response should be 16 bytes (4 pages)
	if len(data) < expectedBytes {
		// Return what we got
		return data, nil
	}

	return data[:expectedBytes], nil
}

// scanMorePagesForNDEF reads additional pages to continue TLV scanning.
// Called when NDEF wasn't found in the initial read.
func (t *NTAGTag) scanMorePagesForNDEF(ctx context.Context, existingData []byte) (*NDEFLocation, error) {
	// Read pages 8-11 (next 16 bytes after initial read of pages 4-7)
	moreData, err := t.readRawPages(ctx, ntagUserMemStart+4, 4)
	if err != nil {
		return nil, err
	}

	// Combine data into new slice and scan again
	combined := make([]byte, len(existingData)+len(moreData))
	copy(combined, existingData)
	copy(combined[len(existingData):], moreData)
	Debugf("NTAG scanMorePagesForNDEF: combined data = [% 02X] (len=%d)", combined, len(combined))

	return ScanForNDEFTLV(combined)
}

// validateNDEFLengthAgainstCapacity checks if the NDEF length is reasonable for the tag type
func (t *NTAGTag) validateNDEFLengthAgainstCapacity(ctx context.Context, ndefLength int) error {
	// Ensure tag type detection has been attempted; failure is non-fatal
	// as we fall back to conservative memory bounds for validation
	if err := t.ensureTagTypeDetected(ctx); err != nil {
		Debugf("NTAG validateNDEFLengthAgainstCapacity: tag type detection failed (using conservative bounds): %v", err)
	}

	var maxUserBytes int
	switch t.tagType {
	case NTAGType213:
		maxUserBytes = ntag213UserPages * ntagBlockSize // 36 * 4 = 144 bytes
	case NTAGType215:
		maxUserBytes = ntag215UserPages * ntagBlockSize // 126 * 4 = 504 bytes
	case NTAGType216:
		maxUserBytes = ntag216UserPages * ntagBlockSize // 222 * 4 = 888 bytes
	case NTAGTypeUnknown:
		// Use conservative bounds - assume smallest variant
		maxUserBytes = ntag213UserPages * ntagBlockSize // 144 bytes
	default:
		maxUserBytes = ntag213UserPages * ntagBlockSize // 144 bytes
	}

	// Account for NDEF header overhead (2-4 bytes) and terminator (1 byte)
	maxNDEFLength := maxUserBytes - 5 // Conservative estimate

	if ndefLength > maxNDEFLength {
		return fmt.Errorf("NDEF length %d exceeds tag capacity: max %d bytes for %s",
			ndefLength, maxNDEFLength, t.getTagTypeName())
	}

	return nil
}

func (t *NTAGTag) ensureTagTypeDetected(ctx context.Context) error {
	if t.tagType == NTAGTypeUnknown {
		return t.DetectType(ctx)
	}
	return nil
}

// getMaxFastReadPages returns the maximum number of pages to read in a single FastRead operation
// This is platform-specific to avoid PN532 firmware lockups with large InCommunicateThru payloads
func (t *NTAGTag) getMaxFastReadPages() uint8 {
	// Check if user has explicitly configured a limit
	if t.device.config.MaxFastReadPages > 0 {
		// Bounds check to prevent integer overflow and ensure reasonable limits
		switch {
		case t.device.config.MaxFastReadPages > 255:
			Debugf("NTAG MaxFastReadPages %d exceeds uint8 limit, capping to 255", t.device.config.MaxFastReadPages)
			return 255
		default:
			// #nosec G115 - bounds checked above
			return uint8(t.device.config.MaxFastReadPages)
		}
	}

	// Apply platform-specific defaults
	if runtime.GOOS == "windows" {
		// Check if using UART transport which is most prone to the firmware lockup
		if t.isUARTTransport() {
			// 16 pages = 64 bytes - safe limit that avoids PN532 firmware lockups on Windows UART
			// Based on research showing InCommunicateThru becomes unreliable beyond 64 bytes
			Debugf("NTAG applying Windows UART FastRead limit: 16 pages (64 bytes) to prevent firmware lockup")
			Debugf("NTAG tip: set config.MaxFastReadPages to override this Windows-specific safety limit")
			return 16
		}
		Debugf("NTAG Windows detected but non-UART transport, using default FastRead limits")
	}

	// Default: no limit (use original behavior)
	return 60
}

// isUARTTransport checks if the current transport is UART-based
func (t *NTAGTag) isUARTTransport() bool {
	// Check if transport has UART capability
	if checker, ok := t.device.transport.(TransportCapabilityChecker); ok {
		return checker.HasCapability(CapabilityUART)
	}
	// Fallback: assume it might be UART to be safe
	return true
}

func (t *NTAGTag) readNDEFDataWithFastRead(ctx context.Context, _ *ndefHeader, totalBytes int) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if t.fastReadSupported != nil && !*t.fastReadSupported {
		Debugf("NTAG FastRead disabled - using block-by-block fallback")
		return nil, errors.New("fastread not supported")
	}

	Debugf("NTAG attempting FastRead for NDEF data (%d bytes)", totalBytes)
	readRange := t.calculateReadRange(totalBytes)

	maxPagesPerRead := t.getMaxFastReadPages()

	if readRange.endPage-readRange.startPage+1 <= maxPagesPerRead {
		Debugf("NTAG using single FastRead for pages %d-%d", readRange.startPage, readRange.endPage)
		return t.performSingleFastRead(ctx, readRange.startPage, readRange.endPage)
	}

	Debugf("NTAG using multiple FastReads for pages %d-%d (max %d pages per read)",
		readRange.startPage, readRange.endPage, maxPagesPerRead)
	return t.performMultipleFastReads(ctx, readRange.startPage, readRange.endPage, maxPagesPerRead)
}

type readRange struct {
	startPage uint8
	endPage   uint8
}

func (t *NTAGTag) calculateReadRange(totalBytes int) readRange {
	startPage := uint8(ntagUserMemStart)

	// Validate totalBytes input
	if totalBytes < 0 {
		Debugf("NTAG calculateReadRange: negative totalBytes %d, using 0", totalBytes)
		totalBytes = 0
	}
	if totalBytes > 10000 { // Sanity check against unreasonable values
		Debugf("NTAG calculateReadRange: excessive totalBytes %d, capping to 10000", totalBytes)
		totalBytes = 10000
	}

	blocksNeeded := (totalBytes + ntagBlockSize - 1) / ntagBlockSize

	// Get actual tag memory bounds for validation
	_, userEnd := t.GetUserMemoryRange()
	maxBlocksForTag := int(userEnd - startPage + 1)

	// Validate blocksNeeded against actual tag capacity
	if blocksNeeded > maxBlocksForTag {
		Debugf("NTAG calculateReadRange: blocks needed %d exceeds tag capacity %d for %s, capping to tag capacity",
			blocksNeeded, maxBlocksForTag, t.getTagTypeName())
		blocksNeeded = maxBlocksForTag
	}

	if blocksNeeded > 255 {
		Debugf("NTAG calculateReadRange: blocks needed %d exceeds uint8 limit, capping to 255", blocksNeeded)
		blocksNeeded = 255
	}
	if blocksNeeded < 0 {
		blocksNeeded = 0
	}

	// Safe conversion: blocksNeeded is explicitly bounded above
	// #nosec G115 -- blocksNeeded is explicitly bounded to valid range above
	endPage := startPage + uint8(blocksNeeded) - 1

	// Final bounds check against user memory range
	if endPage > userEnd {
		Debugf("NTAG calculateReadRange: endPage %d exceeds userEnd %d, adjusting", endPage, userEnd)
		endPage = userEnd
	}

	Debugf("NTAG calculateReadRange: totalBytes=%d, blocks=%d, range=%d-%d (%s)",
		totalBytes, blocksNeeded, startPage, endPage, t.getTagTypeName())

	return readRange{startPage: startPage, endPage: endPage}
}

func (t *NTAGTag) performSingleFastRead(ctx context.Context, startPage, endPage uint8) ([]byte, error) {
	data, err := t.FastRead(ctx, startPage, endPage)
	if err != nil {
		if t.isFastReadNotSupportedError(err) {
			t.markFastReadAsUnsupported()
		}
		return nil, err
	}

	return data, nil
}

func (t *NTAGTag) performMultipleFastReads(
	ctx context.Context, startPage, endPage, maxPagesPerRead uint8,
) ([]byte, error) {
	data := make([]byte, 0, int(endPage-startPage+1)*ntagBlockSize)
	currentPage := startPage

	for currentPage <= endPage {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		readEnd := currentPage + maxPagesPerRead - 1
		if readEnd > endPage {
			readEnd = endPage
		}

		chunk, err := t.FastRead(ctx, currentPage, readEnd)
		if err != nil {
			if t.isFastReadNotSupportedError(err) {
				t.markFastReadAsUnsupported()
			}
			return nil, err
		}

		data = append(data, chunk...)
		currentPage = readEnd + 1
	}

	return data, nil
}

func (*NTAGTag) isFastReadNotSupportedError(err error) bool {
	// Use structured error checking for PN532 command not supported errors
	return IsCommandNotSupported(err)
}

func (t *NTAGTag) markFastReadAsUnsupported() {
	Debugf("NTAG marking FastRead as unsupported - will use block-by-block reads")
	supported := false
	t.fastReadSupported = &supported
}

// blockByBlockReadState tracks state during block-by-block reading
type blockByBlockReadState struct {
	data           []byte
	emptyBlocks    int
	maxEmptyBlocks int
}

// processBlock reads and processes a single block, returning true if reading should stop
func (t *NTAGTag) processBlock(ctx context.Context, blockNum int, state *blockByBlockReadState) (stop bool, err error) {
	if blockNum > 255 {
		return true, nil // Prevent overflow - should not happen with valid NTAG tags
	}

	// Safe conversion: blockNum is checked to be <= 255
	blockData, err := t.readBlockWithRetry(ctx, uint8(blockNum)) // #nosec G115
	if err != nil {
		Debugf("NTAG block-by-block read failed at block %d: %v", blockNum, err)
		// If we haven't read any data yet, propagate the error so retry logic can kick in.
		// This handles the case where the first block fails (e.g., during card sliding).
		// If we have some data, stop reading and try to parse what we have.
		if len(state.data) == 0 {
			return true, err
		}
		return true, nil
	}

	// Check if block is empty (all zeros)
	if bytes.Equal(blockData, make([]byte, len(blockData))) {
		state.emptyBlocks++
		if state.emptyBlocks >= state.maxEmptyBlocks {
			Debugf("NTAG stopping block-by-block read after %d empty blocks", state.maxEmptyBlocks)
			return true, nil
		}
	} else {
		state.emptyBlocks = 0
	}

	state.data = append(state.data, blockData...)

	// Check if we've found the NDEF end marker
	if bytes.Contains(state.data, ndefEnd) {
		Debugf("NTAG found NDEF end marker, stopping block-by-block read")
		return true, nil
	}

	return false, nil
}

// readNDEFBlockByBlock is the fallback method using block-by-block reads
func (t *NTAGTag) readNDEFBlockByBlock(ctx context.Context) (*NDEFMessage, error) {
	Debugf("NTAG reading NDEF data using block-by-block method (FastRead unavailable)")

	// Ensure tag type is detected for proper bounds; failure is non-fatal
	// as we fall back to conservative memory range
	if detectErr := t.ensureTagTypeDetected(ctx); detectErr != nil {
		Debugf("NTAG readNDEFBlockByBlock: tag type detection failed (using conservative bounds): %v", detectErr)
	}

	// Get the actual user memory range for this tag type
	userStart, userEnd := t.GetUserMemoryRange()
	maxBlocks := int(userEnd) + 1 // +1 to include userEnd block

	Debugf("NTAG block-by-block reading from block %d to %d (%s)", userStart, userEnd, t.getTagTypeName())

	// Allocate buffer based on actual tag capacity
	estimatedCapacity := (maxBlocks - int(userStart)) * ntagBlockSize
	state := &blockByBlockReadState{
		data:           make([]byte, 0, estimatedCapacity),
		maxEmptyBlocks: 3,
	}

	for i := int(userStart); i < maxBlocks; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		stop, err := t.processBlock(ctx, i, state)
		if err != nil {
			return nil, err
		}
		if stop {
			break
		}
	}

	Debugf("NTAG block-by-block read completed: %d bytes", len(state.data))
	return ParseNDEFMessage(state.data)
}

// WriteNDEF writes NDEF data to the NTAG tag with final verification
func (t *NTAGTag) WriteNDEF(ctx context.Context, message *NDEFMessage) error {
	if len(message.Records) == 0 {
		return errors.New("no NDEF records to write")
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	data, err := BuildNDEFMessageEx(message.Records)
	if err != nil {
		return fmt.Errorf("failed to build NDEF message: %w", err)
	}

	userStart, userEnd, err := t.validateAndGetMemoryRange(ctx, data)
	if err != nil {
		return err
	}

	if err := t.writeDataToBlocks(ctx, data, userStart, userEnd); err != nil {
		return err
	}

	// Verify write by reading back and comparing
	return t.verifyWrittenData(ctx, data, userStart)
}

func (t *NTAGTag) validateAndGetMemoryRange(ctx context.Context, data []byte) (userStart, userEnd uint8, err error) {
	userStart, userEnd = t.GetUserMemoryRange()
	maxBytes := int(userEnd-userStart+1) * ntagBlockSize

	if len(data) <= maxBytes {
		return userStart, userEnd, nil
	}

	// Try to detect tag type if not already known
	if t.tagType == NTAGTypeUnknown {
		if err := t.DetectType(ctx); err == nil {
			userStart, userEnd = t.GetUserMemoryRange()
			maxBytes = int(userEnd-userStart+1) * ntagBlockSize
		}
	}

	if len(data) > maxBytes {
		return 0, 0, fmt.Errorf("NDEF message too large: %d bytes, max %d bytes for tag type", len(data), maxBytes)
	}

	return userStart, userEnd, nil
}

func (t *NTAGTag) writeDataToBlocks(ctx context.Context, data []byte, userStart, userEnd uint8) error {
	block := userStart
	for i := 0; i < len(data); i += ntagBlockSize {
		if err := ctx.Err(); err != nil {
			return err
		}

		if block > userEnd {
			return errors.New("NDEF data exceeds tag memory capacity")
		}

		blockData := t.prepareBlockData(data, i)
		if err := t.WriteBlock(ctx, block, blockData); err != nil {
			return fmt.Errorf("%w (block %d): %w", ErrTagWriteFailed, block, err)
		}
		block++
	}
	return nil
}

func (*NTAGTag) prepareBlockData(data []byte, startIndex int) []byte {
	end := startIndex + ntagBlockSize
	if end > len(data) {
		// Pad last block with zeros
		blockData := make([]byte, ntagBlockSize)
		copy(blockData, data[startIndex:])
		return blockData
	}
	return data[startIndex:end]
}

// verifyWrittenData reads back written data and compares it to the original
func (t *NTAGTag) verifyWrittenData(ctx context.Context, expectedData []byte, startBlock uint8) error {
	numBlocks := (len(expectedData) + ntagBlockSize - 1) / ntagBlockSize

	// Bounds check to prevent uint8 overflow
	if numBlocks > 255-int(startBlock) {
		return fmt.Errorf("too many blocks to verify: %d exceeds uint8 range", numBlocks)
	}

	for i := range numBlocks {
		if err := ctx.Err(); err != nil {
			return err
		}

		block := startBlock + uint8(i)
		readData, err := t.ReadBlock(ctx, block)
		if err != nil {
			return fmt.Errorf("verification read failed (block %d): %w", block, err)
		}

		startIdx := i * ntagBlockSize
		expected := t.prepareBlockData(expectedData, startIdx)

		if !bytes.Equal(readData[:len(expected)], expected) {
			Debugf("Write verify fail block %d: want %X, got %X",
				block, expected, readData[:len(expected)])
			return fmt.Errorf("%w at block %d", ErrWriteVerificationFailed, block)
		}
	}

	Debugf("NTAG write verification successful: %d blocks verified", numBlocks)
	return nil
}

// FastRead performs a fast read operation on NTAG tags
// It reads multiple blocks from startAddr to endAddr (inclusive)
func (t *NTAGTag) FastRead(ctx context.Context, startAddr, endAddr uint8) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if startAddr > endAddr {
		return nil, fmt.Errorf("invalid address range: start (%d) > end (%d)", startAddr, endAddr)
	}

	Debugf("NTAG FastRead: reading pages %d-%d (%d pages)", startAddr, endAddr, endAddr-startAddr+1)

	// FAST_READ command format: 0x3A + start address + end address
	cmd := []byte{ntagCmdFastRead, startAddr, endAddr}

	// Use SendRawCommand for FastRead as some PN532 chips require it
	// (SendDataExchange returns error 0x81 on some PN532 variants)
	var data []byte
	var err error
	if t.requiresRawType2Commands() {
		data, err = t.sendRawType2Command(ctx, cmd)
	} else {
		data, err = t.device.SendRawCommand(ctx, cmd)

		// Re-select target after SendRawCommand to restore PN532 internal state.
		// SendRawCommand uses InCommunicateThru (0x42) which is a raw pass-through command
		// that doesn't maintain the PN532's target selection state. Without re-selection,
		// subsequent InDataExchange calls may fail with timeout error 01.
		// See PN532 User Manual §7.3.9: "The host controller has to take care of the
		// selection of the target it wants to reach (whereas when using the
		// InDataExchange command, it is done automatically)."
		if selectErr := t.device.InSelect(ctx); selectErr != nil {
			Debugln("NTAG FastRead: InSelect after raw command failed:", selectErr)
		}
	}

	if err != nil {
		Debugf("NTAG FastRead failed: %v", err)
		return nil, fmt.Errorf("FAST_READ failed: %w", err)
	}

	// Calculate expected number of bytes
	expectedBytes := int(endAddr-startAddr+1) * ntagBlockSize
	if len(data) < expectedBytes {
		return nil, fmt.Errorf("FAST_READ response too short: expected %d bytes, got %d", expectedBytes, len(data))
	}

	Debugf("NTAG FastRead successful: read %d bytes", expectedBytes)
	return data[:expectedBytes], nil
}

// PwdAuth performs password authentication on the NTAG tag
// password must be exactly 4 bytes (32-bit)
// Returns the 2-byte PACK (Password ACKnowledge) on success
func (t *NTAGTag) PwdAuth(ctx context.Context, password []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(password) != 4 {
		return nil, fmt.Errorf("password must be 4 bytes, got %d", len(password))
	}

	// PWD_AUTH command format: 0x1B + 4-byte password
	cmd := make([]byte, 5)
	cmd[0] = ntagCmdPwdAuth
	copy(cmd[1:], password)

	var data []byte
	var err error
	if t.requiresRawType2Commands() {
		data, err = t.sendRawType2Command(ctx, cmd)
	} else {
		data, err = t.device.SendDataExchange(ctx, cmd)
	}
	if err != nil {
		return nil, fmt.Errorf("PWD_AUTH failed: %w", err)
	}

	// Response should be 2-byte PACK
	if len(data) < 2 || (!t.requiresRawType2Commands() && len(data) != 2) {
		return nil, fmt.Errorf("invalid PACK response length: expected at least 2 bytes, got %d", len(data))
	}

	return data[:2], nil
}

// GetVersion retrieves version information from the NTAG tag using proper GET_VERSION command
// This implementation uses SendRawCommand (like FastRead) for better compatibility across PN532 variants
func (t *NTAGTag) GetVersion(ctx context.Context) (*NTAGVersion, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Try GET_VERSION command using SendRawCommand (0x60)
	// This follows the same pattern as FastRead for better hardware compatibility
	cmd := []byte{ntagCmdGetVersion}

	var data []byte
	var err error
	if t.requiresRawType2Commands() {
		data, err = t.sendRawType2Command(ctx, cmd)
	} else {
		data, err = t.device.SendRawCommand(ctx, cmd)
	}
	if err != nil {
		// If GET_VERSION fails (common with clone devices), fall back to default detection
		// This maintains backward compatibility while enabling proper detection when possible
		return t.getDefaultNTAGVersion(), err
	}

	// GET_VERSION response should be exactly 8 bytes
	if len(data) < 8 {
		// Invalid response length, signal error to trigger CC-based fallback
		return nil, fmt.Errorf("GET_VERSION response too short: got %d bytes, expected 8", len(data))
	}

	// Parse the 8-byte version response
	version := &NTAGVersion{
		FixedHeader:    data[0], // Should be 0x00
		VendorID:       data[1], // Should be 0x04 for NXP
		ProductType:    data[2], // Should be 0x04 for NTAG
		ProductSubtype: data[3], // Should be 0x02 for 50pF
		MajorVersion:   data[4], // Major version
		MinorVersion:   data[5], // Minor version
		StorageSize:    data[6], // Storage size encoding (key field for variant detection)
		ProtocolType:   data[7], // Should be 0x03 for ISO14443-3
	}

	// Validate this looks like a genuine NTAG response
	if version.VendorID != 0x04 || version.ProductType != 0x04 {
		// Not a valid NTAG response, signal error to trigger CC-based fallback
		return nil, fmt.Errorf("invalid NTAG response: VendorID=0x%02X, ProductType=0x%02X",
			version.VendorID, version.ProductType)
	}

	return version, nil
}

// getDefaultNTAGVersion returns a default NTAG215 version for fallback compatibility
// This is used when GET_VERSION is not supported (clone devices, PC/SC mode, etc.)
func (*NTAGTag) getDefaultNTAGVersion() *NTAGVersion {
	return &NTAGVersion{
		FixedHeader:    0x00, // Standard NTAG fixed header
		VendorID:       0x04, // NXP Semiconductors
		ProductType:    0x04, // NTAG
		ProductSubtype: 0x02, // 50 pF
		MajorVersion:   0x01, // Version 1
		MinorVersion:   0x00, // Subversion 0
		StorageSize:    0x11, // NTAG215 storage size encoding (504 bytes)
		ProtocolType:   0x03, // ISO/IEC 14443-3
	}
}

// GetStorageSize calculates the actual storage size from the encoded value
func (v *NTAGVersion) GetStorageSize() int {
	// The most significant 7 bits represent n where size is 2^n
	n := int(v.StorageSize >> 1)
	baseSize := 1 << n

	// If LSB is 1, size is between 2^n and 2^(n+1)
	// For NTAG213 (0x0F): n=7, LSB=1, size=144 (between 128 and 256)
	// For NTAG215 (0x11): n=8, LSB=1, size=504 (between 256 and 512)
	// For NTAG216 (0x13): n=9, LSB=1, size=888 (between 512 and 1024)
	if v.StorageSize&0x01 == 1 {
		// These are the known NTAG sizes
		switch v.StorageSize {
		case 0x0F:
			return 144 // NTAG213
		case 0x11:
			return 504 // NTAG215
		case 0x13:
			return 888 // NTAG216
		}
	}

	return baseSize
}

// GetNTAGType determines the NTAG variant from version information
func (v *NTAGVersion) GetNTAGType() NTAGType {
	// Check vendor and product type first
	if v.VendorID != 0x04 || v.ProductType != 0x04 {
		return NTAGTypeUnknown
	}

	// Determine variant by storage size encoding
	switch v.StorageSize {
	case 0x0F:
		return NTAGType213
	case 0x11:
		return NTAGType215
	case 0x13:
		return NTAGType216
	default:
		return NTAGTypeUnknown
	}
}

// GetUserMemoryRange returns the start and end pages for user memory based on tag type.
func (t *NTAGTag) GetUserMemoryRange() (start, end uint8) {
	switch t.tagType {
	case NTAGType213:
		return ntag213UserStart, ntag213UserEnd
	case NTAGType215:
		return ntag215UserStart, ntag215UserEnd
	case NTAGType216:
		return ntag216UserStart, ntag216UserEnd
	case NTAGTypeUnknown:
		// Default to smallest variant if type unknown
		return ntag213UserStart, ntag213UserEnd
	default:
		// Default to smallest variant if type unknown
		return ntag213UserStart, ntag213UserEnd
	}
}

// GetConfigPage returns the configuration page address for the tag type
func (t *NTAGTag) GetConfigPage() uint8 {
	switch t.tagType {
	case NTAGType213:
		return ntag213ConfigPage
	case NTAGType215:
		return ntag215ConfigPage
	case NTAGType216:
		return ntag216ConfigPage
	case NTAGTypeUnknown:
		return ntag213ConfigPage
	default:
		return ntag213ConfigPage
	}
}

// GetPasswordPage returns the password page address for the tag type
func (t *NTAGTag) GetPasswordPage() uint8 {
	switch t.tagType {
	case NTAGType213:
		return ntag213PwdPage
	case NTAGType215:
		return ntag215PwdPage
	case NTAGType216:
		return ntag216PwdPage
	case NTAGTypeUnknown:
		return ntag213PwdPage
	default:
		return ntag213PwdPage
	}
}

// GetTotalPages returns the total number of pages for the tag type
func (t *NTAGTag) GetTotalPages() uint8 {
	switch t.tagType {
	case NTAGType213:
		return ntag213TotalPages
	case NTAGType215:
		return ntag215TotalPages
	case NTAGType216:
		return ntag216TotalPages
	case NTAGTypeUnknown:
		return ntag213TotalPages
	default:
		return ntag213TotalPages
	}
}

// DetectType detects the NTAG variant using the Capability Container (CC).
//
// This function uses CC-based detection exclusively, avoiding the GET_VERSION command
// which uses InCommunicateThru (0x42). InCommunicateThru doesn't maintain the PN532's
// target selection state, causing subsequent InDataExchange calls to fail with timeout
// error 0x01 on readers with marginal RF connections.
//
// CC-based detection accurately identifies NTAG213/215/216 from the size field in the
// capability container, which is sufficient for NDEF operations.
func (t *NTAGTag) DetectType(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// NTAG21x tags have 7-byte UIDs (distinguishes from MIFARE Classic which has 4-byte UIDs)
	// Note: Genuine NXP NTAGs start with 0x04, but clones/compatibles may use other prefixes
	if len(t.uid) != 7 {
		// Check if this looks like a failed UID parse (all zeros = default fallback from
		// corrupt AutoPoll data during card slide). This is retryable.
		if len(t.uid) == 4 && t.uid[0] == 0 && t.uid[1] == 0 && t.uid[2] == 0 && t.uid[3] == 0 {
			return fmt.Errorf("%w: UID parse failed (got default 4-byte zero UID)", ErrTagDataCorrupt)
		}
		// Real 4-byte UID = MIFARE Classic, not an NTAG - not retryable
		return fmt.Errorf("not an NTAG tag: UID must be 7 bytes, got %d bytes", len(t.uid))
	}

	// Verify by reading the capability container (CC) at page 3
	// NTAG tags should have a valid CC with NDEF magic number 0xE1 at byte 0
	ccData, err := t.ReadBlock(ctx, ntagPageCC)
	if err != nil {
		return fmt.Errorf("%w (NTAG capability container): %w", ErrTagReadFailed, err)
	}

	// Cache CC bytes for later access (Amiibo/LEGO detection)
	t.capabilityContainer = make([]byte, len(ccData))
	copy(t.capabilityContainer, ccData)

	// Read page 4 (first user data page) for Amiibo detection
	// Do this immediately after CC read, before any operations that might cause NAK
	page4Data, err := t.ReadBlock(ctx, 4)
	if err != nil {
		Debugf("NTAG DetectType: failed to read page 4: %v", err)
		// Non-fatal - continue with CC-based detection only
	} else {
		t.userDataPage = make([]byte, len(page4Data))
		copy(t.userDataPage, page4Data)
	}

	// Verify this looks like an NTAG capability container
	// NTAG CC format: [E1] [Version] [Size] [Access]
	if len(ccData) < 4 || ccData[0] != 0xE1 {
		// Non-NDEF tag (Amiibo, Lego Dimensions, blank tags, etc.)
		// Skip probing - it causes NAK errors that break subsequent reads.
		// Use page 4 data for Amiibo detection, assume NTAG215 (most common).
		if len(ccData) >= 1 {
			Debugf("NTAG CC magic byte 0x%02X != 0xE1, skipping probe", ccData[0])
		}
		t.tagType = NTAGType215 // Default for non-NDEF tags
		t.hasNDEF = false       // No valid NDEF CC - skip NDEF operations
		return nil
	}

	// Use CC-based detection for all tags (genuine NXP and clones alike)
	// This avoids GET_VERSION which uses InCommunicateThru and can cause
	// target selection issues on marginal RF connections
	t.tagType = t.detectTypeFromCapabilityContainer(ccData)
	// hasNDEF remains true (set in constructor)

	return nil
}

// detectTypeFromCapabilityContainer attempts to detect NTAG type from the capability container
// when GET_VERSION is not available (clone devices, PC/SC mode, etc.)
func (*NTAGTag) detectTypeFromCapabilityContainer(ccData []byte) NTAGType {
	if len(ccData) < 4 {
		return NTAGTypeUnknown
	}

	// CC format: [Magic 0xE1] [Version] [Size] [Access]
	// Size field encodes the total memory size
	sizeField := ccData[2]

	// NTAG size field encoding (approximate):
	// NTAG213: 0x12 (180 bytes total, 18 * 8 = 144 bytes + overhead)
	// NTAG215: 0x3E (540 bytes total, 62 * 8 = 496 bytes + overhead)
	// NTAG216: 0x6D (924 bytes total, 109 * 8 = 872 bytes + overhead)

	switch sizeField {
	case 0x12:
		Debugf("NTAG detected as NTAG213 from CC size field: 0x%02X", sizeField)
		return NTAGType213
	case 0x3E:
		Debugf("NTAG detected as NTAG215 from CC size field: 0x%02X", sizeField)
		return NTAGType215
	case 0x6D:
		Debugf("NTAG detected as NTAG216 from CC size field: 0x%02X", sizeField)
		return NTAGType216
	default:
		// Unknown size field - try to make educated guess based on range
		switch {
		case sizeField <= 0x20:
			Debugf("NTAG unknown size 0x%02X, guessing NTAG213 (small)", sizeField)
			return NTAGType213
		case sizeField <= 0x50:
			Debugf("NTAG unknown size 0x%02X, guessing NTAG215 (medium)", sizeField)
			return NTAGType215
		default:
			Debugf("NTAG unknown size 0x%02X, guessing NTAG216 (large)", sizeField)
			return NTAGType216
		}
	}
}

// validateWriteBoundary validates that a write operation is within the actual memory bounds
// This catches counterfeit tags that report larger size than actual memory
func (t *NTAGTag) validateWriteBoundary(ctx context.Context, block uint8) error {
	// Ensure tag type is detected first, but only if unknown
	// This allows tests to manually set tag type without triggering re-detection
	if t.tagType == NTAGTypeUnknown {
		_ = t.DetectType(ctx) // Try to detect, ignore error
	}

	// Only validate for potential overwrite scenarios based on GET_VERSION response
	switch t.tagType {
	case NTAGTypeUnknown, NTAGType213:
		// No boundary validation needed for unknown or NTAG213
		return nil
	case NTAGType215, NTAGType216:
		// If tag claims to be NTAG215/216 but we're writing beyond NTAG213 boundary,
		// do a quick boundary check to catch counterfeit tags
		if block >= 45 { // Beyond NTAG213 total pages (0-44)
			// Test if we can actually read this page first
			if !t.canAccessPage(ctx, block) {
				return fmt.Errorf("write to block %d failed: actual tag appears to be NTAG213 "+
					"(only 45 pages), not %s as reported by GET_VERSION", block, t.getTagTypeName())
			}
		}
	}

	return nil
}

// ProbeActualMemorySize probes the tag to find its actual memory size.
// This is useful for clone tags that lie about their capacity in the CC.
// The claimedBytes parameter (from CC) is used to set a reasonable upper bound
// to avoid reading wildly out-of-range pages which can corrupt tag state.
// Returns the last readable page number and total user memory in bytes.
func (t *NTAGTag) ProbeActualMemorySize(ctx context.Context, claimedBytes int) (lastPage uint8, userMemory int) {
	low := uint8(4) // First user page

	// Calculate upper bound from claimed size - be conservative to avoid NAKs
	// Formula: claimedBytes/4 + 4 (header) + 5 (config) - 1 = last page index
	// We subtract 1 because pages are 0-indexed
	high := uint8(44) // Default to NTAG213 last page if no claim
	if claimedBytes > 0 {
		lastPageIndex := claimedBytes/4 + 4 + 5 - 1
		if lastPageIndex > 230 {
			high = 230 // Cap at NTAG216 max
		} else {
			high = uint8(lastPageIndex)
		}
	}

	// Quick check: if page 4 isn't readable, tag has no user memory
	if !t.canAccessPage(ctx, low) {
		return 3, 0
	}

	// Binary search for the last readable page
	for low < high {
		mid := low + (high-low+1)/2
		if t.canAccessPage(ctx, mid) {
			low = mid
		} else {
			high = mid - 1
		}
	}

	lastPage = low
	// User memory = (lastPage - 3) * 4 bytes (pages 4 to lastPage inclusive)
	// But we need to exclude config pages at the end (usually last 5 pages)
	// For safety, subtract 5 pages for config area
	userPages := int(lastPage) - 3 - 5
	if userPages < 0 {
		userPages = 0
	}
	userMemory = userPages * 4

	Debugf("NTAG ProbeActualMemorySize: last readable page=%d, user memory=%d bytes", lastPage, userMemory)
	return lastPage, userMemory
}

// canAccessPage tests if a specific page can be read
func (t *NTAGTag) canAccessPage(ctx context.Context, page uint8) bool {
	if ctx.Err() != nil {
		return false
	}
	var data []byte
	var err error
	if t.requiresRawType2Commands() {
		data, err = t.readRawType2Pages(ctx, page, ntagBlockSize)
	} else {
		data, err = t.device.SendDataExchange(ctx, []byte{ntagCmdRead, page})
	}
	if err != nil {
		// Re-select standard transports to recover from NAK/timeout errors.
		if !t.requiresRawType2Commands() {
			_ = t.device.InSelect(ctx)
		}
		return false
	}
	return len(data) >= 4
}

// ReadCapabilityContainer reads the CC (page 3) and returns it
func (t *NTAGTag) ReadCapabilityContainer(ctx context.Context) ([]byte, error) {
	return t.ReadBlock(ctx, ntagPageCC)
}

// GetCachedCapabilityContainer returns the CC bytes cached during DetectType.
// Returns nil if DetectType has not been called yet.
// The returned slice is a copy to prevent modification of internal state.
func (t *NTAGTag) GetCachedCapabilityContainer() []byte {
	if t.capabilityContainer == nil {
		return nil
	}
	result := make([]byte, len(t.capabilityContainer))
	copy(result, t.capabilityContainer)
	return result
}

// GetCachedUserDataPage returns the first user data page (page 4) cached during DetectType.
// This is useful for detecting special token types like Amiibo (byte 0 = 0xA5).
// Returns nil if DetectType has not been called yet or if page 4 read failed.
// The returned slice is a copy to prevent modification of internal state.
func (t *NTAGTag) GetCachedUserDataPage() []byte {
	if t.userDataPage == nil {
		return nil
	}
	result := make([]byte, len(t.userDataPage))
	copy(result, t.userDataPage)
	return result
}

// GetClaimedSizeFromCC returns the claimed memory size from the CC size field
func GetClaimedSizeFromCC(ccData []byte) int {
	if len(ccData) < 3 {
		return 0
	}
	// CC format: [Magic 0xE1] [Version] [Size] [Access]
	// Size field * 8 = total memory in bytes
	return int(ccData[2]) * 8
}

// getTagTypeName returns a human-readable name for the current tag type
func (t *NTAGTag) getTagTypeName() string {
	switch t.tagType {
	case NTAGTypeUnknown:
		return "Unknown NTAG"
	case NTAGType213:
		return "NTAG213"
	case NTAGType215:
		return "NTAG215"
	case NTAGType216:
		return "NTAG216"
	default:
		return "Unknown NTAG"
	}
}

func (t *NTAGTag) readBlockWithRetry(ctx context.Context, block uint8) ([]byte, error) {
	if t.requiresRawType2Commands() {
		return t.readRawType2Pages(ctx, block, ntagBlockSize)
	}
	for i := range NTAGBlockReadRetries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// SendDataExchangeWithRetry handles timeout (0x01) retries internally
		data, err := t.device.SendDataExchangeWithRetry(ctx, []byte{ntagCmdRead, block})
		if err != nil {
			// If we get authentication error 14, try InCommunicateThru as fallback for clone devices
			if IsPN532AuthenticationError(err) {
				return t.readBlockCommunicateThru(ctx, block)
			}
			// For other errors, fail immediately since SendDataExchangeWithRetry already retried timeouts
			return nil, err
		}

		// NTAG returns 16 bytes (4 blocks) on read
		if len(data) >= ntagBlockSize {
			return data[:ntagBlockSize], nil
		}

		// Short response - retry
		if i < NTAGBlockReadRetries-1 {
			continue
		}
	}

	return nil, fmt.Errorf("%w (block %d after %d retries)", ErrTagReadFailed, block, NTAGBlockReadRetries)
}

// SetPasswordProtection enables password protection on the tag
// password must be exactly 4 bytes, pack must be exactly 2 bytes
// auth0 defines from which page password protection starts (0x00 = disable, 0xFF = only config area)
func (t *NTAGTag) SetPasswordProtection(ctx context.Context, password, pack []byte, auth0 uint8) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if len(password) != 4 {
		return fmt.Errorf("password must be 4 bytes, got %d", len(password))
	}
	if len(pack) != 2 {
		return fmt.Errorf("pack must be 2 bytes, got %d", len(pack))
	}

	// Get the configuration pages based on tag type
	var pwdPage, packPage, cfg0Page uint8
	switch t.tagType {
	case NTAGType213:
		pwdPage = ntag213Pwd
		packPage = ntag213Pack
		cfg0Page = ntag213Cfg0
	case NTAGType215:
		pwdPage = ntag215Pwd
		packPage = ntag215Pack
		cfg0Page = ntag215Cfg0
	case NTAGType216:
		pwdPage = ntag216Pwd
		packPage = ntag216Pack
		cfg0Page = ntag216Cfg0
	case NTAGTypeUnknown:
		return errors.New("unknown NTAG type for password configuration")
	default:
		return errors.New("unknown NTAG type for password configuration")
	}

	// Write password
	if err := t.WriteBlock(ctx, pwdPage, password); err != nil {
		return fmt.Errorf("failed to set password: %w", err)
	}

	// Write PACK
	packData := make([]byte, 4)
	copy(packData, pack)
	if err := t.WriteBlock(ctx, packPage, packData); err != nil {
		return fmt.Errorf("failed to set PACK: %w", err)
	}

	// Read current CFG0
	cfg0, err := t.ReadBlock(ctx, cfg0Page)
	if err != nil {
		return fmt.Errorf("%w (CFG0): %w", ErrTagReadFailed, err)
	}

	// Update AUTH0 in CFG0 (byte 3)
	cfg0[3] = auth0
	if err := t.WriteBlock(ctx, cfg0Page, cfg0); err != nil {
		return fmt.Errorf("failed to update AUTH0: %w", err)
	}

	return nil
}

// DisablePasswordProtection disables password protection by setting AUTH0 to 0xFF
func (t *NTAGTag) DisablePasswordProtection(ctx context.Context) error {
	return t.SetPasswordProtection(ctx, []byte{0xFF, 0xFF, 0xFF, 0xFF}, []byte{0x00, 0x00}, 0xFF)
}

// LockPage permanently locks a page from writing (irreversible!)
// This uses the static lock bytes for pages 0-15 or dynamic lock bytes for higher pages
func (t *NTAGTag) LockPage(ctx context.Context, page uint8) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if page < 3 {
		return errors.New("cannot lock system pages 0-2")
	}

	// For pages 3-15, use static lock bytes
	if page <= 15 {
		return t.lockStaticPage(ctx, page)
	}

	// For pages > 15, use dynamic lock bytes
	var dynLockPage uint8
	switch t.tagType {
	case NTAGType213:
		dynLockPage = ntag213DynLock
	case NTAGType215:
		dynLockPage = ntag215DynLock
	case NTAGType216:
		dynLockPage = ntag216DynLock
	case NTAGTypeUnknown:
		return errors.New("unknown NTAG type for dynamic lock")
	default:
		return errors.New("unknown NTAG type for dynamic lock")
	}

	// Read current dynamic lock bytes
	dynLock, err := t.ReadBlock(ctx, dynLockPage)
	if err != nil {
		return fmt.Errorf("%w (dynamic lock bytes): %w", ErrTagReadFailed, err)
	}

	// Calculate which bit to set based on NTAG type
	// This is complex and varies by tag type - simplified implementation
	// In production, consult the datasheet for exact bit mapping

	// For now, just set a bit in the dynamic lock bytes
	dynLock[0] |= 0x01

	// Write back the dynamic lock bytes
	if err := t.WriteBlock(ctx, dynLockPage, dynLock); err != nil {
		return fmt.Errorf("%w (dynamic lock bytes): %w", ErrTagWriteFailed, err)
	}

	return nil
}

// lockStaticPage locks a page using static lock bytes (pages 3-15)
func (t *NTAGTag) lockStaticPage(ctx context.Context, page uint8) error {
	// Read current lock bytes
	lockPage, err := t.ReadBlock(ctx, ntagPageStaticLock)
	if err != nil {
		return fmt.Errorf("%w (static lock bytes): %w", ErrTagReadFailed, err)
	}

	// Calculate and set the lock bit
	lockByte, lockBit := calculateStaticLockPosition(page)
	lockPage[lockByte] |= (1 << lockBit)

	// Write back the lock bytes
	if err := t.WriteBlock(ctx, ntagPageStaticLock, lockPage); err != nil {
		return fmt.Errorf("%w (static lock bytes): %w", ErrTagWriteFailed, err)
	}
	return nil
}

// MakeReadOnly permanently locks ALL user memory pages, making the tag read-only.
// This is IRREVERSIBLE. After calling this method, no data can be written to the tag.
// It sets all bits in the static lock bytes (pages 3-15) and dynamic lock bytes
// (pages 16+), then sets the CC access byte to read-only (0x0F).
//
// The CC access byte is written last so that a partial failure (e.g., RF error during
// dynamic lock write) does not misleadingly mark the tag as read-only when the hardware
// lock bits are not fully set.
func (t *NTAGTag) MakeReadOnly(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Set static lock bits (page 2, bytes 2-3) to lock pages 3-15
	staticLock, err := t.ReadBlock(ctx, ntagPageStaticLock)
	if err != nil {
		return fmt.Errorf("%w (static lock bytes): %w", ErrTagReadFailed, err)
	}
	staticLock[2] = 0xFF
	staticLock[3] = 0xFF
	if writeErr := t.WriteBlock(ctx, ntagPageStaticLock, staticLock); writeErr != nil {
		return fmt.Errorf("%w (static lock bytes): %w", ErrTagWriteFailed, writeErr)
	}

	// Determine dynamic lock page based on tag type
	var dynLockPage uint8
	switch t.tagType {
	case NTAGType213:
		dynLockPage = ntag213DynLock
	case NTAGType215:
		dynLockPage = ntag215DynLock
	case NTAGType216:
		dynLockPage = ntag216DynLock
	case NTAGTypeUnknown:
		return errors.New("unknown NTAG type for lock operation")
	default:
		return errors.New("unknown NTAG type for lock operation")
	}

	// Set dynamic lock bits (bytes 0-2) to lock all remaining pages
	// Byte 3 is RFUI (reserved for future use) and must not be modified
	dynLock, err := t.ReadBlock(ctx, dynLockPage)
	if err != nil {
		return fmt.Errorf("%w (dynamic lock bytes): %w", ErrTagReadFailed, err)
	}
	dynLock[0] = 0xFF
	dynLock[1] = 0xFF
	dynLock[2] = 0xFF
	if writeErr := t.WriteBlock(ctx, dynLockPage, dynLock); writeErr != nil {
		return fmt.Errorf("%w (dynamic lock bytes): %w", ErrTagWriteFailed, writeErr)
	}

	// Set CC access byte to read-only LAST (page 3, byte 3)
	// CC format: [Magic 0xE1] [Version] [Size] [Access]
	// Access 0x00 = read/write, 0x0F = read-only (NFC Forum standard)
	// Written last so partial failure doesn't misleadingly mark tag as read-only
	ccData, err := t.ReadBlock(ctx, ntagPageCC)
	if err != nil {
		return fmt.Errorf("%w (capability container): %w", ErrTagReadFailed, err)
	}
	ccData[3] = 0x0F
	if writeErr := t.WriteBlock(ctx, ntagPageCC, ccData); writeErr != nil {
		return fmt.Errorf("%w (capability container): %w", ErrTagWriteFailed, writeErr)
	}

	return nil
}

// calculateStaticLockPosition calculates the byte and bit position for static locks
func calculateStaticLockPosition(page uint8) (lockByte, lockBit byte) {
	// Lock byte 0 (byte 2): bits 0-7 control pages 3-9, 15
	// Lock byte 1 (byte 3): bits 0-6 control pages 10-15
	switch {
	case page <= 9:
		return 2, page - 3
	case page == 15:
		return 2, 7
	default: // pages 10-14
		return 3, page - 10
	}
}

// getConfigurationPages returns the CFG0 and CFG1 page numbers for the tag type
func (t *NTAGTag) getConfigurationPages() (cfg0Page, cfg1Page uint8, err error) {
	switch t.tagType {
	case NTAGType213:
		return ntag213Cfg0, ntag213Cfg1, nil
	case NTAGType215:
		return ntag215Cfg0, ntag215Cfg1, nil
	case NTAGType216:
		return ntag216Cfg0, ntag216Cfg1, nil
	case NTAGTypeUnknown:
		return 0, 0, errors.New("unknown NTAG type for access control")
	default:
		return 0, 0, errors.New("unknown NTAG type for access control")
	}
}

// updateCFG1Bits updates the CFG1 configuration bits based on the provided config
func (*NTAGTag) updateCFG1Bits(cfg1 []byte, config AccessControlConfig) {
	// Byte 0: AUTHLIM (bits 0-2), NFC_CNT_EN (bit 3), NFC_CNT_PWD_PROT (bit 4), CFGLCK (bit 6), PROT (bit 7)
	cfg1[0] = config.AuthFailureLimit & 0x07 // Set AUTHLIM
	if config.ConfigLock {
		cfg1[0] |= 0x40 // Set CFGLCK bit
	}
	if config.Protection {
		cfg1[0] |= 0x80 // Set PROT bit
	}
}

// SetAccessControl configures the access control settings
func (t *NTAGTag) SetAccessControl(ctx context.Context, config AccessControlConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if config.AuthFailureLimit > 7 {
		return fmt.Errorf("authFailureLimit must be 0-7, got %d", config.AuthFailureLimit)
	}

	cfg0Page, cfg1Page, err := t.getConfigurationPages()
	if err != nil {
		return err
	}

	// Read current CFG0 (not used in this implementation, kept for future use)
	_, err = t.ReadBlock(ctx, cfg0Page)
	if err != nil {
		return fmt.Errorf("%w (CFG0): %w", ErrTagReadFailed, err)
	}

	// Read current CFG1
	cfg1, err := t.ReadBlock(ctx, cfg1Page)
	if err != nil {
		return fmt.Errorf("%w (CFG1): %w", ErrTagReadFailed, err)
	}

	// Update CFG1
	t.updateCFG1Bits(cfg1, config)

	// Write back CFG1
	if err := t.WriteBlock(ctx, cfg1Page, cfg1); err != nil {
		return fmt.Errorf("%w (CFG1): %w", ErrTagWriteFailed, err)
	}

	return nil
}

// readBlockCommunicateThru tries to read a block using InCommunicateThru instead of InDataExchange
// This is used as a fallback for clone devices that have limited InDataExchange support
//
//nolint:gocognit,revive // Retry logic with multiple fallback paths requires complexity
func (t *NTAGTag) readBlockCommunicateThru(ctx context.Context, block uint8) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Build NTAG read command
	cmd := []byte{ntagCmdRead, block}

	// Try SendRawCommand with retry logic for clone device timeout issues
	var data []byte
	var err error
	successAttempt := 0

	for attempt := 1; attempt <= NTAGFallbackRetries; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}

		Debugf("NTAG InCommunicateThru attempt %d/%d for block %d", attempt, NTAGFallbackRetries, block)
		data, err = t.device.SendRawCommand(ctx, cmd)

		// ALWAYS re-select target after SendRawCommand to restore PN532 internal state,
		// regardless of success or failure. SendRawCommand uses InCommunicateThru (0x42)
		// which doesn't maintain target selection state.
		if selectErr := t.device.InSelect(ctx); selectErr != nil {
			Debugln("NTAG readBlockCommunicateThru: InSelect failed:", selectErr)
		}

		if err == nil {
			successAttempt = attempt
			break
		}

		// Check if this is a clone device timeout issue that we can work around
		if strings.Contains(err.Error(), "PN532 didn't reply") ||
			strings.Contains(err.Error(), "timeout") ||
			strings.Contains(err.Error(), "InCommunicateThru error") {
			Debugf("NTAG InCommunicateThru timeout/error on attempt %d: %v", attempt, err)
			if attempt < NTAGFallbackRetries {
				// Brief pause before retry to let clone device stabilize
				time.Sleep(NTAGTimeoutRetryDelay)
				continue
			}
			// On final attempt, try fallback to InDataExchange without target selection
			Debugf("NTAG InCommunicateThru failed all attempts, trying direct InDataExchange fallback")
			return t.readBlockDirectFallback(ctx, block)
		}

		// For other errors, don't retry
		break
	}

	if err != nil {
		return nil, fmt.Errorf("raw read command failed: %w", err)
	}

	// NTAG returns 16 bytes (4 blocks) on read
	if len(data) < ntagBlockSize {
		return nil, fmt.Errorf("%w: invalid response length %d (expected at least %d)",
			ErrTagReadFailed, len(data), ntagBlockSize)
	}

	if successAttempt > 0 {
		Debugf("NTAG InCommunicateThru succeeded on attempt %d", successAttempt)
	}
	// Return only the requested block
	return data[:ntagBlockSize], nil
}

// readBlockDirectFallback attempts a direct InDataExchange without proper target selection
// This is a last-resort fallback for clones that have both InSelect and InCommunicateThru issues
func (t *NTAGTag) readBlockDirectFallback(ctx context.Context, block uint8) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	Debugf("NTAG attempting direct InDataExchange fallback for block %d", block)

	// Restore target selection before fallback attempt.
	// This is needed because we're coming from readBlockCommunicateThru which used
	// InCommunicateThru (0x42) that may have disrupted the PN532's target selection state.
	if selectErr := t.device.InSelect(ctx); selectErr != nil {
		Debugf("NTAG readBlockDirectFallback: InSelect failed (continuing): %v", selectErr)
	}

	// Try direct InDataExchange with retry, ignoring the error 14 that originally triggered the fallback chain
	data, err := t.device.SendDataExchangeWithRetry(ctx, []byte{ntagCmdRead, block})
	if err != nil {
		// If this also fails, the clone device simply doesn't support NTAG properly
		Debugf("NTAG direct InDataExchange fallback also failed: %v", err)
		return nil, errors.New("NTAG read not supported by this clone device (error 14): " +
			"this clone device has NTAG compatibility issues. Consider using a genuine PN532 device " +
			"or different reader for NTAG cards. MIFARE cards should work fine")
	}

	// NTAG returns 16 bytes (4 blocks) on read
	if len(data) < ntagBlockSize {
		return nil, fmt.Errorf("%w: invalid response length %d (expected at least %d)",
			ErrTagReadFailed, len(data), ntagBlockSize)
	}

	Debugf("NTAG direct InDataExchange fallback succeeded for block %d", block)
	// Return only the requested block
	return data[:ntagBlockSize], nil
}

// DebugInfo returns detailed debug information about the NTAG tag
func (t *NTAGTag) DebugInfo(ctx context.Context) string {
	return t.DebugInfoWithNDEF(ctx, t)
}

// WriteText writes a simple text record to the NTAG tag
func (t *NTAGTag) WriteText(ctx context.Context, text string) error {
	message := &NDEFMessage{
		Records: []NDEFRecord{
			{
				Type: NDEFTypeText,
				Text: text,
			},
		},
	}

	return t.WriteNDEF(ctx, message)
}
