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

// Package testing provides test utilities including a wire-level PN532 simulator.
//
// The VirtualPN532 type implements io.ReadWriter and simulates the PN532 chip
// at the frame protocol level, as specified in the PN532 User Manual section 6.2.
//
// Protocol Reference: PN532 User Manual, section 6.2 "Host controller communication protocol"
// - Normal Information Frame: §6.2.1.1
// - Extended Information Frame: §6.2.1.2
// - ACK Frame: §6.2.1.3
// - NACK Frame: §6.2.1.4
// - Error Frame: §6.2.1.5
package testing

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/ZaparooProject/go-pn532/internal/syncutil"
)

// PN532 Protocol Constants from PN532 User Manual §6.2.1
const (
	// Frame markers (§6.2.1.1)
	pn532Preamble   = 0x00
	pn532StartCode1 = 0x00
	pn532StartCode2 = 0xFF
	pn532Postamble  = 0x00

	// TFI values - Transport Frame Identifier (§6.2.1.1)
	tfiHostToPN532 = 0xD4 // Commands from host controller to PN532
	tfiPN532ToHost = 0xD5 // Responses from PN532 to host controller

	// Extended frame marker (§6.2.1.2)
	extendedFrameMarker = 0xFF

	// Error frame TFI (§6.2.1.5)
	tfiError = 0x7F
)

// ACK and NACK frames from PN532 User Manual §6.2.1.3 and §6.2.1.4
var (
	// ACKFrame is sent to acknowledge successful frame reception (§6.2.1.3)
	// Format: PREAMBLE START_CODE 00 FF POSTAMBLE
	ACKFrame = []byte{0x00, 0x00, 0xFF, 0x00, 0xFF, 0x00}

	// NACKFrame requests retransmission of last response (§6.2.1.4)
	// Format: PREAMBLE START_CODE FF 00 POSTAMBLE
	NACKFrame = []byte{0x00, 0x00, 0xFF, 0xFF, 0x00, 0x00}
)

// PN532 Command codes from PN532 User Manual §7 (Table 12)
const (
	cmdDiagnose            = 0x00 // §7.2.1
	cmdGetFirmwareVersion  = 0x02 // §7.2.2
	cmdGetGeneralStatus    = 0x04 // §7.2.3
	cmdReadRegister        = 0x06 // §7.2.4
	cmdWriteRegister       = 0x08 // §7.2.5
	cmdReadGPIO            = 0x0C // §7.2.6
	cmdWriteGPIO           = 0x0E // §7.2.7
	cmdSetSerialBaudRate   = 0x10 // §7.2.8
	cmdSetParameters       = 0x12 // §7.2.9
	cmdSAMConfiguration    = 0x14 // §7.2.10
	cmdPowerDown           = 0x16 // §7.2.11
	cmdRFConfiguration     = 0x32 // §7.3.1
	cmdRFRegulationTest    = 0x58 // §7.3.2
	cmdInJumpForDEP        = 0x56 // §7.3.3
	cmdInJumpForPSL        = 0x46 // §7.3.4
	cmdInListPassiveTarget = 0x4A // §7.3.5
	cmdInATR               = 0x50 // §7.3.6
	cmdInPSL               = 0x4E // §7.3.7
	cmdInDataExchange      = 0x40 // §7.3.8
	cmdInCommunicateThru   = 0x42 // §7.3.9
	cmdInDeselect          = 0x44 // §7.3.10
	cmdInRelease           = 0x52 // §7.3.11
	cmdInSelect            = 0x54 // §7.3.12
	cmdInAutoPoll          = 0x60 // §7.3.13
	cmdTgInitAsTarget      = 0x8C // §7.3.14
	cmdTgSetGeneralBytes   = 0x92 // §7.3.15
	cmdTgGetData           = 0x86 // §7.3.16
	cmdTgSetData           = 0x8E // §7.3.17
	cmdTgSetMetaData       = 0x94 // §7.3.18
	cmdTgGetInitiatorCmd   = 0x88 // §7.3.19
	cmdTgResponseToInit    = 0x90 // §7.3.20
	cmdTgGetTargetStatus   = 0x8A // §7.3.21
)

// PN532 Error codes from PN532 User Manual §7.1 (Table 13)
const (
	errTimeout           = 0x01 // Time Out
	errCRC               = 0x02 // CRC error
	errParity            = 0x03 // Parity error
	errBitCount          = 0x04 // Erroneous Bit Count
	errMifare            = 0x05 // Mifare framing error
	errCollision         = 0x06 // Abnormal Bit Collision
	errBufferSize        = 0x07 // Buffer size insufficient
	errBufferOverflow    = 0x09 // RF Buffer overflow
	errRFFieldTimeout    = 0x0A // RF field not switched on in time
	errRFProtocol        = 0x0B // RF protocol error
	errTemperature       = 0x0D // Temperature error
	errInternalBuffer    = 0x0E // Internal buffer overflow
	errInvalidParam      = 0x10 // Invalid parameter
	errDepProtocol       = 0x12 // DEP protocol error
	errDataFormat        = 0x13 // Data format error
	errMifareAuth        = 0x14 // Mifare authentication error
	errUIDCheckByte      = 0x23 // UID Check byte is wrong
	errDepState          = 0x25 // Invalid device state
	errOperationNotAllow = 0x26 // Operation not allowed
	errCommand           = 0x27 // Command not acceptable
	errTarget            = 0x29 // Target released by initiator
	errIDMismatch        = 0x2A // Card ID mismatch
	errCardDisappeared   = 0x2B // Card disappeared
	errNFCIDMismatch     = 0x2C // NFCID3 initiator/target mismatch
	errOverCurrent       = 0x2D // Over-current event
	errNADMissing        = 0x2E // NAD missing in DEP frame
)

