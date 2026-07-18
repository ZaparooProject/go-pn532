// Copyright 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: Apache-2.0

package pn532

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type transportCall struct {
	data []byte
	cmd  byte
}

type rawType2Transport struct {
	*MockTransport
	mu    sync.Mutex
	calls []transportCall
}

func newRawType2Device(t *testing.T) (*Device, *rawType2Transport) {
	t.Helper()
	transport := &rawType2Transport{MockTransport: NewMockTransport()}
	transport.SelectTarget()
	device, err := New(transport)
	require.NoError(t, err)
	return device, transport
}

func (t *rawType2Transport) SendCommand(ctx context.Context, cmd byte, data []byte) ([]byte, error) {
	t.mu.Lock()
	t.calls = append(t.calls, transportCall{cmd: cmd, data: append([]byte(nil), data...)})
	t.mu.Unlock()
	return t.MockTransport.SendCommand(ctx, cmd, data)
}

func (*rawType2Transport) HasCapability(capability TransportCapability) bool {
	return capability == CapabilityRequiresRawType2Commands || capability == CapabilityAutoPollNative
}

func (t *rawType2Transport) commandData(cmd byte) [][]byte {
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

func TestCalculateCRCA(t *testing.T) {
	t.Parallel()

	assert.Equal(t, [2]byte{0x26, 0xEE}, calculateCRCA([]byte{ntagCmdRead, 0x04}))
}

func TestNTAGTagRawType2ReadBlock(t *testing.T) {
	t.Parallel()

	device, transport := newRawType2Device(t)
	response := []byte{0x43, 0x00}
	response = append(response, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08)
	response = append(response, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10)
	response = append(response, 0xAA, 0xBB) // Optional card CRC is not part of the returned block.
	transport.SetResponse(cmdInCommunicateThru, response)

	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}, 0)
	data, err := tag.ReadBlock(context.Background(), 4)

	require.NoError(t, err)
	assert.Equal(t, []byte{0x01, 0x02, 0x03, 0x04}, data)
	assert.Equal(t, [][]byte{{ntagCmdRead, 0x04, 0x26, 0xEE}}, transport.commandData(cmdInCommunicateThru))
	assert.Zero(t, transport.GetCallCount(cmdInDataExchange))
	assert.Zero(t, transport.GetCallCount(cmdInSelect))
}

func TestNTAGTagRawType2WriteResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		response      []byte
		errorContains string
	}{
		{name: "ACK", response: []byte{0x43, 0x00, 0x0A}},
		{name: "missing ACK", response: []byte{0x43, 0x00}, errorContains: "missing Type 2 ACK"},
		{name: "NAK", response: []byte{0x43, 0x00, 0x00}, errorContains: "Type 2 NAK"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			device, transport := newRawType2Device(t)
			transport.SetResponse(cmdInCommunicateThru, tt.response)
			tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}, 0)
			tag.tagType = NTAGType213

			err := tag.WriteBlock(context.Background(), 4, []byte{1, 2, 3, 4})
			if tt.errorContains == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.errorContains)
			}
			assert.Equal(t, [][]byte{{ntagCmdWrite, 0x04, 1, 2, 3, 4, 0x78, 0x57}},
				transport.commandData(cmdInCommunicateThru))
			assert.Zero(t, transport.GetCallCount(cmdInDataExchange))
		})
	}
}

