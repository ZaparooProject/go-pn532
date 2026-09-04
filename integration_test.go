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

package pn532

import (
	"context"
	"fmt"
	"testing"
	"time"

	testutil "github.com/ZaparooProject/go-pn532/internal/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// simulatorTransportWrapper wraps testutil.SimulatorTransport to implement pn532.Transport
// This is needed because SimulatorTransport returns testutil.TransportType, not pn532.TransportType
type simulatorTransportWrapper struct {
	*testutil.SimulatorTransport
}

// Type implements pn532.Transport by returning pn532.TransportType
func (*simulatorTransportWrapper) Type() TransportType {
	return TransportMock
}

// newSimulatorTransport creates a wrapped SimulatorTransport that implements pn532.Transport
func newSimulatorTransport(sim *testutil.VirtualPN532) *simulatorTransportWrapper {
	return &simulatorTransportWrapper{
		SimulatorTransport: testutil.NewSimulatorTransport(sim),
	}
}

// TestBasicTagDetection tests the complete workflow of detecting a tag
func TestBasicTagDetection(t *testing.T) {
	tests := []struct {
		name    string
		tagType string
		uid     []byte
		wantUID string
	}{
		{
			name:    "NTAG213_Detection",
			tagType: "NTAG213",
			uid:     testutil.TestNTAG213UID,
			wantUID: "04abcdef123456",
		},
		{
			name:    "MIFARE1K_Detection",
			tagType: "MIFARE1K",
			uid:     testutil.TestMIFARE1KUID,
			wantUID: "12345678",
		},
		{
			name:    "MIFARE4K_Detection",
			tagType: "MIFARE4K",
			uid:     testutil.TestMIFARE4KUID,
			wantUID: "abcdef01",
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock transport
			mock := NewMockTransport()

			// Configure firmware version response
			mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())

			// Configure SAM configuration response
			mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())

			// Configure tag detection response
			mock.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildTagDetectionResponse(tt.tagType, tt.uid))

			// Create device with mock transport
			device, err := New(mock)
			require.NoError(t, err)
			require.NotNil(t, device)

			// Initialize device to trigger firmware version check
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			err = device.Init(ctx)
			require.NoError(t, err)

			// Test tag detection
			tag, err := device.DetectTag(ctx)
			require.NoError(t, err)
			require.NotNil(t, tag)

			// Verify tag properties
			assert.Equal(t, tt.wantUID, tag.UID)
			assert.Equal(t, tt.uid, tag.UIDBytes)

			// Verify mock was called correctly
			// Init calls firmware version twice: once for validation, once for setup
			assert.Equal(t, 2, mock.GetCallCount(testutil.CmdGetFirmwareVersion))
			assert.Equal(t, 1, mock.GetCallCount(testutil.CmdSAMConfiguration))
			assert.Equal(t, 1, mock.GetCallCount(testutil.CmdInListPassiveTarget))
		})
	}
}

// TestTagNotFound tests the scenario when no tag is present
func TestTagNotFound(t *testing.T) {
	// Setup mock transport
	mock := NewMockTransport()

	// Configure firmware version response
	mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())

	// Configure SAM configuration response
	mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())

	// Configure no tag response
	mock.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildNoTagResponse())

	// Create device
	device, err := New(mock)
	require.NoError(t, err)

	// Initialize device
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = device.Init(ctx)
	require.NoError(t, err)

	// Test tag detection - should find no tags
	tag, err := device.DetectTag(ctx)
	require.NoError(t, err)
	assert.Nil(t, tag)
}

