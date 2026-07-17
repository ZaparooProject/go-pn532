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

package uart

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/internal/frame"
	"github.com/ZaparooProject/go-pn532/internal/syncutil"
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
	ackFrame       = []byte{0x00, 0x00, 0xFF, 0x00, 0xFF, 0x00}
	nackFrame      = []byte{0x00, 0x00, 0xFF, 0xFF, 0x00, 0x00}
	openSerialPort = serial.Open
)

// Transport implements the pn532.Transport interface for UART communication.
type Transport struct {
	port                  serial.Port
	currentTrace          *pn532.TraceBuffer
	portName              string
	mu                    syncutil.Mutex
	closeMu               syncutil.Mutex // Protects port close operations
	currentTimeout        time.Duration  // Current timeout for deadline calculations
	lastCommand           byte
	connectionEstablished bool
	disconnected          bool // Set when device-gone is detected during I/O
	profile               deviceProfile
}

// isWindows returns true if running on Windows
func isWindows() bool {
	return runtime.GOOS == "windows"
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

// getReadTimeout returns the unified read timeout for all platforms
// Originally Windows needed 100ms while other platforms used 50ms, but Linux users
// reported intermittent empty data issues, so we unified to 100ms for all platforms
func getReadTimeout() time.Duration {
	return 100 * time.Millisecond
}

// windowsPostWriteDelay adds Windows-specific delay after write operations
func windowsPostWriteDelay() {
	if isWindows() {
		time.Sleep(15 * time.Millisecond) // Windows needs time for buffer flushing
	}
}

// New creates a new UART transport.
func New(portName string) (*Transport, error) {
	profile := resolveDeviceProfile(portName)
	port, timeout, err := openPort(portName, profile)
	if err != nil {
		return nil, fmt.Errorf("failed to open UART port %s: %w", portName, err)
	}

	return &Transport{
		port:           port,
		portName:       portName,
		currentTimeout: timeout, // Initialize with default read timeout
		profile:        profile,
	}, nil
}

func serialMode(profile deviceProfile) *serial.Mode {
	mode := &serial.Mode{
		BaudRate: 115200,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	if profile == profilePN532Killer {
		mode.InitialStatusBits = &serial.ModemOutputBits{DTR: false, RTS: false}
	}
	return mode
}

func openPort(portName string, profile deviceProfile) (serial.Port, time.Duration, error) {
	port, err := openSerialPort(portName, serialMode(profile))
	if err != nil {
		return nil, 0, err
	}

	// Set unified read timeout - originally 50ms on Linux/Mac and 100ms on Windows,
	// but unified to 100ms on all platforms to resolve Linux USB-UART reliability issues
	timeout := getReadTimeout()
	if err := port.SetReadTimeout(timeout); err != nil {
		_ = port.Close()
		return nil, 0, fmt.Errorf("failed to set UART read timeout: %w", err)
	}
	return port, timeout, nil
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

// handleNilPort handles the case when port is nil (e.g., in tests).
func (*Transport) handleNilPort(ctx context.Context) error {
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return errors.New("simulated UART error: no port available")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isDiagnoseROMRAMTest returns true if the command is a ROM or RAM diagnose test.
func isDiagnoseROMRAMTest(cmd byte, args []byte) bool {
	return cmd == 0x00 && len(args) > 0 && (args[0] == 0x01 || args[0] == 0x02)
}

// handleDiagnoseTest handles special Diagnose ROM/RAM test responses.
//
//nolint:wrapcheck // WrapError intentionally wraps errors with trace data
func (t *Transport) handleDiagnoseTest(ctx context.Context, args []byte) ([]byte, error) {
	res, err := t.receiveSpecialDiagnoseByte(ctx, args[0])
	if err != nil {
		return nil, t.currentTrace.WrapError(err)
	}
	t.connectionEstablished = true
	return res, nil
}

// handleNormalResponse handles normal command responses with ACK.
//
//nolint:wrapcheck // WrapError intentionally wraps errors with trace data
func (t *Transport) handleNormalResponse(ctx context.Context, ackData []byte) ([]byte, error) {
	res, err := t.receiveFrame(ctx, ackData)
	if err != nil {
		return nil, t.currentTrace.WrapError(err)
	}

	if t.profile != profilePN532Killer {
		if err := t.sendAck(ctx); err != nil {
			return nil, t.currentTrace.WrapError(err)
		}
	}

	t.connectionEstablished = true
	return res, nil
}

// SendCommand sends a command to the PN532 with context support.
// Context is checked at key points during the operation to allow cancellation.
//
//nolint:wrapcheck // WrapError intentionally wraps errors with trace data
func (t *Transport) SendCommand(ctx context.Context, cmd byte, args []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if t.port == nil {
		return nil, t.handleNilPort(ctx)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.currentTrace = pn532.NewTraceBuffer("UART", t.portName, 16)
	defer func() { t.currentTrace = nil }()

	t.lastCommand = cmd

	ackData, err := t.sendFrame(ctx, cmd, args)
	if err != nil {
		return nil, t.currentTrace.WrapError(err)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := sleepCtx(ctx, 6*time.Millisecond); err != nil {
		return nil, err
	}

	if isDiagnoseROMRAMTest(cmd, args) {
		return t.handleDiagnoseTest(ctx, args)
	}

	return t.handleNormalResponse(ctx, ackData)
}

// setTimeoutUnlocked sets the read timeout without acquiring the mutex (internal use)
func (t *Transport) setTimeoutUnlocked(timeout time.Duration) error {
	if t.port == nil {
		return nil // Allow operation when port is closed
	}
	t.currentTimeout = timeout // Store for deadline calculations
	err := t.port.SetReadTimeout(timeout)
	if err != nil {
		return fmt.Errorf("UART set timeout failed: %w", err)
	}
	return nil
}

// SetTimeout sets the read timeout for the transport
func (t *Transport) SetTimeout(timeout time.Duration) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.setTimeoutUnlocked(timeout)
}

// checkDeviceExists verifies the device file still exists (detects USB unplug).
// Only performs the check for paths that look like real device files.
func (t *Transport) checkDeviceExists() error {
	// Only check paths that look like real device files
	if !t.isRealDevicePath() {
		return nil
	}

	if _, err := os.Stat(t.portName); os.IsNotExist(err) {
		t.markDisconnected()
		return &pn532.TransportError{
			Op:   "checkDevice",
			Port: t.portName,
			Err:  pn532.ErrDeviceNotFound,
			Type: pn532.ErrorTypePermanent,
		}
	}
	return nil
}

// isRealDevicePath returns true if portName looks like a real device file path
func (t *Transport) isRealDevicePath() bool {
	// Unix device paths
	if strings.HasPrefix(t.portName, "/dev/") {
		return true
	}
	// Windows COM ports (COM1, \\.\COM1, etc.)
	upper := strings.ToUpper(t.portName)
	if strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "\\\\.\\COM") {
		return true
	}
	return false
}

// getDeadline returns the deadline for the current operation based on currentTimeout
// Adds 20% buffer to account for OS/driver overhead
func (t *Transport) getDeadline() time.Time {
	timeout := t.currentTimeout
	if timeout <= 0 {
		timeout = getReadTimeout() // Fallback to default
	}
	// Add 20% buffer for OS/driver overhead
	return time.Now().Add(timeout + timeout/5)
}

// Close closes the transport connection
// This method uses a separate mutex (closeMu) to close the port without waiting
// for the main mutex (mu). This allows closing the port while SendCommand is
// blocking on a read. Closing the port causes any blocking reads to fail
// immediately with an I/O error, allowing SendCommand to return promptly.
func (t *Transport) Close() error {
	// Use closeMu to serialize close operations and safely access port
	t.closeMu.Lock()
	port := t.port
	if port != nil {
		// Close the port - this will interrupt any blocking reads in SendCommand
		// We do this BEFORE acquiring mu to avoid deadlock
		if err := port.Close(); err != nil {
			t.closeMu.Unlock()
			return fmt.Errorf("UART close failed: %w", err)
		}
	}
	t.closeMu.Unlock()

	// Now acquire the main mutex to update state
	// This will block until SendCommand finishes (which should be quick now
	// that the port is closed and reads are failing)
	t.mu.Lock()
	t.port = nil
	t.connectionEstablished = false
	t.mu.Unlock()

	return nil
}

// IsConnected returns true if the transport is connected.
// Returns false if the port has been closed or if the device was detected
// as gone during I/O operations (e.g., USB unplug).
func (t *Transport) IsConnected() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.port != nil && !t.disconnected
}

// Reconnect closes and reopens the serial port connection.
// This is used for "nuclear" recovery when the PN532 firmware enters a
// complete lockup state where it stops ACKing any commands.
func (t *Transport) Reconnect() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Close existing port if open
	if t.port != nil {
		_ = t.port.Close()
		t.port = nil
	}

	// Wait for hardware settle time
	time.Sleep(100 * time.Millisecond)

	// Reopen with same settings
	port, timeout, err := openPort(t.portName, t.profile)
	if err != nil {
		return fmt.Errorf("reconnect failed: %w", err)
	}

	t.port = port
	t.currentTimeout = timeout
	t.connectionEstablished = false // Will be set true on first successful command
	t.disconnected = false          // Clear disconnect flag on successful reconnect

	return nil
}

// Type returns the transport type
func (*Transport) Type() pn532.TransportType {
	return pn532.TransportUART
}

// ClearTransportState resets transport state after protocol failures
// This is critical for preventing firmware lockup when switching between
// InCommunicateThru and InDataExchange operations after frame corruption
func (t *Transport) ClearTransportState() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.port != nil {
		// Reset input buffer to clear any stale/corrupted data
		_ = t.port.ResetInputBuffer()

		// Add delay for Windows USB-UART drivers to process buffer reset
		if isWindows() {
			time.Sleep(15 * time.Millisecond)
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return nil
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
func (t *Transport) drainWithRetry(ctx context.Context, operation string) error {
	baseDelay := 2 * time.Millisecond

	for attempt := range pn532.TransportDrainRetries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := t.port.Drain()
		if err == nil {
			return nil
		}

		if isInterruptedSystemCall(err) {
			if attempt < pn532.TransportDrainRetries-1 {
				delay := baseDelay * time.Duration(1<<attempt) // 2ms, 4ms, 8ms
				if sleepErr := sleepCtx(ctx, delay); sleepErr != nil {
					return sleepErr
				}
				continue
			}
		}

		return fmt.Errorf("UART %s drain failed: %w", operation, err)
	}

	return fmt.Errorf("UART %s drain failed after %d retries", operation, pn532.TransportDrainRetries)
}

// wakeUp wakes up the PN532 over UART with robust retry mechanism
func (t *Transport) wakeUp(ctx context.Context) error {
	return t.wakeUpWithRetry(ctx)
}

// wakeUpWithRetry attempts to wake up the PN532 with retry logic and verification
func (t *Transport) wakeUpWithRetry(ctx context.Context) error {
	delays := []time.Duration{
		pn532.UARTWakeupDelay1, // First attempt: quick
		pn532.UARTWakeupDelay2, // Second attempt: medium
		pn532.UARTWakeupDelay3, // Third attempt: longer
	}

	var lastErr error

	for attempt := range pn532.TransportWakeupRetries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := t.singleWakeUpAttempt(ctx)
		if err == nil {
			return nil // Wake-up successful
		}

		lastErr = err
		// Wait with increasing delay before retry
		if attempt < pn532.TransportWakeupRetries-1 {
			if err := sleepCtx(ctx, delays[attempt]); err != nil {
				return err
			}
		}
	}

	return fmt.Errorf("wake-up failed after %d attempts: %w", pn532.TransportWakeupRetries, lastErr)
}

// singleWakeUpAttempt performs a single wake-up attempt
func (t *Transport) singleWakeUpAttempt(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Over UART, PN532 must be "woken up" by sending a 0x55
	// dummy byte sequence and then waiting
	wakeSequence := []byte{
		0x55, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}

	t.traceTX(wakeSequence, "Wakeup")
	bytesWritten, err := t.port.Write(wakeSequence)
	if err != nil {
		return t.handleWriteError("wakeUp", err)
	} else if bytesWritten != len(wakeSequence) {
		return pn532.NewTransportWriteError("wakeUp", t.portName)
	}

	return t.drainWithRetry(ctx, "wake up")
}

// markDisconnected sets the disconnected flag, indicating the device is gone.
// Safe to call from within locked sections (does not acquire mu).
func (t *Transport) markDisconnected() {
	t.disconnected = true
}

// wrapReadError wraps port read errors as TransportError with appropriate type.
// Device-gone errors are classified as permanent; others as transient.
func (t *Transport) wrapReadError(op string, err error) error {
	if pn532.IsDeviceGoneError(err) {
		t.markDisconnected()
		return &pn532.TransportError{
			Op: op, Port: t.portName,
			Err: pn532.ErrDeviceNotFound, Type: pn532.ErrorTypePermanent,
		}
	}
	return &pn532.TransportError{
		Op: op, Port: t.portName,
		Err: fmt.Errorf("read failed: %w", err), Type: pn532.ErrorTypeTransient,
	}
}

// handleWriteError wraps write errors, checking if device is gone
func (t *Transport) handleWriteError(op string, err error) error {
	// Check for OS-level device-gone errors first
	if pn532.IsDeviceGoneError(err) {
		t.markDisconnected()
		return &pn532.TransportError{
			Op: op, Port: t.portName,
			Err: pn532.ErrDeviceNotFound, Type: pn532.ErrorTypePermanent,
		}
	}
	// Check if device file is gone
	if devErr := t.checkDeviceExists(); devErr != nil {
		t.markDisconnected()
		return devErr
	}
	// Device exists but write failed - still likely a fatal condition
	return &pn532.TransportError{
		Op:   op,
		Port: t.portName,
		Err:  err,
		Type: pn532.ErrorTypePermanent,
	}
}

// sendAck sends an ACK frame
func (t *Transport) sendAck(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.traceTX(ackFrame, "ACK")
	n, err := t.port.Write(ackFrame)
	if err != nil {
		return t.handleWriteError("sendAck", err)
	} else if n != len(ackFrame) {
		return pn532.NewTransportWriteError("sendAck", t.portName)
	}

	return t.drainWithRetry(ctx, "ACK")
}

// sendNack sends a NACK frame
func (t *Transport) sendNack(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	t.traceTX(nackFrame, "NACK")
	n, err := t.port.Write(nackFrame)
	if err != nil {
		return t.handleWriteError("sendNack", err)
	} else if n != len(nackFrame) {
		return pn532.NewTransportWriteError("sendNack", t.portName)
	}

	return t.drainWithRetry(ctx, "NACK")
}

// applyACKTimeout sets a short timeout for ACK reads and returns a cleanup function.
// ACKs should arrive quickly, so we cap at TransportACKTimeout to detect lockups faster.
func (t *Transport) applyACKTimeout() (deadline time.Time, cleanup func(), err error) {
	ackTimeout := min(t.currentTimeout, pn532.TransportACKTimeout)
	originalTimeout := t.currentTimeout

	if err := t.setTimeoutUnlocked(ackTimeout); err != nil {
		return time.Time{}, nil, fmt.Errorf("failed to set ACK timeout: %w", err)
	}

	cleanup = func() { _ = t.setTimeoutUnlocked(originalTimeout) }
	deadline = time.Now().Add(ackTimeout)
	pn532.Debugf("waitAck: starting, currentTimeout=%v", ackTimeout)
	return deadline, cleanup, nil
}

// ackBuffers holds pooled buffers for ACK processing
type ackBuffers struct {
	readBuf []byte // single byte read buffer
	ackBuf  []byte // ACK frame accumulator (6 bytes)
	preAck  []byte // pre-ACK data buffer
}

// getACKBuffers allocates pooled buffers for ACK processing
func getACKBuffers() *ackBuffers {
	return &ackBuffers{
		readBuf: frame.GetSmallBuffer(1),
		ackBuf:  frame.GetSmallBuffer(6)[:0],
		preAck:  frame.GetSmallBuffer(16)[:0],
	}
}

// release returns buffers to the pool
func (b *ackBuffers) release() {
	frame.PutBuffer(b.readBuf)
	frame.PutBuffer(b.ackBuf[:cap(b.ackBuf)])
	frame.PutBuffer(b.preAck[:cap(b.preAck)])
}

// waitAck waits for an ACK frame, returning any extra data received before it.
// This handles the Windows driver bug where ACK packets may be delivered out of order.
//
//nolint:gocognit,revive // ACK byte-scanning with timeout inherently requires multiple conditions
func (t *Transport) waitAck(ctx context.Context) ([]byte, error) {
	bufs := getACKBuffers()
	defer bufs.release()

	deadline, cleanup, err := t.applyACKTimeout()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	tries, maxTries := 0, 32
	for !time.Now().After(deadline) && tries < maxTries {
		// Check context on each iteration
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		n, err := t.port.Read(bufs.readBuf)
		if err != nil {
			pn532.Debugf("waitAck: read error at try %d: %v", tries, err)
			return bufs.preAck, t.wrapReadError("waitAck", err)
		}
		if n == 0 {
			tries++
			continue
		}

		bufs.ackBuf = append(bufs.ackBuf, bufs.readBuf[0])
		if len(bufs.ackBuf) < 6 {
			continue
		}

		if bytes.Equal(bufs.ackBuf, ackFrame) {
			t.traceRX(ackFrame, "ACK")
			if len(bufs.preAck) == 0 {
				return []byte{}, nil
			}
			result := make([]byte, len(bufs.preAck))
			copy(result, bufs.preAck)
			return result, nil
		}
		bufs.preAck = append(bufs.preAck, bufs.ackBuf[0])
		bufs.ackBuf = bufs.ackBuf[1:]
		tries++
	}

	// Timeout or max tries reached
	if time.Now().After(deadline) {
		t.traceTimeout("ACK deadline exceeded")
		pn532.Debugf("waitAck: deadline exceeded after %d tries", tries)
	} else {
		t.traceTimeout("No ACK after 32 bytes")
		pn532.Debugf("waitAck: no ACK after %d tries", maxTries)
	}
	return bufs.preAck, pn532.NewNoACKError("waitAck", t.portName)
}

// buildFrame constructs a PN532 command frame and returns it.
func buildFrame(cmd byte, args []byte) ([]byte, error) {
	dataLen := 2 + len(args) // hostToPn532 + cmd + args
	if dataLen > 255 {
		return nil, errors.New("frame data too large")
	}

	totalFrameSize := 3 + 2 + dataLen + 2 // preamble(3) + len+lcs(2) + data + dcs+postamble(2)

	frm := frame.GetFrameBuffer()
	defer frame.PutBuffer(frm)

	// Build frame: preamble + start code + length + LCS + data + DCS + postamble
	frm[0] = 0x00 // preamble
	frm[1] = 0x00
	frm[2] = 0xFF               // start code
	frm[3] = byte(dataLen)      // length
	frm[4] = ^byte(dataLen) + 1 // length checksum

	frm[5] = hostToPn532
	frm[6] = cmd
	copy(frm[7:7+len(args)], args)

	checksum := hostToPn532 + cmd
	for _, b := range args {
		checksum += b
	}
	frm[7+len(args)] = ^checksum + 1 // data checksum
	frm[8+len(args)] = 0x00          // postamble

	result := make([]byte, totalFrameSize)
	copy(result, frm[:totalFrameSize])
	return result, nil
}

// isTransientACKError returns true if the error is a transient ACK-level error worth retrying.
func isTransientACKError(err error) bool {
	return errors.Is(err, pn532.ErrNoACK) ||
		errors.Is(err, pn532.ErrNACKReceived) ||
		errors.Is(err, pn532.ErrFrameCorrupted)
}

// writeFrameAttempt performs a single frame write attempt and waits for ACK.
func (t *Transport) writeFrameAttempt(ctx context.Context, frm []byte, attempt int) ([]byte, error) {
	if t.port != nil {
		_ = t.port.ResetInputBuffer()
	}

	if err := t.wakeUp(ctx); err != nil {
		return nil, err
	}

	t.traceTX(frm, fmt.Sprintf("Cmd 0x%02X (attempt %d)", frm[6], attempt+1))
	n, err := t.port.Write(frm)
	if err != nil {
		return nil, t.handleWriteError("sendFrame", err)
	}
	if n != len(frm) {
		return nil, pn532.NewTransportWriteError("sendFrame", t.portName)
	}

	windowsPostWriteDelay()

	if err := t.drainWithRetry(ctx, "send frame"); err != nil {
		return nil, err
	}

	return t.waitAck(ctx)
}

// sendFrame sends a frame to the PN532 with automatic retry on ACK failures.
// This prevents device lockup when ACK is not received due to timing issues.
func (t *Transport) sendFrame(ctx context.Context, cmd byte, args []byte) ([]byte, error) {
	frm, err := buildFrame(cmd, args)
	if err != nil {
		return nil, pn532.NewDataTooLargeError("sendFrame", t.portName)
	}

	delays := []time.Duration{pn532.TransportACKDelay1, pn532.TransportACKDelay2, pn532.TransportACKDelay3}

	var lastErr error
	for attempt := range pn532.TransportACKRetries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		ackData, err := t.writeFrameAttempt(ctx, frm, attempt)
		if err == nil {
			return ackData, nil
		}

		if !isTransientACKError(err) {
			return nil, err
		}
		lastErr = err

		if t.port != nil {
			_ = t.port.ResetInputBuffer()
		}
		if attempt < pn532.TransportACKRetries-1 {
			if err := sleepCtx(ctx, delays[attempt]); err != nil {
				return nil, err
			}
		}
	}

	return nil, fmt.Errorf("send frame failed after %d ACK retries: %w", pn532.TransportACKRetries, lastErr)
}

// receiveFrame reads a frame from the PN532
func (t *Transport) receiveFrame(ctx context.Context, pre []byte) ([]byte, error) {
	// WORKAROUND: PN532 firmware quirk - InListPassiveTarget responses may arrive as pre-ACK data
	_ = pre // Pre-ACK data handled in receiveFrameAttempt
	const maxTries = 3

	for tries := range maxTries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		data, shouldRetry, err := t.receiveFrameAttempt(ctx, pre, tries)
		if err != nil {
			return nil, err
		}
		if !shouldRetry {
			return data, nil
		}

		// Send NACK and retry
		if err := t.sendNack(ctx); err != nil {
			return nil, err
		}
	}

	// All retries exhausted - check if this is InListPassiveTarget and create synthetic response
	if t.lastCommand == 0x4A {
		// Before returning synthetic response, verify the device is still present.
		// Without this check, a USB disconnect during the retry window gets masked
		// as "no tags detected", delaying disconnect detection by one poll cycle.
		if err := t.checkDeviceExists(); err != nil {
			return nil, err
		}
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
func (t *Transport) receiveFrameAttempt(
	ctx context.Context, pre []byte, tries int,
) (data []byte, retry bool, err error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
	}

	// Use buffer pool for frame reception - this is the highest impact optimization
	buf := frame.GetFrameBuffer()
	defer frame.PutBuffer(buf)

	// FIRMWARE QUIRK: PN532 may send InListPassiveTarget responses as pre-ACK data
	// Check if pre contains a valid frame before discarding
	if data, retry, preErr := t.tryProcessPreAckData(ctx, buf, pre, tries); data != nil || preErr != nil {
		return data, retry, preErr
	}

	totalLen, err := t.readInitialData(ctx, buf)
	if err != nil {
		return nil, false, err
	}

	return t.processFrameData(ctx, buf, totalLen)
}

// tryProcessPreAckData attempts to process pre-ACK data as a valid frame
// Returns data if successful, nil data if should continue normal processing
func (t *Transport) tryProcessPreAckData(
	ctx context.Context, buf, pre []byte, tries int,
) (data []byte, retry bool, err error) {
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

	totalLen, err = t.ensureCompleteFrame(ctx, buf, frameOff, frameLen, totalLen)
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
func (t *Transport) processFrameData(
	ctx context.Context, buf []byte, totalLen int,
) (data []byte, retry bool, err error) {
	off, shouldRetry := t.findFrameStart(buf, totalLen)
	if shouldRetry {
		return nil, true, nil
	}

	result := t.validateFrameLength(buf, off, totalLen)
	off, frameLen, shouldRetry, err := result.NewOff, result.FrameLen, result.Retry, result.Err
	if err != nil || shouldRetry {
		return nil, shouldRetry, err
	}

	totalLen, err = t.ensureCompleteFrame(ctx, buf, off, frameLen, totalLen)
	if err != nil {
		return nil, false, err
	}

	shouldRetry = t.validateFrameChecksum(buf, off, frameLen)
	if shouldRetry {
		return nil, shouldRetry, nil
	}

	return t.extractFrameData(buf, off, frameLen, totalLen)
}

// minFrameHeaderBytes is the minimum bytes needed to validate a frame header.
// Frame structure: preamble (0-2 bytes), 0xFF (start), LEN, LCS
// With typical 2-byte preamble: 00 00 FF LEN LCS = 5 bytes minimum
const minFrameHeaderBytes = 5

// readInitialData reads the initial frame data from the port.
// It accumulates data until we have at least minFrameHeaderBytes or deadline exceeded.
// Uses deadline-based timeout derived from currentTimeout to properly detect USB disconnection.
//
//nolint:gocognit,revive // Accumulation loop with timeout requires multiple conditions
func (t *Transport) readInitialData(ctx context.Context, buf []byte) (int, error) {
	// Platform-specific delay to let the PN532 start sending
	// Windows USB-serial drivers need more time for reliable responses
	initialDelay := 8 * time.Millisecond
	if isWindows() {
		initialDelay = 10 * time.Millisecond
	}
	if err := sleepCtx(ctx, initialDelay); err != nil {
		return 0, err
	}

	totalRead := 0
	deadline := t.getDeadline()

	// Accumulate data until we have enough for frame header validation
	for totalRead < minFrameHeaderBytes {
		// Check context
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		// Check hard deadline to prevent infinite loop on USB disconnect
		if time.Now().After(deadline) {
			// Deadline exceeded - check if device still exists
			if err := t.checkDeviceExists(); err != nil {
				return 0, err
			}
			// Device exists but no response
			if totalRead == 0 {
				// No data at all - try one more time with longer delay
				return t.retryInitialRead(ctx, buf, deadline)
			}
			// Got some data but not enough - return what we have
			// This will likely cause a retry at a higher level
			return totalRead, nil
		}

		bytesRead, err := t.port.Read(buf[totalRead:])
		if err != nil {
			return 0, t.wrapReadError("readInitialData", err)
		}
		totalRead += bytesRead

		// If we have enough bytes, we can stop
		if totalRead >= minFrameHeaderBytes {
			break
		}

		// Small delay between reads to avoid tight loop
		if bytesRead == 0 {
			if err := sleepCtx(ctx, time.Millisecond); err != nil {
				return 0, err
			}
		}
	}

	return totalRead, nil
}

// retryInitialRead performs a retry read with platform-specific timing
func (t *Transport) retryInitialRead(ctx context.Context, buf []byte, deadline time.Time) (int, error) {
	// Check context first
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	default:
	}

	// Check if we still have time for a retry
	if time.Now().After(deadline) {
		if err := t.checkDeviceExists(); err != nil {
			return 0, err
		}
		// Device exists but timed out - return 0 to allow higher-level retry
		return 0, nil
	}

	// Some PN532 modules need more time for certain commands
	retryDelay := 80 * time.Millisecond
	if isWindows() {
		retryDelay = 100 * time.Millisecond
	}
	if err := sleepCtx(ctx, retryDelay); err != nil {
		return 0, err
	}

	bytesRead, err := t.port.Read(buf)
	if err != nil {
		return 0, t.wrapReadError("readInitialData", err)
	}

	// If still no data after retry, check device existence
	if bytesRead == 0 {
		if err := t.checkDeviceExists(); err != nil {
			return 0, err
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
func (t *Transport) ensureCompleteFrame(ctx context.Context, buf []byte, off, frameLen, totalLen int) (int, error) {
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

	return t.readRemainingData(ctx, buf, totalLen, expectedLen)
}

// readRemainingData reads the remaining frame data with deadline-based timeout.
// Uses the transport's currentTimeout to calculate a proper deadline that can
// detect USB disconnection (which manifests as endless 0-byte reads on Linux).
func (t *Transport) readRemainingData(ctx context.Context, buf []byte, totalLen, expectedLen int) (int, error) {
	deadline := t.getDeadline()

	for totalLen < expectedLen {
		// Check context
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		// Check hard deadline to prevent infinite loop on USB disconnect
		if time.Now().After(deadline) {
			// Deadline exceeded - check if device still exists
			if err := t.checkDeviceExists(); err != nil {
				return 0, err
			}
			// Device exists but timed out
			return 0, &pn532.TransportError{
				Op:   "receiveFrame",
				Port: t.portName,
				Err:  pn532.ErrTransportTimeout,
				Type: pn532.ErrorTypeTransient,
			}
		}

		n2, err := t.port.Read(buf[totalLen:expectedLen])
		if err != nil {
			return 0, t.wrapReadError("readRemainingData", err)
		}
		if n2 > 0 {
			totalLen += n2
		} else {
			// No data available, wait a bit
			if err := sleepCtx(ctx, 10*time.Millisecond); err != nil {
				return 0, err
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
func (t *Transport) extractFrameData(buf []byte, off, frameLen, totalLen int) (data []byte, retry bool, err error) {
	// Trace the raw frame received (including preamble, checksums, etc.)
	if totalLen > 0 {
		t.traceRX(buf[:totalLen], "Response")
	}

	data, retry, err = frame.ExtractFrameData(buf, off, frameLen, pn532ToHost)
	if err != nil {
		return data, retry, fmt.Errorf("UART frame data extraction failed: %w", err)
	}
	return data, retry, nil
}

// receiveSpecialDiagnoseByte handles the non-standard single-byte response from ROM/RAM tests
func (t *Transport) receiveSpecialDiagnoseByte(ctx context.Context, _ byte) ([]byte, error) {
	// ROM/RAM tests return a single byte directly without frame format
	// 0x00 = OK, 0xFF = Not Good

	// Wait a bit for the response
	if err := sleepCtx(ctx, 10*time.Millisecond); err != nil {
		return nil, err
	}

	// Read a single byte using buffer pool
	buf := frame.GetSmallBuffer(1)
	defer frame.PutBuffer(buf)
	bytesRead, err := t.port.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("UART special diagnose byte read failed: %w", err)
	}

	if bytesRead == 0 {
		// Try again with longer timeout
		if sleepErr := sleepCtx(ctx, 50*time.Millisecond); sleepErr != nil {
			return nil, sleepErr
		}
		bytesRead, err = t.port.Read(buf)
		if err != nil {
			return nil, fmt.Errorf("UART special diagnose byte retry read failed: %w", err)
		}

		if bytesRead == 0 {
			// No response - some modules might not implement these tests
			return nil, &pn532.TransportError{
				Op:        "receiveSpecialDiagnoseByte",
				Port:      t.portName,
				Err:       pn532.ErrTransportTimeout,
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
func (t *Transport) HasCapability(capability pn532.TransportCapability) bool {
	switch capability {
	case pn532.CapabilityAutoPollNative:
		// UART supports native InAutoPoll command
		return true
	case pn532.CapabilityRequiresInSelect:
		// UART requires InSelect after InListPassiveTarget for proper target selection
		return true
	case pn532.CapabilityUART:
		// This is the UART transport
		return true
	case pn532.CapabilityRequiresRawType2Commands:
		return t.profile == profilePN532Killer
	default:
		return false
	}
}

// CheckHealth verifies the underlying device is still physically present.
// Returns nil if healthy, or a permanent error if the device is gone.
func (t *Transport) CheckHealth() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.disconnected {
		return &pn532.TransportError{
			Op: "checkHealth", Port: t.portName,
			Err: pn532.ErrDeviceNotFound, Type: pn532.ErrorTypePermanent,
		}
	}
	return t.checkDeviceExists()
}

// Ensure Transport implements pn532.Transport, pn532.Reconnecter, and pn532.DeviceHealthChecker
var (
	_ pn532.Transport           = (*Transport)(nil)
	_ pn532.Reconnecter         = (*Transport)(nil)
	_ pn532.DeviceHealthChecker = (*Transport)(nil)
)
