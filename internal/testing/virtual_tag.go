// Copyright (C) 2017 Bitnami
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package testing

import (
	"encoding/hex"
	"fmt"
)

// VirtualTag represents a simulated NFC tag for testing
type VirtualTag struct {
	Type     string
	UID      []byte
	Memory   [][]byte // Block-based memory layout
	Present  bool     // Whether the tag is currently present
	ndefData []byte   // Stored NDEF data for easy access
}

// NewVirtualNTAG213 creates a virtual NTAG213 tag with default content
func NewVirtualNTAG213(uid []byte) *VirtualTag {
	if uid == nil {
		uid = TestNTAG213UID
	}
	
	tag := &VirtualTag{
		Type:    "NTAG213",
		UID:     uid,
		Memory:  make([][]byte, 45), // NTAG213 has 45 blocks (180 bytes)
		Present: true,
	}
	
	// Initialize with default NTAG213 memory layout
	tag.initNTAG213Memory()
	
	// Set default NDEF message: "Hello World"
	tag.SetNDEFText("Hello World")
	
	return tag
}

// NewVirtualMIFARE1K creates a virtual MIFARE Classic 1K tag
func NewVirtualMIFARE1K(uid []byte) *VirtualTag {
	if uid == nil {
		uid = TestMIFARE1KUID
	}
	
	tag := &VirtualTag{
		Type:    "MIFARE1K",
		UID:     uid,
		Memory:  make([][]byte, 64), // MIFARE 1K has 64 blocks (1024 bytes)
		Present: true,
	}
	
	// Initialize with default MIFARE memory layout
	tag.initMIFARE1KMemory()
	
	return tag
}

// NewVirtualMIFARE4K creates a virtual MIFARE Classic 4K tag
func NewVirtualMIFARE4K(uid []byte) *VirtualTag {
	if uid == nil {
		uid = TestMIFARE4KUID
	}
	
	tag := &VirtualTag{
		Type:    "MIFARE4K",
		UID:     uid,
		Memory:  make([][]byte, 256), // MIFARE 4K has 256 blocks (4096 bytes)
		Present: true,
	}
	
	// Initialize with default MIFARE memory layout
	tag.initMIFARE4KMemory()
	
	return tag
}

// GetUIDString returns the UID as a hex string
func (v *VirtualTag) GetUIDString() string {
	return hex.EncodeToString(v.UID)
}