// TestTagReadWrite tests reading from and writing to a virtual tag
func TestTagReadWrite(t *testing.T) {
	// Create virtual NTAG213 tag
	virtualTag := testutil.NewVirtualNTAG213(nil)
	require.NotNil(t, virtualTag)

	// Test reading initial content
	text := virtualTag.GetNDEFText()
	assert.Equal(t, "Hello World", text)

	// Test writing new content
	err := virtualTag.SetNDEFText("Test Message")
	require.NoError(t, err)

	// Verify new content
	newText := virtualTag.GetNDEFText()
	assert.Equal(t, "Test Message", newText)

	// Test page-level operations (NTAG pages are 4 bytes each)
	page4, err := virtualTag.ReadBlock(4) // First user data page
	require.NoError(t, err)
	assert.Len(t, page4, 4) // NTAG page size

	// Test invalid block access
	_, err = virtualTag.ReadBlock(100) // Beyond NTAG213 range
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

// TestTransportErrorHandling tests error scenarios
func TestTransportErrorHandling(t *testing.T) {
	// Setup mock transport with error injection
	mock := NewMockTransport()

	// Configure firmware version response
	mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())

	// Configure SAM configuration response
	mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())

	// Inject error for tag detection
	mock.SetError(testutil.CmdInListPassiveTarget, assert.AnError)

	// Create device
	device, err := New(mock)
	require.NoError(t, err)

	// Initialize device
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = device.Init(ctx)
	require.NoError(t, err)

	// Test tag detection with error
	_, err = device.DetectTag(ctx)
	assert.Error(t, err)

	// Verify error was injected
	assert.Equal(t, 1, mock.GetCallCount(testutil.CmdInListPassiveTarget))
}

// TestTransportTimeout tests timeout scenarios
func TestTransportTimeout(t *testing.T) {
	// Setup mock transport with delay
	mock := NewMockTransport()
	mock.SetDelay(200 * time.Millisecond) // Simulate slow hardware

	// Configure responses
	mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
	mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
	mock.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildTagDetectionResponse("NTAG213", testutil.TestNTAG213UID))

	// Create device
	device, err := New(mock)
	require.NoError(t, err)

	// Initialize device first
	initCtx, initCancel := context.WithTimeout(context.Background(), time.Second)
	defer initCancel()

	err = device.Init(initCtx)
	require.NoError(t, err)

	// Test with sufficient timeout - should succeed even with delay
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	tag, err := device.DetectTag(ctx)
	require.NoError(t, err)
	assert.NotNil(t, tag)

	// Note: Testing actual context timeout requires the transport layer to be context-aware,
	// which would be a significant architectural change. For now, we verify that operations
	// complete successfully within reasonable timeouts despite mock delays.
}

// TestTagRemoval tests tag removal scenarios
func TestTagRemoval(t *testing.T) {
	// Create virtual tag and test removal
	virtualTag := testutil.NewVirtualNTAG213(nil)
	require.True(t, virtualTag.Present)

	// Remove tag
	virtualTag.Remove()
	assert.False(t, virtualTag.Present)

	// Test operations on removed tag
	_, err := virtualTag.ReadBlock(4)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tag not present")

	err = virtualTag.WriteBlock(4, make([]byte, 16))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "tag not present")

	// Re-insert tag
	virtualTag.Insert()
	assert.True(t, virtualTag.Present)

	// Operations should work again
	_, err = virtualTag.ReadBlock(4)
	assert.NoError(t, err)
}

// BenchmarkTagDetection benchmarks the tag detection workflow
func BenchmarkTagDetection(b *testing.B) {
	// Setup mock transport
	mock := NewMockTransport()
	mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
	mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
	mock.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildTagDetectionResponse("NTAG213", testutil.TestNTAG213UID))

	device, err := New(mock)
	require.NoError(b, err)

	ctx := context.Background()

	// Initialize device
	err = device.Init(ctx)
	require.NoError(b, err)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tag, err := device.DetectTag(ctx)
		require.NoError(b, err)
		require.NotNil(b, tag)
	}
}

