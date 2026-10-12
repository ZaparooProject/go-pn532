// Copyright 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pn532

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNTAGTag_WriteNDEFContext_Cancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setupMock     func(*MockTransport)
		name          string
		errorContains string
		cancelAfter   time.Duration
		expectError   bool
	}{
		{
			name: "context cancelled during write",
			setupMock: func(mock *MockTransport) {
				// Simulate slow write that will be cancelled
				mock.SetDelay(100 * time.Millisecond)
				mock.SetError(0x40, context.DeadlineExceeded)
			},
			cancelAfter:   50 * time.Millisecond,
			expectError:   true,
			errorContains: "deadline exceeded",
		},
		{
			name: "successful write with context",
			setupMock: func(mock *MockTransport) {
				// NDEF "test" is 14 bytes, which is 4 NTAG pages (4 bytes each)
				// Block 4: 03 0B D1 01
				// Block 5: 07 54 02 65
				// Block 6: 6E 74 65 73
				// Block 7: 74 FE 00 00
				writeSuccess := []byte{0x41, 0x00}

				// Queue the capability container read, then 4 write responses
				// and 4 read responses for verification
				mock.QueueResponses(0x40,
					[]byte{0x41, 0x00, 0xE1, 0x10, 0x12, 0x00}, // read capability container
					writeSuccess, writeSuccess, writeSuccess, writeSuccess, // 4 writes
					[]byte{0x41, 0x00, 0x03, 0x0B, 0xD1, 0x01}, // read block 4
					[]byte{0x41, 0x00, 0x07, 0x54, 0x02, 0x65}, // read block 5
					[]byte{0x41, 0x00, 0x6E, 0x74, 0x65, 0x73}, // read block 6
					[]byte{0x41, 0x00, 0x74, 0xFE, 0x00, 0x00}, // read block 7
				)
			},
			cancelAfter: 1 * time.Second, // Don't cancel
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			device, mockTransport := createMockDeviceWithTransport(t)
			tt.setupMock(mockTransport)

			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56}, 0x00)
			tag.tagType = NTAGType213 // Set explicitly to skip DetectType

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

			// Test WriteNDEF with context
			err := tag.WriteNDEF(ctx, message)

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
