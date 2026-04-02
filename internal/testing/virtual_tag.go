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

package testing

import (
	"encoding/hex"
	"errors"
	"fmt"
)

// VirtualTag represents a simulated NFC tag for testing
type VirtualTag struct {
	sectorKeys          map[int][]byte
	Type                string
	UID                 []byte
	Memory              [][]byte
	ndefData            []byte
	authenticatedSector int
	Present             bool
	authenticatedKey    byte
	supportsFastRead    bool // Whether tag supports FAST_READ (0x3A) command
}

// NewVirtualNTAG213 creates a virtual NTAG213 tag with default content
func NewVirtualNTAG213(uid []byte) *VirtualTag {
	if uid == nil {
		uid = TestNTAG213UID
	}

	tag := &VirtualTag{
		Type:             "NTAG213",
		UID:              uid,
		Memory:           make([][]byte, 45), // NTAG213 has 45 blocks (180 bytes)
		Present:          true,
		supportsFastRead: true, // Genuine NXP tags support FAST_READ
	}

	// Initialize with default NTAG213 memory layout
	tag.initNTAG213Memory()

	// Set default NDEF message: "Hello World"
	// Ignore error since this is test setup with known good data
	_ = tag.SetNDEFText("Hello World")

	return tag
}

// NewVirtualFudanClone creates a virtual Fudan FM11NT021 clone tag.
// Fudan clones have UID prefix 0x1D and don't support FAST_READ (0x3A) or GET_VERSION (0x60).
// They are functionally NTAG213-compatible but with limited command support.
// See: https://github.com/RfidResearchGroup/proxmark3/issues/2457
func NewVirtualFudanClone(uid []byte) *VirtualTag {
	if uid == nil {
		// Default Fudan UID (starts with 0x1D)
		uid = []byte{0x1D, 0x20, 0xBD, 0xC9, 0x07, 0x10, 0x80}
	}

	tag := &VirtualTag{
		Type:             "NTAG213", // Same type - they're NTAG213 compatible
		UID:              uid,
		Memory:           make([][]byte, 45), // Same memory layout as NTAG213
		Present:          true,
		supportsFastRead: false, // Fudan clones don't support FAST_READ
	}

	// Initialize with default NTAG213 memory layout
	tag.initNTAG213Memory()

	// Set default NDEF message: "Hello World"
	_ = tag.SetNDEFText("Hello World")

	return tag
}

// NewVirtualMIFARE1K creates a virtual MIFARE Classic 1K tag
func NewVirtualMIFARE1K(uid []byte) *VirtualTag {
	if uid == nil {
		uid = TestMIFARE1KUID
	}

	tag := &VirtualTag{
		Type:                "MIFARE1K",
		UID:                 uid,
		Memory:              make([][]byte, 64), // MIFARE 1K has 64 blocks (1024 bytes)
		Present:             true,
		authenticatedSector: -1, // Not authenticated initially
		sectorKeys:          make(map[int][]byte),
	}

	// Initialize with default MIFARE memory layout and keys
	tag.initMIFARE1KMemory()

	return tag
}

// NewVirtualMIFARE4K creates a virtual MIFARE Classic 4K tag
func NewVirtualMIFARE4K(uid []byte) *VirtualTag {
	if uid == nil {
		uid = TestMIFARE4KUID
	}

	tag := &VirtualTag{
		Type:                "MIFARE4K",
		UID:                 uid,
		Memory:              make([][]byte, 256), // MIFARE 4K has 256 blocks (4096 bytes)
		Present:             true,
		authenticatedSector: -1, // Not authenticated initially
		sectorKeys:          make(map[int][]byte),
	}

	// Initialize with default MIFARE memory layout and keys
	tag.initMIFARE4KMemory()

	return tag
}

// GetUIDString returns the UID as a hex string
func (v *VirtualTag) GetUIDString() string {
	return hex.EncodeToString(v.UID)
}

// SupportsFastRead returns whether this tag supports the FAST_READ (0x3A) command.
// Genuine NXP tags support FAST_READ, but clone tags (like Fudan FM11NT021) typically don't.
func (v *VirtualTag) SupportsFastRead() bool {
	return v.supportsFastRead
}

