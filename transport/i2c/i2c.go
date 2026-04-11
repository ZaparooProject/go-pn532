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

// Package i2c provides I2C transport implementation for PN532
package i2c

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/internal/frame"
	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/host/v3"
)

const (
	// PN532 I2C 7-bit address. The datasheet lists 0x48/0x49 as 8-bit
	// write/read addresses; periph.io expects the 7-bit form (0x48 >> 1 = 0x24)
	// and handles the R/W bit internally.
	pn532Addr = 0x24

	// Protocol constants.
	hostToPn532 = 0xD4
	pn532ToHost = 0xD5
	pn532Ready  = 0x01

	// Max clock frequency (400 kHz).
	maxClockFreq = 400 * physic.KiloHertz
)

var (
	ackFrame  = []byte{0x00, 0x00, 0xFF, 0x00, 0xFF, 0x00}
	nackFrame = []byte{0x00, 0x00, 0xFF, 0xFF, 0x00, 0x00}
)

// Transport implements the pn532.Transport interface for I2C communication
type Transport struct {
	dev          *i2c.Dev
	currentTrace *pn532.TraceBuffer // Trace buffer for current command (error-only)
	busName      string
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

// parseI2CPath extracts the bus path from a composite detection path.
// Accepts "/dev/i2c-1:0x24" (detection format) or "/dev/i2c-1" (bare bus).
func parseI2CPath(path string) string {
	bus, _, _ := strings.Cut(path, ":")
	return bus
}

// New creates a new I2C transport
func New(busName string) (*Transport, error) {
	// Initialize host
	if _, err := host.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize periph host: %w", err)
	}

	// Open I2C bus (strip address suffix from detection paths)
	bus, err := i2creg.Open(parseI2CPath(busName))
	if err != nil {
		return nil, fmt.Errorf("failed to open I2C bus %s: %w", busName, err)
	}

	// Create device with PN532 address and max frequency
	dev := &i2c.Dev{Addr: pn532Addr, Bus: bus}

	// Set maximum frequency
	_ = bus.SetSpeed(maxClockFreq) // Ignore error, continue with default speed

	transport := &Transport{
		dev:     dev,
		busName: busName,
		// Match UART's unified timeout - originally 50ms but increased to 100ms
		// for better I2C bus compatibility across different hardware
		timeout: 100 * time.Millisecond,
	}

	return transport, nil
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

		err := t.waitAck(ctx)
		if err == nil {
			return nil // ACK received successfully
		}
		if !isTransientACKError(err) {
			return err // Non-transient error, don't retry
		}
		lastErr = err

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
	t.currentTrace = pn532.NewTraceBuffer("I2C", t.busName, 16)
	defer func() { t.currentTrace = nil }()

	if err := t.sendWithACKRetry(ctx, cmd, args); err != nil {
		return nil, t.currentTrace.WrapError(err)
	}

	// Small delay for PN532 to process command
	if err := sleepCtx(ctx, 6*time.Millisecond); err != nil {
		return nil, err
	}

	resp, err := t.receiveFrame(ctx)
	if err != nil {
		return nil, t.currentTrace.WrapError(err)
	}
	return resp, nil
}

// SetTimeout sets the read timeout for the transport
func (t *Transport) SetTimeout(timeout time.Duration) error {
	t.timeout = timeout
	return nil
}

// Close closes the transport connection
func (*Transport) Close() error {
	// periph.io handles cleanup automatically
	return nil
}

// IsConnected returns true if the transport is connected
func (t *Transport) IsConnected() bool {
	return t.dev != nil
}

// Type returns the transport type
func (*Transport) Type() pn532.TransportType {
	return pn532.TransportI2C
}

// checkReady checks if the PN532 is ready by reading the ready status
// Now includes retry logic with exponential backoff for better hardware compatibility
func (t *Transport) checkReady() error {
	baseDelay := time.Millisecond

	var lastErr error
	for attempt := range pn532.TransportI2CFrameRetries {
		// Use buffer pool for ready status check - small optimization
		ready := frame.GetSmallBuffer(1)

		err := t.dev.Tx(nil, ready)
		if err != nil {
			frame.PutBuffer(ready)
			lastErr = fmt.Errorf("I2C ready check failed: %w", err)
			// Exponential backoff: 1ms, 2ms, 4ms, 8ms, 16ms
			if attempt < pn532.TransportI2CFrameRetries-1 {
				time.Sleep(baseDelay * time.Duration(1<<attempt))
				continue
			}
			return lastErr
		}

		if ready[0] == pn532Ready {
			frame.PutBuffer(ready)
			return nil
		}

		frame.PutBuffer(ready)
		// Device not ready yet, wait with backoff
		if attempt < pn532.TransportI2CFrameRetries-1 {
			time.Sleep(baseDelay * time.Duration(1<<attempt))
		}
	}

	return pn532.NewTransportNotReadyError("checkReady", t.busName)
}

