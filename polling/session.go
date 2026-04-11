// Copyright 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package polling

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/internal/syncutil"
)

// pollCycleTimeout limits how long a single poll cycle can take.
// Prevents PN532 from hanging indefinitely after bad auth attempts.
const pollCycleTimeout = 10 * time.Second

// writeStabilizationDelay is the time to wait after tag detection before
// attempting a write. This allows the card to settle in position when the
// user may still be positioning the card as the write is triggered.
// This prevents error 0x27 ("wrong context") which occurs when the RF field
// becomes unstable due to card movement, causing target selection to be lost.
const writeStabilizationDelay = 75 * time.Millisecond

// Session handles continuous card monitoring with state machine
type Session struct {
	lastPollTime         time.Time
	recoverer            DeviceRecoverer
	device               *pn532.Device
	OnSleepDetected      func()
	OnDeviceDisconnected func(err error)
	pauseChan            chan struct{}
	resumeChan           chan struct{}
	ackChan              chan struct{}
	config               *Config
	OnCardChanged        func(ctx context.Context, tag *pn532.DetectedTag) error
	OnCardDetected       func(ctx context.Context, tag *pn532.DetectedTag) error
	OnCardRemoved        func()
	state                CardState
	stateMutex           syncutil.RWMutex
	writeMutex           syncutil.Mutex
	closed               atomic.Bool
	isPaused             atomic.Bool
}

// NewSession creates a new card monitoring session.
// If sleep recovery is enabled in config, a DefaultRecoverer is automatically
// created for soft reset recovery. Use SetRecoverer to provide a custom
// recoverer with full reconnection capability via ReopenFunc.
func NewSession(device *pn532.Device, config *Config) *Session {
	if config == nil {
		config = DefaultConfig()
	}

	session := &Session{
		device:     device,
		config:     config,
		state:      CardState{},
		pauseChan:  make(chan struct{}, 1),
		resumeChan: make(chan struct{}, 1),
		ackChan:    make(chan struct{}, 1),
	}

	// Auto-create recoverer when sleep recovery is enabled
	if config.SleepRecovery.Enabled {
		session.recoverer = NewDefaultRecoverer(
			device,
			nil, // No reopen func by default, only soft reset
			config.SleepRecovery.RecoveryBackoff,
			config.SleepRecovery.MaxRecoveryAttempts,
		)
	}

	return session
}

// Start begins continuous monitoring for cards. It blocks until ctx is cancelled
// or an unrecoverable error occurs.
//
// Callers must:
//  1. Run Start in a dedicated goroutine.
//  2. Track that goroutine (for example with a sync.WaitGroup).
//  3. On shutdown, cancel the context, call Close, then wait for the goroutine.
//
// Start returns promptly when ctx is cancelled during normal polling. During the
// initial hardware configuration it is bounded by startInitTimeout so a wedged
// transport cannot delay Start's exit indefinitely.
func (s *Session) Start(ctx context.Context) error {
	return s.continuousPolling(ctx)
}

// GetState returns the current card state
func (s *Session) GetState() CardState {
	s.stateMutex.RLock()
	defer s.stateMutex.RUnlock()
	return s.state
}

// GetDevice returns the underlying PN532 device.
// This may return a different device after sleep/wake recovery.
func (s *Session) GetDevice() *pn532.Device {
	s.stateMutex.RLock()
	defer s.stateMutex.RUnlock()
	return s.device
}

// SetOnCardDetected sets the callback for when a card is detected.
// The callback receives a context that is tied to the poll cycle timeout,
// allowing long-running operations (like MIFARE authentication) to be cancelled
// if the tag is removed or the operation takes too long.
func (s *Session) SetOnCardDetected(callback func(context.Context, *pn532.DetectedTag) error) {
	s.stateMutex.Lock()
	defer s.stateMutex.Unlock()
	s.OnCardDetected = callback
}

// SetOnCardRemoved sets the callback for when a card is removed.
func (s *Session) SetOnCardRemoved(callback func()) {
	s.stateMutex.Lock()
	defer s.stateMutex.Unlock()
	s.OnCardRemoved = callback
}

