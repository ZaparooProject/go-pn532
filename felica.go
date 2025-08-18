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
	"context"
	"errors"
	"fmt"
)

// FeliCa command constants based on JIS X 6319-4 specification
const (
	feliCaCmdPolling                = 0x00
	feliCaCmdReadWithoutEncryption  = 0x06
	feliCaCmdWriteWithoutEncryption = 0x08
	feliCaCmdAuthentication         = 0x0A
	feliCaCmdRequestResponse        = 0x04
	feliCaCmdRequestService         = 0x02
)

// FeliCa system codes
const (
	feliCaSystemCodeNDEF     = 0x12FC // NFC Forum Type 3 Tag
	feliCaSystemCodeCommon   = 0xFFFF // Common system code for polling
	feliCaSystemCodeWildcard = 0xFFFF // Wildcard for polling all cards
)

// FeliCa service codes for NDEF operations
const (
	feliCaServiceCodeNDEFRead  = 0x000B // NDEF read service
	feliCaServiceCodeNDEFWrite = 0x0009 // NDEF write service
)

// FeliCa constants
const (
	feliCaBlockSize = 16 // FeliCa uses 16-byte blocks
	feliCaIDmLength = 8  // IDm (Manufacture ID) is 8 bytes
	feliCaPMmLength = 8  // PMm (Manufacture Parameter) is 8 bytes
)

// FeliCaTag represents a FeliCa NFC tag implementing the Tag interface
// Field ordering optimized for memory alignment to reduce struct size from 96 to 80 bytes
type FeliCaTag struct {
	idm []byte
	pmm []byte
	BaseTag
	blockSize   int
	systemCode  uint16
	serviceCode uint16
}

// NewFeliCaTag creates a new FeliCa tag instance from polling response data
// targetData should contain the FeliCa polling response (POL_RES)
func NewFeliCaTag(device *Device, targetData []byte) (*FeliCaTag, error) {
	if len(targetData) < 18 {
		return nil, fmt.Errorf("FeliCa target data too short: %d bytes, need at least 18", len(targetData))
	}

	// Parse FeliCa polling response structure:
	// Byte 0: Response Code (0x01 for polling response)
	// Byte 1: IDm[0]
	// ...
	// Byte 8: IDm[7]
	// Byte 9: PMm[0]
	// ...
	// Byte 16: PMm[7]
	// Byte 17-18: System Code (optional, depends on response length)

	// Extract IDm (8 bytes) - starts at byte 1
	idm := make([]byte, feliCaIDmLength)
	copy(idm, targetData[1:9])

	// Extract PMm (8 bytes) - starts at byte 9
	pmm := make([]byte, feliCaPMmLength)
	copy(pmm, targetData[9:17])

	// Extract system code if present (last 2 bytes, big endian)
	systemCode := uint16(0xFFFF) // Default wildcard
	if len(targetData) >= 19 {
		systemCode = uint16(targetData[17])<<8 | uint16(targetData[18])
	}

	tag := &FeliCaTag{
		BaseTag: BaseTag{
			device:  device,
			tagType: TagTypeFeliCa,
			uid:     idm, // Use IDm as UID for FeliCa
			sak:     0,   // FeliCa doesn't use SAK
		},
		blockSize:   feliCaBlockSize,
		idm:         idm,
		pmm:         pmm,
		systemCode:  systemCode,
		serviceCode: feliCaServiceCodeNDEFRead, // Default to NDEF read
	}

	return tag, nil
}

// GetIDm returns the Manufacture ID (IDm) of the FeliCa tag
func (f *FeliCaTag) GetIDm() []byte {
	return f.idm
}

// GetPMm returns the Manufacture Parameter (PMm) of the FeliCa tag
func (f *FeliCaTag) GetPMm() []byte {
	return f.pmm
}

// GetSystemCode returns the current system code
func (f *FeliCaTag) GetSystemCode() uint16 {
	return f.systemCode
}

// SetSystemCode sets the system code for operations
func (f *FeliCaTag) SetSystemCode(systemCode uint16) {
	f.systemCode = systemCode
}

// GetServiceCode returns the current service code
func (f *FeliCaTag) GetServiceCode() uint16 {
	return f.serviceCode
}

