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
	"fmt"
	"time"
)

// Transport defines the interface for communication with PN532 devices.
// This can be implemented by UART, I2C, or SPI backends.
type Transport interface {
	// SendCommand sends a command to the PN532 and waits for response
	SendCommand(cmd byte, args []byte) ([]byte, error)

	// Close closes the transport connection
	Close() error

	// SetTimeout sets the read timeout for the transport
	SetTimeout(timeout time.Duration) error

	// IsConnected returns true if the transport is connected
	IsConnected() bool

	// Type returns the transport type
	Type() TransportType
}

// TransportType represents the type of transport
type TransportType string

const (
	// TransportUART represents UART/serial transport.
	TransportUART TransportType = "uart"
	// TransportI2C represents I2C bus transport.
	TransportI2C TransportType = "i2c"
	// TransportSPI represents SPI bus transport.
	TransportSPI TransportType = "spi"
	// TransportMock represents a mock transport for testing
	TransportMock TransportType = "mock"
)

// TransportCapability represents specific capabilities or behaviors of a transport
type TransportCapability string

const (
	// CapabilityRequiresInSelect indicates the transport requires explicit InSelect
	CapabilityRequiresInSelect TransportCapability = "requires_in_select"

	// CapabilityAutoPollNative indicates the transport supports native InAutoPoll
	// with full command set and reliable operation (e.g., UART, I2C, SPI)
	CapabilityAutoPollNative TransportCapability = "autopoll_native"
)

// TransportCapabilityChecker defines an interface for querying transport capabilities
// This provides a clean, type-safe alternative to reflection-based mode detection
type TransportCapabilityChecker interface {
	// HasCapability returns true if the transport has the specified capability
	HasCapability(capability TransportCapability) bool
}

// TransportWithRetry wraps a Transport with retry capabilities
type TransportWithRetry struct {
	transport Transport
	config    *RetryConfig
}

// NewTransportWithRetry creates a new transport wrapper with retry logic
func NewTransportWithRetry(transport Transport, config *RetryConfig) *TransportWithRetry {
	if config == nil {
		config = DefaultRetryConfig()
	}
	return &TransportWithRetry{
		transport: transport,
		config:    config,
	}
}

// SendCommand sends a command with retry logic
func (t *TransportWithRetry) SendCommand(cmd byte, args []byte) ([]byte, error) {
	var result []byte
	err := RetryWithConfig(context.Background(), t.config, func() error {
		var err error
		result, err = t.transport.SendCommand(cmd, args)
		if err != nil {
			// Wrap transport errors for better error handling
			return &TransportError{
				Op:        "SendCommand",
				Err:       err,
				Type:      GetErrorType(err),
				Retryable: IsRetryable(err),
			}
		}
		return nil
	})
	return result, err
}

// Close closes the transport connection
func (t *TransportWithRetry) Close() error {
	if err := t.transport.Close(); err != nil {
		return fmt.Errorf("failed to close underlying transport: %w", err)
	}
	return nil
}

// SetTimeout sets the read timeout for the transport
func (t *TransportWithRetry) SetTimeout(timeout time.Duration) error {
	if err := t.transport.SetTimeout(timeout); err != nil {
		return fmt.Errorf("failed to set timeout on underlying transport: %w", err)
	}
	return nil
}

// IsConnected returns true if the transport is connected
func (t *TransportWithRetry) IsConnected() bool {
	return t.transport.IsConnected()
}

// Type returns the transport type
func (t *TransportWithRetry) Type() TransportType {
	return t.transport.Type()
}

// HasCapability forwards capability checking to the underlying transport
func (t *TransportWithRetry) HasCapability(capability TransportCapability) bool {
	if capChecker, ok := t.transport.(TransportCapabilityChecker); ok {
		return capChecker.HasCapability(capability)
	}
	return false
}

// SetRetryConfig updates the retry configuration
func (t *TransportWithRetry) SetRetryConfig(config *RetryConfig) {
	t.config = config
}
