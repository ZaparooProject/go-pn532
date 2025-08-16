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
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// By default we only support MIFARE Classic tags with NDEF formatted data
// which uses a pre-shared standard auth key:
// [0xD3, 0xF7, 0xD3, 0xF7, 0xD3, 0xF7]
//
// This key is used on sector 1 and/or greater. Sector 0 is reserved for the
// MAD (MIFARE Application Directory) and uses a different shared key, but we
// don't care about implementing this.
//
// Additionally that means we should only use sector 1 and above for reading
// and writing our own data.
//
// MIFARE Classic tags may ship blank using the default key:
// [0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF]
//
// Before they work with NDEF data, the tag must also be intialized to use
// the standard NDEF auth key.

var (
	// NDEF standard key for sector 1 and above
	ndefKeyTemplate = []byte{0xD3, 0xF7, 0xD3, 0xF7, 0xD3, 0xF7}

	// Common alternative keys to try
	commonKeys = [][]byte{
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, // Default transport key
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // All zeros
		{0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5}, // MAD key
		{0xB0, 0xB1, 0xB2, 0xB3, 0xB4, 0xB5}, // Common alternative
		{0xA3, 0x96, 0xEF, 0xA4, 0xE2, 0x4F}, // FM11RF08S universal backdoor key
	}

	// Chinese clone unlock commands
	chineseCloneUnlock7Bit = byte(0x40)
	chineseCloneUnlock8Bit = byte(0x43)
)

// MIFARE commands
const (
	mifareCmdAuth  = 0x60
	mifareCmdRead  = 0x30
	mifareCmdWrite = 0xA0
)

// MIFARE memory structure
const (
	mifareBlockSize         = 16 // 16 bytes per block
	mifareSectorSize        = 4  // 4 blocks per sector
	mifareManufacturerBlock = 0  // Manufacturer block
	mifareKeySize           = 6  // 6 bytes per key
)

// Key types
const (
	MIFAREKeyA = 0x00
	MIFAREKeyB = 0x01
)

// Retry levels (progressive recovery)
type retryLevel int

const (
	retryLight    retryLevel = iota // Simple retry with delay
	retryModerate                   // Halt/wake sequence
	retryHeavy                      // RF field reset
	retryNuclear                    // Complete PN532 reinitialization
)

// Authentication timing metrics
type authTiming struct {
	attempts []time.Duration
	mutex    sync.RWMutex
}

func (at *authTiming) add(duration time.Duration) {
	at.mutex.Lock()
	defer at.mutex.Unlock()
	at.attempts = append(at.attempts, duration)
	if len(at.attempts) > 20 {
		at.attempts = at.attempts[1:]
	}
}

func (at *authTiming) getVariance() time.Duration {
	at.mutex.RLock()
	defer at.mutex.RUnlock()
	if len(at.attempts) < 2 {
		return 0
	}

	minVal, maxVal := at.attempts[0], at.attempts[0]
	for _, d := range at.attempts[1:] {
		if d < minVal {
			minVal = d
		}
		if d > maxVal {
			maxVal = d
		}
	}
	return maxVal - minVal
}

// secureKey manages MIFARE keys with automatic zeroing
type secureKey struct {
	data [6]byte
}

// newSecureKey creates a secure key from template
func newSecureKey(template []byte) *secureKey {
	if len(template) != 6 {
		return nil
	}
	sk := &secureKey{}
	copy(sk.data[:], template)
	return sk
}

// bytes returns a copy of the key data (caller must zero it)
func (sk *secureKey) bytes() []byte {
	result := make([]byte, 6)
	copy(result, sk.data[:])
	return result
}

// MIFAREConfig holds all configurable timing parameters for MIFARE operations
type MIFAREConfig struct {
	RetryConfig   *RetryConfig  // Retry backoff configuration
	HardwareDelay time.Duration // Hardware timing delays (reinitialization, tag processing)
}

// DefaultMIFAREConfig returns production-safe MIFARE configuration
func DefaultMIFAREConfig() *MIFAREConfig {
	return &MIFAREConfig{
		RetryConfig: &RetryConfig{
			MaxAttempts:       5,
			InitialBackoff:    200 * time.Millisecond,
			MaxBackoff:        4 * time.Second,
			BackoffMultiplier: 2.0,
			Jitter:            0.1,
			RetryTimeout:      30 * time.Second,
		},
		HardwareDelay: 50 * time.Millisecond,
	}
}

