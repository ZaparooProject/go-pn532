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
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorWrapping(t *testing.T) {
	t.Parallel()
	baseErr := ErrTransportTimeout

	tests := []struct {
		wrapFunc      func(error) error
		name          string
		wantType      ErrorType
		wantRetryable bool
	}{
		{
			name: "fmt.Errorf wrapping",
			wrapFunc: func(err error) error {
				return fmt.Errorf("operation failed: %w", err)
			},
			wantRetryable: true,
			wantType:      ErrorTypeTimeout,
		},
		{
			name: "double wrapping",
			wrapFunc: func(err error) error {
				wrapped := fmt.Errorf("inner error: %w", err)
				return fmt.Errorf("outer error: %w", wrapped)
			},
			wantRetryable: true,
			wantType:      ErrorTypeTimeout,
		},
		{
			name: "custom wrapper",
			wrapFunc: func(err error) error {
				return &customError{msg: "custom wrapper", cause: err}
			},
			wantRetryable: true, // Should still detect wrapped retryable error
			wantType:      ErrorTypeTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wrappedErr := tt.wrapFunc(baseErr)

			// Test retryable detection
			gotRetryable := IsRetryable(wrappedErr)
			if gotRetryable != tt.wantRetryable {
				t.Errorf("IsRetryable() = %v, want %v", gotRetryable, tt.wantRetryable)
			}

			// Test error type detection
			gotType := GetErrorType(wrappedErr)
			if gotType != tt.wantType {
				t.Errorf("GetErrorType() = %v, want %v", gotType, tt.wantType)
			}

			// Test that original error is still accessible
			if !errors.Is(wrappedErr, baseErr) {
				t.Error("errors.Is() = false, want true for base error")
			}
		})
	}
}

type customError struct {
	cause error
	msg   string
}

func (e *customError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.msg, e.cause)
	}
	return e.msg
}

func (e *customError) Unwrap() error {
	return e.cause
}

func TestErrorUnwrapping(t *testing.T) {
	t.Parallel()
	original := ErrFrameCorrupted
	wrapped := fmt.Errorf("context: %w", original)
	doubleWrapped := fmt.Errorf("more context: %w", wrapped)

	tests := []struct {
		err      error
		expected error
		name     string
	}{
		{
			name:     "unwrap once",
			err:      wrapped,
			expected: original,
		},
		{
			name:     "unwrap twice",
			err:      doubleWrapped,
			expected: original, // Should find the original through chain
		},
		{
			name:     "no wrapping",
			err:      original,
			expected: original,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Test errors.Is traverses the chain
			if !errors.Is(tt.err, tt.expected) {
				t.Error("errors.Is() = false, want true")
			}
		})
	}
}

func TestTransportErrorWrapping(t *testing.T) {
	t.Parallel()
	baseErr := errors.New("connection refused")
	transportErr := &TransportError{
		Err:       baseErr,
		Op:        "connect",
		Port:      "/dev/ttyUSB0",
		Type:      ErrorTypePermanent,
		Retryable: false,
	}

	wrappedErr := fmt.Errorf("device initialization failed: %w", transportErr)

	// Test that transport error is still detectable
	var te *TransportError
	if !errors.As(wrappedErr, &te) {
		t.Error("errors.As() failed to find TransportError")
	}

	// Test transport error properties are preserved
	if te.Op != "connect" {
		t.Errorf("Op = %q, want %q", te.Op, "connect")
	}
	if te.Port != "/dev/ttyUSB0" {
		t.Errorf("Port = %q, want %q", te.Port, "/dev/ttyUSB0")
	}
	if te.Type != ErrorTypePermanent {
		t.Errorf("Type = %v, want %v", te.Type, ErrorTypePermanent)
	}

	// Test that the original base error is still accessible
	if !errors.Is(wrappedErr, baseErr) {
		t.Error("errors.Is() failed to find base error")
	}
}

func TestErrorChainRetryability(t *testing.T) {
	t.Parallel()
	tests := []struct {
		errorChain    func() error
		name          string
		wantRetryable bool
	}{
		{
			name: "retryable at root",
			errorChain: func() error {
				return fmt.Errorf("context: %w", ErrTransportRead)
			},
			wantRetryable: true,
		},
		{
			name: "non-retryable at root",
			errorChain: func() error {
				return fmt.Errorf("context: %w", ErrDeviceNotFound)
			},
			wantRetryable: false,
		},
		{
			name: "mixed chain with retryable",
			errorChain: func() error {
				base := ErrNoACK
				wrapped := fmt.Errorf("command failed: %w", base)
				return fmt.Errorf("operation failed: %w", wrapped)
			},
			wantRetryable: true,
		},
		{
			name: "transport error in chain",
			errorChain: func() error {
				transportErr := &TransportError{
					Err:       ErrTransportTimeout,
					Op:        "read",
					Port:      "/dev/ttyUSB0",
					Type:      ErrorTypeTimeout,
					Retryable: true,
				}
				return fmt.Errorf("device error: %w", transportErr)
			},
			wantRetryable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.errorChain()
			got := IsRetryable(err)
			if got != tt.wantRetryable {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.wantRetryable)
			}
		})
	}
}

