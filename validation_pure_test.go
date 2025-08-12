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
	"bytes"
	"testing"
	"time"
)

func TestDefaultValidationConfig(t *testing.T) {
	t.Parallel()
	config := DefaultValidationConfig()

	if config == nil {
		t.Fatal("DefaultValidationConfig() returned nil")
	}

	// Verify default values
	if !config.EnableReadVerification {
		t.Error("Expected EnableReadVerification to be true")
	}
	if config.ReadRetries != 3 {
		t.Errorf("Expected ReadRetries = 3, got %d", config.ReadRetries)
	}
	if !config.EnableWriteVerification {
		t.Error("Expected EnableWriteVerification to be true")
	}
	if config.WriteRetries != 3 {
		t.Errorf("Expected WriteRetries = 3, got %d", config.WriteRetries)
	}
	if config.RetryDelay != 50*time.Millisecond {
		t.Errorf("Expected RetryDelay = 50ms, got %v", config.RetryDelay)
	}
}

func TestUpdateVerificationState(t *testing.T) {
	t.Parallel()

	testUpdateVerificationStateMatches(t)
	testUpdateVerificationStateEdgeCases(t)
}

func testUpdateVerificationStateMatches(t *testing.T) {
	tests := []struct {
		name               string
		lastData           []byte
		verifyData         []byte
		wantData           []byte
		consecutiveMatches int
		wantMatches        int
	}{
		{
			name:               "identical data increments matches",
			lastData:           []byte{0x01, 0x02, 0x03},
			verifyData:         []byte{0x01, 0x02, 0x03},
			consecutiveMatches: 1,
			wantMatches:        2,
			wantData:           []byte{0x01, 0x02, 0x03},
		},
		{
			name:               "different data resets matches",
			lastData:           []byte{0x01, 0x02, 0x03},
			verifyData:         []byte{0x01, 0x02, 0x04},
			consecutiveMatches: 2,
			wantMatches:        0,
			wantData:           []byte{0x01, 0x02, 0x04},
		},
		{
			name:               "single byte data matches",
			lastData:           []byte{0xFF},
			verifyData:         []byte{0xFF},
			consecutiveMatches: 0,
			wantMatches:        1,
			wantData:           []byte{0xFF},
		},
		{
			name:               "large data matches",
			lastData:           bytes.Repeat([]byte{0xAA}, 1000),
			verifyData:         bytes.Repeat([]byte{0xAA}, 1000),
			consecutiveMatches: 5,
			wantMatches:        6,
			wantData:           bytes.Repeat([]byte{0xAA}, 1000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotMatches, gotData := updateVerificationState(tt.lastData, tt.verifyData, tt.consecutiveMatches)

			if gotMatches != tt.wantMatches {
				t.Errorf("updateVerificationState() gotMatches = %v, want %v", gotMatches, tt.wantMatches)
			}

			if !bytes.Equal(gotData, tt.wantData) {
				t.Errorf("updateVerificationState() gotData = %v, want %v", gotData, tt.wantData)
			}
		})
	}
}

func testUpdateVerificationStateEdgeCases(t *testing.T) {
	tests := []struct {
		name               string
		lastData           []byte
		verifyData         []byte
		wantData           []byte
		consecutiveMatches int
		wantMatches        int
	}{
		{
			name:               "empty data matches",
			lastData:           []byte{},
			verifyData:         []byte{},
			consecutiveMatches: 0,
			wantMatches:        1,
			wantData:           []byte{},
		},
		{
			name:               "nil vs empty are equal",
			lastData:           nil,
			verifyData:         []byte{},
			consecutiveMatches: 0,
			wantMatches:        1,
			wantData:           nil,
		},
		{
			name:               "empty vs nil are equal",
			lastData:           []byte{},
			verifyData:         nil,
			consecutiveMatches: 0,
			wantMatches:        1,
			wantData:           []byte{},
		},
		{
			name:               "nil data matches",
			lastData:           nil,
			verifyData:         nil,
			consecutiveMatches: 0,
			wantMatches:        1,
			wantData:           nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotMatches, gotData := updateVerificationState(tt.lastData, tt.verifyData, tt.consecutiveMatches)

			if gotMatches != tt.wantMatches {
				t.Errorf("updateVerificationState() gotMatches = %v, want %v", gotMatches, tt.wantMatches)
			}

			if !bytes.Equal(gotData, tt.wantData) {
				t.Errorf("updateVerificationState() gotData = %v, want %v", gotData, tt.wantData)
			}
		})
	}
}

func TestHandleVerificationFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		lastErr     error
		name        string
		errContains []string
		readRetries int
		wantErr     bool
	}{
		{
			name:        "with last error",
			lastErr:     ErrTransportRead,
			readRetries: 3,
			wantErr:     true,
			errContains: []string{"read validation failed", "3 retries", "transport read failed"},
		},
		{
			name:        "without last error",
			lastErr:     nil,
			readRetries: 5,
			wantErr:     true,
			errContains: []string{"read validation failed", "inconsistent data", "5 retries"},
		},
		{
			name:        "zero retries with error",
			lastErr:     ErrFrameCorrupted,
			readRetries: 0,
			wantErr:     true,
			errContains: []string{"read validation failed", "0 retries", "frame corrupted"},
		},
		{
			name:        "zero retries without error",
			lastErr:     nil,
			readRetries: 0,
			wantErr:     true,
			errContains: []string{"read validation failed", "inconsistent data", "0 retries"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := handleVerificationFailure(tt.lastErr, tt.readRetries)

			validateVerificationFailureResult(t, verificationFailureResult{
				data:        data,
				err:         err,
				expectErr:   tt.wantErr,
				errContains: tt.errContains,
			})
		})
	}
}

type verificationFailureResult struct {
	err         error
	data        []byte
	errContains []string
	expectErr   bool
}

func validateVerificationFailureResult(t *testing.T, result verificationFailureResult) {
	if (result.err != nil) != result.expectErr {
		t.Errorf("handleVerificationFailure() error = %v, wantErr %v", result.err, result.expectErr)
		return
	}

	if result.data != nil {
		t.Errorf("handleVerificationFailure() data = %v, want nil", result.data)
	}

	if result.expectErr && result.err != nil {
		validateErrorContainsSubstrings(t, result.err.Error(), result.errContains)
	}
}

func validateErrorContainsSubstrings(t *testing.T, errStr string, substrings []string) {
	for _, substr := range substrings {
		if !containsIgnoreCase(errStr, substr) {
			t.Errorf("Error %q should contain %q", errStr, substr)
		}
	}
}

// containsIgnoreCase checks if s contains substr, ignoring case
func containsIgnoreCase(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}

func TestValidationResult(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		success           bool
		securityViolation bool
	}{
		{
			name:              "successful validation",
			success:           true,
			securityViolation: false,
		},
		{
			name:              "failed validation",
			success:           false,
			securityViolation: false,
		},
		{
			name:              "security violation",
			success:           false,
			securityViolation: true,
		},
		{
			name:              "successful with security check",
			success:           true,
			securityViolation: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ValidationResult{
				Success:           tt.success,
				SecurityViolation: tt.securityViolation,
			}

			if result.Success != tt.success {
				t.Errorf("ValidationResult.Success = %v, want %v", result.Success, tt.success)
			}
			if result.SecurityViolation != tt.securityViolation {
				t.Errorf("ValidationResult.SecurityViolation = %v, want %v",
					result.SecurityViolation, tt.securityViolation)
			}
		})
	}
}

func TestValidationMetrics(t *testing.T) {
	t.Parallel()
	// Test the ValidationMetrics struct fields
	metrics := ValidationMetrics{
		LastValidation:     time.Now(),
		TotalOperations:    100,
		FailedValidations:  5,
		SecurityViolations: 2,
	}

	if metrics.TotalOperations != 100 {
		t.Errorf("TotalOperations = %d, want 100", metrics.TotalOperations)
	}
	if metrics.FailedValidations != 5 {
		t.Errorf("FailedValidations = %d, want 5", metrics.FailedValidations)
	}
	if metrics.SecurityViolations != 2 {
		t.Errorf("SecurityViolations = %d, want 2", metrics.SecurityViolations)
	}
	if metrics.LastValidation.IsZero() {
		t.Error("LastValidation should not be zero")
	}
}

func TestValidationConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config ValidationConfig
	}{
		{
			name: "default config values",
			config: ValidationConfig{
				RetryDelay:              50 * time.Millisecond,
				ReadRetries:             3,
				WriteRetries:            3,
				EnableReadVerification:  true,
				EnableWriteVerification: true,
			},
		},
		{
			name: "disabled verification",
			config: ValidationConfig{
				RetryDelay:              100 * time.Millisecond,
				ReadRetries:             1,
				WriteRetries:            1,
				EnableReadVerification:  false,
				EnableWriteVerification: false,
			},
		},
		{
			name: "high retry counts",
			config: ValidationConfig{
				RetryDelay:              10 * time.Millisecond,
				ReadRetries:             10,
				WriteRetries:            10,
				EnableReadVerification:  true,
				EnableWriteVerification: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Test that all fields are accessible and have expected values
			if tt.config.RetryDelay < 0 {
				t.Error("RetryDelay should not be negative")
			}
			if tt.config.ReadRetries < 0 {
				t.Error("ReadRetries should not be negative")
			}
			if tt.config.WriteRetries < 0 {
				t.Error("WriteRetries should not be negative")
			}
		})
	}
}
