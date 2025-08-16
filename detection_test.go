package pn532

import (
	"context"
	"errors"
	"testing"
	"time"

	testutil "github.com/ZaparooProject/go-pn532/internal/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDevice_DetectTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setupMock   func(*MockTransport)
		expectTag   bool
		expectError bool
	}{
		{
			name: "Successful_Tag_Detection",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
				mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
				mock.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildTagDetectionResponse("NTAG213", testutil.TestNTAG213UID))
			},
			expectTag: true,
		},
		{
			name: "No_Tag_Found",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
				mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
				mock.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildNoTagResponse())
			},
			expectTag:   false,
			expectError: true, // Should return ErrNoTagDetected
		},
		{
			name: "Detection_Error",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
				mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
				mock.SetError(testutil.CmdInListPassiveTarget, errors.New("detection failed"))
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup mock transport
			mock := NewMockTransport()
			tt.setupMock(mock)

			// Create and initialize device
			device, err := New(mock)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			err = device.InitContext(ctx)
			require.NoError(t, err)

			// Test tag detection
			tag, err := device.DetectTag()

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, tag)
			} else if tt.expectTag {
				assert.NoError(t, err)
				assert.NotNil(t, tag)
				assert.NotEmpty(t, tag.UID)
			} else {
				assert.NoError(t, err)
				assert.Nil(t, tag)
			}
		})
	}
}

func TestDevice_WaitForTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setupMock   func(*MockTransport)
		timeout     time.Duration
		expectError bool
		expectTag   bool
	}{
		{
			name: "Tag_Found_Quickly",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
				mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
				mock.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildTagDetectionResponse("NTAG213", testutil.TestNTAG213UID))
			},
			timeout:     time.Second,
			expectError: false,
			expectTag:   true,
		},
		{
			name: "Timeout_No_Tag",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
				mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
				// The mock will return the same "no tag" response for multiple calls
				mock.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildNoTagResponse())
			},
			timeout:     300 * time.Millisecond, // Give enough time for multiple polling cycles
			expectError: true,
			expectTag:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup mock transport
			mock := NewMockTransport()
			tt.setupMock(mock)

			// Create and initialize device
			device, err := New(mock)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			err = device.InitContext(ctx)
			require.NoError(t, err)

			// Test waiting for tag with timeout context
			waitCtx, waitCancel := context.WithTimeout(context.Background(), tt.timeout)
			defer waitCancel()

			start := time.Now()
			tag, err := device.WaitForTag(waitCtx)
			elapsed := time.Since(start)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, tag)
				// For timeout test, verify we get context deadline exceeded and that it actually waited
				if tt.name == "Timeout_No_Tag" {
					assert.True(t, errors.Is(err, context.DeadlineExceeded), "Expected context deadline exceeded error, got: %v", err)
					// Verify it actually waited close to the timeout duration
					assert.GreaterOrEqual(t, elapsed, tt.timeout-50*time.Millisecond, "Should have waited close to timeout duration")
					// Verify polling happened multiple times
					callCount := mock.GetCallCount(testutil.CmdInListPassiveTarget)
					assert.Greater(t, callCount, 1, "Should have made multiple polling attempts")
				}
			} else if tt.expectTag {
				assert.NoError(t, err)
				assert.NotNil(t, tag)
				assert.NotEmpty(t, tag.UID)
			} else {
				assert.NoError(t, err)
				assert.Nil(t, tag)
			}
		})
	}
}

func TestDevice_SimplePoll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupMock     func(*MockTransport)
		pollingPeriod time.Duration
		timeout       time.Duration
		expectTag     bool
		expectError   bool
	}{
		{
			name: "Successful_Polling",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
				mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
				mock.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildTagDetectionResponse("NTAG213", testutil.TestNTAG213UID))
			},
			pollingPeriod: 50 * time.Millisecond,
			timeout:       time.Second,
			expectTag:     true,
		},
		{
			name: "Polling_Timeout",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
				mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
				mock.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildNoTagResponse())
			},
			pollingPeriod: 20 * time.Millisecond,
			timeout:       100 * time.Millisecond,
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup mock transport
			mock := NewMockTransport()
			tt.setupMock(mock)

			// Create and initialize device
			device, err := New(mock)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			err = device.InitContext(ctx)
			require.NoError(t, err)

			// Test simple polling with timeout context
			pollCtx, pollCancel := context.WithTimeout(context.Background(), tt.timeout)
			defer pollCancel()

			tag, err := device.SimplePoll(pollCtx, tt.pollingPeriod)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, tag)
			} else if tt.expectTag {
				assert.NoError(t, err)
				assert.NotNil(t, tag)
				assert.NotEmpty(t, tag.UID)
			}

			// Verify polling was attempted multiple times for timeout cases
			if tt.expectError {
				// Should have made multiple attempts during the polling period
				callCount := mock.GetCallCount(testutil.CmdInListPassiveTarget)
				assert.Greater(t, callCount, 1, "Should have made multiple polling attempts")
			}
		})
	}
}

