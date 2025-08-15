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
	"context"
	"fmt"
	"time"
)

// DetectTag detects a single tag in the field
// For multiple tag detection, use DetectTags()
func (d *Device) DetectTag() (*DetectedTag, error) {
	return d.DetectTagContext(context.Background())
}

// WaitForTag waits for a tag to be detected with the given context.
// This is a high-level convenience function that handles the async tag detection
// pattern with error handling and timeout management.
//
// It automatically manages the polling loop, error recovery, and provides
// clean timeout handling through the context.
//
// Example usage:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//
//	tag, err := device.WaitForTag(ctx)
//	if err != nil {
//	    if errors.Is(err, context.DeadlineExceeded) {
//	        fmt.Println("Timeout: no tag detected")
//	    }
//	    return err
//	}
//
//	fmt.Printf("Tag detected: %s\n", tag.UID)
func (*Device) handleDetectionError(errorCount *int, err error) error {
	const (
		maxErrors      = 10
		errorThreshold = 3
	)

	*errorCount++

	// Log first few errors for debugging
	if *errorCount <= errorThreshold {
		debugf("Tag detection error #%d: %v", *errorCount, err)
	}

	// Give up after too many errors
	if *errorCount > maxErrors {
		return fmt.Errorf("too many detection errors (%d), last error: %w", *errorCount, err)
	}

	return nil
}

func (*Device) pauseWithContext(ctx context.Context, interval time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(interval):
		return nil
	}
}

func (d *Device) attemptDetection(_ context.Context, errorCount *int) (*DetectedTag, error) {
	detectedTag, err := d.DetectTag()
	if err != nil {
		return nil, d.handleDetectionError(errorCount, err)
	}

	if detectedTag != nil {
		debugf("Tag detected successfully: UID=%s Type=%s", detectedTag.UID, detectedTag.Type)
		return detectedTag, nil
	}

	// No tag detected but no error
	if *errorCount == 0 {
		debugln("No tag detected, continuing to poll...")
	}
	return nil, ErrNoTagDetected
}

func (d *Device) WaitForTag(ctx context.Context) (*DetectedTag, error) {
	const pollInterval = 100 * time.Millisecond
	errorCount := 0

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		detectedTag, err := d.attemptDetection(ctx, &errorCount)
		if err != nil {
			// For any error (including ErrNoTagDetected), pause and continue
			if pauseErr := d.pauseWithContext(ctx, pollInterval); pauseErr != nil {
				return nil, pauseErr
			}
			continue
		}

		return detectedTag, nil
	}
}

// SimplePoll performs straightforward polling with InListPassiveTarget on a regular schedule
// This is a simplified alternative to WaitForTag that removes complex error handling and
// strategy selection, providing predictable poll-poll-poll behavior at regular intervals.
func (d *Device) SimplePoll(ctx context.Context, interval time.Duration) (*DetectedTag, error) {
	// Set transport timeout for individual polling attempts
	if err := d.transport.SetTimeout(interval); err != nil {
		return nil, err
	}
	
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			// Try to detect tags
			tags, err := d.InListPassiveTargetContext(ctx, 1, 0x00)
			if err != nil {
				// Continue polling on errors (device might be temporarily busy)
				debugf("Polling error (continuing): %v", err)
				continue
			}
			
			if len(tags) > 0 {
				return tags[0], nil
			}
			// No tag detected this cycle, continue polling
		}
	}
}

// selectDetectionStrategy chooses the appropriate detection strategy
// Integrates with the polling strategy system for intelligent strategy selection


// DetectTags detects multiple tags in the field
// maxTags: maximum number of tags to detect (1-2)
// baudRate: baud rate and modulation type:
//   - 0x00: 106 kbps type A (ISO/IEC14443 Type A)
//   - 0x01: 212 kbps (FeliCa polling)
//   - 0x02: 424 kbps (FeliCa polling)
//   - 0x03: 106 kbps type B (ISO/IEC14443-3B)
//   - 0x04: 106 kbps Innovision Jewel tag
func (d *Device) DetectTags(maxTags, baudRate byte) ([]*DetectedTag, error) {
	return d.DetectTagsContext(context.Background(), maxTags, baudRate)
}

