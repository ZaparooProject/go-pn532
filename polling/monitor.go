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
	"fmt"
	"time"

	"github.com/ZaparooProject/go-pn532"
)

// Monitor handles continuous card monitoring with state machine
type Monitor struct {
	device         *pn532.Device
	config         *Config
	OnCardDetected func(tag *pn532.DetectedTag) error
	OnCardRemoved  func()
	OnCardChanged  func(tag *pn532.DetectedTag) error
	state          CardState
}

// NewMonitor creates a new card monitor
func NewMonitor(device *pn532.Device, config *Config) *Monitor {
	if config == nil {
		config = DefaultConfig()
	}
	return &Monitor{
		device: device,
		config: config,
		state:  CardState{},
	}
}

// Start begins continuous monitoring for cards
func (m *Monitor) Start(ctx context.Context) error {
	return m.continuousPolling(ctx)
}

// GetState returns the current card state
func (m *Monitor) GetState() CardState {
	return m.state
}

// GetDevice returns the underlying PN532 device
func (m *Monitor) GetDevice() *pn532.Device {
	return m.device
}

// Close cleans up the monitor resources
func (m *Monitor) Close() error {
	// Stop any running removal timer
	if m.state.RemovalTimer != nil {
		m.state.RemovalTimer.Stop()
		m.state.RemovalTimer = nil
	}
	if err := m.device.Close(); err != nil {
		return fmt.Errorf("failed to close device: %w", err)
	}
	return nil
}

// continuousPolling runs continuous InAutoPoll monitoring
func (m *Monitor) continuousPolling(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		detectedTag, err := m.performSinglePoll(ctx)
		if err != nil {
			if !errors.Is(err, ErrNoTagInPoll) {
				m.handlePollingError(err)
			}
			// No tag detected or error handled - continue polling
			continue
		}

		m.processPollingResults(detectedTag)

		// Add a small delay between polling attempts to prevent excessive CPU usage
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
			// Continue to next poll
		}
	}
}

// performSinglePoll performs a single tag detection cycle using direct InListPassiveTarget
func (m *Monitor) performSinglePoll(ctx context.Context) (*pn532.DetectedTag, error) {
	// Create a timeout context for this single poll attempt
	pollCtx, cancel := context.WithTimeout(ctx, m.config.PollInterval)
	defer cancel()

	// Use InListPassiveTargetContext directly to get immediate results
	tags, err := m.device.InListPassiveTargetContext(pollCtx, 1, 0x00)
	if err != nil {
		// Check if it's a timeout (no tag detected) vs actual error
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrNoTagInPoll // No tag detected within timeout
		}
		return nil, fmt.Errorf("tag detection failed: %w", err)
	}

	// Check if any tags were found
	if len(tags) == 0 {
		return nil, ErrNoTagInPoll // No tag detected, but not an error
	}

	return tags[0], nil
}

// handlePollingError handles errors from polling operations
func (m *Monitor) handlePollingError(err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		// Timeout is normal - timer will handle removal detection
		return
	}

	if errors.Is(err, context.Canceled) {
		return
	}

	// For serious device errors, trigger immediate card removal
	// This handles cases like device disconnection
	m.handleCardRemoval()
}

// handleCardRemoval handles card removal state changes
func (m *Monitor) handleCardRemoval() {
	if m.state.Present {
		if m.OnCardRemoved != nil {
			m.OnCardRemoved()
		}
		m.resetCardState()
	}
}

// resetCardState resets the card state to empty
func (m *Monitor) resetCardState() {
	m.state.TransitionToIdle()
}

// processPollingResults processes the detected tag
func (m *Monitor) processPollingResults(detectedTag *pn532.DetectedTag) {
	if detectedTag == nil {
		// No tag detected - removal handled by timer, nothing to do here
		return
	}

	// Card present - handle state transitions
	cardChanged := m.updateCardState(detectedTag)

	// Transition to detected state with removal timer (unless we're currently reading)
	if m.state.DetectionState != StateReading {
		m.state.TransitionToDetected(m.config.CardRemovalTimeout, func() {
			m.handleCardRemoval()
		})
	}

	if cardChanged || m.shouldTestCard(detectedTag.UID) {
		m.testAndRecordCard(detectedTag)
	}
}

// updateCardState updates the card state and returns whether the card changed
func (m *Monitor) updateCardState(detectedTag *pn532.DetectedTag) bool {
	currentUID := detectedTag.UID
	cardType := string(detectedTag.Type)

	if !m.state.Present {
		if m.OnCardDetected != nil {
			_ = m.OnCardDetected(detectedTag)
		}
		m.state.Present = true
		m.state.LastUID = currentUID
		m.state.LastType = cardType
		m.state.TestedUID = ""
		return true
	}

	if m.state.LastUID != currentUID {
		if m.OnCardChanged != nil {
			_ = m.OnCardChanged(detectedTag)
		}
		m.state.LastUID = currentUID
		m.state.LastType = cardType
		m.state.TestedUID = ""
		return true
	}

	return false
}

// shouldTestCard determines if we should test the card
func (m *Monitor) shouldTestCard(currentUID string) bool {
	return m.state.TestedUID != currentUID
}

// testAndRecordCard tests the card and records the result
func (m *Monitor) testAndRecordCard(detectedTag *pn532.DetectedTag) {
	// Transition to reading state to prevent removal timer from firing during long reads
	m.state.TransitionToReading()

	// Mark as tested to prevent repeated testing
	m.state.TestedUID = detectedTag.UID

	// Transition to post-read grace period with shorter timeout
	m.state.TransitionToPostReadGrace(m.config.CardRemovalTimeout, func() {
		m.handleCardRemoval()
	})
}
