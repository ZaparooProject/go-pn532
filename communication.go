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

// SendDataExchange sends a data exchange command to the selected tag
func (d *Device) SendDataExchange(data []byte) ([]byte, error) {
	return d.SendDataExchangeContext(context.Background(), data)
}

// SendRawCommand sends a raw command to the selected tag using InCommunicateThru
// This is used for commands that don't work with InDataExchange (like GET_VERSION)
func (d *Device) SendRawCommand(data []byte) ([]byte, error) {
	return d.SendRawCommandContext(context.Background(), data)
}

// PowerDown puts the PN532 into power down mode to save power consumption
// wakeupEnable: Wake-up enable parameters (bit field):
//   - bit 0: Enable wake-up by HSU (High Speed UART)
//   - bit 1: Enable wake-up by SPI
//   - bit 2: Enable wake-up by I2C
//   - bit 3: Enable wake-up by GPIO (P32)
//   - bit 4: Enable wake-up by GPIO (P34)
//   - bit 5: Enable wake-up by RF field
//   - bit 6: Reserved
//   - bit 7: Enable wake-up by GPIO (P72/INT1)
//
// irqEnable: if 0x01, generates an IRQ when waking up
// Based on PN532 manual section 7.2.11
func (d *Device) PowerDown(wakeupEnable, irqEnable byte) error {
	return d.PowerDownContext(context.Background(), wakeupEnable, irqEnable)
}
