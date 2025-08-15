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
	"runtime"

	"github.com/ZaparooProject/go-pn532/detection"
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