func TestDevice_DetectTags_WithFilters(t *testing.T) {
	t.Parallel()

	// Setup mock with multiple tag types
	mock := NewMockTransport()
	mock.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
	mock.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
	mock.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildTagDetectionResponse("NTAG213", testutil.TestNTAG213UID))

	device, err := New(mock)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = device.InitContext(ctx)
	require.NoError(t, err)

	// Test detection with basic parameters (maxTags=1, baudRate=0)
	tags, err := device.DetectTags(1, 0)
	assert.NoError(t, err)
	assert.Len(t, tags, 1)

	// Test detection with multiple targets
	tags, err = device.DetectTags(2, 0)
	assert.NoError(t, err)
	// Should still return just 1 tag since mock only provides 1
	assert.LessOrEqual(t, len(tags), 1)
}

func TestFilterDetectedTags(t *testing.T) {
	t.Parallel()

	// Create test tags
	testTags := []*DetectedTag{
		{
			Type:     TagTypeNTAG,
			UID:      "04abcdef123456",
			UIDBytes: testutil.TestNTAG213UID,
		},
		{
			Type:     TagTypeMIFARE,
			UID:      "12345678",
			UIDBytes: testutil.TestMIFARE1KUID,
		},
	}

	tests := []struct {
		name        string
		tags        []*DetectedTag
		tagType     TagType
		uidFilter   []byte
		expectedLen int
	}{
		{
			name:        "No_Filter",
			tags:        testTags,
			tagType:     TagTypeAny,
			uidFilter:   nil,
			expectedLen: 2,
		},
		{
			name:        "NTAG_Filter",
			tags:        testTags,
			tagType:     TagTypeNTAG,
			uidFilter:   nil,
			expectedLen: 1,
		},
		{
			name:        "MIFARE_Filter",
			tags:        testTags,
			tagType:     TagTypeMIFARE,
			uidFilter:   nil,
			expectedLen: 1,
		},
		{
			name:        "UID_Bytes_Filter",
			tags:        testTags,
			tagType:     TagTypeAny,
			uidFilter:   testutil.TestNTAG213UID,
			expectedLen: 1,
		},
		{
			name:        "No_Match_Filter",
			tags:        testTags,
			tagType:     TagTypeFeliCa,
			uidFilter:   nil,
			expectedLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			filtered := filterDetectedTags(tt.tags, tt.tagType, tt.uidFilter)
			assert.Len(t, filtered, tt.expectedLen)

			// Verify filtering logic
			for _, tag := range filtered {
				if tt.tagType != TagTypeAny {
					assert.Equal(t, tt.tagType, tag.Type)
				}
				if tt.uidFilter != nil {
					assert.Equal(t, tt.uidFilter, tag.UIDBytes)
				}
			}
		})
	}
}

func TestDevice_InRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setupMock   func(*MockTransport)
		targetID    byte
		expectError bool
	}{
		{
			name: "Successful_Release",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdInRelease, []byte{0x53, 0x00}) // Correct format: cmd response + success status
			},
			targetID:    1,
			expectError: false,
		},
		{
			name: "Release_Error",
			setupMock: func(mock *MockTransport) {
				mock.SetError(testutil.CmdInRelease, errors.New("release failed"))
			},
			targetID:    1,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup mock transport
			mock := NewMockTransport()
			tt.setupMock(mock)

			// Create device
			device, err := New(mock)
			require.NoError(t, err)

			// Test InRelease
			err = device.InRelease(tt.targetID)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, 1, mock.GetCallCount(testutil.CmdInRelease))
			}
		})
	}
}

func TestDevice_InSelect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setupMock   func(*MockTransport)
		targetID    byte
		expectError bool
	}{
		{
			name: "Successful_Select",
			setupMock: func(mock *MockTransport) {
				mock.SetResponse(testutil.CmdInSelect, []byte{0x55, 0x00}) // Correct format: cmd response + success status
			},
			targetID:    1,
			expectError: false,
		},
		{
			name: "Select_Error",
			setupMock: func(mock *MockTransport) {
				mock.SetError(testutil.CmdInSelect, errors.New("select failed"))
			},
			targetID:    1,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Setup mock transport
			mock := NewMockTransport()
			tt.setupMock(mock)

			// Create device
			device, err := New(mock)
			require.NoError(t, err)

			// Test InSelect
			err = device.InSelect(tt.targetID)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, 1, mock.GetCallCount(testutil.CmdInSelect))
			}
		})
	}
}
