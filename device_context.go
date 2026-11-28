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
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Init initializes the PN532 device with context support
func (d *Device) Init(ctx context.Context) error {
	skipFirmwareVersion := d.shouldSkipFirmwareVersion()

	// Test with GetFirmwareVersion first to see if any PN532 commands work via PC/SC
	if !skipFirmwareVersion {
		d.tryFirmwareVersionCheck(ctx)
	}

	skipSAM := d.shouldSkipSAMConfiguration()
	if !skipSAM {
		if err := d.handleSAMConfiguration(ctx); err != nil {
			return err
		}
	}

	// Configure finite passive activation retries to prevent infinite wait lockups
	// Each retry is ~100ms per PN532 datasheet, so 10 retries = ~1 second
	if err := d.SetPassiveActivationRetries(ctx, DefaultPassiveActivationRetries); err != nil {
		// Log but don't fail initialization - this is an optimization, not critical
		// Some older firmware versions might not support this configuration
		_ = err
	}

	// Get firmware version (if supported by transport)
	if !skipFirmwareVersion {
		if err := d.setupFirmwareVersion(ctx); err != nil {
			return err
		}
	} else {
		d.setDefaultFirmwareVersion()
	}

	return nil
}

// Reset reinitializes the device connection after a power loss, sleep/wake cycle,
// or communication failure. This clears internal state and re-runs the initialization
// sequence (GetFirmwareVersion + SAMConfiguration).
//
// Use this when:
//   - Host device wakes from sleep
//   - PN532 module was power cycled
//   - Communication becomes unreliable
//
// If Reset fails, consider closing and reopening the transport entirely.
func (d *Device) Reset(ctx context.Context) error {
	// Clear internal state
	d.firmwareVersion = nil

	// Reinitialize the device
	return d.Init(ctx)
}

// HardReset performs a "nuclear" recovery by closing and reopening the transport
// connection, then reinitializing the PN532. This is the last resort when the PN532
// enters a complete firmware lockup (stops ACKing any commands).
//
// This recovery is more disruptive than Reset() but handles cases where the PN532
// firmware is completely unresponsive. It requires the transport to implement the
// Reconnecter interface.
//
// Returns an error if the transport doesn't support reconnection or if recovery fails.
func (d *Device) HardReset(ctx context.Context) error {
	Debugln("HardReset: attempting nuclear recovery")

	// Check if transport supports reconnection
	reconnecter, ok := d.transport.(Reconnecter)
	if !ok {
		return errors.New("transport does not support reconnection")
	}

	// Close and reopen the transport
	if err := reconnecter.Reconnect(); err != nil {
		return fmt.Errorf("HardReset reconnect failed: %w", err)
	}

	// Allow PN532 to boot after reconnection
	time.Sleep(50 * time.Millisecond)

	// Clear internal state
	d.firmwareVersion = nil

	// Reinitialize with SAMConfiguration
	if err := d.SAMConfiguration(ctx, SAMModeNormal, 0, 0); err != nil {
		return fmt.Errorf("HardReset SAMConfiguration failed: %w", err)
	}

	Debugln("HardReset: recovery successful")
	return nil
}

// shouldSkipFirmwareVersion checks if transport supports firmware version retrieval
func (*Device) shouldSkipFirmwareVersion() bool {
	// All transports now support firmware version retrieval
	return false
}

// tryFirmwareVersionCheck attempts to get firmware version for early validation
func (d *Device) tryFirmwareVersionCheck(ctx context.Context) {
	_, err := d.GetFirmwareVersion(ctx)
	if err != nil {
		// Continue with initialization even if GetFirmwareVersion fails
		// This is expected for some transports/clone devices
		_ = err // Explicitly ignore error
	}
}

// shouldSkipSAMConfiguration determines if SAM configuration should be skipped
func (*Device) shouldSkipSAMConfiguration() bool {
	// All transports now require SAM configuration
	return false
}

// handleSAMConfiguration performs SAM configuration with clone device error handling
func (d *Device) handleSAMConfiguration(ctx context.Context) error {
	err := d.setupSAMConfiguration(ctx)
	if err == nil {
		return nil
	}

	// Check if this looks like a clone device returning wrong response
	errStr := err.Error()
	if strings.Contains(errStr, "unexpected SAM configuration response code: 03") ||
		strings.Contains(errStr, "response too short") ||
		strings.Contains(errStr, "clone device returned empty response") {
		// Clone device returned wrong response format - this is common with some clones
		// Continue without SAM config as these devices often don't support it properly
		Debugf("Warning: Clone device detected (SAM config issue: %s), continuing without SAM configuration", errStr)
		return nil
	}

	return fmt.Errorf("SAM configuration failed: %w", err)
}

// setupFirmwareVersion retrieves and sets the firmware version
func (d *Device) setupFirmwareVersion(ctx context.Context) error {
	fw, err := d.GetFirmwareVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to get firmware version: %w", err)
	}
	d.firmwareVersion = fw
	return nil
}

// setDefaultFirmwareVersion creates a default firmware version for unsupported transports
func (d *Device) setDefaultFirmwareVersion() {
	d.firmwareVersion = &FirmwareVersion{
		Version:          "1.6", // Generic version for PC/SC mode
		SupportIso14443a: true,  // Assume basic NFC-A support
		SupportIso14443b: false, // Conservative defaults
		SupportIso18092:  false,
	}
}

