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

package i2c

import (
	"context"
	"fmt"
	"runtime"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/ZaparooProject/go-pn532/transport/i2c"
)

const (
	// DefaultPN532Address is the standard I2C address for PN532 (0x48 >> 1)
	DefaultPN532Address = 0x24
)

// detector implements the Detector interface for I2C devices
type detector struct{}

// New creates a new I2C detector
func New() detection.Detector {
	return &detector{}
}

// init registers the detector on package import
func init() {
	detection.RegisterDetector(New())
}

// Transport returns the transport type
func (*detector) Transport() string {
	return "i2c"
}

// Detect searches for PN532 devices on I2C buses
func (*detector) Detect(ctx context.Context, opts *detection.Options) ([]detection.DeviceInfo, error) {
	// I2C detection is platform-specific
	switch runtime.GOOS {
	case "linux":
		return detectLinux(ctx, opts)
	case "windows", "darwin":
		// Limited I2C support on Windows and macOS
		return nil, detection.ErrUnsupportedPlatform
	default:
		return nil, detection.ErrUnsupportedPlatform
	}
}

// i2cBusInfo represents an I2C bus with detected devices
type i2cBusInfo struct {
	Path    string  // e.g., "/dev/i2c-1"
	Devices []uint8 // Detected device addresses
	Number  int     // e.g., 1
}

// probeI2CDevice attempts to verify a device is a PN532
func probeI2CDevice(_ context.Context, busPath string, address uint8, mode detection.Mode) (
	found bool, metadata map[string]string,
) {
	metadata = make(map[string]string)
	metadata["address"] = fmt.Sprintf("0x%02X", address)

	if mode == detection.Passive {
		// In passive mode, just check if it's the expected address
		return address == DefaultPN532Address, metadata
	}

	// The PN532 I2C address is 0x48 for write operations
	// but the I2C address detected is typically 0x24 (0x48 >> 1)
	// Try to create transport and probe the device
	transport, err := i2c.New(busPath)
	if err != nil {
		return false, metadata
	}
	defer func() { _ = transport.Close() }()

	// Create a PN532 device
	device, err := pn532.New(transport)
	if err != nil {
		return false, metadata
	}

	// Try to get firmware version
	version, err := device.GetFirmwareVersion()
	if err == nil {
		metadata["firmware"] = version.Version
		return true, metadata
	}

	return false, metadata
}
