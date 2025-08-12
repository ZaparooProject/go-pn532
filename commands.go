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

// PN532 Command codes
const (
	cmdDiagnose            = 0x00
	cmdSamConfiguration    = 0x14
	cmdGetFirmwareVersion  = 0x02
	cmdGetGeneralStatus    = 0x04
	cmdInListPassiveTarget = 0x4A
	cmdInDataExchange      = 0x40
	cmdInRelease           = 0x52
	cmdInSelect            = 0x54
	cmdInAutoPoll          = 0x60
	cmdPowerDown           = 0x16
	cmdInCommunicateThru   = 0x42
	cmdRFConfiguration     = 0x32
)

// PowerDownWakeupFlags provides constants for PowerDown wake-up sources
const (
	WakeupHSU     byte = 0x01 // Wake-up by High Speed UART
	WakeupSPI     byte = 0x02 // Wake-up by SPI
	WakeupI2C     byte = 0x04 // Wake-up by I2C
	WakeupGPIOP32 byte = 0x08 // Wake-up by GPIO P32
	WakeupGPIOP34 byte = 0x10 // Wake-up by GPIO P34
	WakeupRF      byte = 0x20 // Wake-up by RF field
	WakeupINT1    byte = 0x80 // Wake-up by GPIO P72/INT1
)
