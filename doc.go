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

/*
Package pn532 provides a pure Go library for interfacing with PN532 NFC/RFID controllers.

The PN532 is a highly integrated transceiver module for contactless communication
at 13.56 MHz, supporting various protocols including ISO14443A/B and FeliCa.
This library provides a clean, idiomatic Go interface for working with PN532 devices
across multiple transport layers.

Features:
  - Multiple transport support: UART, I2C, SPI
  - Support for NTAG21x and MIFARE Classic tags
  - NDEF (NFC Data Exchange Format) message reading and writing
  - Cross-platform compatibility (Linux, Windows, macOS)
  - Automatic tag detection and identification
  - Retry logic with configurable backoff
  - Comprehensive error handling

Basic Usage:

	import (
	    "github.com/ZaparooProject/go-pn532"
	    "github.com/ZaparooProject/go-pn532/transport/uart"
	)

	// Create a UART transport
	transport, err := uart.New("/dev/ttyUSB0")
	if err != nil {
	    log.Fatal(err)
	}
	defer transport.Close()

	// Create and initialize the PN532 device
	device, err := pn532.New(transport)

	if err != nil {

		return err

	}
	if err := device.Init(); err != nil {
	    log.Fatal(err)
	}

	// Or create with custom options
	device = pn532.New(transport,
	    pn532.WithTimeout(2*time.Second),
	    pn532.WithMaxRetries(5),
	)

	// Detect a tag
	tag, err := device.DetectTag()
	if err != nil {
	    log.Fatal(err)
	}

	if tag != nil {
	    fmt.Printf("Tag detected: %s\n", tag.UID())

	    // Create appropriate tag handler
	    nfcTag, err := device.CreateTag(tag)
	    if err != nil {
	        log.Fatal(err)
	    }

	    // Read NDEF message
	    msg, err := nfcTag.ReadNDEF()
	    if err != nil {
	        log.Fatal(err)
	    }

	    fmt.Printf("NDEF Content: %s\n", msg.Records[0].Text)
	}

Transport Selection:

The library supports multiple transport layers:

  - UART: Most common, works with USB-to-serial adapters
  - I2C: For embedded systems with I2C bus
  - SPI: High-speed communication for embedded systems

Tag Support:

Currently supported tag types:
  - NTAG213/215/216 (NFC Forum Type 2)
  - MIFARE Classic 1K/4K (with NDEF format)

NDEF Support:

The library includes comprehensive NDEF support:
  - Text records
  - URI records
  - WiFi credential records
  - vCard contact records
  - Smart poster records

Error Handling:

All operations return meaningful errors that can be inspected:

	if errors.Is(err, pn532.ErrTimeout) {
	    // Handle timeout
	}

Thread Safety:

Device operations are not thread-safe. If you need concurrent access,
implement appropriate synchronization in your application.
*/
package pn532