// isNTAG returns true if this is an NTAG-type tag (uses 4-byte pages)
func (v *VirtualTag) isNTAG() bool {
	return v.Type == "NTAG213" || v.Type == "NTAG215" || v.Type == "NTAG216"
}

// ReadNTAGPages reads multiple consecutive 4-byte pages and returns them concatenated.
// This simulates the NTAG READ command which returns 4 pages (16 bytes) at once.
func (v *VirtualTag) ReadNTAGPages(startPage, numPages int) ([]byte, error) {
	if !v.Present {
		return nil, errors.New("tag not present")
	}

	result := make([]byte, 0, numPages*4)
	for i := range numPages {
		page := startPage + i
		if page < 0 || page >= len(v.Memory) {
			// Return zeros for out-of-range pages (wrap-around behavior)
			result = append(result, 0, 0, 0, 0)
			continue
		}
		if v.Memory[page] == nil {
			result = append(result, 0, 0, 0, 0)
			continue
		}
		// Ensure we get exactly 4 bytes
		pageData := v.Memory[page]
		if len(pageData) < 4 {
			padded := make([]byte, 4)
			copy(padded, pageData)
			result = append(result, padded...)
		} else {
			result = append(result, pageData[:4]...)
		}
	}
	return result, nil
}

// SetSupportsFastRead allows tests to configure whether the tag supports FAST_READ
func (v *VirtualTag) SetSupportsFastRead(supported bool) {
	v.supportsFastRead = supported
}

// ReadBlock reads a specific memory block
func (v *VirtualTag) ReadBlock(block int) ([]byte, error) {
	if !v.Present {
		return nil, errors.New("tag not present")
	}

	if block < 0 || block >= len(v.Memory) {
		return nil, fmt.Errorf("block %d out of range", block)
	}

	// MIFARE tags require authentication before reading
	if v.isMIFARE() {
		sector := v.blockToSector(block)
		if v.authenticatedSector != sector {
			return nil, fmt.Errorf("not authenticated to sector %d (block %d)", sector, block)
		}
	}

	if v.Memory[block] == nil {
		// Return zeros for uninitialized blocks
		return make([]byte, 16), nil
	}

	// Return a copy to prevent modification
	data := make([]byte, len(v.Memory[block]))
	copy(data, v.Memory[block])
	return data, nil
}

// WriteBlock writes data to a specific memory block
func (v *VirtualTag) WriteBlock(block int, data []byte) error {
	if !v.Present {
		return errors.New("tag not present")
	}

	if block < 0 || block >= len(v.Memory) {
		return fmt.Errorf("block %d out of range", block)
	}

	// MIFARE tags require authentication before writing
	if v.isMIFARE() {
		sector := v.blockToSector(block)
		if v.authenticatedSector != sector {
			return fmt.Errorf("not authenticated to sector %d (block %d)", sector, block)
		}
	}

	// Check for write protection based on tag type
	if v.isBlockWriteProtected(block) {
		return fmt.Errorf("block %d is write protected", block)
	}

	// Ensure data is exactly 16 bytes (NFC block size)
	if len(data) != 16 {
		return fmt.Errorf("data must be exactly 16 bytes, got %d", len(data))
	}

	// Copy data to prevent external modification
	v.Memory[block] = make([]byte, 16)
	copy(v.Memory[block], data)

	return nil
}

