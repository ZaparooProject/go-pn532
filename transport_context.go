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

	// If there's a deadline, set the transport timeout accordingly
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		if timeout > 0 {
			// Save current timeout to restore later
			oldTimeout := 5 * time.Second // Default timeout
			defer func() {
				_ = t.SetTimeout(oldTimeout)
			}()

			if err := t.SetTimeout(timeout); err != nil {
				return nil, err
			}
		}
	}

	// Create a channel for the result
	type result struct {
		err  error
		data []byte
	}
	resultChan := make(chan result, 1)

	// Run the command in a goroutine
	go func() {
		data, err := t.SendCommand(cmd, args)
		resultChan <- result{err, data}
	}()

	// Wait for either the result or context cancellation
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for command response: %w", ctx.Err())
	case res := <-resultChan:
		return res.data, res.err
	}
}

// AsTransportContext converts a Transport to TransportContext
func AsTransportContext(t Transport) TransportContext {
	if tc, ok := t.(TransportContext); ok {
		return tc
	}
	return &transportContextAdapter{Transport: t}
}
