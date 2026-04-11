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

// Package spi provides SPI transport implementation for PN532
package spi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/internal/frame"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
	"periph.io/x/host/v3"
)

const (
	// SPI protocol constants
	spiStatRead  = 0x02
	spiDataWrite = 0x01
	spiDataRead  = 0x03
	spiReady     = 0x01

	// Protocol constants
	hostToPn532 = 0xD4
	pn532ToHost = 0xD5

	// Default SPI settings
	defaultFreq = 1 * physic.MegaHertz
	mode        = spi.Mode0 // CPOL=0, CPHA=0 (LSB first is handled by bit reversal)
)

var (
	ackFrame  = []byte{0x00, 0x00, 0xFF, 0x00, 0xFF, 0x00}
	nackFrame = []byte{0x00, 0x00, 0xFF, 0xFF, 0x00, 0x00}
)

// Transport implements the pn532.Transport interface for SPI communication
type Transport struct {
	port         spi.PortCloser
	conn         spi.Conn
	currentTrace *pn532.TraceBuffer // Trace buffer for current command (error-only)
	portName     string
	timeout      time.Duration
}

// traceTX records a TX operation if trace buffer is active
func (t *Transport) traceTX(data []byte, note string) {
	if t.currentTrace != nil {
		t.currentTrace.RecordTX(data, note)
	}
}

// traceRX records an RX operation if trace buffer is active
func (t *Transport) traceRX(data []byte, note string) {
	if t.currentTrace != nil {
		t.currentTrace.RecordRX(data, note)
	}
}

// traceTimeout records a timeout if trace buffer is active
func (t *Transport) traceTimeout(note string) {
	if t.currentTrace != nil {
		t.currentTrace.RecordTimeout(note)
	}
}

// New creates a new SPI transport
func New(portName string) (*Transport, error) {
	// Initialize host
	if _, err := host.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize periph host: %w", err)
	}

	// Open SPI port
	port, err := spireg.Open(portName)
	if err != nil {
		return nil, fmt.Errorf("failed to open SPI port %s: %w", portName, err)
	}

	// Connect with SPI parameters
	conn, err := port.Connect(defaultFreq, mode, 8)
	if err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("failed to connect SPI: %w", err)
	}

	transport := &Transport{
		port:     port,
		conn:     conn,
		portName: portName,
		timeout:  50 * time.Millisecond,
	}

	// Wake up the PN532
	transport.wakeup()

	return transport, nil
}

// wakeup sends the wake up sequence to PN532
func (t *Transport) wakeup() {
	// Send a dummy byte to wake up the PN532
	time.Sleep(1 * time.Millisecond)
	_ = t.conn.Tx([]byte{0x00}, nil) // Ignore error for wakeup
	time.Sleep(1 * time.Millisecond)
}

// reverseBit reverses the bits in a byte (LSB <-> MSB)
// PN532 uses LSB first, but most SPI implementations are MSB first
func reverseBit(b byte) byte {
	var result byte
	for range 8 {
		result <<= 1
		result |= b & 1
		b >>= 1
	}
	return result
}

// reverseBytes reverses bits in all bytes of a slice using buffer pool
func reverseBytes(data []byte) []byte {
	// Use buffer pool for reversed data - optimization for frequent bit reversal operations
	reversed := frame.GetBuffer(len(data))
	// Note: caller is responsible for calling frame.PutBuffer(reversed) when done
	for i, b := range data {
		reversed[i] = reverseBit(b)
	}
	return reversed[:len(data)] // Slice to exact size needed
}

// waitReady polls the PN532 status until it's ready
func (t *Transport) waitReady() error {
	deadline := time.Now().Add(t.timeout)
	statusCmd := []byte{reverseBit(spiStatRead), 0}

	// Use buffer pool for status response
	statusResp := frame.GetSmallBuffer(2)
	defer frame.PutBuffer(statusResp)

	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Millisecond) // Small delay between status checks

		if err := t.conn.Tx(statusCmd, statusResp); err != nil {
			return fmt.Errorf("SPI status read failed: %w", err)
		}

		if reverseBit(statusResp[1]) == spiReady {
			return nil
		}

		time.Sleep(5 * time.Millisecond) // Wait before next check
	}

	return pn532.NewTransportNotReadyError("waitReady", t.portName)
}

// sleepCtx performs a context-aware sleep. Returns ctx.Err() if context is cancelled.
// Uses a named timer that is explicitly stopped on cancellation so the underlying
// runtime timer can be released immediately rather than lingering until it fires.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isTransientACKError returns true if the error is a transient ACK-level error worth retrying.
func isTransientACKError(err error) bool {
	return errors.Is(err, pn532.ErrNoACK) ||
		errors.Is(err, pn532.ErrNACKReceived) ||
		errors.Is(err, pn532.ErrFrameCorrupted)
}

