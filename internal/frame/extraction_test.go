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

package frame

import (
	"bytes"
	"math"
	"testing"
)

// TestExtractFrameDataOffsetOverflow covers offsets large enough that stepping
// to the TFI position would wrap past the maximum int and index buf negatively.
func TestExtractFrameDataOffsetOverflow(t *testing.T) {
	t.Parallel()
	buf := []byte{0x00, 0x00, 0xFF, 0x02, 0xFE, 0xD5, 0x03, 0x28, 0x00}

	for _, off := range []int{math.MaxInt, math.MaxInt - 1, math.MaxInt - 2, len(buf)} {
		data, retry, err := ExtractFrameData(buf, off, 2, 0xD5)
		if err == nil {
			t.Errorf("off=%d: expected error, got data=%v retry=%v", off, data, retry)
		}
		if data != nil {
			t.Errorf("off=%d: expected nil data, got %d bytes", off, len(data))
		}
	}
}

// TestExtractFrameDataLengthOverflow covers frame lengths large enough that the
// bounds check would wrap past the maximum int, which previously let a corrupt
// length size the pooled buffer and panic in make.
func TestExtractFrameDataLengthOverflow(t *testing.T) {
	t.Parallel()
	buf := []byte{0x00, 0x00, 0xFF, 0x02, 0xFE, 0xD5, 0x03, 0x28, 0x00}

	for _, frameLen := range []int{math.MaxInt, math.MaxInt - 1, math.MaxInt - 3, 1 << 40, len(buf) + 1} {
		data, retry, err := ExtractFrameData(buf, 3, frameLen, 0xD5)
		if err == nil {
			t.Errorf("frameLen=%d: expected error, got data=%d bytes retry=%v", frameLen, len(data), retry)
		}
		if data != nil {
			t.Errorf("frameLen=%d: expected nil data, got %d bytes", frameLen, len(data))
		}
	}
}

// TestExtractFrameDataValid confirms the tightened bounds checks still accept
// well-formed frames, including one that fills buf exactly.
func TestExtractFrameDataValid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		buf      []byte
		want     []byte
		off      int
		frameLen int
	}{
		{
			name:     "minimal frame",
			buf:      []byte{0x00, 0x00, 0xFF, 0x02, 0xFE, 0xD5, 0x03, 0x28, 0x00},
			off:      3,
			frameLen: 2,
			want:     []byte{0x03},
		},
		{
			name:     "data ends exactly at buffer end",
			buf:      []byte{0xFF, 0x02, 0xFE, 0xD5, 0x03, 0x28},
			off:      1,
			frameLen: 3,
			want:     []byte{0x03, 0x28},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, retry, err := ExtractFrameData(tt.buf, tt.off, tt.frameLen, 0xD5)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if retry {
				t.Fatal("unexpected retry")
			}
			if !bytes.Equal(data, tt.want) {
				t.Fatalf("got %#x, want %#x", data, tt.want)
			}
			PutBuffer(data)
		})
	}
}
