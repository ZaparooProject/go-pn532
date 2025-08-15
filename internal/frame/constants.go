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

// Package frame provides frame manipulation and protocol constants for PN532 communication
package frame

// Frame direction constants - these indicate the direction of data flow
const (
	HostToPn532 = 0xD4 // Commands from host to PN532
	Pn532ToHost = 0xD5 // Responses from PN532 to host
)

// Frame markers and control bytes
const (
	Preamble   = 0x00 // Frame preamble byte
	StartCode1 = 0x00 // Start code byte 1
	StartCode2 = 0xFF // Start code byte 2
	Postamble  = 0x00 // Frame postamble byte
)

// Frame size limits
const (
	MaxFrameDataLength = 263 // Maximum data length in frame (PN532 spec)
	MinFrameLength     = 6   // Minimum frame length (preamble + startcode + len + lcs + tfi + dcs)
)

// ACK and NACK frames - these are used for flow control
var (
	AckFrame  = []byte{0x00, 0x00, 0xFF, 0x00, 0xFF, 0x00}
	NackFrame = []byte{0x00, 0x00, 0xFF, 0xFF, 0x00, 0x00}
)
