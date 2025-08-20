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

// Session handles continuous card monitoring with state machine
type Session struct {
	device         *pn532.Device
	config         *Config
	OnCardDetected func(tag *pn532.DetectedTag) error
	OnCardRemoved  func()
	OnCardChanged  func(tag *pn532.DetectedTag) error
	pauseChan      chan struct{}
	resumeChan     chan struct{}
	ackChan        chan struct{}
	actor          *DeviceActor
	state          CardState
	stateMutex     sync.RWMutex
	writeMutex     sync.Mutex
	isPaused       atomic.Bool
}

// NewSession creates a new card monitoring session
func NewSession(device *pn532.Device, config *Config) *Session {
	if config == nil {
		config = DefaultConfig()
	}
	return &Session{
		device:     device,
		config:     config,
		state:      CardState{},
		pauseChan:  make(chan struct{}, 1),
		resumeChan: make(chan struct{}, 1),
		ackChan:    make(chan struct{}, 1),
	}
}

// NewActorBasedSession creates a session using DeviceActor underneath
func NewActorBasedSession(device *pn532.Device, config *Config) *Session {
	session := NewSession(device, config)

	// Create DeviceActor with callbacks that delegate to session callbacks
	callbacks := DeviceCallbacks{
		OnCardDetected: func(tag *pn532.DetectedTag) error {
			if session.OnCardDetected != nil {
				return session.OnCardDetected(tag)
			}
			return nil
		},
		OnCardRemoved: func() {
			if session.OnCardRemoved != nil {
				session.OnCardRemoved()
			}
		},
		OnCardChanged: func(tag *pn532.DetectedTag) error {
			if session.OnCardChanged != nil {
				return session.OnCardChanged(tag)
			}
			return nil
		},
	}

	session.actor = NewDeviceActor(device, config, callbacks)
	return session
}

// Start begins continuous monitoring for cards
func (s *Session) Start(ctx context.Context) error {
	// If we have an actor, delegate to it instead of using direct polling
	if s.actor != nil {
		return s.actor.Start(ctx)
	}
	// Fall back to direct polling for regular sessions
	return s.continuousPolling(ctx)
}

// GetState returns the current card state
func (s *Session) GetState() CardState {
	s.stateMutex.RLock()
	defer s.stateMutex.RUnlock()
	return s.state
}

// GetDevice returns the underlying PN532 device
func (s *Session) GetDevice() *pn532.Device {
	return s.device
}

// GetDeviceActor returns the underlying DeviceActor for actor-based sessions
func (s *Session) GetDeviceActor() *DeviceActor {
	return s.actor
}

// Close cleans up the monitor resources
func (s *Session) Close() error {
	// Stop any running removal timer
	s.stateMutex.Lock()
	if s.state.RemovalTimer != nil {
		safeTimerStop(s.state.RemovalTimer)
		s.state.RemovalTimer = nil
	}
	s.stateMutex.Unlock()

	// Reset pause state and drain channels to prevent corruption
	s.isPaused.Store(false)

	// Drain pause/resume channels to prevent future state corruption
	select {
	case <-s.pauseChan:
	default:
	}
	select {
	case <-s.resumeChan:
	default:
	}

	// If we have an actor, stop it first to stop the polling goroutine
	if s.actor != nil {
		ctx := context.Background() // Use background context for cleanup
		if err := s.actor.Stop(ctx); err != nil {
			return fmt.Errorf("failed to stop device actor: %w", err)
		}
	}

	return nil
}

// Pause temporarily stops the polling loop
// This is used to coordinate with write operations
func (s *Session) Pause() {
	if s.isPaused.CompareAndSwap(false, true) {
		// Signal pause to the polling loop - use non-blocking send for when no loop is running
		select {
		case s.pauseChan <- struct{}{}:
			// Successfully sent pause signal
		default:
			// Channel full or no receiver - that's OK, isPaused flag is set
		}
	}
}

// Resume restarts the polling loop after a pause
func (s *Session) Resume() {
	if s.isPaused.CompareAndSwap(true, false) {
		// Signal resume to the polling loop - use non-blocking send for when no loop is running
		select {
		case s.resumeChan <- struct{}{}:
			// Successfully sent resume signal
		default:
			// Channel full or no receiver - that's OK, isPaused flag is cleared
		}
	}
}

// pauseWithAck pauses polling and waits for acknowledgment
func (s *Session) pauseWithAck(ctx context.Context) error {
	// Check if context is already cancelled
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Check if already paused to avoid redundant operations
	if s.isPaused.Load() {
		return nil
	}

	// Use atomic operation to set pause state safely
	if !s.isPaused.CompareAndSwap(false, true) {
		return nil // Another goroutine beat us to it
	}

	// Send pause signal with context-aware non-blocking send
	select {
	case s.pauseChan <- struct{}{}:
		// Successfully sent pause signal, now wait for acknowledgment
		select {
		case <-s.ackChan:
			// Polling goroutine has acknowledged the pause
			return nil
		case <-ctx.Done():
			// Context cancelled, restore pause state and return error
			s.isPaused.Store(false)
			return ctx.Err()
		}
	case <-ctx.Done():
		// Context cancelled, restore pause state and return error
		s.isPaused.Store(false)
		return ctx.Err()
	default:
		// Channel full or no receiver - that's OK since isPaused flag is set
		return nil
	}
}

