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
	"sync"
	"time"
)

// BlockingMockTransport is a simple mock transport that can block operations on demand
// This is used for testing deadlock scenarios and context cancellation
type BlockingMockTransport struct {
	blockChan    chan struct{}
	ResponseFunc func(cmd byte, data []byte) ([]byte, error)
	Response     []byte
	timeout      time.Duration
	mu           sync.Mutex
	closed       bool
}

// NewBlockingMockTransport creates a new blocking mock transport
func NewBlockingMockTransport() *BlockingMockTransport {
	return &BlockingMockTransport{
		blockChan: make(chan struct{}),
		timeout:   5 * time.Second, // Default timeout
	}
}

// SendCommand blocks until Unblock() is called, timeout expires, or the transport is closed
func (m *BlockingMockTransport) SendCommand(cmd byte, data []byte) ([]byte, error) {
	m.mu.Lock()
	blockChan := m.blockChan
	closed := m.closed
	responseFunc := m.ResponseFunc
	response := m.Response
	timeout := m.timeout
	m.mu.Unlock()

	if closed {
		return nil, ErrTransportRead
	}

	// Wait for either unblock signal or timeout
	select {
	case <-blockChan:
		// Operation was unblocked, proceed normally
	case <-time.After(timeout):
		// Timeout occurred, return timeout error
		return nil, NewTimeoutError("SendCommand", "mock")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrTransportRead
	}

	// Use configurable response logic
	if responseFunc != nil {
		return responseFunc(cmd, data)
	}
	if response != nil {
		return append([]byte(nil), response...), nil
	}

	// Default response for backward compatibility
	return []byte{0x01, 0x02}, nil
}

// Unblock allows one blocked SendCommand to proceed
func (m *BlockingMockTransport) Unblock() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		close(m.blockChan)
		m.blockChan = make(chan struct{})
	}
}

// Close unblocks all operations and marks transport as closed
func (m *BlockingMockTransport) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.blockChan)
	}
	return nil
}

// SetResponse configures a fixed response for all SendCommand calls
func (m *BlockingMockTransport) SetResponse(response []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Response = response
	m.ResponseFunc = nil
}

// SetResponseFunc configures a dynamic response function for SendCommand calls
func (m *BlockingMockTransport) SetResponseFunc(fn func(cmd byte, data []byte) ([]byte, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ResponseFunc = fn
	m.Response = nil
}

// SetTimeout configures the timeout for blocking operations
func (m *BlockingMockTransport) SetTimeout(timeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.timeout = timeout
	return nil
}

// IsConnected always returns true for this mock
func (m *BlockingMockTransport) IsConnected() bool {
	return !m.closed
}

// Type returns TransportMock
func (*BlockingMockTransport) Type() TransportType {
	return TransportMock
}

// NewBlockingMockTransportWithResponse creates a mock transport with a predefined response
func NewBlockingMockTransportWithResponse(response []byte) *BlockingMockTransport {
	mock := NewBlockingMockTransport()
	mock.SetResponse(response)
	return mock
}

// NewBlockingMockTransportWithFunc creates a mock transport with a response function
func NewBlockingMockTransportWithFunc(fn func(cmd byte, data []byte) ([]byte, error)) *BlockingMockTransport {
	mock := NewBlockingMockTransport()
	mock.SetResponseFunc(fn)
	return mock
}
