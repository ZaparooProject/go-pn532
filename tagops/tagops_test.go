//nolint:paralleltest // Test file - not using parallel tests
package tagops

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- TagType Tests ---

func TestTagTypeDisplayName(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		tagType  pn532.TagType
	}{
		{"Unknown tag type", "Unknown", pn532.TagTypeUnknown},
		{"NTAG tag type", "NTAG", pn532.TagTypeNTAG},
		{"MIFARE tag type", "MIFARE Classic", pn532.TagTypeMIFARE},
		{"Any tag type", "Unknown", pn532.TagTypeAny},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := TagTypeDisplayName(tc.tagType)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestTagType_Constants(t *testing.T) {
	// Verify constants are distinct (pn532.TagType is a string type)
	assert.NotEqual(t, pn532.TagTypeUnknown, pn532.TagTypeNTAG)
	assert.NotEqual(t, pn532.TagTypeUnknown, pn532.TagTypeMIFARE)
	assert.NotEqual(t, pn532.TagTypeNTAG, pn532.TagTypeMIFARE)

	// Verify TagType values
	assert.Equal(t, pn532.TagTypeUnknown, pn532.TagType("UNKNOWN"))
	assert.Equal(t, pn532.TagTypeNTAG, pn532.TagType("NTAG"))
	assert.Equal(t, pn532.TagTypeMIFARE, pn532.TagType("MIFARE"))
}

// --- DetectTagTypeFromUID Tests ---

func TestDetectTagTypeFromUID(t *testing.T) {
	tests := []struct {
		name     string
		expected pn532.TagType
		uid      []byte
	}{
		{
			name:     "7-byte UID starting with 0x04 (NTAG)",
			uid:      []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			expected: pn532.TagTypeNTAG,
		},
		{
			name:     "4-byte UID (MIFARE)",
			uid:      []byte{0x01, 0x02, 0x03, 0x04},
			expected: pn532.TagTypeMIFARE,
		},
		{
			name:     "7-byte UID not starting with 0x04",
			uid:      []byte{0x08, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
			expected: pn532.TagTypeUnknown,
		},
		{
			name:     "Empty UID",
			uid:      []byte{},
			expected: pn532.TagTypeUnknown,
		},
		{
			name:     "Single byte UID",
			uid:      []byte{0x04},
			expected: pn532.TagTypeUnknown,
		},
		{
			name:     "10-byte UID",
			uid:      []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09},
			expected: pn532.TagTypeUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectTagTypeFromUID(tc.uid)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// --- CompareUID Tests ---

func TestCompareUID(t *testing.T) {
	tests := []struct {
		name     string
		uid1     []byte
		uid2     []byte
		expected bool
	}{
		{
			name:     "Identical UIDs",
			uid1:     []byte{0x04, 0x01, 0x02, 0x03},
			uid2:     []byte{0x04, 0x01, 0x02, 0x03},
			expected: true,
		},
		{
			name:     "Different UIDs same length",
			uid1:     []byte{0x04, 0x01, 0x02, 0x03},
			uid2:     []byte{0x04, 0x01, 0x02, 0x04},
			expected: false,
		},
		{
			name:     "Different lengths",
			uid1:     []byte{0x04, 0x01, 0x02},
			uid2:     []byte{0x04, 0x01, 0x02, 0x03},
			expected: false,
		},
		{
			name:     "Both empty",
			uid1:     []byte{},
			uid2:     []byte{},
			expected: true,
		},
		{
			name:     "One empty",
			uid1:     []byte{0x04},
			uid2:     []byte{},
			expected: false,
		},
		{
			name:     "Both nil",
			uid1:     nil,
			uid2:     nil,
			expected: true,
		},
		{
			name:     "Nil vs empty",
			uid1:     nil,
			uid2:     []byte{},
			expected: true, // bytes.Equal treats nil and empty as equal
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := CompareUID(tc.uid1, tc.uid2)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// --- TagInfo Tests ---

func TestTagInfo_NTAGTypes(t *testing.T) {
	tests := []struct {
		name         string
		expectedType string
		totalPages   int
	}{
		{"NTAG213", "NTAG213", 45},
		{"NTAG215", "NTAG215", 135},
		{"NTAG216", "NTAG216", 231},
		{"Unknown NTAG", "NTAG (unknown, 100 pages)", 100},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create TagOperations with NTAG type and specified pages
			ops := &TagOperations{
				tagType:    pn532.TagTypeNTAG,
				totalPages: tc.totalPages,
				tag:        &pn532.DetectedTag{UIDBytes: []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}},
			}

			info, err := ops.GetTagInfo()
			require.NoError(t, err)
			assert.Equal(t, tc.expectedType, info.NTAGType)
			assert.Equal(t, ntagTypeName, info.TypeName)
		})
	}
}

func TestTagInfo_MIFARE(t *testing.T) {
	// 4-byte UID indicates 1K
	ops := &TagOperations{
		tagType: pn532.TagTypeMIFARE,
		tag:     &pn532.DetectedTag{UIDBytes: []byte{0x01, 0x02, 0x03, 0x04}},
	}

	info, err := ops.GetTagInfo()
	require.NoError(t, err)
	assert.Equal(t, mifareClassicName, info.TypeName)
	assert.Equal(t, "MIFARE Classic 1K", info.MIFAREType)
	assert.Equal(t, 16, info.Sectors)
	assert.Equal(t, 1024, info.TotalMemory)
}

func TestTagInfo_Unknown(t *testing.T) {
	ops := &TagOperations{
		tagType: pn532.TagTypeUnknown,
		tag:     &pn532.DetectedTag{UIDBytes: []byte{0x01}},
	}

	info, err := ops.GetTagInfo()
	require.NoError(t, err)
	assert.Equal(t, unknownTagName, info.TypeName)
}

func TestTagInfo_NoTag(t *testing.T) {
	ops := &TagOperations{}

	info, err := ops.GetTagInfo()
	require.Error(t, err)
	assert.Equal(t, ErrNoTag, err)
	assert.Nil(t, info)
}

// --- TagOperations Tests ---

func TestTagOperations_GetUID(t *testing.T) {
	expectedUID := []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	ops := &TagOperations{
		tag: &pn532.DetectedTag{UIDBytes: expectedUID},
	}

	uid := ops.GetUID()
	assert.Equal(t, expectedUID, uid)
}

func TestTagOperations_GetUID_NoTag(t *testing.T) {
	ops := &TagOperations{}

	uid := ops.GetUID()
	assert.Nil(t, uid)
}

func TestTagOperations_GetTagType(t *testing.T) {
	ops := &TagOperations{
		tagType: pn532.TagTypeNTAG,
	}

	assert.Equal(t, pn532.TagTypeNTAG, ops.GetTagType())
}

func TestNew(t *testing.T) {
	// New should create a TagOperations with nil tag and empty type
	ops := New(nil)

	assert.NotNil(t, ops)
	assert.Nil(t, ops.tag)
	// Empty string is the zero value for pn532.TagType (which is a string type)
	assert.Equal(t, pn532.TagType(""), ops.tagType)
}

// --- Error Constants ---

func TestErrors(t *testing.T) {
	// Verify error messages
	assert.Equal(t, "no tag detected", ErrNoTag.Error())
	assert.Equal(t, "unsupported tag type", ErrUnsupportedTag.Error())
	assert.Equal(t, "authentication failed with all known keys", ErrAuthFailed.Error())
}

// --- detectAndInitializeTag Tests ---

func TestTagOperations_detectAndInitializeTag_NoTag(t *testing.T) {
	ops := &TagOperations{
		tag: nil,
	}

	err := ops.detectAndInitializeTag(context.TODO())
	assert.Equal(t, ErrNoTag, err)
}

// --- TagInfo MIFARE Variants ---

func TestTagInfo_MIFARE_UnknownSize(t *testing.T) {
	// 7-byte UID results in unknown size (UID length doesn't determine card type)
	ops := &TagOperations{
		tagType: pn532.TagTypeMIFARE,
		tag:     &pn532.DetectedTag{UIDBytes: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}},
	}

	info, err := ops.GetTagInfo()
	require.NoError(t, err)
	assert.Equal(t, mifareClassicName, info.TypeName)
	// 7-byte UIDs result in unknown size since we can't determine from UID alone
	assert.Contains(t, info.MIFAREType, "MIFARE Classic")
}

// --- Reader Tests ---

func TestTagOperations_ReadBlocks_NoTag(t *testing.T) {
	ops := &TagOperations{
		tag: nil,
	}

	blocks, err := ops.ReadBlocks(context.TODO(), 0, 5)
	require.Error(t, err)
	assert.Nil(t, blocks)
}

func TestTagOperations_ReadBlocks_UnsupportedType(t *testing.T) {
	ops := &TagOperations{
		tag:     &pn532.DetectedTag{UIDBytes: []byte{0x01}},
		tagType: pn532.TagTypeUnknown,
	}

	blocks, err := ops.ReadBlocks(context.TODO(), 0, 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported tag type")
	assert.Nil(t, blocks)
}

func TestTagOperations_ReadNDEF_NoTag(t *testing.T) {
	ops := &TagOperations{
		tag: nil,
	}

	message, err := ops.ReadNDEF(context.TODO())
	require.Error(t, err)
	assert.Nil(t, message)
}

func TestTagOperations_ReadNDEF_UnsupportedType(t *testing.T) {
	ops := &TagOperations{
		tag:     &pn532.DetectedTag{UIDBytes: []byte{0x01}},
		tagType: pn532.TagTypeUnknown,
	}

	message, err := ops.ReadNDEF(context.TODO())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported tag type")
	assert.Nil(t, message)
}

func TestTagOperations_ReadNDEF_UnauthenticatedMIFARE(t *testing.T) {
	// Unauthenticated MIFARE tags should return empty NDEF (not error)
	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)

	mifare := pn532.NewMIFARETag(device, []byte{0x01, 0x02, 0x03, 0x04}, 0x08)
	// Don't call TryAuthenticate - tag is unauthenticated

	ops := &TagOperations{
		tag:            &pn532.DetectedTag{UIDBytes: []byte{0x01, 0x02, 0x03, 0x04}},
		tagType:        pn532.TagTypeMIFARE,
		mifareInstance: mifare,
	}

	message, err := ops.ReadNDEF(context.Background())
	require.NoError(t, err, "Unauthenticated MIFARE should not error")
	require.NotNil(t, message, "Should return empty NDEF message")
	assert.Empty(t, message.Records, "NDEF message should have no records")
}

// --- Writer Tests ---

func TestTagOperations_WriteBlocks_NoTag(t *testing.T) {
	ops := &TagOperations{
		tag: nil,
	}

	err := ops.WriteBlocks(context.TODO(), 0, []byte{0x01})
	require.Error(t, err)
}

func TestTagOperations_WriteBlocks_UnsupportedType(t *testing.T) {
	ops := &TagOperations{
		tag:     &pn532.DetectedTag{UIDBytes: []byte{0x01}},
		tagType: pn532.TagTypeUnknown,
	}

	err := ops.WriteBlocks(context.TODO(), 0, []byte{0x01})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestTagOperations_WriteNDEF_NoTag(t *testing.T) {
	ops := &TagOperations{
		tag: nil,
	}

	err := ops.WriteNDEF(context.TODO(), &pn532.NDEFMessage{})
	require.Error(t, err)
}

func TestTagOperations_WriteNDEF_UnsupportedType(t *testing.T) {
	ops := &TagOperations{
		tag:     &pn532.DetectedTag{UIDBytes: []byte{0x01}},
		tagType: pn532.TagTypeUnknown,
	}

	err := ops.WriteNDEF(context.TODO(), &pn532.NDEFMessage{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported tag type")
}

// --- UserMemory Tests (Regression) ---
// These tests verify the corrected UserMemory calculations in GetTagInfo()

func TestTagInfo_NTAGUserMemory(t *testing.T) {
	tests := []struct {
		name               string
		totalPages         int
		expectedUserMemory int
	}{
		{
			name:               "NTAG213_UserMemory",
			totalPages:         45,
			expectedUserMemory: 144, // 36 pages * 4 bytes
		},
		{
			name:               "NTAG215_UserMemory",
			totalPages:         135,
			expectedUserMemory: 504, // 126 pages * 4 bytes
		},
		{
			name:               "NTAG216_UserMemory",
			totalPages:         231,
			expectedUserMemory: 888, // 222 pages * 4 bytes
		},
		{
			name:               "Unknown_NTAG_UserMemory",
			totalPages:         100,
			expectedUserMemory: 364, // (100 - 9) * 4 = 91 * 4 = 364
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ops := &TagOperations{
				tagType:    pn532.TagTypeNTAG,
				totalPages: tc.totalPages,
				tag:        &pn532.DetectedTag{UIDBytes: []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}},
			}

			info, err := ops.GetTagInfo()
			require.NoError(t, err)
			assert.Equal(t, tc.expectedUserMemory, info.UserMemory,
				"UserMemory should be calculated correctly for %s", tc.name)
		})
	}
}

func TestTagInfo_MIFAREUserMemory(t *testing.T) {
	tests := []struct {
		name               string
		uid                []byte
		expectedUserMemory int
	}{
		{
			name:               "MIFARE_Classic_1K_UserMemory",
			uid:                []byte{0x01, 0x02, 0x03, 0x04}, // 4-byte UID = 1K
			expectedUserMemory: 720,                            // 15 sectors * 3 blocks * 16 bytes
		},
		{
			name:               "MIFARE_Unknown_Size_UserMemory",
			uid:                []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}, // 7-byte UID = unknown
			expectedUserMemory: 720,                                              // defaults to 720
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ops := &TagOperations{
				tagType: pn532.TagTypeMIFARE,
				tag:     &pn532.DetectedTag{UIDBytes: tc.uid},
			}

			info, err := ops.GetTagInfo()
			require.NoError(t, err)
			assert.Equal(t, tc.expectedUserMemory, info.UserMemory,
				"UserMemory should be calculated correctly for %s", tc.name)
		})
	}
}

// --- Detection Fallback Logic Tests (Regression) ---
// These tests verify the refactored detectAndInitializeTag that avoids
// redundant detection attempts when one type fails.

func TestDetectAndInitializeTag_NoTag_ReturnsError(t *testing.T) {
	ops := &TagOperations{
		tag: nil,
	}

	err := ops.detectAndInitializeTag(context.Background())

	require.Error(t, err)
	assert.Equal(t, ErrNoTag, err)
}

func TestTryInitNTAG_FailsOn4ByteUID(t *testing.T) {
	// Verify that tryInitNTAG correctly fails for non-7-byte UIDs
	// This is important because the UID length check happens before
	// any device I/O, so we can test it without mocking device responses

	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)
	mockTransport.SelectTarget()

	ops := New(device)
	ops.tag = &pn532.DetectedTag{
		UIDBytes: []byte{0x01, 0x02, 0x03, 0x04}, // 4-byte UID - invalid for NTAG
	}

	result := ops.tryInitNTAG(context.Background())

	assert.False(t, result, "tryInitNTAG should fail for 4-byte UID")
}

func TestTryInitNTAG_SucceedsWithValidTag(t *testing.T) {
	// Verify tryInitNTAG succeeds with a valid 7-byte UID and proper mocking

	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)
	mockTransport.SelectTarget()

	// Mock valid CC read
	ccData := make([]byte, 16)
	ccData[0] = 0xE1 // NDEF magic
	ccData[1] = 0x10 // Version
	ccData[2] = 0x12 // Size
	mockTransport.QueueResponses(0x40,
		append([]byte{0x41, 0x00}, ccData...),                              // CC read succeeds
		[]byte{0x41, 0x00, 0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x0F, 0x03}, // GET_VERSION response for NTAG213
	)

	ops := New(device)
	ops.tag = &pn532.DetectedTag{
		UIDBytes: []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}, // 7-byte NXP UID
	}

	result := ops.tryInitNTAG(context.Background())

	assert.True(t, result, "tryInitNTAG should succeed with valid 7-byte UID")
	assert.Equal(t, pn532.TagTypeNTAG, ops.tagType)
}

