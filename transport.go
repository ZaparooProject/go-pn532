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
	"fmt"
	"sync"
	"time"
)

// Transport defines the interface for communication with PN532 devices.
// This can be implemented by UART, I2C, or SPI backends.
type Transport interface {
	// SendCommand sends a command to the PN532 and waits for response
	SendCommand(cmd byte, args []byte) ([]byte, error)

	// SendCommandWithContext sends a command to the PN532 with context support
	SendCommandWithContext(ctx context.Context, cmd byte, args []byte) ([]byte, error)

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

// SendCommandWithContext sends a command with context support and retry logic
func (t *TransportWithRetry) SendCommandWithContext(ctx context.Context, cmd byte, args []byte) ([]byte, error) {
	var result []byte
	err := RetryWithConfig(ctx, t.config, func() error {
		var err error
		result, err = t.transport.SendCommandWithContext(ctx, cmd, args)
		if err != nil {
			// Wrap transport errors for better error handling
			return &TransportError{
				Op:        "SendCommandWithContext",
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

// MockTransport provides a mock implementation of Transport for testing
type MockTransport struct {
	responses map[byte][]byte
	callCount map[byte]int
	errorMap  map[byte]error
	timeout   time.Duration
	delay     time.Duration
	mu        sync.RWMutex
	connected bool
}

// NewMockTransport creates a new mock transport
func NewMockTransport() *MockTransport {
	return &MockTransport{
		connected: true,
		timeout:   time.Second,
		responses: make(map[byte][]byte),
		callCount: make(map[byte]int),
		delay:     0,
		errorMap:  make(map[byte]error),
	}
}

// SendCommand implements Transport interface
func (m *MockTransport) SendCommand(cmd byte, _ []byte) ([]byte, error) {
	m.mu.RLock()
	connected := m.connected
	delay := m.delay
	m.mu.RUnlock()

	if !connected {
		return nil, errors.New("transport not connected")
	}

	// Simulate hardware delay if configured
	if delay > 0 {
		time.Sleep(delay)
	}

	m.mu.Lock()
	// Track call count
	m.callCount[cmd]++

	// Check for injected error
	if err, exists := m.errorMap[cmd]; exists {
		m.mu.Unlock()
		return nil, err
	}

	// Return configured response
	if response, exists := m.responses[cmd]; exists {
		m.mu.Unlock()
		return response, nil
	}
	m.mu.Unlock()

	// Default response for unknown commands
	return []byte{0xD5, cmd + 1, 0x00}, nil // Basic ACK response
}

// SendCommandWithContext implements Transport interface with context support
func (m *MockTransport) SendCommandWithContext(ctx context.Context, cmd byte, _ []byte) ([]byte, error) {
	// Check context cancellation first
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	m.mu.RLock()
	connected := m.connected
	delay := m.delay
	m.mu.RUnlock()

	if !connected {
		return nil, errors.New("transport not connected")
	}

	// Simulate hardware delay if configured with context awareness
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	m.mu.Lock()
	// Track call count
	m.callCount[cmd]++

	// Check for injected error
	if err, exists := m.errorMap[cmd]; exists {
		m.mu.Unlock()
		return nil, err
	}

	// Return configured response
	if response, exists := m.responses[cmd]; exists {
		m.mu.Unlock()
		return response, nil
	}
	m.mu.Unlock()

	// Default response for unknown commands
	return []byte{0xD5, cmd + 1, 0x00}, nil // Basic ACK response
}

// Close implements Transport interface
func (m *MockTransport) Close() error {
	m.mu.Lock()
	m.connected = false
	m.mu.Unlock()
	return nil
}

// SetTimeout implements Transport interface
func (m *MockTransport) SetTimeout(timeout time.Duration) error {
	m.mu.Lock()
	m.timeout = timeout
	m.mu.Unlock()
	return nil
}

// IsConnected implements Transport interface
func (m *MockTransport) IsConnected() bool {
	m.mu.RLock()
	connected := m.connected
	m.mu.RUnlock()
	return connected
}

// Type implements Transport interface
func (*MockTransport) Type() TransportType {
	return TransportMock
}

// Test helper methods

// SetResponse configures a response for a specific command
func (m *MockTransport) SetResponse(cmd byte, response []byte) {
	m.mu.Lock()
	m.responses[cmd] = response
	m.mu.Unlock()
}

// SetError configures an error to be returned for a specific command
func (m *MockTransport) SetError(cmd byte, err error) {
	m.mu.Lock()
	m.errorMap[cmd] = err
	m.mu.Unlock()
}

// ClearError removes error injection for a command
func (m *MockTransport) ClearError(cmd byte) {
	m.mu.Lock()
	delete(m.errorMap, cmd)
	m.mu.Unlock()
}

// SetDelay configures a delay to simulate hardware response time
func (m *MockTransport) SetDelay(delay time.Duration) {
	m.mu.Lock()
	m.delay = delay
	m.mu.Unlock()
}

// GetCallCount returns how many times a command was called
func (m *MockTransport) GetCallCount(cmd byte) int {
	m.mu.RLock()
	count := m.callCount[cmd]
	m.mu.RUnlock()
	return count
}

// Reset clears all call counts and resets state
func (m *MockTransport) Reset() {
	m.mu.Lock()
	m.callCount = make(map[byte]int)
	m.connected = true
	m.mu.Unlock()
}