// SetServiceCode sets the service code for operations
func (f *FeliCaTag) SetServiceCode(serviceCode uint16) {
	f.serviceCode = serviceCode
}

// ReadBlock reads a single block from the FeliCa tag
// For FeliCa, block numbers are 16-bit, but we use uint8 for interface compatibility
// TODO: Consider extending interface for 16-bit block addressing
func (f *FeliCaTag) ReadBlock(block uint8) ([]byte, error) {
	return f.ReadBlockExtended(uint16(block))
}

// ReadBlockExtended reads a single block using 16-bit block addressing
func (f *FeliCaTag) ReadBlockExtended(block uint16) ([]byte, error) {
	// FeliCa Read Without Encryption command structure:
	// Command: 0x06
	// IDm: 8 bytes (card identifier)
	// Service Count: 1 byte (number of services)
	// Service Code List: 2 bytes per service
	// Block Count: 1 byte (number of blocks to read)
	// Block List: 2 or 3 bytes per block depending on format

	cmd := []byte{feliCaCmdReadWithoutEncryption}

	// Add IDm (8 bytes)
	cmd = append(cmd, f.idm...)

	// Service count, service code, block count, and block list element
	cmd = append(cmd, 0x01, // Service count (1 service)
		byte(f.serviceCode&0xFF), byte((f.serviceCode>>8)&0xFF), // Service code (2 bytes, little endian)
		0x01, // Block count (1 block)
		// Block list element (3 bytes for 16-bit block addressing)
		// Format: 0x80 | (block >> 8), block & 0xFF, 0x00
		0x80|byte((block>>8)&0x0F), byte(block&0xFF), 0x00)

	// Send command via data exchange
	response, err := f.device.SendDataExchange(cmd)
	if err != nil {
		return nil, fmt.Errorf("FeliCa read command failed: %w", err)
	}

	// Check response format
	if len(response) < 12 {
		return nil, fmt.Errorf("FeliCa read response too short: %d bytes", len(response))
	}

	// Response format:
	// Response Code: 1 byte (0x07 for Read Without Encryption response)
	// IDm: 8 bytes
	// Status Flag 1: 1 byte
	// Status Flag 2: 1 byte
	// Block Data: 16 bytes per block

	if response[0] != 0x07 {
		return nil, fmt.Errorf("invalid FeliCa read response code: 0x%02X", response[0])
	}

	// Check status flags
	statusFlag1 := response[9]
	statusFlag2 := response[10]

	if statusFlag1 != 0x00 || statusFlag2 != 0x00 {
		return nil, fmt.Errorf("FeliCa read failed with status: 0x%02X%02X", statusFlag1, statusFlag2)
	}

	// Extract block data (starts at byte 11)
	if len(response) < 11+feliCaBlockSize {
		return nil, errors.New("FeliCa read response missing block data")
	}

	blockData := make([]byte, feliCaBlockSize)
	copy(blockData, response[11:11+feliCaBlockSize])

	return blockData, nil
}

// WriteBlock writes a single block to the FeliCa tag
// For FeliCa, block numbers are 16-bit, but we use uint8 for interface compatibility
func (f *FeliCaTag) WriteBlock(block uint8, data []byte) error {
	return f.WriteBlockExtended(uint16(block), data)
}