// sendFrame sends a frame to the PN532 via I2C
func (t *Transport) sendFrame(cmd byte, args []byte) error {
	// Use buffer pool for frame construction - major optimization
	dataLen := 2 + len(args) // hostToPn532 + cmd + args
	if dataLen > 255 {
		// TODO: extended frames are not implemented
		return pn532.NewDataTooLargeError("sendFrame", t.busName)
	}

	// Calculate total frame size: preamble(3) + len+lcs(2) + data + dcs+postamble(2)
	totalFrameSize := 3 + 2 + dataLen + 2

	frm := frame.GetBuffer(totalFrameSize)
	defer frame.PutBuffer(frm)

	// Build frame manually for better performance
	frm[0] = 0x00 // preamble
	frm[1] = 0x00
	frm[2] = 0xFF               // start code
	frm[3] = byte(dataLen)      // length
	frm[4] = ^byte(dataLen) + 1 // length checksum

	// Add data: TFI + command + args
	frm[5] = hostToPn532
	frm[6] = cmd
	copy(frm[7:7+len(args)], args)

	// Calculate and add data checksum
	checksum := hostToPn532 + cmd
	for _, b := range args {
		checksum += b
	}

	frm[7+len(args)] = ^checksum + 1 // data checksum
	frm[8+len(args)] = 0x00          // postamble

	// Send frame via I2C (slice to exact size)
	t.traceTX(frm[:totalFrameSize], fmt.Sprintf("Cmd 0x%02X", cmd))
	if err := t.dev.Tx(frm[:totalFrameSize], nil); err != nil {
		return fmt.Errorf("failed to send I2C frame: %w", err)
	}

	return nil
}

// waitAck waits for an ACK frame from the PN532
func (t *Transport) waitAck(ctx context.Context) error {
	deadline := time.Now().Add(t.timeout)

	// PN532 prepends a RDY status byte to every I2C read, so we read 7 bytes
	// (1 RDY + 6 ACK) and skip the first byte for comparison.
	ackBuf := frame.GetSmallBuffer(7)
	defer frame.PutBuffer(ackBuf)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := t.checkReady(); err != nil {
			if err := sleepCtx(ctx, time.Millisecond); err != nil {
				return err
			}
			continue
		}

		if err := t.dev.Tx(nil, ackBuf); err != nil {
			return fmt.Errorf("I2C ACK read failed: %w", err)
		}

		// Skip the leading RDY byte, compare remaining 6 bytes
		if bytes.Equal(ackBuf[1:7], ackFrame) {
			t.traceRX(ackFrame, "ACK")
			return nil
		}

		if err := sleepCtx(ctx, time.Millisecond); err != nil {
			return err
		}
	}

	t.traceTimeout("No ACK received")
	return pn532.NewNoACKError("waitAck", t.busName)
}

// sendAck sends an ACK frame to the PN532
func (t *Transport) sendAck() error {
	t.traceTX(ackFrame, "ACK")
	if err := t.dev.Tx(ackFrame, nil); err != nil {
		return fmt.Errorf("failed to send ACK: %w", err)
	}
	return nil
}

// sendNack sends a NACK frame to the PN532, requesting retransmission.
func (t *Transport) sendNack() error {
	t.traceTX(nackFrame, "NACK")
	if err := t.dev.Tx(nackFrame, nil); err != nil {
		return fmt.Errorf("failed to send NACK: %w", err)
	}
	// ESPHome uses delay(10) after NACK — give PN532 time to prepare retransmit
	time.Sleep(10 * time.Millisecond)
	return nil
}