// SimulatorPowerMode represents the PN532 power state (§3.1.2)
type SimulatorPowerMode int

const (
	PowerModeNormal    SimulatorPowerMode = iota // CPU running
	PowerModePowerDown                           // Oscillator stopped
)

// SimulatorState tracks the internal state of the simulated PN532
type SimulatorState struct {
	PowerMode      SimulatorPowerMode
	RFFieldOn      bool
	SAMConfigured  bool
	SelectedTarget int // -1 = none, 1-2 = target number
}

// VirtualPN532 simulates a PN532 chip at the wire protocol level.
// It implements io.ReadWriter to plug directly into transport layer tests.
//
// The simulator enforces the PN532 protocol as specified in the User Manual:
// - Frame format validation (checksums, structure)
// - ACK/NACK handshake
// - State machine (power modes, RF field, target selection)
// - Command-specific responses
type VirtualPN532 struct {
	preACKGarbage       []byte
	tags                []*VirtualTag
	lastResponse        []byte
	rxBuffer            bytes.Buffer
	txBuffer            bytes.Buffer
	state               SimulatorState
	mu                  syncutil.Mutex
	firmwareIC          byte
	firmwareRev         byte
	firmwareSupport     byte
	injectChecksumError bool
	injectNACK          bool
	dropNextACK         bool
	zombieMode          bool
	firmwareVer         byte
	// Power glitch simulation - truncates response after N bytes (0 = disabled)
	powerGlitchAfterBytes int
	// Collision mode - when enabled, InListPassiveTarget returns collision error
	// if multiple tags are present
	collisionMode bool
	// ACK failure simulation - number of ACKs to skip before succeeding
	// Used to test transport retry logic
	ackFailuresRemaining int
}

// NewVirtualPN532 creates a new wire-level PN532 simulator.
// The simulator starts in PowerModeNormal with no RF field and no tags.
func NewVirtualPN532() *VirtualPN532 {
	return &VirtualPN532{
		state: SimulatorState{
			PowerMode:      PowerModeNormal,
			RFFieldOn:      false,
			SAMConfigured:  false,
			SelectedTarget: -1,
		},
		// Default firmware version: PN532 v1.6 (from manual §7.2.2)
		firmwareIC:      0x32,
		firmwareVer:     0x01,
		firmwareRev:     0x06,
		firmwareSupport: 0x07,
	}
}

// Write implements io.Writer - receives data from the host controller.
// This parses incoming frames and generates appropriate responses.
func (v *VirtualPN532) Write(data []byte) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	// Append to receive buffer
	_, _ = v.rxBuffer.Write(data)

	// Try to process complete frames
	if err := v.processReceivedData(); err != nil {
		return len(data), err
	}

	return len(data), nil
}

// Read implements io.Reader - returns response data to the host controller.
func (v *VirtualPN532) Read(buf []byte) (int, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.txBuffer.Len() == 0 {
		return 0, nil // No data available
	}

	n, err := v.txBuffer.Read(buf)
	if err != nil {
		return n, fmt.Errorf("read from tx buffer: %w", err)
	}
	return n, nil
}

// AddTag adds a virtual tag that can be detected by InListPassiveTarget.
func (v *VirtualPN532) AddTag(tag *VirtualTag) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.tags = append(v.tags, tag)
}

// SetTag is a convenience method that removes all existing tags and adds a single tag.
// Useful for test scenarios where only one tag is needed.
func (v *VirtualPN532) SetTag(tag *VirtualTag) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.tags = []*VirtualTag{tag}
	v.state.SelectedTarget = -1
}

// RemoveAllTags removes all virtual tags.
func (v *VirtualPN532) RemoveAllTags() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.tags = nil
	v.state.SelectedTarget = -1
}

// SetFirmwareVersion configures the firmware version returned by GetFirmwareVersion.
func (v *VirtualPN532) SetFirmwareVersion(ic, ver, rev, support byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.firmwareIC = ic
	v.firmwareVer = ver
	v.firmwareRev = rev
	v.firmwareSupport = support
}

// InjectChecksumError causes the next response to have an invalid checksum.
func (v *VirtualPN532) InjectChecksumError() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.injectChecksumError = true
}

