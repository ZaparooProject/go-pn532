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
	"github.com/ZaparooProject/go-pn532/polling"
	"github.com/ZaparooProject/go-pn532/transport/i2c"
	"github.com/ZaparooProject/go-pn532/transport/spi"
	"github.com/ZaparooProject/go-pn532/transport/uart"
)

type config struct {
	devicePath   *string
	timeout      *time.Duration
	writeText    *string
	debug        *bool
	pollInterval *time.Duration
}

func parseFlags() *config {
	cfg := &config{
		devicePath: flag.String("device", "",
			"Serial device path (e.g., /dev/ttyUSB0 or COM3). Leave empty for auto-detection."),
		timeout:   flag.Duration("timeout", 30*time.Second, "Timeout for tag detection (default: 30s)"),
		writeText: flag.String("write", "", "Text to write to the tag (if not specified, will only read)"),
		debug:     flag.Bool("debug", false, "Enable debug output"),
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

func setupSession(device *pn532.Device, cfg *config) (*polling.Session, error) {
	sessionConfig := polling.DefaultConfig()
	sessionConfig.PollInterval = *cfg.pollInterval

	session := polling.NewSession(device, sessionConfig)
	return session, nil
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

func runSessionLoop(ctx context.Context, session *polling.Session, device *pn532.Device, cfg *config) error {
	// If write mode, use WriteToTag for coordinated write operation
	if *cfg.writeText != "" {
		return handleWriteMode(ctx, session, *cfg.timeout, *cfg.writeText)
	}

	// Otherwise run in continuous session mode
	// Set up tag detection callback for read-only mode
	session.OnCardDetected = func(detectedTag *pn532.DetectedTag) error {
		// Display tag information
		return handleTagReading(device, detectedTag)
	}

	// Set up tag removal callback to ensure proper state cleanup
	session.OnCardRemoved = func() {
		_, _ = fmt.Println("Tag removed - ready for next tag...")
	}

	// Start the session - this blocks until context is cancelled
	if err := session.Start(ctx); err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}

	return handleContinuousMode(ctx)
}

func handleWriteMode(
	ctx context.Context,
	session *polling.Session,
	timeout time.Duration,
	writeText string,
) error {
	_, _ = fmt.Println("Waiting for tag to write...")

	err := session.WriteToNextTag(ctx, timeout, func(_ context.Context, tag pn532.Tag) error {
		// Write the text to the tag
		if err := writeTextIfRequested(tag, writeText); err != nil {
			return err
		}

		// Also display tag information after writing
		_, _ = fmt.Print("\n=== Tag Information After Write ===\n")
		_, _ = fmt.Print(tag.DebugInfo())
		return nil
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			_, _ = fmt.Printf("timeout: no tag detected within %s\n", timeout)
			return nil
		}
		return fmt.Errorf("write operation failed: %w", err)
	}

	_, _ = fmt.Println("Write operation completed successfully!")
	return nil
}

func handleContinuousMode(ctx context.Context) error {
	// Just wait for context cancellation in continuous mode
	<-ctx.Done()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		_, _ = fmt.Println("Session completed")
	}
	return nil
}

func main() {
	cfg := parseFlags()
	connectOpts := buildConnectOptions(cfg)

	device, err := connectToDevice(cfg, connectOpts)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to connect to device: %v\n", err)
		return
	}
	defer func() { _ = device.Close() }()

	session, err := setupSession(device, cfg)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to setup session: %v\n", err)
		return
	}

	_, _ = fmt.Printf("Waiting for NFC tag (timeout: %s, poll interval: %s)...\n", *cfg.timeout, *cfg.pollInterval)

	ctx, cancel := context.WithTimeout(context.Background(), *cfg.timeout)
	defer cancel()

	if err := runSessionLoop(ctx, session, device, cfg); err != nil {
		_, _ = fmt.Printf("%v\n", err)
	}
}

func handleTagReading(device *pn532.Device, detectedTag *pn532.DetectedTag) error {
	tag, err := device.CreateTag(detectedTag)
	if err != nil {
		return fmt.Errorf("failed to create tag: %w", err)
	}

	_, _ = fmt.Print(tag.DebugInfo())
	return nil
}
