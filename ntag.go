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
	"errors"
	"fmt"
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
	fastReadSupported *bool
	BaseTag
	tagType NTAGType
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
	}
}

// ReadBlock reads a block from the NTAG tag
func (t *NTAGTag) ReadBlock(block uint8) ([]byte, error) {
	data, err := t.device.SendDataExchange([]byte{ntagCmdRead, block})
	if err != nil {
		// If we get authentication error 14, try InCommunicateThru as fallback for clone devices
		if strings.Contains(err.Error(), "data exchange error: 14") {
			return t.readBlockCommunicateThru(block)
		}
		return nil, fmt.Errorf("failed to read block %d: %w", block, err)
	}

	// NTAG returns 16 bytes (4 blocks) on read
	if len(data) < ntagBlockSize {
		return nil, fmt.Errorf("invalid read response length: %d", len(data))
	}

	// Return only the requested block
	return data[:ntagBlockSize], nil
}

// WriteBlock writes a block to the NTAG tag
func (t *NTAGTag) WriteBlock(block uint8, data []byte) error {
	if len(data) != ntagBlockSize {
		return fmt.Errorf("invalid block size: expected %d, got %d", ntagBlockSize, len(data))
	}

	// Validate write boundary to catch counterfeit tags
	if err := t.validateWriteBoundary(block); err != nil {
		return err
	}

	cmd := []byte{ntagCmdWrite, block}
	cmd = append(cmd, data...)

	_, err := t.device.SendDataExchange(cmd)
	if err != nil {
		return fmt.Errorf("failed to write block %d: %w", block, err)
	}

	return nil
}

// ReadNDEF reads NDEF data from the NTAG tag using FastRead for optimal performance
func (t *NTAGTag) ReadNDEF() (*NDEFMessage, error) {
	header, err := t.readNDEFHeader()
	if err != nil {
		return nil, err
	}

	totalBytes := header.headerSize + header.ndefLength + 1

	_ = t.ensureTagTypeDetected() // Ignore error, use conservative approach

	data, err := t.readNDEFDataWithFastRead(header, totalBytes)
	if err != nil {
		return t.readNDEFBlockByBlock()
	}

	if len(data) > totalBytes {
		data = data[:totalBytes]
	}

	return ParseNDEFMessage(data)
}

type ndefHeader struct {
	ndefLength int
	headerSize int
}

func (t *NTAGTag) readNDEFHeader() (*ndefHeader, error) {
	block4, err := t.ReadBlock(ntagUserMemStart)
	if err != nil {
		return nil, fmt.Errorf("failed to read NDEF header: %w", err)
	}

	time.Sleep(5 * time.Millisecond)

	if block4[0] != 0x03 {
		return nil, ErrNoNDEF
	}

	header := &ndefHeader{}
	if block4[1] < 0xFF {
		header.ndefLength = int(block4[1])
		header.headerSize = 2
	} else {
		header.ndefLength = int(uint16(block4[2])<<8 | uint16(block4[3]))
		header.headerSize = 4
	}

	return header, nil
}

func (t *NTAGTag) ensureTagTypeDetected() error {
	if t.tagType == NTAGTypeUnknown {
		return t.DetectType()
	}
	return nil
}

func (t *NTAGTag) readNDEFDataWithFastRead(_ *ndefHeader, totalBytes int) ([]byte, error) {
	if t.fastReadSupported != nil && !*t.fastReadSupported {
		return nil, errors.New("fastread not supported")
	}

	readRange := t.calculateReadRange(totalBytes)

	maxPagesPerRead := uint8(60)

	if readRange.endPage-readRange.startPage+1 <= maxPagesPerRead {
		return t.performSingleFastRead(readRange.startPage, readRange.endPage)
	}

	return t.performMultipleFastReads(readRange.startPage, readRange.endPage, maxPagesPerRead)
}

type readRange struct {
	startPage uint8
	endPage   uint8
}

func (t *NTAGTag) calculateReadRange(totalBytes int) readRange {
	startPage := uint8(ntagUserMemStart)
	blocksNeeded := (totalBytes + ntagBlockSize - 1) / ntagBlockSize

	if blocksNeeded > 255 {
		blocksNeeded = 255
	}
	if blocksNeeded < 0 {
		blocksNeeded = 0
	}

	// Safe conversion: blocksNeeded is guaranteed to be in range [0, 255]
	// #nosec G115 -- blocksNeeded is explicitly bounded to [0, 255] above
	endPage := startPage + uint8(blocksNeeded) - 1
	_, userEnd := t.GetUserMemoryRange()
	if endPage > userEnd {
		endPage = userEnd
	}

	return readRange{startPage: startPage, endPage: endPage}
}

