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

// Package i2c provides I2C transport implementation for PN532
package i2c

import (
	"bytes"
	"context"
	"fmt"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/internal/frame"
	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/host/v3"
)

const (
	// PN532 I2C address.
	pn532WriteAddr = 0x48 // Write operation
	pn532ReadAddr  = 0x49 // Read operation

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
	dev     *i2c.Dev
	busName string
	timeout time.Duration
}

// New creates a new I2C transport
func New(busName string) (*Transport, error) {
	// Initialize host
	if _, err := host.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize periph host: %w", err)
	}

	// Open I2C bus
	bus, err := i2creg.Open(busName)
	if err != nil {
		return nil, fmt.Errorf("failed to open I2C bus %s: %w", busName, err)
	}

	// Create device with PN532 address and max frequency
	dev := &i2c.Dev{Addr: pn532WriteAddr, Bus: bus}

	// Set maximum frequency
	_ = bus.SetSpeed(maxClockFreq) // Ignore error, continue with default speed

	transport := &Transport{
		dev:     dev,
		busName: busName,
		timeout: 50 * time.Millisecond,
	}

	return transport, nil
}

// SendCommand sends a command to the PN532 and waits for response
func (t *Transport) SendCommand(cmd byte, args []byte) ([]byte, error) {
	if err := t.sendFrame(cmd, args); err != nil {
		return nil, err
	}

	if err := t.waitAck(); err != nil {
		return nil, err
	}

	// Small delay for PN532 to process command
	time.Sleep(6 * time.Millisecond)

	return t.receiveFrame()
}

// SendCommandWithContext sends a command to the PN532 with context support
func (t *Transport) SendCommandWithContext(ctx context.Context, cmd byte, args []byte) ([]byte, error) {
	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// For now, delegate to existing implementation
	// TODO: Add context-aware operations
	return t.SendCommand(cmd, args)
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
func (t *Transport) checkReady() error {
	// Use buffer pool for ready status check - small optimization
	ready := frame.GetSmallBuffer(1)
	defer frame.PutBuffer(ready)

	if err := t.dev.Tx(nil, ready); err != nil {
		return fmt.Errorf("I2C ready check failed: %w", err)
	}

	if ready[0] != pn532Ready {
		return pn532.NewTransportNotReadyError("waitReady", t.busName)
	}

	return nil
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
	if err := t.dev.Tx(frm[:totalFrameSize], nil); err != nil {
		return fmt.Errorf("failed to send I2C frame: %w", err)
	}

	return nil
}

// waitAck waits for an ACK frame from the PN532
func (t *Transport) waitAck() error {
	deadline := time.Now().Add(t.timeout)

	// Use buffer pool for ACK frame reading
	ackBuf := frame.GetSmallBuffer(6)
	defer frame.PutBuffer(ackBuf)

	for time.Now().Before(deadline) {
		// Check if PN532 is ready
		if err := t.checkReady(); err != nil {
			time.Sleep(time.Millisecond)
			continue
		}

		// Read ACK frame into pooled buffer
		if err := t.dev.Tx(nil, ackBuf); err != nil {
			return fmt.Errorf("I2C ACK read failed: %w", err)
		}

		if bytes.Equal(ackBuf, ackFrame) {
			return nil
		}

		time.Sleep(time.Millisecond)
	}

	return pn532.NewNoACKError("waitAck", t.busName)
}

// sendAck sends an ACK frame to the PN532
func (t *Transport) sendAck() error {
	if err := t.dev.Tx(ackFrame, nil); err != nil {
		return fmt.Errorf("failed to send ACK: %w", err)
	}
	return nil
}

// sendNack sends a NACK frame to the PN532
func (t *Transport) sendNack() error {
	if err := t.dev.Tx(nackFrame, nil); err != nil {
		return fmt.Errorf("failed to send NACK: %w", err)
	}
	return nil
}

// receiveFrame reads a response frame from the PN532
func (t *Transport) receiveFrame() ([]byte, error) {
	deadline := time.Now().Add(t.timeout)
	const maxTries = 3

	for tries := 0; tries < maxTries; tries++ {
		if time.Now().After(deadline) {
			return nil, &pn532.TransportError{
				Op: "receiveFrame", Port: t.busName,
				Err:       pn532.ErrTransportTimeout,
				Type:      pn532.ErrorTypeTimeout,
				Retryable: true,
			}
		}

		data, shouldRetry, err := t.receiveFrameAttempt()
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

// receiveFrameAttempt performs a single frame receive attempt
func (t *Transport) receiveFrameAttempt() (data []byte, shouldRetry bool, err error) {
	// Check if PN532 is ready
	if readyErr := t.checkReady(); readyErr != nil {
		time.Sleep(time.Millisecond)
		// Device not ready, retry without error
		return nil, true, nil
	}

	buf, err := t.readFrameData()
	if err != nil {
		return nil, false, err
	}
	defer frame.PutBuffer(buf) // Ensure buffer is returned to pool

	off, err := t.findI2CFrameStart(buf)
	if err != nil {
		return nil, false, err
	}

	frameLen, shouldRetry, err := t.validateI2CFrameLength(buf, off)
	if err != nil || shouldRetry {
		return nil, shouldRetry, err
	}

	shouldRetry, err = t.validateI2CFrameChecksum(buf, off, frameLen)
	if err != nil || shouldRetry {
		return nil, shouldRetry, err
	}

	return t.extractI2CFrameData(buf, off, frameLen)
}

// readFrameData reads frame data from I2C using buffer pool
func (t *Transport) readFrameData() ([]byte, error) {
	// Use buffer pool for frame reading - major optimization
	buf := frame.GetBuffer(255 + 7) // max data + frame overhead
	if err := t.dev.Tx(nil, buf); err != nil {
		frame.PutBuffer(buf) // Return to pool on error
		return nil, fmt.Errorf("I2C frame data read failed: %w", err)
	}

	return buf, nil
}

// findI2CFrameStart locates the frame start marker (0x00 0xFF)
func (t *Transport) findI2CFrameStart(buf []byte) (int, error) {
	for off := 0; off < len(buf)-1; off++ {
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
func (t *Transport) validateI2CFrameLength(buf []byte, off int) (frameLen int, shouldRetry bool, err error) {
	frameLen, shouldRetry, err = frame.ValidateFrameLength(buf, off-1, len(buf), "receiveFrame", t.busName)
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
