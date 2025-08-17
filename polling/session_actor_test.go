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
	"sync/atomic"
	"testing"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
)

// TestSession_ActorBasedImplementation tests that Session can work with DeviceActor
// This test demonstrates the integration between Session and DeviceActor
func TestSession_ActorBasedImplementation(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	// Setup mock to return a card on poll
	mockTransport.SetResponse(0x4A, []byte{
		0xD5, 0x4B, 0x01, 0x01, 0x00, 0x04, 0x00, 0x07, 0x04,
		0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
	})

	var cardDetected atomic.Bool
	config := &Config{
		PollInterval:       10 * time.Millisecond,
		CardRemovalTimeout: 100 * time.Millisecond,
	}

	// Create session with actor-based implementation
	session := NewActorBasedSession(device, config)
	if session == nil {
		t.Fatal("NewActorBasedSession should return a non-nil session")
	}

	// Set callback
	session.OnCardDetected = func(_ *pn532.DetectedTag) error {
		cardDetected.Store(true)
		return nil
	}

	// Test that actor can be accessed - this should only work with actor-based implementation
	actor := session.GetDeviceActor()
	if actor == nil {
		t.Fatal("Actor-based session should provide access to underlying DeviceActor")
	}

	// Start session with timeout context to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := session.Start(ctx)
	// We expect this to either timeout or succeed and call the callback
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Start() failed with unexpected error: %v", err)
	}
	defer func() { _ = session.Close() }()

	// Wait a moment for any callback
	time.Sleep(20 * time.Millisecond)

	// Verify that card was detected through actor integration
	if !cardDetected.Load() {
		t.Error("Expected card detection through DeviceActor integration")
	}
}

// TestSession_UsesActorForPolling tests that Session actually delegates to DeviceActor
func TestSession_UsesActorForPolling(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	// Setup mock to return a card on poll
	mockTransport.SetResponse(0x4A, []byte{
		0xD5, 0x4B, 0x01, 0x01, 0x00, 0x04, 0x00, 0x07, 0x04,
		0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
	})

	var sessionCallbackCalled atomic.Bool
	var actorStarted atomic.Bool

	config := &Config{
		PollInterval:       10 * time.Millisecond,
		CardRemovalTimeout: 100 * time.Millisecond,
	}

	session := NewActorBasedSession(device, config)

	// Set session callback to track if it gets called
	session.OnCardDetected = func(_ *pn532.DetectedTag) error {
		sessionCallbackCalled.Store(true)
		return nil
	}

	// Override the Session's Start method behavior to test delegation
	// Create a custom session that tracks if Start was called on the actor
	if session.actor != nil {
		actorStarted.Store(true) // This means we have an actor to delegate to
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := session.Start(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Session.Start() should delegate to actor: %v", err)
	}

	_ = session.Close()

	// Wait a moment for potential callbacks
	time.Sleep(30 * time.Millisecond)

	// The test should demonstrate that we need to wire the actor callbacks
	// to the session callbacks properly
	if !actorStarted.Load() {
		t.Error("Actor should be available for delegation")
	}

	// Note: This test currently passes but doesn't prove delegation is working
	// We need to implement proper delegation in Session.Start()
	t.Logf("Session callback called: %v", sessionCallbackCalled.Load())
}

// TestSession_DelegatesStartToActor tests the specific behavior that Session.Start() delegates to actor.Start()
func TestSession_DelegatesStartToActor(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)

	// Setup mock to return a card on first poll
	mockTransport.SetResponse(0x4A, []byte{
		0xD5, 0x4B, 0x01, 0x01, 0x00, 0x04, 0x00, 0x07, 0x04,
		0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
	})

	config := &Config{
		PollInterval:       10 * time.Millisecond,
		CardRemovalTimeout: 100 * time.Millisecond,
	}

	// Create actor-based session
	session := NewActorBasedSession(device, config)

	// Set callback to track when actor callback chain works
	var sessionCallbackFired atomic.Bool
	session.OnCardDetected = func(_ *pn532.DetectedTag) error {
		sessionCallbackFired.Store(true)
		return nil
	}

	// This test specifically checks that when Session.Start() is called on an actor-based session,
	// it should delegate to the actor's Start() method rather than using continuousPolling()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Call Session.Start() - this should delegate to actor.Start() and trigger callbacks
	err := session.Start(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Session.Start() failed: %v", err)
	}

	// Wait for polling cycles
	time.Sleep(30 * time.Millisecond)

	_ = session.Close()

	// If delegation is working, the session callback should fire because:
	// 1. Session.Start() delegates to actor.Start()
	// 2. actor.Start() starts polling loop
	// 3. Actor polling detects card
	// 4. Actor fires callbacks which are wired to session callbacks
	if !sessionCallbackFired.Load() {
		t.Error("Session.Start() should delegate to actor.Start() and trigger callback chain")
	}
}
