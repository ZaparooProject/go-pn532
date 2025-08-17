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

package polling

import (
	"context"
	"testing"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanner_WriteToNextTag_NotRunning(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)
	scanner, err := NewScanner(device, DefaultScanConfig())
	require.NoError(t, err)

	ctx := context.Background()
	err = scanner.WriteToNextTag(ctx, 1*time.Second, func(_ pn532.Tag) error {
		return nil
	})

	require.Error(t, err)
	assert.Equal(t, ErrScannerNotRunning, err)
}

func TestScanner_WriteToCurrentTag_NotSupported(t *testing.T) {
	t.Parallel()
	device, mockTransport := createMockDeviceWithTransport(t)
	mockTransport.SetResponse(0x4A, []byte{0x4B, 0x00}) // InListPassiveTarget response (no tags)

	scanner, err := NewScanner(device, DefaultScanConfig())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start scanner first so it's running
	err = scanner.Start(ctx)
	require.NoError(t, err)
	defer func() {
		if stopErr := scanner.Stop(); stopErr != nil {
			t.Errorf("Failed to stop scanner: %v", stopErr)
		}
	}()

	// Now WriteToCurrentTag should return "not supported" error
	err = scanner.WriteToCurrentTag(func(_ pn532.Tag) error {
		return nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestScanner_HasPendingWrite_InitiallyFalse(t *testing.T) {
	t.Parallel()
	device, _ := createMockDeviceWithTransport(t)
	scanner, err := NewScanner(device, DefaultScanConfig())
	require.NoError(t, err)

	assert.False(t, scanner.HasPendingWrite())
}

func TestScanner_Multiple_WriteToNextTag_AlreadyPending(t *testing.T) {
	t.Parallel()
	t.Skip("Complex concurrency test - depends on precise timing with mocks")
}