// MIFARETag represents a MIFARE Classic tag
type MIFARETag struct {
	ndefKey *secureKey
	config  *MIFAREConfig
	BaseTag
	timing          authTiming
	lastAuthSector  int
	authMutex       sync.RWMutex
	lastAuthKeyType byte
}

// NewMIFARETag creates a new MIFARE tag instance
func NewMIFARETag(device *Device, uid []byte, sak byte) *MIFARETag {
	tag := &MIFARETag{
		ndefKey: newSecureKey(ndefKeyTemplate),
		BaseTag: BaseTag{
			tagType: TagTypeMIFARE,
			uid:     uid,
			device:  device,
			sak:     sak,
		},
		lastAuthSector: -1, // Not authenticated initially
		config:         DefaultMIFAREConfig(),
	}

	return tag
}

// SetConfig allows runtime configuration of MIFARE behavior for testing
func (t *MIFARETag) SetConfig(config *MIFAREConfig) {
	if config != nil {
		t.config = config
	}
}

// SetRetryConfig allows runtime configuration of retry behavior for testing
func (t *MIFARETag) SetRetryConfig(config *RetryConfig) {
	if config != nil {
		t.config.RetryConfig = config
	}
}

// authenticateNDEF authenticates to a sector using NDEF standard key with robust retry
func (t *MIFARETag) authenticateNDEF(sector uint8, keyType byte) error {
	if t.ndefKey == nil {
		return errors.New("NDEF key not available")
	}

	key := t.ndefKey.bytes()
	err := t.AuthenticateRobust(sector, keyType, key)

	// SECURITY: Zero key copy after use
	for i := range key {
		key[i] = 0
	}

	return err
}

// ReadBlockAuto reads a block with automatic authentication using the key provider
func (t *MIFARETag) ReadBlockAuto(block uint8) ([]byte, error) {
	sector := block / mifareSectorSize

	// SECURITY: Thread-safe authentication state checking
	t.authMutex.RLock()
	needAuth := t.lastAuthSector != int(sector)
	t.authMutex.RUnlock()

	if needAuth {
		// Try Key A first, then Key B
		err := t.authenticateNDEF(sector, MIFAREKeyA)
		if err != nil {
			// Try Key B
			err = t.authenticateNDEF(sector, MIFAREKeyB)
			if err != nil {
				return nil, fmt.Errorf("failed to authenticate to sector %d: %w", sector, err)
			}
		}
	}

	return t.ReadBlock(block)
}

// WriteBlockAuto writes a block with automatic authentication using the key provider
func (t *MIFARETag) WriteBlockAuto(block uint8, data []byte) error {
	sector := block / mifareSectorSize

	// Check if we need to authenticate
	if t.lastAuthSector != int(sector) {
		// For write operations, typically Key B is required (but this depends on access bits)
		// Try Key B first, then Key A
		err := t.authenticateNDEF(sector, MIFAREKeyB)
		if err != nil {
			// Try Key A
			err = t.authenticateNDEF(sector, MIFAREKeyA)
			if err != nil {
				return fmt.Errorf("failed to authenticate to sector %d: %w", sector, err)
			}
		}
	}

	return t.WriteBlock(block, data)
}

// ReadBlock reads a block from the MIFARE tag
func (t *MIFARETag) ReadBlock(block uint8) ([]byte, error) {
	// Check if we need to authenticate to this sector
	sector := int(block / mifareSectorSize)
	t.authMutex.RLock()
	authenticated := t.lastAuthSector == sector
	t.authMutex.RUnlock()

	if !authenticated {
		return nil, fmt.Errorf("not authenticated to sector %d (block %d)", sector, block)
	}

	// Send read command
	data, err := t.device.SendDataExchange([]byte{mifareCmdRead, block})
	if err != nil {
		return nil, fmt.Errorf("failed to read block %d: %w", block, err)
	}

	// MIFARE Classic returns 16 bytes on read
	if len(data) < mifareBlockSize {
		return nil, fmt.Errorf("invalid read response length: %d", len(data))
	}

	return data[:mifareBlockSize], nil
}

