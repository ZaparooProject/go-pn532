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
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"

	// Import all detectors to register them
	_ "github.com/ZaparooProject/go-pn532/detection/i2c"
	_ "github.com/ZaparooProject/go-pn532/detection/spi"
	_ "github.com/ZaparooProject/go-pn532/detection/uart"
	"github.com/ZaparooProject/go-pn532/transport/i2c"
	"github.com/ZaparooProject/go-pn532/transport/spi"
	"github.com/ZaparooProject/go-pn532/transport/uart"
)

// mustPrint prints to stdout, panicking on error (for test output only)
func mustPrint(args ...any) {
	_, err := fmt.Print(args...)
	if err != nil {
		panic(err)
	}
}

// mustPrintf prints formatted output to stdout, panicking on error (for test output only)
func mustPrintf(format string, args ...any) {
	_, err := fmt.Printf(format, args...)
	if err != nil {
		panic(err)
	}
}

// mustPrintln prints with newline to stdout, panicking on error (for test output only)
func mustPrintln(args ...any) {
	_, err := fmt.Println(args...)
	if err != nil {
		panic(err)
	}
}

type config struct {
	devicePath   *string
	timeout      *time.Duration
	writeText    *string
	debug        *bool
	testRobust   *bool
	testTiming   *bool
	pollInterval *time.Duration
}

func parseFlags() *config {
	cfg := &config{
		devicePath: flag.String("device", "",
			"Serial device path (e.g., /dev/ttyUSB0 or COM3). Leave empty for auto-detection."),
		timeout:    flag.Duration("timeout", 30*time.Second, "Timeout for tag detection (default: 30s)"),
		writeText:  flag.String("write", "", "Text to write to the tag (if not specified, will only read)"),
		debug:      flag.Bool("debug", false, "Enable debug output"),
		testRobust: flag.Bool("test-robust", false, "Test robust authentication features for Chinese clone cards"),
		testTiming: flag.Bool("test-timing", false, "Test timing variance analysis"),
		pollInterval: flag.Duration("poll-interval", 100*time.Millisecond,
			"Polling interval for tag detection (default: 100ms)"),
	}
	flag.Parse()

	// Enable debug output if --debug flag is set
	if *cfg.debug {
		pn532.SetDebugEnabled(true)
	}

	return cfg
}

// newTransport creates a new transport from a device path.
func newTransport(path string) (pn532.Transport, error) {
	if path == "" {
		return nil, errors.New("empty device path")
	}

	pathLower := strings.ToLower(path)

	// Check for I2C pattern
	if strings.Contains(pathLower, "i2c") {
		transport, err := i2c.New(path)
		if err != nil {
			return nil, fmt.Errorf("failed to create I2C transport: %w", err)
		}
		return transport, nil
	}

	// Check for SPI pattern
	if strings.Contains(pathLower, "spi") {
		transport, err := spi.New(path)
		if err != nil {
			return nil, fmt.Errorf("failed to create SPI transport: %w", err)
		}
		return transport, nil
	}

	// Default to UART for serial ports
	transport, err := uart.New(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create UART transport: %w", err)
	}
	return transport, nil
}

// newTransportFromDevice creates a new transport from a detected device.
func newTransportFromDevice(device detection.DeviceInfo) (pn532.Transport, error) {
	switch strings.ToLower(device.Transport) {
	case "uart":
		transport, err := uart.New(device.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to create UART transport: %w", err)
		}
		return transport, nil
	case "i2c":
		transport, err := i2c.New(device.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to create I2C transport: %w", err)
		}
		return transport, nil
	case "spi":
		transport, err := spi.New(device.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to create SPI transport: %w", err)
		}
		return transport, nil
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", device.Transport)
	}
}

func buildConnectOptions(cfg *config) []pn532.ConnectOption {
	var connectOpts []pn532.ConnectOption

	if *cfg.devicePath == "" {
		connectOpts = append(connectOpts,
			pn532.WithAutoDetection(),
			pn532.WithTransportFromDeviceFactory(newTransportFromDevice))
		_, _ = fmt.Println("Auto-detecting PN532 devices...")
	} else {
		connectOpts = append(connectOpts, pn532.WithTransportFactory(newTransport))
		_, _ = fmt.Printf("Opening device: %s\n", *cfg.devicePath)
	}

	// Set device timeout to prevent InListPassiveTarget from blocking indefinitely
	connectOpts = append(connectOpts, pn532.WithConnectTimeout(*cfg.timeout))
	return connectOpts
}

