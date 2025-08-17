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

package polling

import (
	"context"

	"github.com/ZaparooProject/go-pn532"
)

// startScanning runs the main polling loop using the underlying Monitor
func (s *Scanner) startScanning(ctx context.Context) error {
	// Create underlying monitor with enhanced configuration
	monitorConfig := &Config{
		PollInterval:       s.config.PollInterval,
		CardRemovalTimeout: s.config.CardRemovalTimeout,
	}

	s.monitor = NewMonitor(s.device, monitorConfig)
	defer func() {
		if s.monitor != nil {
			_ = s.monitor.Close() // Ignore error in defer cleanup
		}
	}()

	// Set up event handlers to integrate with Scanner callbacks
	s.setupEventHandlers()

	// Start the underlying monitor
	return s.monitor.Start(ctx)
}

// setupEventHandlers configures the monitor callbacks to integrate with Scanner functionality
func (s *Scanner) setupEventHandlers() {
	s.monitor.OnCardDetected = func(detectedTag *pn532.DetectedTag) error {
		// Process any pending writes first - this is the key coordination point
		s.processPendingWrites(detectedTag)

		// Then call user's callback if provided
		if s.OnTagDetected != nil {
			return s.OnTagDetected(detectedTag)
		}
		return nil
	}

	s.monitor.OnCardRemoved = func() {
		// Call user's callback if provided
		if s.OnTagRemoved != nil {
			s.OnTagRemoved()
		}
	}

	s.monitor.OnCardChanged = func(detectedTag *pn532.DetectedTag) error {
		// Process any pending writes for the new tag
		s.processPendingWrites(detectedTag)

		// Then call user's callback if provided
		if s.OnTagChanged != nil {
			return s.OnTagChanged(detectedTag)
		}
		return nil
	}
}