// GetFirmwareVersion returns the PN532 firmware version
func (d *Device) GetFirmwareVersion(ctx context.Context) (*FirmwareVersion, error) {
	res, err := d.transport.SendCommand(ctx, cmdGetFirmwareVersion, []byte{})
	if err != nil {
		return nil, fmt.Errorf("failed to send GetFirmwareVersion command: %w", err)
	}

	d.debugFirmwareResponse(res)

	if len(res) < 5 {
		return nil, errors.New("unexpected firmware version response")
	}

	return d.parseFirmwareResponse(res)
}

// debugFirmwareResponse logs the firmware response for debugging
func (*Device) debugFirmwareResponse(res []byte) {
	Debugf("GetFirmwareVersion response: [%s] (len=%d)",
		strings.Join(func() []string {
			strs := make([]string, len(res))
			for i, b := range res {
				strs[i] = fmt.Sprintf("0x%02X", b)
			}
			return strs
		}(), " "), len(res))
}

// parseFirmwareResponse parses the firmware version response from various device types
func (d *Device) parseFirmwareResponse(res []byte) (*FirmwareVersion, error) {
	// Check for standard PN532 response format first
	if res[0] == 0x03 {
		return d.parseStandardFirmwareResponse(res)
	}

	// Handle unexpected response format validation
	if len(res) == 5 && res[0] != 0x03 {
		return nil, errors.New("unexpected firmware version response")
	}

	// Handle clone device variations
	return d.parseCloneFirmwareResponse(res)
}

// parseStandardFirmwareResponse parses standard PN532 firmware response
func (*Device) parseStandardFirmwareResponse(res []byte) (*FirmwareVersion, error) {
	if res[1] != 0x32 {
		return nil, fmt.Errorf("unexpected IC: %x", res[1])
	}
	return &FirmwareVersion{
		Version:          fmt.Sprintf("%d.%d", res[2], res[3]),
		SupportIso14443a: res[4]&0x01 == 0x01,
		SupportIso14443b: res[4]&0x02 == 0x02,
		SupportIso18092:  res[4]&0x04 == 0x04,
	}, nil
}

// parseCloneFirmwareResponse handles clone device firmware response variations
func (d *Device) parseCloneFirmwareResponse(res []byte) (*FirmwareVersion, error) {
	// Clone device returned SAM configuration response (0x15)
	if len(res) == 1 && res[0] == 0x15 {
		Debugln("Clone device returned SAM config response (0x15) for GetFirmwareVersion")
		return d.createDefaultFirmwareVersion(), nil
	}

	if len(res) >= 3 {
		// Try to extract version information from different positions
		if version := d.parseCloneD5Format(res); version != nil {
			return version, nil
		}

		// Fallback: Create a generic firmware version for compatibility
		Debugln("Using fallback firmware version for clone device")
		return d.createDefaultFirmwareVersion(), nil
	}

	return nil, fmt.Errorf("unexpected firmware version response: got %d bytes: %v", len(res), res)
}

// parseCloneD5Format parses clone devices with 0xD5 prefix
func (*Device) parseCloneD5Format(res []byte) *FirmwareVersion {
	if len(res) >= 5 && res[0] == 0xD5 && res[1] == 0x03 {
		// Some clones prefix with 0xD5 (response command byte)
		Debugln("Detected clone format with 0xD5 prefix")
		if len(res) >= 7 && res[2] == 0x32 {
			return &FirmwareVersion{
				Version:          fmt.Sprintf("%d.%d", res[3], res[4]),
				SupportIso14443a: res[5]&0x01 == 0x01,
				SupportIso14443b: res[5]&0x02 == 0x02,
				SupportIso18092:  res[5]&0x04 == 0x04,
			}
		}
	}
	return nil
}

// createDefaultFirmwareVersion creates a default firmware version for clone devices
func (*Device) createDefaultFirmwareVersion() *FirmwareVersion {
	return &FirmwareVersion{
		Version:          "1.6", // Generic version for clones
		SupportIso14443a: true,  // Assume basic NFC-A support
		SupportIso14443b: false, // Conservative defaults
		SupportIso18092:  false,
	}
}

// GetGeneralStatus returns the PN532 general status with context support
func (d *Device) GetGeneralStatus(ctx context.Context) (*GeneralStatus, error) {
	res, err := d.transport.SendCommand(ctx, cmdGetGeneralStatus, []byte{})
	if err != nil {
		return nil, fmt.Errorf("failed to send GetGeneralStatus command: %w", err)
	}
	if len(res) < 4 || res[0] != 0x05 {
		return nil, errors.New("unexpected general status response")
	}

	return &GeneralStatus{
		LastError:    res[1],
		FieldPresent: res[2] == 0x01,
		Targets:      res[3],
	}, nil
}

