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
	"sync"
	"sync/atomic"
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
	pauseChan      chan struct{}
	pauseAckChan   chan struct{} // Added for pause acknowledgment
	resumeChan     chan struct{}
	state          CardState
	stateMutex     sync.RWMutex // Protects state access
	writeMutex     sync.Mutex
	isPaused       atomic.Bool
}

// NewMonitor creates a new card monitor
func NewMonitor(device *pn532.Device, config *Config) *Monitor {
	if config == nil {
		config = DefaultConfig()
	}
	return &Monitor{
		device:       device,
		config:       config,
		state:        CardState{},
		pauseChan:    make(chan struct{}, 1),
		pauseAckChan: make(chan struct{}, 1), // Added for pause acknowledgment
		resumeChan:   make(chan struct{}, 1),
	}
}

// Start begins continuous monitoring for cards
func (m *Monitor) Start(ctx context.Context) error {
	return m.continuousPolling(ctx)
}

// GetState returns the current card state
func (m *Monitor) GetState() CardState {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()
	return m.state
}

// GetDevice returns the underlying PN532 device
func (m *Monitor) GetDevice() *pn532.Device {
	return m.device
}

// Close cleans up the monitor resources
func (m *Monitor) Close() error {
	// Stop any running removal timer
	m.stateMutex.Lock()
	if m.state.RemovalTimer != nil {
		m.state.RemovalTimer.Stop()
		m.state.RemovalTimer = nil
	}
	m.stateMutex.Unlock()

	if err := m.device.Close(); err != nil {
		return fmt.Errorf("failed to close device: %w", err)
	}
	return nil
}

// Pause temporarily stops the polling loop
// This is used to coordinate with write operations
func (m *Monitor) Pause() {
	if m.isPaused.CompareAndSwap(false, true) {
		// Signal pause to the polling loop - use non-blocking send for when no loop is running
		select {
		case m.pauseChan <- struct{}{}:
			// Successfully sent pause signal
		default:
			// Channel full or no receiver - that's OK, isPaused flag is set
		}
	}
}

// Resume restarts the polling loop after a pause
func (m *Monitor) Resume() {
	if m.isPaused.CompareAndSwap(true, false) {
		// Signal resume to the polling loop - use non-blocking send for when no loop is running
		select {
		case m.resumeChan <- struct{}{}:
			// Successfully sent resume signal
		default:
			// Channel full or no receiver - that's OK, isPaused flag is cleared
		}
	}
}

// pauseWithAck pauses polling and waits for acknowledgment
func (m *Monitor) pauseWithAck() error {
	if !m.isPaused.CompareAndSwap(false, true) {
		return nil // Already paused
	}

	// Send pause signal
	select {
	case m.pauseChan <- struct{}{}:
		// Wait for acknowledgment that polling loop actually paused
		select {
		case <-m.pauseAckChan:
			return nil
		case <-time.After(100 * time.Millisecond):
			// Timeout waiting for ack - proceed anyway but this indicates a potential issue
			return nil
		}
	default:
		// Channel full or no receiver - polling loop not running
		return nil
	}
}

// WriteToTag performs a thread-safe write operation to a detected tag
// This method pauses polling during the write to prevent interference
func (m *Monitor) WriteToTag(detectedTag *pn532.DetectedTag, writeFn func(pn532.Tag) error) error {
	// Acquire write mutex to prevent concurrent writes
	m.writeMutex.Lock()
	defer m.writeMutex.Unlock()

	// Enhanced pause with acknowledgment
	if err := m.pauseWithAck(); err != nil {
		return err
	}
	defer m.Resume()

	// Create tag from detected tag
	tag, err := m.device.CreateTag(detectedTag)
	if err != nil {
		return fmt.Errorf("failed to create tag: %w", err)
	}

	// Execute the write function
	return writeFn(tag)
}

// continuousPolling runs continuous InAutoPoll monitoring
func (m *Monitor) continuousPolling(ctx context.Context) error {
	ticker := time.NewTicker(m.config.PollInterval)
	defer ticker.Stop()

	for {
		if err := m.handleContextAndPause(ctx); err != nil {
			return err
		}

		if err := m.executeSinglePollingCycle(ctx); err != nil {
			return err
		}

		if err := m.waitForNextPollOrPause(ctx, ticker); err != nil {
			return err
		}
	}
}

// executeSinglePollingCycle performs one polling cycle and processes results
func (m *Monitor) executeSinglePollingCycle(ctx context.Context) error {
	detectedTag, err := m.performSinglePoll(ctx)
	if err != nil {
		if !errors.Is(err, ErrNoTagInPoll) {
			m.handlePollingError(err)
		}
		return nil
	}

	if err := m.processPollingResults(detectedTag); err != nil {
		return fmt.Errorf("callback error during polling: %w", err)
	}
	return nil
}