// InjectNACK causes the simulator to expect a NACK and retransmit.
func (v *VirtualPN532) InjectNACK() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.injectNACK = true
}

// DropNextACK causes the simulator to not send ACK for the next command.
func (v *VirtualPN532) DropNextACK() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.dropNextACK = true
}

// InjectPreACKGarbage causes the simulator to inject the specified garbage bytes
// before the next ACK frame. This tests the UART transport's tryProcessPreAckData
// handling, which was a critical Windows bug fix for garbage data appearing
// before ACK frames on noisy UART lines.
func (v *VirtualPN532) InjectPreACKGarbage(garbage []byte) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.preACKGarbage = make([]byte, len(garbage))
	copy(v.preACKGarbage, garbage)
}

// SetZombieMode enables or disables zombie mode. When enabled, the simulator
// will ACK commands but never send the response frame. This tests timeout
// handling and recovery when the PN532 becomes unresponsive after acknowledging
// a command (e.g., due to power issues, hardware fault, or firmware hang).
func (v *VirtualPN532) SetZombieMode(enabled bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.zombieMode = enabled
}

// SetPowerGlitch configures the simulator to simulate a power glitch by
// truncating the next response frame after the specified number of bytes.
// This tests recovery code when power is interrupted mid-communication.
// Set to 0 to disable power glitch simulation.
//
// Example: SetPowerGlitch(3) would truncate a response like:
//
//	[00 00 FF 04 FC D5 03 ...] -> [00 00 FF]
//
// leaving an incomplete frame that the transport must handle.
func (v *VirtualPN532) SetPowerGlitch(afterBytes int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.powerGlitchAfterBytes = afterBytes
}

// SetCollisionMode enables or disables collision simulation mode.
// When enabled and multiple tags are present, InListPassiveTarget will
// return a collision error (0x06) instead of detecting tags.
// This simulates the real PN532 behavior when multiple tags are in the RF field
// and the anti-collision algorithm fails.
func (v *VirtualPN532) SetCollisionMode(enabled bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.collisionMode = enabled
}

// SetACKFailures configures the simulator to skip sending ACK frames for
// the specified number of commands. After that many failures, ACKs resume.
// This tests transport-level retry logic when ACK is not received.
//
// Example: SetACKFailures(2) causes the first 2 commands to not receive ACK,
// forcing the transport to retry. The 3rd attempt will receive ACK normally.
func (v *VirtualPN532) SetACKFailures(count int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.ackFailuresRemaining = count
}

// GetState returns the current simulator state.
func (v *VirtualPN532) GetState() SimulatorState {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.state
}

// HasPendingResponse returns true if the simulator has response data waiting to be read.
// This is useful for I2C ready status simulation.
func (v *VirtualPN532) HasPendingResponse() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.txBuffer.Len() > 0
}

// Reset clears all state and buffers.
func (v *VirtualPN532) Reset() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.rxBuffer.Reset()
	v.txBuffer.Reset()
	v.lastResponse = nil
	v.state = SimulatorState{
		PowerMode:      PowerModeNormal,
		RFFieldOn:      false,
		SAMConfigured:  false,
		SelectedTarget: -1,
	}
	v.injectChecksumError = false
	v.injectNACK = false
	v.dropNextACK = false
	v.preACKGarbage = nil
	v.zombieMode = false
	v.powerGlitchAfterBytes = 0
	v.collisionMode = false
	v.ackFailuresRemaining = 0
}

// processReceivedData parses frames from the receive buffer and generates responses.
//
//nolint:gocognit,revive // Protocol parsing requires multiple conditions
func (v *VirtualPN532) processReceivedData() error {
	for {
		data := v.rxBuffer.Bytes()
		if len(data) < 6 {
			return nil // Not enough data for minimum frame
		}

		// Check for ACK frame (§6.2.1.3)
		if bytes.HasPrefix(data, ACKFrame) {
			v.rxBuffer.Next(len(ACKFrame))
			// ACK received - abort current operation per §6.2.2.1.d
			continue
		}

		// Check for NACK frame (§6.2.1.4)
		if bytes.HasPrefix(data, NACKFrame) {
			v.rxBuffer.Next(len(NACKFrame))
			// Retransmit last response
			if v.lastResponse != nil {
				v.txBuffer.Write(v.lastResponse)
			}
			continue
		}

		// Find frame start (0x00 0xFF pattern per §6.2.1.6)
		startIdx := v.findFrameStart(data)
		if startIdx < 0 {
			// No valid frame start found, discard preamble bytes
			v.rxBuffer.Reset()
			return nil
		}
		if startIdx > 0 {
			v.rxBuffer.Next(startIdx)
			data = v.rxBuffer.Bytes()
		}

		// Parse and validate frame
		frame, frameLen, err := v.parseFrame(data)
		if err != nil {
			if errors.Is(err, errIncompleteFrame) {
				return nil // Wait for more data
			}
			// Frame error - send NACK behavior would go here
			// For now, discard bad data
			v.rxBuffer.Next(1)
			continue
		}

		// Consume the frame from buffer
		v.rxBuffer.Next(frameLen)

		// Process the command
		if err := v.processCommand(frame); err != nil {
			return err
		}
	}
}