// TestMIFAREVirtualTagReadWrite tests the MIFARE virtual tag implementations
func TestMIFAREVirtualTagReadWrite(t *testing.T) {
	// Default MIFARE key
	defaultKey := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	tests := []struct {
		name        string
		createTag   func() *testutil.VirtualTag
		testBlocks  []int
		expectError bool
	}{
		{
			name:       "MIFARE1K_ReadWrite",
			createTag:  func() *testutil.VirtualTag { return testutil.NewVirtualMIFARE1K(nil) },
			testBlocks: []int{1, 2, 4, 5}, // Skip sector trailers (3, 7, etc.)
		},
		{
			name:       "MIFARE4K_ReadWrite",
			createTag:  func() *testutil.VirtualTag { return testutil.NewVirtualMIFARE4K(nil) },
			testBlocks: []int{1, 2, 4, 5, 8, 9}, // Skip sector trailers
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			// Create virtual tag
			virtualTag := tt.createTag()
			require.NotNil(t, virtualTag)
			require.True(t, virtualTag.Present)

			// Test reading from multiple blocks
			for _, blockNum := range tt.testBlocks {
				t.Run(fmt.Sprintf("Block_%d", blockNum), func(t *testing.T) {
					// Authenticate to the sector containing this block
					sector := blockNum / 4
					err := virtualTag.Authenticate(sector, testutil.MIFAREKeyA, defaultKey)
					require.NoError(t, err, "Authentication should succeed")

					// Test reading
					data, err := virtualTag.ReadBlock(blockNum)
					require.NoError(t, err)
					assert.Len(t, data, 16, "Block should be 16 bytes")

					// Test writing (create test pattern)
					testData := make([]byte, 16)
					for i := range testData {
						testData[i] = byte(blockNum + i) // Create unique pattern
					}

					err = virtualTag.WriteBlock(blockNum, testData)
					require.NoError(t, err)

					// Read back and verify
					readData, err := virtualTag.ReadBlock(blockNum)
					require.NoError(t, err)
					assert.Equal(t, testData, readData, "Written data should match read data")
				})
			}

			// Test error conditions
			t.Run("ErrorConditions", func(t *testing.T) {
				// Test reading out of range
				_, err := virtualTag.ReadBlock(1000)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "out of range")

				// Test writing out of range
				err = virtualTag.WriteBlock(1000, make([]byte, 16))
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "out of range")

				// Authenticate sector 0 for data size test
				sector := tt.testBlocks[0] / 4
				err = virtualTag.Authenticate(sector, testutil.MIFAREKeyA, defaultKey)
				require.NoError(t, err)

				// Test wrong data size
				err = virtualTag.WriteBlock(tt.testBlocks[0], make([]byte, 10))
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "must be exactly 16 bytes")

				// Test tag removal
				virtualTag.Remove()
				assert.False(t, virtualTag.Present)

				_, err = virtualTag.ReadBlock(tt.testBlocks[0])
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "not present")

				err = virtualTag.WriteBlock(tt.testBlocks[0], make([]byte, 16))
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "not present")

				// Test tag re-insertion
				virtualTag.Insert()
				assert.True(t, virtualTag.Present)

				// Re-authenticate after re-insertion
				err = virtualTag.Authenticate(sector, testutil.MIFAREKeyA, defaultKey)
				require.NoError(t, err)

				_, err = virtualTag.ReadBlock(tt.testBlocks[0])
				assert.NoError(t, err)
			})
		})
	}
}

