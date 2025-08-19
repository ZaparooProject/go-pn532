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

package i2c

import (
	"context"
	"errors"
	"testing"
)

// TestI2CContextCancellation tests that I2C transport
// properly handles context cancellation
func TestI2CContextCancellation(t *testing.T) {
	t.Parallel()
	// This test verifies that context cancellation is checked before operations

	// Create a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Create a transport instance
	transport := &Transport{}

	cmd := byte(0x02) // GetFirmwareVersion
	args := []byte{}

	_, err := transport.SendCommandWithContext(ctx, cmd, args)

	// We expect this to return context.Canceled immediately
	if err == nil {
		t.Error("Expected context cancellation error, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
}