var errIncompleteFrame = errors.New("incomplete frame")

// findFrameStart locates the 0x00 0xFF start code pattern (§6.2.1.6)
func (*VirtualPN532) findFrameStart(data []byte) int {
	for i := range len(data) - 1 {
		if data[i] == pn532StartCode1 && data[i+1] == pn532StartCode2 {
			return i
		}
	}
	return -1
}

// parseFrame validates and extracts a frame from the buffer.
// Returns the frame data (TFI + command + params), total frame length consumed, and any error.
func (v *VirtualPN532) parseFrame(data []byte) (resultData []byte, resultLen int, resultErr error) {
	// Minimum frame: START(2) + LEN(1) + LCS(1) + TFI(1) + DCS(1) = 6 bytes
	if len(data) < 6 {
		return nil, 0, errIncompleteFrame
	}

	// Validate start code (§6.2.1.1)
	if data[0] != pn532StartCode1 || data[1] != pn532StartCode2 {
		return nil, 0, errors.New("invalid start code")
	}

	offset := 2

	// Check for extended frame (§6.2.1.2)
	if data[offset] == extendedFrameMarker && data[offset+1] == extendedFrameMarker {
		return v.parseExtendedFrame(data)
	}

	// Normal frame
	frameLen := int(data[offset])
	lcs := data[offset+1]

	// Validate length checksum: LEN + LCS = 0x00 (§6.2.1.1)
	if (byte(frameLen)+lcs)&0xFF != 0 { //nolint:gosec // intentional truncation for checksum validation
		return nil, 0, errors.New("length checksum error")
	}

	// Check we have complete frame
	// Frame structure: START(2) + LEN(1) + LCS(1) + DATA(frameLen) + DCS(1) + POSTAMBLE(1)
	totalLen := 2 + 1 + 1 + frameLen + 1 + 1
	if len(data) < totalLen {
		return nil, 0, errIncompleteFrame
	}

	// Extract frame data (TFI + command + params)
	frameData := data[offset+2 : offset+2+frameLen]

	// Validate data checksum: TFI + PD0 + ... + PDn + DCS = 0x00 (§6.2.1.1)
	dcsOffset := offset + 2 + frameLen
	dcs := data[dcsOffset]
	checksum := byte(0)
	for _, b := range frameData {
		checksum += b
	}
	checksum += dcs
	if checksum != 0 {
		return nil, 0, errors.New("data checksum error")
	}

	// Validate TFI for host-to-PN532 direction
	if frameData[0] != tfiHostToPN532 {
		return nil, 0, fmt.Errorf("invalid TFI: expected 0x%02X, got 0x%02X", tfiHostToPN532, frameData[0])
	}

	return frameData, totalLen, nil
}

// parseExtendedFrame handles extended information frames (§6.2.1.2)
func (*VirtualPN532) parseExtendedFrame(data []byte) (resultData []byte, resultLen int, resultErr error) {
	// Extended frame: START(2) + 0xFF(1) + 0xFF(1) + LENM(1) + LENL(1) + LCS(1) + DATA + DCS(1) + POSTAMBLE(1)
	if len(data) < 9 {
		return nil, 0, errIncompleteFrame
	}

	lenM := int(data[4])
	lenL := int(data[5])
	frameLen := lenM*256 + lenL
	lcs := data[6]

	// Validate extended length checksum: LENM + LENL + LCS = 0x00
	if (byte(lenM)+byte(lenL)+lcs)&0xFF != 0 { //nolint:gosec // intentional truncation for checksum validation
		return nil, 0, errors.New("extended length checksum error")
	}

	// Check we have complete frame
	totalLen := 2 + 2 + 2 + 1 + frameLen + 1 + 1
	if len(data) < totalLen {
		return nil, 0, errIncompleteFrame
	}

	// Extract and validate frame data
	frameData := data[7 : 7+frameLen]

	dcsOffset := 7 + frameLen
	dcs := data[dcsOffset]
	checksum := byte(0)
	for _, b := range frameData {
		checksum += b
	}
	checksum += dcs
	if checksum != 0 {
		return nil, 0, errors.New("extended data checksum error")
	}

	if frameData[0] != tfiHostToPN532 {
		return nil, 0, errors.New("invalid TFI in extended frame")
	}

	return frameData, totalLen, nil
}

