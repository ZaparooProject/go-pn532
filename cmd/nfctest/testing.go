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

package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
)

// Testing handles reader and card testing
type Testing struct {
	config    *Config
	output    *Output
	discovery *Discovery
}

// NewTesting creates a new testing handler
func NewTesting(config *Config, output *Output, discovery *Discovery) *Testing {
	return &Testing{
		config:    config,
		output:    output,
		discovery: discovery,
	}
}

// TestReader performs basic connectivity tests on a reader
func (t *Testing) TestReader(_ context.Context, reader detection.DeviceInfo) error {
	t.output.ReaderTestHeader(reader)

	transport, err := t.discovery.CreateTransport(reader)
	if err != nil {
		t.output.TestFailure()
		return fmt.Errorf("failed to create transport: %w", err)
	}

	// Ensure transport cleanup on all exit paths
	defer func() {
		if closeErr := transport.Close(); closeErr != nil {
			t.output.Verbose("Warning: transport close failed: %v", closeErr)
		}
	}()

	device, err := pn532.New(transport, pn532.WithTimeout(5*time.Second))
	if err != nil {
		// Transport will be closed by defer
		t.output.TestFailure()
		return fmt.Errorf("failed to create device: %w", err)
	}

	defer func() {
		if closeErr := device.Close(); closeErr != nil {
			t.output.Verbose("Warning: device close failed: %v", closeErr)
		}
	}()

	// Initialize device (SAM configuration, etc.)
	if initErr := device.Init(); initErr != nil {
		// Both device and transport will be closed by defer
		t.output.TestFailure()
		return fmt.Errorf("failed to initialize device: %w", initErr)
	}

	version, err := device.GetFirmwareVersion()
	if err != nil {
		t.output.TestFailure()
		return fmt.Errorf("failed to get firmware version: %w", err)
	}

	t.output.TestSuccess(reader, version)

	if err := t.runConnectivityTests(device); err != nil {
		return fmt.Errorf("connectivity tests failed: %w", err)
	}

	return nil
}

// runConnectivityTests runs additional connectivity validation tests
func (t *Testing) runConnectivityTests(device *pn532.Device) error {
	t.output.Verbose("   Running connectivity tests...")

	// Test SAM configuration
	if err := device.SAMConfiguration(pn532.SAMModeNormal, 20, 1); err != nil {
		return fmt.Errorf("SAM configuration failed: %w", err)
	}

	t.output.Verbose("   OK: SAM configuration OK")

	return nil
}

// TestCard performs tests on the detected card
func (t *Testing) TestCard(device *pn532.Device, detected *pn532.DetectedTag) error {
	// Create tag interface
	tag, err := device.CreateTag(detected)
	if err != nil {
		return fmt.Errorf("failed to create tag interface: %w", err)
	}

	switch detected.Type {
	case pn532.TagTypeNTAG:
		return t.testNTAGTag(tag)
	case pn532.TagTypeMIFARE:
		return t.testMIFARETag(tag)
	case pn532.TagTypeFeliCa:
		return t.testFeliCaTag(tag)
	case pn532.TagTypeUnknown:
		return errors.New("unknown tag type detected")
	case pn532.TagTypeAny:
		return errors.New("generic card type - specific testing not available")
	default:
		return fmt.Errorf("unsupported tag type: %s", detected.Type)
	}
}

// TestCardWithTag tests a card using an already created tag instance
// This method is used with the polling.Monitor.WriteToTag for thread-safe operations
// TestCardWithTag tests a card using an already created tag instance
// This method is used with the polling.Monitor.WriteToTag for thread-safe operations
func (t *Testing) TestCardWithTag(tag pn532.Tag) error {
	switch typedTag := tag.(type) {
	case *pn532.NTAGTag:
		return t.testNTAGTag(typedTag)
	case *pn532.MIFARETag:
		return t.testMIFARETag(typedTag)
	case *pn532.FeliCaTag:
		return t.testFeliCaTag(typedTag)
	default:
		_, _ = fmt.Printf("   Unsupported tag type: %T\n", tag)
		return fmt.Errorf("unsupported tag type: %T", tag)
	}
}

// testNTAGTag tests NTAG-specific operations
func (t *Testing) testNTAGTag(tag pn532.Tag) error {
	ntagTag, ok := tag.(*pn532.NTAGTag)
	if !ok {
		return errors.New("tag is not an NTAG tag")
	}

	if err := t.testNTAGCapabilityContainer(ntagTag); err != nil {
		return err
	}

	t.testNTAGNDEF(ntagTag)

	t.testNTAGWriteCapability(ntagTag)
	t.testNTAGStress(ntagTag)

	return nil
}

