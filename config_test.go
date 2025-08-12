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
	"testing"
	"time"
)

func TestDefaultDeviceConfig(t *testing.T) {
	t.Parallel()
	config := DefaultDeviceConfig()

	if config == nil {
		t.Fatal("DefaultDeviceConfig() returned nil")
	}

	// Verify default timeout
	if config.Timeout != 1*time.Second {
		t.Errorf("Expected Timeout = 1s, got %v", config.Timeout)
	}

	// Verify RetryConfig is set and not nil
	if config.RetryConfig == nil {
		t.Error("RetryConfig should not be nil")
	}

	// Verify RetryConfig has expected defaults
	retryConfig := config.RetryConfig
	if retryConfig.MaxAttempts != 3 {
		t.Errorf("Expected MaxAttempts = 3, got %d", retryConfig.MaxAttempts)
	}
	if retryConfig.InitialBackoff != 10*time.Millisecond {
		t.Errorf("Expected InitialBackoff = 10ms, got %v", retryConfig.InitialBackoff)
	}
	if retryConfig.MaxBackoff != 1*time.Second {
		t.Errorf("Expected MaxBackoff = 1s, got %v", retryConfig.MaxBackoff)
	}
	if retryConfig.BackoffMultiplier != 2.0 {
		t.Errorf("Expected BackoffMultiplier = 2.0, got %f", retryConfig.BackoffMultiplier)
	}
	if retryConfig.Jitter != 0.1 {
		t.Errorf("Expected Jitter = 0.1, got %f", retryConfig.Jitter)
	}
	if retryConfig.RetryTimeout != 5*time.Second {
		t.Errorf("Expected RetryTimeout = 5s, got %v", retryConfig.RetryTimeout)
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	t.Parallel()
	config := DefaultRetryConfig()

	if config == nil {
		t.Fatal("DefaultRetryConfig() returned nil")
	}

	// Test all default values
	tests := []struct {
		got      any
		expected any
		name     string
	}{
		{config.MaxAttempts, 3, "MaxAttempts"},
		{config.InitialBackoff, 10 * time.Millisecond, "InitialBackoff"},
		{config.MaxBackoff, 1 * time.Second, "MaxBackoff"},
		{config.BackoffMultiplier, 2.0, "BackoffMultiplier"},
		{config.Jitter, 0.1, "Jitter"},
		{config.RetryTimeout, 5 * time.Second, "RetryTimeout"},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.got != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestRetryConfigValidation(t *testing.T) {
	t.Parallel()
	tests := getRetryConfigTestCases()

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			validateRetryConfig(t, retryConfigValidation{
				config:        tt.config,
				shouldBeValid: tt.valid,
			})
		})
	}
}

func getRetryConfigTestCases() []struct {
	config *RetryConfig
	name   string
	valid  bool
} {
	return []struct {
		config *RetryConfig
		name   string
		valid  bool
	}{
		{
			name:   "nil config should be handled",
			config: nil,
			valid:  true,
		},
		{
			name: "zero max attempts",
			config: &RetryConfig{
				MaxAttempts: 0,
			},
			valid: true,
		},
		{
			name: "negative max attempts",
			config: &RetryConfig{
				MaxAttempts: -1,
			},
			valid: true,
		},
		{
			name: "valid config",
			config: &RetryConfig{
				MaxAttempts:       5,
				InitialBackoff:    50 * time.Millisecond,
				MaxBackoff:        2 * time.Second,
				BackoffMultiplier: 1.5,
				Jitter:            0.2,
				RetryTimeout:      10 * time.Second,
			},
			valid: true,
		},
		{
			name: "zero timeouts",
			config: &RetryConfig{
				MaxAttempts:       3,
				InitialBackoff:    0,
				MaxBackoff:        0,
				BackoffMultiplier: 2.0,
				Jitter:            0,
				RetryTimeout:      0,
			},
			valid: true,
		},
	}
}

type retryConfigValidation struct {
	config        *RetryConfig
	shouldBeValid bool
}

func validateRetryConfig(t *testing.T, validation retryConfigValidation) {
	defer func() {
		if r := recover(); r != nil {
			if validation.shouldBeValid {
				t.Errorf("Config should be valid but caused panic: %v", r)
			}
		}
	}()

	if validation.config != nil {
		_ = validation.config.MaxAttempts
		_ = validation.config.InitialBackoff
		_ = validation.config.MaxBackoff
	}
}

func TestExponentialBackoff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		attempt     int
		initial     time.Duration
		maxDuration time.Duration
		multiplier  float64
		want        time.Duration
	}{
		{
			name:        "first attempt",
			attempt:     1,
			initial:     10 * time.Millisecond,
			maxDuration: 1 * time.Second,
			multiplier:  2.0,
			want:        10 * time.Millisecond,
		},
		{
			name:        "second attempt",
			attempt:     2,
			initial:     10 * time.Millisecond,
			maxDuration: 1 * time.Second,
			multiplier:  2.0,
			want:        20 * time.Millisecond,
		},
		{
			name:        "third attempt",
			attempt:     3,
			initial:     10 * time.Millisecond,
			maxDuration: 1 * time.Second,
			multiplier:  2.0,
			want:        40 * time.Millisecond,
		},
		{
			name:        "max duration reached",
			attempt:     10,
			initial:     100 * time.Millisecond,
			maxDuration: 500 * time.Millisecond,
			multiplier:  2.0,
			want:        500 * time.Millisecond,
		},
		{
			name:        "zero attempt",
			attempt:     0,
			initial:     50 * time.Millisecond,
			maxDuration: 1 * time.Second,
			multiplier:  2.0,
			want:        50 * time.Millisecond,
		},
		{
			name:        "negative attempt",
			attempt:     -1,
			initial:     50 * time.Millisecond,
			maxDuration: 1 * time.Second,
			multiplier:  2.0,
			want:        50 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ExponentialBackoff(tt.attempt, tt.initial, tt.maxDuration, tt.multiplier)
			if got != tt.want {
				t.Errorf("ExponentialBackoff() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTransportType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tt   TransportType
		str  string
	}{
		{"UART", TransportUART, "uart"},
		{"I2C", TransportI2C, "i2c"},
		{"SPI", TransportSPI, "spi"},
		{"Mock", TransportMock, "mock"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if string(test.tt) != test.str {
				t.Errorf("TransportType %s = %q, want %q", test.name, string(test.tt), test.str)
			}
		})
	}
}

func TestTransportCapability(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cap  TransportCapability
		str  string
	}{
		{"RequiresInSelect", CapabilityRequiresInSelect, "requires_in_select"},
		{"AutoPollNative", CapabilityAutoPollNative, "autopoll_native"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if string(test.cap) != test.str {
				t.Errorf("TransportCapability %s = %q, want %q", test.name, string(test.cap), test.str)
			}
		})
	}
}