func TestTryInitMIFARE_SucceedsWithValidAuth(t *testing.T) {
	// Verify that tryInitMIFARE succeeds when auth works

	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)
	mockTransport.SelectTarget()

	// Mock successful auth and read response
	mockTransport.SetResponse(0x40, append([]byte{0x41, 0x00}, make([]byte, 16)...))

	ops := New(device)
	ops.tag = &pn532.DetectedTag{
		UIDBytes: []byte{0x01, 0x02, 0x03, 0x04},
		SAK:      0x08, // MIFARE Classic 1K SAK
	}

	result := ops.tryInitMIFARE(context.Background())

	assert.True(t, result, "tryInitMIFARE should succeed with valid auth")
	assert.Equal(t, pn532.TagTypeMIFARE, ops.tagType)
}

func TestDetectAndInitializeTag_UnknownFallsBackToMIFARE(t *testing.T) {
	// Test that when type is Unknown and NTAG init fails,
	// the code falls back to MIFARE and succeeds if MIFARE auth works.
	// Note: Only Unknown/FeliCa types try both NTAG and MIFARE fallback.
	// Pre-typed tags (NTAG, MIFARE) only try their specific init.

	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)
	mockTransport.SelectTarget()

	// Mock returns success for MIFARE auth
	mockTransport.SetResponse(0x40, append([]byte{0x41, 0x00}, make([]byte, 16)...))

	ops := New(device)
	ops.tag = &pn532.DetectedTag{
		Type:     pn532.TagTypeUnknown,           // Unknown type triggers fallback
		UIDBytes: []byte{0x01, 0x02, 0x03, 0x04}, // 4-byte UID
		SAK:      0x08,
	}

	err = ops.detectAndInitializeTag(context.Background())

	// Should succeed via MIFARE fallback (NTAG fails, MIFARE succeeds)
	require.NoError(t, err)
	assert.Equal(t, pn532.TagTypeMIFARE, ops.tagType, "Should fall back to MIFARE")
}