// SetOnCardChanged sets the callback for when the card changes.
// The callback receives a context that is tied to the poll cycle timeout,
// allowing long-running operations to be cancelled if needed.
func (s *Session) SetOnCardChanged(callback func(context.Context, *pn532.DetectedTag) error) {
	s.stateMutex.Lock()
	defer s.stateMutex.Unlock()
	s.OnCardChanged = callback
}

// SetOnSleepDetected sets the callback for when system sleep/wake is detected.
func (s *Session) SetOnSleepDetected(callback func()) {
	s.stateMutex.Lock()
	defer s.stateMutex.Unlock()
	s.OnSleepDetected = callback
}

// SetOnDeviceDisconnected sets the callback for when the device is disconnected.
// This is called when a fatal error indicates the device is no longer available
// (e.g., USB unplugged). The error parameter contains the underlying cause.
func (s *Session) SetOnDeviceDisconnected(callback func(err error)) {
	s.stateMutex.Lock()
	defer s.stateMutex.Unlock()
	s.OnDeviceDisconnected = callback
}

// SetRecoverer configures a custom device recoverer for sleep/wake handling.
// Use this to provide a recoverer with ReopenFunc for full reconnection
// capability beyond the default soft reset behavior.
func (s *Session) SetRecoverer(r DeviceRecoverer) {
	s.stateMutex.Lock()
	defer s.stateMutex.Unlock()
	s.recoverer = r
}