// waitForNextPollOrPause waits for the next poll interval or handles pause signals
func (m *Monitor) waitForNextPollOrPause(ctx context.Context, ticker *time.Ticker) error {
	select {
	case <-ticker.C:
		return nil
	case <-m.pauseChan:
		return m.handlePauseSignal(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// handlePauseSignal sends acknowledgment and waits for resume
func (m *Monitor) handlePauseSignal(ctx context.Context) error {
	// Send acknowledgment
	select {
	case m.pauseAckChan <- struct{}{}:
	default:
	}
	// Wait for resume
	return m.waitForResume(ctx)
}

func (m *Monitor) handleContextAndPause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.pauseChan:
		return m.waitForResume(ctx)
	default:
		return nil
	}
}

func (m *Monitor) waitForResume(ctx context.Context) error {
	select {
	case <-m.resumeChan:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// performSinglePoll performs a single tag detection cycle using direct InListPassiveTarget
func (m *Monitor) performSinglePoll(ctx context.Context) (*pn532.DetectedTag, error) {
	// Use immediate polling without timeout to avoid double delay
	// The polling interval is handled in the main loop
	tags, err := m.device.InListPassiveTargetContext(ctx, 1, 0x00)
	if err != nil {
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
	m.stateMutex.Lock()
	wasPresent := m.state.Present
	if wasPresent {
		m.state.TransitionToIdle()
	}
	m.stateMutex.Unlock()

	// Call callback outside the lock to avoid potential deadlocks
	if wasPresent && m.OnCardRemoved != nil {
		m.OnCardRemoved()
	}
}

// processPollingResults processes the detected tag and returns any callback errors
func (m *Monitor) processPollingResults(detectedTag *pn532.DetectedTag) error {
	if detectedTag == nil {
		// No tag detected - removal handled by timer, nothing to do here
		return nil
	}

	// Card present - handle state transitions
	cardChanged, err := m.updateCardState(detectedTag)
	if err != nil {
		return err
	}

	// Transition to detected state with removal timer (unless we're currently reading)
	m.stateMutex.Lock()
	shouldTransition := m.state.DetectionState != StateReading
	if shouldTransition {
		m.state.TransitionToDetected(m.config.CardRemovalTimeout, func() {
			m.handleCardRemoval()
		})
	}
	m.stateMutex.Unlock()

	if cardChanged || m.shouldTestCard(detectedTag.UID) {
		m.testAndRecordCard(detectedTag)
	}

	return nil
}

// updateCardState updates the card state and returns whether the card changed and any callback error
func (m *Monitor) updateCardState(detectedTag *pn532.DetectedTag) (bool, error) {
	currentUID := detectedTag.UID
	cardType := string(detectedTag.Type)

	m.stateMutex.Lock()
	defer m.stateMutex.Unlock()

	if !m.state.Present {
		// Release lock before callback to avoid potential deadlocks
		m.stateMutex.Unlock()
		if m.OnCardDetected != nil {
			if err := m.OnCardDetected(detectedTag); err != nil {
				m.stateMutex.Lock() // Re-acquire for defer
				return false, fmt.Errorf("OnCardDetected callback failed: %w", err)
			}
		}
		m.stateMutex.Lock() // Re-acquire for state modification

		m.state.Present = true
		m.state.LastUID = currentUID
		m.state.LastType = cardType
		m.state.TestedUID = ""
		return true, nil
	}

	if m.state.LastUID != currentUID {
		// Release lock before callback to avoid potential deadlocks
		m.stateMutex.Unlock()
		if m.OnCardChanged != nil {
			if err := m.OnCardChanged(detectedTag); err != nil {
				m.stateMutex.Lock() // Re-acquire for defer
				return false, fmt.Errorf("OnCardChanged callback failed: %w", err)
			}
		}
		m.stateMutex.Lock() // Re-acquire for state modification

		m.state.LastUID = currentUID
		m.state.LastType = cardType
		m.state.TestedUID = ""
		return true, nil
	}

	return false, nil
}

// shouldTestCard determines if we should test the card
func (m *Monitor) shouldTestCard(currentUID string) bool {
	m.stateMutex.RLock()
	defer m.stateMutex.RUnlock()
	return m.state.TestedUID != currentUID
}

// testAndRecordCard tests the card and records the result
func (m *Monitor) testAndRecordCard(detectedTag *pn532.DetectedTag) {
	m.stateMutex.Lock()
	defer m.stateMutex.Unlock()

	// Transition to reading state to prevent removal timer from firing during long reads
	m.state.TransitionToReading()

	// Mark as tested to prevent repeated testing
	m.state.TestedUID = detectedTag.UID

	// Transition to post-read grace period with shorter timeout
	m.state.TransitionToPostReadGrace(m.config.CardRemovalTimeout, func() {
		m.handleCardRemoval()
	})
}
