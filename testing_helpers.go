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
	"time"
)

// SimpleMockTransport is a basic mock transport for testing
type SimpleMockTransport struct {
	Error    error
	Response []byte
}

// NewSimpleMockTransport creates a new simple mock transport
func NewSimpleMockTransport() *SimpleMockTransport {
	return &SimpleMockTransport{
		Response: []byte{0x01, 0x02}, // Default response
	}
}

// SendCommand returns the configured response or error
func (m *SimpleMockTransport) SendCommand(_ byte, _ []byte) ([]byte, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	return append([]byte(nil), m.Response...), nil
}

// SetResponse configures the response to return
func (m *SimpleMockTransport) SetResponse(response []byte) {
	m.Response = response
}

// SetError configures an error to return
func (m *SimpleMockTransport) SetError(err error) {
	m.Error = err
}

// Close implements Transport interface
func (*SimpleMockTransport) Close() error { return nil }

// SetTimeout implements Transport interface
func (*SimpleMockTransport) SetTimeout(_ time.Duration) error { return nil }

// IsConnected implements Transport interface
func (*SimpleMockTransport) IsConnected() bool { return true }

// Type implements Transport interface
func (*SimpleMockTransport) Type() TransportType { return TransportMock }
