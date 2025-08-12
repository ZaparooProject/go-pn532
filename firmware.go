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

// FirmwareVersion contains PN532 firmware information
type FirmwareVersion struct {
	Version          string
	SupportIso14443a bool
	SupportIso14443b bool
	SupportIso18092  bool
}

// GeneralStatus contains PN532 general status information
type GeneralStatus struct {
	LastError    byte
	FieldPresent bool
	Targets      byte
}

// GetFirmwareVersion returns the PN532 firmware version
func (d *Device) GetFirmwareVersion() (*FirmwareVersion, error) {
	return d.GetFirmwareVersionContext(context.Background())
}

// GetGeneralStatus returns the PN532 general status
func (d *Device) GetGeneralStatus() (*GeneralStatus, error) {
	return d.GetGeneralStatusContext(context.Background())
}

// DiagnoseResult contains the result of a diagnose test
type DiagnoseResult struct {
	Data       []byte
	TestNumber byte
	Success    bool
}

// DiagnoseTestNumber constants
const (
	DiagnoseCommunicationTest = 0x00
	DiagnoseROMTest           = 0x01
	DiagnoseRAMTest           = 0x02
	// 0x03 is not used
	DiagnosePollingTest     = 0x04
	DiagnoseEchoBackTest    = 0x05
	DiagnoseAttentionTest   = 0x06
	DiagnoseSelfAntennaTest = 0x07
)

// Diagnose performs a self-diagnosis test
func (d *Device) Diagnose(testNumber byte, data []byte) (*DiagnoseResult, error) {
	return d.DiagnoseContext(context.Background(), testNumber, data)
}
