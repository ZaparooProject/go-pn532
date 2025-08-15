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

package uart

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/internal/frame"
	"go.bug.st/serial"
)

// FrameValidationResult represents the result of frame length validation
type FrameValidationResult struct {
	Err      error // 8 bytes (interface)
	NewOff   int   // 8 bytes (on 64-bit)
	FrameLen int   // 8 bytes (on 64-bit)
	Retry    bool  // 1 byte
}

const (
	hostToPn532 = 0xD4
	pn532ToHost = 0xD5
	pn532Ready  = 0x01
)

var (
	ackFrame  = []byte{0x00, 0x00, 0xFF, 0x00, 0xFF, 0x00}
	nackFrame = []byte{0x00, 0x00, 0xFF, 0xFF, 0x00, 0x00}
)

// Transport implements the pn532.Transport interface for UART communication.
type Transport struct {
	port        serial.Port
	portName    string
	mu          sync.Mutex
	lastCommand byte // Track last command for special handling
}

// New creates a new UART transport.
func New(portName string) (*Transport, error) {
	port, err := serial.Open(portName, &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open UART port %s: %w", portName, err)
	}

	// Set default timeout - 50ms to match working reference implementation
	// This timeout has been proven to work with InListPassiveTarget
	if err := port.SetReadTimeout(50 * time.Millisecond); err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("failed to set UART read timeout: %w", err)
	}

	return &Transport{
		port:     port,
		portName: portName,
	}, nil
}

// SendCommand sends a command to the PN532 and waits for response.
func (t *Transport) SendCommand(cmd byte, args []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Track the command for special handling
	t.lastCommand = cmd

	ackData, err := t.sendFrame(cmd, args)
	if err != nil {
		return nil, err
	}

	_ = ackData // ACK data handled in waitAck

	// Wake delay - 6ms has been proven to work reliably
	time.Sleep(6 * time.Millisecond)

	// Special handling for Diagnose ROM/RAM tests
	if cmd == 0x00 && len(args) > 0 && (args[0] == 0x01 || args[0] == 0x02) {
		// ROM test (0x01) and RAM test (0x02) return non-standard single byte response
		// They return 0x00 for OK, 0xFF for failure
		res, diagErr := t.receiveSpecialDiagnoseByte(args[0])
		if diagErr != nil {
			return nil, diagErr
		}

		// No ACK is sent after receiving the byte response
		return res, nil
	}

	res, err := t.receiveFrame(ackData)
	if err != nil {
		return nil, err
	}

	if err := t.sendAck(); err != nil {
		return nil, err
	}

	return res, nil
}

// SetTimeout sets the read timeout for the transport
func (t *Transport) SetTimeout(timeout time.Duration) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	err := t.port.SetReadTimeout(timeout)
	if err != nil {
		return fmt.Errorf("UART set timeout failed: %w", err)
	}
	return nil
}

// Close closes the transport connection
func (t *Transport) Close() error {
	if t.port != nil {
		err := t.port.Close()
		if err != nil {
			return fmt.Errorf("UART close failed: %w", err)
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
	return pn532.TransportUART
}

// isInterruptedSystemCall checks if an error is caused by an interrupted system call
func isInterruptedSystemCall(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "interrupted system call") ||
		strings.Contains(errStr, "eintr")
}

// drainWithRetry performs port drain with retry logic for interrupted system calls
func (t *Transport) drainWithRetry(operation string) error {
	const maxRetries = 3
	baseDelay := 2 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		err := t.port.Drain()
		if err == nil {
			return nil
		}

		if isInterruptedSystemCall(err) {
			if attempt < maxRetries-1 {
				delay := baseDelay * time.Duration(1<<attempt) // 2ms, 4ms, 8ms
				time.Sleep(delay)
				continue
			}
		}

		return fmt.Errorf("UART %s drain failed: %w", operation, err)
	}

	return fmt.Errorf("UART %s drain failed after %d retries", operation, maxRetries)
}

// wakeUp wakes up the PN532 over UART
func (t *Transport) wakeUp() error {
	// Over UART, PN532 must be "woken up" by sending a 0x55
	// dummy byte and then waiting
	bytesWritten, err := t.port.Write([]byte{
		0x55, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	})
	if err != nil {
		return fmt.Errorf("UART wake up write failed: %w", err)
	} else if bytesWritten != 16 {
		return pn532.NewTransportWriteError("wakeUp", t.portName)
	}

	return t.drainWithRetry("wake up")
}

