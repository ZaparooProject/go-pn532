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

package uart

import (
	"testing"

	"github.com/ZaparooProject/go-pn532"
)

// TestTransportCreation verifies basic transport creation and properties
func TestTransportCreation(t *testing.T) {
	t.Parallel()

	testPortName := "/dev/ttyUSB0"
	transport := &Transport{
		portName: testPortName,
	}

	// Verify port name is stored correctly
	if transport.portName != testPortName {
		t.Errorf("Expected port name %s, got %s", testPortName, transport.portName)
	}

	// Verify transport type
	expectedType := pn532.TransportUART
	if transport.Type() != expectedType {
		t.Errorf("Expected transport type %v, got %v", expectedType, transport.Type())
	}

	// Verify IsConnected returns false for uninitialized transport
	if transport.IsConnected() {
		t.Error("Expected IsConnected() to return false for uninitialized transport")
	}
}
