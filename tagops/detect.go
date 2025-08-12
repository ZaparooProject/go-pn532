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
	"bytes"
	"fmt"
)

const (
	unknownTagName    = "Unknown"
	ntagTypeName      = "NTAG"
	mifareClassicName = "MIFARE Classic"
)

// TagInfo contains detailed information about a detected tag
type TagInfo struct {
	// String fields (24 bytes each on 64-bit)
	TypeName   string
	NTAGType   string
	MIFAREType string

	// Slice field (24 bytes on 64-bit)
	UID []byte

	// Integer fields (8 bytes each on 64-bit for TagType as int, 4 bytes for int)
	Type        TagType
	TotalPages  int
	UserMemory  int
	Sectors     int
	TotalMemory int
}

// GetTagInfo returns detailed information about the currently detected tag
func (t *TagOperations) GetTagInfo() (*TagInfo, error) {
	if t.tag == nil {
		return nil, ErrNoTag
	}

	info := &TagInfo{
		Type: t.tagType,
		UID:  t.tag.UIDBytes,
	}

	switch t.tagType {
	case TagTypeNTAG:
		info.TypeName = ntagTypeName
		info.TotalPages = t.totalPages
		info.UserMemory = (t.totalPages - 4) * 4 // Exclude first 4 pages

		// Determine specific NTAG type based on total pages
		switch t.totalPages {
		case 45:
			info.NTAGType = "NTAG213"
		case 135:
			info.NTAGType = "NTAG215"
		case 231:
			info.NTAGType = "NTAG216"
		default:
			info.NTAGType = fmt.Sprintf("NTAG (unknown, %d pages)", t.totalPages)
		}

	case TagTypeMIFARE:
		info.TypeName = mifareClassicName
		// Try to determine if it's 1K or 4K
		// This is a simplified check - real detection would need SAK analysis
		if len(t.tag.UIDBytes) == 4 {
			info.MIFAREType = "MIFARE Classic 1K"
			info.Sectors = 16
			info.TotalMemory = 1024
		} else {
			// Could be 4K, but need more checks
			info.MIFAREType = "MIFARE Classic (unknown size)"
			info.Sectors = 16 // Default to 1K
			info.TotalMemory = 1024
		}

	case TagTypeUnknown:
		info.TypeName = unknownTagName
	default:
		info.TypeName = unknownTagName
	}

	return info, nil
}

// String returns a human-readable string representation of the tag type
func (t TagType) String() string {
	switch t {
	case TagTypeUnknown:
		return unknownTagName
	case TagTypeNTAG:
		return ntagTypeName
	case TagTypeMIFARE:
		return mifareClassicName
	default:
		return unknownTagName
	}
}

// DetectTagTypeFromUID attempts to determine tag type from UID characteristics
// This is a helper function that can be used before full tag initialization
func DetectTagTypeFromUID(uid []byte) TagType {
	// This is a simplified detection based on UID patterns
	// Real detection should use SAK and ATQA values

	if len(uid) == 7 {
		// 7-byte UID often indicates NTAG
		if uid[0] == 0x04 {
			return TagTypeNTAG
		}
	} else if len(uid) == 4 {
		// 4-byte UID often indicates MIFARE Classic
		return TagTypeMIFARE
	}

	return TagTypeUnknown
}

// IsNDEFCapable returns whether the tag supports NDEF
func (t *TagOperations) IsNDEFCapable() bool {
	switch t.tagType {
	case TagTypeNTAG:
		return true // All NTAG variants support NDEF
	case TagTypeMIFARE:
		// MIFARE Classic can support NDEF if formatted properly
		// Try to read block 4 (sector 1) to test NDEF capability
		_, err := t.mifareInstance.ReadBlockAuto(4)
		return err == nil
	case TagTypeUnknown:
		return false
	default:
		return false
	}
}

// CompareUID compares two UIDs for equality
func CompareUID(uid1, uid2 []byte) bool {
	return bytes.Equal(uid1, uid2)
}
