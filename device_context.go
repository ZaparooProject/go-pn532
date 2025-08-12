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

package pn532

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// InitContext initializes the PN532 device with context support
func (d *Device) InitContext(ctx context.Context) error {
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

// shouldSkipFirmwareVersion checks if transport supports firmware version retrieval
func (*Device) shouldSkipFirmwareVersion() bool {
	// All transports now support firmware version retrieval
	return false
}

// tryFirmwareVersionCheck attempts to get firmware version for early validation
func (d *Device) tryFirmwareVersionCheck(ctx context.Context) {
	_, err := d.GetFirmwareVersionContext(ctx)
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
	err := d.setupSAMConfigurationContext(ctx)
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
		debugf("Warning: Clone device detected (SAM config issue: %s), continuing without SAM configuration", errStr)
		return nil
	}

	return fmt.Errorf("SAM configuration failed: %w", err)
}

// setupFirmwareVersion retrieves and sets the firmware version
func (d *Device) setupFirmwareVersion(ctx context.Context) error {
	fw, err := d.GetFirmwareVersionContext(ctx)
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

// GetFirmwareVersionContext returns the PN532 firmware version with context support
func (d *Device) GetFirmwareVersionContext(ctx context.Context) (*FirmwareVersion, error) {
	tc := AsTransportContext(d.transport)
	res, err := tc.SendCommandContext(ctx, cmdGetFirmwareVersion, []byte{})
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
	debugf("GetFirmwareVersion response: [%s] (len=%d)",
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
		debugln("Clone device returned SAM config response (0x15) for GetFirmwareVersion")
		return d.createDefaultFirmwareVersion(), nil
	}

	if len(res) >= 3 {
		// Try to extract version information from different positions
		if version := d.parseCloneD5Format(res); version != nil {
			return version, nil
		}

		// Fallback: Create a generic firmware version for compatibility
		debugln("Using fallback firmware version for clone device")
		return d.createDefaultFirmwareVersion(), nil
	}

	return nil, fmt.Errorf("unexpected firmware version response: got %d bytes: %v", len(res), res)
}

// parseCloneD5Format parses clone devices with 0xD5 prefix
func (*Device) parseCloneD5Format(res []byte) *FirmwareVersion {
	if len(res) >= 5 && res[0] == 0xD5 && res[1] == 0x03 {
		// Some clones prefix with 0xD5 (response command byte)
		debugln("Detected clone format with 0xD5 prefix")
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

// GetGeneralStatusContext returns the PN532 general status with context support
func (d *Device) GetGeneralStatusContext(ctx context.Context) (*GeneralStatus, error) {
	tc := AsTransportContext(d.transport)
	res, err := tc.SendCommandContext(ctx, cmdGetGeneralStatus, []byte{})
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

// DiagnoseContext performs a self-diagnosis test with context support
func (d *Device) DiagnoseContext(ctx context.Context, testNumber byte, data []byte) (*DiagnoseResult, error) {
	tc := AsTransportContext(d.transport)

	// Build command: TestNumber + optional data
	cmdPayload := append([]byte{testNumber}, data...)

	res, err := tc.SendCommandContext(ctx, cmdDiagnose, cmdPayload)
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

// setupSAMConfigurationContext configures the SAM with context support
func (d *Device) setupSAMConfigurationContext(ctx context.Context) error {
	return d.SAMConfigurationContext(ctx, SAMModeNormal, 0x00, 0x00)
}

// SAMConfigurationContext configures the SAM with context support
func (d *Device) SAMConfigurationContext(ctx context.Context, mode SAMMode, timeout, irq byte) error {
	tc := AsTransportContext(d.transport)
	res, err := tc.SendCommandContext(ctx, cmdSamConfiguration, []byte{byte(mode), timeout, irq})
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

// DetectTagContext detects a single tag in the field with context support
func (d *Device) DetectTagContext(ctx context.Context) (*DetectedTag, error) {
	tags, err := d.DetectTagsContext(ctx, 1, 0x00)
	if err != nil {
		return nil, err
	}
	if len(tags) == 0 {
		return nil, ErrNoTagDetected
	}
	return tags[0], nil
}

// DetectTagsContext detects multiple tags in the field with context support
// Uses polling strategy system to choose between InAutoPoll and InListPassiveTarget
func (d *Device) DetectTagsContext(ctx context.Context, maxTags, baudRate byte) ([]*DetectedTag, error) {
	if maxTags > 2 {
		maxTags = 2 // PN532 can handle maximum 2 targets
	}
	if maxTags == 0 {
		maxTags = 1
	}

	// Use polling strategy system to determine the appropriate detection method
	strategy := d.selectDetectionStrategy()

	debugf("Using polling strategy: %s", strategy)

	switch strategy {
	case PollStrategyAutoPoll:
		return d.detectTagsWithInAutoPoll(ctx, maxTags, baudRate)

	case PollStrategyLegacy:
		return d.detectTagsWithInListPassiveTarget(ctx, maxTags, baudRate)

	case PollStrategyManual:
		// Manual strategy requires explicit application control
		return nil, errors.New("manual polling strategy requires explicit application control")

	case PollStrategyAuto:
		// This shouldn't happen as selectDetectionStrategy resolves auto to specific strategy
		// Default to legacy method for auto strategy case
		debugf("Auto strategy not resolved, defaulting to legacy")
		return d.detectTagsWithInListPassiveTarget(ctx, maxTags, baudRate)

	default:
		// Default to legacy method for unknown strategies
		debugf("Unknown strategy %s, defaulting to legacy", strategy)
		return d.detectTagsWithInListPassiveTarget(ctx, maxTags, baudRate)
	}
}

// detectTagsWithInAutoPoll uses InAutoPoll for hardware-managed continuous polling
func (d *Device) detectTagsWithInAutoPoll(ctx context.Context, maxTags, baudRate byte) ([]*DetectedTag, error) {
	debugln("Using InAutoPoll strategy")

	config := d.GetPollConfig()
	optimized := d.getOptimizedPollParams(PollStrategyAutoPoll)

	targetTypes := d.selectTargetTypes(config, optimized, baudRate)
	pollPeriod := d.selectPollPeriod(config, optimized)

	if err := d.prepareTransportForInAutoPoll(ctx); err != nil {
		return d.handleTransportPreparationError(err)
	}

	results, err := d.InAutoPollContext(ctx, config.PollCount, pollPeriod, targetTypes)
	if err != nil {
		params := autoPollParams{maxTags: maxTags, baudRate: baudRate}
		return d.handleInAutoPollError(ctx, err, config, optimized, params)
	}

	if len(results) == 0 {
		return nil, ErrNoTagDetected
	}

	d.recordPollSuccess()
	debugf("InAutoPoll detected %d tag(s), converting to DetectedTag format with proper target numbering", len(results))

	return d.convertAutoPollResults(ctx, results, maxTags)
}

// selectTargetTypes determines the appropriate target types for polling
func (*Device) selectTargetTypes(config *ContinuousPollConfig, optimized *OptimizedPollParams,
	baudRate byte,
) []AutoPollTarget {
	targetTypes := config.TargetTypes
	if len(targetTypes) == 0 {
		// Use transport-optimized target types
		targetTypes = optimized.TargetTypes
		if baudRate != 0x00 && len(targetTypes) < 4 {
			// For non-standard baud rates, ensure we have comprehensive coverage
			targetTypes = make([]AutoPollTarget, 0, 4)
			targetTypes = append(targetTypes,
				AutoPollGeneric106kbps, AutoPollMifare, AutoPollFeliCa212, AutoPollFeliCa424)
		}
	}
	return targetTypes
}

// selectPollPeriod determines the appropriate poll period
func (*Device) selectPollPeriod(config *ContinuousPollConfig, optimized *OptimizedPollParams) byte {
	pollPeriod := config.PollPeriod
	if pollPeriod == 0 || (config.Strategy == PollStrategyAuto && pollPeriod == 2) {
		// Use transport-optimized poll period
		pollPeriod = optimized.PollPeriod
	}
	return pollPeriod
}

// handleTransportPreparationError handles transport preparation failures
func (d *Device) handleTransportPreparationError(err error) ([]*DetectedTag, error) {
	debugf("Transport preparation failed: %v, recording failure", err)
	if d.pollState != nil {
		d.pollState.recordFailure()
	}
	return nil, fmt.Errorf("transport preparation failed: %w", err)
}

// handleInAutoPollError handles InAutoPoll command failures with retry logic
func (d *Device) handleInAutoPollError(
	ctx context.Context, err error, config *ContinuousPollConfig,
	optimized *OptimizedPollParams, pollParams autoPollParams,
) ([]*DetectedTag, error) {
	debugf("InAutoPoll failed: %v", err)
	if d.pollState != nil {
		d.pollState.recordFailure()
	}

	// Check if we should retry or fallback
	if d.pollState != nil && d.pollState.shouldRetry() {
		return d.retryInAutoPoll(ctx, config, optimized, pollParams.maxTags, pollParams.baudRate)
	}

	return nil, fmt.Errorf("InAutoPoll failed: %w", err)
}

// retryInAutoPoll performs a retry attempt with delay
func (d *Device) retryInAutoPoll(
	ctx context.Context, config *ContinuousPollConfig, optimized *OptimizedPollParams, maxTags, baudRate byte,
) ([]*DetectedTag, error) {
	debugln("Retrying InAutoPoll after delay")

	// Use optimized retry delay if configured delay is default
	retryDelay := config.RetryDelay
	if retryDelay == 500*time.Millisecond {
		retryDelay = optimized.RetryDelay
	}

	select {
	case <-time.After(retryDelay):
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled while retrying tag detection: %w", ctx.Err())
	}

	return d.detectTagsWithInAutoPoll(ctx, maxTags, baudRate)
}

// recordPollSuccess records a successful polling operation
func (d *Device) recordPollSuccess() {
	if d.pollState != nil {
		d.pollState.recordSuccess()
	}
}

// detectTagsWithInListPassiveTarget uses traditional InListPassiveTarget polling
func (d *Device) detectTagsWithInListPassiveTarget(
	ctx context.Context, maxTags, baudRate byte,
) ([]*DetectedTag, error) {
	debugln("Using InListPassiveTarget strategy")

	// Apply transport-specific optimizations and timing
	if err := d.prepareTransportForInListPassiveTarget(ctx); err != nil {
		debugf("Transport preparation failed: %v", err)
		return nil, fmt.Errorf("transport preparation failed: %w", err)
	}

	// Use InListPassiveTarget for legacy compatibility
	return d.InListPassiveTargetContext(ctx, maxTags, baudRate)
}

// prepareTransportForInAutoPoll applies transport-specific preparations for InAutoPoll
func (d *Device) prepareTransportForInAutoPoll(ctx context.Context) error {
	// Apply transport-specific timing and RF field management
	switch d.transport.Type() {
	case TransportUART:
		// UART typically doesn't need special preparation for InAutoPoll
		return nil

	case TransportI2C, TransportSPI, TransportMock:
		// Other transports may need minimal delay
		select {
		case <-time.After(10 * time.Millisecond):
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for transport stabilization: %w", ctx.Err())
		}
		return nil

	default:
		// Other transports may need minimal delay
		select {
		case <-time.After(10 * time.Millisecond):
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for default transport stabilization: %w", ctx.Err())
		}
		return nil
	}
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

// convertAutoPollResults converts InAutoPoll results to DetectedTag format
func (d *Device) convertAutoPollResults(
	_ context.Context, results []AutoPollResult, maxTags byte,
) ([]*DetectedTag, error) {
	tags := make([]*DetectedTag, 0, len(results))
	for index, result := range results {
		if index >= int(maxTags) {
			break // Respect maxTags limit
		}

		// Parse the target data to extract UID, ATQ, SAK
		uid, atq, sak := d.parseTargetData(result.Type, result.TargetData)

		// Determine tag type based on AutoPoll target type first, then ATQ/SAK
		tagType := d.identifyTagTypeFromTarget(result.Type, atq, sak)

		tag := &DetectedTag{
			Type:           tagType,
			UID:            fmt.Sprintf("%x", uid),
			UIDBytes:       uid,
			ATQ:            atq,
			SAK:            sak,
			TargetNumber:   d.getTargetNumber(index), // Use appropriate target number
			DetectedAt:     time.Now(),
			TargetData:     result.TargetData, // Store full target data for FeliCa
			FromInAutoPoll: true,              // Mark as from InAutoPoll to skip InSelect
		}
		tags = append(tags, tag)
	}

	return tags, nil
}

// getTargetNumber returns the appropriate target number for the given index
// This ensures proper PN532 protocol compliance for target selection
func (*Device) getTargetNumber(index int) byte {
	// Target numbers are now 0-based as InListPassiveTarget assigns them this way
	// This change aligns with the protocol used by all transport types
	return byte(index)
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

// SendDataExchangeContext sends a data exchange command with context support
func (d *Device) SendDataExchangeContext(ctx context.Context, data []byte) ([]byte, error) {
	tc := AsTransportContext(d.transport)
	targetNum := d.getCurrentTarget()
	res, err := tc.SendCommandContext(ctx, cmdInDataExchange, append([]byte{targetNum}, data...))
	if err != nil {
		return nil, fmt.Errorf("failed to send data exchange command: %w", err)
	}

	// Check for error frame (TFI = 0x7F)
	if len(res) >= 2 && res[0] == 0x7F {
		errorCode := res[1]
		return nil, fmt.Errorf("PN532 error: 0x%02X", errorCode)
	}

	if len(res) < 2 || res[0] != 0x41 {
		return nil, errors.New("unexpected data exchange response")
	}
	if res[1] != 0x00 {
		return nil, fmt.Errorf("data exchange error: %02x", res[1])
	}
	return res[2:], nil
}

// SendRawCommandContext sends a raw command with context support
func (d *Device) SendRawCommandContext(ctx context.Context, data []byte) ([]byte, error) {
	tc := AsTransportContext(d.transport)
	res, err := tc.SendCommandContext(ctx, cmdInCommunicateThru, data)
	if err != nil {
		return nil, fmt.Errorf("failed to send communicate through command: %w", err)
	}

	// Check for error frame (TFI = 0x7F)
	if len(res) >= 2 && res[0] == 0x7F {
		errorCode := res[1]
		return nil, fmt.Errorf("PN532 error: 0x%02X", errorCode)
	}

	if len(res) < 2 || res[0] != 0x43 {
		return nil, errors.New("unexpected InCommunicateThru response")
	}
	if res[1] != 0x00 {
		return nil, fmt.Errorf("InCommunicateThru error: %02x", res[1])
	}
	return res[2:], nil
}

// InReleaseContext releases the selected target(s) with context support
func (d *Device) InReleaseContext(ctx context.Context, targetNumber byte) error {
	tc := AsTransportContext(d.transport)
	res, err := tc.SendCommandContext(ctx, cmdInRelease, []byte{targetNumber})
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

// InSelectContext selects the specified target with context support
func (d *Device) InSelectContext(ctx context.Context, targetNumber byte) error {
	tc := AsTransportContext(d.transport)
	res, err := tc.SendCommandContext(ctx, cmdInSelect, []byte{targetNumber})
	if err != nil {
		return fmt.Errorf("InSelect command failed: %w", err)
	}

	if len(res) != 2 || res[0] != 0x55 {
		return errors.New("unexpected InSelect response")
	}

	// Check status byte
	if res[1] == 0x27 {
		return fmt.Errorf("InSelect failed: unknown target number %02x", targetNumber)
	}
	if res[1] != 0x00 {
		return fmt.Errorf("InSelect failed with status: %02x", res[1])
	}

	return nil
}

// InAutoPollContext polls for targets with context support
func (d *Device) InAutoPollContext(
	ctx context.Context, pollCount, pollPeriod byte, targetTypes []AutoPollTarget,
) ([]AutoPollResult, error) {
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

	tc := AsTransportContext(d.transport)
	res, err := tc.SendCommandContext(ctx, cmdInAutoPoll, data)
	if err != nil {
		return nil, fmt.Errorf("InAutoPoll command failed: %w", err)
	}

	if len(res) < 2 || res[0] != 0x61 {
		return nil, errors.New("unexpected InAutoPoll response")
	}

	numTargets := res[1]
	if numTargets == 0 {
		return []AutoPollResult{}, nil
	}

	// Parse results
	results := []AutoPollResult{}
	offset := 2

	for i := 0; i < int(numTargets); i++ {
		if offset+2 > len(res) {
			return nil, fmt.Errorf("%w: response truncated when expecting target %d header", ErrInvalidResponse, i+1)
		}

		targetType := AutoPollTarget(res[offset])
		dataLen := res[offset+1]
		offset += 2

		if offset+int(dataLen) > len(res) {
			return nil, errors.New("invalid response data length")
		}

		targetData := res[offset : offset+int(dataLen)]
		offset += int(dataLen)

		results = append(results, AutoPollResult{
			Type:       targetType,
			TargetData: targetData,
		})
	}

	return results, nil
}

// InListPassiveTargetContext detects passive targets using InListPassiveTarget command
func (d *Device) InListPassiveTargetContext(ctx context.Context, maxTg, brTy byte) ([]*DetectedTag, error) {
	maxTg = d.normalizeMaxTargets(maxTg)
	data := []byte{maxTg, brTy}

	debugf("InListPassiveTarget - maxTg=%d, brTy=0x%02X, transport=%s", maxTg, brTy, d.transport.Type())

	res, err := d.executeInListPassiveTarget(ctx, data)
	if err != nil {
		return d.handleInListPassiveTargetError(ctx, err, maxTg, brTy)
	}

	debugf("InListPassiveTarget response (%d bytes): %X", len(res), res)

	if err := d.validateInListPassiveTargetResponse(res); err != nil {
		return nil, err
	}

	return d.parseInListPassiveTargetResponse(res)
}

// normalizeMaxTargets ensures maxTg is within valid range
func (*Device) normalizeMaxTargets(maxTg byte) byte {
	if maxTg > 2 {
		maxTg = 2 // PN532 can handle maximum 2 targets
	}
	if maxTg == 0 {
		maxTg = 1
	}
	return maxTg
}

// executeInListPassiveTarget sends the InListPassiveTarget command
func (d *Device) executeInListPassiveTarget(ctx context.Context, data []byte) ([]byte, error) {
	tc := AsTransportContext(d.transport)
	result, err := tc.SendCommandContext(ctx, cmdInListPassiveTarget, data)
	if err != nil {
		return nil, fmt.Errorf("failed to send InListPassiveTarget command: %w", err)
	}
	return result, nil
}

// handleInListPassiveTargetError handles command errors with clone device fallback
func (d *Device) handleInListPassiveTargetError(
	ctx context.Context, err error, maxTg, brTy byte,
) ([]*DetectedTag, error) {
	debugf("InListPassiveTarget command failed: %v", err)

	// Check if this looks like a clone device compatibility issue
	if strings.Contains(err.Error(), "clone device returned empty response") ||
		strings.Contains(err.Error(), "need at least 2 bytes for status") {
		debugln("Clone device detected - InListPassiveTarget not supported, falling back to InAutoPoll")
		return d.fallbackToInAutoPoll(ctx, maxTg, brTy)
	}

	return nil, fmt.Errorf("InListPassiveTarget command failed: %w", err)
}

// validateInListPassiveTargetResponse validates the response format
func (*Device) validateInListPassiveTargetResponse(res []byte) error {
	if len(res) < 2 {
		debugf("Response too short (%d bytes) - may indicate clone device timing issue", len(res))
		return fmt.Errorf("InListPassiveTarget response too short: got %d bytes, expected at least 2", len(res))
	}

	// Check response format: should start with 0x4B (InListPassiveTarget response)
	if res[0] != 0x4B {
		debugf("Invalid response format - expected 0x4B response code, got: %X", res)
		// Some clone devices may return wrapped responses - try to extract the actual PN532 response
		if len(res) <= 2 || res[1] != 0x4B {
			return fmt.Errorf("unexpected InListPassiveTarget response: expected 0x4B, got %v", res)
		}
		debugln("Detected wrapped response, adjusting offset")
		// Modify res in place to skip the wrapper byte
		copy(res, res[1:])
	}

	return nil
}

// parseInListPassiveTargetResponse parses the response and creates DetectedTag objects
func (d *Device) parseInListPassiveTargetResponse(res []byte) ([]*DetectedTag, error) {
	numTargets := res[1]
	debugf("InListPassiveTarget found %d targets", numTargets)

	if numTargets == 0 {
		debugln("No targets detected - this may indicate clone device needs different timing or initialization")
		return []*DetectedTag{}, nil
	}

	tags := make([]*DetectedTag, 0, int(numTargets))
	offset := 2

	for i := 0; i < int(numTargets); i++ {
		tag, newOffset, err := d.parseTargetAtOffset(res, offset, i+1)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
		offset = newOffset
	}

	return tags, nil
}

// parseTargetAtOffset parses a single target from the response at the given offset
func (d *Device) parseTargetAtOffset(res []byte, offset, targetIndex int) (*DetectedTag, int, error) {
	debugf("Parsing target %d at offset %d", targetIndex, offset)

	if offset >= len(res) {
		return nil, 0, fmt.Errorf("response truncated when expecting target %d", targetIndex)
	}

	// Target number (logical number assigned by PN532)
	targetNumber := res[offset]
	offset++
	debugf("Target %d - targetNumber=%d", targetIndex, targetNumber)

	result, err := d.parseInListTargetData(res, offset, targetIndex)
	if err != nil {
		return nil, 0, err
	}

	tagType := d.identifyTagType(result.atq, result.sak)
	debugf("Target %d - Identified as %v", targetIndex, tagType)

	tag := &DetectedTag{
		Type:         tagType,
		UID:          fmt.Sprintf("%x", result.uid),
		UIDBytes:     result.uid,
		ATQ:          result.atq,
		SAK:          result.sak,
		TargetNumber: targetNumber,
		DetectedAt:   time.Now(),
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

type autoPollParams struct {
	maxTags  byte
	baudRate byte
}

// parseInListTargetData extracts ATQ, SAK, and UID from InListPassiveTarget response data
func (*Device) parseInListTargetData(res []byte, offset, targetIndex int) (*targetParseResult, error) {
	// SENS_RES (ATQ) - 2 bytes
	if offset+2 > len(res) {
		return nil, fmt.Errorf("response truncated when expecting target %d SENS_RES", targetIndex)
	}
	atq := res[offset : offset+2]
	offset += 2
	debugf("Target %d - ATQ=%X", targetIndex, atq)

	// SEL_RES (SAK) - 1 byte
	if offset >= len(res) {
		return nil, fmt.Errorf("response truncated when expecting target %d SEL_RES", targetIndex)
	}
	sak := res[offset]
	offset++
	debugf("Target %d - SAK=0x%02X", targetIndex, sak)

	// UID length and UID
	if offset >= len(res) {
		return nil, fmt.Errorf("response truncated when expecting target %d UID length", targetIndex)
	}
	uidLen := res[offset]
	offset++
	debugf("Target %d - UID length=%d", targetIndex, uidLen)

	if offset+int(uidLen) > len(res) {
		return nil, fmt.Errorf("response truncated when expecting target %d UID", targetIndex)
	}
	uid := res[offset : offset+int(uidLen)]
	offset += int(uidLen)
	debugf("Target %d - UID=%X", targetIndex, uid)

	return &targetParseResult{
		atq:       atq,
		sak:       sak,
		uid:       uid,
		newOffset: offset,
	}, nil
}

// fallbackToInAutoPoll provides a fallback detection method for clone devices
// that don't support InListPassiveTarget command properly
func (d *Device) fallbackToInAutoPoll(ctx context.Context, maxTg, brTy byte) ([]*DetectedTag, error) {
	debugln("Using InAutoPoll fallback for clone device compatibility")

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
	debugln("Applying stabilization delay for clone device")
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		return nil, fmt.Errorf("context cancelled while waiting for RF field stabilization: %w", ctx.Err())
	}

	// Use InAutoPoll with shorter timeout for faster failure detection
	// Use period=3 (3*150ms = 450ms) for quicker response
	results, err := d.InAutoPollContext(ctx, maxTg, 3, targetTypes)
	if err != nil {
		debugf("InAutoPoll fallback also failed: %v", err)
		return nil, fmt.Errorf("both InListPassiveTarget and InAutoPoll failed for clone device: %w", err)
	}

	if len(results) == 0 {
		debugln("No targets detected via InAutoPoll fallback")
		return []*DetectedTag{}, nil
	}

	debugf("InAutoPoll fallback detected %d targets", len(results))

	// Convert AutoPoll results to DetectedTag format
	return d.convertAutoPollResults(ctx, results, maxTg)
}

// PowerDownContext puts the PN532 into power down mode with context support
func (d *Device) PowerDownContext(ctx context.Context, wakeupEnable, irqEnable byte) error {
	tc := AsTransportContext(d.transport)
	res, err := tc.SendCommandContext(ctx, cmdPowerDown, []byte{wakeupEnable, irqEnable})
	if err != nil {
		return fmt.Errorf("PowerDown command failed: %w", err)
	}

	// PowerDown response should be 0x17
	if len(res) != 1 || res[0] != 0x17 {
		return fmt.Errorf("unexpected PowerDown response: %v", res)
	}

	return nil
}

// performRFWarmup performs a warmup tag detection to initialize RF field circuitry
// This helps avoid slow first scan times by "priming" the PN532's RF field
func (d *Device) performRFWarmup(ctx context.Context) {
	// Use a very short timeout for the warmup - just enough to initialize the RF field
	// but not long enough to cause startup delays when no tag is present
	warmupCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	// Perform a single tag detection to warm up the RF field
	// We ignore both the result and any error since this is just for initialization
	// This should quickly activate the RF field without causing startup delays
	_, _ = d.DetectTagContext(warmupCtx)
}
