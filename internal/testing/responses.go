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

package testing

// BuildFirmwareVersionResponse creates a GetFirmwareVersion response
func BuildFirmwareVersionResponse() []byte {
	// Firmware version response: IC, Ver, Rev, Support
	// Example: PN532 version 1.6 revision 7, supports ISO14443A/B
	return []byte{0xD5, 0x03, 0x32, 0x01, 0x06, 0x07}
}

// BuildSAMConfigurationResponse creates a SAMConfiguration response
// BuildSAMConfigurationResponse creates a SAMConfiguration response
// BuildSAMConfigurationResponse creates a SAMConfiguration response
func BuildSAMConfigurationResponse() []byte {
	// SAM configuration response: expected response code 0x15
	return []byte{0x15}
}

// BuildTagDetectionResponse creates an InListPassiveTarget response
func BuildTagDetectionResponse(tagType string, uid []byte) []byte {
	switch tagType {
	case "NTAG213":
		return buildNTAGDetectionResponse(uid)
	case "MIFARE1K":
		return buildMIFAREDetectionResponse(uid, 0x08) // SAK for 1K
	case "MIFARE4K":
		return buildMIFAREDetectionResponse(uid, 0x18) // SAK for 4K
	default:
		// Generic ISO14443A response
		return buildGenericDetectionResponse(uid)
	}
}

// BuildNoTagResponse creates an empty InListPassiveTarget response
func BuildNoTagResponse() []byte {
	return []byte{0xD5, 0x4B, 0x00} // No targets found
}

// BuildDataExchangeResponse creates an InDataExchange response
func BuildDataExchangeResponse(data []byte) []byte {
	response := []byte{0xD5, 0x41, 0x00} // Success status
	response = append(response, data...)
	return response
}

// BuildErrorResponse creates an error response for any command
func BuildErrorResponse(cmd, errorCode byte) []byte {
	return []byte{0xD5, cmd + 1, errorCode}
}

// buildNTAGDetectionResponse creates a response for NTAG tags
func buildNTAGDetectionResponse(uid []byte) []byte {
	response := []byte{0xD5, 0x4B, 0x01, 0x01} // Command + 1 target found

	// ATQA (Answer To Request Type A), SAK (Select Acknowledge), UID length and UID
	response = append(response, 0x00, 0x44, 0x00, byte(len(uid)))
	response = append(response, uid...)

	return response
}

// buildMIFAREDetectionResponse creates a response for MIFARE Classic tags
func buildMIFAREDetectionResponse(uid []byte, sak byte) []byte {
	response := []byte{0xD5, 0x4B, 0x01, 0x01} // Command + 1 target found

	// ATQA (Answer To Request Type A), SAK (Select Acknowledge), UID length and UID
	response = append(response, 0x00, 0x04, sak, byte(len(uid)))
	response = append(response, uid...)

	return response
}

// buildGenericDetectionResponse creates a generic ISO14443A response
func buildGenericDetectionResponse(uid []byte) []byte {
	response := []byte{0xD5, 0x4B, 0x01, 0x01} // Command + 1 target found

	// ATQA (Answer To Request Type A), SAK (Select Acknowledge), UID length and UID
	response = append(response, 0x00, 0x04, 0x00, byte(len(uid)))
	response = append(response, uid...)

	return response
}

// Common UIDs for testing
var (
	// TestNTAG213UID is a sample NTAG213 UID
	TestNTAG213UID = []byte{0x04, 0xAB, 0xCD, 0xEF, 0x12, 0x34, 0x56}

	// TestMIFARE1KUID is a sample MIFARE Classic 1K UID
	TestMIFARE1KUID = []byte{0x12, 0x34, 0x56, 0x78}

	// TestMIFARE4KUID is a sample MIFARE Classic 4K UID
	TestMIFARE4KUID = []byte{0xAB, 0xCD, 0xEF, 0x01}
)

// Command bytes for reference
const (
	CmdGetFirmwareVersion  = 0x02
	CmdInListPassiveTarget = 0x4A
	CmdInDataExchange      = 0x40
	CmdInRelease           = 0x52
	CmdSAMConfiguration    = 0x14
	CmdInSelect            = 0x54
)