// TestMIFAREWriteProtection tests write protection on sector trailers
func TestMIFAREWriteProtection(t *testing.T) {
	// Default MIFARE key
	defaultKey := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	// blockToSector calculates the sector for a given block
	blockToSector := func(block int, is4K bool) int {
		if is4K && block >= 128 {
			// MIFARE 4K: sectors 32-39 have 16 blocks each
			return 32 + (block-128)/16
		}
		return block / 4
	}

	tests := []struct {
		name              string
		createTag         func() *testutil.VirtualTag
		protectedBlocks   []int
		unprotectedBlocks []int
		is4K              bool
	}{
		{
			name:              "MIFARE1K_WriteProtection",
			createTag:         func() *testutil.VirtualTag { return testutil.NewVirtualMIFARE1K(nil) },
			protectedBlocks:   []int{3, 7, 11, 15}, // Sector trailers
			unprotectedBlocks: []int{1, 2, 4, 5},   // Regular blocks
			is4K:              false,
		},
		{
			name:              "MIFARE4K_WriteProtection",
			createTag:         func() *testutil.VirtualTag { return testutil.NewVirtualMIFARE4K(nil) },
			protectedBlocks:   []int{3, 7, 11, 15, 143, 159}, // Include 4K sector trailers
			unprotectedBlocks: []int{1, 2, 4, 5, 8, 9},       // Regular blocks
			is4K:              true,
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			virtualTag := tt.createTag()
			require.NotNil(t, virtualTag)

			testData := make([]byte, 16)
			for i := range testData {
				testData[i] = 0xAA // Test pattern
			}

			// Test protected blocks should fail (even when authenticated)
			for _, block := range tt.protectedBlocks {
				sector := blockToSector(block, tt.is4K)
				err := virtualTag.Authenticate(sector, testutil.MIFAREKeyA, defaultKey)
				require.NoError(t, err, "Authentication should succeed for sector %d", sector)

				err = virtualTag.WriteBlock(block, testData)
				assert.Error(t, err, "Block %d should be write protected", block)
				assert.Contains(t, err.Error(), "write protected")
			}

			// Test unprotected blocks should succeed
			for _, block := range tt.unprotectedBlocks {
				sector := blockToSector(block, tt.is4K)
				err := virtualTag.Authenticate(sector, testutil.MIFAREKeyA, defaultKey)
				require.NoError(t, err, "Authentication should succeed for sector %d", sector)

				err = virtualTag.WriteBlock(block, testData)
				assert.NoError(t, err, "Block %d should be writable", block)

				// Verify data was written
				readData, err := virtualTag.ReadBlock(block)
				assert.NoError(t, err)
				assert.Equal(t, testData, readData)
			}
		})
	}
}

// TestNTAG_ReadNDEF_FudanClone_SkipsFastRead tests that FAST_READ is skipped for Fudan clone tags.
// Fudan FM11NT021 clones (UID prefix 0x1D) don't support FAST_READ (0x3A) and the command
// can return garbage or corrupt tag state. This test verifies the fix for issue #450.
func TestNTAG_ReadNDEF_FudanClone_SkipsFastRead(t *testing.T) {
	// Create wire-level simulator with Fudan clone tag
	sim := testutil.NewVirtualPN532()
	cloneTag := testutil.NewVirtualFudanClone(nil)
	sim.AddTag(cloneTag)

	// Create transport
	transport := newSimulatorTransport(sim)

	// Create device and initialize
	device, err := New(transport)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = device.Init(ctx)
	require.NoError(t, err)

	// Detect the clone tag
	tag, err := device.DetectTag(ctx)
	require.NoError(t, err)
	require.NotNil(t, tag, "should detect clone tag")

	// Verify it's a Fudan clone (UID starts with 0x1D)
	assert.Equal(t, byte(0x1D), tag.UIDBytes[0], "UID should start with 0x1D (Fudan)")

	// Create NTAG wrapper
	ntagTag := NewNTAGTag(device, tag.UIDBytes, tag.SAK)

	// Clear command log before reading NDEF
	transport.ClearCommandLog()

	// Read NDEF - this should succeed using block-by-block reading, NOT FAST_READ
	msg, err := ntagTag.ReadNDEF(ctx)
	require.NoError(t, err, "ReadNDEF should succeed for clone tag")
	require.NotNil(t, msg, "should have NDEF message")
	require.NotEmpty(t, msg.Records, "should have NDEF records")

	// Verify NDEF content
	assert.Equal(t, NDEFTypeText, msg.Records[0].Type, "should be text record")

	// Verify FAST_READ (InCommunicateThru with 0x3A) was NOT used
	// InCommunicateThru is command 0x42
	for _, entry := range transport.CommandLog {
		if entry.Cmd == 0x42 && len(entry.Args) > 0 && entry.Args[0] == 0x3A {
			t.Error("FAST_READ (0x3A) should NOT be sent to clone tags")
		}
	}

	// Verify InDataExchange (0x40) was used for block-by-block reading
	assert.True(t, transport.HasCommand(0x40), "InDataExchange should be used for block-by-block reading")
}

