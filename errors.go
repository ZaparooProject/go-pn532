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
)

// Error categories for better error handling and retry logic
var (
	// Transport errors - potentially retryable
	ErrTransportTimeout  = errors.New("transport timeout")
	ErrTransportWrite    = errors.New("transport write failed")
	ErrTransportRead     = errors.New("transport read failed")
	ErrTransportClosed   = errors.New("transport is closed")
	ErrTransportNotReady = errors.New("transport not ready")

	// Communication errors - potentially retryable
	ErrCommunicationFailed = errors.New("communication failed")
	ErrNoACK               = errors.New("no ACK received")
	ErrNACKReceived        = errors.New("NACK received")
	ErrFrameCorrupted      = errors.New("frame corrupted")
	ErrChecksumMismatch    = errors.New("checksum mismatch")

	// Device errors - generally not retryable
	ErrDeviceNotFound     = errors.New("device not found")
	ErrDeviceNotSupported = errors.New("device not supported")
	ErrCommandFailed      = errors.New("command execution failed")
	ErrInvalidResponse    = errors.New("invalid response format")

	// Tag errors - generally not retryable
	ErrTagNotFound    = errors.New("tag not found")
	ErrTagAuthFailed  = errors.New("tag authentication failed")
	ErrTagReadFailed  = errors.New("tag read failed")
	ErrTagWriteFailed = errors.New("tag write failed")
	ErrTagUnsupported = errors.New("tag type not supported")

	// Data errors - not retryable
	ErrInvalidParameter = errors.New("invalid parameter")
	ErrDataTooLarge     = errors.New("data too large")
	ErrInvalidFormat    = errors.New("invalid data format")
)

// ErrorType represents the category of error for retry logic
type ErrorType int

const (
	// ErrorTypeTransient indicates a potentially retryable error
	ErrorTypeTransient ErrorType = iota
	// ErrorTypePermanent indicates a non-retryable error
	ErrorTypePermanent
	// ErrorTypeTimeout indicates a timeout error (special handling)
	ErrorTypeTimeout
)

// TransportError wraps transport-level errors with additional context
type TransportError struct {
	Err       error     // Underlying error
	Op        string    // Operation that failed
	Port      string    // Port or device identifier
	Type      ErrorType // Error category
	Retryable bool      // Whether the error is retryable
}

func (e *TransportError) Error() string {
	if e.Port != "" {
		return fmt.Sprintf("%s %s: %v", e.Op, e.Port, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Op, e.Err)
}

func (e *TransportError) Unwrap() error {
	return e.Err
}

// IsRetryable returns true if the error is potentially retryable
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	var te *TransportError
	if errors.As(err, &te) {
		return te.Retryable
	}

	// Check for known retryable errors
	switch {
	case errors.Is(err, ErrTransportTimeout),
		errors.Is(err, ErrTransportRead),
		errors.Is(err, ErrTransportWrite),
		errors.Is(err, ErrCommunicationFailed),
		errors.Is(err, ErrNoACK),
		errors.Is(err, ErrFrameCorrupted),
		errors.Is(err, ErrChecksumMismatch):
		return true
	default:
		return false
	}
}

// GetErrorType categorizes an error
func GetErrorType(err error) ErrorType {
	if err == nil {
		return ErrorTypePermanent
	}

	var te *TransportError
	if errors.As(err, &te) {
		return te.Type
	}

	// Categorize known errors
	switch {
	case errors.Is(err, ErrTransportTimeout):
		return ErrorTypeTimeout
	case IsRetryable(err):
		return ErrorTypeTransient
	default:
		return ErrorTypePermanent
	}
}

// Error constructors for consistent error creation

// NewTransportError creates a standard transport error with consistent formatting
func NewTransportError(op, port string, err error, errType ErrorType) *TransportError {
	return &TransportError{
		Op:        op,
		Port:      port,
		Err:       err,
		Type:      errType,
		Retryable: errType == ErrorTypeTransient || errType == ErrorTypeTimeout,
	}
}

// NewTimeoutError creates a timeout error for transport operations
func NewTimeoutError(op, port string) *TransportError {
	return NewTransportError(op, port, ErrTransportTimeout, ErrorTypeTimeout)
}

// NewFrameCorruptedError creates a frame corruption error
func NewFrameCorruptedError(op, port string) *TransportError {
	return NewTransportError(op, port, ErrFrameCorrupted, ErrorTypeTransient)
}

// NewDataTooLargeError creates a data too large error (permanent)
func NewDataTooLargeError(op, port string) *TransportError {
	return NewTransportError(op, port, ErrDataTooLarge, ErrorTypePermanent)
}

// NewTransportWriteError creates a write error (transient)
func NewTransportWriteError(op, port string) *TransportError {
	return NewTransportError(op, port, ErrTransportWrite, ErrorTypeTransient)
}

// NewTransportReadError creates a read error (transient)
func NewTransportReadError(op, port string) *TransportError {
	return NewTransportError(op, port, ErrTransportRead, ErrorTypeTransient)
}

// NewNoACKError creates a "no ACK received" error (timeout)
func NewNoACKError(op, port string) *TransportError {
	return NewTransportError(op, port, ErrNoACK, ErrorTypeTimeout)
}

// NewNACKReceivedError creates a "NACK received" error (transient)
func NewNACKReceivedError(op, port string) *TransportError {
	return NewTransportError(op, port, ErrNACKReceived, ErrorTypeTransient)
}

// NewInvalidResponseError creates an invalid response error (permanent)
func NewInvalidResponseError(op, port string) *TransportError {
	return NewTransportError(op, port, ErrInvalidResponse, ErrorTypePermanent)
}

// NewChecksumMismatchError creates a checksum mismatch error (transient)
func NewChecksumMismatchError(op, port string) *TransportError {
	return NewTransportError(op, port, ErrChecksumMismatch, ErrorTypeTransient)
}

// NewTransportNotReadyError creates a transport not ready error (timeout)
func NewTransportNotReadyError(op, port string) *TransportError {
	return NewTransportError(op, port, ErrTransportNotReady, ErrorTypeTimeout)
}
