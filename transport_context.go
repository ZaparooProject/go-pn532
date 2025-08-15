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

// TransportContext defines the interface for communication with PN532 devices
// with context support for cancellation and timeouts.
type TransportContext interface {
	Transport

	// SendCommandContext sends a command to the PN532 with context support
	SendCommandContext(ctx context.Context, cmd byte, args []byte) ([]byte, error)
}

// transportContextAdapter wraps a Transport to provide context support
type transportContextAdapter struct {
	Transport
}

// SendCommandContext implements TransportContext by using the context deadline
func (t *transportContextAdapter) SendCommandContext(ctx context.Context, cmd byte, args []byte) ([]byte, error) {
	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled before sending command: %w", ctx.Err())
	default:
	}

	// Calculate timeout with safety margin
	var timeout time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
		if timeout <= 0 {
			return nil, fmt.Errorf("context deadline already passed")
		}
		// Apply safety margin to prevent race conditions
		if timeout > 10*time.Millisecond {
			timeout = timeout - 5*time.Millisecond
		}
	} else {
		// No deadline, use reasonable default
		timeout = 5 * time.Second
	}

	// Set the transport timeout
	if err := t.SetTimeout(timeout); err != nil {
		return nil, fmt.Errorf("failed to set transport timeout: %w", err)
	}

	// Create channels for result and cancellation coordination
	type result struct {
		err  error
		data []byte
	}
	resultChan := make(chan result, 1)
	doneChan := make(chan struct{})

	// Track if we need to abandon the operation
	var abandoned bool
	defer func() {
		if abandoned {
			// Give the goroutine a moment to finish naturally
			select {
			case <-resultChan:
				// Operation completed, consume result to prevent goroutine leak
			case <-time.After(10 * time.Millisecond):
				// Timeout waiting for goroutine, it's truly hung
			}
		}
	}()

	// Run the command in a goroutine with proper cleanup
	go func() {
		defer close(doneChan)
		data, err := t.SendCommand(cmd, args)

		// Check if we should still report the result
		select {
		case resultChan <- result{err, data}:
			// Result sent successfully
		default:
			// Result channel full or abandoned, operation was cancelled
		}
	}()

	// Wait for either the result or context cancellation
	select {
	case <-ctx.Done():
		abandoned = true
		return nil, fmt.Errorf("context cancelled while waiting for command response: %w", ctx.Err())
	case res := <-resultChan:
		return res.data, res.err
	case <-doneChan:
		// Goroutine finished, try to get result
		select {
		case res := <-resultChan:
			return res.data, res.err
		default:
			// This shouldn't happen, but handle gracefully
			return nil, fmt.Errorf("command completed but no result available")
		}
	}
}

// AsTransportContext converts a Transport to TransportContext
func AsTransportContext(t Transport) TransportContext {
	if tc, ok := t.(TransportContext); ok {
		return tc
	}
	return &transportContextAdapter{Transport: t}
}