// SetNDEFText sets a simple text NDEF message
func (v *VirtualTag) SetNDEFText(text string) error {
	// Build simple NDEF text record
	// This is a simplified implementation - real NDEF is more complex
	textBytes := []byte(text)

	// NDEF Text Record format (simplified):
	// [Header][Type Length][Payload Length][Type][Language][Text]
	ndefRecord := make([]byte, 0, 7+len(textBytes))
	ndefRecord = append(ndefRecord,
		0xD1,                   // Header: MB=1, ME=1, CF=0, SR=1, IL=0, TNF=1 (Well Known)
		0x01,                   // Type Length: 1 byte
		byte(len(textBytes)+3), //nolint:gosec // NDEF text records are small
		0x54,                   // Type: "T" for Text
		0x02,                   // Language code length
		0x65, 0x6E,             // Language code: "en"
		// Text follows
	)
	ndefRecord = append(ndefRecord, textBytes...)

	// NDEF message wrapper
	ndefMessage := make([]byte, 0, 2+len(ndefRecord)+1)
	ndefMessage = append(ndefMessage,
		0x03,                  // NDEF Message TLV
		byte(len(ndefRecord)), //nolint:gosec // NDEF records fit in a byte
	)
	ndefMessage = append(ndefMessage, ndefRecord...)
	ndefMessage = append(ndefMessage, 0xFE) // Terminator TLV

	v.ndefData = ndefMessage

	// For NTAG cards, write NDEF to blocks 4-39 (user data area)
	if v.Type == "NTAG213" {
		return v.writeNDEFToNTAG()
	}

	return nil
}

// GetNDEFText extracts text from the NDEF message (simplified)
func (v *VirtualTag) GetNDEFText() string {
	if v.Type == "NTAG213" {
		return v.extractNDEFTextFromNTAG()
	}
	return ""
}

// Remove sets the tag as not present
func (v *VirtualTag) Remove() {
	v.Present = false
}

// Insert sets the tag as present
func (v *VirtualTag) Insert() {
	v.Present = true
}

// Internal helper methods

func (v *VirtualTag) initNTAG213Memory() {
	// NTAG213 has 45 pages of 4 bytes each (180 bytes total)
	// Page 0: UID bytes 0-2 + BCC0
	v.Memory[0] = make([]byte, 4)
	if len(v.UID) >= 3 {
		copy(v.Memory[0][:3], v.UID[:3])
		// BCC0 = UID0 XOR UID1 XOR UID2 XOR 0x88
		v.Memory[0][3] = v.UID[0] ^ v.UID[1] ^ v.UID[2] ^ 0x88
	}

	// Page 1: UID bytes 3-6
	v.Memory[1] = make([]byte, 4)
	if len(v.UID) > 3 {
		// Copy UID[3:min(7, len)] to page 1
		end := len(v.UID)
		if end > 7 {
			end = 7
		}
		copy(v.Memory[1], v.UID[3:end])
	}

	// Page 2: BCC1, Internal, Lock bytes
	v.Memory[2] = make([]byte, 4)
	if len(v.UID) >= 7 {
		// BCC1 = UID3 XOR UID4 XOR UID5 XOR UID6
		v.Memory[2][0] = v.UID[3] ^ v.UID[4] ^ v.UID[5] ^ v.UID[6]
	} else if len(v.UID) > 3 {
		// Calculate BCC1 with available bytes, zero-pad missing
		var bcc byte
		for i := 3; i < len(v.UID); i++ {
			bcc ^= v.UID[i]
		}
		v.Memory[2][0] = bcc
	}
	v.Memory[2][1] = 0x48 // Internal byte
	v.Memory[2][2] = 0x00 // Lock byte 0
	v.Memory[2][3] = 0x00 // Lock byte 1

	// Page 3: Capability Container (CC)
	// E1 10 12 00 = NDEF magic, version 1.0, 144 bytes user memory, read/write
	v.Memory[3] = []byte{0xE1, 0x10, 0x12, 0x00}

	// Pages 4-39: User data (where NDEF goes) - 36 pages of 4 bytes = 144 bytes
	for i := 4; i < 40; i++ {
		v.Memory[i] = make([]byte, 4)
	}

	// Pages 40-44: Configuration pages
	for i := 40; i < 45; i++ {
		v.Memory[i] = make([]byte, 4)
	}
}