// processCommand handles a parsed command frame.
// frameData contains: TFI(1) + Command(1) + Params(n)
//
//nolint:revive,cyclop,gocyclo // High cyclomatic complexity expected for command dispatch
func (v *VirtualPN532) processCommand(frameData []byte) error {
	if len(frameData) < 2 {
		return v.sendErrorFrame()
	}

	// Inject pre-ACK garbage if configured (for testing UART garbage handling)
	if len(v.preACKGarbage) > 0 {
		v.txBuffer.Write(v.preACKGarbage)
		v.preACKGarbage = nil
	}

	// ACK failure simulation - skip ACK to test transport retry logic
	if v.ackFailuresRemaining > 0 {
		v.ackFailuresRemaining--
		return nil // Don't send ACK or response, forcing transport to retry
	}

	// Send ACK first (unless testing ACK drop)
	if !v.dropNextACK {
		v.txBuffer.Write(ACKFrame)
	}
	v.dropNextACK = false

	// In zombie mode, ACK is sent but response is never generated
	// This simulates a PN532 that hangs after acknowledging a command
	if v.zombieMode {
		return nil
	}

	cmd := frameData[1]
	params := frameData[2:]

	// Dispatch command
	var response []byte
	var err error

	switch cmd {
	case cmdGetFirmwareVersion:
		response, err = v.handleGetFirmwareVersion()
	case cmdSAMConfiguration:
		response, err = v.handleSAMConfiguration(params)
	case cmdInListPassiveTarget:
		response, err = v.handleInListPassiveTarget(params)
	case cmdInDataExchange:
		response, err = v.handleInDataExchange(params)
	case cmdInRelease:
		response, err = v.handleInRelease(params)
	case cmdInSelect:
		response, err = v.handleInSelect(params)
	case cmdPowerDown:
		response, err = v.handlePowerDown(params)
	case cmdRFConfiguration:
		response, err = v.handleRFConfiguration(params)
	case cmdGetGeneralStatus:
		response, err = v.handleGetGeneralStatus()
	case cmdSetParameters:
		response, err = v.handleSetParameters(params)
	case cmdInCommunicateThru:
		response, err = v.handleInCommunicateThru(params)
	default:
		// Unknown command - send syntax error (§6.2.2.2.c)
		return v.sendErrorFrame()
	}

	if err != nil {
		return err
	}

	// Build and send response frame
	return v.sendResponse(cmd, response)
}

// sendResponse builds and sends a response frame.
// Response command code = request command code + 1 (per PN532 protocol)
func (v *VirtualPN532) sendResponse(cmd byte, data []byte) error {
	responseCmd := cmd + 1
	frameData := append([]byte{tfiPN532ToHost, responseCmd}, data...)

	frame := v.buildFrame(frameData)

	// Apply error injection
	if v.injectChecksumError {
		v.injectChecksumError = false
		// Corrupt the DCS byte
		if len(frame) > 2 {
			frame[len(frame)-2] ^= 0xFF
		}
	}

	// Apply power glitch simulation - truncate frame mid-transmission
	if v.powerGlitchAfterBytes > 0 && v.powerGlitchAfterBytes < len(frame) {
		frame = frame[:v.powerGlitchAfterBytes]
		v.powerGlitchAfterBytes = 0 // One-shot behavior
	}

	v.lastResponse = frame
	_, _ = v.txBuffer.Write(frame)

	return nil
}

// sendErrorFrame sends a syntax error frame (§6.2.1.5)
// The error frame is a fixed frame [00 00 FF 01 FF 7F 81 00] that signals
// a syntax/application-level error. It does NOT carry a specific error code.
// Specific error codes (Table 13) are returned in response Status bytes, not here.
func (v *VirtualPN532) sendErrorFrame() error {
	// Fixed error frame format per PN532 manual and node-pn532 reference:
	// - LEN = 0x01 (just the TFI byte)
	// - LCS = 0xFF (0x01 + 0xFF = 0x100, lower byte = 0)
	// - TFI = 0x7F (error frame identifier)
	// - DCS = 0x81 (0x7F + 0x81 = 0x100, lower byte = 0)
	frame := []byte{
		pn532Preamble,
		pn532StartCode1, pn532StartCode2,
		0x01,     // LEN (just TFI)
		0xFF,     // LCS
		tfiError, // 0x7F
		0x81,     // DCS
		pn532Postamble,
	}

	v.lastResponse = frame
	_, _ = v.txBuffer.Write(frame)
	return nil
}

// buildFrame constructs a complete frame from TFI + data.
func (*VirtualPN532) buildFrame(frameData []byte) []byte {
	dataLen := len(frameData)

	// Use extended frame if needed (§6.2.1.2)
	if dataLen > 255 {
		return buildExtendedFrame(frameData)
	}

	// Calculate checksums
	lcs := byte(0 - dataLen) //nolint:gosec // intentional two's complement checksum

	dcs := byte(0)
	for _, b := range frameData {
		dcs += b
	}
	dcs = byte(0 - int(dcs)) // sum + DCS = 0

	// Build frame: PREAMBLE + START + LEN + LCS + DATA + DCS + POSTAMBLE
	frame := make([]byte, 0, 3+2+dataLen+2)
	frame = append(frame, pn532Preamble, pn532StartCode1, pn532StartCode2, byte(dataLen), lcs)
	frame = append(frame, frameData...)
	frame = append(frame, dcs, pn532Postamble)

	return frame
}