// --- Reader Function Tests ---

func TestProbeActualMemorySize_NoTag(t *testing.T) {
	ops := &TagOperations{}

	lastPage, userMemory, err := ops.ProbeActualMemorySize(context.Background(), 144)

	require.Error(t, err)
	assert.Equal(t, ErrNoTag, err)
	assert.Equal(t, uint8(0), lastPage)
	assert.Equal(t, 0, userMemory)
}

func TestProbeActualMemorySize_UnsupportedTag(t *testing.T) {
	ops := &TagOperations{
		tag:     &pn532.DetectedTag{UIDBytes: []byte{0x01, 0x02, 0x03, 0x04}},
		tagType: pn532.TagTypeMIFARE, // Not NTAG
	}

	lastPage, userMemory, err := ops.ProbeActualMemorySize(context.Background(), 144)

	require.Error(t, err)
	assert.Equal(t, ErrUnsupportedTag, err)
	assert.Equal(t, uint8(0), lastPage)
	assert.Equal(t, 0, userMemory)
}

func TestProbeActualMemorySize_NilNtagInstance(t *testing.T) {
	ops := &TagOperations{
		tag:          &pn532.DetectedTag{UIDBytes: []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}},
		tagType:      pn532.TagTypeNTAG,
		ntagInstance: nil, // NTAG type but no instance
	}

	lastPage, userMemory, err := ops.ProbeActualMemorySize(context.Background(), 144)

	require.Error(t, err)
	assert.Equal(t, ErrUnsupportedTag, err)
	assert.Equal(t, uint8(0), lastPage)
	assert.Equal(t, 0, userMemory)
}