// ReadBlockDirect reads a block directly without authentication (for clone tags)
func (t *MIFARETag) ReadBlockDirect(block uint8) ([]byte, error) {
	// Send read command directly
	data, err := t.device.SendDataExchange([]byte{mifareCmdRead, block})
	if err != nil {
		// If we get a timeout error, try InCommunicateThru as fallback
		if strings.Contains(err.Error(), "data exchange error: 01") {
			return t.readBlockCommunicateThru(block)
		}
		return nil, fmt.Errorf("failed to read block %d: %w", block, err)
	}

	// MIFARE Classic returns 16 bytes on read
	if len(data) < mifareBlockSize {
		return nil, fmt.Errorf("invalid read response length: %d", len(data))
	}

	return data[:mifareBlockSize], nil
}

// WriteBlock writes a block to the MIFARE tag
func (t *MIFARETag) WriteBlock(block uint8, data []byte) error {
	// Validate data size
	if len(data) != mifareBlockSize {
		return fmt.Errorf("invalid block size: expected %d, got %d", mifareBlockSize, len(data))
	}

	// Check if we need to authenticate to this sector
	sector := int(block / mifareSectorSize)
	if t.lastAuthSector != sector {
		return fmt.Errorf("not authenticated to sector %d (block %d)", sector, block)
	}

	// Don't allow writing to manufacturer block
	if block == mifareManufacturerBlock {
		return errors.New("cannot write to manufacturer block")
	}

	// Send write command
	cmd := []byte{mifareCmdWrite, block}
	cmd = append(cmd, data...)

	_, err := t.device.SendDataExchange(cmd)
	if err != nil {
		return fmt.Errorf("failed to write block %d: %w", block, err)
	}

	return nil
}

// WriteBlockDirect writes a block directly without authentication (for clone tags)
func (t *MIFARETag) WriteBlockDirect(block uint8, data []byte) error {
	// Validate data size
	if len(data) != mifareBlockSize {
		return fmt.Errorf("invalid block size: expected %d, got %d", mifareBlockSize, len(data))
	}

	// Don't allow writing to manufacturer block
	if block == mifareManufacturerBlock {
		return errors.New("cannot write to manufacturer block")
	}

	// First, try to read the block to see if the tag is responsive
	_, err := t.ReadBlockDirect(block)
	if err != nil {
		// If we can't even read, the tag might not support direct access at all
		return fmt.Errorf("clone tag does not support direct block access: %w", err)
	}

	// Send write command directly
	cmd := []byte{mifareCmdWrite, block}
	cmd = append(cmd, data...)

	_, err = t.device.SendDataExchange(cmd)
	if err != nil {
		// Try alternative approach - some clones might need different handling
		return t.writeBlockDirectAlternative(block, data, err)
	}

	return nil
}

// writeBlockDirectAlternative tries alternative methods for clone tags that don't respond to standard writes
func (t *MIFARETag) writeBlockDirectAlternative(block uint8, data []byte, originalErr error) error {
	// Check if the original error was a timeout (0x01)
	if strings.Contains(originalErr.Error(), "data exchange error: 01") {
		// Try using InCommunicateThru instead of InDataExchange
		// Some clone tags might respond better to raw communication
		err := t.writeBlockCommunicateThru(block, data)
		if err == nil {
			return nil
		}

		// If InCommunicateThru also fails, this tag may not support writing
		return errors.New("tag does not support writing: this tag may be read-only or have limited write functionality")
	}

	// For other errors, return the original error
	return fmt.Errorf("failed to write block %d: %w", block, originalErr)
}

// writeBlockCommunicateThru tries to write a block using InCommunicateThru instead of InDataExchange
func (t *MIFARETag) writeBlockCommunicateThru(block uint8, data []byte) error {
	// Validate data size
	if len(data) != mifareBlockSize {
		return fmt.Errorf("invalid block size: expected %d, got %d", mifareBlockSize, len(data))
	}

	// Don't allow writing to manufacturer block
	if block == mifareManufacturerBlock {
		return errors.New("cannot write to manufacturer block")
	}

	// Build MIFARE write command
	cmd := []byte{mifareCmdWrite, block}
	cmd = append(cmd, data...)

	// Use SendRawCommand instead of SendDataExchange
	_, err := t.device.SendRawCommand(cmd)
	if err != nil {
		return fmt.Errorf("raw write command failed: %w", err)
	}

	return nil
}