// sendWithACKRetry sends a frame and waits for ACK, retrying on transient errors.
// Returns nil on success, or the last error if all retries are exhausted.
func (t *Transport) sendWithACKRetry(ctx context.Context, cmd byte, args []byte) error {
	delays := []time.Duration{pn532.TransportACKDelay1, pn532.TransportACKDelay2, pn532.TransportACKDelay3}

	var lastErr error
	for attempt := range pn532.TransportACKRetries {
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := t.sendFrame(cmd, args); err != nil {
			return err
		}

		err := t.waitAck()
		if err == nil {
			return nil // ACK received successfully
		}
		if !isTransientACKError(err) {
			return err // Non-transient error, don't retry
		}
		lastErr = err

		// Wake up device before retry
		t.wakeup()

		// Wait before retry (except on last attempt)
		if attempt < pn532.TransportACKRetries-1 {
			if err := sleepCtx(ctx, delays[attempt]); err != nil {
				return err
			}
		}
	}

	return fmt.Errorf("send command failed after %d ACK retries: %w", pn532.TransportACKRetries, lastErr)
}

// SendCommand sends a command to the PN532 and waits for response.
// Includes automatic retry on ACK failures to prevent device lockup.
// Context is checked at key points during the operation to allow cancellation.
//
//nolint:wrapcheck // WrapError intentionally wraps errors with trace data
func (t *Transport) SendCommand(ctx context.Context, cmd byte, args []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Create trace buffer for this command (only used on error)
	t.currentTrace = pn532.NewTraceBuffer("SPI", t.portName, 16)
	defer func() { t.currentTrace = nil }()

	if err := t.sendWithACKRetry(ctx, cmd, args); err != nil {
		return nil, t.currentTrace.WrapError(err)
	}

	// Small delay for PN532 to process command
	if err := sleepCtx(ctx, 6*time.Millisecond); err != nil {
		return nil, err
	}

	resp, err := t.receiveFrame()
	if err != nil {
		return nil, t.currentTrace.WrapError(err)
	}
	return resp, nil
}

// sendFrame sends a command frame to the PN532
func (t *Transport) sendFrame(cmd byte, args []byte) error {
	// Calculate frame size
	length := byte(len(args) + 2)                      //nolint:gosec // PN532 frames are max 265 bytes
	frameSize := 3 + 1 + 1 + 1 + 1 + len(args) + 1 + 1 // preamble + len + lcs + tfi + cmd + args + dcs + postamble

	// Use buffer pool for frame construction - major optimization
	frameBuf := frame.GetBuffer(frameSize)
	defer frame.PutBuffer(frameBuf)

	// Build frame manually for better performance
	offset := 0
	// Preamble and start code
	frameBuf[offset] = 0x00
	frameBuf[offset+1] = 0x00
	frameBuf[offset+2] = 0xFF
	offset += 3

	// Length and checksum
	frameBuf[offset] = length
	frameBuf[offset+1] = ^length + 1
	offset += 2

	// TFI (Frame identifier)
	frameBuf[offset] = hostToPn532
	offset++

	// Command
	frameBuf[offset] = cmd
	offset++

	// Arguments
	copy(frameBuf[offset:offset+len(args)], args)
	offset += len(args)

	// Data checksum
	checksum := hostToPn532 + cmd
	for _, arg := range args {
		checksum += arg
	}
	frameBuf[offset] = ^checksum + 1
	offset++

	// Postamble
	frameBuf[offset] = 0x00

	// Prepare for SPI transmission using buffer pool
	spiDataBuf := frame.GetBuffer(frameSize + 1) // +1 for SPI command
	defer frame.PutBuffer(spiDataBuf)

	spiDataBuf[0] = reverseBit(spiDataWrite)

	// Reverse bytes and copy to SPI buffer
	reversedFrame := reverseBytes(frameBuf[:frameSize])
	defer frame.PutBuffer(reversedFrame) // Clean up reversed frame buffer
	copy(spiDataBuf[1:], reversedFrame)

	// Trace the frame being sent (before bit reversal for readability)
	t.traceTX(frameBuf[:frameSize], fmt.Sprintf("Cmd 0x%02X", cmd))

	// Send the frame
	time.Sleep(2 * time.Millisecond) // Required delay
	if err := t.conn.Tx(spiDataBuf[:frameSize+1], nil); err != nil {
		return pn532.NewTransportWriteError("sendFrame", t.portName)
	}

	return nil
}

