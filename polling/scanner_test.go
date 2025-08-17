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

package polling

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
	testutil "github.com/ZaparooProject/go-pn532/internal/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewScanner(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)

	t.Run("with valid parameters", func(t *testing.T) {
		t.Parallel()
		config := DefaultScanConfig()
		scanner, err := NewScanner(device, config)
		require.NoError(t, err)

		assert.NotNil(t, scanner)
		assert.Equal(t, device, scanner.device)
		assert.Equal(t, config, scanner.config)
		assert.False(t, scanner.IsRunning())
		assert.False(t, scanner.HasPendingWrite())
	})

	t.Run("with nil config uses defaults", func(t *testing.T) {
		t.Parallel()
		scanner, err := NewScanner(device, nil)
		require.NoError(t, err)

		assert.NotNil(t, scanner)
		assert.NotNil(t, scanner.config)
		assert.Equal(t, 250*time.Millisecond, scanner.config.PollInterval)
	})

	t.Run("with nil device returns error", func(t *testing.T) {
		t.Parallel()
		scanner, err := NewScanner(nil, DefaultScanConfig())
		require.Error(t, err)
		assert.Nil(t, scanner)
		assert.Contains(t, err.Error(), "device cannot be nil")
	})
}

func TestDefaultScanConfig(t *testing.T) {
	t.Parallel()
	config := DefaultScanConfig()

	assert.NotNil(t, config)
	assert.Equal(t, 250*time.Millisecond, config.PollInterval)
	assert.Equal(t, 2*time.Second, config.CardRemovalTimeout)
	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, 100*time.Millisecond, config.RetryBackoff)
}

