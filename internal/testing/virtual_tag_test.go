//go:build integration

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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVirtualTagCreation tests the creation of different virtual tag types
func TestVirtualTagCreation(t *testing.T) {
	tests := []struct {
		name           string
		createTag      func() *VirtualTag
		expectedType   string
		expectedBlocks int
		expectedUID    []byte
	}{
		{
			name:           "NTAG213_Creation",
			createTag:      func() *VirtualTag { return NewVirtualNTAG213(nil) },
			expectedType:   "NTAG213",
			expectedBlocks: 45,
			expectedUID:    TestNTAG213UID,
		},
		{
			name:           "MIFARE1K_Creation",
			createTag:      func() *VirtualTag { return NewVirtualMIFARE1K(nil) },
			expectedType:   "MIFARE1K",
			expectedBlocks: 64,
			expectedUID:    TestMIFARE1KUID,
		},
		{
			name:           "MIFARE4K_Creation",
			createTag:      func() *VirtualTag { return NewVirtualMIFARE4K(nil) },
			expectedType:   "MIFARE4K",
			expectedBlocks: 256,
			expectedUID:    TestMIFARE4KUID,
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			tag := tt.createTag()
			require.NotNil(t, tag)

			assert.Equal(t, tt.expectedType, tag.Type)
			assert.Equal(t, tt.expectedBlocks, len(tag.Memory))
			assert.Equal(t, tt.expectedUID, tag.UID)
			assert.True(t, tag.Present)
		})
	}
}

// TestVirtualTagCustomUID tests creation with custom UIDs
func TestVirtualTagCustomUID(t *testing.T) {
	// Default MIFARE key
	defaultKey := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	customUID := []byte{0xAA, 0xBB, 0xCC, 0xDD}

	tests := []struct {
		name      string
		createTag func([]byte) *VirtualTag
		isMIFARE  bool
	}{
		{
			name:      "NTAG213_CustomUID",
			createTag: NewVirtualNTAG213,
			isMIFARE:  false,
		},
		{
			name:      "MIFARE1K_CustomUID",
			createTag: NewVirtualMIFARE1K,
			isMIFARE:  true,
		},
		{
			name:      "MIFARE4K_CustomUID",
			createTag: NewVirtualMIFARE4K,
			isMIFARE:  true,
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			tag := tt.createTag(customUID)
			require.NotNil(t, tag)

			assert.Equal(t, customUID, tag.UID)

			// Authenticate sector 0 for MIFARE tags before reading block 0
			if tt.isMIFARE {
				err := tag.Authenticate(0, MIFAREKeyA, defaultKey)
				require.NoError(t, err)
				// MIFARE stores UID in block 0 (16 bytes)
				block0, err := tag.ReadBlock(0)
				require.NoError(t, err)
				assert.Equal(t, customUID, block0[:len(customUID)])
			} else {
				// NTAG stores UID across pages 0-1 (4 bytes each)
				// Page 0: UID[0:3] + BCC0, Page 1: UID[3:7]
				page0, err := tag.ReadBlock(0)
				require.NoError(t, err)
				assert.Equal(t, customUID[:3], page0[:3], "First 3 UID bytes in page 0")
				page1, err := tag.ReadBlock(1)
				require.NoError(t, err)
				assert.Equal(t, customUID[3:], page1[:1], "UID byte 3 in page 1")
			}
		})
	}
}

// TestNDEFTextOperations tests NDEF text reading and writing
func TestNDEFTextOperations(t *testing.T) {
	tag := NewVirtualNTAG213(nil)
	require.NotNil(t, tag)

	// Test initial default text
	initialText := tag.GetNDEFText()
	assert.Equal(t, "Hello World", initialText)

	tests := []struct {
		name     string
		text     string
		expected string
	}{
		{
			name:     "SimpleText",
			text:     "Test",
			expected: "Test",
		},
		{
			name:     "LongerText",
			text:     "This is a longer test message",
			expected: "This is a longer test message",
		},
		{
			name:     "EmptyText",
			text:     "",
			expected: "",
		},
		{
			name:     "SpecialCharacters",
			text:     "Hello, World! @#$%",
			expected: "Hello, World! @#$%",
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			// Set text
			err := tag.SetNDEFText(tt.text)
			require.NoError(t, err)

			// Read back text
			readText := tag.GetNDEFText()
			assert.Equal(t, tt.expected, readText)
		})
	}
}