// InitiatorListPassiveTargets detects passive targets with optional filtering
// This is a compatibility method for the PN532 InListPassiveTarget command
func (d *Device) InitiatorListPassiveTargets(maxTags int, tagType TagType, uid []byte) ([]*DetectedTag, error) {
	// Get all tags first using standard detection
	// The baudRate is set to 0x00 for 106 kbps Type A
	allTags, err := d.DetectTags(byte(maxTags), 0x00)
	if err != nil {
		return nil, err
	}

	return filterDetectedTags(allTags, tagType, uid), nil
}

// filterDetectedTags applies tag type and UID filtering to detected tags
func filterDetectedTags(allTags []*DetectedTag, tagType TagType, uid []byte) []*DetectedTag {
	var filteredTags []*DetectedTag
	for _, tag := range allTags {
		if shouldIncludeTag(tag, tagType, uid) {
			filteredTags = append(filteredTags, tag)
		}
	}
	return filteredTags
}

// shouldIncludeTag determines if a tag should be included based on filters
func shouldIncludeTag(tag *DetectedTag, tagType TagType, uid []byte) bool {
	if !matchesTagTypeFilter(tag, tagType) {
		return false
	}
	return matchesUIDFilter(tag, uid)
}

// matchesTagTypeFilter checks if tag matches the tag type filter
func matchesTagTypeFilter(tag *DetectedTag, tagType TagType) bool {
	return tagType == TagTypeAny || tag.Type == tagType
}

// matchesUIDFilter checks if tag UID matches the UID filter
func matchesUIDFilter(tag *DetectedTag, uid []byte) bool {
	if len(uid) == 0 {
		return true // No UID filter
	}

	if len(tag.UIDBytes) < len(uid) {
		return false
	}

	// Check if the beginning of the tag UID matches the filter
	for i, b := range uid {
		if tag.UIDBytes[i] != b {
			return false
		}
	}
	return true
}

// identifyTagType identifies the tag type based on ATQ and SAK values
func (d *Device) identifyTagType(atq []byte, sak byte) TagType {
	if len(atq) != 2 {
		return TagTypeUnknown
	}

	// NTAG identification patterns
	if d.isNTAGPattern(atq, sak) {
		return TagTypeNTAG
	}

	// MIFARE Classic identification patterns
	if d.isMIFAREPattern(atq, sak) {
		return TagTypeMIFARE
	}

	return TagTypeUnknown
}

// isNTAGPattern checks if the ATQ and SAK match known NTAG patterns
func (*Device) isNTAGPattern(atq []byte, sak byte) bool {
	debugf("isNTAGPattern - checking ATQ=%X, SAK=0x%02X", atq, sak)

	if isStandardNTAGPattern(atq, sak) {
		return true
	}

	if isAdditionalNTAGPattern(atq, sak) {
		return true
	}

	debugln("No NTAG pattern matched")
	return false
}

// isStandardNTAGPattern checks standard NTAG patterns (ISO14443-3A compliant)
func isStandardNTAGPattern(atq []byte, sak byte) bool {
	if sak == 0x00 && isStandardNTAGATQPattern(atq) {
		debugln("NTAG pattern matched (SAK=0x00)")
		return true
	}

	if sak == 0x04 && isStandardATQPattern(atq) {
		debugln("NTAG pattern matched (SAK=0x04)")
		return true
	}

	return false
}

// isStandardNTAGATQPattern checks for standard NTAG ATQ patterns including byte-swapped variants
func isStandardNTAGATQPattern(atq []byte) bool {
	return (atq[0] == 0x00 && atq[1] == 0x44) ||
		(atq[0] == 0x44 && atq[1] == 0x00) || // Byte-swapped (common in clones)
		(atq[0] == 0x01 && atq[1] == 0x01) // Alternative NTAG215 pattern
}

// isStandardATQPattern checks for standard ATQ patterns
func isStandardATQPattern(atq []byte) bool {
	return (atq[0] == 0x00 && atq[1] == 0x44) ||
		(atq[0] == 0x44 && atq[1] == 0x00)
}

