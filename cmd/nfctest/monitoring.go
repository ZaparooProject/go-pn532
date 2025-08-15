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
func (m *Monitoring) MonitorCards(ctx context.Context, readers []detection.DeviceInfo, isQuick bool) error {
	_, _ = fmt.Println("\nMonitoring for cards... (Ctrl+C to quit)")

	setup, err := m.initializeDevices(readers)
	if err != nil {
		return err
	}

	// Clean up all devices on exit
	defer func() {
		for _, device := range setup.Devices {
			_ = device.Close()
		}
	}()

	return m.startMonitoringLoop(ctx, setup, isQuick)
}

func (m *Monitoring) initializeDevices(readers []detection.DeviceInfo) (*MonitoringSetup, error) {
	setup := &MonitoringSetup{
		Devices:     make([]*pn532.Device, 0, len(readers)),
		ReaderPaths: make([]string, 0, len(readers)),
		CardStates:  make([]CardState, 0, len(readers)),
	}

	for _, reader := range readers {
		device, err := m.createDevice(reader)
		if err != nil {
			m.output.Warning("Failed to create device for %s: %v", reader.Path, err)
			continue
		}

		setup.Devices = append(setup.Devices, device)
		setup.ReaderPaths = append(setup.ReaderPaths, reader.Path)
		setup.CardStates = append(setup.CardStates, CardState{})
	}

	if len(setup.Devices) == 0 {
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

func (m *Monitoring) startMonitoringLoop(ctx context.Context, setup *MonitoringSetup, isQuick bool) error {
	// Start continuous polling for each reader in separate goroutines
	errChan := make(chan error, len(setup.Devices))
	for i, device := range setup.Devices {
		go func(_ int, dev *pn532.Device, readerPath string, state *CardState) {
			err := m.continuousPolling(ctx, dev, readerPath, state, isQuick)
			errChan <- err
		}(i, device, setup.ReaderPaths[i], &setup.CardStates[i])
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

// continuousPolling runs continuous InAutoPoll monitoring for a single reader
func (m *Monitoring) continuousPolling(
	ctx context.Context, device *pn532.Device, readerPath string, state *CardState, isQuick bool,
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		detectedTag, err := m.performSinglePoll(ctx, device)
		if err != nil {
			if errors.Is(err, ErrNoTagInPoll) {
				// No tag detected - pass nil to processPollingResults to handle card removal
				m.processPollingResults(device, nil, state, readerPath, isQuick)
			} else {
				m.handlePollingError(err, state, readerPath)
			}
			continue
		}

		m.processPollingResults(device, detectedTag, state, readerPath, isQuick)
		// No additional sleep needed - SimplePoll handles timing internally
	}
}

// performSinglePoll performs a single tag detection cycle using the sophisticated strategy system
func (m *Monitoring) performSinglePoll(ctx context.Context, device *pn532.Device) (*pn532.DetectedTag, error) {
	// Use simplified polling with configurable interval from config
	detectedTag, err := device.SimplePoll(ctx, m.config.PollInterval)
	if err != nil {
		if errors.Is(err, pn532.ErrNoTagDetected) || errors.Is(err, context.DeadlineExceeded) {
			return nil, ErrNoTagInPoll // No tag detected, but not an error
		}
		return nil, fmt.Errorf("tag detection failed: %w", err)
	}
	return detectedTag, nil
}

// handlePollingError handles errors from InAutoPoll operations
func (m *Monitoring) handlePollingError(err error, state *CardState, readerPath string) {
	if errors.Is(err, context.DeadlineExceeded) {
		m.handleCardRemoval(state, readerPath)
		return
	}

	if errors.Is(err, context.Canceled) {
		return
	}

	// For other device errors, also handle card removal
	m.handleCardRemoval(state, readerPath)
}

// handleCardRemoval handles card removal state changes
func (m *Monitoring) handleCardRemoval(state *CardState, readerPath string) {
	if state.Present {
		m.output.Info("Card removed from %s", readerPath)
		m.resetCardState(state)
	}
}

// resetCardState resets the card state to empty
func (*Monitoring) resetCardState(state *CardState) {
	state.Present = false
	state.LastUID = ""
	state.LastType = ""
	state.TestedUID = ""
}

// processPollingResults processes the detected tag
func (m *Monitoring) processPollingResults(
	device *pn532.Device, detectedTag *pn532.DetectedTag, state *CardState, readerPath string, isQuick bool,
) {
	if detectedTag == nil {
		m.handleCardRemoval(state, readerPath)
		return
	}

	cardChanged := m.updateCardState(state, detectedTag, readerPath)

	if cardChanged || m.shouldTestCard(state, detectedTag.UID) {
		m.testAndRecordCard(device, detectedTag, state, isQuick)
	}
}

// updateCardState updates the card state and returns whether the card changed
func (m *Monitoring) updateCardState(state *CardState, detectedTag *pn532.DetectedTag, readerPath string) bool {
	currentUID := detectedTag.UID
	cardType := string(detectedTag.Type)

	if !state.Present {
		m.output.NewCardDetected(readerPath, cardType, currentUID)
		state.Present = true
		state.LastUID = currentUID
		state.LastType = cardType
		state.TestedUID = ""
		return true
	}

	if state.LastUID != currentUID {
		m.output.DifferentCardDetected(readerPath, cardType, currentUID)
		state.LastUID = currentUID
		state.LastType = cardType
		state.TestedUID = ""
		return true
	}

	return false
}

// shouldTestCard determines if we should test the card
func (*Monitoring) shouldTestCard(state *CardState, currentUID string) bool {
	return state.TestedUID != currentUID
}

// testAndRecordCard tests the card and records the result
func (m *Monitoring) testAndRecordCard(
	device *pn532.Device, detectedTag *pn532.DetectedTag, state *CardState, isQuick bool,
) {
	if err := m.testing.TestCard(device, detectedTag, TestMode{Quick: isQuick}); err != nil {
		m.output.Error("Card test failed: %v", err)
	} else {
		m.output.OK("Card test completed")
	}
	state.TestedUID = detectedTag.UID
}

// CheckCardsQuick performs a quick check for cards on all readers
func (m *Monitoring) CheckCardsQuick(readers []detection.DeviceInfo) {
	for _, reader := range readers {
		transport, err := m.discovery.CreateTransport(reader)
		if err != nil {
			continue
		}

		device, err := pn532.New(transport, pn532.WithTimeout(1*time.Second))
		if err != nil {
			_ = transport.Close()
			continue
		}

		// Initialize device (SAM configuration, etc.)
		if initErr := device.Init(); initErr != nil {
			_ = device.Close()
			_ = transport.Close()
			continue
		}

		tags, err := device.DetectTags(1, 0x00)
		if err == nil && len(tags) > 0 {
			_, _ = fmt.Printf("CARD: Card on %s: %s (UID: %s)\n",
				reader.Path, tags[0].Type, tags[0].UID)
		}

		_ = device.Close()
		_ = transport.Close()
	}
}