// WriteToNextTag waits for the next tag detection and performs a write operation
// This method blocks until a tag is detected or timeout occurs
// sessionCtx controls session lifetime, writeCtx controls write operation lifetime
func (s *Session) WriteToNextTag(
	sessionCtx context.Context,
	writeCtx context.Context,
	timeout time.Duration,
	writeFn func(context.Context, pn532.Tag) error,
) error {
	// Acquire write mutex to prevent concurrent writes
	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()

	// Pause polling to prevent interference with our write operation
	if err := s.pauseWithAck(sessionCtx); err != nil {
		return fmt.Errorf("failed to pause polling: %w", err)
	}
	defer s.Resume()

	// Create a timeout context that cancels if either session or timeout expires
	timeoutCtx, cancel := context.WithTimeout(sessionCtx, timeout)
	defer cancel()

	// Poll continuously until we find a tag or timeout
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	for {
		// Attempt to detect a tag
		detectedTag, err := s.performSinglePoll(timeoutCtx)
		if err == nil {
			// Tag found - create Tag object and call write function
			tag, tagErr := s.device.CreateTag(detectedTag)
			if tagErr != nil {
				return fmt.Errorf("failed to create tag: %w", tagErr)
			}
			return writeFn(writeCtx, tag)
		}

		if !errors.Is(err, ErrNoTagInPoll) {
			// Real error occurred
			return fmt.Errorf("tag detection failed: %w", err)
		}

		// Wait for next poll interval or timeout
		select {
		case <-ticker.C:
			continue
		case <-timeoutCtx.Done():
			if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
				return errors.New("timeout waiting for tag")
			}
			return timeoutCtx.Err()
		}
	}
}

// WriteToTag performs a thread-safe write operation to a detected tag
// This method pauses polling during the write to prevent interference
// sessionCtx controls session lifetime, writeCtx controls write operation lifetime
func (s *Session) WriteToTag(
	sessionCtx context.Context,
	writeCtx context.Context,
	detectedTag *pn532.DetectedTag,
	writeFn func(context.Context, pn532.Tag) error,
) error {
	// Acquire write mutex to prevent concurrent writes
	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()

	// Enhanced pause with acknowledgment - now requires context
	if err := s.pauseWithAck(sessionCtx); err != nil {
		return fmt.Errorf("failed to pause polling: %w", err)
	}
	defer s.Resume()

	// Create tag from detected tag
	tag, err := s.device.CreateTag(detectedTag)
	if err != nil {
		return fmt.Errorf("failed to create tag: %w", err)
	}

	// Execute the write function with the write context
	return writeFn(writeCtx, tag)
}

// continuousPolling runs continuous InAutoPoll monitoring
func (s *Session) continuousPolling(ctx context.Context) error {
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	for {
		if err := s.handleContextAndPause(ctx); err != nil {
			return err
		}

		if err := s.executeSinglePollingCycle(ctx); err != nil {
			return err
		}

		if err := s.waitForNextPollOrPause(ctx, ticker); err != nil {
			return err
		}
	}
}

// executeSinglePollingCycle performs one polling cycle and processes results
func (s *Session) executeSinglePollingCycle(ctx context.Context) error {
	detectedTag, err := s.performSinglePoll(ctx)
	if err != nil {
		if !errors.Is(err, ErrNoTagInPoll) {
			s.handlePollingError(err)
		}
		return nil
	}

	if err := s.processPollingResults(detectedTag); err != nil {
		return fmt.Errorf("callback error during polling: %w", err)
	}
	return nil
}

