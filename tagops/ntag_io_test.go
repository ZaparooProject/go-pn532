// Copyright 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: Apache-2.0

//nolint:paralleltest // Tests use an intentionally stateful transport simulator.
package tagops

import (
	"context"
	"sync"
	"testing"

	"github.com/ZaparooProject/go-pn532"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testCmdInDataExchange    = 0x40
	testCmdInCommunicateThru = 0x42
)

type tagopsTransportCall struct {
	data []byte
	cmd  byte
}

type rawTagopsTransport struct {
	*pn532.MockTransport
	mu    sync.Mutex
	calls []tagopsTransportCall
}

func (t *rawTagopsTransport) SendCommand(ctx context.Context, cmd byte, data []byte) ([]byte, error) {
	t.mu.Lock()
	t.calls = append(t.calls, tagopsTransportCall{cmd: cmd, data: append([]byte(nil), data...)})
	t.mu.Unlock()
	return t.MockTransport.SendCommand(ctx, cmd, data)
}

func (*rawTagopsTransport) HasCapability(capability pn532.TransportCapability) bool {
	return capability == pn532.CapabilityRequiresRawType2Commands ||
		capability == pn532.CapabilityAutoPollNative
}

func (t *rawTagopsTransport) commandData(cmd byte) [][]byte {
	t.mu.Lock()
	defer t.mu.Unlock()

	var data [][]byte
	for _, call := range t.calls {
		if call.cmd == cmd {
			data = append(data, call.data)
		}
	}
	return data
}

func newRawTagOperations(t *testing.T) (*TagOperations, *rawTagopsTransport) {
	t.Helper()

	transport := &rawTagopsTransport{MockTransport: pn532.NewMockTransport()}
	transport.SelectTarget()
	device, err := pn532.New(transport)
	require.NoError(t, err)
	uid := []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}

	return &TagOperations{
		device:       device,
		tag:          &pn532.DetectedTag{UIDBytes: uid},
		ntagInstance: pn532.NewNTAGTag(device, uid, 0),
		tagType:      pn532.TagTypeNTAG,
		totalPages:   45,
	}, transport
}

func TestTagOperationsNTAGReadsUseTagCommandHandling(t *testing.T) {
	t.Run("single page read", func(t *testing.T) {
		ops, transport := newRawTagOperations(t)
		transport.SetResponse(testCmdInCommunicateThru, rawType2Response(
			[]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		))

		data, err := ops.ReadBlocks(context.Background(), 4, 4)

		require.NoError(t, err)
		assert.Equal(t, []byte{1, 2, 3, 4}, data)
		assert.Equal(t, byte(0x30), transport.commandData(testCmdInCommunicateThru)[0][0])
		assert.Zero(t, transport.GetCallCount(testCmdInDataExchange))
	})

	t.Run("multi-page fast read", func(t *testing.T) {
		ops, transport := newRawTagOperations(t)
		transport.SetResponse(testCmdInCommunicateThru, rawType2Response([]byte{1, 2, 3, 4, 5, 6, 7, 8}))

		data, err := ops.ReadBlocks(context.Background(), 4, 5)

		require.NoError(t, err)
		assert.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7, 8}, data)
		assert.Equal(t, byte(0x3A), transport.commandData(testCmdInCommunicateThru)[0][0])
		assert.Zero(t, transport.GetCallCount(testCmdInDataExchange))
	})
}

func TestTagOperationsNTAGWritesUseTagCommandHandling(t *testing.T) {
	ops, transport := newRawTagOperations(t)
	transport.QueueResponses(testCmdInCommunicateThru,
		rawType2Response([]byte{0xE1, 0x10, 0x12, 0x00, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
		rawType2Response(make([]byte, 16)),
		rawType2Response([]byte{0x0A}),
	)

	err := ops.WriteBlocks(context.Background(), 4, []byte{1, 2, 3, 4})

	require.NoError(t, err)
	commands := transport.commandData(testCmdInCommunicateThru)
	require.Len(t, commands, 3)
	assert.Equal(t, byte(0xA2), commands[2][0])
	assert.Zero(t, transport.GetCallCount(testCmdInDataExchange))
}

func rawType2Response(payload []byte) []byte {
	return append([]byte{0x43, 0x00}, payload...)
}