// Diagnose performs a self-diagnosis test with context support
func (d *Device) Diagnose(ctx context.Context, testNumber byte, data []byte) (*DiagnoseResult, error) {
	// Build command: TestNumber + optional data
	cmdPayload := append([]byte{testNumber}, data...)

	res, err := d.transport.SendCommand(ctx, cmdDiagnose, cmdPayload)
	if err != nil {
		return nil, fmt.Errorf("diagnose command failed: %w", err)
	}

	// Check response format
	if len(res) < 1 {
		return nil, errors.New("empty diagnose response")
	}

	result := &DiagnoseResult{
		TestNumber: testNumber,
	}

	// Special handling for ROM/RAM tests which return status byte wrapped by transport
	if testNumber == DiagnoseROMTest || testNumber == DiagnoseRAMTest {
		if len(res) != 2 || res[0] != 0x01 {
			return nil, fmt.Errorf("unexpected ROM/RAM diagnose response format: %v", res)
		}
		result.Data = res[1:]           // The single status byte
		result.Success = res[1] == 0x00 // 0x00 = OK, 0xFF = Not Good
		return result, nil
	}

	// Standard response should start with 0x01
	if res[0] != 0x01 {
		return nil, fmt.Errorf("unexpected diagnose response header: 0x%02X", res[0])
	}

	result.Data = res[1:]

	// Set Success flag based on test type
	switch testNumber {
	case DiagnoseCommunicationTest:
		// Communication test echoes back the entire command (test number + data)
		result.Success = bytes.Equal(result.Data, cmdPayload)
	case DiagnosePollingTest:
		// Returns number of failures (0 = all succeeded)
		if len(result.Data) == 0 {
			return nil, errors.New("empty data for polling test")
		}
		result.Success = result.Data[0] == 0
	case DiagnoseEchoBackTest:
		// Echo back test runs infinitely, so no response expected
		// If we get here, it means the test setup was successful
		result.Success = true
	case DiagnoseAttentionTest, DiagnoseSelfAntennaTest:
		// For these tests, if no error, assume success
		result.Success = true
	default:
		// Unknown test number, but got valid response
		result.Success = true
	}

	return result, nil
}

// setupSAMConfiguration configures the SAM with context support
func (d *Device) setupSAMConfiguration(ctx context.Context) error {
	return d.SAMConfiguration(ctx, SAMModeNormal, 0x00, 0x00)
}

// SAMConfiguration configures the SAM with context support
func (d *Device) SAMConfiguration(ctx context.Context, mode SAMMode, timeout, irq byte) error {
	res, err := d.transport.SendCommand(ctx, cmdSamConfiguration, []byte{byte(mode), timeout, irq})
	if err != nil {
		return fmt.Errorf("SAM configuration command failed: %w", err)
	}

	// Validate SAM configuration response
	if len(res) == 0 {
		return errors.New("empty SAM configuration response")
	}

	// Expected response: 0x15 (command response code)
	// Some transports may return additional data (e.g., PC/SC status words)
	if res[0] != 0x15 {
		return fmt.Errorf("unexpected SAM configuration response code: %02X, expected 0x15 (full response: %v)",
			res[0], res)
	}

	return nil
}

// DetectTag detects a single tag in the field with context support
func (d *Device) DetectTag(ctx context.Context) (*DetectedTag, error) {
	Debugln("Using InListPassiveTarget strategy")

	// Apply transport-specific optimizations and timing
	if err := d.prepareTransportForInListPassiveTarget(ctx); err != nil {
		Debugf("Transport preparation failed: %v", err)
		return nil, fmt.Errorf("transport preparation failed: %w", err)
	}

	// Release any previously selected targets to clear HALT states
	// This addresses intermittent "empty valid tag" issues where tags get stuck
	if err := d.InRelease(ctx); err != nil {
		Debugf("InRelease failed, continuing anyway: %v", err)
		// Don't fail the operation if InRelease fails - it's an optimization
	}

	// Small delay to allow RF field and tags to stabilize after release
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(10 * time.Millisecond):
	}

	return d.InListPassiveTarget(ctx, 0x00)
}

// prepareTransportForInListPassiveTarget applies transport-specific preparations for InListPassiveTarget
func (d *Device) prepareTransportForInListPassiveTarget(_ context.Context) error {
	// Apply transport-specific timing and RF field management
	switch d.transport.Type() {
	case TransportUART:
		// UART typically doesn't need special preparation
		return nil

	case TransportI2C, TransportSPI, TransportMock:
		// Other transports typically don't need special preparation
		return nil

	default:
		return nil
	}
}

// convertAutoPollResult converts a single InAutoPoll result to DetectedTag format
func (d *Device) convertAutoPollResult(result *AutoPollResult) *DetectedTag {
	if result == nil {
		return nil
	}

	// Parse the target data to extract UID, ATQ, SAK
	uid, atq, sak := d.parseTargetData(result.Type, result.TargetData)

	// Determine tag type based on AutoPoll target type first, then ATQ/SAK
	tagType := d.identifyTagTypeFromTarget(result.Type, atq, sak)

	return &DetectedTag{
		Type:       tagType,
		UID:        fmt.Sprintf("%x", uid),
		UIDBytes:   uid,
		ATQ:        atq,
		SAK:        sak,
		DetectedAt: time.Now(),
		TargetData: result.TargetData, // Store full target data for FeliCa
	}
}

