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
	"context"
	"errors"
	"testing"
	"time"
)

// TestUARTContextCancellationDuringDelay tests that UART transport
// properly handles context cancellation during hardware delays
func TestUARTContextCancellationDuringDelay(t *testing.T) {
	t.Parallel()
	// This test verifies that context cancellation is checked before operations

	// Create a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Create a transport instance
	transport := &Transport{}

	cmd := byte(0x02) // GetFirmwareVersion
	args := []byte{}

	start := time.Now()
	_, err := transport.SendCommandWithContext(ctx, cmd, args)
	elapsed := time.Since(start)

	// We expect this to return context.Canceled immediately
	if err == nil {
		t.Error("Expected context cancellation error, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}

	// The operation should return immediately (< 10ms)
	if elapsed > 10*time.Millisecond {
		t.Errorf("Operation took too long: %v, expected < 10ms for immediate cancellation", elapsed)
	}
}

// TestUARTContextTimeoutDuringOperation tests that context timeout
// interrupts operations that would normally take longer
func TestUARTContextTimeoutDuringOperation(t *testing.T) {
	t.Parallel()
	// This test verifies that context timeout interrupts long-running operations
	// The goal is to ensure delays and timeouts in UART operations respect context

	// Create a context with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Create a transport instance
	transport := &Transport{}

	cmd := byte(0x02) // GetFirmwareVersion
	args := []byte{}

	start := time.Now()
	_, err := transport.SendCommandWithContext(ctx, cmd, args)
	elapsed := time.Since(start)

	// We expect this to return context.DeadlineExceeded due to timeout
	if err == nil {
		t.Error("Expected context timeout error, got nil")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded error, got: %v", err)
	}

	// The operation should timeout within reasonable time of the context deadline
	// (should be ~50ms, but allow some margin for test execution time)
	if elapsed < 40*time.Millisecond || elapsed > 150*time.Millisecond {
		t.Errorf("Operation timing unexpected: %v, expected ~50ms ± 50ms for context timeout", elapsed)
	}
}