// receiveFrame reads a response frame from the PN532
func (t *Transport) receiveFrame(ctx context.Context) ([]byte, error) {
	deadline := time.Now().Add(t.timeout)
	const maxTries = 3

	for range maxTries {
		// Check context
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if time.Now().After(deadline) {
			return nil, &pn532.TransportError{
				Op: "receiveFrame", Port: t.busName,
				Err:       pn532.ErrTransportTimeout,
				Type:      pn532.ErrorTypeTimeout,
				Retryable: true,
			}
		}

		data, shouldRetry, err := t.receiveFrameAttempt(ctx)
		if err != nil {
			return nil, err
		}
		if !shouldRetry {
			return data, nil
		}

		// Send NACK and retry
		if err := t.sendNack(); err != nil {
			return nil, err
		}
	}

	// All retries exhausted
	return nil, &pn532.TransportError{
		Op: "receiveFrame", Port: t.busName,
		Err:       pn532.ErrCommunicationFailed,
		Type:      pn532.ErrorTypeTransient,
		Retryable: true,
	}
}

// receiveFrameAttempt reads a response using ESPHome's two-pass NACK pattern:
// 1. Read a small header to determine response length
// 2. Send NACK to request retransmission
// 3. Read the full response with the exact known size
// This avoids over-reading which causes clock-stretch bus lockups.
//
//nolint:gocognit,revive,cyclop // hardware protocol handler with inherent complexity
func (t *Transport) receiveFrameAttempt(ctx context.Context) (data []byte, shouldRetry bool, err error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}

	// PASS 1: Read header to determine response length.
	// ESPHome reads 6 frame bytes (+ 1 RDY = 7 total) for the header.
	respLen, err := t.readResponseLength(ctx)
	if err != nil {
		return nil, false, err
	}

	// Send NACK to request retransmission of the full response.
	if nackErr := t.sendNack(); nackErr != nil {
		return nil, false, nackErr
	}

	// PASS 2: Read the full response with exact size.
	// Total read: RDY(1) + preamble(3) + LEN(1) + LCS(1) + data(respLen) + DCS(1) + postamble(1)
	// = 1 + 6 + respLen + 2 = respLen + 9
	fullReadSize := respLen + 9

	// delay(1) before read — matches ESPHome's read_data
	time.Sleep(time.Millisecond)

	// Poll for ready
	deadline := time.Now().Add(t.timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		default:
		}
		if readyErr := t.checkReady(); readyErr == nil {
			break
		}
		if sleepErr := sleepCtx(ctx, 5*time.Millisecond); sleepErr != nil {
			return nil, false, sleepErr
		}
	}

	buf := frame.GetBuffer(fullReadSize)
	if txErr := t.dev.Tx(nil, buf); txErr != nil {
		frame.PutBuffer(buf)
		return nil, false, fmt.Errorf("I2C response read failed: %w", txErr)
	}

	// Skip RDY byte — frame data starts at index 1
	frameData := buf[1:]
	frameSize := fullReadSize - 1

	if frameSize > 0 {
		t.traceRX(frameData[:frameSize], "Response")
	}

	off, err := t.findI2CFrameStart(frameData, frameSize)
	if err != nil {
		frame.PutBuffer(buf)
		return nil, false, err
	}

	frameLen, shouldRetry, err := t.validateI2CFrameLength(frameData, off, frameSize)
	if err != nil || shouldRetry {
		frame.PutBuffer(buf)
		return nil, shouldRetry, err
	}

	shouldRetry, err = t.validateI2CFrameChecksum(frameData, off, frameLen)
	if err != nil || shouldRetry {
		frame.PutBuffer(buf)
		return nil, shouldRetry, err
	}

	result, shouldRetry, err := t.extractI2CFrameData(frameData, off, frameLen)
	frame.PutBuffer(buf)
	return result, shouldRetry, err
}

