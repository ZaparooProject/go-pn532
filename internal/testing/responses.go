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
	response := make([]byte, 0, 3+len(data))
	response = append(response, 0xD5, 0x41, 0x00) // Success status
	response = append(response, data...)
	return response
}

// BuildErrorResponse creates an error response for any command
func BuildErrorResponse(cmd, errorCode byte) []byte {
	return []byte{0xD5, cmd + 1, errorCode}
}

// buildNTAGDetectionResponse creates a response for NTAG tags
func buildNTAGDetectionResponse(uid []byte) []byte {
	response := make([]byte, 0, 8+len(uid))
	// Command + 1 target found + ATQA (Answer To Request Type A), SAK (Select Acknowledge), UID length
	//nolint:gosec // NFC UIDs are max 10 bytes
	response = append(response, 0xD5, 0x4B, 0x01, 0x01, 0x00, 0x44, 0x00, byte(len(uid)))
	response = append(response, uid...)

	return response
}

// buildMIFAREDetectionResponse creates a response for MIFARE Classic tags
func buildMIFAREDetectionResponse(uid []byte, sak byte) []byte {
	response := make([]byte, 0, 8+len(uid))
	// Command + 1 target found + ATQA (Answer To Request Type A), SAK (Select Acknowledge), UID length
	//nolint:gosec // NFC UIDs are max 10 bytes
	response = append(response, 0xD5, 0x4B, 0x01, 0x01, 0x00, 0x04, sak, byte(len(uid)))
	response = append(response, uid...)

	return response
}

// buildGenericDetectionResponse creates a generic ISO14443A response
func buildGenericDetectionResponse(uid []byte) []byte {
	response := make([]byte, 0, 8+len(uid))
	// Command + 1 target found + ATQA (Answer To Request Type A), SAK (Select Acknowledge), UID length
	//nolint:gosec // NFC UIDs are max 10 bytes
	response = append(response, 0xD5, 0x4B, 0x01, 0x01, 0x00, 0x04, 0x00, byte(len(uid)))
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