// sendAck sends an ACK frame
func (t *Transport) sendAck() error {
	n, err := t.port.Write(ackFrame)
	if err != nil {
		return fmt.Errorf("UART ACK write failed: %w", err)
	} else if n != len(ackFrame) {
		return pn532.NewTransportWriteError("sendAck", t.portName)
	}

	return t.drainWithRetry("ACK")
}

// sendNack sends a NACK frame
func (t *Transport) sendNack() error {
	n, err := t.port.Write(nackFrame)
	if err != nil {
		return fmt.Errorf("UART NACK write failed: %w", err)
	} else if n != len(nackFrame) {
		return pn532.NewTransportWriteError("sendNack", t.portName)
	}

	return t.drainWithRetry("NACK")
}

// waitAck waits for an ACK frame, returning any extra data received before it
// This handles the Windows driver bug where ACK packets may be delivered out of order
func (t *Transport) waitAck() ([]byte, error) {
	tries := 0
	maxTries := 32 // bytes to scan through

	// Use buffer pool for ACK processing - reduces small allocations
	buf := frame.GetSmallBuffer(1)
	defer frame.PutBuffer(buf)

	ackBuf := frame.GetSmallBuffer(6) // ACK is 6 bytes
	defer frame.PutBuffer(ackBuf)
	ackBuf = ackBuf[:0] // Reset length

	preAck := frame.GetSmallBuffer(16) // Pre-ACK data buffer
	defer frame.PutBuffer(preAck)
	preAck = preAck[:0] // Reset length

	for {
		if tries >= maxTries {
			return preAck, pn532.NewNoACKError("waitAck", t.portName)
		}

		n, err := t.port.Read(buf)
		if err != nil {
			return preAck, fmt.Errorf("UART ACK read failed: %w", err)
		} else if n == 0 {
			tries++
			continue
		}

		// Debug what we're reading in waitAck
		_ = tries // For potential future debugging

		ackBuf = append(ackBuf, buf[0])
		if len(ackBuf) < 6 {
			continue
		}

		if bytes.Equal(ackBuf, ackFrame) {
			// Copy preAck data to new buffer for return since we'll release the pooled buffer
			if len(preAck) == 0 {
				return []byte{}, nil
			}
			result := make([]byte, len(preAck))
			copy(result, preAck)
			return result, nil
		}
		preAck = append(preAck, ackBuf[0])
		ackBuf = ackBuf[1:]
		tries++
	}
}

// sendFrame sends a frame to the PN532
func (t *Transport) sendFrame(cmd byte, args []byte) ([]byte, error) {
	// Calculate total frame size
	dataLen := 2 + len(args) // hostToPn532 + cmd + args
	if dataLen > 255 {
		// TODO: extended frames are not implemented
		return nil, pn532.NewDataTooLargeError("sendFrame", t.portName)
	}

	totalFrameSize := 3 + 2 + dataLen + 2 // preamble(3) + len+lcs(2) + data + dcs+postamble(2)

	// Use buffer pool for frame construction - major optimization
	frm := frame.GetFrameBuffer()
	defer frame.PutBuffer(frm)

	// Build frame manually for better performance and control
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

	// Create final frame slice
	finalFrame := make([]byte, totalFrameSize)
	copy(finalFrame, frm[:totalFrameSize])

	// Wake up and write frame
	if err := t.wakeUp(); err != nil {
		return nil, err
	}

	n, err := t.port.Write(finalFrame)
	if err != nil {
		return nil, fmt.Errorf("UART send frame write failed: %w", err)
	} else if n != len(finalFrame) {
		return nil, pn532.NewTransportWriteError("sendFrame", t.portName)
	}

	if err := t.drainWithRetry("send frame"); err != nil {
		return nil, err
	}

	return t.waitAck()
}