func connectToDevice(cfg *config, connectOpts []pn532.ConnectOption) (*pn532.Device, error) {
	device, err := pn532.ConnectDevice(*cfg.devicePath, connectOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PN532 device: %w", err)
	}

	// Show firmware version
	if version, versionErr := device.GetFirmwareVersion(); versionErr == nil {
		_, _ = fmt.Printf("PN532 Firmware: %s\n", version.Version)
	}

	return device, nil
}

func waitForAndCreateTag(device *pn532.Device, timeout, pollInterval time.Duration) (pn532.Tag, error) {
	_, _ = fmt.Printf("Waiting for NFC tag (timeout: %s, poll interval: %s)...\n", timeout, pollInterval)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Use simplified polling with configurable intervals
	detectedTag, err := device.SimplePoll(ctx, pollInterval)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("timeout: no tag detected within %s", timeout)
		}
		return nil, fmt.Errorf("tag detection failed: %w", err)
	}

	// Handle case where no tag was detected (SimplePoll returns nil, nil)
	if detectedTag == nil {
		return nil, errors.New("no tag detected")
	}

	tag, err := device.CreateTag(detectedTag)
	if err != nil {
		return nil, fmt.Errorf("failed to create tag: %w", err)
	}

	return tag, nil
}

func writeTextIfRequested(tag pn532.Tag, writeText string) error {
	if writeText == "" {
		return nil
	}

	_, _ = fmt.Print("\n=== Writing to tag ===\n")
	if err := tag.WriteText(writeText); err != nil {
		return fmt.Errorf("failed to write text: %w", err)
	}
	_, _ = fmt.Println("Write successful!")
	return nil
}

func testChineseCloneUnlock(mifareTag *pn532.MIFARETag) {
	mustPrint("\n=== Testing Chinese Clone Unlock Sequences ===\n")

	// Test Gen1 unlock commands directly
	unlockCommands := []struct {
		name string
		desc string
		cmd  byte
	}{
		{"Gen1 7-bit", "Chinese Gen1 clone unlock (7-bit UID)", 0x40},
		{"Gen1 8-bit", "Chinese Gen1 clone unlock (4-byte UID)", 0x43},
	}

	foundUnlock := false

	for _, unlock := range unlockCommands {
		mustPrintf("Trying %s (0x%02X): ", unlock.name, unlock.cmd)

		// Access the device directly to test unlock commands
		device := mifareTag.GetDevice() // We'll need to add this method
		if device == nil {
			mustPrintln("FAILED - cannot access device")
			continue
		}

		// Try the unlock command
		start := time.Now()
		_, err := device.SendDataExchange([]byte{unlock.cmd})
		duration := time.Since(start)

		if err == nil {
			mustPrintf("SUCCESS (%.2fms) - %s\n", float64(duration.Nanoseconds())/1000000, unlock.desc)
			foundUnlock = true

			// If unlock successful, try to read manufacturer block directly
			mustPrintln("  Attempting direct block 0 read (no authentication needed)...")
			if data, readErr := mifareTag.ReadBlockDirect(0); readErr == nil {
				mustPrintf("  Block 0 (UID): %02X %02X %02X %02X %02X %02X %02X %02X...\n",
					data[0], data[1], data[2], data[3], data[4], data[5], data[6], data[7])
				mustPrintln("  → This is a Gen1 Chinese clone with backdoor access!")
			} else {
				mustPrintf("  Block 0 read failed: %v\n", readErr)
			}
		} else {
			mustPrintf("FAILED (%.2fms) - %v\n", float64(duration.Nanoseconds())/1000000, err)
		}
	}

	if !foundUnlock {
		mustPrintln("\nNo Chinese clone unlock sequences successful.")
		mustPrintln("This may be a Gen2/CUID/FUID clone or genuine card.")

		// Test FM11RF08S universal backdoor key
		mustPrint("\nTesting FM11RF08S universal backdoor key: ")
		backdoorKey := []byte{0xA3, 0x96, 0xEF, 0xA4, 0xE2, 0x4F}

		start := time.Now()
		err := mifareTag.AuthenticateRobust(1, pn532.MIFAREKeyA, backdoorKey)
		duration := time.Since(start)

		if err == nil {
			mustPrintf("SUCCESS (%.2fms)\n", float64(duration.Nanoseconds())/1000000)
			mustPrintln("→ This is likely an FM11RF08S clone with universal backdoor!")
		} else {
			mustPrintf("FAILED (%.2fms) - %v\n", float64(duration.Nanoseconds())/1000000, err)
			mustPrintln("→ Backdoor key authentication failed")
		}
	}
}

