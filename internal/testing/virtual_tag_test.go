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


	customUID := []byte{0xAA, 0xBB, 0xCC, 0xDD}

	tests := []struct {
		name      string
		createTag func([]byte) *VirtualTag
	}{
		{
			name:      "NTAG213_CustomUID",
			createTag: NewVirtualNTAG213,
		},
		{
			name:      "MIFARE1K_CustomUID",
			createTag: NewVirtualMIFARE1K,
		},
		{
			name:      "MIFARE4K_CustomUID",
			createTag: NewVirtualMIFARE4K,
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {


			tag := tt.createTag(customUID)
			require.NotNil(t, tag)

			assert.Equal(t, customUID, tag.UID)
			// Verify UID is stored in block 0
			block0, err := tag.ReadBlock(0)
			require.NoError(t, err)
			assert.Equal(t, customUID, block0[:len(customUID)])
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
		name           string
		setupTag       func(*VirtualTag)
		expectedText   string
		description    string
	}{
		{
			name: "EmptyNDEFMessage",
			setupTag: func(tag *VirtualTag) {
				// Clear all user data blocks
				for i := 4; i < 40; i++ {
					emptyBlock := make([]byte, 16)
					tag.Memory[i] = emptyBlock
				}
			},
			expectedText: "",
			description:  "Tag with no NDEF message should return empty string",
		},
		{
			name: "MalformedNDEFHeader",
			setupTag: func(tag *VirtualTag) {
				// Set malformed NDEF data in block 4
				malformedBlock := []byte{
					0x03, 0x10, // NDEF TLV with length 16
					0xC1, 0x01, 0x05, 0x54, 0x02, 0x65, 0x6E, // Malformed header (C1 instead of D1)
					0x48, 0x65, 0x6C, 0x6C, 0x6F, 0xFE, 0x00, 0x00,
				}
				tag.Memory[4] = malformedBlock
			},
			expectedText: "",
			description:  "Tag with malformed NDEF header should return empty string",
		},
		{
			name: "MessageSpanningMultipleBlocks",
			setupTag: func(tag *VirtualTag) {
				// Create a message that spans two blocks
				// Block 4: NDEF header + start of text
				block4 := []byte{
					0x03, 0x1A, // NDEF TLV with length 26
					0xD1, 0x01, 0x16, 0x54, 0x02, 0x65, 0x6E, // NDEF Text record header
					0x54, 0x68, 0x69, 0x73, 0x20, 0x69, 0x73, 0x20, // "This is "
				}
				tag.Memory[4] = block4

				// Block 5: Continuation of text + terminator
				block5 := []byte{
					0x61, 0x20, 0x6C, 0x6F, 0x6E, 0x67, 0x20, 0x6D, // "a long m"
					0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0xFE, 0x00, // "essage" + terminator
				}
				tag.Memory[5] = block5
			},
			expectedText: "This is a long message",
			description:  "Message spanning multiple blocks should be parsed correctly",
		},
		{
			name: "MessageAtBlockBoundary",
			setupTag: func(tag *VirtualTag) {
				// Create a message that ends exactly at block boundary
				block4 := []byte{
					0x03, 0x0D, // NDEF TLV with length 13
					0xD1, 0x01, 0x09, 0x54, 0x02, 0x65, 0x6E, // NDEF Text record header
					0x42, 0x6F, 0x75, 0x6E, 0x64, 0x61, 0x72, 0x79, // "Boundary"
				}
				tag.Memory[4] = block4

				// Block 5: Just terminator
				block5 := []byte{0xFE, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
				tag.Memory[5] = block5
			},
			expectedText: "Boundary",
			description:  "Message ending at exact block boundary should be parsed correctly",
		},
		{
			name: "NoTerminatorInMessage",
			setupTag: func(tag *VirtualTag) {
				// Create a message without explicit terminator (ends with null bytes)
				block4 := []byte{
					0x03, 0x0C, // NDEF TLV with length 12
					0xD1, 0x01, 0x08, 0x54, 0x02, 0x65, 0x6E, // NDEF Text record header
					0x4E, 0x6F, 0x54, 0x65, 0x72, 0x6D, 0x00, 0x00, // "NoTerm" + nulls
				}
				tag.Memory[4] = block4
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


	tests := []struct {
		name      string
		createTag func() *VirtualTag
		testFunc  func(*testing.T, *VirtualTag)
	}{
		{
			name:      "NTAG213_MemoryLayout",
			createTag: func() *VirtualTag { return NewVirtualNTAG213(nil) },
			testFunc: func(t *testing.T, tag *VirtualTag) {
				// Block 0: UID should be set
				block0, err := tag.ReadBlock(0)
				require.NoError(t, err)
				assert.Equal(t, TestNTAG213UID, block0[:len(TestNTAG213UID)])

				// Block 2: Capability Container should be set
				block2, err := tag.ReadBlock(2)
				require.NoError(t, err)
				assert.Equal(t, byte(0xE1), block2[2]) // CC byte
				assert.Equal(t, byte(0x10), block2[3]) // Version/access

				// Blocks 4-39: User data area should be initialized
				for i := 4; i < 40; i++ {
					_, err := tag.ReadBlock(i)
					assert.NoError(t, err, "Block %d should be readable", i)
				}
			},
		},
		{
			name:      "MIFARE1K_MemoryLayout",
			createTag: func() *VirtualTag { return NewVirtualMIFARE1K(nil) },
			testFunc: func(t *testing.T, tag *VirtualTag) {
				// Block 0: UID should be set
				block0, err := tag.ReadBlock(0)
				require.NoError(t, err)
				assert.Equal(t, TestMIFARE1KUID, block0[:len(TestMIFARE1KUID)])

				// Check sector trailers (blocks 3, 7, 11, 15)
				for sector := 0; sector < 16; sector++ {
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