// WriteBlockExtended writes a single block using 16-bit block addressing
func (f *FeliCaTag) WriteBlockExtended(block uint16, data []byte) error {
	// Validate data length
	if len(data) != feliCaBlockSize {
		return fmt.Errorf("FeliCa block data must be exactly %d bytes, got %d", feliCaBlockSize, len(data))
	}

	// FeliCa Write Without Encryption command structure:
	// Command: 0x08
	// IDm: 8 bytes (card identifier)
	// Service Count: 1 byte (number of services)
	// Service Code List: 2 bytes per service
	// Block Count: 1 byte (number of blocks to write)
	// Block List: 2 or 3 bytes per block depending on format
	// Block Data: 16 bytes per block

	cmd := []byte{feliCaCmdWriteWithoutEncryption}

	// Add IDm (8 bytes)
	cmd = append(cmd, f.idm...)

	// Service count, service code, block count, and block list element
	cmd = append(cmd, 0x01, // Service count (1 service)
		byte(f.serviceCode&0xFF), byte((f.serviceCode>>8)&0xFF), // Service code (2 bytes, little endian)
		0x01, // Block count (1 block)
		// Block list element (3 bytes for 16-bit block addressing)
		// Format: 0x80 | (block >> 8), block & 0xFF, 0x00
		0x80|byte((block>>8)&0x0F), byte(block&0xFF), 0x00)

	// Add block data (16 bytes)
	cmd = append(cmd, data...)

	// Send command via data exchange
	response, err := f.device.SendDataExchange(cmd)
	if err != nil {
		return fmt.Errorf("FeliCa write command failed: %w", err)
	}

	// Check response format
	if len(response) < 11 {
		return fmt.Errorf("FeliCa write response too short: %d bytes", len(response))
	}

	// Response format:
	// Response Code: 1 byte (0x09 for Write Without Encryption response)
	// IDm: 8 bytes
	// Status Flag 1: 1 byte
	// Status Flag 2: 1 byte

	if response[0] != 0x09 {
		return fmt.Errorf("invalid FeliCa write response code: 0x%02X", response[0])
	}

	// Check status flags
	statusFlag1 := response[9]
	statusFlag2 := response[10]

	if statusFlag1 != 0x00 || statusFlag2 != 0x00 {
		return fmt.Errorf("FeliCa write failed with status: 0x%02X%02X", statusFlag1, statusFlag2)
	}

	return nil
}

// ReadNDEF reads NDEF data from the FeliCa tag
// Uses system code 0x12FC and service code 0x000B for NFC Forum Type 3 compliance
func (f *FeliCaTag) ReadNDEF() (*NDEFMessage, error) {
	// Switch to NDEF system code and read service code
	originalSystemCode := f.systemCode
	originalServiceCode := f.serviceCode

	f.systemCode = feliCaSystemCodeNDEF
	f.serviceCode = feliCaServiceCodeNDEFRead

	// Restore original codes when done
	defer func() {
		f.systemCode = originalSystemCode
		f.serviceCode = originalServiceCode
	}()

	// Step 1: Read Attribute Information Block (Block 0)
	aibData, err := f.ReadBlockExtended(0)
	if err != nil {
		return nil, fmt.Errorf("failed to read attribute information block: %w", err)
	}

	// Step 2: Validate AIB structure and checksum
	if len(aibData) < 16 {
		return nil, fmt.Errorf("attribute information block too short: %d bytes", len(aibData))
	}

	if !f.validateAIB(aibData) {
		return nil, errors.New("invalid attribute information block or checksum mismatch")
	}

	// Step 3: Check version and permissions
	version := aibData[0]
	if version != 0x10 {
		return nil, fmt.Errorf("unsupported NDEF version: 0x%02X", version)
	}

	rwFlag := aibData[10]
	// Note: rwFlag indicates read-only status, but we can still read NDEF regardless
	_ = rwFlag

	// Step 4: Extract NDEF length (Ln) - 3 bytes big endian at bytes 11-13
	ndefLength := uint32(aibData[11])<<16 | uint32(aibData[12])<<8 | uint32(aibData[13])

	if ndefLength == 0 {
		// Empty NDEF message
		return &NDEFMessage{Records: []NDEFRecord{}}, nil
	}

	// Step 5: Calculate blocks needed to read
	blocksNeeded := (ndefLength + feliCaBlockSize - 1) / feliCaBlockSize

	// Step 6: Read NDEF data blocks starting from Block 1
	if blocksNeeded > 0xFFFF {
		return nil, fmt.Errorf("NDEF data too large: requires %d blocks but maximum is %d", blocksNeeded, 0xFFFF)
	}
	ndefData := make([]byte, 0, blocksNeeded*feliCaBlockSize)

	for block := uint16(1); uint32(block) <= blocksNeeded; block++ {
		blockData, err := f.ReadBlockExtended(block)
		if err != nil {
			return nil, fmt.Errorf("failed to read NDEF block %d: %w", block, err)
		}
		ndefData = append(ndefData, blockData...)
	}

	// Step 7: Trim to actual NDEF length
	if len(ndefData) < int(ndefLength) {
		return nil, fmt.Errorf("read insufficient NDEF data: got %d bytes, expected %d", len(ndefData), ndefLength)
	}

	actualNdefData := ndefData[:ndefLength]

	// Step 8: Parse NDEF data using existing parser
	return ParseNDEFMessage(actualNdefData)
}

