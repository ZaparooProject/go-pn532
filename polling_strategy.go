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

package pn532

import (
	"time"
)

// PollStrategy represents the polling strategy to use for tag detection
type PollStrategy string

const (
	// PollStrategyAuto automatically selects the best polling strategy
	// Currently defaults to InListPassiveTarget for better compatibility
	PollStrategyAuto PollStrategy = "auto"

	// PollStrategyAutoPoll uses the PN532's InAutoPoll command for continuous polling
	// Available for explicit selection but not used as default
	PollStrategyAutoPoll PollStrategy = "autopoll"

	// PollStrategyLegacy uses the traditional InListPassiveTarget approach
	// This is the preferred default strategy for reliable tag detection
	PollStrategyLegacy PollStrategy = "legacy"

	// PollStrategyManual disables automatic polling and requires explicit calls
	// This gives applications full control over when and how polling occurs
	PollStrategyManual PollStrategy = "manual"
)

// ContinuousPollConfig contains configuration for continuous polling operations
type ContinuousPollConfig struct {
	Strategy          PollStrategy
	TargetTypes       []AutoPollTarget
	RetryDelay        time.Duration
	MaxRetries        int
	PollCount         byte
	PollPeriod        byte
	RetryOnFailure    bool
	CompatibilityMode bool
}

// DefaultContinuousPollConfig returns a default continuous polling configuration
func DefaultContinuousPollConfig() *ContinuousPollConfig {
	return &ContinuousPollConfig{
		Strategy:   PollStrategyLegacy, // Use InListPassiveTarget as default
		PollCount:  0xFF, // Endless polling
		PollPeriod: 2,    // 300ms (2 * 150ms)
		TargetTypes: []AutoPollTarget{
			AutoPollGeneric106kbps, // ISO14443-4A, MIFARE, DEP
			AutoPollFeliCa212,      // FeliCa 212 kbps
			AutoPollFeliCa424,      // FeliCa 424 kbps
		},
		RetryOnFailure:    true,
		RetryDelay:        500 * time.Millisecond,
		MaxRetries:        3,
		CompatibilityMode: false,
	}
}

// Validate checks if the configuration is valid
func (c *ContinuousPollConfig) Validate() error {
	if c.PollPeriod < 1 || c.PollPeriod > 15 {
		return ErrInvalidParameter
	}

	if c.Strategy == PollStrategyAutoPoll && len(c.TargetTypes) == 0 {
		return ErrInvalidParameter
	}

	if c.RetryDelay < 0 {
		return ErrInvalidParameter
	}

	if c.MaxRetries < 0 {
		return ErrInvalidParameter
	}

	return nil
}

// Clone creates a deep copy of the configuration
func (c *ContinuousPollConfig) Clone() *ContinuousPollConfig {
	clone := *c
	// Deep copy the target types slice
	if len(c.TargetTypes) > 0 {
		clone.TargetTypes = make([]AutoPollTarget, len(c.TargetTypes))
		copy(clone.TargetTypes, c.TargetTypes)
	}
	return &clone
}

// pollStrategyState tracks the current state of polling strategy selection
type pollStrategyState struct {
	lastFailure           time.Time
	config                *ContinuousPollConfig
	currentStrategy       PollStrategy
	failureCount          int
	autoPollSupported     bool
	legacyFallbackEnabled bool
}

// newPollStrategyState creates a new polling strategy state
func newPollStrategyState(config *ContinuousPollConfig) *pollStrategyState {
	if config == nil {
		config = DefaultContinuousPollConfig()
	}

	return &pollStrategyState{
		currentStrategy:       config.Strategy,
		failureCount:          0,
		autoPollSupported:     false,
		legacyFallbackEnabled: false,
		config:                config.Clone(),
	}
}

// updateSupport updates the support flags based on transport capabilities
type pollSupport struct {
	hasAutoPollNative bool
}

func (s *pollStrategyState) updateSupport(support pollSupport) {
	s.autoPollSupported = support.hasAutoPollNative

	// If auto strategy is selected, choose the best available strategy
	if s.currentStrategy == PollStrategyAuto {
		// Default to InListPassive strategy for better compatibility
		// InAutoPoll remains available for explicit selection
		s.currentStrategy = PollStrategyLegacy
	}
}

// recordFailure records a polling failure and updates strategy if needed
func (s *pollStrategyState) recordFailure() {
	s.failureCount++
	s.lastFailure = time.Now()

	// Check if we should switch strategies
	if s.config.RetryOnFailure &&
		s.currentStrategy == PollStrategyAutoPoll &&
		(s.config.MaxRetries == 0 || s.failureCount >= s.config.MaxRetries) {
		s.legacyFallbackEnabled = true
		s.currentStrategy = PollStrategyLegacy
	}
}

// recordSuccess records a successful polling operation
func (s *pollStrategyState) recordSuccess() {
	s.failureCount = 0
}

// shouldRetry determines if a retry should be attempted
func (s *pollStrategyState) shouldRetry() bool {
	if !s.config.RetryOnFailure {
		return false
	}

	if s.config.MaxRetries > 0 && s.failureCount >= s.config.MaxRetries {
		return false
	}

	// Check retry delay
	if !s.lastFailure.IsZero() && time.Since(s.lastFailure) < s.config.RetryDelay {
		return false
	}

	return true
}

// getCurrentStrategy returns the current active polling strategy
func (s *pollStrategyState) getCurrentStrategy() PollStrategy {
	return s.currentStrategy
}