// Close cleans up the monitor resources
func (s *Session) Close() error {
	// Mark session as closed to prevent timer callbacks from executing
	s.closed.Store(true)

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
		// Successfully sent pause signal, now wait for acknowledgment with timeout.
		// Use a named timer so cancellation releases the underlying runtime timer
		// immediately rather than lingering until it fires.
		ackTimer := time.NewTimer(100 * time.Millisecond)
		defer ackTimer.Stop()
		select {
		case <-s.ackChan:
			// Polling goroutine has acknowledged the pause
			return nil
		case <-ackTimer.C:
			// No acknowledgment received - likely no polling loop running
			// This is OK for testing scenarios, pause state is already set
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

// executeWriteToTag creates a tag from detected tag and executes the write function.
// Includes a stabilization delay to let the card settle before writing, which prevents
// error 0x27 ("wrong context") when the RF field is disrupted by card movement.
// Clears transport state on write error for recovery.
func (s *Session) executeWriteToTag(
	writeCtx context.Context,
	detectedTag *pn532.DetectedTag,
	writeFn func(context.Context, pn532.Tag) error,
) error {
	// Stabilization delay - let the card settle in position before writing.
	// This prevents error 0x27 ("wrong context") which occurs when the user is
	// still positioning the card as the write triggers. The RF field becomes
	// unstable during movement, causing the PN532 to lose target selection.
	stabilizationTimer := time.NewTimer(writeStabilizationDelay)
	select {
	case <-writeCtx.Done():
		stabilizationTimer.Stop()
		return writeCtx.Err()
	case <-stabilizationTimer.C:
	}

	// Use GetDevice() to get the current device reference (may be updated after recovery)
	device := s.GetDevice()
	if device == nil {
		return errors.New("device not available")
	}
	tag, tagErr := device.CreateTag(detectedTag)
	if tagErr != nil {
		return fmt.Errorf("failed to create tag: %w", tagErr)
	}
	writeErr := writeFn(writeCtx, tag)
	if writeErr != nil {
		_ = device.ClearTransportState()
	}
	return writeErr
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
			return s.executeWriteToTag(writeCtx, detectedTag, writeFn)
		}

		if !errors.Is(err, ErrNoTagInPoll) {
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

	// Pause with ACK handshake to ensure clean state before stopping
	if err := s.pauseWithAck(sessionCtx); err != nil {
		return fmt.Errorf("failed to pause polling: %w", err)
	}
	defer s.Resume()

	return s.executeWriteToTag(writeCtx, detectedTag, writeFn)
}

// WriteToNextTagWithRetry waits for the next tag detection and performs a write operation
// with automatic retry on transient errors. This is useful for handling intermittent
// write failures due to card placement issues or timing problems.
// sessionCtx controls session lifetime, writeCtx controls write operation lifetime.
// maxRetries specifies how many times to retry the write operation (default 3 if <= 0).
func (s *Session) WriteToNextTagWithRetry(
	sessionCtx context.Context,
	writeCtx context.Context,
	timeout time.Duration,
	maxRetries int,
	writeFn func(context.Context, pn532.Tag) error,
) error {
	// Wrap the write function with retry logic
	wrappedFn := func(ctx context.Context, tag pn532.Tag) error {
		return pn532.WriteNDEFWithRetry(ctx, func(innerCtx context.Context) error {
			return writeFn(innerCtx, tag)
		}, maxRetries, string(tag.Type()))
	}

	return s.WriteToNextTag(sessionCtx, writeCtx, timeout, wrappedFn)
}

// WriteToTagWithRetry performs a thread-safe write operation to a detected tag
// with automatic retry on transient errors. This is useful for handling intermittent
// write failures due to card placement issues or timing problems.
// sessionCtx controls session lifetime, writeCtx controls write operation lifetime.
// maxRetries specifies how many times to retry the write operation (default 3 if <= 0).
func (s *Session) WriteToTagWithRetry(
	sessionCtx context.Context,
	writeCtx context.Context,
	detectedTag *pn532.DetectedTag,
	maxRetries int,
	writeFn func(context.Context, pn532.Tag) error,
) error {
	// Wrap the write function with retry logic
	wrappedFn := func(ctx context.Context, tag pn532.Tag) error {
		return pn532.WriteNDEFWithRetry(ctx, func(innerCtx context.Context) error {
			return writeFn(innerCtx, tag)
		}, maxRetries, string(tag.Type()))
	}

	return s.WriteToTag(sessionCtx, writeCtx, detectedTag, wrappedFn)
}

// continuousPolling runs continuous InAutoPoll monitoring
func (s *Session) continuousPolling(ctx context.Context) error {
	return s.runPollingLoop(ctx, s.executeSinglePollingCycle)
}

// startInitTimeout caps how long Session.Start will spend configuring the
// PN532 before the polling loop begins. A wedged transport must not block
// Start's goroutine past this bound.
const startInitTimeout = 5 * time.Second

// configureInitialHardware runs the one-time hardware setup that Start performs
// before entering the polling loop. Both commands are bounded by startInitTimeout
// so cancellation during Start's first two I/O calls is observed promptly even
// if the transport is unresponsive.
func (s *Session) configureInitialHardware(ctx context.Context) error {
	initCtx, cancel := context.WithTimeout(ctx, startInitTimeout)
	defer cancel()

	if err := s.device.SetPollingRetries(initCtx, s.config.HardwareTimeoutRetries); err != nil {
		return fmt.Errorf("failed to configure hardware polling retries: %w", err)
	}

	// Set device timeout to match hardware retries plus buffer.
	// Each retry is ~150ms, plus 1s buffer for host/driver overhead.
	hwTimeout := time.Duration(s.config.HardwareTimeoutRetries)*150*time.Millisecond + time.Second
	if err := s.device.SetTimeout(hwTimeout); err != nil {
		return fmt.Errorf("failed to set device timeout: %w", err)
	}
	return nil
}

// runPollingLoop is the generic polling loop used by both single and multi-tag modes
func (s *Session) runPollingLoop(ctx context.Context, cycleFunc func(context.Context) error) error {
	// Bound the initial hardware configuration with a per-start timeout so a
	// sluggish or unresponsive transport cannot delay Start's exit indefinitely.
	// This keeps the goroutine that runs Start honest about cancellation even
	// when the hardware is wedged on its first command.
	if err := s.configureInitialHardware(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	for {
		if err := s.handleContextAndPause(ctx); err != nil {
			return err
		}

		if err := cycleFunc(ctx); err != nil {
			return err
		}

		if err := s.waitForNextPollOrPause(ctx, ticker); err != nil {
			return err
		}
	}
}

// executeSinglePollingCycle performs one polling cycle and processes results
func (s *Session) executeSinglePollingCycle(ctx context.Context) error {
	// Check for time discontinuity (sleep detection)
	if err := s.checkSleepAndRecover(ctx); err != nil {
		return err // Recovery failed - exit polling loop
	}

	// Add timeout to detection phase to prevent PN532 from hanging indefinitely
	// Note: This timeout only applies to detection (performSinglePoll), not callbacks.
	// Callbacks receive the parent context so they can do long operations like writes.
	pollCtx, cancel := context.WithTimeout(ctx, pollCycleTimeout)
	defer cancel()

	detectedTag, err := s.performSinglePoll(pollCtx)
	if err != nil {
		return s.handlePollError(ctx, err)
	}

	// Pass parent ctx (not pollCtx) to callbacks - they may need more time for writes
	if err := s.processPollingResults(ctx, detectedTag); err != nil {
		return fmt.Errorf("callback error during polling: %w", err)
	}
	return nil
}

// handlePollError processes errors from performSinglePoll, including NoACK recovery.
// Returns nil to continue polling, or a non-nil error to exit the polling loop.
func (s *Session) handlePollError(ctx context.Context, err error) error {
	// No tag detected is not an error - continue polling
	if errors.Is(err, ErrNoTagInPoll) {
		return nil
	}

	// Check for NoACK error which indicates PN532 firmware lockup
	if errors.Is(err, pn532.ErrNoACK) {
		if s.attemptHardReset(ctx) {
			return nil // Recovery succeeded, continue polling
		}
		// Hard reset failed - check if the device is actually gone.
		// This prevents a wasted poll cycle by detecting disconnection
		// immediately rather than waiting for the next I/O failure.
		if devErr := s.checkDeviceHealth(); devErr != nil {
			s.handlePollingError(devErr)
			return devErr
		}
	}

	// Handle other errors normally
	s.handlePollingError(err)
	if pn532.IsFatal(err) {
		return err // Fatal error - exit polling loop
	}
	return nil // Transient error - continue polling
}

// checkDeviceHealth checks if the transport's underlying device is still present.
// Uses the DeviceHealthChecker interface if available on the transport.
func (s *Session) checkDeviceHealth() error {
	device := s.GetDevice()
	if device == nil {
		return nil
	}
	checker, ok := device.Transport().(pn532.DeviceHealthChecker)
	if !ok {
		return nil
	}
	if err := checker.CheckHealth(); err != nil {
		return fmt.Errorf("device health check failed: %w", err)
	}
	return nil
}

// attemptHardReset attempts nuclear recovery when PN532 enters firmware lockup.
// Returns true if recovery succeeded and polling can continue, false otherwise.
func (s *Session) attemptHardReset(ctx context.Context) bool {
	pn532.Debugln("PN532 lockup detected (no ACK), attempting hard reset")

	device := s.GetDevice()
	if device == nil {
		pn532.Debugln("attemptHardReset: device is nil")
		return false
	}

	if err := device.HardReset(ctx); err != nil {
		pn532.Debugf("attemptHardReset: failed: %v", err)
		return false
	}

	// Clear card state after recovery - tag may have moved during lockup
	s.stateMutex.Lock()
	s.state.TransitionToIdle()
	s.stateMutex.Unlock()

	pn532.Debugln("attemptHardReset: recovery successful, resuming polling")
	return true
}

// checkSleepAndRecover detects time discontinuity (system sleep) and attempts recovery
func (s *Session) checkSleepAndRecover(ctx context.Context) error {
	cfg := s.config.SleepRecovery
	if !cfg.Enabled {
		s.lastPollTime = time.Now()
		return nil
	}

	if err := s.handleTimeDiscontinuity(ctx, cfg); err != nil {
		return err
	}

	s.lastPollTime = time.Now()
	return nil
}

// handleTimeDiscontinuity checks for and handles time discontinuity (sleep detection)
func (s *Session) handleTimeDiscontinuity(ctx context.Context, cfg SleepRecoveryConfig) error {
	if s.lastPollTime.IsZero() {
		return nil
	}

	elapsed := time.Since(s.lastPollTime)
	if !cfg.DetectSleep(elapsed, s.config.PollInterval) {
		return nil
	}

	// Sleep detected - call callback and attempt recovery
	s.stateMutex.RLock()
	onSleep := s.OnSleepDetected
	recoverer := s.recoverer
	s.stateMutex.RUnlock()

	if onSleep != nil {
		onSleep()
	}

	return s.attemptSleepRecovery(ctx, recoverer)
}

// attemptSleepRecovery attempts to recover the device after sleep
func (s *Session) attemptSleepRecovery(ctx context.Context, recoverer DeviceRecoverer) error {
	if recoverer == nil {
		return nil
	}

	if err := recoverer.AttemptRecovery(ctx); err != nil {
		return fmt.Errorf("sleep recovery failed: %w", err)
	}

	// Update device reference if it changed during recovery
	if newDevice := recoverer.GetDevice(); newDevice != nil {
		s.stateMutex.Lock()
		if newDevice != s.device {
			s.device = newDevice
		}
		s.stateMutex.Unlock()
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
	// Use standard InListPassiveTarget with PN532 hardware handling retries
	// The hardware retry count was configured via SetPollingRetries() during session start
	tag, err := s.device.InListPassiveTarget(ctx, 0x00)
	if err != nil {
		return nil, fmt.Errorf("tag detection failed: %w", err)
	}

	// Check if a tag was found
	if tag == nil {
		return nil, ErrNoTagInPoll // No tag detected, but not an error
	}

	return tag, nil
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

	// If this is a fatal error (device disconnected), notify via callback
	// but do NOT trigger card removal - the card wasn't removed, the reader was
	if pn532.IsFatal(err) {
		s.stateMutex.RLock()
		onDisconnected := s.OnDeviceDisconnected
		s.stateMutex.RUnlock()

		if onDisconnected != nil {
			onDisconnected(err)
		}
		return
	}

	// For non-fatal errors, trigger card removal (transient comm issues)
	s.handleCardRemoval()
}

// handleCardRemoval handles card removal state changes
func (s *Session) handleCardRemoval() {
	// Bail out if session is closed to prevent timer callbacks from executing after cleanup
	if s.closed.Load() {
		return
	}

	s.stateMutex.Lock()
	// If we're in reading state, a new poll cycle is actively processing - ignore stale timer
	// This handles the edge case where timer.Stop() returned false (callback already spawned)
	// but the callback runs after TransitionToReading() released the lock
	if s.state.DetectionState == StateReading {
		pn532.Debugf("handleCardRemoval: in reading state, bailing out")
		s.stateMutex.Unlock()
		return
	}
	pn532.Debugf("handleCardRemoval: state=%v, present=%v", s.state.DetectionState, s.state.Present)
	wasPresent := s.state.Present
	if wasPresent {
		s.state.TransitionToIdle()
	}
	onRemoved := s.OnCardRemoved
	s.stateMutex.Unlock()

	// Call callback outside the lock to avoid potential deadlocks
	if wasPresent && onRemoved != nil {
		pn532.Debugf("handleCardRemoval: calling OnCardRemoved callback")
		onRemoved()
	}
}

// processPollingResults processes the detected tag and returns any callback errors.
// The context is passed to callbacks, allowing them to be cancelled if the poll cycle
// timeout is exceeded (e.g., when a tag is removed during MIFARE authentication).
func (s *Session) processPollingResults(ctx context.Context, detectedTag *pn532.DetectedTag) error {
	if detectedTag == nil {
		// No tag detected - removal handled by timer, nothing to do here
		return nil
	}

	// Stop any existing removal timer and transition to reading state BEFORE
	// calling callbacks. This prevents the old timer from firing during callback
	// execution (e.g., during NDEF reading which can take significant time).
	s.stateMutex.Lock()
	s.state.TransitionToReading()
	s.stateMutex.Unlock()

	// Card present - handle state transitions (calls OnCardDetected/OnCardChanged)
	cardChanged, err := s.updateCardState(ctx, detectedTag)
	// Handle callback failure without killing polling loop
	if err != nil {
		return s.handleCallbackFailure(ctx, err)
	}

	// Callback succeeded - reset failure counter
	s.stateMutex.Lock()
	s.state.ConsecutiveStableFailures = 0
	s.stateMutex.Unlock()

	// After callback completes, set up the appropriate timer for this card
	if cardChanged || s.shouldTestCard(detectedTag.UID) {
		s.testAndRecordCard(detectedTag)
	} else {
		// Card unchanged and already tested - just reset the removal timer
		s.stateMutex.Lock()
		s.state.TransitionToDetected(s.config.CardRemovalTimeout, func() {
			s.handleCardRemoval()
		})
		s.stateMutex.Unlock()
	}

	return nil
}

// handleCallbackFailure handles callback failures or RF errors during callback execution.
// Returns nil to continue polling - the per-cycle timeout handles giving up on stuck cards.
func (s *Session) handleCallbackFailure(ctx context.Context, failureErr error) error {
	s.stateMutex.Lock()
	s.state.ConsecutiveStableFailures++
	s.state.Present = false // Reset so callback runs again on next poll
	failures := s.state.ConsecutiveStableFailures
	s.stateMutex.Unlock()

	pn532.Debugf("Callback failed (attempt %d, will retry): %v", failures, failureErr)

	// Check if error contains NoACK - indicates PN532 firmware lockup
	// This can happen during MIFARE auth retries when the PN532 enters a bad state
	if errors.Is(failureErr, pn532.ErrNoACK) {
		pn532.Debugln("NoACK error in callback - attempting hard reset")
		if s.attemptHardReset(ctx) {
			return nil // Recovery succeeded, continue polling
		}
		// Hard reset failed - fall through to RF cycle as last resort
	}

	// After first callback failure, cycle RF field to clear stuck state
	// This prevents PN532 from hanging on subsequent InListPassiveTarget calls
	// Note: Threshold is 1 (not 3) because hangs occur immediately after auth failures
	if failures >= 1 {
		pn532.Debugf("Cycling RF field to clear stuck state")
		if err := s.device.CycleRFField(ctx); errors.Is(err, pn532.ErrNoACK) {
			pn532.Debugln("NoACK during CycleRFField - attempting hard reset")
			s.attemptHardReset(ctx)
		}
	}

	return nil
}

// safeCallCallback executes a callback with panic recovery.
// The context is passed to the callback, allowing it to be cancelled if needed.
func (*Session) safeCallCallback(
	ctx context.Context,
	callback func(context.Context, *pn532.DetectedTag) error,
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
		callbackErr = callback(ctx, tag)
	}()
	if callbackErr != nil {
		return fmt.Errorf("%s callback failed: %w", callbackName, callbackErr)
	}
	return nil
}

// updateCardState updates the card state and returns whether the card changed and any callback error.
// The context is passed to callbacks, allowing long-running operations to be cancelled.
func (s *Session) updateCardState(ctx context.Context, detectedTag *pn532.DetectedTag) (bool, error) {
	currentUID := detectedTag.UID
	cardType := string(detectedTag.Type)

	// Capture state and callbacks under lock to avoid races
	s.stateMutex.RLock()
	wasPresent := s.state.Present
	wasChanged := wasPresent && s.state.LastUID != currentUID
	onDetected := s.OnCardDetected
	onChanged := s.OnCardChanged
	s.stateMutex.RUnlock()

	// Call callbacks outside of lock with panic recovery
	if !wasPresent && onDetected != nil {
		if err := s.safeCallCallback(ctx, onDetected, detectedTag, "OnCardDetected"); err != nil {
			return false, err
		}
	} else if wasChanged && onChanged != nil {
		if err := s.safeCallCallback(ctx, onChanged, detectedTag, "OnCardChanged"); err != nil {
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