func buildExtendedFrame(frameData []byte) []byte {
	dataLen := len(frameData)
	lenM := byte(dataLen >> 8) //nolint:gosec // intentional truncation for extended frame
	lenL := byte(dataLen & 0xFF)
	lcs := byte(0 - int(lenM) - int(lenL)) //nolint:gosec // intentional two's complement checksum

	dcs := byte(0)
	for _, b := range frameData {
		dcs += b
	}
	dcs = byte(0 - int(dcs))

	frame := make([]byte, 0, 8+dataLen+2)
	frame = append(frame, pn532Preamble, pn532StartCode1, pn532StartCode2,
		extendedFrameMarker, extendedFrameMarker, lenM, lenL, lcs)
	frame = append(frame, frameData...)
	frame = append(frame, dcs, pn532Postamble)

	return frame
}

// Command handlers

// handleGetFirmwareVersion returns firmware version info (§7.2.2)
// Response: IC + Ver + Rev + Support
func (v *VirtualPN532) handleGetFirmwareVersion() ([]byte, error) {
	return []byte{v.firmwareIC, v.firmwareVer, v.firmwareRev, v.firmwareSupport}, nil
}

// handleSAMConfiguration configures the SAM (§7.2.10)
// Input: Mode [Timeout] [IRQ]
// Response: (empty - just acknowledgment)
func (v *VirtualPN532) handleSAMConfiguration(params []byte) ([]byte, error) {
	if len(params) < 1 {
		return nil, v.sendErrorFrame()
	}

	mode := params[0]
	switch mode {
	case 0x01: // Normal mode
		v.state.SAMConfigured = true
		v.state.PowerMode = PowerModeNormal
	case 0x02: // Virtual Card mode
		v.state.SAMConfigured = true
	case 0x03: // Wired Card mode
		v.state.SAMConfigured = true
	case 0x04: // Dual Card mode
		v.state.SAMConfigured = true
	default:
		return nil, v.sendErrorFrame()
	}

	return []byte{}, nil
}

// handleInListPassiveTarget detects passive targets (§7.3.5)
// Input: MaxTg + BrTy + [InitiatorData]
// Response: NbTg + [TargetData1] + [TargetData2]
//
//nolint:gocognit,revive // Protocol handling requires multiple condition checks
func (v *VirtualPN532) handleInListPassiveTarget(params []byte) ([]byte, error) {
	if len(params) < 2 {
		return nil, v.sendErrorFrame()
	}

	maxTg := params[0]
	brTy := params[1]

	// Validate MaxTg (§7.3.5 Syntax Error Conditions)
	if maxTg == 0 || maxTg > 2 {
		return nil, v.sendErrorFrame()
	}

	// Validate BrTy
	if brTy > 0x04 {
		return nil, v.sendErrorFrame()
	}

	// Turn on RF field (implied by this command)
	v.state.RFFieldOn = true

	// Count present tags
	presentTagCount := 0
	for _, tag := range v.tags {
		if tag.Present {
			presentTagCount++
		}
	}

	// Collision mode: if multiple tags are present, return collision error
	// This simulates real PN532 behavior when anti-collision fails
	if v.collisionMode && presentTagCount > 1 {
		// Return response with collision error status (0x06)
		// Per §7.1 Table 13: errCollision = 0x06 (Abnormal Bit Collision)
		// When collision occurs during anti-collision, NbTg=0 with error
		return []byte{0x00}, nil // No tags detected due to collision
	}

	// Find matching tags
	var targetData []byte
	nbTg := byte(0)

	for i, tag := range v.tags {
		if !tag.Present {
			continue
		}
		if nbTg >= maxTg {
			break
		}

		// Build target data based on baud rate type
		tgData := v.buildTargetData(byte(i+1), brTy, tag)
		if tgData != nil {
			targetData = append(targetData, tgData...)
			nbTg++
			if nbTg == 1 {
				v.state.SelectedTarget = i + 1
			}
		}
	}

	response := append([]byte{nbTg}, targetData...)
	return response, nil
}

