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
	"errors"
	"time"

	"github.com/ZaparooProject/go-pn532"
)

// Operating modes
type Mode int

const (
	ModeComprehensive Mode = iota
)

// Transport type constants for type-safe transport detection
const (
	TransportUART = "uart"
	TransportI2C  = "i2c"
	TransportSPI  = "spi"
)

// Config holds application configuration
type Config struct {
	Mode               Mode
	ConnectTimeout     time.Duration
	DetectTimeout      time.Duration
	PollInterval       time.Duration
	CardRemovalTimeout time.Duration
	Verbose            bool
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Mode:               ModeComprehensive,
		ConnectTimeout:     10 * time.Second,
		DetectTimeout:      30 * time.Second,
		PollInterval:       50 * time.Millisecond,
		CardRemovalTimeout: 300 * time.Millisecond,
		Verbose:            false,
	}
}


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
	LastUID           string
	LastType          string
	TestedUID         string
	Present           bool
	LastSeenTime      time.Time
	RemovalTimer      *time.Timer
	DetectionState    CardDetectionState
	ReadStartTime     time.Time
}

// MonitoringSetup holds monitoring configuration
type MonitoringSetup struct {
	Devices     []*pn532.Device
	ReaderPaths []string
	CardStates  []CardState
}

// ErrNoTagInPoll indicates no tag was detected during polling (not an error condition)
var ErrNoTagInPoll = errors.New("no tag detected in polling cycle")

// TransitionToReading moves to reading state and suspends removal timer
func (cs *CardState) TransitionToReading() {
	cs.DetectionState = StateReading
	cs.ReadStartTime = time.Now()
	if cs.RemovalTimer != nil {
		cs.RemovalTimer.Stop()
		cs.RemovalTimer = nil
	}
}

// TransitionToPostReadGrace moves to post-read grace period with short timeout
func (cs *CardState) TransitionToPostReadGrace(timeout time.Duration, callback func()) {
	cs.DetectionState = StatePostReadGrace
	if cs.RemovalTimer != nil {
		cs.RemovalTimer.Stop()
	}
	// Short grace period after read completion
	cs.RemovalTimer = time.AfterFunc(timeout/2, callback)
}

// TransitionToDetected moves to tag detected state with normal removal timeout
func (cs *CardState) TransitionToDetected(timeout time.Duration, callback func()) {
	cs.DetectionState = StateTagDetected
	cs.LastSeenTime = time.Now()
	if cs.RemovalTimer != nil {
		cs.RemovalTimer.Stop()
	}
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
	if cs.RemovalTimer != nil {
		cs.RemovalTimer.Stop()
		cs.RemovalTimer = nil
	}
}

// CanStartRemovalTimer returns true if the state allows removal timer to run
func (cs *CardState) CanStartRemovalTimer() bool {
	return cs.DetectionState == StateTagDetected || cs.DetectionState == StatePostReadGrace
}
