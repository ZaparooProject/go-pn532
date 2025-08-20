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
	"context"
	"testing"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
)

func TestRunWriteMode_SimplifiedErrorHandling(t *testing.T) {
	t.Parallel()
	// Create mock device and transport
	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	if err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Setup mock responses for WriteToNextTag flow
	mockTransport.SetResponse(0x4A, []byte{
		0xD5, 0x4B, 0x01, 0x01, 0x00, 0x04, 0x00, 0x07, 0x04,
		0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
	}) // InListPassiveTarget response
	mockTransport.SetResponse(0x54, []byte{0x55, 0x00}) // InSelect response (cmd 0x54, response 0x55, status 0x00)
	mockTransport.SetResponse(0x40, []byte{0x41, 0x00}) // DataExchange response for NDEF write

	// Test config
	cfg := &config{
		writeText: "test text",
		debug:     false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test should demonstrate that the current error handling is too complex
	// We expect this to work, but want to simplify the error handling logic
	err = runWriteMode(ctx, device, cfg)
	if err != nil {
		t.Errorf("runWriteMode failed: %v", err)
	}
}