func TestNTAGTagRawType2SpecialCommands(t *testing.T) {
	t.Parallel()

	t.Run("FastRead", func(t *testing.T) {
		t.Parallel()
		device, transport := newRawType2Device(t)
		transport.SetResponse(cmdInCommunicateThru, []byte{0x43, 0x00, 1, 2, 3, 4, 0xAA, 0xBB})
		tag := NewNTAGTag(device, nil, 0)

		data, err := tag.FastRead(context.Background(), 4, 4)
		require.NoError(t, err)
		assert.Equal(t, []byte{1, 2, 3, 4}, data)
		assert.Equal(t, [][]byte{{ntagCmdFastRead, 4, 4, 0x84, 0x71}}, transport.commandData(cmdInCommunicateThru))
	})

	t.Run("GetVersion", func(t *testing.T) {
		t.Parallel()
		device, transport := newRawType2Device(t)
		versionData := []byte{0x00, 0x04, 0x04, 0x02, 0x01, 0x00, 0x0F, 0x03, 0xAA, 0xBB}
		transport.SetResponse(cmdInCommunicateThru, append([]byte{0x43, 0x00}, versionData...))
		tag := NewNTAGTag(device, nil, 0)

		version, err := tag.GetVersion(context.Background())
		require.NoError(t, err)
		assert.Equal(t, NTAGType213, version.GetNTAGType())
		assert.Equal(t, [][]byte{{ntagCmdGetVersion, 0xF8, 0x32}}, transport.commandData(cmdInCommunicateThru))
	})

	t.Run("PwdAuth", func(t *testing.T) {
		t.Parallel()
		device, transport := newRawType2Device(t)
		transport.SetResponse(cmdInCommunicateThru, []byte{0x43, 0x00, 0x12, 0x34, 0xAA, 0xBB})
		tag := NewNTAGTag(device, nil, 0)

		pack, err := tag.PwdAuth(context.Background(), []byte{1, 2, 3, 4})
		require.NoError(t, err)
		assert.Equal(t, []byte{0x12, 0x34}, pack)
		command := []byte{ntagCmdPwdAuth, 1, 2, 3, 4}
		assert.Equal(t, appendCRCA(command), transport.commandData(cmdInCommunicateThru)[0])
	})
}

func TestNTAGTagRawType2ReadNDEF(t *testing.T) {
	t.Parallel()

	device, transport := newRawType2Device(t)
	ndefData := []byte{
		0x03, 0x0D, 0xD1, 0x01, 0x09, 0x54, 0x02, 0x65,
		0x6E, 0x48, 0x65, 0x6C, 0x6C, 0x6F, 0x21, 0xFE,
	}
	headerResponse := append([]byte{0x43, 0x00}, ndefData...)
	headerResponse = append(headerResponse, 0xAA, 0xBB)
	transport.QueueResponse(cmdInCommunicateThru, headerResponse)
	for offset := 0; offset < len(ndefData); offset += ntagBlockSize {
		readData := make([]byte, 16)
		copy(readData, ndefData[offset:])
		blockResponse := append([]byte{0x43, 0x00}, readData...)
		transport.QueueResponse(cmdInCommunicateThru, append(blockResponse, 0xCC, 0xDD))
	}

	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}, 0)
	tag.tagType = NTAGType213
	message, err := tag.ReadNDEF(context.Background())

	require.NoError(t, err)
	require.Len(t, message.Records, 1)
	assert.Equal(t, "Hello!", message.Records[0].Text)
	commands := transport.commandData(cmdInCommunicateThru)
	require.Len(t, commands, 5)
	for _, command := range commands {
		assert.Equal(t, byte(ntagCmdRead), command[0])
	}
	assert.Zero(t, transport.GetCallCount(cmdInDataExchange))
	assert.Zero(t, transport.GetCallCount(cmdInSelect))
}

func TestNTAGTagRawType2WriteNDEFAndVerify(t *testing.T) {
	t.Parallel()

	device, transport := newRawType2Device(t)
	message := &NDEFMessage{Records: []NDEFRecord{{Type: NDEFTypeText, Text: "Killer"}}}
	encoded, err := BuildNDEFMessageEx(message.Records)
	require.NoError(t, err)
	blocks := (len(encoded) + ntagBlockSize - 1) / ntagBlockSize

	for range blocks {
		transport.QueueResponse(cmdInCommunicateThru, []byte{0x43, 0x00, 0x0A})
	}
	for i := range blocks {
		blockData := make([]byte, ntagBlockSize)
		start := i * ntagBlockSize
		end := min(start+ntagBlockSize, len(encoded))
		copy(blockData, encoded[start:end])
		responseData := make([]byte, 16)
		copy(responseData, blockData)
		response := append([]byte{0x43, 0x00}, responseData...)
		transport.QueueResponse(cmdInCommunicateThru, append(response, 0xAA, 0xBB))
	}

	tag := NewNTAGTag(device, []byte{0x04, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC}, 0)
	tag.tagType = NTAGType213
	require.NoError(t, tag.WriteNDEF(context.Background(), message))
	assert.Equal(t, blocks*2, transport.GetCallCount(cmdInCommunicateThru))
	assert.Zero(t, transport.GetCallCount(cmdInDataExchange))
}