// identifyTagTypeFromTarget identifies tag type from AutoPollTarget type and ATQ/SAK
func (d *Device) identifyTagTypeFromTarget(targetType AutoPollTarget, atq []byte, sak byte) TagType {
	// For FeliCa targets, we can determine the type directly from the AutoPollTarget
	switch targetType {
	case AutoPollGeneric212kbps, AutoPollGeneric424kbps, AutoPollFeliCa212, AutoPollFeliCa424:
		return TagTypeFeliCa
	case AutoPollGeneric106kbps, AutoPollMifare, AutoPollISO14443A:
		// For Type A targets, use ATQ/SAK identification
		return d.identifyTagType(atq, sak)
	case AutoPollJewel:
		// Jewel tags not yet fully supported
		return TagTypeUnknown
	case AutoPollISO14443B, AutoPollISO14443B4:
		// Type B tags not yet fully supported
		return TagTypeUnknown
	default:
		return TagTypeUnknown
	}
}

// parseTargetData extracts UID, ATQ, and SAK from InAutoPoll target data
// The format depends on the target type:
// - Type A (ISO14443A): [SENS_RES(2), SEL_RES(1), NFCID1_LEN(1), NFCID1...]
// - Type B (ISO14443B): [ATQB(11), ATTRIB_RES_LEN(1), ATTRIB_RES...]
// - FeliCa: [POL_RES(18) or POL_RES(20)]
func (d *Device) parseTargetData(targetType AutoPollTarget, targetData []byte) (uid, atq []byte, sak byte) {
	// Default values for unsupported formats
	uid = []byte{0x00, 0x00, 0x00, 0x00}
	atq = []byte{0x00, 0x00}
	sak = 0x00

	switch targetType {
	case AutoPollGeneric106kbps, AutoPollMifare, AutoPollISO14443A:
		uid, atq, sak = d.parseISO14443AData(targetData)
	case AutoPollJewel:
		uid = d.parseJewelData(targetData)
	case AutoPollGeneric212kbps, AutoPollGeneric424kbps, AutoPollFeliCa212, AutoPollFeliCa424:
		uid = d.parseFeliCaData(targetData)
	case AutoPollISO14443B, AutoPollISO14443B4:
		uid = d.parseISO14443BData(targetData)
	}

	return uid, atq, sak
}

// parseISO14443AData parses ISO14443 Type A target data
func (*Device) parseISO14443AData(targetData []byte) (uid, atq []byte, sak byte) {
	uid = []byte{0x00, 0x00, 0x00, 0x00}
	atq = []byte{0x00, 0x00}
	sak = 0x00

	if len(targetData) < 4 {
		return uid, atq, sak
	}

	// Parse ATQ and SAK from the first 3 bytes
	atq = targetData[0:2] // SENS_RES (ATQ)
	sak = targetData[2]   // SEL_RES (SAK)

	// Try parsing UID length at offset 3 first (test/mock format)
	// Format: ATQ(2) + SAK(1) + UID_LENGTH(1) + UID(n)
	if len(targetData) > 3 {
		uidLen := targetData[3]
		if uidLen > 0 && len(targetData) >= 4+int(uidLen) {
			uid = targetData[4 : 4+int(uidLen)]
			return uid, atq, sak
		}
	}

	// Try parsing UID length at offset 4 (real hardware format)
	// Format: ATQ(2) + SAK(1) + UNKNOWN(1) + UID_LENGTH(1) + UID(n)
	if len(targetData) > 4 {
		uidLen := targetData[4]
		if uidLen > 0 && len(targetData) >= 5+int(uidLen) {
			uid = targetData[5 : 5+int(uidLen)]
			return uid, atq, sak
		}
	}

	return uid, atq, sak
}

// parseJewelData parses Jewel target data
func (*Device) parseJewelData(targetData []byte) []byte {
	if len(targetData) >= 6 {
		return targetData[2:6] // UID (4 bytes)
	}
	return []byte{0x00, 0x00, 0x00, 0x00}
}

// parseFeliCaData parses FeliCa target data
func (*Device) parseFeliCaData(targetData []byte) []byte {
	if len(targetData) >= 18 {
		return targetData[2:10] // NFCID2 (8 bytes)
	}
	return []byte{0x00, 0x00, 0x00, 0x00}
}

// parseISO14443BData parses ISO14443 Type B target data
func (*Device) parseISO14443BData(targetData []byte) []byte {
	if len(targetData) >= 11 && len(targetData) >= 5 {
		return targetData[1:5] // PUPI acts as UID for Type B
	}
	return []byte{0x00, 0x00, 0x00, 0x00}
}

// SendDataExchange sends a data exchange command with context support
func (d *Device) SendDataExchange(ctx context.Context, data []byte) ([]byte, error) {
	const targetNum byte = 1
	if len(data) > 0 {
		Debugf("SendDataExchange: target=%d, data[0]=0x%02X, len=%d", targetNum, data[0], len(data))
	} else {
		Debugf("SendDataExchange: target=%d, data=(empty), len=0", targetNum)
	}
	res, err := d.transport.SendCommand(ctx, cmdInDataExchange, append([]byte{targetNum}, data...))
	if err != nil {
		return nil, fmt.Errorf("failed to send data exchange command: %w", err)
	}

	// Check for error frame (TFI = 0x7F)
	if len(res) >= 2 && res[0] == 0x7F {
		errorCode := res[1]
		return nil, NewPN532ErrorWithDetails(errorCode, "InDataExchange", len(data), targetNum)
	}

	if len(res) < 2 || res[0] != 0x41 {
		return nil, errors.New("unexpected data exchange response")
	}
	if res[1] != 0x00 {
		return nil, NewPN532ErrorWithDetails(res[1], "InDataExchange", len(data), targetNum)
	}
	return res[2:], nil
}

