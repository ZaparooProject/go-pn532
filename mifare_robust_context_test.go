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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMIFARETag_AuthenticateRobustContext_ActuallyRobust(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		expectError   bool
	}{
		{
			name: "should fail immediately without retry logic (demonstrating current limitation)",
			setupMock: func(mock *MockTransport) {
				// Set error - current implementation won't retry, robust version would
				mock.SetError(0x40, errors.New("auth failed"))
			},
			expectError:   true,
			errorContains: "auth failed", // Current implementation fails immediately
		},
		{
			name: "context cancellation should work",
			setupMock: func(mock *MockTransport) {
				// Add delay to allow context cancellation
				mock.SetDelay(200 * time.Millisecond)
				mock.SetError(0x40, context.Canceled)
			},
			expectError:   true,
			errorContains: "context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tag, mockTransport := setupMIFARETagTest(t, tt.setupMock)

			// Use short timeout for cancellation test
			timeout := 1 * time.Second
			if tt.errorContains == "context" {
				timeout = 100 * time.Millisecond
			}

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			err := tag.AuthenticateRobustContext(ctx, 1, 0x00, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}

			// Verify that robust context doesn't retry like it should
			// The current implementation only makes 1 call, robust should make multiple
			if tt.name == "should fail immediately without retry logic (demonstrating current limitation)" {
				// Current implementation makes only 1 call (no retry)
				// A proper robust implementation should make multiple calls
				callCount := mockTransport.GetCallCount(0x40)

				// This should fail because current AuthenticateRobustContext doesn't retry
				// When we implement proper robust context authentication, this assertion should pass
				require.Greater(t, callCount, 1,
					"AuthenticateRobustContext should retry multiple times like AuthenticateRobust, "+
						"but currently only makes %d call(s). This demonstrates the current "+
						"implementation is not actually robust.",
					callCount)
			}
		})
	}
}