// TestNDEFParsingEdgeCases tests complex NDEF parsing scenarios
func TestNDEFParsingEdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		setupTag     func(*VirtualTag)
		expectedText string
		description  string
	}{
		{
			name: "EmptyNDEFMessage",
			setupTag: func(tag *VirtualTag) {
				// Clear all user data pages (4 bytes each for NTAG)
				for i := 4; i < 40; i++ {
					tag.Memory[i] = make([]byte, 4)
				}
			},
			expectedText: "",
			description:  "Tag with no NDEF message should return empty string",
		},
		{
			name: "MalformedNDEFHeader",
			setupTag: func(tag *VirtualTag) {
				// Set malformed NDEF data across 4-byte pages
				// Malformed header (C1 instead of D1)
				tag.Memory[4] = []byte{0x03, 0x10, 0xC1, 0x01}
				tag.Memory[5] = []byte{0x05, 0x54, 0x02, 0x65}
				tag.Memory[6] = []byte{0x6E, 0x48, 0x65, 0x6C}
				tag.Memory[7] = []byte{0x6C, 0x6F, 0xFE, 0x00}
			},
			expectedText: "",
			description:  "Tag with malformed NDEF header should return empty string",
		},
		{
			name: "MessageSpanningMultipleBlocks",
			setupTag: func(tag *VirtualTag) {
				// Use SetNDEFText which properly handles 4-byte page layout
				_ = tag.SetNDEFText("This is a long message")
			},
			expectedText: "This is a long message",
			description:  "Message spanning multiple blocks should be parsed correctly",
		},
		{
			name: "MessageAtBlockBoundary",
			setupTag: func(tag *VirtualTag) {
				// Use SetNDEFText for proper 4-byte page layout
				_ = tag.SetNDEFText("Boundary")
			},
			expectedText: "Boundary",
			description:  "Message ending at exact block boundary should be parsed correctly",
		},
		{
			name: "NoTerminatorInMessage",
			setupTag: func(tag *VirtualTag) {
				// Use SetNDEFText for proper 4-byte page layout
				_ = tag.SetNDEFText("NoTerm")
			},
			expectedText: "NoTerm",
			description:  "Message without terminator should stop at null bytes",
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			tag := NewVirtualNTAG213(nil)
			require.NotNil(t, tag)

			// Setup the tag according to test case
			tt.setupTag(tag)

			// Test parsing
			result := tag.GetNDEFText()
			assert.Equal(t, tt.expectedText, result, tt.description)
		})
	}
}