// WriteNDEF writes NDEF data to the FeliCa tag
// Uses system code 0x12FC and service code 0x0009 for NFC Forum Type 3 compliance
func (f *FeliCaTag) WriteNDEF(message *NDEFMessage) error {
	if message == nil {
		return errors.New("NDEF message cannot be nil")
	}

	// Switch to NDEF system code and write service code
	originalSystemCode, originalServiceCode := f.systemCode, f.serviceCode
	f.systemCode = feliCaSystemCodeNDEF
	defer f.restoreSystemCodes(originalSystemCode, originalServiceCode)

	return f.executeNDEFWrite(message)
}

// WriteNDEFWithContext writes NDEF data to the FeliCa tag with context support
func (f *FeliCaTag) WriteNDEFWithContext(_ context.Context, message *NDEFMessage) error {
	// TODO: Add context cancellation checks at appropriate points during the write operation
	return f.WriteNDEF(message)
}

// restoreSystemCodes restores the original system and service codes
func (f *FeliCaTag) restoreSystemCodes(systemCode, serviceCode uint16) {
	f.systemCode = systemCode
	f.serviceCode = serviceCode
}

// executeNDEFWrite performs the NDEF write operation
func (f *FeliCaTag) executeNDEFWrite(message *NDEFMessage) error {
	// Step 1: Read and validate current AIB
	currentAIB, err := f.readAndValidateAIB()
	if err != nil {
		return err
	}

	// Step 2: Validate write permissions
	if writeErr := f.validateWritePermissions(currentAIB); writeErr != nil {
		return writeErr
	}

	// Step 3: Build and validate NDEF data
	ndefData, err := f.buildAndValidateNDEFData(message, currentAIB)
	if err != nil {
		return err
	}

	// Step 4: Write NDEF data to blocks
	if err := f.writeNDEFDataToBlocks(ndefData); err != nil {
		return err
	}

	// Step 5: Update AIB with new length
	return f.updateAIBWithNDEFLength(currentAIB, len(ndefData))
}

// readAndValidateAIB reads and validates the Attribute Information Block
func (f *FeliCaTag) readAndValidateAIB() ([]byte, error) {
	f.serviceCode = feliCaServiceCodeNDEFRead
	currentAIB, err := f.ReadBlockExtended(0)
	if err != nil {
		return nil, fmt.Errorf("failed to read current attribute information block: %w", err)
	}

	if !f.validateAIB(currentAIB) {
		return nil, errors.New("current attribute information block is invalid")
	}

	return currentAIB, nil
}

// buildAndValidateNDEFData builds NDEF message data and validates size constraints
func (f *FeliCaTag) buildAndValidateNDEFData(message *NDEFMessage, currentAIB []byte) ([]byte, error) {
	var ndefData []byte
	if len(message.Records) > 0 {
		data, err := BuildNDEFMessageEx(message.Records)
		if err != nil {
			return nil, fmt.Errorf("failed to build NDEF message: %w", err)
		}
		ndefData = data
	}

	// Validate data size against tag capacity
	maxBlocks := uint16(currentAIB[3])<<8 | uint16(currentAIB[4])
	maxBytes := uint32(maxBlocks) * feliCaBlockSize
	if err := f.validateDataSize(ndefData, maxBytes); err != nil {
		return nil, err
	}

	return ndefData, nil
}

// writeNDEFDataToBlocks writes NDEF data to the FeliCa blocks
func (f *FeliCaTag) writeNDEFDataToBlocks(ndefData []byte) error {
	// Pad NDEF data to block boundary
	paddedLength := ((len(ndefData) + feliCaBlockSize - 1) / feliCaBlockSize) * feliCaBlockSize
	paddedData := make([]byte, paddedLength)
	copy(paddedData, ndefData)

	// Write NDEF data blocks
	f.serviceCode = feliCaServiceCodeNDEFWrite
	return f.writeNDEFBlocks(paddedData)
}