// isRetryableRFError checks if an error is an RF-related PN532 error that should be retried.
// This includes timeout (0x01) and RF communication errors (CRC, parity, framing, etc.)
// that commonly occur during card sliding into a reader slot.
func isRetryableRFError(err error) bool {
	var pn532Err *PN532Error
	if errors.As(err, &pn532Err) {
		return pn532Err.IsTimeoutError() || pn532Err.IsRFError()
	}
	return false
}

// SendDataExchangeWithRetry sends a data exchange command with automatic retry on RF errors.
// RF errors (timeout, CRC, parity, framing) indicate transient communication failures that
// commonly occur during card sliding into a reader slot. A small delay between retries
// allows the RF field to stabilize.
//
// Configuration:
//   - 3 attempts total (1 initial + 2 retries)
//   - 15ms delay between retries (allows RF stabilization)
//   - Retries on timeout (0x01) and RF errors (CRC, parity, framing, etc.)
func (d *Device) SendDataExchangeWithRetry(ctx context.Context, data []byte) ([]byte, error) {
	const maxAttempts = 3
	const retryDelay = 15 * time.Millisecond

	var lastErr error
	for attempt := range maxAttempts {
		if ctxErr := ctx.Err(); ctxErr != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, ctxErr
		}

		result, err := d.SendDataExchange(ctx, data)
		if err == nil {
			if attempt > 0 {
				Debugf("SendDataExchangeWithRetry: succeeded on attempt %d", attempt+1)
			}
			return result, nil
		}

		if !isRetryableRFError(err) {
			return nil, err
		}

		// RF error - save it and retry after delay
		lastErr = err
		if attempt < maxAttempts-1 {
			Debugf("SendDataExchangeWithRetry: RF error on attempt %d, retrying after %v", attempt+1, retryDelay)
			time.Sleep(retryDelay)
		}
	}

	return nil, lastErr
}

// SendRawCommand sends a raw command with context support
func (d *Device) SendRawCommand(ctx context.Context, data []byte) ([]byte, error) {
	const targetNum byte = 1
	res, err := d.transport.SendCommand(ctx, cmdInCommunicateThru, data)
	if err != nil {
		return nil, fmt.Errorf("failed to send communicate through command: %w", err)
	}

	// Check for error frame (TFI = 0x7F)
	if len(res) >= 2 && res[0] == 0x7F {
		errorCode := res[1]
		return nil, NewPN532ErrorWithDetails(errorCode, "InCommunicateThru", len(data), targetNum)
	}

	if len(res) < 2 || res[0] != 0x43 {
		return nil, errors.New("unexpected InCommunicateThru response")
	}
	if res[1] != 0x00 {
		return nil, NewPN532ErrorWithDetails(res[1], "InCommunicateThru", len(data), targetNum)
	}
	return res[2:], nil
}

// InRelease releases all selected targets with context support
func (d *Device) InRelease(ctx context.Context) error {
	// Always release all targets (target 0 = release all)
	res, err := d.transport.SendCommand(ctx, cmdInRelease, []byte{0x00})
	if err != nil {
		return fmt.Errorf("InRelease command failed: %w", err)
	}

	if len(res) != 2 || res[0] != 0x53 {
		return errors.New("unexpected InRelease response")
	}

	// Check status byte
	if res[1] != 0x00 {
		return fmt.Errorf("InRelease failed with status: %02x", res[1])
	}

	return nil
}

// InDeselect deselects all targets while keeping target information in PN532 memory.
// Unlike InRelease, this allows InSelect to re-select the target later.
// For MIFARE cards, this sends HLTA which puts the tag in HALT state.
// Use InSelect after InDeselect to wake up HALTed tags with WUPA.
func (d *Device) InDeselect(ctx context.Context) error {
	// Deselect all targets (target 0 = all targets)
	res, err := d.transport.SendCommand(ctx, cmdInDeselect, []byte{0x00})
	if err != nil {
		return fmt.Errorf("InDeselect command failed: %w", err)
	}

	if len(res) != 2 || res[0] != 0x45 {
		return errors.New("unexpected InDeselect response")
	}

	// Check status byte
	if res[1] != 0x00 {
		return fmt.Errorf("InDeselect failed with status: %02x", res[1])
	}

	return nil
}

// InSelect selects target 1 for communication with context support
func (d *Device) InSelect(ctx context.Context) error {
	const targetNumber byte = 1
	Debugf("InSelect: selecting target %d", targetNumber)
	res, err := d.transport.SendCommand(ctx, cmdInSelect, []byte{targetNumber})
	if err != nil {
		Debugf("InSelect: command failed: %v", err)
		return fmt.Errorf("InSelect command failed: %w", err)
	}

	if len(res) != 2 || res[0] != 0x55 {
		return errors.New("unexpected InSelect response")
	}

	// Check status byte
	if res[1] == 0x27 {
		// 0x27 = Wrong Context - target likely already selected by InListPassiveTarget
		Debugf("InSelect returned 0x27 for target %d - assuming already selected", targetNumber)
		return nil
	}
	if res[1] != 0x00 {
		Debugf("InSelect: failed with status: %02x", res[1])
		return fmt.Errorf("InSelect failed with status: %02x", res[1])
	}

	Debugf("InSelect: success for target %d", targetNumber)
	return nil
}

