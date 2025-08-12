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
	"testing"
)

func TestCommandConstants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		constant byte
		expected byte
	}{
		{"cmdDiagnose", cmdDiagnose, 0x00},
		{"cmdSamConfiguration", cmdSamConfiguration, 0x14},
		{"cmdGetFirmwareVersion", cmdGetFirmwareVersion, 0x02},
		{"cmdGetGeneralStatus", cmdGetGeneralStatus, 0x04},
		{"cmdInListPassiveTarget", cmdInListPassiveTarget, 0x4A},
		{"cmdInDataExchange", cmdInDataExchange, 0x40},
		{"cmdInRelease", cmdInRelease, 0x52},
		{"cmdInSelect", cmdInSelect, 0x54},
		{"cmdInAutoPoll", cmdInAutoPoll, 0x60},
		{"cmdPowerDown", cmdPowerDown, 0x16},
		{"cmdInCommunicateThru", cmdInCommunicateThru, 0x42},
		{"cmdRFConfiguration", cmdRFConfiguration, 0x32},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.constant != tt.expected {
				t.Errorf("%s = 0x%02X, want 0x%02X", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestPowerDownWakeupFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		flag     byte
		expected byte
	}{
		{"WakeupHSU", WakeupHSU, 0x01},
		{"WakeupSPI", WakeupSPI, 0x02},
		{"WakeupI2C", WakeupI2C, 0x04},
		{"WakeupGPIOP32", WakeupGPIOP32, 0x08},
		{"WakeupGPIOP34", WakeupGPIOP34, 0x10},
		{"WakeupRF", WakeupRF, 0x20},
		{"WakeupINT1", WakeupINT1, 0x80},
	}

	for _, tt := range tests {
		tt := tt // capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.flag != tt.expected {
				t.Errorf("%s = 0x%02X, want 0x%02X", tt.name, tt.flag, tt.expected)
			}
		})
	}
}

func TestWakeupFlagsCombination(t *testing.T) {
	t.Parallel()
	// Test that flags can be combined with bitwise OR
	combined := WakeupHSU | WakeupI2C | WakeupRF
	expected := byte(0x01 | 0x04 | 0x20) // 0x25

	if combined != expected {
		t.Errorf("Combined flags = 0x%02X, want 0x%02X", combined, expected)
	}

	// Test individual flags are powers of 2 (except for gaps)
	powerOf2Tests := []struct {
		name string
		flag byte
	}{
		{"WakeupHSU", WakeupHSU},
		{"WakeupSPI", WakeupSPI},
		{"WakeupI2C", WakeupI2C},
		{"WakeupGPIOP32", WakeupGPIOP32},
		{"WakeupGPIOP34", WakeupGPIOP34},
		{"WakeupRF", WakeupRF},
		{"WakeupINT1", WakeupINT1},
	}

	for _, tt := range powerOf2Tests {
		tt := tt // capture loop variable
		t.Run(tt.name+"_power_of_2", func(t *testing.T) {
			t.Parallel()
			// Check that flag has exactly one bit set (is a power of 2)
			if tt.flag != 0 && (tt.flag&(tt.flag-1)) != 0 {
				t.Errorf("%s (0x%02X) is not a power of 2", tt.name, tt.flag)
			}
		})
	}
}

func TestCommandUniqueness(t *testing.T) {
	t.Parallel()
	commands := map[string]byte{
		"cmdDiagnose":            cmdDiagnose,
		"cmdSamConfiguration":    cmdSamConfiguration,
		"cmdGetFirmwareVersion":  cmdGetFirmwareVersion,
		"cmdGetGeneralStatus":    cmdGetGeneralStatus,
		"cmdInListPassiveTarget": cmdInListPassiveTarget,
		"cmdInDataExchange":      cmdInDataExchange,
		"cmdInRelease":           cmdInRelease,
		"cmdInSelect":            cmdInSelect,
		"cmdInAutoPoll":          cmdInAutoPoll,
		"cmdPowerDown":           cmdPowerDown,
		"cmdInCommunicateThru":   cmdInCommunicateThru,
		"cmdRFConfiguration":     cmdRFConfiguration,
	}

	// Check for duplicate values
	seen := make(map[byte]string)
	for name, value := range commands {
		if existing, exists := seen[value]; exists {
			t.Errorf("Duplicate command value 0x%02X: %s and %s", value, name, existing)
		}
		seen[value] = name
	}
}

func TestWakeupFlagUniqueness(t *testing.T) {
	t.Parallel()
	flags := map[string]byte{
		"WakeupHSU":     WakeupHSU,
		"WakeupSPI":     WakeupSPI,
		"WakeupI2C":     WakeupI2C,
		"WakeupGPIOP32": WakeupGPIOP32,
		"WakeupGPIOP34": WakeupGPIOP34,
		"WakeupRF":      WakeupRF,
		"WakeupINT1":    WakeupINT1,
	}

	// Check for duplicate values
	seen := make(map[byte]string)
	for name, value := range flags {
		if existing, exists := seen[value]; exists {
			t.Errorf("Duplicate wakeup flag value 0x%02X: %s and %s", value, name, existing)
		}
		seen[value] = name
	}
}