func TestReadCapabilityContainer_NoTag(t *testing.T) {
	ops := &TagOperations{}

	data, err := ops.ReadCapabilityContainer(context.Background())

	require.Error(t, err)
	assert.Equal(t, ErrNoTag, err)
	assert.Nil(t, data)
}

func TestReadCapabilityContainer_UnsupportedTag(t *testing.T) {
	ops := &TagOperations{
		tag:     &pn532.DetectedTag{UIDBytes: []byte{0x01, 0x02, 0x03, 0x04}},
		tagType: pn532.TagTypeMIFARE, // Not NTAG
	}

	data, err := ops.ReadCapabilityContainer(context.Background())

	require.Error(t, err)
	assert.Equal(t, ErrUnsupportedTag, err)
	assert.Nil(t, data)
}

func TestReadCapabilityContainer_NilNtagInstance(t *testing.T) {
	ops := &TagOperations{
		tag:          &pn532.DetectedTag{UIDBytes: []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}},
		tagType:      pn532.TagTypeNTAG,
		ntagInstance: nil, // NTAG type but no instance
	}

	data, err := ops.ReadCapabilityContainer(context.Background())

	require.Error(t, err)
	assert.Equal(t, ErrUnsupportedTag, err)
	assert.Nil(t, data)
}

func TestGetClaimedSizeFromCC(t *testing.T) {
	tests := []struct {
		name     string
		ccData   []byte
		expected int
	}{
		{
			name:     "NTAG213_Size",
			ccData:   []byte{0xE1, 0x10, 0x12, 0x00}, // 0x12 * 8 = 144 bytes
			expected: 144,
		},
		{
			name:     "NTAG215_Size",
			ccData:   []byte{0xE1, 0x10, 0x3E, 0x00}, // 0x3E * 8 = 496 bytes
			expected: 496,
		},
		{
			name:     "NTAG216_Size",
			ccData:   []byte{0xE1, 0x10, 0x6D, 0x00}, // 0x6D * 8 = 872 bytes
			expected: 872,
		},
		{
			name:     "Empty_CC",
			ccData:   []byte{},
			expected: 0,
		},
		{
			name:     "Short_CC",
			ccData:   []byte{0xE1, 0x10},
			expected: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := GetClaimedSizeFromCC(tc.ccData)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// --- Stabilization and Retry Tests ---
// These tests verify the robust initialization behavior added to handle
// the "sliding card into slot" use case.

func TestInitFromDetectedTag_NilTag(t *testing.T) {
	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)

	ops := New(device)

	err = ops.InitFromDetectedTag(context.Background(), nil)

	require.Error(t, err)
	assert.Equal(t, ErrNoTag, err)
}

func TestInitFromDetectedTag_Success(t *testing.T) {
	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)
	mockTransport.SelectTarget()

	// Mock valid CC read for NTAG
	ccData := make([]byte, 16)
	ccData[0] = 0xE1 // NDEF magic
	ccData[1] = 0x10 // Version
	ccData[2] = 0x12 // Size (NTAG213)
	mockTransport.SetResponse(0x40, append([]byte{0x41, 0x00}, ccData...))

	ops := New(device)
	tag := &pn532.DetectedTag{
		UID:      "04010203040506",
		UIDBytes: []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
		Type:     pn532.TagTypeNTAG,
	}

	err = ops.InitFromDetectedTag(context.Background(), tag)

	require.NoError(t, err)
	assert.Equal(t, pn532.TagTypeNTAG, ops.tagType)
}

func TestIsDefinitivelyNotNTAG(t *testing.T) {
	tests := []struct {
		err      error
		name     string
		expected bool
	}{
		{
			name:     "Nil_error",
			err:      nil,
			expected: false,
		},
		{
			name:     "Real_4byte_UID_error",
			err:      errors.New("not an NTAG tag: UID must be 7 bytes, got 4 bytes"),
			expected: true,
		},
		{
			name:     "Zero_UID_parse_failed",
			err:      errors.New("UID must be 7 bytes - parse failed"),
			expected: false, // Contains "parse failed" so it's retryable
		},
		{
			name:     "Other_error",
			err:      errors.New("some other error"),
			expected: false,
		},
		{
			name:     "Invalid_CC_error",
			err:      errors.New("not an NTAG tag: invalid capability container"),
			expected: false, // This is retryable
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isDefinitivelyNotNTAG(tc.err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestTryInitNTAG_RetriesOnTransientErrors(t *testing.T) {
	// Verify that tryInitNTAG retries when it gets transient errors
	// This tests the "retry on any error" behavior

	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)
	mockTransport.SelectTarget()

	// First read fails (transient), second succeeds
	ccDataBad := []byte{0x41, 0x00, 0xFF, 0xFF, 0xFF, 0xFF} // Invalid CC (no 0xE1 magic)
	ccDataGood := make([]byte, 16)
	ccDataGood[0] = 0xE1 // NDEF magic
	ccDataGood[1] = 0x10 // Version
	ccDataGood[2] = 0x12 // Size (NTAG213)

	mockTransport.QueueResponses(0x40,
		ccDataBad, // First attempt: garbage CC
		append([]byte{0x41, 0x00}, ccDataGood...), // Second attempt: valid CC
	)

	ops := New(device)
	ops.tag = &pn532.DetectedTag{
		UIDBytes: []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
	}

	result := ops.tryInitNTAG(context.Background())

	assert.True(t, result, "tryInitNTAG should succeed after retry")
	assert.Equal(t, pn532.TagTypeNTAG, ops.tagType)
}

func TestTryInitNTAG_StopsOnDefinitiveFailure(t *testing.T) {
	// Verify that tryInitNTAG stops immediately for definitive failures
	// (like a real 4-byte UID which indicates MIFARE, not NTAG)

	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)
	mockTransport.SelectTarget()

	ops := New(device)
	ops.tag = &pn532.DetectedTag{
		UIDBytes: []byte{0x01, 0x02, 0x03, 0x04}, // Real 4-byte UID = MIFARE
	}

	// No mock responses needed - should fail on UID check before any I/O

	result := ops.tryInitNTAG(context.Background())

	assert.False(t, result, "tryInitNTAG should fail immediately for 4-byte UID")
	// Verify no device I/O happened (call count should be 0)
	assert.Equal(t, 0, mockTransport.GetCallCount(0x40))
}

func TestTryInitMIFARE_RetriesOnTransientErrors(t *testing.T) {
	// Verify that tryInitMIFARE attempts multiple retries on auth failure
	// Even when all auth attempts fail, MIFARE init succeeds (tag is usable for UID)
	// but the MIFARE instance will be marked as unauthenticated

	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)
	mockTransport.SelectTarget()

	// Make all auth attempts fail - we just want to verify retry count
	mockTransport.SetResponse(0x40, []byte{0x41, 0x01}) // Timeout error

	ops := New(device)
	ops.tag = &pn532.DetectedTag{
		UIDBytes: []byte{0x01, 0x02, 0x03, 0x04},
		SAK:      0x08,
	}

	result := ops.tryInitMIFARE(context.Background())

	// MIFARE init now succeeds even without auth (tag is usable for UID-only)
	assert.True(t, result, "tryInitMIFARE should succeed (unauthenticated)")
	assert.NotNil(t, ops.mifareInstance, "MIFARE instance should be set")
	assert.False(t, ops.mifareInstance.IsAuthenticated(), "MIFARE should be unauthenticated")
	// Verify multiple attempts were made
	assert.GreaterOrEqual(t, mockTransport.GetCallCount(0x40), 2,
		"Should make multiple retry attempts")
}

func TestDetectAndInitializeTag_TriesFallbackOnNTAGFailure(t *testing.T) {
	t.Parallel()

	// Verify that when NTAG init fails, MIFARE fallback is attempted
	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)
	mockTransport.SelectTarget()

	// Make all init attempts fail
	mockTransport.SetResponse(0x40, []byte{0x41, 0x01}) // Timeout error

	ops := New(device)
	ops.tag = &pn532.DetectedTag{
		Type:     pn532.TagTypeNTAG,
		UIDBytes: []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
		SAK:      0x00,
	}

	// Should fail after trying both NTAG and MIFARE
	err = ops.detectAndInitializeTag(context.Background())
	require.Error(t, err)
	assert.Equal(t, ErrUnsupportedTag, err)

	// Verify commands were sent (NTAG read and MIFARE auth attempts via InDataExchange)
	assert.Positive(t, mockTransport.GetCallCount(0x40), "Should have sent commands")
}

func TestDetectAndInitializeTag_MIFARESucceedsWithoutAuth(t *testing.T) {
	t.Parallel()

	// Verify that MIFARE init succeeds even when auth fails (unauthenticated mode)
	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)
	mockTransport.SelectTarget()

	// Make all auth attempts fail
	mockTransport.SetResponse(0x40, []byte{0x41, 0x14}) // Auth error

	ops := New(device)
	ops.tag = &pn532.DetectedTag{
		Type:     pn532.TagTypeMIFARE,
		UIDBytes: []byte{0x01, 0x02, 0x03, 0x04},
		SAK:      0x08,
	}

	// Should succeed - MIFARE tags are valid even without auth (UID-only mode)
	err = ops.detectAndInitializeTag(context.Background())
	require.NoError(t, err)
	assert.Equal(t, pn532.TagTypeMIFARE, ops.tagType)
	assert.NotNil(t, ops.mifareInstance)
	assert.False(t, ops.mifareInstance.IsAuthenticated(), "Should be unauthenticated")

	// Verify auth commands were attempted
	assert.Positive(t, mockTransport.GetCallCount(0x40), "Should have sent auth commands")
}

