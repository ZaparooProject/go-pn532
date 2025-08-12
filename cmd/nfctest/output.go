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

package main

import (
	"errors"
	"fmt"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
)

// Output handles consistent formatting of messages
type Output struct {
	verbose bool
}

// NewOutput creates a new output handler
func NewOutput(verbose bool) *Output {
	return &Output{verbose: verbose}
}

// ReaderTestHeader prints the appropriate header for reader testing
func (o *Output) ReaderTestHeader(reader detection.DeviceInfo) {
	if o.verbose {
		_, _ = fmt.Printf("Testing reader: %s\n", reader.String())
	} else {
		_, _ = fmt.Printf("Testing %s reader at %s... ", reader.Transport, reader.Path)
	}
}

// TestFailure prints failure indicator for non-verbose mode
func (o *Output) TestFailure() {
	if !o.verbose {
		_, _ = fmt.Print("FAIL\n")
	}
}

// TestSuccess prints success message with firmware version
func (o *Output) TestSuccess(reader detection.DeviceInfo, version *pn532.FirmwareVersion) {
	if o.verbose {
		_, _ = fmt.Printf("   OK: Firmware: %s\n", version.Version)
		_, _ = fmt.Printf("   OK: Device: %s\n", reader.Path)
	} else {
		_, _ = fmt.Printf("OK: (firmware v%s)\n", version.Version)
	}
}

// NewCardDetected prints message for newly detected card
func (*Output) NewCardDetected(readerPath, cardType, currentUID string) {
	_, _ = fmt.Printf("\nCARD: Card detected on %s: %s (UID: %s)\n",
		readerPath, cardType, currentUID)
}

// DifferentCardDetected prints message for different card detected
func (*Output) DifferentCardDetected(readerPath, cardType, currentUID string) {
	_, _ = fmt.Printf("\nCARD: New card detected on %s: %s (UID: %s)\n",
		readerPath, cardType, currentUID)
}

// NDEFResults prints NDEF results in a standard format
func (o *Output) NDEFResults(ndef *pn532.NDEFMessage, err error) {
	if err != nil {
		o.ndefError(err)
		return
	}

	// Get NDEF record count for display
	_, _ = fmt.Printf(" OK: Found %d record(s)\n", len(ndef.Records))
	// Always show NDEF content, not just in verbose mode
	for i, record := range ndef.Records {
		o.ndefRecord(i, &record)
	}
}

func (*Output) ndefError(err error) {
	if errors.Is(err, pn532.ErrNoNDEF) {
		_, _ = fmt.Print(" WARNING: No NDEF data\n")
	} else {
		_, _ = fmt.Printf(" ERROR: Failed: %v\n", err)
	}
}

func (*Output) ndefRecord(i int, record *pn532.NDEFRecord) {
	_, _ = fmt.Printf("      Record %d: Type=%s\n", i, record.Type)
	if record.Text != "" {
		_, _ = fmt.Printf("        TEXT: %s\n", record.Text)
	}
	if record.URI != "" {
		_, _ = fmt.Printf("        URI: %s\n", record.URI)
	}
	if record.WiFi != nil {
		_, _ = fmt.Printf("        WiFi: %s\n", record.WiFi.SSID)
	}
	if record.VCard != nil {
		_, _ = fmt.Printf("        VCard: %s\n", record.VCard.FormattedName)
	}
}

// Error prints an error message
func (*Output) Error(format string, args ...any) {
	_, _ = fmt.Printf("ERROR: "+format+"\n", args...)
}

// Warning prints a warning message
func (*Output) Warning(format string, args ...any) {
	_, _ = fmt.Printf("WARNING: "+format+"\n", args...)
}

// Info prints an info message
func (*Output) Info(format string, args ...any) {
	_, _ = fmt.Printf("INFO: "+format+"\n", args...)
}

// OK prints a success message
func (*Output) OK(format string, args ...any) {
	_, _ = fmt.Printf("OK: "+format+"\n", args...)
}

// Verbose prints only if verbose mode is enabled
func (o *Output) Verbose(format string, args ...any) {
	if o.verbose {
		_, _ = fmt.Printf(format+"\n", args...)
	}
}
