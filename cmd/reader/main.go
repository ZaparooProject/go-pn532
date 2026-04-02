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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	_ "github.com/ZaparooProject/go-pn532/detection/i2c"
	_ "github.com/ZaparooProject/go-pn532/detection/spi"
	_ "github.com/ZaparooProject/go-pn532/detection/uart"
	"github.com/ZaparooProject/go-pn532/polling"
	"github.com/ZaparooProject/go-pn532/tagops"
	"github.com/ZaparooProject/go-pn532/transport/i2c"
	"github.com/ZaparooProject/go-pn532/transport/spi"
	"github.com/ZaparooProject/go-pn532/transport/uart"
)

type config struct {
	writeText     string
	devicePath    string
	bulkWriteFile string
	debug         bool
	stressTest    bool
	loop          bool
}

// Package-level flag variables
var (
	flagWriteText     string
	flagDevicePath    string
	flagBulkWriteFile string
	flagDebug         bool
	flagStressTest    bool
	flagLoop          bool
)

func init() {
	flag.StringVar(&flagWriteText, "write", "", "Text to write to the next scanned tag (exits after write)")
	flag.StringVar(&flagDevicePath, "device", "", "Device path (auto-detect if empty)")
	flag.StringVar(&flagBulkWriteFile, "bulkwrite", "", "Bulk write mode: path to CSV file with name,url pairs")
	flag.BoolVar(&flagDebug, "debug", false, "Enable debug output")
	flag.BoolVar(&flagStressTest, "test", false, "Enable stress test mode for hardware validation")
	flag.BoolVar(&flagLoop, "loop", false, "Continuously wait for readers, restart on disconnect (for bulk testing)")
}

