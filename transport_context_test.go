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
	"testing"
	"time"
)

// TestSendCommandContext_BasicCancellation tests that context cancellation works
func TestSendCommandContext_BasicCancellation(t *testing.T) {
	t.Parallel()

	transport := &mockSimpleTransport{}
	transportCtx := AsTransportContext(transport)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := transportCtx.SendCommandContext(ctx, 0x01, []byte{0x02})
	if err == nil {
		t.Error("expected cancellation error, got nil")
	}

	// Verify it's a context cancellation error
	if ctx.Err() == nil {
		t.Error("context should be cancelled")
	}
}

// Simple mock transport for basic testing
type mockSimpleTransport struct{}

func (*mockSimpleTransport) SendCommand(_ byte, _ []byte) ([]byte, error) {
	// Simulate some work
	time.Sleep(10 * time.Millisecond)
	return []byte{0x00, 0xFF, 0x00}, nil
}

func (*mockSimpleTransport) Close() error                     { return nil }
func (*mockSimpleTransport) SetTimeout(_ time.Duration) error { return nil }
func (*mockSimpleTransport) IsConnected() bool                { return true }
func (*mockSimpleTransport) Type() TransportType              { return TransportMock }
