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

type config struct {
	devicePath *string
	timeout    *time.Duration
	writeText  *string
	debug      *bool
	validate   *bool
}

func parseFlags() *config {
	cfg := &config{
		devicePath: flag.String("device", "",
			"Serial device path (e.g., /dev/ttyUSB0 or COM3). Leave empty for auto-detection."),
		timeout:   flag.Duration("timeout", 30*time.Second, "Timeout for tag detection (default: 30s)"),
		writeText: flag.String("write", "", "Text to write to the tag (if not specified, will only read)"),
		debug:     flag.Bool("debug", false, "Enable debug output"),
		validate:  flag.Bool("validate", true, "Enable read/write validation (default: true)"),
	}
	flag.Parse()
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

	if *cfg.validate {
		connectOpts = append(connectOpts, pn532.WithValidation(nil))
		_, _ = fmt.Println("Validation enabled")
	}

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

func waitForAndCreateTag(device *pn532.Device, timeout time.Duration) (pn532.Tag, error) {
	_, _ = fmt.Printf("Waiting for NFC tag (timeout: %s)...\n", timeout)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	detectedTag, err := device.WaitForTag(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("timeout: no tag detected within %s", timeout)
		}
		return nil, fmt.Errorf("tag detection failed: %w", err)
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

func main() {
	cfg := parseFlags()

	connectOpts := buildConnectOptions(cfg)

	device, err := connectToDevice(cfg, connectOpts)
	if err != nil {
		_, _ = fmt.Printf("Failed to connect to device: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = device.Close() }()

	tag, err := waitForAndCreateTag(device, *cfg.timeout)
	if err != nil {
		_, _ = fmt.Printf("%v\n", err)
		return
	}

	if err := writeTextIfRequested(tag, *cfg.writeText); err != nil {
		_, _ = fmt.Printf("%v\n", err)
		return
	}

	_, _ = fmt.Print(tag.DebugInfo())
}