func parseConfig() *config {
	cfg := &config{
		writeText:     flagWriteText,
		devicePath:    flagDevicePath,
		bulkWriteFile: flagBulkWriteFile,
		debug:         flagDebug,
		stressTest:    flagStressTest,
		loop:          flagLoop,
	}

	// Enable debug output only if --debug flag is explicitly set
	if cfg.debug {
		pn532.SetDebugEnabled(true)
	}

	return cfg
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

// newTransport creates a new transport from a device path by trying different transports.
func newTransport(path string) (pn532.Transport, error) {
	if path == "" {
		return nil, errors.New("empty device path")
	}

	pathLower := strings.ToLower(path)

	// Check for I2C pattern
	if strings.Contains(pathLower, "i2c") {
		transport, err := i2c.New(path)
		if err != nil {
			return nil, fmt.Errorf("failed to create I2C transport for %s: %w", path, err)
		}
		return transport, nil
	}

	// Check for SPI pattern
	if strings.Contains(pathLower, "spi") {
		transport, err := spi.New(path)
		if err != nil {
			return nil, fmt.Errorf("failed to create SPI transport for %s: %w", path, err)
		}
		return transport, nil
	}

	// Default to UART for serial ports
	transport, err := uart.New(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create UART transport for %s: %w", path, err)
	}
	return transport, nil
}

func connectToDevice(ctx context.Context, cfg *config) (*pn532.Device, error) {
	var connectOpts []pn532.ConnectOption

	if cfg.devicePath == "" {
		// Auto-detection case
		connectOpts = append(connectOpts,
			pn532.WithAutoDetection(),
			pn532.WithTransportFromDeviceFactory(newTransportFromDevice))
		if cfg.debug {
			_, _ = fmt.Println("Auto-detecting PN532 devices...")
		}
	} else {
		// Specific device path
		connectOpts = append(connectOpts, pn532.WithTransportFactory(newTransport))
		if cfg.debug {
			_, _ = fmt.Printf("Opening device: %s\n", cfg.devicePath)
		}
	}

	// Set reasonable timeout
	connectOpts = append(connectOpts, pn532.WithConnectTimeout(5*time.Second))

	device, err := pn532.ConnectDevice(ctx, cfg.devicePath, connectOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PN532 device: %w", err)
	}

	// Show firmware version if debug enabled
	if cfg.debug {
		if version, versionErr := device.GetFirmwareVersion(ctx); versionErr == nil {
			_, _ = fmt.Printf("PN532 Firmware: %s\n", version.Version)
		}
	}

	return device, nil
}

// printTagInfo prints detailed information about a detected tag
func printTagInfo(info *tagops.TagInfo) {
	switch info.Type {
	case pn532.TagTypeNTAG:
		_, _ = fmt.Printf("  NTAG variant: %s\n", info.NTAGType)
		_, _ = fmt.Printf("  Total pages: %d, User memory: %d bytes\n", info.TotalPages, info.UserMemory)
	case pn532.TagTypeMIFARE:
		_, _ = fmt.Printf("  MIFARE variant: %s\n", info.MIFAREType)
		_, _ = fmt.Printf("  Sectors: %d, Total memory: %d bytes\n", info.Sectors, info.TotalMemory)
	case pn532.TagTypeUnknown, pn532.TagTypeFeliCa, pn532.TagTypeAny:
		// No additional details for these types
	}
}

// printNDEFRecord prints a single NDEF record
func printNDEFRecord(i int, record *pn532.NDEFRecord) {
	switch {
	case record.Text != "":
		_, _ = fmt.Printf("    [%d] Text: %q\n", i, record.Text)
	case record.URI != "":
		_, _ = fmt.Printf("    [%d] URI: %s\n", i, record.URI)
	default:
		_, _ = fmt.Printf("    [%d] Type: %s\n", i, record.Type)
	}
}

// printNDEFMessage prints NDEF message contents
func printNDEFMessage(ndefMsg *pn532.NDEFMessage, err error) {
	switch {
	case err != nil:
		_, _ = fmt.Printf("  NDEF: not readable (%v)\n", err)
	case ndefMsg != nil && len(ndefMsg.Records) > 0:
		_, _ = fmt.Printf("  NDEF records: %d\n", len(ndefMsg.Records))
		for i := range ndefMsg.Records {
			printNDEFRecord(i, &ndefMsg.Records[i])
		}
	default:
		_, _ = fmt.Println("  NDEF: empty or not formatted")
	}
}

// handleTagDetected processes a detected tag using tagops
func handleTagDetected(ctx context.Context, ops *tagops.TagOperations, detectedTag *pn532.DetectedTag) error {
	// Print basic tag information with human-readable type name
	typeName := tagops.TagTypeDisplayName(detectedTag.Type)
	_, _ = fmt.Printf("\nTag detected: UID=%s Type=%s Manufacturer=%s\n",
		detectedTag.UID, typeName, detectedTag.Manufacturer())

	// Initialize tagops from the already-detected tag.
	// We use InitFromDetectedTag instead of DetectTag because the polling loop
	// already detected the tag via InListPassiveTarget. Calling DetectTag would
	// perform a redundant detection with InRelease(0) which can corrupt tag state.
	if err := ops.InitFromDetectedTag(ctx, detectedTag); err != nil {
		_, _ = fmt.Printf("  Tag type detection: %v\n", err)
		return nil // Continue monitoring
	}

	// Get and print detailed tag information
	info, err := ops.GetTagInfo()
	if err != nil {
		_, _ = fmt.Printf("  Failed to get tag info: %v\n", err)
		return nil
	}
	printTagInfo(info)

	// Try to read NDEF data using tagops (returns *pn532.NDEFMessage directly)
	ndefMsg, ndefErr := ops.ReadNDEF(ctx)
	printNDEFMessage(ndefMsg, ndefErr)

	// Return the error so the polling session can retry if needed
	if ndefErr != nil {
		return fmt.Errorf("NDEF read failed: %w", ndefErr)
	}
	return nil
}

func runReadMode(ctx context.Context, device *pn532.Device, _ *config) error {
	// Create session with default configuration
	sessionConfig := polling.DefaultConfig()
	session := polling.NewSession(device, sessionConfig)

	// Ensure session cleanup for fast shutdown
	defer func() {
		if err := session.Close(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Failed to close session: %v\n", err)
		}
	}()

	_, _ = fmt.Println("Starting continuous tag monitoring. Press Ctrl+C to stop...")

	// Use tagops for high-level tag operations
	ops := tagops.New(device)

	// Set up tag detection callback using the setter method
	// The callback receives a context with a timeout from the poll cycle,
	// ensuring long-running operations (like MIFARE auth) can be cancelled
	session.SetOnCardDetected(func(callbackCtx context.Context, detectedTag *pn532.DetectedTag) error {
		return handleTagDetected(callbackCtx, ops, detectedTag)
	})

	// Set up tag removal callback using the setter method
	session.SetOnCardRemoved(func() {
		_, _ = fmt.Println("Tag removed - ready for next tag...")
	})

	// Start the session in a goroutine to allow for immediate cancellation
	done := make(chan error, 1)
	go func() {
		done <- session.Start(ctx)
	}()

	// Wait for either session completion or context cancellation
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("failed to start session: %w", err)
		}
		return nil
	case <-ctx.Done():
		// Context cancelled - session.Close() will be called by defer
		return ctx.Err()
	}
}

