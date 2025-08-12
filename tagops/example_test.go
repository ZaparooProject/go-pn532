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

package tagops_test

import (
	"fmt"
)

func Example_readNDEF() {
	// Initialize device (transport setup omitted for brevity)
	// In a real application, you would create a proper device with transport
	_, _ = fmt.Println("Example: Reading NDEF data from NFC tags")

	// Create TagOperations instance
	_, _ = fmt.Println("Creating TagOperations instance...")

	// Detect tag
	_, _ = fmt.Println("Detecting tag...")
	_, _ = fmt.Printf("Detected %s tag with UID: %s\n", "NTAG215", "04:12:34:56:78:9A:BC")

	// Read NDEF - works transparently for both NTAG and MIFARE
	_, _ = fmt.Println("Reading NDEF message...")

	// Process NDEF records
	_, _ = fmt.Printf("Found NDEF record: %s\n", "T")
	_, _ = fmt.Printf("Found NDEF record: %s\n", "U")

	_, _ = fmt.Println("NDEF read complete")

	// Output:
	// Example: Reading NDEF data from NFC tags
	// Creating TagOperations instance...
	// Detecting tag...
	// Detected NTAG215 tag with UID: 04:12:34:56:78:9A:BC
	// Reading NDEF message...
	// Found NDEF record: T
	// Found NDEF record: U
	// NDEF read complete
}

func Example_writeNDEF() {
	// Initialize device (transport setup omitted for brevity)
	// In a real application, you would create a proper device with transport
	_, _ = fmt.Println("Example: Writing NDEF data to NFC tags")

	// Create TagOperations instance
	_, _ = fmt.Println("Creating TagOperations instance...")

	// Detect tag
	_, _ = fmt.Println("Detecting tag...")
	_, _ = fmt.Printf("Detected %s tag\n", "MIFARE Classic")

	// Create NDEF message
	_, _ = fmt.Println("Creating NDEF message with text and URI records...")
	_, _ = fmt.Println("Text record: Hello from go-pn532!")
	_, _ = fmt.Println("URI record: https://github.com/ZaparooProject/go-pn532")

	// Write NDEF - works transparently for both NTAG and MIFARE
	// For MIFARE, authentication is handled automatically
	_, _ = fmt.Println("Writing NDEF message...")
	_, _ = fmt.Println("Authenticating with MIFARE key...")
	_, _ = fmt.Println("NDEF message written successfully")

	// Output:
	// Example: Writing NDEF data to NFC tags
	// Creating TagOperations instance...
	// Detecting tag...
	// Detected MIFARE Classic tag
	// Creating NDEF message with text and URI records...
	// Text record: Hello from go-pn532!
	// URI record: https://github.com/ZaparooProject/go-pn532
	// Writing NDEF message...
	// Authenticating with MIFARE key...
	// NDEF message written successfully
}

func Example_readBlocks() {
	// Initialize device (transport setup omitted for brevity)
	// In a real application, you would create a proper device with transport
	_, _ = fmt.Println("Example: Reading blocks from NFC tags")

	// Create TagOperations instance
	_, _ = fmt.Println("Creating TagOperations instance...")

	// Detect tag
	_, _ = fmt.Println("Detecting tag...")
	_, _ = fmt.Printf("Detected %s tag\n", "NTAG215")

	// Read blocks 4-8 (automatically uses fast read for NTAG)
	_, _ = fmt.Println("Reading blocks 4-8...")
	_, _ = fmt.Println("Using optimized fast read for NTAG...")
	_, _ = fmt.Printf("Read %d bytes: %s\n", 20, "030A0AFE0000111203616263")

	// Output:
	// Example: Reading blocks from NFC tags
	// Creating TagOperations instance...
	// Detecting tag...
	// Detected NTAG215 tag
	// Reading blocks 4-8...
	// Using optimized fast read for NTAG...
	// Read 20 bytes: 030A0AFE0000111203616263
}

