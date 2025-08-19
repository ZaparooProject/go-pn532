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
	"errors"
	"time"
)

// CardDetectionState represents the finite state machine for card detection
type CardDetectionState int

const (
	StateIdle CardDetectionState = iota
	StateTagDetected
	StateReading
	StatePostReadGrace
)

// CardState tracks the state of a card on a reader
type CardState struct {
	LastSeenTime   time.Time
	ReadStartTime  time.Time
	RemovalTimer   *time.Timer
	LastUID        string
	LastType       string
	TestedUID      string
	DetectionState CardDetectionState
	Present        bool
}

// ErrNoTagInPoll indicates no tag was detected during polling (not an error condition)
var ErrNoTagInPoll = errors.New("no tag detected in polling cycle")

// safeTimerStop safely stops a timer and drains its channel to prevent resource leaks
func safeTimerStop(timer *time.Timer) {
	if timer != nil {
		// Stop the timer first
		stopped := timer.Stop()
		// If Stop() returned false, the timer already fired and the value was sent to C
		// In that case, we need to drain the channel to prevent blocking
		if !stopped {
			select {
			case <-timer.C:
				// Timer fired, drained the channel
			default:
				// Timer was already drained or never fired
			}
		}
	}
}

// TransitionToReading moves to reading state and suspends removal timer
func (cs *CardState) TransitionToReading() {
	cs.DetectionState = StateReading
	cs.ReadStartTime = time.Now()
	safeTimerStop(cs.RemovalTimer)
	cs.RemovalTimer = nil
}

// TransitionToPostReadGrace moves to post-read grace period with short timeout
func (cs *CardState) TransitionToPostReadGrace(timeout time.Duration, callback func()) {
	cs.DetectionState = StatePostReadGrace
	safeTimerStop(cs.RemovalTimer)
	// Short grace period after read completion
	cs.RemovalTimer = time.AfterFunc(timeout/2, callback)
}

// TransitionToDetected moves to tag detected state with normal removal timeout
func (cs *CardState) TransitionToDetected(timeout time.Duration, callback func()) {
	cs.DetectionState = StateTagDetected
	cs.LastSeenTime = time.Now()
	safeTimerStop(cs.RemovalTimer)
	cs.RemovalTimer = time.AfterFunc(timeout, callback)
}

// TransitionToIdle resets to idle state
func (cs *CardState) TransitionToIdle() {
	cs.DetectionState = StateIdle
	cs.Present = false
	cs.LastUID = ""
	cs.LastType = ""
	cs.TestedUID = ""
	cs.LastSeenTime = time.Time{}
	cs.ReadStartTime = time.Time{}
	safeTimerStop(cs.RemovalTimer)
	cs.RemovalTimer = nil
}

// CanStartRemovalTimer returns true if the state allows removal timer to run
func (cs *CardState) CanStartRemovalTimer() bool {
	return cs.DetectionState == StateTagDetected || cs.DetectionState == StatePostReadGrace
}