func runWriteMode(ctx context.Context, device *pn532.Device, cfg *config) error {
	if device == nil {
		return errors.New("device cannot be nil for write mode")
	}

	if cfg.writeText == "" {
		return errors.New("writeText cannot be empty for write mode")
	}

	// Create session with default configuration for write operations
	sessionConfig := polling.DefaultConfig()
	session := polling.NewSession(device, sessionConfig)

	// Ensure session cleanup for fast shutdown
	defer func() {
		if err := session.Close(); err != nil && cfg.debug {
			_, _ = fmt.Fprintf(os.Stderr, "Failed to close session: %v\n", err)
		}
	}()

	_, _ = fmt.Printf("Waiting for tag to write text: %q\n", cfg.writeText)
	_, _ = fmt.Println("Please place a tag near the reader...")

	// Set a reasonable timeout for tag detection (30 seconds)
	timeout := 30 * time.Second

	// Use WriteToNextTag to wait for a tag and write to it
	// Note: Using the same context for both session and write operations in this simple example.
	// For concurrent scenarios where you want independent cancellation control, create separate contexts:
	//   sessionCtx := ctx
	//   writeCtx, cancelWrite := context.WithCancel(ctx)
	//   defer cancelWrite()
	err := session.WriteToNextTag(ctx, ctx, timeout, func(ctx context.Context, tag pn532.Tag) error {
		_, _ = fmt.Println("Tag detected! Writing text...")

		// Create NDEF message with text record
		message := &pn532.NDEFMessage{
			Records: []pn532.NDEFRecord{
				{
					Type: pn532.NDEFTypeText,
					Text: cfg.writeText,
				},
			},
		}

		// Write the NDEF message to the tag
		if err := tag.WriteNDEF(ctx, message); err != nil {
			return fmt.Errorf("failed to write NDEF message: %w", err)
		}

		_, _ = fmt.Printf("Successfully wrote text to tag: %q\n", cfg.writeText)
		return nil
	})
	// Handle any errors from the write operation
	if err != nil {
		if errors.Is(err, context.Canceled) {
			_, _ = fmt.Println("Write operation cancelled.")
		}
		return fmt.Errorf("write operation failed: %w", err)
	}

	return nil
}

func diagnoseFirmware(ctx context.Context, device *pn532.Device) {
	version, err := device.GetFirmwareVersion(ctx)
	if err != nil {
		_, _ = fmt.Printf("[✗] Firmware: %v\n", err)
		return
	}
	var protocols []string
	if version.SupportIso14443a {
		protocols = append(protocols, "ISO14443A")
	}
	if version.SupportIso14443b {
		protocols = append(protocols, "ISO14443B")
	}
	if version.SupportIso18092 {
		protocols = append(protocols, "ISO18092")
	}
	_, _ = fmt.Printf("[✓] Firmware: v%s (%s)\n", version.Version, strings.Join(protocols, ", "))
}

func diagnoseRTT(ctx context.Context, device *pn532.Device) {
	const samples = 10
	var rtts []time.Duration
	for range samples {
		start := time.Now()
		_, err := device.GetFirmwareVersion(ctx)
		if err == nil {
			rtts = append(rtts, time.Since(start))
		}
	}
	if len(rtts) == 0 {
		_, _ = fmt.Println("[✗] Communication: Failed to measure RTT")
		return
	}
	minRTT, maxRTT, sumRTT := rtts[0], rtts[0], time.Duration(0)
	for _, rtt := range rtts {
		sumRTT += rtt
		if rtt < minRTT {
			minRTT = rtt
		}
		if rtt > maxRTT {
			maxRTT = rtt
		}
	}
	avgRTT := sumRTT / time.Duration(len(rtts))
	_, _ = fmt.Printf("[✓] Communication: RTT min=%s avg=%s max=%s (%d samples)\n",
		minRTT.Round(time.Millisecond), avgRTT.Round(time.Millisecond), maxRTT.Round(time.Millisecond), len(rtts))
}