// waitAck waits for ACK frame from PN532
func (t *Transport) waitAck() error {
	// Wait for device to be ready
	if err := t.waitReady(); err != nil {
		t.traceTimeout("Device not ready for ACK")
		return err
	}

	// Read ACK frame using buffer pool
	readCmd := []byte{reverseBit(spiDataRead)}
	readData := frame.GetSmallBuffer(len(ackFrame) + 1) // +1 for read command
	defer frame.PutBuffer(readData)

	if err := t.conn.Tx(readCmd, readData); err != nil {
		return pn532.NewTransportReadError("waitAck", t.portName)
	}

	// Convert from LSB to MSB and skip the first byte (status)
	ack := reverseBytes(readData[1:])
	defer frame.PutBuffer(ack)

	if !bytes.Equal(ack, ackFrame) {
		if bytes.Equal(ack, nackFrame) {
			t.traceRX(nackFrame, "NACK")
			return pn532.NewNACKReceivedError("waitAck", t.portName)
		}
		t.traceRX(ack, "Invalid ACK")
		return pn532.NewInvalidResponseError("waitAck", t.portName)
	}

	t.traceRX(ackFrame, "ACK")
	return nil
}

// receiveFrame receives a response frame from the PN532
func (t *Transport) receiveFrame() ([]byte, error) {
	// Wait for device to be ready
	if err := t.waitReady(); err != nil {
		return nil, err
	}

	// First read to get the frame length using buffer pool
	readCmd := []byte{reverseBit(spiDataRead)}
	headerData := frame.GetSmallBuffer(8) // Read header to determine length
	defer frame.PutBuffer(headerData)

	if err := t.conn.Tx(readCmd, headerData); err != nil {
		return nil, pn532.NewTransportReadError("receiveFrame", t.portName)
	}

	// Convert from LSB to MSB and skip the first byte
	header := reverseBytes(headerData[1:])
	defer frame.PutBuffer(header)

	// Validate preamble and start code
	if !bytes.Equal(header[0:3], []byte{0x00, 0x00, 0xFF}) {
		return nil, pn532.NewFrameCorruptedError("receiveFrame", t.portName)
	}

	// Get length
	length := header[3]
	lengthChecksum := header[4]

	// Validate length checksum
	if (length + lengthChecksum) != 0 {
		return nil, pn532.NewInvalidResponseError("receiveFrame", t.portName)
	}

	// Now read the full frame including data and checksums using buffer pool
	fullLength := int(length) + 2               // +2 for data checksum and postamble
	fullData := frame.GetBuffer(fullLength + 1) // +1 for read command
	defer frame.PutBuffer(fullData)

	// Read the remaining data
	readCmd2 := []byte{reverseBit(spiDataRead)}
	if err := t.conn.Tx(readCmd2, fullData); err != nil {
		return nil, pn532.NewTransportReadError("receiveFrame", t.portName)
	}

	// Convert and skip first byte
	data := reverseBytes(fullData[1:])
	defer frame.PutBuffer(data)

	// Trace the complete response (header + data)
	// Combine header and data for a complete picture
	fullResponse := make([]byte, len(header)+len(data))
	copy(fullResponse, header)
	copy(fullResponse[len(header):], data)
	t.traceRX(fullResponse, "Response")

	// Skip TFI (first byte) and extract response data
	if len(data) < int(length)+1 {
		return nil, pn532.NewFrameCorruptedError("receiveFrame", t.portName)
	}

	// data includes: TFI + response code + actual data
	// We want to skip TFI (data[0]) and response code (data[1])
	responseLen := int(length) - 2 // Subtract TFI and response code
	if responseLen < 0 {
		responseLen = 0
	}

	// Copy response data to new buffer since we'll release the pooled buffer
	responseData := make([]byte, responseLen)
	if responseLen > 0 {
		copy(responseData, data[2:2+responseLen])
	}

	// Verify data checksum
	checksum := frame.CalculateChecksum(data[:length])
	dataChecksum := data[length]
	if (checksum + dataChecksum) != 0 {
		return nil, pn532.NewChecksumMismatchError("receiveFrame", t.portName)
	}

	return responseData, nil
}

// SetTimeout sets the read timeout for the transport
func (t *Transport) SetTimeout(timeout time.Duration) error {
	t.timeout = timeout
	return nil
}

// Close closes the transport connection
func (t *Transport) Close() error {
	if t.port != nil {
		err := t.port.Close()
		if err != nil {
			return fmt.Errorf("SPI close failed: %w", err)
		}
	}
	return nil
}

// IsConnected returns true if the transport is connected
func (t *Transport) IsConnected() bool {
	return t.port != nil
}

// Type returns the transport type
func (*Transport) Type() pn532.TransportType {
	return pn532.TransportSPI
}