// readBlockCommunicateThru tries to read a block using InCommunicateThru instead of InDataExchange
func (t *MIFARETag) readBlockCommunicateThru(block uint8) ([]byte, error) {
	// Build MIFARE read command
	cmd := []byte{mifareCmdRead, block}

	// Use SendRawCommand instead of SendDataExchange
	data, err := t.device.SendRawCommand(cmd)
	if err != nil {
		return nil, fmt.Errorf("raw read command failed: %w", err)
	}

	// MIFARE Classic returns 16 bytes on read
	if len(data) < mifareBlockSize {
		return nil, fmt.Errorf("invalid read response length: %d", len(data))
	}

	return data[:mifareBlockSize], nil
}

// ReadNDEF reads NDEF data from the MIFARE tag using bulk sector reads
func (t *MIFARETag) ReadNDEF() (*NDEFMessage, error) {
	maxSectors, initialCapacity := t.getTagCapacityParams()
	data := make([]byte, 0, initialCapacity)

	for sector := uint8(1); sector < maxSectors; sector++ {
		if err := t.authenticateSector(sector); err != nil {
			if sector == 1 {
				return nil, fmt.Errorf("failed to read NDEF data: tag may not be NDEF formatted: %w", err)
			}
			break
		}

		sectorData, foundEnd := t.readSectorData(sector)
		if len(sectorData) > 0 {
			data = append(data, sectorData...)
		}

		readState := ndefReadContinue
		if foundEnd {
			readState = ndefReadFoundEnd
		}
		if t.shouldStopReading(sectorData, readState, data) {
			break
		}
	}

	return ParseNDEFMessage(data)
}

func (t *MIFARETag) getTagCapacityParams() (maxSectors uint8, initialCapacity int) {
	if t.IsMIFARE4K() {
		return 40, 255 * mifareBlockSize // 4K tag has 40 sectors
	}
	return 16, 64 * mifareBlockSize // 1K tag has 16 sectors
}

func (t *MIFARETag) authenticateSector(sector uint8) error {
	// Try to authenticate to the sector with Key A
	if err := t.authenticateNDEF(sector, MIFAREKeyA); err != nil {
		// Try Key B if Key A failed
		return t.authenticateNDEF(sector, MIFAREKeyB)
	}
	return nil
}

type ndefReadState int

const (
	ndefReadContinue ndefReadState = iota
	ndefReadFoundEnd
)

func (*MIFARETag) shouldStopReading(sectorData []byte, readState ndefReadState, allData []byte) bool {
	// Check if we found the NDEF end marker
	if readState == ndefReadFoundEnd || bytes.Contains(allData, ndefEnd) {
		return true
	}
	// Stop if sector was empty
	return len(sectorData) == 0 || isEmptyData(sectorData)
}

// readSectorData reads all data blocks in a sector (excluding the trailer)
// Returns the data and whether an NDEF end marker was found
func (t *MIFARETag) readSectorData(sector uint8) ([]byte, bool) {
	startBlock := sector * mifareSectorSize
	endBlock := startBlock + mifareSectorSize - 1 // -1 to exclude trailer

	data := make([]byte, 0, (mifareSectorSize-1)*mifareBlockSize)
	foundEnd := false

	for block := startBlock; block < endBlock; block++ {
		blockData, err := t.ReadBlock(block)
		if err != nil {
			break
		}

		data = append(data, blockData...)

		// Check for NDEF end marker
		if bytes.Contains(blockData, ndefEnd) {
			foundEnd = true
			break
		}
	}

	return data, foundEnd
}

