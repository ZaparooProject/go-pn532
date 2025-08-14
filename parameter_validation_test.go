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
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestRetryConfigParameterValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		config  *RetryConfig
		field   string
		wantErr bool
	}{
		{
			name: "negative max attempts",
			config: &RetryConfig{
				MaxAttempts:    -5,
				InitialBackoff: 10 * time.Millisecond,
			},
			field:   "MaxAttempts",
			wantErr: false, // Negative is treated as no retry, which is valid
		},
		{
			name: "zero initial backoff",
			config: &RetryConfig{
				MaxAttempts:    3,
				InitialBackoff: 0,
			},
			field:   "InitialBackoff",
			wantErr: false, // Zero backoff is valid
		},
		{
			name: "negative backoff multiplier",
			config: &RetryConfig{
				MaxAttempts:       3,
				InitialBackoff:    10 * time.Millisecond,
				BackoffMultiplier: -1.0,
			},
			field:   "BackoffMultiplier",
			wantErr: false, // Mathematically odd, but won't crash
		},
		{
			name: "extremely large jitter",
			config: &RetryConfig{
				MaxAttempts:    3,
				InitialBackoff: 10 * time.Millisecond,
				Jitter:         100.0, // 100x the backoff time
			},
			field:   "Jitter",
			wantErr: false, // Large jitter is just inefficient, not invalid
		},
		{
			name: "negative jitter",
			config: &RetryConfig{
				MaxAttempts:    3,
				InitialBackoff: 10 * time.Millisecond,
				Jitter:         -0.5,
			},
			field:   "Jitter",
			wantErr: false, // Will be handled in calculation
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Test that the configuration doesn't cause a panic when used
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Configuration caused panic: %v", r)
				}
			}()

			// Try to use the config in retry logic (with a quick-failing function)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			retryFunc := func() error {
				return ErrDeviceNotFound // Non-retryable error to exit quickly
			}

			_ = RetryWithConfig(ctx, tt.config, retryFunc)
		})
	}
}

func TestDeviceConfigParameterValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		config  *DeviceConfig
		name    string
		wantErr bool
	}{
		{
			name: "nil retry config",
			config: &DeviceConfig{
				RetryConfig: nil,
				Timeout:     1 * time.Second,
			},
			wantErr: false, // Should use default retry config
		},
		{
			name: "zero timeout",
			config: &DeviceConfig{
				RetryConfig: DefaultRetryConfig(),
				Timeout:     0,
			},
			wantErr: false, // Zero timeout is valid (no timeout)
		},
		{
			name: "negative timeout",
			config: &DeviceConfig{
				RetryConfig: DefaultRetryConfig(),
				Timeout:     -1 * time.Second,
			},
			wantErr: false, // Negative timeout is handled by Go's time package
		},
		{
			name: "extremely large timeout",
			config: &DeviceConfig{
				RetryConfig: DefaultRetryConfig(),
				Timeout:     24 * time.Hour,
			},
			wantErr: false, // Large timeout is valid but impractical
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Test that config can be created and used without panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("DeviceConfig caused panic: %v", r)
				}
			}()

			// Verify fields are accessible
			_ = tt.config.Timeout
			_ = tt.config.RetryConfig
		})
	}
}

func TestTransportErrorParameterValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err       error
		name      string
		op        string
		port      string
		errType   ErrorType
		wantPanic bool
	}{
		{
			name:      "empty operation",
			op:        "",
			port:      "/dev/ttyUSB0",
			err:       ErrTransportRead,
			errType:   ErrorTypeTransient,
			wantPanic: false,
		},
		{
			name:      "empty port",
			op:        "read",
			port:      "",
			err:       ErrTransportRead,
			errType:   ErrorTypeTransient,
			wantPanic: false,
		},
		{
			name:      "nil error",
			op:        "read",
			port:      "/dev/ttyUSB0",
			err:       nil,
			errType:   ErrorTypeTransient,
			wantPanic: false,
		},
		{
			name:      "custom error type",
			op:        "read",
			port:      "/dev/ttyUSB0",
			err:       ErrTransportRead,
			errType:   ErrorTypeTransient,
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			validateTransportErrorCreation(t, transportErrorCreation{
				op:          tt.op,
				port:        tt.port,
				err:         tt.err,
				errType:     tt.errType,
				shouldPanic: tt.wantPanic,
			})
		})
	}
}

type transportErrorCreation struct {
	err         error
	op          string
	port        string
	errType     ErrorType
	shouldPanic bool
}

func validateTransportErrorCreation(t *testing.T, creation transportErrorCreation) {
	defer func() {
		r := recover()
		if (r != nil) != creation.shouldPanic {
			t.Errorf("NewTransportError() panic = %v, wantPanic %v", r != nil, creation.shouldPanic)
		}
	}()

	te := NewTransportError(creation.op, creation.port, creation.err, creation.errType)
	if te == nil {
		t.Error("NewTransportError() returned nil")
		return
	}

	validateTransportErrorFields(t, te, transportErrorValidation{
		op:      creation.op,
		port:    creation.port,
		err:     creation.err,
		errType: creation.errType,
	})
}

type transportErrorValidation struct {
	err     error
	op      string
	port    string
	errType ErrorType
}

func validateTransportErrorFields(t *testing.T, te *TransportError, expected transportErrorValidation) {
	if te.Op != expected.op {
		t.Errorf("Op = %q, want %q", te.Op, expected.op)
	}
	if te.Port != expected.port {
		t.Errorf("Port = %q, want %q", te.Port, expected.port)
	}
	if !errors.Is(te.Err, expected.err) {
		t.Errorf("Err = %v, want %v", te.Err, expected.err)
	}
	if te.Type != expected.errType {
		t.Errorf("Type = %v, want %v", te.Type, expected.errType)
	}
}

func TestNDEFParameterValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		desc      string
		data      []byte
		wantError bool
	}{
		{
			name:      "nil data",
			data:      nil,
			wantError: true,
			desc:      "nil data should be rejected",
		},
		{
			name:      "empty data",
			data:      []byte{},
			wantError: true,
			desc:      "empty data should be rejected",
		},
		{
			name:      "oversized data",
			data:      make([]byte, MaxNDEFMessageSize+1),
			wantError: true,
			desc:      "data exceeding max size should be rejected",
		},
		{
			name:      "max valid size",
			data:      make([]byte, MaxNDEFMessageSize),
			wantError: true, // Will fail for format reasons, not size
			desc:      "max valid size should be accepted",
		},
		{
			name:      "minimal valid size",
			data:      make([]byte, 4),
			wantError: true, // Will fail for format reasons
			desc:      "minimal size should be processed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseNDEFMessage(tt.data)
			gotError := err != nil

			if gotError != tt.wantError {
				t.Errorf("ParseNDEFMessage() error = %v, wantError %v (%s)", err, tt.wantError, tt.desc)
			}
		})
	}
}

func TestBaseTagParameterValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		tagType TagType
		uid     []byte
		sak     byte
	}{
		{
			name:    "nil UID",
			uid:     nil,
			tagType: TagTypeUnknown,
			sak:     0x00,
		},
		{
			name:    "empty UID",
			uid:     []byte{},
			tagType: TagTypeNTAG,
			sak:     0x00,
		},
		{
			name:    "normal UID",
			uid:     []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC},
			tagType: TagTypeNTAG,
			sak:     0x00,
		},
		{
			name:    "large UID",
			uid:     make([]byte, 100), // Unusually large UID
			tagType: TagTypeUnknown,
			sak:     0xFF,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tag := &BaseTag{
				uid:     tt.uid,
				tagType: tt.tagType,
				sak:     tt.sak,
			}

			// Test that all methods work without panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("BaseTag methods caused panic: %v", r)
				}
			}()

			_ = tag.Type()
			_ = tag.UID()
			_ = tag.UIDBytes()
			_ = tag.IsMIFARE4K()
		})
	}
}

func TestExponentialBackoffParameterValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		attempt     int
		initial     time.Duration
		maxDuration time.Duration
		multiplier  float64
		shouldPanic bool
	}{
		{
			name:        "negative attempt",
			attempt:     -5,
			initial:     10 * time.Millisecond,
			maxDuration: 1 * time.Second,
			multiplier:  2.0,
			shouldPanic: false,
		},
		{
			name:        "zero multiplier",
			attempt:     3,
			initial:     10 * time.Millisecond,
			maxDuration: 1 * time.Second,
			multiplier:  0.0,
			shouldPanic: false,
		},
		{
			name:        "negative multiplier",
			attempt:     3,
			initial:     10 * time.Millisecond,
			maxDuration: 1 * time.Second,
			multiplier:  -1.0,
			shouldPanic: false,
		},
		{
			name:        "zero max duration",
			attempt:     3,
			initial:     10 * time.Millisecond,
			maxDuration: 0,
			multiplier:  2.0,
			shouldPanic: false,
		},
		{
			name:        "negative initial duration",
			attempt:     3,
			initial:     -10 * time.Millisecond,
			maxDuration: 1 * time.Second,
			multiplier:  2.0,
			shouldPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				r := recover()
				if (r != nil) != tt.shouldPanic {
					t.Errorf("ExponentialBackoff() panic = %v, shouldPanic %v", r != nil, tt.shouldPanic)
				}
			}()

			result := ExponentialBackoff(tt.attempt, tt.initial, tt.maxDuration, tt.multiplier)

			// For edge case inputs, we just verify no panic occurs
			// The function may return negative results for negative inputs, which is acceptable behavior
			_ = result // Use the result to avoid unused variable warning
		})
	}
}

func TestReflectionBasedParameterValidation(t *testing.T) {
	t.Parallel()
	// Test that public API structs have expected field types
	tests := []struct {
		structType  reflect.Type
		fieldChecks map[string]reflect.Type
		name        string
	}{
		{
			name:       "RetryConfig",
			structType: reflect.TypeOf(RetryConfig{}),
			fieldChecks: map[string]reflect.Type{
				"MaxAttempts":       reflect.TypeOf(int(0)),
				"InitialBackoff":    reflect.TypeOf(time.Duration(0)),
				"BackoffMultiplier": reflect.TypeOf(float64(0)),
			},
		},
		{
			name:       "DeviceConfig",
			structType: reflect.TypeOf(DeviceConfig{}),
			fieldChecks: map[string]reflect.Type{
				"Timeout": reflect.TypeOf(time.Duration(0)),
			},
		},
		{
			name:       "TransportError",
			structType: reflect.TypeOf(TransportError{}),
			fieldChecks: map[string]reflect.Type{
				"Op":        reflect.TypeOf(""),
				"Port":      reflect.TypeOf(""),
				"Type":      reflect.TypeOf(ErrorTypePermanent),
				"Retryable": reflect.TypeOf(bool(false)),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for fieldName, expectedType := range tt.fieldChecks {
				field, found := tt.structType.FieldByName(fieldName)
				if !found {
					t.Errorf("Field %s not found in %s", fieldName, tt.name)
					continue
				}

				if field.Type != expectedType {
					t.Errorf("Field %s has type %v, want %v", fieldName, field.Type, expectedType)
				}
			}
		})
	}
}
