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
	"encoding/hex"
	"time"
)

// AutoPollTarget defines the target type for InAutoPoll.
type AutoPollTarget byte

const (
	// AutoPollGeneric106kbps is the generic passive mode for ISO14443-4A, Mifare, DEP.
	AutoPollGeneric106kbps AutoPollTarget = 0x00
	// AutoPollGeneric212kbps is the generic passive mode for FeliCa, DEP.
	AutoPollGeneric212kbps AutoPollTarget = 0x01
	// AutoPollGeneric424kbps is the generic passive mode for FeliCa, DEP.
	AutoPollGeneric424kbps AutoPollTarget = 0x02

	// AutoPollISO14443B is for ISO14443-4B specific passive mode.
	AutoPollISO14443B AutoPollTarget = 0x03
	// AutoPollJewel is for Innovision Jewel tags.
	AutoPollJewel AutoPollTarget = 0x04
	// AutoPollMifare is for Mifare tags.
	AutoPollMifare AutoPollTarget = 0x10
	// AutoPollFeliCa212 is for FeliCa at 212 kbps.
	AutoPollFeliCa212 AutoPollTarget = 0x11
	// AutoPollFeliCa424 is for FeliCa at 424 kbps.
	AutoPollFeliCa424 AutoPollTarget = 0x12
	// AutoPollISO14443A is for ISO14443-4A.
	AutoPollISO14443A AutoPollTarget = 0x20
	// AutoPollISO14443B4 is for ISO14443-4B.
	AutoPollISO14443B4 AutoPollTarget = 0x23
)

// AutoPollResult contains the result of an InAutoPoll operation.
type AutoPollResult struct {
	TargetData []byte
	Type       AutoPollTarget
}

// ToDetectedTag converts an AutoPollResult to a DetectedTag with proper configuration
// for InAutoPoll results. This method automatically sets FromInAutoPoll=true and
// handles UID extraction and tag type mapping.
func (a *AutoPollResult) ToDetectedTag() *DetectedTag {
	// Map AutoPollTarget to TagType first
	tagType := a.mapToTagType()

	// Extract UID from target data - format varies by tag type
	var uid string
	var uidBytes []byte

	uidBytes, uid = a.extractUID(tagType)

	return &DetectedTag{
		Type:           tagType,
		UID:            uid,
		UIDBytes:       uidBytes,
		TargetData:     a.TargetData,
		FromInAutoPoll: true, // Critical: Skip InSelect since InAutoPoll handles selection
		TargetNumber:   1,    // Use target 1 for compatibility
		DetectedAt:     time.Now(),
	}
}

// mapToTagType maps AutoPollTarget to appropriate TagType
func (a *AutoPollResult) mapToTagType() TagType {
	switch a.Type {
	case AutoPollGeneric106kbps, AutoPollISO14443A:
		// 106kbps cards could be NTAG or MIFARE - default to NTAG as it's most common
		// in testing and more universally compatible
		return TagTypeNTAG
	case AutoPollMifare:
		return a.determineMIFAREType()
	case AutoPollFeliCa212, AutoPollFeliCa424, AutoPollGeneric212kbps, AutoPollGeneric424kbps:
		return TagTypeFeliCa
	case AutoPollISO14443B, AutoPollISO14443B4:
		// ISO14443-4B cards are less common, map to NTAG for compatibility
		return TagTypeNTAG
	case AutoPollJewel:
		// Jewel tags are quite different, but map to NTAG for now
		return TagTypeNTAG
	default:
		return TagTypeUnknown
	}
}

// determineMIFAREType analyzes TargetData to distinguish between MIFARE and NTAG
func (a *AutoPollResult) determineMIFAREType() TagType {
	if len(a.TargetData) < 10 {
		return TagTypeMIFARE
	}

	// Extract ATQ (bytes 7-8) and SAK (byte 9) from TargetData
	atq := a.TargetData[7:9]
	sak := a.TargetData[9]

	// Check if this matches known NTAG patterns
	if isStandardNTAGPattern(atq, sak) || isAdditionalNTAGPattern(atq, sak) {
		return TagTypeNTAG
	}

	// Check if this matches known MIFARE patterns
	if isMIFAREPattern(atq, sak) {
		return TagTypeMIFARE
	}

	// If it doesn't match standard MIFARE patterns but AutoPoll detected it as MIFARE,
	// it's likely an NTAG card that doesn't match our known patterns - default to NTAG
	return TagTypeNTAG
}

// isMIFAREPattern checks if ATQ/SAK matches known MIFARE patterns
func isMIFAREPattern(atq []byte, sak byte) bool {
	return (atq[0] == 0x00 && atq[1] == 0x04 && sak == 0x08) || // MIFARE Classic 1K
		(atq[0] == 0x00 && atq[1] == 0x02 && sak == 0x18) // MIFARE Classic 4K
}

// extractUID extracts UID bytes and hex string from TargetData based on tag type
func (a *AutoPollResult) extractUID(tagType TagType) (uidBytes []byte, uid string) {
	switch {
	case len(a.TargetData) < 4:
		// Handle edge case of very short target data
		return a.TargetData, hex.EncodeToString(a.TargetData)
	case tagType == TagTypeMIFARE && len(a.TargetData) >= 8:
		// MIFARE Classic: UID is typically 4 bytes
		// TargetData format for MIFARE: [TgType][NbTg][ATQ][SAK][UID4][additional]
		// For TargetData like 010004080463CF41E4, UID appears to be bytes 4-7: 0463CF41
		uidBytes := a.TargetData[4:8]
		return uidBytes, hex.EncodeToString(uidBytes)
	case len(a.TargetData) >= 7:
		// NTAG/other cards: UID is typically first 7 bytes
		uidBytes := a.TargetData[:7]
		return uidBytes, hex.EncodeToString(uidBytes)
	default:
		// Fallback for shorter data
		uidBytes := a.TargetData[:4]
		return uidBytes, hex.EncodeToString(uidBytes)
	}
}

// InAutoPoll polls for targets of specified types
// pollCount: number of polling cycles (0xFF for endless)
// pollPeriod: polling period in units of 150ms (1-15)
// targetTypes: types of targets to poll for
// Based on PN532 manual section 7.3.13
func (d *Device) InAutoPoll(pollCount, pollPeriod byte, targetTypes []AutoPollTarget) ([]AutoPollResult, error) {
	return d.InAutoPollContext(context.Background(), pollCount, pollPeriod, targetTypes)
}
