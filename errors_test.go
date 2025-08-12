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
	"strings"
	"testing"
)

func TestIsRetryable(t *testing.T) {
	t.Parallel()
	tests := getIsRetryableTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsRetryable(tt.err)
			if got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func getIsRetryableTestCases() []struct {
	err  error
	name string
	want bool
} {
	return []struct {
		err  error
		name string
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "transport timeout retryable",
			err:  ErrTransportTimeout,
			want: true,
		},
		{
			name: "transport read retryable",
			err:  ErrTransportRead,
			want: true,
		},
		{
			name: "transport write retryable",
			err:  ErrTransportWrite,
			want: true,
		},
		{
			name: "communication failed retryable",
			err:  ErrCommunicationFailed,
			want: true,
		},
		{
			name: "no ACK retryable",
			err:  ErrNoACK,
			want: true,
		},
		{
			name: "frame corrupted retryable",
			err:  ErrFrameCorrupted,
			want: true,
		},
		{
			name: "checksum mismatch retryable",
			err:  ErrChecksumMismatch,
			want: true,
		},
		{
			name: "device not found not retryable",
			err:  ErrDeviceNotFound,
			want: false,
		},
		{
			name: "tag not found not retryable",
			err:  ErrTagNotFound,
			want: false,
		},
		{
			name: "data too large not retryable",
			err:  ErrDataTooLarge,
			want: false,
		},
		{
			name: "invalid parameter not retryable",
			err:  ErrInvalidParameter,
			want: false,
		},
		{
			name: "wrapped retryable error",
			err:  errors.New("outer: " + ErrTransportTimeout.Error()),
			want: false,
		},
	}
}

