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
	"errors"
	"fmt"
)

// Modes handles the different operating modes
type Modes struct {
	config         *Config
	output         *Output
	discovery      *Discovery
	sessionManager *SessionManager
	testing        *Testing
}

// NewModes creates a new modes handler
func NewModes(
	config *Config, output *Output, discovery *Discovery, sessionManager *SessionManager, testing *Testing,
) *Modes {
	return &Modes{
		config:         config,
		output:         output,
		discovery:      discovery,
		sessionManager: sessionManager,
		testing:        testing,
	}
}

// RunComprehensive runs full testing of readers and cards
func (m *Modes) RunComprehensive(ctx context.Context) error {
	_, _ = fmt.Println("NFC Test Tool - Comprehensive Mode")
	_, _ = fmt.Println("=====================================")

	// Discover readers
	readers, err := m.discovery.DiscoverReaders(ctx)
	if err != nil {
		return err
	}

	if len(readers) == 0 {
		return errors.New("no PN532 readers found")
	}

	// Test each reader
	for _, reader := range readers {
		if err := m.testing.TestReader(ctx, reader); err != nil {
			m.output.Warning("Reader test failed: %v", err)
			continue
		}
	}

	// Start continuous card monitoring
	return m.sessionManager.MonitorCards(ctx, readers)
}
