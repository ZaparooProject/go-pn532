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
)

// SAMMode represents the SAM configuration mode
type SAMMode byte

const (
	// SAMModeNormal - normal mode (default)
	SAMModeNormal SAMMode = 0x01
	// SAMModeVirtualCard - Virtual Card mode
	SAMModeVirtualCard SAMMode = 0x02
	// SAMModeWiredCard - Wired Card mode
	SAMModeWiredCard SAMMode = 0x03
	// SAMModeDualCard - Dual Card mode
	SAMModeDualCard SAMMode = 0x04

	// SAMNormal is an alias for SAMModeNormal for backward compatibility
	SAMNormal = SAMModeNormal
)

// SAMConfiguration configures the SAM with advanced modes
// mode: Configuration mode (Normal, VirtualCard, WiredCard, DualCard)
// timeout: Timeout value (0x00-0xFF) - time to wait for tag
// irq: IRQ settings (0x00 = no IRQ, 0x01 = IRQ enabled)
// Based on PN532 manual section 7.2.10
func (d *Device) SAMConfiguration(mode SAMMode, timeout, irq byte) error {
	return d.SAMConfigurationContext(context.Background(), mode, timeout, irq)
}
