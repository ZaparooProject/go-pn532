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
	"fmt"
	"os"
)

// debugEnabled controls whether debug logging is active
// This can be controlled via build tags or environment variables
var debugEnabled = false

func init() {
	// Enable debug logging if DEBUG environment variable is set
	if os.Getenv("PN532_DEBUG") != "" || os.Getenv("DEBUG") != "" {
		debugEnabled = true
	}
}

// debugf prints debug information only when debug mode is enabled
// This eliminates the performance overhead of fmt.Printf in production
func debugf(format string, args ...any) {
	if debugEnabled {
		_, _ = fmt.Printf("DEBUG: "+format+"\n", args...)
	}
}

// debugln prints debug information only when debug mode is enabled
// This eliminates the performance overhead of fmt.Printf in production
func debugln(args ...any) {
	if debugEnabled {
		_, _ = fmt.Print("DEBUG: ")
		_, _ = fmt.Println(args...)
	}
}

// SetDebugEnabled allows programmatic control of debug logging
// Useful for testing or application-controlled debug modes
func SetDebugEnabled(enabled bool) {
	debugEnabled = enabled
}

// IsDebugEnabled returns the current debug logging state
func IsDebugEnabled() bool {
	return debugEnabled
}