// buildTargetData constructs TargetData for InListPassiveTarget response
func (*VirtualPN532) buildTargetData(tg, brTy byte, tag *VirtualTag) []byte {
	switch brTy {
	case 0x00: // 106 kbps Type A
		// Format: Tg + SENS_RES(2) + SEL_RES(1) + NFCIDLength(1) + NFCID1t
		// Example from manual: 01 04 00 08 04 92 2E 58 32
		sensRes := []byte{0x04, 0x00} // Default SENS_RES (ATQA)
		selRes := byte(0x08)          // Default SEL_RES (SAK) - MIFARE Classic 1K

		// Adjust based on tag type
		switch tag.Type {
		case "NTAG213", "NTAG215", "NTAG216":
			sensRes = []byte{0x44, 0x00}
			selRes = 0x00 // NFC Forum Type 2 Tag
		case "MIFARE1K":
			sensRes = []byte{0x04, 0x00}
			selRes = 0x08
		case "MIFARE4K":
			sensRes = []byte{0x02, 0x00}
			selRes = 0x18
		}

		data := make([]byte, 0, 1+len(sensRes)+2+len(tag.UID))
		data = append(data, tg)
		data = append(data, sensRes...)
		data = append(data, selRes, byte(len(tag.UID))) //nolint:gosec // NFC UIDs are max 10 bytes
		data = append(data, tag.UID...)
		return data

	case 0x01, 0x02: // 212/424 kbps FeliCa
		// Format: Tg + POL_RES_length + 0x01 + NFCID2t(8) + Pad(8) + [SYST_CODE(2)]
		if len(tag.UID) < 8 {
			return nil
		}
		data := make([]byte, 0, 3+8+8)
		data = append(data, tg, 18, 0x01)                                   // Tg + POL_RES length + response code
		data = append(data, tag.UID[:8]...)                                 // IDm (NFCID2t)
		data = append(data, 0x00, 0xF0, 0xFF, 0xFF, 0xFF, 0xFF, 0x00, 0x00) // PMm (Pad)
		return data

	case 0x03: // 106 kbps Type B
		// Not commonly used in this library
		return nil

	case 0x04: // Jewel
		// Format: Tg + ATQA_RES(2) + RID_RES(4)
		data := []byte{tg}
		data = append(data, 0x00, 0x0C) // ATQA
		if len(tag.UID) >= 4 {
			data = append(data, tag.UID[:4]...)
		} else {
			data = append(data, 0x00, 0x00, 0x00, 0x00)
		}
		return data
	}

	return nil
}

// handleInDataExchange exchanges data with a target (§7.3.8)
// Input: Tg + DataOut
// Response: Status + DataIn
func (v *VirtualPN532) handleInDataExchange(params []byte) ([]byte, error) {
	if len(params) < 2 {
		return nil, v.sendErrorFrame()
	}

	tg := int(params[0])
	dataOut := params[1:]

	// Validate target selection
	if v.state.SelectedTarget < 1 {
		return []byte{errTarget}, nil // No target selected
	}
	if tg != v.state.SelectedTarget {
		return []byte{errTarget}, nil // Wrong target
	}

	// Find the tag
	tagIdx := tg - 1
	if tagIdx < 0 || tagIdx >= len(v.tags) {
		return []byte{errTarget}, nil
	}

	tag := v.tags[tagIdx]
	if !tag.Present {
		return []byte{errCardDisappeared}, nil
	}

	// Process the command (simplified - just echo for now)
	// Real implementation would interpret MIFARE/NTAG commands
	response, err := v.processTagCommand(tag, dataOut)
	if err != nil {
		// PN532 protocol returns error codes as data, not Go errors
		return []byte{errMifare}, nil //nolint:nilerr // error code returned in data per PN532 protocol
	}

	return append([]byte{0x00}, response...), nil // Status 0x00 = success
}

// processTagCommand handles tag-specific commands
//
//nolint:cyclop,gocognit,gocyclo,revive // Tag command handling requires multiple conditions
func (*VirtualPN532) processTagCommand(tag *VirtualTag, cmd []byte) ([]byte, error) {
	if len(cmd) == 0 {
		return nil, errors.New("empty command")
	}

	// MIFARE/NTAG command handling
	switch cmd[0] {
	case 0x30: // READ (returns 16 bytes: 4 consecutive pages for NTAG, or 1 block for MIFARE)
		if len(cmd) < 2 {
			return nil, errors.New("invalid read command")
		}
		page := int(cmd[1])

		// NTAG READ returns 4 consecutive 4-byte pages (16 bytes total)
		if tag.isNTAG() {
			return tag.ReadNTAGPages(page, 4)
		}

		// MIFARE READ returns a single 16-byte block
		data, err := tag.ReadBlock(page)
		if err != nil {
			return nil, err
		}
		return data, nil

	case 0xA2: // WRITE (4 bytes to block for NTAG)
		if len(cmd) < 6 {
			return nil, errors.New("invalid write command")
		}
		block := int(cmd[1])
		// NTAG writes 4 bytes
		data := make([]byte, 16)
		copy(data, cmd[2:6])
		if err := tag.WriteBlock(block, data); err != nil {
			return nil, err
		}
		return []byte{0x0A}, nil // ACK

	case 0x3A: // FAST_READ (NTAG specific - read multiple pages)
		if len(cmd) < 3 {
			return nil, errors.New("invalid fast_read command")
		}
		startPage := int(cmd[1])
		endPage := int(cmd[2])

		// Check if tag supports FAST_READ (clone tags typically don't)
		if !tag.SupportsFastRead() {
			return nil, errors.New("FAST_READ not supported by this tag")
		}

		if startPage > endPage {
			return nil, errors.New("invalid page range")
		}

		// Read all pages from start to end (inclusive)
		numPages := endPage - startPage + 1
		return tag.ReadNTAGPages(startPage, numPages)

	case 0x60, 0x61: // MIFARE Auth A/B
		if len(cmd) < 8 {
			return nil, errors.New("invalid auth command")
		}
		block := int(cmd[1])
		sector := block / 4
		key := cmd[2:8]
		keyType := byte(MIFAREKeyA)
		if cmd[0] == 0x61 {
			keyType = MIFAREKeyB
		}
		if err := tag.Authenticate(sector, keyType, key); err != nil {
			return nil, err
		}
		return []byte{}, nil // Success (empty response)

	default:
		// Unknown command - pass through
		return []byte{}, nil
	}
}