func diagnoseTest(ctx context.Context, device *pn532.Device, testNum byte, name string) {
	result, err := device.Diagnose(ctx, testNum, nil)
	switch {
	case err != nil:
		_, _ = fmt.Printf("[✗] %s: %v\n", name, err)
	case result.Success:
		_, _ = fmt.Printf("[✓] %s: OK\n", name)
	default:
		_, _ = fmt.Printf("[✗] %s: FAILED\n", name)
	}
}

func diagnoseRFField(ctx context.Context, device *pn532.Device) {
	status, err := device.GetGeneralStatus(ctx)
	switch {
	case err != nil:
		_, _ = fmt.Printf("[✗] RF field: %v\n", err)
	case status.FieldPresent:
		_, _ = fmt.Println("[✓] RF field: Active")
	default:
		_, _ = fmt.Println("[✓] RF field: Inactive (will activate on poll)")
	}
}

func diagnoseTagDetection(ctx context.Context, device *pn532.Device) {
	detectCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	tag, err := device.DetectTag(detectCtx)
	if err != nil || tag == nil {
		_, _ = fmt.Println("[✓] Tag detection: Ready (no tag present)")
	} else {
		_, _ = fmt.Printf("[✓] Tag detection: %s (UID: %s)\n", tag.Type, tag.UID)
	}
}

func runDiagnostics(ctx context.Context, device *pn532.Device) {
	_, _ = fmt.Println("\nPN532 Diagnostics")
	_, _ = fmt.Println("=================")

	diagnoseFirmware(ctx, device)
	diagnoseRTT(ctx, device)
	diagnoseTest(ctx, device, pn532.DiagnoseROMTest, "ROM test")
	diagnoseTest(ctx, device, pn532.DiagnoseRAMTest, "RAM test")
	diagnoseTest(ctx, device, pn532.DiagnoseSelfAntennaTest, "Antenna test")
	diagnoseRFField(ctx, device)
	diagnoseTagDetection(ctx, device)

	_, _ = fmt.Println()
}

const loopRetryDelay = 2 * time.Second

func runLoop(ctx context.Context, cfg *config) error {
	readerNum := 0
	for {
		readerNum++
		_, _ = fmt.Printf("\n=== Reader #%d — waiting for device... ===\n", readerNum)

		err := run(ctx, cfg)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return err
			}
			_, _ = fmt.Fprintf(os.Stderr, "Reader #%d error: %v\n", readerNum, err)
		} else {
			_, _ = fmt.Printf("Reader #%d session ended cleanly.\n", readerNum)
		}

		// Wait before retrying, but respect cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(loopRetryDelay):
		}
	}
}

func run(ctx context.Context, cfg *config) error {
	// Connect to device
	device, err := connectToDevice(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := device.Close(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Failed to close device: %v\n", err)
		}
	}()

	// Run diagnostics on startup
	runDiagnostics(ctx, device)

	// Mode selection based on flags
	if cfg.stressTest {
		// Stress test mode - multi-tag read/write validation
		return runStressTestMode(ctx, device, cfg)
	}
	if cfg.bulkWriteFile != "" {
		// Bulk write mode - write URLs from CSV and lock cards
		return runBulkWriteMode(ctx, device, cfg)
	}
	if cfg.writeText != "" {
		// Write mode - write text to next scanned tag and exit
		return runWriteMode(ctx, device, cfg)
	}
	// Read mode - continuously monitor for tags
	return runReadMode(ctx, device, cfg)
}

func main() {
	flag.Parse()
	os.Exit(mainWithExitCode())
}

func mainWithExitCode() int {
	// Parse command-line flags
	cfg := parseConfig()

	// Initialize session log file (always, regardless of debug flag)
	logPath, logErr := pn532.InitSessionLog()
	if logErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: could not create session log: %v\n", logErr)
	}
	defer func() {
		if err := pn532.CloseSessionLog(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Warning: could not close session log: %v\n", err)
		}
	}()

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		_, _ = fmt.Print("\nShutting down gracefully...\n")
		cancel()
	}()

	// Run the main application logic
	runFn := run
	if cfg.loop {
		runFn = runLoop
	}
	if err := runFn(ctx, cfg); err != nil {
		if errors.Is(err, context.Canceled) {
			// User requested shutdown, exit cleanly
			return 0
		}
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		// Show log path on error for debugging
		if logPath != "" {
			_, _ = fmt.Fprintf(os.Stderr, "Debug log: %s\n", logPath)
		}
		return 1
	}
	return 0
}