func (v *VirtualTag) initMIFARE1KMemory() {
	// Block 0: UID and BCC (read-only)
	v.Memory[0] = make([]byte, 16)
	copy(v.Memory[0][:len(v.UID)], v.UID)

	// Initialize all other blocks as empty
	for i := 1; i < 64; i++ {
		v.Memory[i] = make([]byte, 16)
	}

	// Default MIFARE key: FF FF FF FF FF FF
	defaultKey := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	// Set default keys and access bits for sector trailers
	for sector := range 16 {
		trailerBlock := sector*4 + 3
		v.Memory[trailerBlock] = []byte{
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // Key A
			0xFF, 0x07, 0x80, 0x69, // Access bits
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // Key B
		}
		// Store keys in sectorKeys map (Key A + Key B)
		v.sectorKeys[sector] = append(append([]byte{}, defaultKey...), defaultKey...)
	}
}

func (v *VirtualTag) initMIFARE4KMemory() {
	// Similar to 1K but with more sectors
	v.Memory[0] = make([]byte, 16)
	copy(v.Memory[0][:len(v.UID)], v.UID)

	for i := 1; i < 256; i++ {
		v.Memory[i] = make([]byte, 16)
	}

	// Default MIFARE key: FF FF FF FF FF FF
	defaultKey := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	// Set default keys for all sector trailers
	// Sectors 0-31 have 4 blocks each, sectors 32-39 have 16 blocks each
	for sector := range 32 {
		trailerBlock := sector*4 + 3
		v.Memory[trailerBlock] = []byte{
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // Key A
			0xFF, 0x07, 0x80, 0x69, // Access bits
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // Key B
		}
		// Store keys in sectorKeys map (Key A + Key B)
		v.sectorKeys[sector] = append(append([]byte{}, defaultKey...), defaultKey...)
	}
	for sector := 32; sector < 40; sector++ {
		trailerBlock := 128 + (sector-32)*16 + 15
		v.Memory[trailerBlock] = []byte{
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // Key A
			0xFF, 0x07, 0x80, 0x69, // Access bits
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // Key B
		}
		// Store keys in sectorKeys map (Key A + Key B)
		v.sectorKeys[sector] = append(append([]byte{}, defaultKey...), defaultKey...)
	}
}

func (v *VirtualTag) isBlockWriteProtected(block int) bool {
	switch v.Type {
	case "NTAG213":
		// Blocks 0-2 are read-only
		return block < 3 || block >= 40
	case "MIFARE1K":
		// Sector trailers are write-protected without proper authentication
		return (block+1)%4 == 0
	case "MIFARE4K":
		// Sector trailers are write-protected
		if block < 128 {
			return (block+1)%4 == 0
		}
		return (block-128)%16 == 15
	}
	return false
}

func (v *VirtualTag) writeNDEFToNTAG() error {
	if len(v.ndefData) > 144 { // 36 pages * 4 bytes = 144 bytes user memory
		return errors.New("NDEF data too large for NTAG213")
	}

	// Write NDEF data starting at page 4, 4 bytes per page
	dataOffset := 0
	for page := 4; page < 40 && dataOffset < len(v.ndefData); page++ {
		pageData := make([]byte, 4)
		endOffset := dataOffset + 4
		if endOffset > len(v.ndefData) {
			endOffset = len(v.ndefData)
		}
		copy(pageData, v.ndefData[dataOffset:endOffset])
		v.Memory[page] = pageData
		dataOffset += 4
	}

	return nil
}

func (v *VirtualTag) extractNDEFTextFromNTAG() string {
	// Gather all user data pages into contiguous buffer
	data := v.gatherUserDataPages()

	// Look for NDEF Text record header (0xD1, 0x01)
	for i := range len(data) - 7 {
		if data[i] != 0xD1 || data[i+1] != 0x01 {
			continue
		}
		if text := v.parseTextRecord(data, i); text != "" {
			return text
		}
	}
	return ""
}

func (v *VirtualTag) gatherUserDataPages() []byte {
	var data []byte
	for page := 4; page < 40; page++ {
		if v.Memory[page] == nil {
			data = append(data, 0, 0, 0, 0)
		} else {
			data = append(data, v.Memory[page]...)
		}
	}
	return data
}

