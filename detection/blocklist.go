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
	"path/filepath"
	"strings"
)

// DefaultBlocklist returns a list of known problematic USB devices
// that should not be probed during detection.
// Format: VID:PID in hexadecimal (case-insensitive).
func DefaultBlocklist() []string {
	return []string{
		// Add known problematic devices here as discovered
		// Example entries:
		// "1234:5678", // Vendor X device that crashes on probe
		// "ABCD:EF01", // Device Y that hangs on PN532 commands
	}
}

// IsBlocked checks if a USB device is in the blocklist.
func IsBlocked(vidpid string, blocklist []string) bool {
	// Normalize to uppercase for comparison
	vidpid = strings.ToUpper(strings.TrimSpace(vidpid))

	for _, blocked := range blocklist {
		blocked = strings.ToUpper(strings.TrimSpace(blocked))
		if vidpid == blocked {
			return true
		}
	}
	return false
}

// ParseVIDPID extracts VID:PID from various USB descriptor formats.
func ParseVIDPID(descriptor string) string {
	// Handle common formats:
	// "VID:1234 PID:5678"
	// "1234:5678"
	// "vendor=1234 product=5678"

	descriptor = strings.ToUpper(descriptor)

	// Try to find VID and PID separately
	var vid, pid string

	// Look for VID
	if idx := strings.Index(descriptor, "VID:"); idx >= 0 {
		vid = extractHex(descriptor[idx+4:])
	} else if idx := strings.Index(descriptor, "VENDOR="); idx >= 0 {
		vid = extractHex(descriptor[idx+7:])
	} else if idx := strings.Index(descriptor, "VID="); idx >= 0 {
		vid = extractHex(descriptor[idx+4:])
	}

	// Look for PID
	if idx := strings.Index(descriptor, "PID:"); idx >= 0 {
		pid = extractHex(descriptor[idx+4:])
	} else if idx := strings.Index(descriptor, "PRODUCT="); idx >= 0 {
		pid = extractHex(descriptor[idx+8:])
	} else if idx := strings.Index(descriptor, "PID="); idx >= 0 {
		pid = extractHex(descriptor[idx+4:])
	}

	// If we found both, return in standard format
	if vid != "" && pid != "" {
		return vid + ":" + pid
	}

	// Try simple VID:PID format
	if strings.Count(descriptor, ":") == 1 {
		parts := strings.Split(descriptor, ":")
		if len(parts) == 2 && isHex(parts[0]) && isHex(parts[1]) {
			return descriptor
		}
	}

	return ""
}

// extractHex extracts the first sequence of hex digits from a string.
func extractHex(s string) string {
	var result strings.Builder
	foundHex := false

	for _, r := range s {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') {
			_, _ = result.WriteRune(r)
			foundHex = true
		} else if foundHex {
			// Stop at first non-hex character after finding hex
			break
		}
	}
	return result.String()
}

// isHex checks if a string contains only hexadecimal characters.
func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'A' || r > 'F') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// IsPathIgnored checks if a device path should be ignored.
// Supports exact path matching and normalized path comparison.
func IsPathIgnored(devicePath string, ignorePaths []string) bool {
	if devicePath == "" || len(ignorePaths) == 0 {
		return false
	}

	// Normalize the device path for comparison
	normalizedDevice := normalizedPath(devicePath)

	for _, ignorePath := range ignorePaths {
		if ignorePath == "" {
			continue
		}

		normalizedIgnore := normalizedPath(ignorePath)

		// Exact match
		if normalizedDevice == normalizedIgnore {
			return true
		}

		// Also check original paths for exact match
		if devicePath == ignorePath {
			return true
		}
	}
	return false
}

// normalizedPath normalizes a device path for comparison
func normalizedPath(path string) string {
	// Clean the path to resolve any relative components
	cleaned := filepath.Clean(path)

	// Convert to lowercase for case-insensitive comparison on Windows
	return strings.ToLower(cleaned)
}
