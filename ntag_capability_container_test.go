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
	"errors"
	"testing"

	"github.com/ZaparooProject/go-pn532/internal/syncutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// memoryNTAGTransport answers the tag's READ and WRITE commands from an
// in-memory NTAG, so a test can check what a write leaves on the tag.
type memoryNTAGTransport struct {
	*MockTransport
	writeErr    error
	pages       [][ntagBlockSize]byte
	writtenPage []uint8
	mu          syncutil.Mutex
}

func newMemoryNTAGDevice(t *testing.T, totalPages int) (*Device, *memoryNTAGTransport) {
	t.Helper()
	transport := &memoryNTAGTransport{
		MockTransport: NewMockTransport(),
		pages:         make([][ntagBlockSize]byte, totalPages),
	}
	transport.SelectTarget()
	device, err := New(transport)
	require.NoError(t, err)
	return device, transport
}

func (m *memoryNTAGTransport) SendCommand(ctx context.Context, cmd byte, data []byte) ([]byte, error) {
	if cmd != cmdInDataExchange || len(data) < 3 {
		return m.MockTransport.SendCommand(ctx, cmd, data)
	}
	switch data[1] {
	case ntagCmdRead:
		return m.read(int(data[2])), nil
	case ntagCmdWrite:
		return m.write(int(data[2]), data[3:])
	default:
		return m.MockTransport.SendCommand(ctx, cmd, data)
	}
}

// noAnswer is how the PN532 reports a tag that did not answer: a timeout.
var noAnswer = []byte{0x41, 0x01}

func (m *memoryNTAGTransport) read(page int) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if page >= len(m.pages) {
		return noAnswer
	}
	// A real tag returns four pages, wrapping to page 0 past the end.
	resp := []byte{0x41, 0x00}
	for i := range 4 {
		resp = append(resp, m.pages[(page+i)%len(m.pages)][:]...)
	}
	return resp
}

func (m *memoryNTAGTransport) write(page int, data []byte) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return nil, m.writeErr
	}
	if page >= len(m.pages) || len(data) < ntagBlockSize {
		return noAnswer, nil
	}
	m.writtenPage = append(m.writtenPage, uint8(page)) //nolint:gosec // page came from a byte
	for i := range ntagBlockSize {
		if page == ntagPageCC {
			// One-time programmable: bits can be set, never cleared.
			m.pages[page][i] |= data[i]
		} else {
			m.pages[page][i] = data[i]
		}
	}
	return []byte{0x41, 0x00}, nil
}

func (m *memoryNTAGTransport) container() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]byte(nil), m.pages[ntagPageCC][:]...)
}

func (m *memoryNTAGTransport) wrotePage(page uint8) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, written := range m.writtenPage {
		if written == page {
			return true
		}
	}
	return false
}

var ntagTestUID = []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}

func textMessage(text string) *NDEFMessage {
	return &NDEFMessage{Records: []NDEFRecord{{Type: NDEFTypeText, Text: text}}}
}

// A tag with a blank capability container is not an NDEF tag, so what
// WriteNDEF wrote to it could not be read back: DetectType saw no container
// and reported the tag as having no NDEF.
func TestNTAGTag_WriteNDEF_WritesBlankCapabilityContainer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		totalPages int
		wantSize   byte
	}{
		{name: "NTAG213", totalPages: ntag213TotalPages, wantSize: 0x12},
		{name: "NTAG215", totalPages: ntag215TotalPages, wantSize: 0x3E},
		{name: "NTAG216", totalPages: ntag216TotalPages, wantSize: 0x6D},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			device, transport := newMemoryNTAGDevice(t, tt.totalPages)
			ctx := context.Background()

			// With no container to read the type from, detection can only
			// assume one. The container must not be sized from that.
			writer := NewNTAGTag(device, ntagTestUID, 0)
			require.NoError(t, writer.DetectType(ctx))
			require.False(t, writer.HasNDEF())

			require.NoError(t, writer.WriteNDEF(ctx, textMessage("hello")))

			assert.Equal(t, []byte{0xE1, 0x10, tt.wantSize, 0x00}, transport.container())
			assert.True(t, writer.HasNDEF())

			reader := NewNTAGTag(device, ntagTestUID, 0)
			require.NoError(t, reader.DetectType(ctx))
			assert.True(t, reader.HasNDEF(), "a tag WriteNDEF has written must read back as an NDEF tag")
		})
	}
}

func TestNTAGTag_WriteNDEF_LeavesExistingCapabilityContainer(t *testing.T) {
	t.Parallel()

	containers := map[string][ntagBlockSize]byte{
		"valid NDEF container": {0xE1, 0x10, 0x3E, 0x00},
		// An Amiibo's container. It is one-time programmable, so writing an
		// NDEF one over it would only corrupt it.
		"foreign container": {0xF1, 0x10, 0xFF, 0xEE},
	}
	for name, container := range containers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			device, transport := newMemoryNTAGDevice(t, ntag215TotalPages)
			transport.pages[ntagPageCC] = container
			tag := NewNTAGTag(device, ntagTestUID, 0)
			tag.tagType = NTAGType215

			require.NoError(t, tag.WriteNDEF(context.Background(), textMessage("hello")))

			assert.Equal(t, container[:], transport.container())
			assert.False(t, transport.wrotePage(ntagPageCC))
		})
	}
}

// A size that cannot be established is not guessed: a wrong one could never
// be corrected.
func TestNTAGTag_WriteNDEF_LeavesBlankContainerOnUnknownSize(t *testing.T) {
	t.Parallel()
	device, transport := newMemoryNTAGDevice(t, 20)
	tag := NewNTAGTag(device, ntagTestUID, 0)
	tag.tagType = NTAGType213

	require.NoError(t, tag.WriteNDEF(context.Background(), textMessage("hi")))

	assert.Equal(t, []byte{0x00, 0x00, 0x00, 0x00}, transport.container())
	assert.False(t, transport.wrotePage(ntagPageCC))
}

func TestNTAGTag_WriteNDEF_CapabilityContainerWriteFailure(t *testing.T) {
	t.Parallel()
	device, transport := newMemoryNTAGDevice(t, ntag215TotalPages)
	transport.writeErr = errors.New("transport fault")
	tag := NewNTAGTag(device, ntagTestUID, 0)
	tag.tagType = NTAGType215

	err := tag.WriteNDEF(context.Background(), textMessage("hello"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write capability container")
}
