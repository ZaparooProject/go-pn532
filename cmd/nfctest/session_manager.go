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

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/ZaparooProject/go-pn532/polling"
)

// SessionManager handles card monitoring and polling sessions
type SessionManager struct {
	config    *Config
	output    *Output
	discovery *Discovery
	testing   *Testing
}

// NewSessionManager creates a new session manager
func NewSessionManager(config *Config, output *Output, discovery *Discovery, testing *Testing) *SessionManager {
	return &SessionManager{
		config:    config,
		output:    output,
		discovery: discovery,
		testing:   testing,
	}
}

// MonitorCards continuously monitors for cards using proper InAutoPoll continuous polling
func (sm *SessionManager) MonitorCards(ctx context.Context, readers []detection.DeviceInfo) error {
	_, _ = fmt.Println("\nMonitoring for cards... (Ctrl+C to quit)")

	setup, err := sm.initializeDevices(readers)
	if err != nil {
		return err
	}

	// Clean up all devices on exit
	defer func() {
		for _, session := range setup.Sessions {
			_ = session.Close()
		}
	}()

	return sm.startSessionLoop(ctx, setup)
}

func (sm *SessionManager) initializeDevices(readers []detection.DeviceInfo) (*SessionSetup, error) {
	setup := &SessionSetup{
		Sessions:    make([]*polling.Session, 0, len(readers)),
		ReaderPaths: make([]string, 0, len(readers)),
	}

	for _, reader := range readers {
		device, err := sm.createDevice(reader)
		if err != nil {
			sm.output.Warning("Failed to create device for %s: %v", reader.Path, err)
			continue
		}

		// Create polling config from main config
		pollingConfig := &polling.Config{
			PollInterval:       sm.config.PollInterval,
			CardRemovalTimeout: sm.config.CardRemovalTimeout,
		}

		session := polling.NewSession(device, pollingConfig)

		// Set up event callbacks (capture variables to avoid closure issues)
		readerPath := reader.Path

		// Common testing function to avoid code duplication
		testCard := func(session *polling.Session, tag *pn532.DetectedTag) {
			writeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := session.WriteToTag(writeCtx, tag, func(cardTag pn532.Tag) error {
				return sm.testing.TestCardWithTag(cardTag)
			}); err != nil {
				sm.output.Error("Card test failed: %v", err)
			} else {
				sm.output.OK("Card test completed")
			}
		}

		session.OnCardDetected = func(tag *pn532.DetectedTag) error {
			sm.output.NewCardDetected(readerPath, string(tag.Type), tag.UID)
			testCard(session, tag)
			return nil
		}
		session.OnCardRemoved = func() {
			sm.output.Info("Card removed from %s", readerPath)
		}
		session.OnCardChanged = func(tag *pn532.DetectedTag) error {
			sm.output.DifferentCardDetected(readerPath, string(tag.Type), tag.UID)
			testCard(session, tag)
			return nil
		}

		setup.Sessions = append(setup.Sessions, session)
		setup.ReaderPaths = append(setup.ReaderPaths, reader.Path)
	}

	if len(setup.Sessions) == 0 {
		return nil, errors.New("no functional readers available for session management")
	}

	return setup, nil
}

func (sm *SessionManager) createDevice(reader detection.DeviceInfo) (*pn532.Device, error) {
	transport, err := sm.discovery.CreateTransport(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	device, err := pn532.New(transport, pn532.WithTimeout(5*time.Second))
	if err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("failed to create PN532 device: %w", err)
	}

	// Initialize device (SAM configuration, etc.)
	if err := device.Init(); err != nil {
		_ = device.Close()
		_ = transport.Close()
		return nil, fmt.Errorf("failed to initialize device: %w", err)
	}

	// No polling configuration needed - simplified approach uses direct timeout-based detection
	return device, nil
}

func (*SessionManager) startSessionLoop(ctx context.Context, setup *SessionSetup) error {
	// Create a cancellable context for all sessions
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start continuous polling for each reader in separate goroutines
	errChan := make(chan error, len(setup.Sessions))
	for i, session := range setup.Sessions {
		go func(_ int, sess *polling.Session) {
			err := sess.Start(sessionCtx)
			errChan <- err
		}(i, session)
	}

	// Wait for context cancellation or collect all session results
	var firstError error
	completedSessions := 0

	for completedSessions < len(setup.Sessions) {
		select {
		case <-ctx.Done():
			// Cancel all sessions and wait for them to complete
			cancel()
			// Drain remaining error messages to prevent goroutine leaks
			for completedSessions < len(setup.Sessions) {
				<-errChan
				completedSessions++
			}
			return ctx.Err()
		case err := <-errChan:
			completedSessions++
			if err != nil && !errors.Is(err, context.Canceled) && firstError == nil {
				firstError = err
				cancel() // Cancel other sessions on first error
			}
		}
	}

	return firstError
}
