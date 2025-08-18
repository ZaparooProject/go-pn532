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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetFirmwareVersionContextCancellation(t *testing.T) {
	t.Parallel()

	mock := NewMockTransport()
	// Configure mock to simulate a delay that allows cancellation
	mock.SetDelay(100 * time.Millisecond)

	device, err := New(mock)
	require.NoError(t, err)

	// Create context that cancels quickly
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	// This should fail due to context cancellation before the mock delay completes
	_, err = device.GetFirmwareVersionContext(ctx)

	// Verify that context cancellation is propagated
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded,
		"Expected context.DeadlineExceeded, got: %v", err)
}

func TestGetGeneralStatusContextCancellation(t *testing.T) {
	t.Parallel()

	mock := NewMockTransport()
	mock.SetDelay(50 * time.Millisecond)

	device, err := New(mock)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = device.GetGeneralStatusContext(ctx)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded,
		"Expected context.DeadlineExceeded, got: %v", err)
}

func TestDiagnoseContextCancellation(t *testing.T) {
	t.Parallel()

	mock := NewMockTransport()
	mock.SetDelay(50 * time.Millisecond)

	device, err := New(mock)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = device.DiagnoseContext(ctx, 0x00, []byte{0x01, 0x02})

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded,
		"Expected context.DeadlineExceeded, got: %v", err)
}