// InAutoPoll polls for a single target with context support
func (d *Device) InAutoPoll(
	ctx context.Context, pollCount, pollPeriod byte, targetTypes []AutoPollTarget,
) (*AutoPollResult, error) {
	if pollPeriod < 1 || pollPeriod > 15 {
		return nil, errors.New("poll period must be between 1 and 15")
	}

	if len(targetTypes) == 0 || len(targetTypes) > 15 {
		return nil, errors.New("must specify 1-15 target types")
	}

	// Build command data
	data := []byte{pollCount, pollPeriod}
	for _, tt := range targetTypes {
		data = append(data, byte(tt))
	}

	res, err := d.transport.SendCommand(ctx, cmdInAutoPoll, data)
	if err != nil {
		return nil, fmt.Errorf("InAutoPoll command failed: %w", err)
	}

	if len(res) < 2 || res[0] != 0x61 {
		return nil, errors.New("unexpected InAutoPoll response")
	}

	numTargets := res[1]
	if numTargets == 0 {
		return nil, nil //nolint:nilnil // nil result, nil error is valid "no tag detected" response
	}

	// Parse first result only
	offset := 2
	if offset+2 > len(res) {
		return nil, fmt.Errorf("%w: response truncated when expecting target header", ErrInvalidResponse)
	}

	targetType := AutoPollTarget(res[offset])
	dataLen := res[offset+1]
	offset += 2

	if offset+int(dataLen) > len(res) {
		return nil, errors.New("invalid response data length")
	}

	targetData := res[offset : offset+int(dataLen)]

	return &AutoPollResult{
		Type:       targetType,
		TargetData: targetData,
	}, nil
}

// InListPassiveTarget detects a single passive target using InListPassiveTarget command
func (d *Device) InListPassiveTarget(ctx context.Context, brTy byte) (*DetectedTag, error) {
	const maxTg byte = 1
	data := []byte{maxTg, brTy}

	Debugf("InListPassiveTarget - maxTg=%d, brTy=0x%02X, transport=%s", maxTg, brTy, d.transport.Type())

	res, err := d.executeInListPassiveTarget(ctx, data)
	if err != nil {
		return d.handleInListPassiveTargetError(ctx, err, brTy)
	}

	Debugf("InListPassiveTarget response (%d bytes): %X", len(res), res)

	if err := d.validateInListPassiveTargetResponse(res); err != nil {
		return nil, err
	}

	return d.parseInListPassiveTargetResponse(res)
}

// InListPassiveTargetWithTimeout detects a single passive target with timeout support.
// The mxRtyATR parameter controls the maximum retry count for target detection:
// - 0x00: Try once, no retry
// - 0x01-0xFE: Retry count (each retry is ~150ms according to PN532 datasheet)
// - 0xFF: Retry infinitely (blocking mode - use with caution)
//
// For continuous polling with card removal detection, use low values (0x01-0x10) to ensure
// the command returns quickly when no card is present.
func (d *Device) InListPassiveTargetWithTimeout(
	ctx context.Context, brTy, mxRtyATR byte,
) (*DetectedTag, error) {
	const maxTg byte = 1
	data := []byte{maxTg, brTy, mxRtyATR}

	Debugf("InListPassiveTargetWithTimeout - maxTg=%d, brTy=0x%02X, mxRtyATR=0x%02X, transport=%s",
		maxTg, brTy, mxRtyATR, d.transport.Type())

	res, err := d.executeInListPassiveTarget(ctx, data)
	if err != nil {
		return d.handleInListPassiveTargetError(ctx, err, brTy)
	}

	Debugf("InListPassiveTargetWithTimeout response (%d bytes): %X", len(res), res)

	if err := d.validateInListPassiveTargetResponse(res); err != nil {
		return nil, err
	}

	return d.parseInListPassiveTargetResponse(res)
}

// executeInListPassiveTarget sends the InListPassiveTarget command
func (d *Device) executeInListPassiveTarget(ctx context.Context, data []byte) ([]byte, error) {
	// Dynamically adjust transport timeout based on mxRtyATR when provided
	// mxRtyATR is the 3rd byte of the InListPassiveTarget payload
	var prevTimeout time.Duration
	if d.config != nil {
		prevTimeout = d.config.Timeout
	}
	computed := d.computeInListHostTimeout(ctx, data)
	Debugf("executeInListPassiveTarget: computed timeout=%v, prevTimeout=%v", computed, prevTimeout)
	// Best-effort set; if it fails we'll proceed with previous timeout
	if err := d.transport.SetTimeout(computed); err != nil {
		Debugf("executeInListPassiveTarget: SetTimeout failed: %v", err)
	}
	// Restore previous timeout after the command completes
	defer func() { _ = d.transport.SetTimeout(prevTimeout) }()

	Debugln("executeInListPassiveTarget: calling SendCommand")
	result, err := d.transport.SendCommand(ctx, cmdInListPassiveTarget, data)
	if err != nil {
		Debugf("executeInListPassiveTarget: SendCommand failed: %v", err)
		return nil, fmt.Errorf("failed to send InListPassiveTarget command: %w", err)
	}
	d.lastTargetReply.Store(time.Now().UnixNano())
	Debugf("executeInListPassiveTarget: SendCommand succeeded, result len=%d", len(result))
	return result, nil
}

