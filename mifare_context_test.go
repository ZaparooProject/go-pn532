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

func TestMIFARETag_WriteNDEFContext_Cancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		cancelAfter   time.Duration
		expectError   bool
	}{
		{
			name: "context cancelled during authentication",
			setupMock: func(mock *MockTransport) {
				// Simulate slow authentication that will be cancelled
				mock.SetDelay(100 * time.Millisecond)
				mock.SetError(0x40, context.Canceled)
			},
			cancelAfter:   50 * time.Millisecond,
			expectError:   true,
			errorContains: "context deadline exceeded",
		},
		{
			name: "successful write with context",
			setupMock: func(mock *MockTransport) {
				// Setup for successful NDEF write with MIFARE NDEF key
				// Auth response
				mock.SetResponse(0x40, []byte{0x41, 0x00})
				// Read sector 1, block 0
				mock.SetResponse(0x40, []byte{
					0x41, 0x00, 0x10, 0x44, 0x03, 0x02, 0x00, 0x00,
					0x03, 0x00, 0xFE, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				})
				// Write block response
				mock.SetResponse(0x40, []byte{0x41, 0x00})
			},
			cancelAfter: 1 * time.Second, // Don't cancel
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag, _ := setupMIFARETagTest(t, tt.setupMock)

			// Create a context that will be cancelled
			ctx, cancel := context.WithTimeout(context.Background(), tt.cancelAfter)
			defer cancel()

			message := &NDEFMessage{
				Records: []NDEFRecord{
					{
						Type: NDEFTypeText,
						Text: "test",
					},
				},
			}

			// Test WriteNDEFWithContext
			err := tag.WriteNDEFWithContext(ctx, message)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