// --- Retry Constants Tests ---

func TestRetryConstants(t *testing.T) {
	// Verify the retry constants are set to expected values
	assert.Equal(t, 5, initMaxRetries,
		"initMaxRetries should be 5")
	assert.Equal(t, 50*time.Millisecond, initRetryDelay,
		"initRetryDelay should be 50ms")
}

// --- GetCachedCapabilityContainer Tests ---

func TestTagOperations_GetCachedCapabilityContainer_NilNtagInstance(t *testing.T) {
	ops := &TagOperations{
		tag:          &pn532.DetectedTag{UIDBytes: []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}},
		tagType:      pn532.TagTypeNTAG,
		ntagInstance: nil, // NTAG type but no instance
	}

	cc := ops.GetCachedCapabilityContainer()

	assert.Nil(t, cc, "GetCachedCapabilityContainer should return nil when ntagInstance is nil")
}

func TestTagOperations_GetCachedCapabilityContainer_NotNTAG(t *testing.T) {
	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)

	mifare := pn532.NewMIFARETag(device, []byte{0x01, 0x02, 0x03, 0x04}, 0x08)

	ops := &TagOperations{
		tag:            &pn532.DetectedTag{UIDBytes: []byte{0x01, 0x02, 0x03, 0x04}},
		tagType:        pn532.TagTypeMIFARE,
		mifareInstance: mifare,
		ntagInstance:   nil,
	}

	cc := ops.GetCachedCapabilityContainer()

	assert.Nil(t, cc, "GetCachedCapabilityContainer should return nil for non-NTAG tags")
}

