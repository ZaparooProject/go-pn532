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
	"time"

	"github.com/ZaparooProject/go-pn532"
)

// Operating modes
type Mode int

const (
	ModeComprehensive Mode = iota
	ModeQuick
	ModeVendorTest
)

// Transport type constants for type-safe transport detection
const (
	TransportUART = "uart"
	TransportI2C  = "i2c"
	TransportSPI  = "spi"
)

// Config holds application configuration
type Config struct {
	Mode           Mode
	ConnectTimeout time.Duration
	DetectTimeout  time.Duration
	PollInterval   time.Duration
	Verbose        bool
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Mode:           ModeComprehensive,
		ConnectTimeout: 10 * time.Second,
		DetectTimeout:  30 * time.Second,
		PollInterval:   50 * time.Millisecond,
		Verbose:        false,
	}
}

// TestMode represents the testing mode configuration
type TestMode struct {
	Quick bool
}

// CardState tracks the state of a card on a reader
type CardState struct {
	LastUID   string
	LastType  string
	TestedUID string
	Present   bool
}

// MonitoringSetup holds monitoring configuration
type MonitoringSetup struct {
	Devices     []*pn532.Device
	ReaderPaths []string
	CardStates  []CardState
}

// ErrNoTagInPoll indicates no tag was detected during polling (not an error condition)
var ErrNoTagInPoll = errors.New("no tag detected in polling cycle")
