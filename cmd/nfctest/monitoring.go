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

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/ZaparooProject/go-pn532/polling"
)

// Monitoring handles card monitoring and polling
type Monitoring struct {
	config    *Config
	output    *Output
	discovery *Discovery
	testing   *Testing
}

// NewMonitoring creates a new monitoring handler
func NewMonitoring(config *Config, output *Output, discovery *Discovery, testing *Testing) *Monitoring {
	return &Monitoring{
		config:    config,
		output:    output,
		discovery: discovery,
		testing:   testing,
	}
}

// MonitorCards continuously monitors for cards using proper InAutoPoll continuous polling
func (m *Monitoring) MonitorCards(ctx context.Context, readers []detection.DeviceInfo) error {
	_, _ = fmt.Println("\nMonitoring for cards... (Ctrl+C to quit)")

	setup, err := m.initializeDevices(readers)
	if err != nil {
		return err
	}

	// Clean up all devices on exit
	defer func() {
		for _, monitor := range setup.Monitors {
			_ = monitor.Close()
		}
	}()

	return m.startMonitoringLoop(ctx, setup)
}

func (m *Monitoring) initializeDevices(readers []detection.DeviceInfo) (*MonitoringSetup, error) {
	setup := &MonitoringSetup{
		Monitors:    make([]*polling.Monitor, 0, len(readers)),
		ReaderPaths: make([]string, 0, len(readers)),
	}

	for _, reader := range readers {
		device, err := m.createDevice(reader)
		if err != nil {
			m.output.Warning("Failed to create device for %s: %v", reader.Path, err)
			continue
		}

		// Create polling config from main config
		pollingConfig := &polling.Config{
			PollInterval:       m.config.PollInterval,
			CardRemovalTimeout: m.config.CardRemovalTimeout,
		}

		monitor := polling.NewMonitor(device, pollingConfig)

		// Set up event callbacks (capture variables to avoid closure issues)
		readerPath := reader.Path
		monitor.OnCardDetected = func(tag *pn532.DetectedTag) error {
			m.output.NewCardDetected(readerPath, string(tag.Type), tag.UID)
			// Test the card
			if err := m.testing.TestCard(device, tag); err != nil {
				m.output.Error("Card test failed: %v", err)
			} else {
				m.output.OK("Card test completed")
			}
			return nil
		}
		monitor.OnCardRemoved = func() {
			m.output.Info("Card removed from %s", readerPath)
		}
		monitor.OnCardChanged = func(tag *pn532.DetectedTag) error {
			m.output.DifferentCardDetected(readerPath, string(tag.Type), tag.UID)
			// Test the card again since it changed
			if err := m.testing.TestCard(device, tag); err != nil {
				m.output.Error("Card test failed: %v", err)
			} else {
				m.output.OK("Card test completed")
			}
			return nil
		}

		setup.Monitors = append(setup.Monitors, monitor)
		setup.ReaderPaths = append(setup.ReaderPaths, reader.Path)
	}

	if len(setup.Monitors) == 0 {
		return nil, errors.New("no functional readers available for monitoring")
	}

	return setup, nil
}

func (m *Monitoring) createDevice(reader detection.DeviceInfo) (*pn532.Device, error) {
	transport, err := m.discovery.CreateTransport(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	device, err := pn532.New(transport, pn532.WithTimeout(5*time.Second))
	if err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("failed to create PN532 device: %w", err)
	}

	// Initialize device (SAM configuration, etc.)
	if err := device.Init(); err != nil {
		_ = device.Close()
		_ = transport.Close()
		return nil, fmt.Errorf("failed to initialize device: %w", err)
	}

	// No polling configuration needed - simplified approach uses direct timeout-based detection
	return device, nil
}

func (*Monitoring) startMonitoringLoop(ctx context.Context, setup *MonitoringSetup) error {
	// Start continuous polling for each reader in separate goroutines
	errChan := make(chan error, len(setup.Monitors))
	for i, monitor := range setup.Monitors {
		go func(_ int, mon *polling.Monitor) {
			err := mon.Start(ctx)
			errChan <- err
		}(i, monitor)
	}

	// Wait for context cancellation or first error
	select {
	case <-ctx.Done():
		return nil
	case err := <-errChan:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	}
}
