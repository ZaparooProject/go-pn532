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

package detection

import (
	"testing"
)

func TestIsPathIgnored(t *testing.T) {
	t.Parallel()

	tests := getPathIgnoredTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IsPathIgnored(tt.devicePath, tt.ignorePaths)
			if result != tt.expected {
				t.Errorf("IsPathIgnored(%q, %v) = %v, want %v",
					tt.devicePath, tt.ignorePaths, result, tt.expected)
			}
		})
	}
}

type pathIgnoredTest struct {
	name        string
	devicePath  string
	ignorePaths []string
	expected    bool
}

//nolint:funlen // Test data function, acceptable to be longer
func getPathIgnoredTests() []pathIgnoredTest {
	basicTests := []pathIgnoredTest{
		{
			name:        "empty ignore list",
			devicePath:  "/dev/ttyUSB0",
			ignorePaths: []string{},
			expected:    false,
		},
		{
			name:        "empty device path",
			devicePath:  "",
			ignorePaths: []string{"/dev/ttyUSB0"},
			expected:    false,
		},
		{
			name:        "exact match unix path",
			devicePath:  "/dev/ttyUSB0",
			ignorePaths: []string{"/dev/ttyUSB0"},
			expected:    true,
		},
		{
			name:        "exact match windows path",
			devicePath:  "COM2",
			ignorePaths: []string{"COM2"},
			expected:    true,
		},
	}

	caseTests := []pathIgnoredTest{
		{
			name:        "case insensitive match",
			devicePath:  "/dev/ttyUSB0",
			ignorePaths: []string{"/DEV/TTYUSB0"},
			expected:    true,
		},
		{
			name:        "windows case insensitive",
			devicePath:  "com2",
			ignorePaths: []string{"COM2"},
			expected:    true,
		},
	}

	multipleTests := []pathIgnoredTest{
		{
			name:        "no match",
			devicePath:  "/dev/ttyUSB1",
			ignorePaths: []string{"/dev/ttyUSB0"},
			expected:    false,
		},
		{
			name:        "multiple paths with match",
			devicePath:  "/dev/ttyUSB1",
			ignorePaths: []string{"/dev/ttyUSB0", "/dev/ttyUSB1", "COM2"},
			expected:    true,
		},
		{
			name:        "multiple paths no match",
			devicePath:  "/dev/ttyUSB2",
			ignorePaths: []string{"/dev/ttyUSB0", "/dev/ttyUSB1", "COM2"},
			expected:    false,
		},
	}

	specialTests := []pathIgnoredTest{
		{
			name:        "i2c path format",
			devicePath:  "/dev/i2c-1:0x24",
			ignorePaths: []string{"/dev/i2c-1:0x24"},
			expected:    true,
		},
		{
			name:        "spi path format",
			devicePath:  "/dev/spidev0.0",
			ignorePaths: []string{"/dev/spidev0.0"},
			expected:    true,
		},
		{
			name:        "path with relative components",
			devicePath:  "/dev/../dev/ttyUSB0",
			ignorePaths: []string{"/dev/ttyUSB0"},
			expected:    true,
		},
		{
			name:        "empty strings in ignore list",
			devicePath:  "/dev/ttyUSB0",
			ignorePaths: []string{"", "/dev/ttyUSB0", ""},
			expected:    true,
		},
	}

	result := make([]pathIgnoredTest, 0, len(basicTests)+len(caseTests)+len(multipleTests)+len(specialTests))
	result = append(result, basicTests...)
	result = append(result, caseTests...)
	result = append(result, multipleTests...)
	result = append(result, specialTests...)
	return result
}

func TestOptionsWithIgnorePaths(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	if opts.IgnorePaths != nil {
		t.Errorf("DefaultOptions().IgnorePaths should be nil, got %v", opts.IgnorePaths)
	}

	// Test that we can set ignore paths
	opts.IgnorePaths = []string{"/dev/ttyUSB0", "COM2"}
	if len(opts.IgnorePaths) != 2 {
		t.Errorf("Expected 2 ignore paths, got %d", len(opts.IgnorePaths))
	}
}