// tryKeyOnSector attempts to authenticate with a given key and sector
func tryKeyOnSector(
	mifareTag *pn532.MIFARETag,
	sector uint8,
	key []byte,
	keyType byte,
	keyName string,
) (success bool, duration time.Duration) {
	mustPrintf("  Trying Key %s [%02X %02X %02X %02X %02X %02X]: ",
		keyName, key[0], key[1], key[2], key[3], key[4], key[5])

	start := time.Now()
	err := mifareTag.AuthenticateRobust(sector, keyType, key)
	duration = time.Since(start)

	if err == nil {
		mustPrintf("SUCCESS (%.2fms)\n", float64(duration.Nanoseconds())/1000000)
		testSectorRead(mifareTag, sector)
		return true, duration
	}

	mustPrintf("FAILED (%.2fms) - %v\n", float64(duration.Nanoseconds())/1000000, err)
	analysis := mifareTag.AnalyzeLastError(err)
	mustPrintf("    Error analysis: %s\n", analysis)
	return false, duration
}

// testSectorRead attempts to read a block from the authenticated sector
func testSectorRead(mifareTag *pn532.MIFARETag, sector uint8) {
	block := sector * 4 // First block of sector
	if data, readErr := mifareTag.ReadBlock(block); readErr == nil {
		mustPrintf("    Block %d read: %02X %02X %02X ... (16 bytes)\n",
			block, data[0], data[1], data[2])
	} else {
		mustPrintf("    Block %d read failed: %v\n", block, readErr)
		analysis := mifareTag.AnalyzeLastError(readErr)
		mustPrintf("    Error analysis: %s\n", analysis)
	}
}

// testSectorAuthentication tests all keys for a given sector
func testSectorAuthentication(
	mifareTag *pn532.MIFARETag,
	sector uint8,
	testKeys [][]byte,
) (success bool, authCount int) {
	mustPrintf("\nTesting sector %d:\n", sector)

	for _, key := range testKeys {
		for _, keyType := range []byte{pn532.MIFAREKeyA, pn532.MIFAREKeyB} {
			keyName := "A"
			if keyType == pn532.MIFAREKeyB {
				keyName = "B"
			}

			if keySuccess, _ := tryKeyOnSector(mifareTag, sector, key, keyType, keyName); keySuccess {
				return true, 1
			}
			authCount++
		}
	}

	mustPrintf("  No working keys found for sector %d\n", sector)
	return false, authCount
}