func TestIsRetryable_TransportError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		transport *TransportError
		name      string
		want      bool
	}{
		{
			name: "transport error retryable=true",
			transport: &TransportError{
				Err:       errors.New("test error"),
				Op:        "read",
				Port:      "/dev/ttyUSB0",
				Type:      ErrorTypeTransient,
				Retryable: true,
			},
			want: true,
		},
		{
			name: "transport error retryable=false",
			transport: &TransportError{
				Err:       errors.New("test error"),
				Op:        "write",
				Port:      "/dev/ttyUSB0",
				Type:      ErrorTypeTransient,
				Retryable: false,
			},
			want: false,
		},
		{
			name: "transport error with retryable underlying error but retryable=false",
			transport: &TransportError{
				Err:       ErrTransportTimeout,
				Op:        "read",
				Port:      "/dev/ttyUSB0",
				Type:      ErrorTypeTimeout,
				Retryable: false,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsRetryable(tt.transport)
			if got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetErrorType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		name string
		want ErrorType
	}{
		{
			name: "nil error",
			err:  nil,
			want: ErrorTypePermanent,
		},
		{
			name: "transport timeout",
			err:  ErrTransportTimeout,
			want: ErrorTypeTimeout,
		},
		{
			name: "transport read",
			err:  ErrTransportRead,
			want: ErrorTypeTransient,
		},
		{
			name: "transport write",
			err:  ErrTransportWrite,
			want: ErrorTypeTransient,
		},
		{
			name: "communication failed",
			err:  ErrCommunicationFailed,
			want: ErrorTypeTransient,
		},
		{
			name: "no ACK",
			err:  ErrNoACK,
			want: ErrorTypeTransient,
		},
		{
			name: "frame corrupted",
			err:  ErrFrameCorrupted,
			want: ErrorTypeTransient,
		},
		{
			name: "checksum mismatch",
			err:  ErrChecksumMismatch,
			want: ErrorTypeTransient,
		},
		{
			name: "device not found",
			err:  ErrDeviceNotFound,
			want: ErrorTypePermanent,
		},
		{
			name: "tag not found",
			err:  ErrTagNotFound,
			want: ErrorTypePermanent,
		},
		{
			name: "unknown error",
			err:  errors.New("unknown error"),
			want: ErrorTypePermanent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := GetErrorType(tt.err)
			if got != tt.want {
				t.Errorf("GetErrorType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetErrorType_TransportError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		transport *TransportError
		name      string
		want      ErrorType
	}{
		{
			name: "transport error transient",
			transport: &TransportError{
				Err:       errors.New("test error"),
				Op:        "read",
				Port:      "/dev/ttyUSB0",
				Type:      ErrorTypeTransient,
				Retryable: true,
			},
			want: ErrorTypeTransient,
		},
		{
			name: "transport error timeout",
			transport: &TransportError{
				Err:       errors.New("test error"),
				Op:        "read",
				Port:      "/dev/ttyUSB0",
				Type:      ErrorTypeTimeout,
				Retryable: true,
			},
			want: ErrorTypeTimeout,
		},
		{
			name: "transport error permanent",
			transport: &TransportError{
				Err:       errors.New("test error"),
				Op:        "open",
				Port:      "/dev/ttyUSB0",
				Type:      ErrorTypePermanent,
				Retryable: false,
			},
			want: ErrorTypePermanent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := GetErrorType(tt.transport)
			if got != tt.want {
				t.Errorf("GetErrorType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewTransportError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err     error
		name    string
		op      string
		port    string
		errType ErrorType
	}{
		{
			name:    "basic transport error",
			op:      "read",
			port:    "/dev/ttyUSB0",
			err:     errors.New("permission denied"),
			errType: ErrorTypePermanent,
		},
		{
			name:    "empty port",
			op:      "write",
			port:    "",
			err:     errors.New("connection lost"),
			errType: ErrorTypeTransient,
		},
		{
			name:    "timeout error",
			op:      "command",
			port:    "ACR122U",
			err:     ErrTransportTimeout,
			errType: ErrorTypeTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			te := NewTransportError(tt.op, tt.port, tt.err, tt.errType)

			if te.Op != tt.op {
				t.Errorf("Op = %q, want %q", te.Op, tt.op)
			}
			if te.Port != tt.port {
				t.Errorf("Port = %q, want %q", te.Port, tt.port)
			}
			if !errors.Is(te.Err, tt.err) {
				t.Errorf("Err = %v, want %v", te.Err, tt.err)
			}
			if te.Type != tt.errType {
				t.Errorf("Type = %v, want %v", te.Type, tt.errType)
			}
		})
	}
}

func TestTransportError_Error(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		te   *TransportError
		want []string // Substrings that should be present
	}{
		{
			name: "with port",
			te: &TransportError{
				Err:  errors.New("connection failed"),
				Op:   "read",
				Port: "/dev/ttyUSB0",
			},
			want: []string{"read", "/dev/ttyUSB0", "connection failed"},
		},
		{
			name: "without port",
			te: &TransportError{
				Err:  errors.New("device busy"),
				Op:   "write",
				Port: "",
			},
			want: []string{"write", "device busy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.te.Error()
			for _, substr := range tt.want {
				if !strings.Contains(got, substr) {
					t.Errorf("Error() = %q, should contain %q", got, substr)
				}
			}
		})
	}
}

func TestTransportError_Unwrap(t *testing.T) {
	t.Parallel()
	originalErr := errors.New("original error")
	te := &TransportError{
		Err:  originalErr,
		Op:   "test",
		Port: "/dev/test",
	}

	unwrapped := te.Unwrap()
	if !errors.Is(unwrapped, originalErr) {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, originalErr)
	}
}

func TestNewTimeoutError(t *testing.T) {
	t.Parallel()
	te := NewTimeoutError("read", "/dev/ttyUSB0")

	if te.Op != "read" {
		t.Errorf("Op = %q, want %q", te.Op, "read")
	}
	if te.Port != "/dev/ttyUSB0" {
		t.Errorf("Port = %q, want %q", te.Port, "/dev/ttyUSB0")
	}
	if te.Type != ErrorTypeTimeout {
		t.Errorf("Type = %v, want %v", te.Type, ErrorTypeTimeout)
	}
	if !te.Retryable {
		t.Error("Retryable should be true for timeout errors")
	}
}

func TestNewFrameCorruptedError(t *testing.T) {
	t.Parallel()
	te := NewFrameCorruptedError("read", "/dev/ttyUSB0")

	if te.Op != "read" {
		t.Errorf("Op = %q, want %q", te.Op, "read")
	}
	if te.Port != "/dev/ttyUSB0" {
		t.Errorf("Port = %q, want %q", te.Port, "/dev/ttyUSB0")
	}
	if te.Type != ErrorTypeTransient {
		t.Errorf("Type = %v, want %v", te.Type, ErrorTypeTransient)
	}
	if !te.Retryable {
		t.Error("Retryable should be true for frame corrupted errors")
	}
}

func TestNewDataTooLargeError(t *testing.T) {
	t.Parallel()
	te := NewDataTooLargeError("write", "/dev/ttyUSB0")

	if te.Op != "write" {
		t.Errorf("Op = %q, want %q", te.Op, "write")
	}
	if te.Port != "/dev/ttyUSB0" {
		t.Errorf("Port = %q, want %q", te.Port, "/dev/ttyUSB0")
	}
	if te.Type != ErrorTypePermanent {
		t.Errorf("Type = %v, want %v", te.Type, ErrorTypePermanent)
	}
	if te.Retryable {
		t.Error("Retryable should be false for data too large errors")
	}
}