func (t *NTAGTag) performSingleFastRead(startPage, endPage uint8) ([]byte, error) {
	data, err := t.FastRead(startPage, endPage)
	if err != nil {
		if t.isFastReadNotSupportedError(err) {
			t.markFastReadAsUnsupported()
		}
		return nil, err
	}

	return data, nil
}

func (t *NTAGTag) performMultipleFastReads(startPage, endPage, maxPagesPerRead uint8) ([]byte, error) {
	data := make([]byte, 0, int(endPage-startPage+1)*ntagBlockSize)
	currentPage := startPage

	for currentPage <= endPage {
		readEnd := currentPage + maxPagesPerRead - 1
		if readEnd > endPage {
			readEnd = endPage
		}

		chunk, err := t.FastRead(currentPage, readEnd)
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
	errStr := err.Error()
	return strings.Contains(errStr, "PN532 error: 0x81") ||
		strings.Contains(errStr, "InCommunicateThru error: 0x01") ||
		strings.Contains(errStr, "Invalid command") ||
		errStr == "FAST_READ failed: PN532 error: 0x81" ||
		errStr == "FAST_READ failed: InCommunicateThru error: 0x01"
}

func (t *NTAGTag) markFastReadAsUnsupported() {
	supported := false
	t.fastReadSupported = &supported
}

// readNDEFBlockByBlock is the fallback method using block-by-block reads
func (t *NTAGTag) readNDEFBlockByBlock() (*NDEFMessage, error) {
	// Read user memory blocks starting from block 4
	data := make([]byte, 0, ntagMaxBlocks*ntagBlockSize)
	emptyBlocks := 0
	maxEmptyBlocks := 3

	for i := ntagUserMemStart; i < ntagMaxBlocks; i++ {
		if i > 255 {
			break // Prevent overflow
		}
		// Safe conversion: i is checked to be <= 255
		blockData, err := t.readBlockWithRetry(uint8(i)) // #nosec G115
		if err != nil {
			break
		}

		// Check if block is empty (all zeros)
		if bytes.Equal(blockData, make([]byte, len(blockData))) {
			emptyBlocks++
			if emptyBlocks >= maxEmptyBlocks {
				break
			}
		} else {
			emptyBlocks = 0
		}

		data = append(data, blockData...)

		// Check if we've found the NDEF end marker
		if bytes.Contains(data, ndefEnd) {
			break
		}
	}

	return ParseNDEFMessage(data)
}

// WriteNDEF writes NDEF data to the NTAG tag
func (t *NTAGTag) WriteNDEF(message *NDEFMessage) error {
	if len(message.Records) == 0 {
		return errors.New("no NDEF records to write")
	}

	data, err := BuildNDEFMessageEx(message.Records)
	if err != nil {
		return fmt.Errorf("failed to build NDEF message: %w", err)
	}

	userStart, userEnd, err := t.validateAndGetMemoryRange(data)
	if err != nil {
		return err
	}

	return t.writeDataToBlocks(data, userStart, userEnd)
}

func (t *NTAGTag) validateAndGetMemoryRange(data []byte) (userStart, userEnd uint8, err error) {
	userStart, userEnd = t.GetUserMemoryRange()
	maxBytes := int(userEnd-userStart+1) * ntagBlockSize

	if len(data) <= maxBytes {
		return userStart, userEnd, nil
	}

	// Try to detect tag type if not already known
	if t.tagType == NTAGTypeUnknown {
		if err := t.DetectType(); err == nil {
			userStart, userEnd = t.GetUserMemoryRange()
			maxBytes = int(userEnd-userStart+1) * ntagBlockSize
		}
	}

	if len(data) > maxBytes {
		return 0, 0, fmt.Errorf("NDEF message too large: %d bytes, max %d bytes for tag type", len(data), maxBytes)
	}

	return userStart, userEnd, nil
}

func (t *NTAGTag) writeDataToBlocks(data []byte, userStart, userEnd uint8) error {
	block := userStart
	for i := 0; i < len(data); i += ntagBlockSize {
		if block > userEnd {
			return errors.New("NDEF data exceeds tag memory capacity")
		}

		blockData := t.prepareBlockData(data, i)
		if err := t.WriteBlock(block, blockData); err != nil {
			return fmt.Errorf("failed to write block %d: %w", block, err)
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

// FastRead performs a fast read operation on NTAG tags
// It reads multiple blocks from startAddr to endAddr (inclusive)
func (t *NTAGTag) FastRead(startAddr, endAddr uint8) ([]byte, error) {
	if startAddr > endAddr {
		return nil, fmt.Errorf("invalid address range: start (%d) > end (%d)", startAddr, endAddr)
	}

	// FAST_READ command format: 0x3A + start address + end address
	cmd := []byte{ntagCmdFastRead, startAddr, endAddr}

	// Use SendRawCommand for FastRead as some PN532 chips require it
	// (SendDataExchange returns error 0x81 on some PN532 variants)
	data, err := t.device.SendRawCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("FAST_READ failed: %w", err)
	}

	// Calculate expected number of bytes
	expectedBytes := int(endAddr-startAddr+1) * ntagBlockSize
	if len(data) < expectedBytes {
		return nil, fmt.Errorf("FAST_READ response too short: expected %d bytes, got %d", expectedBytes, len(data))
	}

	return data[:expectedBytes], nil
}

// PwdAuth performs password authentication on the NTAG tag
// password must be exactly 4 bytes (32-bit)
// Returns the 2-byte PACK (Password ACKnowledge) on success
func (t *NTAGTag) PwdAuth(password []byte) ([]byte, error) {
	if len(password) != 4 {
		return nil, fmt.Errorf("password must be 4 bytes, got %d", len(password))
	}

	// PWD_AUTH command format: 0x1B + 4-byte password
	cmd := make([]byte, 5)
	cmd[0] = ntagCmdPwdAuth
	copy(cmd[1:], password)

	data, err := t.device.SendDataExchange(cmd)
	if err != nil {
		return nil, fmt.Errorf("PWD_AUTH failed: %w", err)
	}

	// Response should be 2-byte PACK
	if len(data) != 2 {
		return nil, fmt.Errorf("invalid PACK response length: expected 2 bytes, got %d", len(data))
	}

	return data, nil
}

// GetVersion retrieves version information from the NTAG tag using proper GET_VERSION command
// This implementation uses SendRawCommand (like FastRead) for better compatibility across PN532 variants
func (t *NTAGTag) GetVersion() (*NTAGVersion, error) {
	// Try GET_VERSION command using SendRawCommand (0x60)
	// This follows the same pattern as FastRead for better hardware compatibility
	cmd := []byte{ntagCmdGetVersion}

	data, err := t.device.SendRawCommand(cmd)
	if err != nil {
		// If GET_VERSION fails (common with clone devices), fall back to default detection
		// This maintains backward compatibility while enabling proper detection when possible
		return t.getDefaultNTAGVersion(), err
	}

	// GET_VERSION response should be exactly 8 bytes
	if len(data) < 8 {
		// Invalid response length, use fallback
		return t.getDefaultNTAGVersion(), nil
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
		// Not a valid NTAG response, use fallback
		return t.getDefaultNTAGVersion(), nil
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

// DetectType attempts to detect the NTAG variant using GET_VERSION command with fallback
func (t *NTAGTag) DetectType() error {
	// First verify this is an NTAG by reading the capability container (CC) at page 3
	// NTAG tags should have a valid CC with NDEF magic number 0xE1 at byte 0
	ccData, err := t.ReadBlock(ntagPageCC)
	if err != nil {
		return fmt.Errorf("failed to read NTAG capability container: %w", err)
	}

	// Verify this looks like an NTAG capability container
	// NTAG CC format: [E1] [Version] [Size] [Access]
	if len(ccData) < 4 || ccData[0] != 0xE1 {
		return errors.New("not an NTAG tag: invalid capability container")
	}

	// Now try to get the actual version information using GET_VERSION
	version, err := t.GetVersion()
	if err != nil {
		// If GET_VERSION fails, we still know it's an NTAG from the CC check
		// Use fallback detection method
		t.tagType = NTAGType215 // Default fallback
		return err
	}

	// Use the version information to determine the exact variant
	t.tagType = version.GetNTAGType()

	if t.tagType == NTAGTypeUnknown {
		// Even if storage size is unknown, we know it's an NTAG from CC check
		// Default to NTAG215 for safety
		t.tagType = NTAGType215
	}

	return nil
}

// canAccessPageSafely tests if a specific page can be accessed (readable) with error handling
// This method is more lenient to avoid disrupting normal operations
func (t *NTAGTag) canAccessPageSafely(page uint8) bool {
	// Try to access the page with minimal impact
	data, err := t.device.SendDataExchange([]byte{ntagCmdRead, page})
	if err != nil {
		// If access fails, the page is likely beyond the boundary
		return false
	}
	// Success if we got at least 4 bytes back
	return len(data) >= 4
}

// validateWriteBoundary validates that a write operation is within the actual memory bounds
// This catches counterfeit tags that report larger size than actual memory
func (t *NTAGTag) validateWriteBoundary(block uint8) error {
	// Ensure tag type is detected first, but only if unknown
	// This allows tests to manually set tag type without triggering re-detection
	if t.tagType == NTAGTypeUnknown {
		_ = t.DetectType() // Try to detect, ignore error
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
			if !t.canAccessPageSafely(block) {
				return fmt.Errorf("write to block %d failed: actual tag appears to be NTAG213 "+
					"(only 45 pages), not %s as reported by GET_VERSION", block, t.getTagTypeName())
			}
		}
	}

	return nil
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

func (t *NTAGTag) readBlockWithRetry(block uint8) ([]byte, error) {
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		data, err := t.device.SendDataExchange([]byte{ntagCmdRead, block})
		if err != nil {
			// If we get authentication error 14, try InCommunicateThru as fallback for clone devices
			if strings.Contains(err.Error(), "data exchange error: 14") {
				return t.readBlockCommunicateThru(block)
			}
			if i < maxRetries-1 {
				continue
			}
			return nil, err
		}

		// NTAG returns 16 bytes (4 blocks) on read
		if len(data) >= ntagBlockSize {
			return data[:ntagBlockSize], nil
		}

		if i < maxRetries-1 {
			continue
		}
	}

	return nil, fmt.Errorf("failed to read block %d after %d retries", block, maxRetries)
}

// SetPasswordProtection enables password protection on the tag
// password must be exactly 4 bytes, pack must be exactly 2 bytes
// auth0 defines from which page password protection starts (0x00 = disable, 0xFF = only config area)
func (t *NTAGTag) SetPasswordProtection(password, pack []byte, auth0 uint8) error {
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
	if err := t.WriteBlock(pwdPage, password); err != nil {
		return fmt.Errorf("failed to set password: %w", err)
	}

	// Write PACK
	packData := make([]byte, 4)
	copy(packData, pack)
	if err := t.WriteBlock(packPage, packData); err != nil {
		return fmt.Errorf("failed to set PACK: %w", err)
	}

	// Read current CFG0
	cfg0, err := t.ReadBlock(cfg0Page)
	if err != nil {
		return fmt.Errorf("failed to read CFG0: %w", err)
	}

	// Update AUTH0 in CFG0 (byte 3)
	cfg0[3] = auth0
	if err := t.WriteBlock(cfg0Page, cfg0); err != nil {
		return fmt.Errorf("failed to update AUTH0: %w", err)
	}

	return nil
}

// DisablePasswordProtection disables password protection by setting AUTH0 to 0xFF
func (t *NTAGTag) DisablePasswordProtection() error {
	return t.SetPasswordProtection([]byte{0xFF, 0xFF, 0xFF, 0xFF}, []byte{0x00, 0x00}, 0xFF)
}

// LockPage permanently locks a page from writing (irreversible!)
// This uses the static lock bytes for pages 0-15 or dynamic lock bytes for higher pages
func (t *NTAGTag) LockPage(page uint8) error {
	if page < 3 {
		return errors.New("cannot lock system pages 0-2")
	}

	// For pages 3-15, use static lock bytes
	if page <= 15 {
		return t.lockStaticPage(page)
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
	dynLock, err := t.ReadBlock(dynLockPage)
	if err != nil {
		return fmt.Errorf("failed to read dynamic lock bytes: %w", err)
	}

	// Calculate which bit to set based on NTAG type
	// This is complex and varies by tag type - simplified implementation
	// In production, consult the datasheet for exact bit mapping

	// For now, just set a bit in the dynamic lock bytes
	dynLock[0] |= 0x01

	// Write back the dynamic lock bytes
	if err := t.WriteBlock(dynLockPage, dynLock); err != nil {
		return fmt.Errorf("failed to write dynamic lock bytes: %w", err)
	}

	return nil
}

// lockStaticPage locks a page using static lock bytes (pages 3-15)
func (t *NTAGTag) lockStaticPage(page uint8) error {
	// Read current lock bytes
	lockPage, err := t.ReadBlock(ntagPageStaticLock)
	if err != nil {
		return fmt.Errorf("failed to read static lock bytes: %w", err)
	}

	// Calculate and set the lock bit
	lockByte, lockBit := calculateStaticLockPosition(page)
	lockPage[lockByte] |= (1 << lockBit)

	// Write back the lock bytes
	if err := t.WriteBlock(ntagPageStaticLock, lockPage); err != nil {
		return fmt.Errorf("failed to write static lock bytes: %w", err)
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
func (t *NTAGTag) SetAccessControl(config AccessControlConfig) error {
	if config.AuthFailureLimit > 7 {
		return fmt.Errorf("authFailureLimit must be 0-7, got %d", config.AuthFailureLimit)
	}

	cfg0Page, cfg1Page, err := t.getConfigurationPages()
	if err != nil {
		return err
	}

	// Read current CFG0 (not used in this implementation, kept for future use)
	_, err = t.ReadBlock(cfg0Page)
	if err != nil {
		return fmt.Errorf("failed to read CFG0: %w", err)
	}

	// Read current CFG1
	cfg1, err := t.ReadBlock(cfg1Page)
	if err != nil {
		return fmt.Errorf("failed to read CFG1: %w", err)
	}

	// Update CFG1
	t.updateCFG1Bits(cfg1, config)

	// Write back CFG1
	if err := t.WriteBlock(cfg1Page, cfg1); err != nil {
		return fmt.Errorf("failed to write CFG1: %w", err)
	}

	return nil
}

// readBlockCommunicateThru tries to read a block using InCommunicateThru instead of InDataExchange
// This is used as a fallback for clone devices that have limited InDataExchange support
func (t *NTAGTag) readBlockCommunicateThru(block uint8) ([]byte, error) {
	// Build NTAG read command
	cmd := []byte{ntagCmdRead, block}

	// Try SendRawCommand with retry logic for clone device timeout issues
	var data []byte
	var err error
	maxRetries := 3
	successAttempt := 0

	for attempt := 1; attempt <= maxRetries; attempt++ {
		debugf("NTAG InCommunicateThru attempt %d/%d for block %d", attempt, maxRetries, block)
		data, err = t.device.SendRawCommand(cmd)
		if err == nil {
			successAttempt = attempt
			break
		}

		// Check if this is a clone device timeout issue that we can work around
		if strings.Contains(err.Error(), "PN532 didn't reply") ||
			strings.Contains(err.Error(), "timeout") ||
			strings.Contains(err.Error(), "InCommunicateThru error") {
			debugf("NTAG InCommunicateThru timeout/error on attempt %d: %v", attempt, err)
			if attempt < maxRetries {
				// Brief pause before retry to let clone device stabilize
				time.Sleep(100 * time.Millisecond)
				continue
			}
			// On final attempt, try fallback to InDataExchange without target selection
			debugf("NTAG InCommunicateThru failed all attempts, trying direct InDataExchange fallback")
			return t.readBlockDirectFallback(block)
		}

		// For other errors, don't retry
		break
	}

	if err != nil {
		return nil, fmt.Errorf("raw read command failed: %w", err)
	}

	// NTAG returns 16 bytes (4 blocks) on read
	if len(data) < ntagBlockSize {
		return nil, fmt.Errorf("invalid read response length: %d", len(data))
	}

	if successAttempt > 0 {
		debugf("NTAG InCommunicateThru succeeded on attempt %d", successAttempt)
	}
	// Return only the requested block
	return data[:ntagBlockSize], nil
}

// readBlockDirectFallback attempts a direct InDataExchange without proper target selection
// This is a last-resort fallback for clones that have both InSelect and InCommunicateThru issues
func (t *NTAGTag) readBlockDirectFallback(block uint8) ([]byte, error) {
	debugf("NTAG attempting direct InDataExchange fallback for block %d", block)

	// Try direct InDataExchange, ignoring the error 14 that originally triggered the fallback chain
	data, err := t.device.SendDataExchange([]byte{ntagCmdRead, block})
	if err != nil {
		// If this also fails, the clone device simply doesn't support NTAG properly
		debugf("NTAG direct InDataExchange fallback also failed: %v", err)
		return nil, errors.New("NTAG read not supported by this clone device (error 14): " +
			"this clone device has NTAG compatibility issues. Consider using a genuine PN532 device " +
			"or different reader for NTAG cards. MIFARE cards should work fine")
	}

	// NTAG returns 16 bytes (4 blocks) on read
	if len(data) < ntagBlockSize {
		return nil, fmt.Errorf("invalid read response length: %d", len(data))
	}

	debugf("NTAG direct InDataExchange fallback succeeded for block %d", block)
	// Return only the requested block
	return data[:ntagBlockSize], nil
}

// DebugInfo returns detailed debug information about the NTAG tag
func (t *NTAGTag) DebugInfo() string {
	return t.DebugInfoWithNDEF(t)
}

// WriteText writes a simple text record to the NTAG tag
func (t *NTAGTag) WriteText(text string) error {
	message := &NDEFMessage{
		Records: []NDEFRecord{
			{
				Type: NDEFTypeText,
				Text: text,
			},
		},
	}

	return t.WriteNDEF(message)
}