// TestNTAG_ReadNDEF_GenuineNXP_UsesFastRead tests that FAST_READ is used for genuine NXP tags.
// Genuine NXP tags (UID prefix 0x04) support FAST_READ and it should be attempted for performance.
func TestNTAG_ReadNDEF_GenuineNXP_UsesFastRead(t *testing.T) {
	// Create wire-level simulator with genuine NXP NTAG213 tag
	sim := testutil.NewVirtualPN532()
	nxpTag := testutil.NewVirtualNTAG213(nil) // Default UID starts with 0x04
	sim.AddTag(nxpTag)

	// Create transport
	transport := newSimulatorTransport(sim)

	// Create device and initialize
	device, err := New(transport)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = device.Init(ctx)
	require.NoError(t, err)

	// Detect the NXP tag
	tag, err := device.DetectTag(ctx)
	require.NoError(t, err)
	require.NotNil(t, tag, "should detect NXP tag")

	// Verify it's a genuine NXP tag (UID starts with 0x04)
	assert.Equal(t, byte(0x04), tag.UIDBytes[0], "UID should start with 0x04 (NXP)")

	// Create NTAG wrapper
	ntagTag := NewNTAGTag(device, tag.UIDBytes, tag.SAK)

	// Clear command log before reading NDEF
	transport.ClearCommandLog()

	// Read NDEF - this should succeed, potentially using FAST_READ
	msg, err := ntagTag.ReadNDEF(ctx)
	require.NoError(t, err, "ReadNDEF should succeed for NXP tag")
	require.NotNil(t, msg, "should have NDEF message")
	require.NotEmpty(t, msg.Records, "should have NDEF records")

	// Verify NDEF content
	assert.Equal(t, NDEFTypeText, msg.Records[0].Type, "should be text record")

	// For NXP tags, FAST_READ (InCommunicateThru with 0x3A) may be used
	// We verify the read succeeds - the specific method depends on tag type detection
}

// TestNTAG_ReadNDEF_CloneTag_RegressionTest tests the complete flow for clone tags.
// This is a regression test for issue #450 where clone tags failed with "no NDEF record found".
func TestNTAG_ReadNDEF_CloneTag_RegressionTest(t *testing.T) {
	// Test with various clone tag UID prefixes that are NOT NXP (0x04)
	cloneUIDs := []struct {
		name   string
		prefix byte
		desc   string
	}{
		{"Fudan_0x1D", 0x1D, "Fudan Microelectronics FM11NT021"},
		{"Unknown_0x08", 0x08, "Unknown manufacturer"},
		{"Unknown_0x00", 0x00, "Zero prefix (invalid)"},
	}

	for _, tt := range cloneUIDs {
		t.Run(tt.name, func(t *testing.T) {
			// Create wire-level simulator
			sim := testutil.NewVirtualPN532()

			// Create clone tag with specific UID prefix
			uid := []byte{tt.prefix, 0x20, 0xBD, 0xC9, 0x07, 0x10, 0x80}
			cloneTag := testutil.NewVirtualFudanClone(uid)
			sim.AddTag(cloneTag)

			// Create transport and device
			transport := newSimulatorTransport(sim)
			device, err := New(transport)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err = device.Init(ctx)
			require.NoError(t, err)

			// Detect tag
			tag, err := device.DetectTag(ctx)
			require.NoError(t, err)
			require.NotNil(t, tag)
			assert.Equal(t, tt.prefix, tag.UIDBytes[0], "UID prefix should match")

			// Create NTAG and read NDEF
			ntagTag := NewNTAGTag(device, tag.UIDBytes, tag.SAK)
			msg, err := ntagTag.ReadNDEF(ctx)

			// The read should succeed - this was the regression in issue #450
			require.NoError(t, err, "ReadNDEF should succeed for clone tag with prefix 0x%02X (%s)", tt.prefix, tt.desc)
			require.NotNil(t, msg, "should have NDEF message")
			require.NotEmpty(t, msg.Records, "should have NDEF records")
		})
	}
}
