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

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/ZaparooProject/go-pn532/transport/i2c"
	"github.com/ZaparooProject/go-pn532/transport/spi"
	"github.com/ZaparooProject/go-pn532/transport/uart"
)

// Discovery handles reader discovery and transport creation
type Discovery struct {
	config *Config
	output *Output
}

// NewDiscovery creates a new discovery handler
func NewDiscovery(config *Config, output *Output) *Discovery {
	return &Discovery{
		config: config,
		output: output,
	}
}

// DiscoverReaders discovers all available PN532 readers
func (d *Discovery) DiscoverReaders(ctx context.Context) ([]detection.DeviceInfo, error) {
	d.output.Verbose("Discovering readers...")

	opts := detection.DefaultOptions()
	opts.Timeout = d.config.ConnectTimeout
	opts.Mode = detection.Safe

	readers, err := detection.DetectAllContext(ctx, &opts)
	if err != nil {
		return nil, d.provideDetailedDiscoveryError(err)
	}

	d.output.Verbose("   Found %d reader(s)", len(readers))

	return readers, nil
}

// CreateTransport creates the appropriate transport for a device
func (*Discovery) CreateTransport(reader detection.DeviceInfo) (pn532.Transport, error) {
	switch reader.Transport {
	case TransportUART:
		transport, err := uart.New(reader.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to create UART transport: %w", err)
		}
		return transport, nil
	case TransportI2C:
		transport, err := i2c.New(reader.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to create I2C transport: %w", err)
		}
		return transport, nil
	case TransportSPI:
		transport, err := spi.New(reader.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to create SPI transport: %w", err)
		}
		return transport, nil
	default:
		return nil, fmt.Errorf("unsupported transport type: %s", reader.Transport)
	}
}

// FindNewReaders finds readers that are new compared to the last scan
func (*Discovery) FindNewReaders(lastReaders, currentReaders []detection.DeviceInfo) []detection.DeviceInfo {
	var newReaders []detection.DeviceInfo

	for _, current := range currentReaders {
		found := false
		for _, last := range lastReaders {
			if current.Path == last.Path && current.Transport == last.Transport {
				found = true
				break
			}
		}
		if !found {
			newReaders = append(newReaders, current)
		}
	}

	return newReaders
}

// FindDisconnectedReaders finds readers that were present but are now gone
func (*Discovery) FindDisconnectedReaders(lastReaders, currentReaders []detection.DeviceInfo) []detection.DeviceInfo {
	var disconnected []detection.DeviceInfo

	for _, last := range lastReaders {
		found := false
		for _, current := range currentReaders {
			if last.Path == current.Path && last.Transport == current.Transport {
				found = true
				break
			}
		}
		if !found {
			disconnected = append(disconnected, last)
		}
	}

	return disconnected
}

// HandleDiscoveryError handles reader discovery errors in vendor test mode
func (d *Discovery) HandleDiscoveryError(err error) {
	d.output.Verbose("Discovery error: %v", err)
}

// provideDetailedDiscoveryError provides more helpful error messages for common discovery failures
func (d *Discovery) provideDetailedDiscoveryError(err error) error {
	if errors.Is(err, detection.ErrUnsupportedPlatform) {
		return fmt.Errorf("reader discovery failed: I2C/SPI detection not supported on this platform, but UART should work. " +
			"If you have a USB PN532 device, ensure it's connected and not in use by another process. " +
			"You can check with: lsof | grep '/dev/cu.usbserial' or 'lsof | grep '/dev/ttyUSB'")
	}

	if errors.Is(err, detection.ErrNoDevicesFound) {
		return fmt.Errorf("reader discovery failed: no PN532 devices found. " +
			"Ensure your PN532 device is connected via USB and the driver is loaded. " +
			"On macOS, USB devices appear as /dev/cu.usbserial-*")
	}

	// Check for device access conflicts (device busy/in use by another process)
	if isDeviceBusyError(err) {
		return fmt.Errorf("reader discovery failed: device may be in use by another process. " +
			"Try killing any existing nfctest processes with: pkill -f nfctest. " +
			"Original error: %w", err)
	}

	// Default detailed message
	return fmt.Errorf("reader discovery failed: %w. " +
		"Common causes: device disconnected, insufficient permissions, or communication failure", err)
}

// isDeviceBusyError checks if the error indicates the device is busy/in use by another process
func isDeviceBusyError(err error) bool {
	// Only check for errors that specifically indicate device access conflicts,
	// not general communication failures
	return errors.Is(err, pn532.ErrTransportClosed) ||
		errors.Is(err, pn532.ErrDeviceNotFound)
}
