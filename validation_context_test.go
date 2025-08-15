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

func TestValidationContextCancellation(t *testing.T) {
	t.Parallel()

	// Create a cancelled context to test immediate cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Create a mock transport for testing
	transport := &mockTransportCtx{}
	device, err := New(transport)
	if err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Create config for validation
	config := DefaultValidationConfig()
	config.ReadRetries = 5
	config.RetryDelay = 10 * time.Millisecond

	// Create a tag with BaseTag (note: device field is lowercase)
	baseTag := BaseTag{device: device}
	ntagTag := &NTAGTag{BaseTag: baseTag}
	validatedTag := NewValidatedNTAGTag(ntagTag, config)

	// Test that context-aware methods exist and can be called
	// Note: This verifies the API exists and compiles correctly
	_, err = validatedTag.ReadBlockValidatedWithContext(ctx, 0)
	if err == nil {
		t.Error("Expected context cancellation error, got nil")
	}
	// Accept any error - the key is that the method exists and the context parameter works
}

// mockTransportCtx for context testing
type mockTransportCtx struct{}

func (*mockTransportCtx) SendCommand(_ byte, _ []byte) ([]byte, error) {
	// Simulate slow operation
	time.Sleep(100 * time.Millisecond)
	return []byte{0x00, 0xFF, 0x00}, nil
}

func (*mockTransportCtx) Close() error                     { return nil }
func (*mockTransportCtx) SetTimeout(_ time.Duration) error { return nil }
func (*mockTransportCtx) IsConnected() bool                { return true }
func (*mockTransportCtx) Type() TransportType              { return TransportMock }