// ReadBlock reads a specific memory block
func (v *VirtualTag) ReadBlock(block int) ([]byte, error) {
	if !v.Present {
		return nil, fmt.Errorf("tag not present")
	}
	
	if block < 0 || block >= len(v.Memory) {
		return nil, fmt.Errorf("block %d out of range", block)
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
		return fmt.Errorf("tag not present")
	}
	
	if block < 0 || block >= len(v.Memory) {
		return fmt.Errorf("block %d out of range", block)
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
	ndefRecord := []byte{
		0xD1,       // Header: MB=1, ME=1, CF=0, SR=1, IL=0, TNF=1 (Well Known)
		0x01,       // Type Length: 1 byte
		byte(len(textBytes) + 3), // Payload Length: language code (2) + encoding (1) + text
		0x54,       // Type: "T" for Text
		0x02,       // Language code length
		0x65, 0x6E, // Language code: "en"
		// Text follows
	}
	ndefRecord = append(ndefRecord, textBytes...)
	
	// NDEF message wrapper
	ndefMessage := []byte{
		0x03,                    // NDEF Message TLV
		byte(len(ndefRecord)),   // Length
	}
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
	// Block 0: UID and BCC (read-only)
	v.Memory[0] = make([]byte, 16)
	copy(v.Memory[0][:len(v.UID)], v.UID)
	
	// Block 1: More UID (read-only)
	v.Memory[1] = make([]byte, 16)
	
	// Block 2: Lock bytes and CC (Capability Container)
	v.Memory[2] = []byte{0x00, 0x00, 0xE1, 0x10, 0x12, 0x00, 0x01, 0x03, 0xA0, 0x10, 0x44, 0x03, 0x00, 0x00, 0x00, 0x00}
	
	// Block 3: CC continued
	v.Memory[3] = make([]byte, 16)
	
	// Blocks 4-39: User data (where NDEF goes)
	for i := 4; i < 40; i++ {
		v.Memory[i] = make([]byte, 16)
	}
	
	// Blocks 40-44: Configuration and lock (read-only for most)
	for i := 40; i < 45; i++ {
		v.Memory[i] = make([]byte, 16)
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
	
	// Set default keys and access bits for sector trailers
	for sector := 0; sector < 16; sector++ {
		trailerBlock := sector*4 + 3
		// Default MIFARE key: FF FF FF FF FF FF
		v.Memory[trailerBlock] = []byte{
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // Key A
			0xFF, 0x07, 0x80, 0x69,             // Access bits
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // Key B
		}
	}
}

func (v *VirtualTag) initMIFARE4KMemory() {
	// Similar to 1K but with more sectors
	v.Memory[0] = make([]byte, 16)
	copy(v.Memory[0][:len(v.UID)], v.UID)
	
	for i := 1; i < 256; i++ {
		v.Memory[i] = make([]byte, 16)
	}
	
	// Set default keys for all sector trailers
	// Sectors 0-31 have 4 blocks each, sectors 32-39 have 16 blocks each
	for sector := 0; sector < 32; sector++ {
		trailerBlock := sector*4 + 3
		v.Memory[trailerBlock] = []byte{
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // Key A
			0xFF, 0x07, 0x80, 0x69,             // Access bits
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // Key B
		}
	}
	for sector := 32; sector < 40; sector++ {
		trailerBlock := 128 + (sector-32)*16 + 15
		v.Memory[trailerBlock] = []byte{
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // Key A
			0xFF, 0x07, 0x80, 0x69,             // Access bits
			0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // Key B
		}
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
	if len(v.ndefData) > 144 { // 36 blocks * 4 bytes usable per block
		return fmt.Errorf("NDEF data too large for NTAG213")
	}
	
	// Write NDEF data starting at block 4
	dataOffset := 0
	for block := 4; block < 40 && dataOffset < len(v.ndefData); block++ {
		blockData := make([]byte, 16)
		endOffset := dataOffset + 16
		if endOffset > len(v.ndefData) {
			endOffset = len(v.ndefData)
		}
		copy(blockData, v.ndefData[dataOffset:endOffset])
		v.Memory[block] = blockData
		dataOffset += 16
	}
	
	return nil
}

func (v *VirtualTag) extractNDEFTextFromNTAG() string {
	// Simple NDEF text extraction - look for text record in user data area
	for block := 4; block < 40; block++ {
		if v.Memory[block] == nil {
			continue
		}
		
		// Look for NDEF Text record header (0xD1, 0x01)
		for i := 0; i < len(v.Memory[block])-1; i++ {
			if v.Memory[block][i] == 0xD1 && v.Memory[block][i+1] == 0x01 {
				// Found text record, try to extract text
				if i+7 < len(v.Memory[block]) {
					// Skip header, type length, payload length, type, language encoding byte, language code (2 bytes)
					// Structure: [0xD1][0x01][payload_len][0x54][0x02][0x65][0x6E][text...]
					textStart := i + 7
					textData := []byte{}
					
					// Collect text bytes from this and subsequent blocks
					for b := block; b < 40; b++ {
						if v.Memory[b] == nil {
							break
						}
						
						start := 0
						if b == block {
							start = textStart
						}
						
						for j := start; j < len(v.Memory[b]); j++ {
							if v.Memory[b][j] == 0xFE || v.Memory[b][j] == 0x00 {
								// End of NDEF or null terminator
								return string(textData)
							}
							textData = append(textData, v.Memory[b][j])
						}
					}
					
					return string(textData)
				}
			}
		}
	}
	
	return ""
}