// handleInRelease releases a target (§7.3.11)
// Input: Tg
// Response: Status
func (v *VirtualPN532) handleInRelease(params []byte) ([]byte, error) {
	if len(params) < 1 {
		return nil, v.sendErrorFrame()
	}

	tg := int(params[0])
	switch tg {
	case 0:
		// Release all targets
		v.state.SelectedTarget = -1
		for _, tag := range v.tags {
			tag.ResetAuthentication()
		}
	case v.state.SelectedTarget:
		tagIdx := tg - 1
		if tagIdx >= 0 && tagIdx < len(v.tags) {
			v.tags[tagIdx].ResetAuthentication()
		}
		v.state.SelectedTarget = -1
	}

	return []byte{0x00}, nil // Success
}

// handleInSelect selects a target for communication (§7.3.12)
// Input: Tg
// Response: Status
func (v *VirtualPN532) handleInSelect(params []byte) ([]byte, error) {
	if len(params) < 1 {
		return nil, v.sendErrorFrame()
	}

	tg := int(params[0])
	if tg < 1 || tg > len(v.tags) {
		return []byte{errTarget}, nil
	}

	if !v.tags[tg-1].Present {
		return []byte{errCardDisappeared}, nil
	}

	v.state.SelectedTarget = tg
	return []byte{0x00}, nil
}

// handlePowerDown puts the PN532 into power-down mode (§7.2.11)
// Input: WakeUpEnable [GenerateIRQ]
// Response: Status
func (v *VirtualPN532) handlePowerDown(params []byte) ([]byte, error) {
	if len(params) < 1 {
		return nil, v.sendErrorFrame()
	}

	v.state.PowerMode = PowerModePowerDown
	v.state.SelectedTarget = -1
	v.state.RFFieldOn = false

	return []byte{0x00}, nil // Success
}

// handleRFConfiguration configures RF parameters (§7.3.1)
// Input: CfgItem + ConfigurationData
// Response: (empty)
func (v *VirtualPN532) handleRFConfiguration(params []byte) ([]byte, error) {
	if len(params) < 1 {
		return nil, v.sendErrorFrame()
	}

	cfgItem := params[0]
	if cfgItem == 0x01 { // RF Field
		if len(params) < 2 {
			return nil, v.sendErrorFrame()
		}
		v.state.RFFieldOn = params[1] != 0x00
	}
	// Other config items can be added as elif cases here

	return []byte{}, nil
}

// handleGetGeneralStatus returns general status (§7.2.3)
// Response: Err + Field + NbTg + [Tg + BrRx + BrTx + Type]... + SAMStatus
func (v *VirtualPN532) handleGetGeneralStatus() ([]byte, error) {
	field := byte(0x00)
	if v.state.RFFieldOn {
		field = 0x01
	}

	nbTg := byte(0)
	if v.state.SelectedTarget > 0 {
		nbTg = 1
	}

	response := []byte{
		0x00,  // Err (no error)
		field, // Field
		nbTg,  // NbTg
	}

	if nbTg > 0 {
		response = append(response,
			byte(v.state.SelectedTarget), //nolint:gosec // PN532 target index is 1 or 2
			0x00,                         // BrRx (106 kbps)
			0x00,                         // BrTx (106 kbps)
			0x00,                         // Type
		)
	}

	response = append(response, 0x00) // SAM status

	return response, nil
}

// handleSetParameters sets internal parameters (§7.2.9)
// Input: Flags
// Response: (empty)
func (*VirtualPN532) handleSetParameters(_ []byte) ([]byte, error) {
	// Accept any parameters for now
	return []byte{}, nil
}

// handleInCommunicateThru sends raw data through RF (§7.3.9)
// Input: DataOut
// Response: Status + DataIn
func (v *VirtualPN532) handleInCommunicateThru(params []byte) ([]byte, error) {
	if v.state.SelectedTarget < 1 {
		return []byte{errTarget}, nil
	}

	// Similar to InDataExchange but raw
	tagIdx := v.state.SelectedTarget - 1
	if tagIdx < 0 || tagIdx >= len(v.tags) {
		return []byte{errTarget}, nil
	}

	tag := v.tags[tagIdx]
	if !tag.Present {
		return []byte{errCardDisappeared}, nil
	}

	response, err := v.processTagCommand(tag, params)
	if err != nil {
		// PN532 protocol returns error codes as data, not Go errors
		return []byte{errMifare}, nil //nolint:nilerr // error code returned in data per PN532 protocol
	}

	return append([]byte{0x00}, response...), nil
}