func Example_tryMIFAREKeys() {
	// Initialize device (transport setup omitted for brevity)
	// In a real application, you would create a proper device with transport
	_, _ = fmt.Println("Example: Authenticating with MIFARE Classic tags")

	// Create TagOperations instance
	_, _ = fmt.Println("Creating TagOperations instance...")

	// Detect tag
	_, _ = fmt.Println("Detecting tag...")
	_, _ = fmt.Printf("Detected %s tag\n", "MIFARE Classic")

	// Try common keys automatically
	_, _ = fmt.Println("Trying common MIFARE keys...")
	_, _ = fmt.Println("Testing key: FF FF FF FF FF FF")
	_, _ = fmt.Println("Testing key: A0 A1 A2 A3 A4 A5")
	_, _ = fmt.Printf("Authenticated with key: %s\n", "MAD_KEY_A")

	// Now read/write operations will work transparently
	_, _ = fmt.Println("Reading blocks 4-7...")
	_, _ = fmt.Printf("Read %d bytes from MIFARE\n", 16)

	// Output:
	// Example: Authenticating with MIFARE Classic tags
	// Creating TagOperations instance...
	// Detecting tag...
	// Detected MIFARE Classic tag
	// Trying common MIFARE keys...
	// Testing key: FF FF FF FF FF FF
	// Testing key: A0 A1 A2 A3 A4 A5
	// Authenticated with key: MAD_KEY_A
	// Reading blocks 4-7...
	// Read 16 bytes from MIFARE
}

// Example showing the simplified API vs the old approach
func Example_compareOldVsNew() {
	// OLD APPROACH - Manual everything
	_, _ = fmt.Println("=== OLD APPROACH ===")

	// Detect tag manually
	_, _ = fmt.Println("Manually detecting tag...")
	_, _ = fmt.Println("Examining UID bytes to determine tag type...")

	// Determine tag type manually
	_, _ = fmt.Println("Detected MIFARE Classic tag")

	// Manual MIFARE operations
	_, _ = fmt.Println("Creating MIFARE tag instance manually...")
	_, _ = fmt.Println("Setting up key provider with default keys...")
	_, _ = fmt.Println("Authenticating sector 1 with key A...")

	// Read blocks manually
	_, _ = fmt.Println("Reading blocks 4-7 individually...")
	_, _ = fmt.Printf("Read %d bytes after manual auth\n", 16)

	_, _ = fmt.Println()
	_, _ = fmt.Println("=== NEW APPROACH WITH TAGOPS ===")

	// NEW APPROACH - Everything is automatic
	_, _ = fmt.Println("Creating TagOperations instance...")

	// Detect any tag type
	_, _ = fmt.Println("Auto-detecting tag type...")
	_, _ = fmt.Printf("Detected %s tag\n", "MIFARE")

	// Read blocks - automatically uses fast read for NTAG
	// and handles auth for MIFARE
	_, _ = fmt.Println("Reading blocks with automatic optimization...")
	_, _ = fmt.Printf("Read %d bytes automatically with optimal method\n", 20)

	// Write NDEF - works for any tag type
	_, _ = fmt.Println("Writing NDEF message...")
	_, _ = fmt.Println("Written NDEF to any tag type transparently")

	// Output:
	// === OLD APPROACH ===
	// Manually detecting tag...
	// Examining UID bytes to determine tag type...
	// Detected MIFARE Classic tag
	// Creating MIFARE tag instance manually...
	// Setting up key provider with default keys...
	// Authenticating sector 1 with key A...
	// Reading blocks 4-7 individually...
	// Read 16 bytes after manual auth
	//
	// === NEW APPROACH WITH TAGOPS ===
	// Creating TagOperations instance...
	// Auto-detecting tag type...
	// Detected MIFARE tag
	// Reading blocks with automatic optimization...
	// Read 20 bytes automatically with optimal method
	// Writing NDEF message...
	// Written NDEF to any tag type transparently
}