// updateAIBWithNDEFLength updates and writes the AIB with new NDEF length
func (f *FeliCaTag) updateAIBWithNDEFLength(currentAIB []byte, ndefDataLen int) error {
	if ndefDataLen < 0 || ndefDataLen > 0xFFFFFF {
		return fmt.Errorf("NDEF data length out of range: %d bytes (must be 0-16777215)", ndefDataLen)
	}

	newAIB := f.updateAIBWithLength(currentAIB, uint32(ndefDataLen))
	if err := f.WriteBlockExtended(0, newAIB); err != nil {
		return fmt.Errorf("failed to write updated attribute information block: %w", err)
	}

	return nil
}

// Polling performs a FeliCa polling operation to detect and initialize the tag
// This is a FeliCa-specific operation for card detection and system code discovery
func (f *FeliCaTag) Polling(systemCode uint16) error {
	// FeliCa Polling command structure:
	// Command: 0x00
	// System Code: 2 bytes (big endian)
	// Request Code: 1 byte (0x01 for system code and time slot)
	// Time Slot: 1 byte (0x03 for maximum time slots)

	cmd := []byte{feliCaCmdPolling}

	// Add system code (2 bytes, big endian), request code, and time slot
	cmd = append(cmd,
		byte((systemCode>>8)&0xFF), byte(systemCode&0xFF), // System code (2 bytes, big endian)
		0x01, // Request code (0x01)
		0x03, // Time slot (0x03 for maximum slots)
	)

	// Send command via data exchange
	response, err := f.device.SendDataExchange(cmd)
	if err != nil {
		return fmt.Errorf("FeliCa polling command failed: %w", err)
	}

	// Check response format
	if len(response) < 19 {
		return fmt.Errorf("FeliCa polling response too short: %d bytes", len(response))
	}

	// Response format:
	// Response Code: 1 byte (0x01 for polling response)
	// IDm: 8 bytes
	// PMm: 8 bytes
	// Request Data: variable (2 bytes system code if requested)

	if response[0] != 0x01 {
		return fmt.Errorf("invalid FeliCa polling response code: 0x%02X", response[0])
	}

	// Update IDm and PMm from polling response
	copy(f.idm, response[1:9])
	copy(f.pmm, response[9:17])

	// Update UID in BaseTag
	f.uid = make([]byte, len(f.idm))
	copy(f.uid, f.idm)

	// Update system code if present in response
	if len(response) >= 19 {
		responseSystemCode := uint16(response[17])<<8 | uint16(response[18])
		f.systemCode = responseSystemCode
	}

	return nil
}

// RequestService requests service information from the FeliCa tag
// This is used to check if specific service codes are available
func (f *FeliCaTag) RequestService(serviceCodes []uint16) ([]byte, error) {
	if len(serviceCodes) == 0 || len(serviceCodes) > 32 {
		return nil, fmt.Errorf("invalid service code count: %d (must be 1-32)", len(serviceCodes))
	}

	// FeliCa Request Service command structure:
	// Command: 0x02
	// IDm: 8 bytes (card identifier)
	// Node Count: 1 byte (number of service codes)
	// Node Code List: 2 bytes per service code

	cmd := []byte{feliCaCmdRequestService}

	// Add IDm (8 bytes)
	cmd = append(cmd, f.idm...)

	// Node count (number of service codes)
	cmd = append(cmd, byte(len(serviceCodes)))

	// Add service codes (2 bytes each, little endian)
	for _, serviceCode := range serviceCodes {
		cmd = append(cmd, byte(serviceCode&0xFF), byte((serviceCode>>8)&0xFF))
	}

	// Send command via data exchange
	response, err := f.device.SendDataExchange(cmd)
	if err != nil {
		return nil, fmt.Errorf("FeliCa request service command failed: %w", err)
	}

	// Check response format
	expectedMinLen := 10 + len(serviceCodes)*2
	if len(response) < expectedMinLen {
		return nil, fmt.Errorf("FeliCa request service response too short: %d bytes", len(response))
	}

	// Response format:
	// Response Code: 1 byte (0x03 for Request Service response)
	// IDm: 8 bytes
	// Node Count: 1 byte
	// Node Key Version List: 2 bytes per service code

	if response[0] != 0x03 {
		return nil, fmt.Errorf("invalid FeliCa request service response code: 0x%02X", response[0])
	}

	nodeCount := response[9]
	if int(nodeCount) != len(serviceCodes) {
		return nil, fmt.Errorf("service count mismatch: expected %d, got %d", len(serviceCodes), nodeCount)
	}

	// Extract node key version list
	nodeKeyVersions := make([]byte, len(serviceCodes)*2)
	copy(nodeKeyVersions, response[10:10+len(serviceCodes)*2])

	return nodeKeyVersions, nil
}

