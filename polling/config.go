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

import "time"

// SleepRecoveryConfig configures automatic recovery after host sleep/wake
type SleepRecoveryConfig struct {
	// Enabled enables sleep detection and recovery attempts
	Enabled bool

	// TimeDiscontinuityThreshold is the minimum elapsed time beyond the expected
	// poll interval that indicates a sleep occurred. Default: 2 seconds
	TimeDiscontinuityThreshold time.Duration

	// MaxRecoveryAttempts is the number of recovery attempts before
	// treating as a fatal error. Default: 3
	MaxRecoveryAttempts int

	// RecoveryBackoff is the delay between recovery attempts
	RecoveryBackoff time.Duration
}

// DefaultSleepRecoveryConfig returns sensible defaults for sleep recovery
func DefaultSleepRecoveryConfig() SleepRecoveryConfig {
	return SleepRecoveryConfig{
		Enabled:                    true,
		TimeDiscontinuityThreshold: 2 * time.Second,
		MaxRecoveryAttempts:        3,
		RecoveryBackoff:            500 * time.Millisecond,
	}
}

// DetectSleep checks if the elapsed time since last poll indicates a system sleep.
// Returns true if elapsed time exceeds (pollInterval + TimeDiscontinuityThreshold).
func (cfg SleepRecoveryConfig) DetectSleep(elapsed, pollInterval time.Duration) bool {
	if !cfg.Enabled {
		return false
	}
	expectedMax := pollInterval + cfg.TimeDiscontinuityThreshold
	return elapsed > expectedMax
}

// Config holds polling configuration options
type Config struct {
	PollInterval       time.Duration
	CardRemovalTimeout time.Duration
	// HardwareTimeoutRetries controls how long PN532 waits for card detection
	// 0x00 = immediate return, 0x01-0xFE = retry count (~150ms each), 0xFF = infinite
	// Higher values reduce LED blinking frequency but increase detection latency
	HardwareTimeoutRetries byte
	// FeliCa adds a FeliCa poll to each cycle in which the ISO14443A poll
	// finds no tag. It is off by default, and polling is then unchanged.
	//
	// When on, the PN532 is set to give up on a target type after a few
	// activation attempts instead of waiting for a tag, so a cycle can poll
	// both types. If the reader keeps failing the FeliCa poll, the session
	// stops sending it and calls Session.OnFeliCaUnavailable.
	FeliCa bool
	// SleepRecovery configures automatic recovery after host sleep/wake cycles
	SleepRecovery SleepRecoveryConfig
}

// DefaultConfig returns the default polling configuration
func DefaultConfig() *Config {
	return &Config{
		PollInterval:           250 * time.Millisecond,
		CardRemovalTimeout:     600 * time.Millisecond,
		HardwareTimeoutRetries: 0x20, // ~4.8s timeout (32 * 150ms) for reduced LED blinking
		SleepRecovery:          DefaultSleepRecoveryConfig(),
	}
}