// readResponseLength reads just the response header to determine the data length.
// Matches ESPHome's read_response_length_() pattern.
func (t *Transport) readResponseLength(ctx context.Context) (int, error) {
	// delay(1) before read — matches ESPHome's read_data
	time.Sleep(time.Millisecond)

	// Poll for ready
	deadline := time.Now().Add(t.timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
		if err := t.checkReady(); err == nil {
			break
		}
		if err := sleepCtx(ctx, 5*time.Millisecond); err != nil {
			return 0, err
		}
	}

	// Read 7 bytes: 1 RDY + 6 frame header bytes
	// Frame header: [preamble 00] [startcode 00 FF] [LEN] [LCS] [TFI D5]
	hdr := frame.GetSmallBuffer(7)
	defer frame.PutBuffer(hdr)

	if err := t.dev.Tx(nil, hdr); err != nil {
		return 0, fmt.Errorf("I2C response header read failed: %w", err)
	}

	// hdr[0]=RDY, hdr[1..6]=frame header
	// Validate preamble and start code
	if hdr[1] != 0x00 || hdr[2] != 0x00 || hdr[3] != 0xFF {
		return 0, &pn532.TransportError{
			Op: "readResponseLength", Port: t.busName,
			Err: pn532.ErrFrameCorrupted, Type: pn532.ErrorTypeTransient, Retryable: true,
		}
	}

	// Validate length checksum
	fullLen := int(hdr[4])
	lcs := hdr[5]
	if ((fullLen + int(lcs)) & 0xFF) != 0 {
		return 0, &pn532.TransportError{
			Op: "readResponseLength", Port: t.busName,
			Err: pn532.ErrFrameCorrupted, Type: pn532.ErrorTypeTransient, Retryable: true,
		}
	}

	// Validate TFI byte
	if hdr[6] != pn532ToHost {
		return 0, &pn532.TransportError{
			Op: "readResponseLength", Port: t.busName,
			Err: pn532.ErrFrameCorrupted, Type: pn532.ErrorTypeTransient, Retryable: true,
		}
	}

	// fullLen includes TFI byte, actual data length is fullLen - 1
	dataLen := fullLen - 1
	if fullLen == 0 {
		dataLen = 0
	}

	return dataLen, nil
}

// findI2CFrameStart locates the frame start marker (0x00 0xFF)
// CRITICAL FIX: Now accepts actualLen to only search through actual received data
// This prevents false positives from searching uninitialized buffer space
func (t *Transport) findI2CFrameStart(buf []byte, actualLen int) (int, error) {
	// Only search through actual data received, not entire buffer
	searchLen := actualLen
	if searchLen > len(buf) {
		searchLen = len(buf)
	}

	for off := range searchLen - 1 {
		if buf[off] == 0x00 && buf[off+1] == 0xFF {
			return off + 2, nil // Skip to length byte
		}
	}

	return 0, &pn532.TransportError{
		Op: "receiveFrame", Port: t.busName,
		Err:       pn532.ErrFrameCorrupted,
		Type:      pn532.ErrorTypeTransient,
		Retryable: true,
	}
}

// validateI2CFrameLength validates the frame length and its checksum
// CRITICAL FIX: Now uses actualLen instead of len(buf) to avoid reading beyond actual data
func (t *Transport) validateI2CFrameLength(buf []byte, off, actualLen int) (frameLen int, shouldRetry bool, err error) {
	frameLen, shouldRetry, err = frame.ValidateFrameLength(buf, off-1, actualLen, "receiveFrame", t.busName)
	if err != nil {
		return frameLen, shouldRetry, fmt.Errorf("I2C frame length validation failed: %w", err)
	}
	return frameLen, shouldRetry, nil
}

// validateI2CFrameChecksum validates the frame data checksum
func (t *Transport) validateI2CFrameChecksum(buf []byte, off, frameLen int) (bool, error) {
	if off+2+frameLen+1 > len(buf) {
		return false, pn532.NewFrameCorruptedError("receiveFrame", t.busName)
	}

	start := off + 2
	end := off + 2 + frameLen + 1
	return frame.ValidateFrameChecksum(buf, start, end), nil
}

// extractI2CFrameData extracts and validates the final frame data
func (t *Transport) extractI2CFrameData(buf []byte, off, frameLen int) (data []byte, shouldRetry bool, err error) {
	// Extract frame data using shared utility
	data, shouldRetry, err = frame.ExtractFrameData(buf, off, frameLen, pn532ToHost)
	if err != nil {
		return data, shouldRetry, fmt.Errorf("I2C frame data extraction failed: %w", err)
	}
	if shouldRetry {
		return data, shouldRetry, nil
	}

	// I2C-specific: Send ACK for successful frame
	if err := t.sendAck(); err != nil {
		return nil, false, err
	}

	return data, false, nil
}

// Ensure Transport implements pn532.Transport
var _ pn532.Transport = (*Transport)(nil)