func TestErrorTypeChainDetection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		errorFunc func() error
		name      string
		wantType  ErrorType
	}{
		{
			name: "timeout in chain",
			errorFunc: func() error {
				return fmt.Errorf("operation timeout: %w", ErrTransportTimeout)
			},
			wantType: ErrorTypeTimeout,
		},
		{
			name: "transient in chain",
			errorFunc: func() error {
				base := ErrCommunicationFailed
				return fmt.Errorf("retry failed: %w", base)
			},
			wantType: ErrorTypeTransient,
		},
		{
			name: "permanent in chain",
			errorFunc: func() error {
				return fmt.Errorf("device missing: %w", ErrDeviceNotFound)
			},
			wantType: ErrorTypePermanent,
		},
		{
			name: "transport error type preserved",
			errorFunc: func() error {
				te := &TransportError{
					Type:      ErrorTypeTransient,
					Retryable: true,
				}
				return fmt.Errorf("wrapped transport: %w", te)
			},
			wantType: ErrorTypeTransient,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.errorFunc()
			got := GetErrorType(err)
			if got != tt.wantType {
				t.Errorf("GetErrorType() = %v, want %v", got, tt.wantType)
			}
		})
	}
}

func TestErrorStringPropagation(t *testing.T) {
	t.Parallel()
	baseErr := ErrTransportRead
	contextErr := fmt.Errorf("failed to read from device: %w", baseErr)
	outerErr := fmt.Errorf("tag detection failed: %w", contextErr)

	errorStr := outerErr.Error()

	// Check that all context is preserved in the error string
	expectedParts := []string{
		"tag detection failed",
		"failed to read from device",
		"transport read failed",
	}

	for _, part := range expectedParts {
		if !strings.Contains(errorStr, part) {
			t.Errorf("Error string %q should contain %q", errorStr, part)
		}
	}
}

func TestComplexErrorScenarios(t *testing.T) {
	t.Parallel()

	tests := getComplexErrorTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.buildError()

			validateErrorExpectations(t, err, tt.expectIs)
			logErrorTypes(t, err)
			validateErrorProperties(t, errorProperties{
				err:       err,
				retryable: tt.retryable,
				errorType: tt.errorType,
			})
		})
	}
}

func getComplexErrorTestCases() []struct {
	name       string
	buildError func() error
	expectIs   []error
	expectAs   []any
	retryable  bool
	errorType  ErrorType
} {
	return []struct {
		name       string
		buildError func() error
		expectIs   []error
		expectAs   []any
		retryable  bool
		errorType  ErrorType
	}{
		{
			name: "transport error wrapped multiple times",
			buildError: func() error {
				te := NewTimeoutError("read", "/dev/ttyUSB0")
				wrapped := fmt.Errorf("command timeout: %w", te)
				return fmt.Errorf("device communication failed: %w", wrapped)
			},
			expectIs:  []error{},
			expectAs:  []any{},
			retryable: true,
			errorType: ErrorTypeTimeout,
		},
		{
			name: "custom error with transport error",
			buildError: func() error {
				te := NewFrameCorruptedError("read", "/dev/ttyUSB0")
				custom := &customError{msg: "custom error", cause: te}
				return fmt.Errorf("operation failed: %w", custom)
			},
			expectIs:  []error{},
			expectAs:  []any{},
			retryable: true,
			errorType: ErrorTypeTransient,
		},
	}
}

func validateErrorExpectations(t *testing.T, err error, expectedErrors []error) {
	for _, expectedErr := range expectedErrors {
		if !errors.Is(err, expectedErr) {
			t.Errorf("errors.Is() = false for %v", expectedErr)
		}
	}
}

func logErrorTypes(t *testing.T, err error) {
	var te *TransportError
	if errors.As(err, &te) {
		t.Logf("Found TransportError: %v", te)
	}

	var ce *customError
	if errors.As(err, &ce) {
		t.Logf("Found customError: %v", ce)
	}
}

type errorProperties struct {
	err       error
	retryable bool
	errorType ErrorType
}

func validateErrorProperties(t *testing.T, props errorProperties) {
	if IsRetryable(props.err) != props.retryable {
		t.Errorf("IsRetryable() = %v, want %v", IsRetryable(props.err), props.retryable)
	}

	if GetErrorType(props.err) != props.errorType {
		t.Errorf("GetErrorType() = %v, want %v", GetErrorType(props.err), props.errorType)
	}
}