// testNTAGCapabilityContainer tests reading the NTAG capability container
func (*Testing) testNTAGCapabilityContainer(ntagTag *pn532.NTAGTag) error {
	_, _ = fmt.Print("   Reading capability container...")
	_, err := ntagTag.ReadBlock(3)
	if err != nil {
		_, _ = fmt.Printf(" ERROR: Failed: %v\n", err)
		return fmt.Errorf("failed to read capability container: %w", err)
	}
	_, _ = fmt.Print(" OK\n")
	return nil
}

// testNTAGNDEF tests reading NDEF data
func (t *Testing) testNTAGNDEF(ntagTag *pn532.NTAGTag) {
	_, _ = fmt.Print("   TESTING: Reading NDEF data...")
	ndef, err := ntagTag.ReadNDEF()
	t.output.NDEFResults(ndef, err)
}

// testNTAGWriteCapability tests write operations
func (*Testing) testNTAGWriteCapability(ntagTag *pn532.NTAGTag) {
	_, _ = fmt.Print("   TEXT: Testing write capability...")

	originalData, readErr := ntagTag.ReadBlock(4)
	if readErr != nil {
		_, _ = fmt.Print(" WARNING: Cannot read block for write test\n")
		return
	}

	testData := []byte("TEST")
	writeErr := ntagTag.WriteBlock(4, testData)
	if writeErr != nil {
		_, _ = fmt.Printf(" ERROR: Write failed: %v\n", writeErr)
		return
	}

	restoreErr := ntagTag.WriteBlock(4, originalData)
	if restoreErr != nil {
		_, _ = fmt.Printf(" WARNING: Write successful but failed to restore: %v\n", restoreErr)
	} else {
		_, _ = fmt.Print(" OK\n")
	}
}

// testNTAGStress performs stress testing on NTAG
func (*Testing) testNTAGStress(ntagTag *pn532.NTAGTag) {
	_, _ = fmt.Print("   STRESS: Stress test (10 rapid reads)...")
	success := 0
	for i := 0; i < 10; i++ {
		if _, err := ntagTag.ReadBlock(0); err == nil {
			success++
		}
	}
	_, _ = fmt.Printf(" OK: %d/10 succeeded\n", success)
}

// testMIFARETag tests MIFARE-specific operations
func (t *Testing) testMIFARETag(tag pn532.Tag) error {
	mifareTag, ok := tag.(*pn532.MIFARETag)
	if !ok {
		return errors.New("tag is not a MIFARE tag")
	}

	// Try to read NDEF data (library handles authentication automatically)
	_, _ = fmt.Print("   TESTING: Reading NDEF data...")
	ndef, err := mifareTag.ReadNDEF()
	t.output.NDEFResults(ndef, err)

	t.testMifareManufacturerBlock(mifareTag)

	return nil
}

// testMifareManufacturerBlock tests manufacturer block access with default keys
func (*Testing) testMifareManufacturerBlock(mifareTag *pn532.MIFARETag) {
	// Additional MIFARE tests - try to read manufacturer block with basic auth
	_, _ = fmt.Print("   Testing manufacturer block access...")

	// Try common default keys for sector 0 (manufacturer data)
	defaultKeys := [][]byte{
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, // Default transport key
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // All zeros
		{0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5}, // MAD key
	}

	accessed := false
	for _, key := range defaultKeys {
		// Try Key A first
		if err := mifareTag.Authenticate(0, 0x00, key); err == nil {
			if _, readErr := mifareTag.ReadBlock(0); readErr == nil {
				accessed = true
				break
			}
		}
	}

	if accessed {
		_, _ = fmt.Print(" OK: Accessible with default keys\n")
	} else {
		_, _ = fmt.Print(" WARNING: Uses custom keys (normal for field cards)\n")
	}
}

// testFeliCaTag tests FeliCa-specific operations
func (*Testing) testFeliCaTag(_ pn532.Tag) error {
	_, _ = fmt.Print("   TESTING: FeliCa tag detected...")
	// FeliCa-specific tests would go here
	_, _ = fmt.Print(" WARNING: FeliCa testing not yet implemented\n")
	return nil
}