// receiveFrame reads a frame from the PN532
func (t *Transport) receiveFrame(pre []byte) ([]byte, error) {
	// WORKAROUND: PN532 firmware quirk - InListPassiveTarget responses may arrive as pre-ACK data
	_ = pre // Pre-ACK data handled in receiveFrameAttempt
	const maxTries = 3

	for tries := 0; tries < maxTries; tries++ {
		data, shouldRetry, err := t.receiveFrameAttempt(pre, tries)
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

	// All retries exhausted - check if this is InListPassiveTarget and create synthetic response
	if t.lastCommand == 0x4A {
		// Create synthetic "no tags detected" response
		// Some PN532 firmware doesn't send a response when no tags are detected
		return []byte{0x4B, 0x00}, nil
	}

	return nil, &pn532.TransportError{
		Op: "receiveFrame", Port: t.portName,
		Err:       pn532.ErrInvalidResponse,
		Type:      pn532.ErrorTypePermanent,
		Retryable: false,
	}
}

// receiveFrameAttempt performs a single frame receive attempt
func (t *Transport) receiveFrameAttempt(pre []byte, tries int) (data []byte, retry bool, err error) {
	// Use buffer pool for frame reception - this is the highest impact optimization
	buf := frame.GetFrameBuffer()
	defer frame.PutBuffer(buf)

	// FIRMWARE QUIRK: PN532 may send InListPassiveTarget responses as pre-ACK data
	// Check if pre contains a valid frame before discarding
	if data, retry, preErr := t.tryProcessPreAckData(buf, pre, tries); data != nil || preErr != nil {
		return data, retry, preErr
	}

	totalLen, err := t.readInitialData(buf)
	if err != nil {
		return nil, false, err
	}

	return t.processFrameData(buf, totalLen)
}

// tryProcessPreAckData attempts to process pre-ACK data as a valid frame
// Returns data if successful, nil data if should continue normal processing
func (t *Transport) tryProcessPreAckData(buf, pre []byte, tries int) (data []byte, retry bool, err error) {
	if tries != 0 || len(pre) == 0 {
		return nil, false, nil // Continue normal processing
	}

	// Try to parse pre as a frame first
	if len(pre) < 6 {
		return nil, false, nil // Continue normal processing
	}

	// Copy pre data to buf and try to parse it
	copy(buf, pre)
	totalLen := len(pre)

	// Try to find frame start in pre data
	off, shouldRetry := t.findFrameStart(buf, totalLen)
	if shouldRetry {
		return nil, false, nil // Continue normal processing
	}

	// Continue processing this as a valid frame
	result := t.validateFrameLength(buf, off, totalLen)
	frameOff, frameLen, shouldRetry, err := result.NewOff, result.FrameLen, result.Retry, result.Err
	if err != nil {
		return nil, false, err
	}
	if shouldRetry {
		return nil, true, nil
	}

	totalLen, err = t.ensureCompleteFrame(buf, frameOff, frameLen, totalLen)
	if err != nil {
		return nil, false, err
	}

	shouldRetry = t.validateFrameChecksum(buf, frameOff, frameLen)
	if shouldRetry {
		return nil, false, nil // Continue normal processing
	}

	return t.extractFrameData(buf, frameOff, frameLen, totalLen)
}

// processFrameData processes the frame data through validation and extraction
func (t *Transport) processFrameData(buf []byte, totalLen int) (data []byte, retry bool, err error) {
	off, shouldRetry := t.findFrameStart(buf, totalLen)
	if shouldRetry {
		return nil, true, nil
	}

	result := t.validateFrameLength(buf, off, totalLen)
	off, frameLen, shouldRetry, err := result.NewOff, result.FrameLen, result.Retry, result.Err
	if err != nil || shouldRetry {
		return nil, shouldRetry, err
	}

	totalLen, err = t.ensureCompleteFrame(buf, off, frameLen, totalLen)
	if err != nil {
		return nil, false, err
	}

	shouldRetry = t.validateFrameChecksum(buf, off, frameLen)
	if shouldRetry {
		return nil, shouldRetry, nil
	}

	return t.extractFrameData(buf, off, frameLen, totalLen)
}

// readInitialData reads the initial frame data from the port
func (t *Transport) readInitialData(buf []byte) (int, error) {
	// Small delay to let the PN532 start sending
	time.Sleep(5 * time.Millisecond)

	bytesRead, err := t.port.Read(buf)
	if err != nil {
		return 0, fmt.Errorf("UART initial data read failed: %w", err)
	}

	// If we got 0 bytes, try one more time with a longer delay
	// Some PN532 modules need more time for certain commands
	if bytesRead == 0 {
		time.Sleep(50 * time.Millisecond)
		bytesRead, err = t.port.Read(buf)
		if err != nil {
			return 0, fmt.Errorf("UART initial data retry read failed: %w", err)
		}
	}

	return bytesRead, nil
}

// findFrameStart locates the frame start marker (0xFF)
func (*Transport) findFrameStart(buf []byte, totalLen int) (offset int, shouldRetry bool) {
	// UART-specific frame start detection: look for 0xFF marker
	// This differs from the shared utility which expects full 0x00 0xFF sequence
	off := 0
	for ; off < totalLen; off++ {
		if buf[off] == 0xFF {
			break
		}
	}
	if off == totalLen {
		// Signal retry to restore the behavior that allowed retries before commit ee8d50d
		return 0, true
	}
	return off, false
}

// validateFrameLength validates the frame length and its checksum
func (t *Transport) validateFrameLength(buf []byte, off, totalLen int) FrameValidationResult {
	frameLen, shouldRetry, err := frame.ValidateFrameLength(buf, off, totalLen, "receiveFrame", t.portName)
	return FrameValidationResult{
		NewOff:   off + 1,
		FrameLen: frameLen,
		Retry:    shouldRetry,
		Err:      err,
	}
}

// ensureCompleteFrame reads additional data if needed to get the complete frame
func (t *Transport) ensureCompleteFrame(buf []byte, off, frameLen, totalLen int) (int, error) {
	expectedLen := off + 2 + frameLen + 1 // off + LEN + LCS + frameLen + DCS
	if expectedLen <= totalLen {
		return totalLen, nil // Frame is already complete
	}

	// Ensure we don't exceed buffer size
	if expectedLen > len(buf) {
		return 0, &pn532.TransportError{
			Op: "receiveFrame", Port: t.portName,
			Err:       pn532.ErrDataTooLarge,
			Type:      pn532.ErrorTypePermanent,
			Retryable: false,
		}
	}

	return t.readRemainingData(buf, totalLen, expectedLen)
}

// readRemainingData reads the remaining frame data with timeout
func (t *Transport) readRemainingData(buf []byte, totalLen, expectedLen int) (int, error) {
	timeout := time.After(2 * time.Second)

	for totalLen < expectedLen {
		select {
		case <-timeout:
			return 0, &pn532.TransportError{
				Op: "receiveFrame", Port: t.portName,
				Err:       pn532.ErrTimeout,
				Type:      pn532.ErrorTypeTransient,
				Retryable: true,
			}
		default:
			n2, err := t.port.Read(buf[totalLen:expectedLen])
			if err != nil {
				return 0, fmt.Errorf("UART remaining data read failed: %w", err)
			}
			if n2 > 0 {
				totalLen += n2
			} else {
				// No data available, wait a bit
				time.Sleep(10 * time.Millisecond)
			}
		}
	}

	return totalLen, nil
}

// validateFrameChecksum validates the frame data checksum
func (*Transport) validateFrameChecksum(buf []byte, off, frameLen int) bool {
	start := off + 2
	end := off + 2 + frameLen + 1
	return frame.ValidateFrameChecksum(buf, start, end)
}

// extractFrameData extracts and validates the final frame data
func (*Transport) extractFrameData(buf []byte, off, frameLen, _ int) (data []byte, retry bool, err error) {
	data, retry, err = frame.ExtractFrameData(buf, off, frameLen, pn532ToHost)
	if err != nil {
		return data, retry, fmt.Errorf("UART frame data extraction failed: %w", err)
	}
	return data, retry, nil
}

// receiveSpecialDiagnoseByte handles the non-standard single-byte response from ROM/RAM tests
func (t *Transport) receiveSpecialDiagnoseByte(_ byte) ([]byte, error) {
	// ROM/RAM tests return a single byte directly without frame format
	// 0x00 = OK, 0xFF = Not Good

	// Wait a bit for the response
	time.Sleep(10 * time.Millisecond)

	// Read a single byte using buffer pool
	buf := frame.GetSmallBuffer(1)
	defer frame.PutBuffer(buf)
	bytesRead, err := t.port.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("UART special diagnose byte read failed: %w", err)
	}

	if bytesRead == 0 {
		// Try again with longer timeout
		time.Sleep(50 * time.Millisecond)
		bytesRead, err = t.port.Read(buf)
		if err != nil {
			return nil, fmt.Errorf("UART special diagnose byte retry read failed: %w", err)
		}

		if bytesRead == 0 {
			// No response - some modules might not implement these tests
			return nil, &pn532.TransportError{
				Op:        "receiveSpecialDiagnoseByte",
				Port:      t.portName,
				Err:       pn532.ErrTimeout,
				Type:      pn532.ErrorTypeTransient,
				Retryable: true,
			}
		}
	}

	// For the upper layers, we'll format this as a standard response
	// The response format expected is [0x01, status_byte]
	return []byte{0x01, buf[0]}, nil
}

// HasCapability implements the TransportCapabilityChecker interface
// UART transport supports native InAutoPoll for reduced CPU usage
func (*Transport) HasCapability(capability pn532.TransportCapability) bool {
	switch capability {
	case pn532.CapabilityAutoPollNative:
		// UART supports native InAutoPoll command
		return true
	case pn532.CapabilityRequiresInSelect:
		// UART requires InSelect after InListPassiveTarget for proper target selection
		return true
	default:
		return false
	}
}

// Ensure Transport implements pn532.Transport
var _ pn532.Transport = (*Transport)(nil)