// computeInListHostTimeout derives a host-side timeout from mxRtyATR and context deadline
// According to PN532 docs, each retry is ~150ms. We add baseline slack and bound by context if present.
func (d *Device) computeInListHostTimeout(ctx context.Context, data []byte) time.Duration {
	var fallback time.Duration
	if d.config != nil {
		fallback = d.config.Timeout
	}

	// When mxRtyATR (3rd byte) is provided, derive a timeout from it.
	if len(data) >= 3 {
		return d.computeTimeoutFromRetryCount(ctx, data[2])
	}

	// No mxRtyATR provided; use context deadline if present or fallback
	return d.getContextTimeoutOrFallback(ctx, fallback)
}

// computeTimeoutFromRetryCount calculates timeout based on mxRtyATR retry count
func (d *Device) computeTimeoutFromRetryCount(ctx context.Context, mx byte) time.Duration {
	// 0xFF means infinite retry on hardware; rely on context or cap to a safe upper bound
	if mx == 0xFF {
		return d.handleInfiniteRetry(ctx)
	}

	// Each retry ~150ms; add small baseline slack for host/driver overhead
	expected := time.Duration(int(mx))*150*time.Millisecond + 300*time.Millisecond

	// When mx is explicitly set (not 0xFF), honor the calculated timeout directly
	// without enforcing the device default minimum. This allows quick re-selects
	// with low retry counts (e.g., mx=0x02 for ~600ms timeout).
	// Only cap to reasonable maximum and respect context deadline.
	expected = min(expected, 8*time.Second)

	// Respect context deadline if sooner
	return d.getContextTimeoutOrFallback(ctx, expected)
}

// handleInfiniteRetry handles the special case of infinite retry (0xFF)
func (*Device) handleInfiniteRetry(ctx context.Context) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		if rem := time.Until(deadline); rem > 0 {
			return rem
		}
	}
	// No deadline; choose a conservative cap
	return 10 * time.Second
}

// getContextTimeoutOrFallback returns context deadline if present and positive, otherwise fallback
func (*Device) getContextTimeoutOrFallback(ctx context.Context, fallback time.Duration) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		if rem := time.Until(deadline); rem > 0 && rem < fallback {
			return rem
		}
	}
	return fallback
}

// handleInListPassiveTargetError handles command errors with clone device fallback
func (d *Device) handleInListPassiveTargetError(
	ctx context.Context, err error, brTy byte,
) (*DetectedTag, error) {
	Debugf("InListPassiveTarget command failed: %v", err)

	// Check if this looks like a clone device compatibility issue
	if strings.Contains(err.Error(), "clone device returned empty response") ||
		strings.Contains(err.Error(), "need at least 2 bytes for status") {
		Debugln("Clone device detected - InListPassiveTarget not supported, falling back to InAutoPoll")
		return d.fallbackToInAutoPoll(ctx, brTy)
	}

	return nil, fmt.Errorf("InListPassiveTarget command failed: %w", err)
}

// validateInListPassiveTargetResponse validates the response format
func (*Device) validateInListPassiveTargetResponse(res []byte) error {
	if res == nil || len(res) < 2 { //nolint:staticcheck // explicit nil check for nilaway
		Debugf("Response too short (%d bytes) - may indicate clone device timing issue", len(res))
		return fmt.Errorf("InListPassiveTarget response too short: got %d bytes, expected at least 2", len(res))
	}

	// Check response format: should start with 0x4B (InListPassiveTarget response)
	if res[0] != 0x4B {
		Debugf("Invalid response format - expected 0x4B response code, got: %X", res)
		// Some clone devices may return wrapped responses - try to extract the actual PN532 response
		if len(res) <= 2 || res[1] != 0x4B {
			return fmt.Errorf("unexpected InListPassiveTarget response: expected 0x4B, got %v", res)
		}
		Debugln("Detected wrapped response, adjusting offset")
		// Modify res in place to skip the wrapper byte
		copy(res, res[1:])
	}

	return nil
}

// parseInListPassiveTargetResponse parses the response and creates a DetectedTag
func (d *Device) parseInListPassiveTargetResponse(res []byte) (*DetectedTag, error) {
	if len(res) < 2 {
		return nil, fmt.Errorf("response too short: %d bytes", len(res))
	}
	numTargets := res[1]
	Debugf("InListPassiveTarget found %d targets", numTargets)

	if numTargets == 0 {
		Debugln("No targets detected - this may indicate clone device needs different timing or initialization")
		return nil, nil //nolint:nilnil // nil tag, nil error is valid "no tag detected" response
	}

	// Parse first target only
	tag, _, err := d.parseTargetAtOffset(res, 2, 1)
	if err != nil {
		return nil, err
	}

	return tag, nil
}

// parseTargetAtOffset parses a single target from the response at the given offset
func (d *Device) parseTargetAtOffset(res []byte, offset, targetIndex int) (*DetectedTag, int, error) {
	Debugf("Parsing target %d at offset %d", targetIndex, offset)

	if offset >= len(res) {
		return nil, 0, fmt.Errorf("response truncated when expecting target %d", targetIndex)
	}

	// Skip target number byte (logical number assigned by PN532, no longer used)
	offset++

	result, err := d.parseInListTargetData(res, offset, targetIndex)
	if err != nil {
		return nil, 0, err
	}

	tagType := d.identifyTagType(result.atq, result.sak)
	Debugf("Target %d - Identified as %v", targetIndex, tagType)

	tag := &DetectedTag{
		Type:       tagType,
		UID:        fmt.Sprintf("%x", result.uid),
		UIDBytes:   result.uid,
		ATQ:        result.atq,
		SAK:        result.sak,
		DetectedAt: time.Now(),
	}

	return tag, result.newOffset, nil
}