func TestTagOperations_GetCachedCapabilityContainer_ReturnsCC(t *testing.T) {
	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	require.NoError(t, err)
	mockTransport.SelectTarget()

	// Expected CC for NTAG215: [0xE1, 0x10, 0x3E, 0x00]
	expectedCC := []byte{0xE1, 0x10, 0x3E, 0x00}
	ccResponse := make([]byte, 16) // NTAG read returns 16 bytes
	copy(ccResponse, expectedCC)

	// Mock CC read response
	mockTransport.SetResponse(0x40, append([]byte{0x41, 0x00}, ccResponse...))

	ops := New(device)
	tag := &pn532.DetectedTag{
		UID:      "04010203040506",
		UIDBytes: []byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06},
		Type:     pn532.TagTypeNTAG,
	}

	err = ops.InitFromDetectedTag(context.Background(), tag)
	require.NoError(t, err)

	// After initialization, GetCachedCapabilityContainer should return the CC
	cc := ops.GetCachedCapabilityContainer()
	require.NotNil(t, cc, "GetCachedCapabilityContainer should return cached CC after init")
	assert.Equal(t, expectedCC, cc, "Cached CC should match the CC read during DetectType")
}

// --- MakeReadOnly Tests ---

func TestTagOperations_MakeReadOnly_NoTag(t *testing.T) {
	ops := &TagOperations{
		tag: nil,
	}

	err := ops.MakeReadOnly(context.TODO())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoTag)
}

func TestTagOperations_MakeReadOnly_MIFARE(t *testing.T) {
	ops := &TagOperations{
		tag:     &pn532.DetectedTag{UIDBytes: []byte{0x01}},
		tagType: pn532.TagTypeMIFARE,
	}

	err := ops.MakeReadOnly(context.TODO())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLockNotSupported)
}

func TestTagOperations_MakeReadOnly_UnsupportedTypes(t *testing.T) {
	unsupportedTypes := []pn532.TagType{
		pn532.TagTypeUnknown,
		pn532.TagTypeFeliCa,
	}

	for _, tagType := range unsupportedTypes {
		ops := &TagOperations{
			tag:     &pn532.DetectedTag{UIDBytes: []byte{0x01}},
			tagType: tagType,
		}

		err := ops.MakeReadOnly(context.TODO())
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrUnsupportedTag)
	}
}
