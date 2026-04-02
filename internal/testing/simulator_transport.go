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

package testing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"
)

// TransportType mirrors pn532.TransportType to avoid import cycle
type TransportType string

const (
	// TransportMock represents a mock transport for testing
	TransportMock TransportType = "mock"
)

// SimulatorTransport wraps VirtualPN532 and implements pn532.Transport interface.
// This allows using the wire-level simulator with the high-level Device API for
// integration testing.
type SimulatorTransport struct {
	sim        *VirtualPN532
	CommandLog []CommandLogEntry
	timeout    time.Duration
	connected  bool
}

// CommandLogEntry records a command sent to the transport
type CommandLogEntry struct {
	Timestamp time.Time
	Args      []byte
	Cmd       byte
}

// NewSimulatorTransport creates a new transport backed by VirtualPN532
func NewSimulatorTransport(sim *VirtualPN532) *SimulatorTransport {
	return &SimulatorTransport{
		sim:        sim,
		timeout:    time.Second,
		connected:  true,
		CommandLog: make([]CommandLogEntry, 0),
	}
}

// SendCommand sends a command to the simulated PN532 and returns the response.
// It handles frame encoding/decoding internally with context support.
func (t *SimulatorTransport) SendCommand(ctx context.Context, cmd byte, args []byte) ([]byte, error) {
	// Check context
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Log the command for test verification
	t.CommandLog = append(t.CommandLog, CommandLogEntry{
		Cmd:       cmd,
		Args:      append([]byte(nil), args...), // Copy to avoid mutation
		Timestamp: time.Now(),
	})

	// Build the frame
	frameData := t.buildFrame(cmd, args)

	// Send to simulator
	_, err := t.sim.Write(frameData)
	if err != nil {
		return nil, fmt.Errorf("write failed: %w", err)
	}

	// Check context after write
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Read ACK
	ackBuf := make([]byte, 6)
	bytesRead, err := t.sim.Read(ackBuf)
	if err != nil {
		return nil, fmt.Errorf("read ACK failed: %w", err)
	}
	if bytesRead < 6 || !bytes.Equal(ackBuf[:6], ACKFrame) {
		return nil, errors.New("expected ACK frame")
	}

	// Check context after ACK
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Read response frame
	respBuf := make([]byte, 512)
	respLen, err := t.sim.Read(respBuf)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	// Parse response frame
	return t.parseFrame(respBuf[:respLen])
}

// Close closes the transport
func (t *SimulatorTransport) Close() error {
	t.connected = false
	return nil
}

// SetTimeout sets the read timeout
func (t *SimulatorTransport) SetTimeout(timeout time.Duration) error {
	t.timeout = timeout
	return nil
}

// IsConnected returns whether the transport is connected
func (t *SimulatorTransport) IsConnected() bool {
	return t.connected
}

// Type returns the transport type
func (*SimulatorTransport) Type() TransportType {
	return TransportMock
}

// GetSimulator returns the underlying VirtualPN532 for test setup
func (t *SimulatorTransport) GetSimulator() *VirtualPN532 {
	return t.sim
}

// ClearCommandLog clears the command log
func (t *SimulatorTransport) ClearCommandLog() {
	t.CommandLog = make([]CommandLogEntry, 0)
}

// HasCommand checks if a specific command was sent
func (t *SimulatorTransport) HasCommand(cmd byte) bool {
	for _, entry := range t.CommandLog {
		if entry.Cmd == cmd {
			return true
		}
	}
	return false
}

// GetCommandCount returns how many times a command was sent
func (t *SimulatorTransport) GetCommandCount(cmd byte) int {
	count := 0
	for _, entry := range t.CommandLog {
		if entry.Cmd == cmd {
			count++
		}
	}
	return count
}

// buildFrame builds a PN532 frame from command and arguments
func (*SimulatorTransport) buildFrame(cmd byte, args []byte) []byte {
	// TFI (Host to PN532) + command byte + args
	dataLen := 2 + len(args) // TFI + CMD + args

	// Data checksum: 0x100 - (sum of TFI + CMD + args)
	sum := int(tfiHostToPN532) + int(cmd)
	for _, b := range args {
		sum += int(b)
	}
	dcs := byte(-sum) //nolint:gosec // intentional two's complement checksum

	// Build frame: PREAMBLE + START_CODE + LEN + LCS + TFI + CMD + DATA + DCS + POSTAMBLE
	frame := make([]byte, 0, 9+len(args))
	//nolint:gosec // intentional truncation for PN532 frame length fields
	frame = append(frame,
		0x00, 0x00, 0xFF, // Preamble and start code
		byte(dataLen),  // Length
		byte(-dataLen), // Length checksum (two's complement)
		tfiHostToPN532, // TFI (Host to PN532)
		cmd,            // Command
	)
	frame = append(frame, args...)   // Data (args)
	frame = append(frame, dcs, 0x00) // DCS + Postamble

	return frame
}

// parseFrame parses a PN532 response frame and extracts the data
func (*SimulatorTransport) parseFrame(frame []byte) ([]byte, error) {
	if len(frame) < 7 {
		return nil, fmt.Errorf("frame too short: %d bytes", len(frame))
	}

	// Find start code
	startIdx := -1
	for i := range len(frame) - 2 {
		if frame[i] == 0x00 && frame[i+1] == 0xFF {
			startIdx = i
			break
		}
	}
	if startIdx < 0 {
		return nil, errors.New("start code not found")
	}

	// Get length
	lenIdx := startIdx + 2
	if lenIdx >= len(frame) {
		return nil, errors.New("frame truncated at length")
	}
	dataLen := int(frame[lenIdx])

	// Check length checksum
	lcsIdx := lenIdx + 1
	if lcsIdx >= len(frame) {
		return nil, errors.New("frame truncated at LCS")
	}
	if (frame[lenIdx]+frame[lcsIdx])&0xFF != 0 {
		return nil, errors.New("length checksum mismatch")
	}

	// Extract data
	dataStart := lcsIdx + 1
	dataEnd := dataStart + dataLen
	if dataEnd > len(frame) {
		return nil, fmt.Errorf("data extends beyond frame: need %d, have %d", dataEnd, len(frame))
	}

	data := frame[dataStart:dataEnd]

	// Verify TFI
	if len(data) < 1 || data[0] != tfiPN532ToHost {
		return nil, fmt.Errorf("unexpected TFI: 0x%02X", data[0])
	}

	// Return response data (skip TFI byte)
	return data[1:], nil
}

// Note: SimulatorTransport implements a Transport-compatible interface.
// The actual interface compliance is verified in the tests where the
// type is used with pn532.New().