// targetParseResult groups the parsed target data
type targetParseResult struct {
	atq       []byte
	uid       []byte
	newOffset int
	sak       byte
}

// parseInListTargetData extracts ATQ, SAK, and UID from InListPassiveTarget response data
func (*Device) parseInListTargetData(res []byte, offset, targetIndex int) (*targetParseResult, error) {
	// SENS_RES (ATQ) - 2 bytes
	if offset+2 > len(res) {
		return nil, fmt.Errorf("response truncated when expecting target %d SENS_RES", targetIndex)
	}
	atq := res[offset : offset+2]
	offset += 2
	Debugf("Target %d - ATQ=%X", targetIndex, atq)

	// SEL_RES (SAK) - 1 byte
	if offset >= len(res) {
		return nil, fmt.Errorf("response truncated when expecting target %d SEL_RES", targetIndex)
	}
	sak := res[offset]
	offset++
	Debugf("Target %d - SAK=0x%02X", targetIndex, sak)

	// UID length and UID
	if offset >= len(res) {
		return nil, fmt.Errorf("response truncated when expecting target %d UID length", targetIndex)
	}
	uidLen := res[offset]
	offset++
	Debugf("Target %d - UID length=%d", targetIndex, uidLen)

	if offset+int(uidLen) > len(res) {
		return nil, fmt.Errorf("response truncated when expecting target %d UID", targetIndex)
	}
	uid := res[offset : offset+int(uidLen)]
	offset += int(uidLen)
	Debugf("Target %d - UID=%X", targetIndex, uid)

	return &targetParseResult{
		atq:       atq,
		sak:       sak,
		uid:       uid,
		newOffset: offset,
	}, nil
}

// fallbackToInAutoPoll provides a fallback detection method for clone devices
// that don't support InListPassiveTarget command properly
func (d *Device) fallbackToInAutoPoll(ctx context.Context, brTy byte) (*DetectedTag, error) {
	Debugln("Using InAutoPoll fallback for clone device compatibility")

	// Convert baudRate parameter to appropriate AutoPoll target types
	var targetTypes []AutoPollTarget
	switch brTy {
	case 0x00: // 106kbps (ISO14443-A)
		targetTypes = []AutoPollTarget{AutoPollISO14443A, AutoPollGeneric106kbps, AutoPollMifare}
	case 0x01: // 212kbps (FeliCa)
		targetTypes = []AutoPollTarget{AutoPollFeliCa212, AutoPollGeneric212kbps}
	case 0x02: // 424kbps (FeliCa)
		targetTypes = []AutoPollTarget{AutoPollFeliCa424, AutoPollGeneric424kbps}
	case 0x03: // 847kbps (ISO14443-B)
		targetTypes = []AutoPollTarget{AutoPollISO14443B}
	default:
		// Default to 106kbps Type A for unknown baud rates
		targetTypes = []AutoPollTarget{AutoPollISO14443A, AutoPollGeneric106kbps}
	}

	// Add extra stabilization delay for clone devices
	Debugln("Applying stabilization delay for clone device")
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for RF field stabilization: %w", ctx.Err())
	}

	// Use InAutoPoll with shorter timeout for faster failure detection
	// Use period=3 (3*150ms = 450ms) for quicker response
	result, err := d.InAutoPoll(ctx, 1, 3, targetTypes)
	if err != nil {
		Debugf("InAutoPoll fallback also failed: %v", err)
		return nil, fmt.Errorf("both InListPassiveTarget and InAutoPoll failed for clone device: %w", err)
	}

	if result == nil {
		Debugln("No targets detected via InAutoPoll fallback")
		return nil, nil //nolint:nilnil // nil tag, nil error is valid "no tag detected" response
	}

	Debugln("InAutoPoll fallback detected 1 target")

	// Convert AutoPoll result to DetectedTag format
	return d.convertAutoPollResult(result), nil
}

// PowerDown puts the PN532 into power down mode with context support
func (d *Device) PowerDown(ctx context.Context, wakeupEnable, irqEnable byte) error {
	res, err := d.transport.SendCommand(ctx, cmdPowerDown, []byte{wakeupEnable, irqEnable})
	if err != nil {
		return fmt.Errorf("PowerDown command failed: %w", err)
	}

	// PowerDown response should be 0x17
	if len(res) != 1 || res[0] != 0x17 {
		return fmt.Errorf("unexpected PowerDown response: %v", res)
	}

	return nil
}

// ClearTransportState clears corrupted transport state to prevent firmware lockup
// This is critical when switching between InCommunicateThru and InDataExchange
// operations after frame reception failures
func (d *Device) ClearTransportState() error {
	// Check if the transport supports state clearing
	if clearer, ok := d.transport.(interface{ ClearTransportState() error }); ok {
		if err := clearer.ClearTransportState(); err != nil {
			return fmt.Errorf("failed to clear transport state: %w", err)
		}
	}
	// If transport doesn't support clearing, that's ok (some transports may not need it)
	return nil
}
