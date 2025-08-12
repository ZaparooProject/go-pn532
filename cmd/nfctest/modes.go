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
	"time"

	"github.com/ZaparooProject/go-pn532/detection"
)

// Modes handles the different operating modes
type Modes struct {
	config     *Config
	output     *Output
	discovery  *Discovery
	monitoring *Monitoring
	testing    *Testing
}

// NewModes creates a new modes handler
func NewModes(config *Config, output *Output, discovery *Discovery, monitoring *Monitoring, testing *Testing) *Modes {
	return &Modes{
		config:     config,
		output:     output,
		discovery:  discovery,
		monitoring: monitoring,
		testing:    testing,
	}
}

// RunComprehensive runs full testing of readers and cards
func (m *Modes) RunComprehensive(ctx context.Context) error {
	fmt.Println("NFC Test Tool - Comprehensive Mode")
	fmt.Println("=====================================")

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
		if err := m.testing.TestReader(ctx, reader, TestMode{Quick: false}); err != nil {
			m.output.Warning("Reader test failed: %v", err)
			continue
		}
	}

	// Start continuous card monitoring
	return m.monitoring.MonitorCards(ctx, readers, false)
}

// RunQuick runs lighter, faster testing cycles
func (m *Modes) RunQuick(ctx context.Context) error {
	fmt.Println("NFC Test Tool - Quick Mode")
	fmt.Println("=============================")

	// Discover readers with shorter timeout
	quickCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	readers, err := m.discovery.DiscoverReaders(quickCtx)
	if err != nil {
		return err
	}

	if len(readers) == 0 {
		return errors.New("no PN532 readers found")
	}

	// Quick test each reader
	for _, reader := range readers {
		if err := m.testing.TestReader(ctx, reader, TestMode{Quick: true}); err != nil {
			m.output.Warning("Reader test failed: %v", err)
			continue
		}
	}

	// Start quick card monitoring
	return m.monitoring.MonitorCards(ctx, readers, true)
}

// RunVendorTest runs continuous operation for testing readers being sold
func (m *Modes) RunVendorTest(ctx context.Context) error {
	fmt.Println("NFC Test Tool - Vendor Test Mode")
	fmt.Println("=================================")
	fmt.Println("Continuous monitoring for readers and cards...")
	fmt.Println("   (Ctrl+C to quit)")

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastReaders []detection.DeviceInfo

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			readers, err := m.discovery.DiscoverReaders(ctx)
			if err != nil {
				m.discovery.HandleDiscoveryError(err)
				continue
			}

			lastReaders = m.processReaderChanges(ctx, lastReaders, readers)
		}
	}
}

// processReaderChanges handles reader connection/disconnection changes
func (m *Modes) processReaderChanges(ctx context.Context, lastReaders, readers []detection.DeviceInfo) []detection.DeviceInfo {
	// Check for new readers
	newReaders := m.discovery.FindNewReaders(lastReaders, readers)
	for _, reader := range newReaders {
		fmt.Printf("New reader detected: %s\n", reader.String())
		if err := m.testing.TestReader(ctx, reader, TestMode{Quick: true}); err != nil {
			m.output.Error("Reader test failed: %v", err)
		} else {
			m.output.OK("Reader test passed")
		}
	}

	// Check for disconnected readers
	disconnected := m.discovery.FindDisconnectedReaders(lastReaders, readers)
	for _, reader := range disconnected {
		fmt.Printf("Reader disconnected: %s\n", reader.String())
	}

	// Quick card check on all readers
	if len(readers) > 0 {
		m.monitoring.CheckCardsQuick(readers)
	}

	return readers
}
