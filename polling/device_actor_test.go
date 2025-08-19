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
	"sync/atomic"
	"testing"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
)

// TestDeviceActor_BasicCardDetection tests that the actor can detect cards
// This test demonstrates the core polling functionality needed in the actor model
func TestDeviceActor_BasicCardDetection(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)

	// cardDetected would track card detection events in the callback

	config := &Config{
		PollInterval:       10 * time.Millisecond,
		CardRemovalTimeout: 100 * time.Millisecond,
	}

	// DeviceActor now exists, create one to test
	actor := NewDeviceActor(device, config, DeviceCallbacks{})
	if actor == nil {
		t.Error("NewDeviceActor should return a non-nil actor")
	}

	// Basic creation works, now test that Start/Stop methods exist
	err := actor.Start(context.Background())
	if err != nil {
		t.Errorf("Start() should not return error for basic case, got: %v", err)
	}

	err = actor.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop() should not return error for basic case, got: %v", err)
	}
}

// TestDeviceActor_CardDetectionPolling tests that the actor actually polls for cards
// This test will fail until actual polling is implemented
func TestDeviceActor_CardDetectionPolling(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	var cardDetected atomic.Bool
	callbacks := DeviceCallbacks{
		OnCardDetected: func(_ *pn532.DetectedTag) error {
			cardDetected.Store(true)
			return nil
		},
	}

	config := &Config{
		PollInterval:       10 * time.Millisecond,
		CardRemovalTimeout: 100 * time.Millisecond,
	}

	// Setup mock to return a card on poll
	mockTransport.SetResponse(0x4A, []byte{
		0xD5, 0x4B, 0x01, 0x01, 0x00, 0x04, 0x00, 0x07, 0x04,
		0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
	})

	actor := NewDeviceActor(device, config, callbacks)
	if actor == nil {
		t.Fatal("NewDeviceActor should return a non-nil actor")
	}

	// Start the actor and let it poll for a short time
	err := actor.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer func() { _ = actor.Stop(context.Background()) }()

	// Wait a short time for polling to detect the mock card
	time.Sleep(50 * time.Millisecond)

	// Check if callback was called
	if !cardDetected.Load() {
		t.Error("Expected card detection callback to be called, but it wasn't")
	}
}

// TestDeviceActor_Metrics verifies that metrics are tracked during polling operations
func TestDeviceActor_Metrics(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	// Setup mock to return a card on poll
	mockTransport.SetResponse(0x4A, []byte{
		0xD5, 0x4B, 0x01, 0x01, 0x00, 0x04, 0x00, 0x07, 0x04,
		0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
	})

	callbacks := DeviceCallbacks{
		OnCardDetected: func(_ *pn532.DetectedTag) error {
			return nil
		},
	}

	config := &Config{
		PollInterval: 10 * time.Millisecond, // Fast polling for test
	}

	actor := NewDeviceActor(device, config, callbacks)

	// Check initial metrics
	metrics := actor.GetMetrics()
	if metrics.PollCycles != 0 {
		t.Errorf("Expected initial PollCycles to be 0, got %d", metrics.PollCycles)
	}
	if metrics.CardsDetected != 0 {
		t.Errorf("Expected initial CardsDetected to be 0, got %d", metrics.CardsDetected)
	}
	if metrics.PollErrors != 0 {
		t.Errorf("Expected initial PollErrors to be 0, got %d", metrics.PollErrors)
	}

	// Start the actor and let it poll
	err := actor.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer func() { _ = actor.Stop(context.Background()) }()

	// Wait for polling to complete
	time.Sleep(50 * time.Millisecond)

	// Verify metrics were updated
	finalMetrics := actor.GetMetrics()
	if finalMetrics.PollCycles <= 0 {
		t.Error("Poll cycles should be tracked and greater than 0")
	}
	if finalMetrics.CardsDetected <= 0 {
		t.Error("Cards detected should be tracked and greater than 0")
	}
	// Poll latency might be 0 for very fast mock operations, especially on Windows
	// We mainly care that it's being tracked (not negative)
	if finalMetrics.LastPollLatency < 0 {
		t.Error("Poll latency should be tracked and non-negative")
	}
}

// TestDeviceActor_PollingInterval verifies that polling interval is correctly maintained
func TestDeviceActor_PollingInterval(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	callbacks := DeviceCallbacks{
		OnCardDetected: func(_ *pn532.DetectedTag) error {
			return nil
		},
	}

	config := &Config{
		PollInterval: 10 * time.Millisecond, // Fast polling for test
	}

	actor := NewDeviceActor(device, config, callbacks)

	// Test 1: No card present - should slow down polling
	// Setup mock to return no cards
	mockTransport.SetResponse(0x4A, []byte{0xD5, 0x4B, 0x00}) // No cards response

	err := actor.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}
	defer func() { _ = actor.Stop(context.Background()) }()

	// Wait for polling to complete at least one cycle
	time.Sleep(20 * time.Millisecond)

	// Check that polling interval is maintained correctly
	currentInterval := actor.GetCurrentPollInterval()
	if currentInterval != config.PollInterval {
		t.Errorf("Expected polling interval to be %v, got %v", config.PollInterval, currentInterval)
	}
}

// TestDeviceActor_Stop verifies that Stop() properly terminates polling
func TestDeviceActor_Stop(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	// Setup mock response
	mockTransport.SetResponse(0x4A, []byte{0xD5, 0x4B, 0x00})

	callbacks := DeviceCallbacks{}
	config := &Config{
		PollInterval: 10 * time.Millisecond,
	}

	actor := NewDeviceActor(device, config, callbacks)

	// Start the actor
	err := actor.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Let it run briefly
	time.Sleep(20 * time.Millisecond)

	// Stop should complete without error
	err = actor.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop() should not return error, got: %v", err)
	}

	// Verify that polling actually stopped by checking metrics don't increase
	initialMetrics := actor.GetMetrics()
	time.Sleep(50 * time.Millisecond) // Wait longer than poll interval
	finalMetrics := actor.GetMetrics()

	if finalMetrics.PollCycles > initialMetrics.PollCycles {
		t.Error("Polling should have stopped, but poll cycles continued to increase")
	}
}
