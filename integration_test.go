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

// TestBasicTagDetection tests the complete workflow of detecting a tag
func TestBasicTagDetection(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

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
			
			err = device.InitContext(ctx)
			require.NoError(t, err)

			// Test tag detection
			tags, err := device.DetectTagsContext(ctx, 1, 0)
			require.NoError(t, err)
			require.Len(t, tags, 1)

			// Verify tag properties
			tag := tags[0]
			assert.Equal(t, tt.wantUID, tag.UID)
			assert.Equal(t, tt.uid, tag.UIDBytes)

			// Verify mock was called correctly
			// InitContext calls firmware version twice: once for validation, once for setup
			assert.Equal(t, 2, mock.GetCallCount(testutil.CmdGetFirmwareVersion))
			assert.Equal(t, 1, mock.GetCallCount(testutil.CmdSAMConfiguration))
			assert.Equal(t, 1, mock.GetCallCount(testutil.CmdInListPassiveTarget))
		})
	}
}

// TestTagNotFound tests the scenario when no tag is present
func TestTagNotFound(t *testing.T) {
	t.Parallel()

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
	
	err = device.InitContext(ctx)
	require.NoError(t, err)

	// Test tag detection - should find no tags
	tags, err := device.DetectTagsContext(ctx, 1, 0)
	require.NoError(t, err)
	assert.Len(t, tags, 0)
}

// TestTagReadWrite tests reading from and writing to a virtual tag
func TestTagReadWrite(t *testing.T) {
	t.Parallel()

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

	// Test block-level operations
	block4, err := virtualTag.ReadBlock(4) // First user data block
	require.NoError(t, err)
	assert.Len(t, block4, 16) // Standard NFC block size

	// Test invalid block access
	_, err = virtualTag.ReadBlock(100) // Beyond NTAG213 range
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

// TestTransportErrorHandling tests error scenarios
func TestTransportErrorHandling(t *testing.T) {
	t.Parallel()

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
	
	err = device.InitContext(ctx)
	require.NoError(t, err)

	// Test tag detection with error
	_, err = device.DetectTagsContext(ctx, 1, 0)
	assert.Error(t, err)

	// Verify error was injected
	assert.Equal(t, 1, mock.GetCallCount(testutil.CmdInListPassiveTarget))
}

// TestTransportTimeout tests timeout scenarios
func TestTransportTimeout(t *testing.T) {
	t.Parallel()

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
	
	err = device.InitContext(initCtx)
	require.NoError(t, err)

	// Test with sufficient timeout - should succeed even with delay
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	tags, err := device.DetectTagsContext(ctx, 1, 0)
	require.NoError(t, err)
	assert.Len(t, tags, 1)
	
	// Note: Testing actual context timeout requires the transport layer to be context-aware,
	// which would be a significant architectural change. For now, we verify that operations
	// complete successfully within reasonable timeouts despite mock delays.
}

// TestTagRemoval tests tag removal scenarios
func TestTagRemoval(t *testing.T) {
	t.Parallel()

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
	err = device.InitContext(ctx)
	require.NoError(b, err)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tags, err := device.DetectTagsContext(ctx, 1, 0)
		require.NoError(b, err)
		require.Len(b, tags, 1)
	}
}

// TestMIFAREVirtualTagReadWrite tests the MIFARE virtual tag implementations
func TestMIFAREVirtualTagReadWrite(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			// Create virtual tag
			virtualTag := tt.createTag()
			require.NotNil(t, virtualTag)
			require.True(t, virtualTag.Present)

			// Test reading from multiple blocks
			for _, blockNum := range tt.testBlocks {
				t.Run(fmt.Sprintf("Block_%d", blockNum), func(t *testing.T) {
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

				_, err = virtualTag.ReadBlock(tt.testBlocks[0])
				assert.NoError(t, err)
			})
		})
	}
}

// TestMIFAREWriteProtection tests write protection on sector trailers
func TestMIFAREWriteProtection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		createTag         func() *testutil.VirtualTag
		protectedBlocks   []int
		unprotectedBlocks []int
	}{
		{
			name:              "MIFARE1K_WriteProtection",
			createTag:         func() *testutil.VirtualTag { return testutil.NewVirtualMIFARE1K(nil) },
			protectedBlocks:   []int{3, 7, 11, 15}, // Sector trailers
			unprotectedBlocks: []int{1, 2, 4, 5},   // Regular blocks
		},
		{
			name:              "MIFARE4K_WriteProtection",
			createTag:         func() *testutil.VirtualTag { return testutil.NewVirtualMIFARE4K(nil) },
			protectedBlocks:   []int{3, 7, 11, 15, 143, 159}, // Include 4K sector trailers
			unprotectedBlocks: []int{1, 2, 4, 5, 8, 9},       // Regular blocks
		},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			virtualTag := tt.createTag()
			require.NotNil(t, virtualTag)

			testData := make([]byte, 16)
			for i := range testData {
				testData[i] = 0xAA // Test pattern
			}

			// Test protected blocks should fail
			for _, block := range tt.protectedBlocks {
				err := virtualTag.WriteBlock(block, testData)
				assert.Error(t, err, "Block %d should be write protected", block)
				assert.Contains(t, err.Error(), "write protected")
			}

			// Test unprotected blocks should succeed
			for _, block := range tt.unprotectedBlocks {
				err := virtualTag.WriteBlock(block, testData)
				assert.NoError(t, err, "Block %d should be writable", block)

				// Verify data was written
				readData, err := virtualTag.ReadBlock(block)
				assert.NoError(t, err)
				assert.Equal(t, testData, readData)
			}
		})
	}
}