// waitForNextPollOrPause waits for the next poll interval or handles pause signals
func (s *Session) waitForNextPollOrPause(ctx context.Context, ticker *time.Ticker) error {
	select {
	case <-ticker.C:
		return nil
	case <-s.pauseChan:
		return s.handlePauseSignal(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// handlePauseSignal sends acknowledgment and waits for resume
func (s *Session) handlePauseSignal(ctx context.Context) error {
	// Send acknowledgment to indicate polling is paused
	select {
	case s.ackChan <- struct{}{}:
		// Successfully sent acknowledgment
	default:
		// Channel full or no receiver - continue anyway
	}
	// Wait for resume
	return s.waitForResume(ctx)
}

func (s *Session) handleContextAndPause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.pauseChan:
		return s.waitForResume(ctx)
	default:
		return nil
	}
}

func (s *Session) waitForResume(ctx context.Context) error {
	select {
	case <-s.resumeChan:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// performSinglePoll performs a single tag detection cycle using direct InListPassiveTarget
func (s *Session) performSinglePoll(ctx context.Context) (*pn532.DetectedTag, error) {
	// Use immediate polling without timeout to avoid double delay
	// The polling interval is handled in the main loop
	tags, err := s.device.InListPassiveTargetContext(ctx, 1, 0x00)
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
func (s *Session) handlePollingError(err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		// Timeout is normal - timer will handle removal detection
		return
	}

	if errors.Is(err, context.Canceled) {
		return
	}

	// For serious device errors, trigger immediate card removal
	// This handles cases like device disconnection
	s.handleCardRemoval()
}

// handleCardRemoval handles card removal state changes
func (s *Session) handleCardRemoval() {
	s.stateMutex.Lock()
	wasPresent := s.state.Present
	if wasPresent {
		s.state.TransitionToIdle()
	}
	s.stateMutex.Unlock()

	// Call callback outside the lock to avoid potential deadlocks
	if wasPresent && s.OnCardRemoved != nil {
		s.OnCardRemoved()
	}
}

// processPollingResults processes the detected tag and returns any callback errors
func (s *Session) processPollingResults(detectedTag *pn532.DetectedTag) error {
	if detectedTag == nil {
		// No tag detected - removal handled by timer, nothing to do here
		return nil
	}

	// Card present - handle state transitions
	cardChanged, err := s.updateCardState(detectedTag)
	if err != nil {
		return err
	}

	// Transition to detected state with removal timer (unless we're currently reading)
	s.stateMutex.Lock()
	shouldTransition := s.state.DetectionState != StateReading
	if shouldTransition {
		s.state.TransitionToDetected(s.config.CardRemovalTimeout, func() {
			s.handleCardRemoval()
		})
	}
	s.stateMutex.Unlock()

	if cardChanged || s.shouldTestCard(detectedTag.UID) {
		s.testAndRecordCard(detectedTag)
	}

	return nil
}

// safeCallCallback executes a callback with panic recovery
func (*Session) safeCallCallback(
	callback func(*pn532.DetectedTag) error,
	tag *pn532.DetectedTag,
	callbackName string,
) error {
	var callbackErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				callbackErr = fmt.Errorf("%s callback panicked: %v", callbackName, r)
			}
		}()
		callbackErr = callback(tag)
	}()
	if callbackErr != nil {
		return fmt.Errorf("%s callback failed: %w", callbackName, callbackErr)
	}
	return nil
}

// updateCardState updates the card state and returns whether the card changed and any callback error
func (s *Session) updateCardState(detectedTag *pn532.DetectedTag) (bool, error) {
	currentUID := detectedTag.UID
	cardType := string(detectedTag.Type)

	// Check state and determine what callbacks to call without holding lock
	s.stateMutex.RLock()
	wasPresent := s.state.Present
	wasChanged := wasPresent && s.state.LastUID != currentUID
	s.stateMutex.RUnlock()

	// Call callbacks outside of any locks with panic recovery
	if !wasPresent && s.OnCardDetected != nil {
		if err := s.safeCallCallback(s.OnCardDetected, detectedTag, "OnCardDetected"); err != nil {
			return false, err
		}
	} else if wasChanged && s.OnCardChanged != nil {
		if err := s.safeCallCallback(s.OnCardChanged, detectedTag, "OnCardChanged"); err != nil {
			return false, err
		}
	}

	// Update state under lock
	s.stateMutex.Lock()
	defer s.stateMutex.Unlock()

	if !wasPresent {
		s.state.Present = true
		s.state.LastUID = currentUID
		s.state.LastType = cardType
		s.state.TestedUID = ""
		return true, nil
	}

	if wasChanged {
		s.state.LastUID = currentUID
		s.state.LastType = cardType
		s.state.TestedUID = ""
		return true, nil
	}

	return false, nil
}

// shouldTestCard determines if we should test the card
func (s *Session) shouldTestCard(currentUID string) bool {
	s.stateMutex.RLock()
	defer s.stateMutex.RUnlock()
	return s.state.TestedUID != currentUID
}

// testAndRecordCard tests the card and records the result
func (s *Session) testAndRecordCard(detectedTag *pn532.DetectedTag) {
	s.stateMutex.Lock()
	defer s.stateMutex.Unlock()

	// Transition to reading state to prevent removal timer from firing during long reads
	s.state.TransitionToReading()

	// Mark as tested to prevent repeated testing
	s.state.TestedUID = detectedTag.UID

	// Transition to post-read grace period with shorter timeout
	s.state.TransitionToPostReadGrace(s.config.CardRemovalTimeout, func() {
		s.handleCardRemoval()
	})
}
