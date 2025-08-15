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

type commandResult struct {
	err  error
	data []byte
}

// SendCommandContext implements TransportContext by using the context deadline
func (t *transportContextAdapter) SendCommandContext(ctx context.Context, cmd byte, args []byte) ([]byte, error) {
	if err := t.checkContextCancelled(ctx); err != nil {
		return nil, err
	}

	timeout, err := t.calculateTimeout(ctx)
	if err != nil {
		return nil, err
	}

	if err := t.SetTimeout(timeout); err != nil {
		return nil, fmt.Errorf("failed to set transport timeout: %w", err)
	}

	return t.executeCommandWithContext(ctx, cmd, args)
}

func (*transportContextAdapter) checkContextCancelled(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled before sending command: %w", ctx.Err())
	default:
		return nil
	}
}

func (*transportContextAdapter) calculateTimeout(ctx context.Context) (time.Duration, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 5 * time.Second, nil
	}

	timeout := time.Until(deadline)
	if timeout <= 0 {
		return 0, errors.New("context deadline already passed")
	}

	// Apply safety margin to prevent race conditions
	if timeout > 10*time.Millisecond {
		timeout -= 5 * time.Millisecond
	}

	return timeout, nil
}

func (t *transportContextAdapter) executeCommandWithContext(
	ctx context.Context, cmd byte, args []byte,
) ([]byte, error) {
	resultChan := make(chan commandResult, 1)
	doneChan := make(chan struct{})

	var abandoned bool
	defer t.cleanupAbandonedOperation(&abandoned, resultChan)

	t.startCommandExecution(resultChan, doneChan, cmd, args)

	return t.waitForCommandResult(ctx, resultChan, doneChan, &abandoned)
}

func (*transportContextAdapter) cleanupAbandonedOperation(abandoned *bool, resultChan chan commandResult) {
	if *abandoned {
		select {
		case <-resultChan:
			// Operation completed, consume result to prevent goroutine leak
		case <-time.After(10 * time.Millisecond):
			// Timeout waiting for goroutine, it's truly hung
		}
	}
}

func (t *transportContextAdapter) startCommandExecution(
	resultChan chan commandResult, doneChan chan struct{}, cmd byte, args []byte,
) {
	go func() {
		defer close(doneChan)
		data, err := t.SendCommand(cmd, args)

		select {
		case resultChan <- commandResult{err, data}:
			// Result sent successfully
		default:
			// Result channel full or abandoned, operation was cancelled
		}
	}()
}

func (t *transportContextAdapter) waitForCommandResult(
	ctx context.Context, resultChan chan commandResult, doneChan chan struct{}, abandoned *bool,
) ([]byte, error) {
	select {
	case <-ctx.Done():
		*abandoned = true
		return nil, fmt.Errorf("context cancelled while waiting for command response: %w", ctx.Err())
	case res := <-resultChan:
		return res.data, res.err
	case <-doneChan:
		return t.handleCompletedOperation(resultChan)
	}
}

func (*transportContextAdapter) handleCompletedOperation(resultChan chan commandResult) ([]byte, error) {
	select {
	case res := <-resultChan:
		return res.data, res.err
	default:
		return nil, errors.New("command completed but no result available")
	}
}

// AsTransportContext converts a Transport to TransportContext
func AsTransportContext(t Transport) TransportContext {
	if tc, ok := t.(TransportContext); ok {
		return tc
	}
	return &transportContextAdapter{Transport: t}
}