func TestScanner_StartSetsRunningState(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	mockTransport.SetResponse(0x4A, []byte{0x4B, 0x00}) // InListPassiveTarget response (no tags)

	scanner, err := NewScanner(device, &ScanConfig{
		PollInterval:       10 * time.Millisecond, // Fast for testing
		CardRemovalTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = scanner.Start(ctx)
	require.NoError(t, err)
	defer func() {
		if stopErr := scanner.Stop(); stopErr != nil {
			t.Errorf("Failed to stop scanner: %v", stopErr)
		}
	}()

	// Wait a bit for goroutine to start
	time.Sleep(50 * time.Millisecond)
	assert.True(t, scanner.IsRunning())
}

func TestScanner_StopClearsRunningState(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	mockTransport.SetResponse(0x4A, []byte{0x4B, 0x00}) // InListPassiveTarget response (no tags)

	scanner, err := NewScanner(device, &ScanConfig{
		PollInterval:       10 * time.Millisecond, // Fast for testing
		CardRemovalTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = scanner.Start(ctx)
	require.NoError(t, err)

	// Wait for startup
	time.Sleep(50 * time.Millisecond)
	assert.True(t, scanner.IsRunning())

	err = scanner.Stop()
	require.NoError(t, err)
	assert.False(t, scanner.IsRunning())
}

func TestScanner_DoubleStartReturnsError(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	mockTransport.SetResponse(0x4A, []byte{0x4B, 0x00}) // InListPassiveTarget response (no tags)

	scanner, err := NewScanner(device, &ScanConfig{
		PollInterval:       10 * time.Millisecond, // Fast for testing
		CardRemovalTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = scanner.Start(ctx)
	require.NoError(t, err)
	defer func() {
		if stopErr := scanner.Stop(); stopErr != nil {
			t.Errorf("Failed to stop scanner: %v", stopErr)
		}
	}()

	// Second start should fail
	err = scanner.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestScanner_StopWhenNotRunningSafe(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	mockTransport.SetResponse(0x4A, []byte{0x4B, 0x00}) // InListPassiveTarget response (no tags)

	scanner, err := NewScanner(device, &ScanConfig{
		PollInterval:       10 * time.Millisecond, // Fast for testing
		CardRemovalTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	err = scanner.Stop()
	assert.NoError(t, err)
}

func TestScanner_WriteToNextTag_Success(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	// Mock responses for initialization
	mockTransport.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
	mockTransport.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())

	// Mock tag detection - will be called multiple times (polling)
	testUID := []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}
	mockTransport.SetResponse(testutil.CmdInListPassiveTarget,
		testutil.BuildTagDetectionResponse("NTAG213", testUID))

	// Mock InSelect and read/write operations for the tag
	mockTransport.SetResponse(testutil.CmdInSelect, []byte{0x55, 0x00}) // InSelect success
	mockTransport.SetResponse(testutil.CmdInDataExchange, testutil.BuildDataExchangeResponse([]byte{}))

	// Initialize device
	initCtx, initCancel := context.WithTimeout(context.Background(), time.Second)
	defer initCancel()
	err := device.InitContext(initCtx)
	require.NoError(t, err)

	scanner, err := NewScanner(device, &ScanConfig{
		PollInterval:       50 * time.Millisecond, // Fast for testing
		CardRemovalTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = scanner.Start(ctx)
	require.NoError(t, err)
	defer func() {
		if stopErr := scanner.Stop(); stopErr != nil {
			t.Errorf("Failed to stop scanner: %v", stopErr)
		}
	}()

	// Set up write operation that should succeed
	writeComplete := make(chan error, 1)
	writeOperation := func(_ pn532.Tag) error {
		// Simulate successful write operation
		return nil
	}

	// Start write in goroutine
	go func() {
		err := scanner.WriteToNextTag(ctx, 2*time.Second, writeOperation)
		writeComplete <- err
	}()

	// Wait for write to complete
	select {
	case err := <-writeComplete:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Write operation timed out")
	}

	// Verify no pending write remains
	assert.False(t, scanner.HasPendingWrite())
}

func TestScanner_WriteToNextTag_Timeout(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	// Mock responses for initialization
	mockTransport.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
	mockTransport.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())

	// Mock response to always return no tags detected
	mockTransport.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildNoTagResponse())

	scanner, err := NewScanner(device, &ScanConfig{
		PollInterval:       50 * time.Millisecond, // Fast for testing
		CardRemovalTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = scanner.Start(ctx)
	require.NoError(t, err)
	defer func() {
		if stopErr := scanner.Stop(); stopErr != nil {
			t.Errorf("Failed to stop scanner: %v", stopErr)
		}
	}()

	// Set up write operation with short timeout
	writeOperation := func(_ pn532.Tag) error {
		return nil
	}

	// Start write with very short timeout
	start := time.Now()
	err = scanner.WriteToNextTag(ctx, 200*time.Millisecond, writeOperation)
	duration := time.Since(start)

	// Should timeout since no tags are detected
	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.GreaterOrEqual(t, duration, 200*time.Millisecond, "Should wait at least timeout duration")
	assert.Less(t, duration, 400*time.Millisecond, "Should not wait much longer than timeout")

	// Verify no pending write remains
	assert.False(t, scanner.HasPendingWrite())
}

func TestScanner_WriteToNextTag_AlreadyPending(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	// Mock responses for initialization
	mockTransport.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
	mockTransport.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())

	// Mock response to never return tags so writes timeout
	mockTransport.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildNoTagResponse())

	scanner, err := NewScanner(device, &ScanConfig{
		PollInterval:       50 * time.Millisecond, // Fast for testing
		CardRemovalTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = scanner.Start(ctx)
	require.NoError(t, err)
	defer func() {
		if stopErr := scanner.Stop(); stopErr != nil {
			t.Errorf("Failed to stop scanner: %v", stopErr)
		}
	}()

	// This test verifies the mutex serialization behavior:
	// Multiple concurrent write attempts are properly serialized
	const numWrites = 3
	results := make(chan error, numWrites)

	// Start multiple write operations
	for i := 0; i < numWrites; i++ {
		go func(_ int) {
			err := scanner.WriteToNextTag(ctx, 300*time.Millisecond, func(_ pn532.Tag) error {
				return nil
			})
			results <- err
		}(i)
	}

	// All writes should timeout (no tags available) but be properly serialized
	timeoutCount := 0
	for i := 0; i < numWrites; i++ {
		select {
		case err := <-results:
			if errors.Is(err, context.DeadlineExceeded) {
				timeoutCount++
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Write operation didn't complete")
		}
	}

	// All writes should have timed out due to no tags
	assert.Equal(t, numWrites, timeoutCount, "All writes should timeout when no tags are available")

	// No pending writes should remain
	assert.False(t, scanner.HasPendingWrite())
}

func TestScanner_WriteToNextTag_Cancellation(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	// Mock responses for initialization
	mockTransport.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
	mockTransport.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())

	// Mock response to never return tags so write stays pending
	mockTransport.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildNoTagResponse())

	scanner, err := NewScanner(device, &ScanConfig{
		PollInterval:       50 * time.Millisecond, // Fast for testing
		CardRemovalTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	scannerCtx, scannerCancel := context.WithCancel(context.Background())
	defer scannerCancel()

	err = scanner.Start(scannerCtx)
	require.NoError(t, err)
	defer func() {
		if stopErr := scanner.Stop(); stopErr != nil {
			t.Errorf("Failed to stop scanner: %v", stopErr)
		}
	}()

	// Create cancellable context for write operation
	writeCtx, writeCancel := context.WithCancel(context.Background())

	// Start write operation
	writeComplete := make(chan error, 1)
	go func() {
		err := scanner.WriteToNextTag(writeCtx, 5*time.Second, func(_ pn532.Tag) error {
			return nil
		})
		writeComplete <- err
	}()

	// Wait for write to be queued
	time.Sleep(100 * time.Millisecond)
	assert.True(t, scanner.HasPendingWrite())

	// Cancel the write context
	start := time.Now()
	writeCancel()

	// Write should complete quickly with cancellation error
	select {
	case err := <-writeComplete:
		duration := time.Since(start)
		require.Error(t, err)
		require.ErrorIs(t, err, context.Canceled)
		assert.Less(t, duration, 1*time.Second, "Should cancel quickly")
	case <-time.After(2 * time.Second):
		t.Fatal("Write operation didn't complete after cancellation")
	}

	// Verify no pending write remains
	assert.False(t, scanner.HasPendingWrite())
}

func TestScanner_WriteAfterStop(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	// Mock responses for initialization
	mockTransport.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
	mockTransport.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())
	mockTransport.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildNoTagResponse())

	scanner, err := NewScanner(device, &ScanConfig{
		PollInterval:       50 * time.Millisecond, // Fast for testing
		CardRemovalTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start and then stop the scanner
	err = scanner.Start(ctx)
	require.NoError(t, err)

	// Wait for scanner to be running
	time.Sleep(100 * time.Millisecond)
	assert.True(t, scanner.IsRunning())

	// Stop the scanner
	err = scanner.Stop()
	require.NoError(t, err)
	assert.False(t, scanner.IsRunning())

	// Try to write after stopping - should fail immediately
	err = scanner.WriteToNextTag(ctx, 1*time.Second, func(_ pn532.Tag) error {
		return nil
	})

	require.Error(t, err)
	assert.Equal(t, ErrScannerNotRunning, err)
	assert.False(t, scanner.HasPendingWrite())
}

func TestScanner_ConcurrentWriteAttempts(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	// Mock responses for initialization
	mockTransport.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
	mockTransport.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())

	// Mock response to never return tags so writes stay pending
	mockTransport.SetResponse(testutil.CmdInListPassiveTarget, testutil.BuildNoTagResponse())

	scanner, err := NewScanner(device, &ScanConfig{
		PollInterval:       50 * time.Millisecond, // Fast for testing
		CardRemovalTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = scanner.Start(ctx)
	require.NoError(t, err)
	defer func() {
		if stopErr := scanner.Stop(); stopErr != nil {
			t.Errorf("Failed to stop scanner: %v", stopErr)
		}
	}()

	const numGoroutines = 5
	results := make(chan error, numGoroutines)

	// Launch multiple concurrent write attempts
	for i := 0; i < numGoroutines; i++ {
		go func(_ int) {
			err := scanner.WriteToNextTag(ctx, 1*time.Second, func(_ pn532.Tag) error {
				return nil
			})
			results <- err
		}(i)
	}

	// Collect results
	var successCount, timeoutCount, otherErrorCount int
	for i := 0; i < numGoroutines; i++ {
		select {
		case err := <-results:
			switch {
			case err == nil:
				successCount++
			case errors.Is(err, context.DeadlineExceeded):
				timeoutCount++
			default:
				otherErrorCount++
			}
		case <-time.After(6 * time.Second):
			t.Fatal("Concurrent write attempts timed out")
		}
	}

	// Since no tags are detected, all should timeout due to mutex serialization
	assert.Equal(t, 0, successCount, "No writes should succeed without tags")
	assert.Equal(t, numGoroutines, timeoutCount, "All writes should timeout when no tags are available")
	assert.Equal(t, 0, otherErrorCount, "No other errors should occur")

	// Final state should have no pending writes (all should have timed out)
	time.Sleep(100 * time.Millisecond) // Allow cleanup
	assert.False(t, scanner.HasPendingWrite())
}

func TestScanner_EventCallbacks(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	// Mock responses for initialization
	mockTransport.SetResponse(testutil.CmdGetFirmwareVersion, testutil.BuildFirmwareVersionResponse())
	mockTransport.SetResponse(testutil.CmdSAMConfiguration, testutil.BuildSAMConfigurationResponse())

	// Mock tag detection response
	testUID := []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}
	mockTransport.SetResponse(testutil.CmdInListPassiveTarget,
		testutil.BuildTagDetectionResponse("NTAG213", testUID))

	// Initialize device
	initCtx, initCancel := context.WithTimeout(context.Background(), time.Second)
	defer initCancel()
	err := device.InitContext(initCtx)
	require.NoError(t, err)

	scanner, err := NewScanner(device, &ScanConfig{
		PollInterval:       50 * time.Millisecond, // Fast for testing
		CardRemovalTimeout: 100 * time.Millisecond,
	})
	require.NoError(t, err)

	// Set up callback tracking with atomic operations to prevent races
	var detectedCalls, removedCalls, changedCalls int64
	var detectedTags []*pn532.DetectedTag
	var tagsMutex sync.Mutex

	scanner.OnTagDetected = func(tag *pn532.DetectedTag) error {
		atomic.AddInt64(&detectedCalls, 1)
		tagsMutex.Lock()
		detectedTags = append(detectedTags, tag)
		tagsMutex.Unlock()
		return nil
	}

	scanner.OnTagRemoved = func() {
		atomic.AddInt64(&removedCalls, 1)
	}

	scanner.OnTagChanged = func(_ *pn532.DetectedTag) error {
		atomic.AddInt64(&changedCalls, 1)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = scanner.Start(ctx)
	require.NoError(t, err)

	// Wait for scanning to detect a tag
	time.Sleep(200 * time.Millisecond)

	err = scanner.Stop()
	require.NoError(t, err)

	// For a simple case, we should get at least one detection
	assert.GreaterOrEqual(t, atomic.LoadInt64(&detectedCalls), int64(1), "OnTagDetected should be called at least once")
	assert.Equal(t, int64(0), atomic.LoadInt64(&changedCalls),
		"OnTagChanged should not be called in this simple sequence")

	// If any tags were detected, verify the details
	tagsMutex.Lock()
	tagsLen := len(detectedTags)
	var firstTag *pn532.DetectedTag
	if tagsLen > 0 {
		firstTag = detectedTags[0]
	}
	tagsMutex.Unlock()

	if tagsLen > 0 {
		assert.Contains(t, strings.ToUpper(firstTag.UID), "04123456789ABC")
		assert.Equal(t, testUID, firstTag.UIDBytes)
	}
}