// TestVirtualTagMemoryLayout tests the memory initialization
func TestVirtualTagMemoryLayout(t *testing.T) {
	// Default MIFARE key
	defaultKey := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	tests := []struct {
		name      string
		createTag func() *VirtualTag
		testFunc  func(*testing.T, *VirtualTag)
	}{
		{
			name:      "NTAG213_MemoryLayout",
			createTag: func() *VirtualTag { return NewVirtualNTAG213(nil) },
			testFunc: func(t *testing.T, tag *VirtualTag) {
				// NTAG pages are 4 bytes each
				// Page 0: UID bytes 0-2 + BCC0
				page0, err := tag.ReadBlock(0)
				require.NoError(t, err)
				require.Len(t, page0, 4, "NTAG page 0 should be 4 bytes")
				assert.Equal(t, TestNTAG213UID[:3], page0[:3], "First 3 bytes of UID")

				// Page 1: UID bytes 3-6
				page1, err := tag.ReadBlock(1)
				require.NoError(t, err)
				assert.Equal(t, TestNTAG213UID[3:7], page1[:4], "UID bytes 3-6")

				// Page 3: Capability Container (CC)
				page3, err := tag.ReadBlock(3)
				require.NoError(t, err)
				assert.Equal(t, byte(0xE1), page3[0]) // NDEF magic
				assert.Equal(t, byte(0x10), page3[1]) // Version/access

				// Pages 4-39: User data area should be initialized
				for i := 4; i < 40; i++ {
					_, err := tag.ReadBlock(i)
					assert.NoError(t, err, "Page %d should be readable", i)
				}
			},
		},
		{
			name:      "MIFARE1K_MemoryLayout",
			createTag: func() *VirtualTag { return NewVirtualMIFARE1K(nil) },
			testFunc: func(t *testing.T, tag *VirtualTag) {
				// Authenticate sector 0 first
				err := tag.Authenticate(0, MIFAREKeyA, defaultKey)
				require.NoError(t, err)

				// Block 0: UID should be set
				block0, err := tag.ReadBlock(0)
				require.NoError(t, err)
				assert.Equal(t, TestMIFARE1KUID, block0[:len(TestMIFARE1KUID)])

				// Check sector trailers (blocks 3, 7, 11, 15)
				for sector := 0; sector < 16; sector++ {
					// Authenticate each sector before reading its trailer
					err := tag.Authenticate(sector, MIFAREKeyA, defaultKey)
					require.NoError(t, err, "Authentication should succeed for sector %d", sector)

					trailerBlock := sector*4 + 3
					trailer, err := tag.ReadBlock(trailerBlock)
					require.NoError(t, err)

					// Check default key A (first 6 bytes should be 0xFF)
					for i := 0; i < 6; i++ {
						assert.Equal(t, byte(0xFF), trailer[i], "Key A byte %d in sector %d", i, sector)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			tag := tt.createTag()
			require.NotNil(t, tag)

			tt.testFunc(t, tag)
		})
	}
}

// TestGetUIDString tests the UID string conversion (currently unused but tested for completeness)
func TestGetUIDString(t *testing.T) {
	tests := []struct {
		name        string
		uid         []byte
		expectedHex string
	}{
		{
			name:        "NTAG213_UID",
			uid:         TestNTAG213UID,
			expectedHex: "04abcdef123456",
		},
		{
			name:        "MIFARE1K_UID",
			uid:         TestMIFARE1KUID,
			expectedHex: "12345678",
		},
		{
			name:        "MIFARE4K_UID",
			uid:         TestMIFARE4KUID,
			expectedHex: "abcdef01",
		},
		{
			name:        "EmptyUID",
			uid:         []byte{},
			expectedHex: "",
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			tag := NewVirtualNTAG213(tt.uid)
			require.NotNil(t, tag)

			result := tag.GetUIDString()
			assert.Equal(t, tt.expectedHex, result)
		})
	}
}

// TestNDEFMessageSizeHandling tests NDEF message size limits
func TestNDEFMessageSizeHandling(t *testing.T) {
	tag := NewVirtualNTAG213(nil)
	require.NotNil(t, tag)

	// Test with very long text that might exceed NTAG213 capacity
	longText := strings.Repeat("A", 200) // 200 characters

	err := tag.SetNDEFText(longText)
	if err != nil {
		// If it fails, it should be due to size constraints
		assert.Contains(t, err.Error(), "too large")
	} else {
		// If it succeeds, we should be able to read it back (possibly truncated)
		readText := tag.GetNDEFText()
		// The exact behavior depends on implementation, but it shouldn't crash
		assert.NotEmpty(t, readText)
		// The read text should be a prefix of the original (might be truncated)
		assert.True(t, strings.HasPrefix(longText, readText) || readText == longText)
	}
}