func testRobustAuthentication(tag pn532.Tag) {
	mifareTag, ok := tag.(*pn532.MIFARETag)
	if !ok {
		mustPrintln("Tag is not a MIFARE tag, skipping robust authentication test")
		return
	}

	mustPrint("\n=== Testing Robust Authentication ===\n")

	testChineseCloneUnlock(mifareTag)

	testKeys := [][]byte{
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, // Default transport key
		{0xD3, 0xF7, 0xD3, 0xF7, 0xD3, 0xF7}, // NDEF key
		{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // All zeros
		{0xA3, 0x96, 0xEF, 0xA4, 0xE2, 0x4F}, // FM11RF08S universal backdoor key
	}

	successfulAuths := 0
	totalAttempts := 0

	for sector := uint8(1); sector < 4; sector++ {
		success, attempts := testSectorAuthentication(mifareTag, sector, testKeys)
		if success {
			successfulAuths++
		}
		totalAttempts += attempts
	}

	mustPrint("\nAuthentication Summary:\n")
	mustPrintf("  Successful: %d/%d (%.1f%%)\n",
		successfulAuths, totalAttempts, float64(successfulAuths*100)/float64(totalAttempts))

	variance := mifareTag.GetTimingVariance()
	mustPrintf("  Timing variance: %.2fms\n", float64(variance.Nanoseconds())/1000000)

	if mifareTag.IsTimingUnstable() {
		mustPrintln("  WARNING: High timing variance detected - possible hardware issues")
	} else {
		mustPrintln("  Timing appears stable")
	}
}

// performTimingAttempts runs authentication attempts and collects timing data
func performTimingAttempts(
	mifareTag *pn532.MIFARETag,
	sector uint8,
	key []byte,
	attempts int,
) (timings []time.Duration, successCount int) {
	timings = make([]time.Duration, 0, attempts)
	successCount = 0

	for i := 0; i < attempts; i++ {
		start := time.Now()
		err := mifareTag.AuthenticateRobust(sector, pn532.MIFAREKeyA, key)
		duration := time.Since(start)
		timings = append(timings, duration)

		if err == nil {
			successCount++
			mustPrintf("  Attempt %2d: SUCCESS (%.2fms)\n", i+1, float64(duration.Nanoseconds())/1000000)
		} else {
			mustPrintf("  Attempt %2d: FAILED  (%.2fms) - %v\n", i+1, float64(duration.Nanoseconds())/1000000, err)
		}

		time.Sleep(100 * time.Millisecond)
	}

	return timings, successCount
}

// calculateTimingStats computes and displays timing statistics
func calculateTimingStats(timings []time.Duration, successCount, attempts int) {
	if len(timings) == 0 {
		return
	}

	var minTime, maxTime, total time.Duration = timings[0], timings[0], 0
	for _, timing := range timings {
		if timing < minTime {
			minTime = timing
		}
		if timing > maxTime {
			maxTime = timing
		}
		total += timing
	}

	avg := total / time.Duration(len(timings))
	variance := maxTime - minTime

	mustPrint("\nTiming Statistics:\n")
	mustPrintf("  Success rate: %d/%d (%.1f%%)\n",
		successCount, attempts, float64(successCount*100)/float64(attempts))
	mustPrintf("  Min time: %.2fms\n", float64(minTime.Nanoseconds())/1000000)
	mustPrintf("  Max time: %.2fms\n", float64(maxTime.Nanoseconds())/1000000)
	mustPrintf("  Avg time: %.2fms\n", float64(avg.Nanoseconds())/1000000)
	mustPrintf("  Variance: %.2fms\n", float64(variance.Nanoseconds())/1000000)

	switch {
	case variance > 1000*time.Millisecond:
		mustPrintln("  WARNING: High variance (>1000ms) indicates possible hardware issues")
	case variance > 500*time.Millisecond:
		mustPrintln("  CAUTION: Moderate variance detected")
	default:
		mustPrintln("  Timing appears stable")
	}
}

func testTimingAnalysis(tag pn532.Tag) {
	mifareTag, ok := tag.(*pn532.MIFARETag)
	if !ok {
		mustPrintln("Tag is not a MIFARE tag, skipping timing analysis test")
		return
	}

	mustPrint("\n=== Testing Timing Analysis ===\n")

	key := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	sector := uint8(1)
	attempts := 10

	mustPrintf("Performing %d authentication attempts to sector %d...\n", attempts, sector)

	timings, successCount := performTimingAttempts(mifareTag, sector, key, attempts)
	calculateTimingStats(timings, successCount, attempts)
}

func main() {
	cfg := parseFlags()

	connectOpts := buildConnectOptions(cfg)

	device, err := connectToDevice(cfg, connectOpts)
	if err != nil {
		_, _ = fmt.Printf("Failed to connect to device: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = device.Close() }()

	tag, err := waitForAndCreateTag(device, *cfg.timeout, *cfg.pollInterval)
	if err != nil {
		_, _ = fmt.Printf("%v\n", err)
		return
	}

	if err := writeTextIfRequested(tag, *cfg.writeText); err != nil {
		_, _ = fmt.Printf("%v\n", err)
		return
	}

	// Run tests if requested
	if *cfg.testRobust {
		testRobustAuthentication(tag)
	}

	if *cfg.testTiming {
		testTimingAnalysis(tag)
	}

	_, _ = fmt.Print(tag.DebugInfo())

	// Exit successfully after reading and displaying the tag
	// Use return instead of os.Exit(0) to allow defer functions to run
}