// isAdditionalNTAGPattern checks additional NTAG215-specific patterns observed in field testing
func isAdditionalNTAGPattern(atq []byte, sak byte) bool {
	if (atq[0] == 0x01 && atq[1] == 0x00 && sak == 0x44) ||
		(atq[0] == 0x00 && atq[1] == 0x04 && sak == 0x00) || // Some NTAG215 variants
		(atq[0] == 0x04 && atq[1] == 0x00 && sak == 0x00) { // Byte-swapped variant
		debugln("NTAG pattern matched (additional patterns)")
		return true
	}
	return false
}

// isMIFAREPattern checks if the ATQ and SAK match known MIFARE patterns
func (*Device) isMIFAREPattern(atq []byte, sak byte) bool {
	debugf("isMIFAREPattern - checking ATQ=%X, SAK=0x%02X", atq, sak)

	// MIFARE Classic 1K identification
	if atq[0] == 0x00 && atq[1] == 0x04 && sak == 0x08 {
		debugln("MIFARE pattern matched (Classic 1K)")
		return true
	}
	// MIFARE Classic 4K identification
	if atq[0] == 0x00 && atq[1] == 0x02 && sak == 0x18 {
		debugln("MIFARE pattern matched (Classic 4K)")
		return true
	}

	// Additional MIFARE-compatible patterns that may work with MIFARE commands
	// ATQ=0100, SAK=0x04 - seen on some MIFARE-compatible cards
	if atq[0] == 0x01 && atq[1] == 0x00 && sak == 0x04 {
		debugln("MIFARE pattern matched (compatible variant)")
		return true
	}

	debugln("No MIFARE pattern matched")
	return false
}

// handleTargetSelection handles target selection based on transport capabilities
func (d *Device) handleTargetSelection(detected *DetectedTag) error {
	isFromInAutoPoll := detected.FromInAutoPoll

	if isFromInAutoPoll {
		// Skip InSelect for tags detected via InAutoPoll - InAutoPoll handles target selection internally
		debugf("InAutoPoll detected tag - skipping InSelect for target %d (InAutoPoll handles selection)",
			detected.TargetNumber)
		d.setCurrentTarget(detected.TargetNumber)
		return nil
	}

	// Standard devices: Always use InSelect for proper target selection
	debugf("Standard device - performing InSelect for target %d", detected.TargetNumber)
	return d.selectTargetWithError(detected.TargetNumber)
}

// selectTargetWithError wraps selectTarget with error formatting
func (d *Device) selectTargetWithError(targetNumber byte) error {
	if err := d.selectTarget(targetNumber); err != nil {
		return fmt.Errorf("failed to select target %d: %w", targetNumber, err)
	}
	return nil
}

// CreateTag creates a Tag instance based on the detected tag
func (d *Device) CreateTag(detected *DetectedTag) (Tag, error) {
	// Handle target selection based on transport capabilities
	if err := d.handleTargetSelection(detected); err != nil {
		return nil, err
	}
	switch detected.Type {
	case TagTypeNTAG:
		return NewNTAGTag(d, detected.UIDBytes, detected.SAK), nil
	case TagTypeMIFARE:
		return NewMIFARETag(d, detected.UIDBytes, detected.SAK), nil
	case TagTypeFeliCa:
		return NewFeliCaTag(d, detected.TargetData)
	case TagTypeUnknown:
		return nil, ErrInvalidTag
	case TagTypeAny:
		return nil, ErrInvalidTag
	default:
		return nil, ErrInvalidTag
	}
}

// InRelease releases the selected target(s)
// targetNumber: logical number of the target to release (0x00 for all targets)
// Based on PN532 manual section 7.3.11
func (d *Device) InRelease(targetNumber byte) error {
	return d.InReleaseContext(context.Background(), targetNumber)
}

// InSelect selects the specified target
// targetNumber: logical number of the target to select
// Based on PN532 manual section 7.3.12
func (d *Device) InSelect(targetNumber byte) error {
	return d.InSelectContext(context.Background(), targetNumber)
}