// validateWritePermissions checks if the tag allows NDEF writing
func (*FeliCaTag) validateWritePermissions(aib []byte) error {
	// Check write permissions
	writeFlag := aib[9]
	if writeFlag == 0x0F {
		return errors.New("tag is write-protected")
	}

	rwFlag := aib[10]
	if rwFlag == 0x01 {
		return errors.New("NDEF data area is read-only")
	}

	return nil
}

// validateDataSize checks if NDEF data fits within tag capacity
func (*FeliCaTag) validateDataSize(ndefData []byte, maxBytes uint32) error {
	if len(ndefData) > int(maxBytes) {
		return fmt.Errorf("NDEF message too large: %d bytes, max %d bytes", len(ndefData), maxBytes)
	}

	if len(ndefData) > 0xFFFFFF {
		return fmt.Errorf("NDEF data too large: %d bytes exceeds 24-bit limit", len(ndefData))
	}

	return nil
}

// writeNDEFBlocks writes the NDEF data blocks to the tag
func (f *FeliCaTag) writeNDEFBlocks(paddedData []byte) error {
	blocksToWrite := len(paddedData) / feliCaBlockSize
	if blocksToWrite > 0xFFFE {
		return fmt.Errorf("NDEF data too large: requires %d blocks but maximum is %d", blocksToWrite, 0xFFFE)
	}

	for block := 0; block < blocksToWrite; block++ {
		blockData := paddedData[block*feliCaBlockSize : (block+1)*feliCaBlockSize]
		if block >= 0xFFFE {
			return fmt.Errorf("block index too large: %d", block)
		}
		blockIndex := uint16(block) + 1 //nolint:gosec // Safe conversion checked above, blocks start at 1
		writeErr := f.WriteBlockExtended(blockIndex, blockData)
		if writeErr != nil {
			return fmt.Errorf("failed to write NDEF block %d: %w", block+1, writeErr)
		}
	}

	return nil
}

// updateAIBWithLength creates a new AIB with updated NDEF length and checksum
func (*FeliCaTag) updateAIBWithLength(currentAIB []byte, ndefLength uint32) []byte {
	newAIB := make([]byte, 16)
	copy(newAIB, currentAIB)

	// Update NDEF length (Ln) - 3 bytes big endian at bytes 11-13
	newAIB[11] = byte((ndefLength >> 16) & 0xFF)
	newAIB[12] = byte((ndefLength >> 8) & 0xFF)
	newAIB[13] = byte(ndefLength & 0xFF)

	// Recalculate checksum for first 14 bytes
	var sum uint16
	for i := 0; i < 14; i++ {
		sum += uint16(newAIB[i])
	}
	newAIB[14] = byte((sum >> 8) & 0xFF)
	newAIB[15] = byte(sum & 0xFF)

	return newAIB
}

// validateAIB validates the Attribute Information Block checksum
// The checksum is a 16-bit sum of the first 14 bytes
func (*FeliCaTag) validateAIB(aib []byte) bool {
	if len(aib) < 16 {
		return false
	}

	// Calculate checksum of first 14 bytes
	var sum uint16
	for i := 0; i < 14; i++ {
		sum += uint16(aib[i])
	}

	// Extract stored checksum (big endian, bytes 14-15)
	storedChecksum := uint16(aib[14])<<8 | uint16(aib[15])

	return sum == storedChecksum
}

// DebugInfo returns detailed debug information about the FeliCa tag
func (f *FeliCaTag) DebugInfo() string {
	return f.DebugInfoWithNDEF(f)
}

// WriteText writes a simple text record to the FeliCa tag
func (f *FeliCaTag) WriteText(text string) error {
	message := &NDEFMessage{
		Records: []NDEFRecord{
			{
				Type: NDEFTypeText,
				Text: text,
			},
		},
	}

	return f.WriteNDEF(message)
}