// isEmptyData checks if the data is all zeros
func isEmptyData(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

// WriteNDEF writes NDEF data to the MIFARE tag
func (t *MIFARETag) WriteNDEF(message *NDEFMessage) error {
	if len(message.Records) == 0 {
		return errors.New("no NDEF records to write")
	}

	data, err := BuildNDEFMessageEx(message.Records)
	if err != nil {
		return fmt.Errorf("failed to build NDEF message: %w", err)
	}

	authResult, err := t.authenticateForNDEF()
	if err != nil {
		return err
	}

	if err := t.validateNDEFSize(data); err != nil {
		return err
	}

	if authResult.isBlank {
		if err := t.formatForNDEFWithKey(authResult.blankKey); err != nil {
			return fmt.Errorf("failed to format tag for NDEF: %w", err)
		}
	}

	if err := t.writeNDEFData(data); err != nil {
		return err
	}

	return t.clearRemainingBlocks(t.calculateNextBlock(len(data)))
}

type authenticationResult struct {
	blankKey        []byte
	isBlank         bool
	isNDEFFormatted bool
}

func (t *MIFARETag) authenticateForNDEF() (*authenticationResult, error) {
	result := &authenticationResult{}

	// First try NDEF key for already-formatted tags to avoid state corruption
	ndefKeyBytes := t.ndefKey.bytes()

	// Try Key A first
	err := t.AuthenticateRobust(1, MIFAREKeyA, ndefKeyBytes)
	if err == nil {
		// SECURITY: Clear sensitive key data after use
		for i := range ndefKeyBytes {
			ndefKeyBytes[i] = 0
		}
		result.isNDEFFormatted = true
		return result, nil
	}

	// Try Key B if Key A failed
	err = t.AuthenticateRobust(1, MIFAREKeyB, ndefKeyBytes)
	if err == nil {
		// SECURITY: Clear sensitive key data after use
		for i := range ndefKeyBytes {
			ndefKeyBytes[i] = 0
		}
		result.isNDEFFormatted = true
		return result, nil
	}

	// SECURITY: Clear sensitive key data after use
	for i := range ndefKeyBytes {
		ndefKeyBytes[i] = 0
	}

	// If NDEF key failed, try common keys for blank tags
	for _, key := range commonKeys {
		// Try Key A first
		err := t.AuthenticateRobust(1, MIFAREKeyA, key)
		if err == nil {
			result.isBlank = true
			result.blankKey = key
			return result, nil
		}
		// Try Key B if Key A failed
		err = t.AuthenticateRobust(1, MIFAREKeyB, key)
		if err == nil {
			result.isBlank = true
			result.blankKey = key
			return result, nil
		}
	}

	if !result.isBlank && !result.isNDEFFormatted {
		return nil, errors.New("cannot authenticate to tag - it may use custom keys, be protected, " +
			"or be a non-standard tag. Supported keys: default (blank), NDEF standard")
	}

	return result, nil
}

func (t *MIFARETag) validateNDEFSize(data []byte) error {
	// Determine max blocks based on card type
	var maxBlocks int
	if t.IsMIFARE4K() {
		maxBlocks = 255 // 4K card has 255 blocks (0-254)
	} else {
		maxBlocks = 64 // 1K card has 64 blocks (0-63)
	}

	dataBlocks := 0
	for i := 4; i < maxBlocks; i++ {
		if i%4 != 3 { // Skip sector trailers
			dataBlocks++
		}
	}
	maxDataSize := dataBlocks * mifareBlockSize

	if len(data) > maxDataSize {
		return fmt.Errorf("NDEF message too large: %d bytes, max %d bytes", len(data), maxDataSize)
	}
	return nil
}

func (t *MIFARETag) writeNDEFData(data []byte) error {
	// Determine max blocks based on card type
	var maxBlocks uint8
	if t.IsMIFARE4K() {
		maxBlocks = 255 // 4K card has 255 blocks (0-254)
	} else {
		maxBlocks = 64 // 1K card has 64 blocks (0-63)
	}

	block := uint8(4)
	for i := 0; i < len(data); i += mifareBlockSize {
		if block%4 == 3 {
			block++
		}

		if block >= maxBlocks {
			return errors.New("NDEF data exceeds tag capacity")
		}

		if err := t.writeDataBlock(block, data, i); err != nil {
			return err
		}
		block++
	}
	return nil
}

func (t *MIFARETag) writeDataBlock(block uint8, data []byte, offset int) error {
	end := offset + mifareBlockSize
	if end > len(data) {
		blockData := make([]byte, mifareBlockSize)
		copy(blockData, data[offset:])
		return t.writeBlockWithError(block, blockData)
	}
	return t.writeBlockWithError(block, data[offset:end])
}

func (t *MIFARETag) writeBlockWithError(block uint8, data []byte) error {
	if err := t.WriteBlockAuto(block, data); err != nil {
		return fmt.Errorf("failed to write block %d: %w", block, err)
	}
	return nil
}

func (*MIFARETag) calculateNextBlock(dataLen int) uint8 {
	block := uint8(4)
	for i := 0; i < dataLen; i += mifareBlockSize {
		if block%4 == 3 {
			block++
		}
		block++
	}
	return block
}

func (t *MIFARETag) clearRemainingBlocks(startBlock uint8) error {
	// Determine max blocks based on card type
	var maxBlocks uint8
	if t.IsMIFARE4K() {
		maxBlocks = 255 // 4K card has 255 blocks (0-254)
	} else {
		maxBlocks = 64 // 1K card has 64 blocks (0-63)
	}

	block := startBlock
	for block < maxBlocks {
		if block%4 == 3 {
			block++
			continue
		}

		emptyBlock := make([]byte, mifareBlockSize)
		if err := t.WriteBlockAuto(block, emptyBlock); err != nil {
			// It's okay if we can't clear all blocks - this is best effort
			_ = err // explicitly ignore error
			break
		}
		block++
	}
	return nil
}

// ResetAuthState resets the PN532's internal authentication state
// This can help when previous failed authentication attempts have polluted the state
func (t *MIFARETag) ResetAuthState() error {
	// SECURITY: Thread-safe state clearing
	t.authMutex.Lock()
	t.lastAuthSector = -1
	t.lastAuthKeyType = 0
	t.authMutex.Unlock()

	// Force PN532 to reset by attempting to re-detect the tag
	// This clears any internal authentication state in the PN532 chip
	_, err := t.device.InListPassiveTargetContext(context.Background(), 1, 0x00)
	return err
}

// Authenticate authenticates a sector on the MIFARE tag
func (t *MIFARETag) Authenticate(sector uint8, keyType byte, key []byte) error {
	if len(key) != 6 {
		return errors.New("MIFARE key must be 6 bytes")
	}

	// SECURITY: Create secure copy of key for protocol operations
	secureKeyCopy := make([]byte, 6)
	copy(secureKeyCopy, key)
	defer func() {
		for i := range secureKeyCopy {
			secureKeyCopy[i] = 0
		}
	}()

	// Validate key type
	if keyType != 0x00 && keyType != 0x01 {
		return fmt.Errorf("invalid key type: 0x%02X (must be 0x00 for Key A or 0x01 for Key B)", keyType)
	}

	// Calculate block number for the sector
	block := sector * mifareSectorSize

	// Build authentication command
	// CRITICAL: Protocol requires key first, then UID (per PN532 manual and working implementations)
	cmd := []byte{mifareCmdAuth + keyType, block}
	cmd = append(cmd, secureKeyCopy...) // Key must come first
	cmd = append(cmd, t.uid[:4]...)     // UID comes second

	_, err := t.device.SendDataExchange(cmd)
	if err != nil {
		// SECURITY: Thread-safe state clearing on failure
		t.authMutex.Lock()
		t.lastAuthSector = -1
		t.lastAuthKeyType = 0
		t.authMutex.Unlock()
		return fmt.Errorf("authentication failed: %w", err)
	}

	// SECURITY: Thread-safe state update on success
	t.authMutex.Lock()
	t.lastAuthSector = int(sector)
	t.lastAuthKeyType = keyType
	t.authMutex.Unlock()

	return nil
}

// AuthenticateRobust performs robust authentication with retry logic and Chinese clone support
// This is the recommended method for authenticating with unreliable tags
func (t *MIFARETag) AuthenticateRobust(sector uint8, keyType byte, key []byte) error {
	start := time.Now()
	defer func() {
		t.timing.add(time.Since(start))
	}()

	// Try standard authentication first
	err := t.authenticateWithRetry(sector, keyType, key)
	if err == nil {
		return nil
	}

	// If standard auth failed, try Chinese clone unlock sequences
	if t.tryChineseCloneUnlock(sector) {
		// Clone unlock successful, tag is accessible without auth
		t.authMutex.Lock()
		t.lastAuthSector = int(sector)
		t.lastAuthKeyType = keyType
		t.authMutex.Unlock()
		return nil
	}

	return fmt.Errorf("robust authentication failed after all attempts: %w", err)
}

// authenticateWithRetry implements progressive retry strategy
func (t *MIFARETag) authenticateWithRetry(sector uint8, keyType byte, key []byte) error {
	var lastErr error

	for attempt := 0; attempt < t.config.RetryConfig.MaxAttempts; attempt++ {
		level := t.getRetryLevel(attempt)

		// Apply recovery strategy based on level
		if err := t.applyRetryStrategy(level, lastErr); err != nil {
			return fmt.Errorf("recovery strategy failed: %w", err)
		}

		// Attempt authentication
		err := t.Authenticate(sector, keyType, key)
		if err == nil {
			return nil
		}

		lastErr = err

		// Check for specific error patterns that indicate permanent failure
		if t.isPermanentFailure(err) {
			return fmt.Errorf("permanent authentication failure: %w", err)
		}

		// Apply exponential backoff with jitter
		delay := t.calculateRetryDelay(attempt)
		time.Sleep(delay)
	}

	return fmt.Errorf("authentication failed after %d attempts: %w", t.config.RetryConfig.MaxAttempts, lastErr)
}

// getRetryLevel determines the retry level based on attempt number
func (*MIFARETag) getRetryLevel(attempt int) retryLevel {
	switch {
	case attempt < 2:
		return retryLight
	case attempt < 3:
		return retryModerate
	case attempt < 4:
		return retryHeavy
	default:
		return retryNuclear
	}
}

// applyRetryStrategy implements the progressive recovery strategy
func (t *MIFARETag) applyRetryStrategy(level retryLevel, _ error) error {
	switch level {
	case retryLight:
		// Light: Just a small delay, no special action needed
		return nil

	case retryModerate:
		// Moderate: Card reinitialization (critical fix from research)
		_, err := t.device.InListPassiveTargetContext(context.Background(), 1, 0x00)
		if err != nil {
			return fmt.Errorf("card reinitialization failed: %w", err)
		}

		// Clear authentication state
		t.authMutex.Lock()
		t.lastAuthSector = -1
		t.lastAuthKeyType = 0
		t.authMutex.Unlock()

		return nil

	case retryHeavy:
		// Heavy: RF field reset sequence (if device supports it)
		// Note: This would require device-level support for field control
		// For now, we do a longer reinitialization sequence
		for i := 0; i < 3; i++ {
			_, err := t.device.InListPassiveTargetContext(context.Background(), 1, 0x00)
			if err == nil {
				break
			}
			time.Sleep(t.config.HardwareDelay)
		}

		t.authMutex.Lock()
		t.lastAuthSector = -1
		t.lastAuthKeyType = 0
		t.authMutex.Unlock()

		return nil

	case retryNuclear:
		// Nuclear: Complete reinitialization
		return t.ResetAuthState()

	default:
		return nil
	}
}

// calculateRetryDelay implements exponential backoff with jitter
func (t *MIFARETag) calculateRetryDelay(attempt int) time.Duration {
	// Exponential backoff: baseDelay * 2^attempt
	if attempt > 30 { // Prevent overflow
		attempt = 30
	}
	//nolint:gosec // G115: Overflow prevented by bounds check above
	shiftAmount := uint(attempt)
	delay := t.config.RetryConfig.InitialBackoff * time.Duration(1<<shiftAmount)
	if delay > t.config.RetryConfig.MaxBackoff {
		delay = t.config.RetryConfig.MaxBackoff
	}

	// Add random jitter based on configured factor
	if t.config.RetryConfig.Jitter > 0 {
		jitterAmount := float64(delay) * t.config.RetryConfig.Jitter
		jitterMax := big.NewInt(int64(jitterAmount))
		if jitterMax.Int64() > 0 {
			jitterBig, _ := rand.Int(rand.Reader, jitterMax)
			jitter := time.Duration(jitterBig.Int64())
			return delay + jitter
		}
	}
	return delay
}

// isPermanentFailure checks if an error indicates a permanent failure
func (*MIFARETag) isPermanentFailure(err error) bool {
	errStr := strings.ToLower(err.Error())

	// Check for specific error patterns that indicate permanent issues
	permanentPatterns := []string{
		"invalid key type",
		"mifare key must be 6 bytes",
		"cannot write to manufacturer block",
	}

	for _, pattern := range permanentPatterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// tryChineseCloneUnlock attempts Chinese clone unlock sequences
func (t *MIFARETag) tryChineseCloneUnlock(_ uint8) bool {
	// Try Gen1 unlock sequence (0x40 for 7-bit, 0x43 for 8-bit)
	commands := []byte{chineseCloneUnlock7Bit, chineseCloneUnlock8Bit}

	for _, cmd := range commands {
		_, err := t.device.SendDataExchange([]byte{cmd})
		if err == nil {
			// Unlock successful - this is a Gen1 clone tag
			return true
		}
	}

	return false
}

// GetTimingVariance returns the timing variance for hardware issue detection
func (t *MIFARETag) GetTimingVariance() time.Duration {
	return t.timing.getVariance()
}

// GetDevice returns the underlying PN532 device for direct access
func (t *MIFARETag) GetDevice() *Device {
	return t.device
}

// IsTimingUnstable checks if timing variance indicates hardware issues
func (t *MIFARETag) IsTimingUnstable() bool {
	variance := t.timing.getVariance()
	// High variance (>1000ms) indicates hardware issues per research
	return variance > 1000*time.Millisecond
}

// AnalyzeLastError provides detailed error analysis based on research findings
func (*MIFARETag) AnalyzeLastError(err error) string {
	if err == nil {
		return "No error"
	}

	errStr := err.Error()

	// Pattern analysis based on research document
	if strings.Contains(errStr, "14") {
		return "Error 0x14: Wrong key or compatibility issues with Chinese clone"
	}
	if strings.Contains(errStr, "01") {
		return "Error 0x01: Timeout - insufficient retries or timing issues"
	}
	if strings.Contains(errStr, "27") {
		return "Error 0x27: Improper state management"
	}
	if strings.Contains(errStr, "80 80 80") {
		return "PN532 firmware bug: successful auth but read failure (sectors 1-15 after sector 0)"
	}
	if strings.Contains(errStr, "data exchange error") {
		return "Communication error - may benefit from InCommunicateThru fallback"
	}

	return fmt.Sprintf("Generic error: %s", errStr)
}

// formatForNDEFWithKey formats a blank MIFARE Classic tag for NDEF use with a specific blank key
func (t *MIFARETag) determineMaxSectors() uint8 {
	if t.IsMIFARE4K() {
		return 40 // MIFARE Classic 4K has 40 sectors (0-39)
	}
	return 16 // MIFARE Classic 1K has 16 sectors (0-15)
}

func (t *MIFARETag) updateSectorKeys(sector uint8, ndefKeyBytes []byte) error {
	// Calculate sector trailer block
	trailerBlock := sector*4 + 3

	// Read current sector trailer to preserve access bits
	trailerData, err := t.ReadBlock(trailerBlock)
	if err != nil {
		return fmt.Errorf("failed to read sector %d trailer: %w", sector, err)
	}

	// Update keys in trailer (keep access bits unchanged)
	// Trailer format: Key A (6 bytes) + Access Bits (4 bytes) + Key B (6 bytes)
	copy(trailerData[0:6], ndefKeyBytes)   // Key A
	copy(trailerData[10:16], ndefKeyBytes) // Key B

	// Write updated trailer
	if err := t.WriteBlock(trailerBlock, trailerData); err != nil {
		return fmt.Errorf("failed to write sector %d trailer: %w", sector, err)
	}

	return nil
}

func (t *MIFARETag) reAuthenticateWithNDEFKey(sector uint8, ndefKeyBytes []byte) error {
	// CRITICAL FIX: Re-authenticate with the new NDEF key
	// This ensures the PN532 authentication state matches the new keys on the tag
	if err := t.AuthenticateRobust(sector, MIFAREKeyA, ndefKeyBytes); err != nil {
		return fmt.Errorf("failed to re-authenticate sector %d with new NDEF key: %w", sector, err)
	}
	return nil
}

func clearKeyBytes(keyBytes []byte) {
	for i := range keyBytes {
		keyBytes[i] = 0
	}
}

func (t *MIFARETag) formatForNDEFWithKey(blankKey []byte) error {
	maxSectors := t.determineMaxSectors()
	ndefKeyBytes := t.ndefKey.bytes()

	for sector := uint8(1); sector < maxSectors; sector++ {
		// First authenticate with the blank key
		if err := t.AuthenticateRobust(sector, MIFAREKeyA, blankKey); err != nil {
			// If we can't authenticate, assume this sector is already formatted or protected
			continue
		}

		if err := t.updateSectorKeys(sector, ndefKeyBytes); err != nil {
			return err
		}

		if err := t.reAuthenticateWithNDEFKey(sector, ndefKeyBytes); err != nil {
			clearKeyBytes(ndefKeyBytes)
			return err
		}
	}

	clearKeyBytes(ndefKeyBytes)

	// Add a small delay to let the tag process the key changes
	time.Sleep(t.config.HardwareDelay)

	return nil
}

// DebugInfo returns detailed debug information about the MIFARE tag
func (t *MIFARETag) DebugInfo() string {
	return t.DebugInfoWithNDEF(t)
}

// WriteText writes a simple text record to the MIFARE tag
func (t *MIFARETag) WriteText(text string) error {
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