func (*VirtualTag) parseTextRecord(data []byte, i int) string {
	// Structure: [0xD1][0x01][payload_len][0x54][lang_len][lang...][text...]
	payloadLen := int(data[i+2])
	if data[i+3] != 0x54 { // Not a text record (type 'T')
		return ""
	}
	langLen := int(data[i+4])
	textStart := i + 5 + langLen
	textLen := payloadLen - 1 - langLen
	if textLen <= 0 || textStart+textLen > len(data) {
		return ""
	}
	// Collect text bytes until terminator
	text := make([]byte, 0, textLen)
	for j := textStart; j < textStart+textLen && j < len(data); j++ {
		if data[j] == 0xFE || data[j] == 0x00 {
			break
		}
		text = append(text, data[j])
	}
	return string(text)
}

// MIFARE authentication methods

// MIFAREKeyA is the key type constant for Key A
const MIFAREKeyA = 0x00

// MIFAREKeyB is the key type constant for Key B
const MIFAREKeyB = 0x01

// Authenticate authenticates a sector with the given key
// keyType: 0x00 for Key A, 0x01 for Key B
func (v *VirtualTag) Authenticate(sector int, keyType byte, key []byte) error {
	if !v.Present {
		return errors.New("tag not present")
	}

	if !v.isMIFARE() {
		return errors.New("authentication only supported for MIFARE tags")
	}

	if len(key) != 6 {
		return errors.New("MIFARE key must be 6 bytes")
	}

	// Check sector bounds
	maxSector := v.getMaxSector()
	if sector < 0 || sector >= maxSector {
		return fmt.Errorf("sector %d out of range (max %d)", sector, maxSector-1)
	}

	// Validate key against stored keys
	storedKeys, exists := v.sectorKeys[sector]
	if !exists {
		return fmt.Errorf("no keys configured for sector %d", sector)
	}

	var expectedKey []byte
	switch keyType {
	case MIFAREKeyA:
		expectedKey = storedKeys[0:6]
	case MIFAREKeyB:
		expectedKey = storedKeys[6:12]
	default:
		return fmt.Errorf("invalid key type: 0x%02X", keyType)
	}

	// Compare keys
	if !bytesEqual(key, expectedKey) {
		v.authenticatedSector = -1 // Clear authentication on failure
		return errors.New("authentication failed: incorrect key")
	}

	v.authenticatedSector = sector
	v.authenticatedKey = keyType
	return nil
}

// ResetAuthentication clears the authentication state
func (v *VirtualTag) ResetAuthentication() {
	v.authenticatedSector = -1
	v.authenticatedKey = 0
}

// IsAuthenticated returns true if authenticated to the given sector
func (v *VirtualTag) IsAuthenticated(sector int) bool {
	return v.authenticatedSector == sector
}

// GetAuthenticatedSector returns the currently authenticated sector (-1 if none)
func (v *VirtualTag) GetAuthenticatedSector() int {
	return v.authenticatedSector
}

// SetSectorKey sets custom keys for a sector
// keys should be 12 bytes: 6 bytes Key A + 6 bytes Key B
func (v *VirtualTag) SetSectorKey(sector int, keys []byte) error {
	if !v.isMIFARE() {
		return errors.New("sector keys only apply to MIFARE tags")
	}
	if len(keys) != 12 {
		return errors.New("keys must be 12 bytes (Key A + Key B)")
	}
	maxSector := v.getMaxSector()
	if sector < 0 || sector >= maxSector {
		return fmt.Errorf("sector %d out of range (max %d)", sector, maxSector-1)
	}
	v.sectorKeys[sector] = append([]byte{}, keys...)
	return nil
}

// Helper methods

func (v *VirtualTag) isMIFARE() bool {
	return v.Type == "MIFARE1K" || v.Type == "MIFARE4K"
}

func (v *VirtualTag) getMaxSector() int {
	switch v.Type {
	case "MIFARE1K":
		return 16
	case "MIFARE4K":
		return 40
	default:
		return 0
	}
}

func (v *VirtualTag) blockToSector(block int) int {
	switch v.Type {
	case "MIFARE1K":
		return block / 4
	case "MIFARE4K":
		if block < 128 {
			return block / 4
		}
		// Sectors 32-39 have 16 blocks each
		return 32 + (block-128)/16
	default:
		return 0
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
