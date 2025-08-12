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

package pn532

import (
	"time"
)

// TransportOptimizer provides transport-specific optimizations
type TransportOptimizer interface {
	// OptimizeForPolling returns transport-specific polling parameters
	OptimizeForPolling(strategy PollStrategy) *OptimizedPollParams

	// GetPreferredStrategy returns the preferred strategy for this transport
	GetPreferredStrategy() PollStrategy

	// GetStabilizationDelay returns the required stabilization delay
	GetStabilizationDelay() time.Duration
}

// OptimizedPollParams contains transport-optimized polling parameters
type OptimizedPollParams struct {
	TargetTypes        []AutoPollTarget
	StabilizationDelay time.Duration
	RetryDelay         time.Duration
	MaxRetries         int
	PollPeriod         byte
}

// getOptimizedPollParams returns optimized polling parameters for the current transport
func (d *Device) getOptimizedPollParams(strategy PollStrategy) *OptimizedPollParams {
	// Check if transport provides its own optimization
	if optimizer, ok := d.transport.(TransportOptimizer); ok {
		return optimizer.OptimizeForPolling(strategy)
	}

	// Default optimization based on transport type
	switch d.transport.Type() {
	case TransportUART:
		return d.getUARTOptimizedParams(strategy)

	case TransportI2C:
		return d.getI2COptimizedParams(strategy)

	case TransportSPI:
		return d.getSPIOptimizedParams(strategy)

	case TransportMock:
		// Mock transport uses default optimized parameters for testing
		return d.getDefaultOptimizedParams(strategy)

	default:
		return d.getDefaultOptimizedParams(strategy)
	}
}

// getUARTOptimizedParams returns UART-optimized polling parameters
func (*Device) getUARTOptimizedParams(strategy PollStrategy) *OptimizedPollParams {
	params := &OptimizedPollParams{
		TargetTypes:        []AutoPollTarget{AutoPollGeneric106kbps, AutoPollMifare},
		StabilizationDelay: 10 * time.Millisecond,
		RetryDelay:         100 * time.Millisecond,
		MaxRetries:         3,
	}

	switch strategy {
	case PollStrategyAutoPoll:
		params.PollPeriod = 2 // 300ms - balanced for UART
	case PollStrategyLegacy:
		params.PollPeriod = 1 // Not used for legacy, but set for consistency
	case PollStrategyAuto, PollStrategyManual:
		params.PollPeriod = 3 // 450ms - conservative
	default:
		params.PollPeriod = 3 // 450ms - conservative
	}

	return params
}

// getI2COptimizedParams returns I2C-optimized polling parameters
func (*Device) getI2COptimizedParams(strategy PollStrategy) *OptimizedPollParams {
	params := &OptimizedPollParams{
		TargetTypes:        []AutoPollTarget{AutoPollGeneric106kbps, AutoPollMifare, AutoPollFeliCa212},
		StabilizationDelay: 5 * time.Millisecond, // I2C is typically faster
		RetryDelay:         50 * time.Millisecond,
		MaxRetries:         5, // I2C can handle more retries
	}

	switch strategy {
	case PollStrategyAutoPoll:
		params.PollPeriod = 1 // 150ms - I2C can handle faster polling
	case PollStrategyAuto, PollStrategyLegacy, PollStrategyManual:
		params.PollPeriod = 2 // 300ms
	default:
		params.PollPeriod = 2 // 300ms
	}

	return params
}

// getSPIOptimizedParams returns SPI-optimized polling parameters
func (*Device) getSPIOptimizedParams(strategy PollStrategy) *OptimizedPollParams {
	params := &OptimizedPollParams{
		TargetTypes: []AutoPollTarget{
			AutoPollGeneric106kbps, AutoPollMifare, AutoPollFeliCa212, AutoPollFeliCa424,
		},
		StabilizationDelay: 5 * time.Millisecond, // SPI is fast
		RetryDelay:         25 * time.Millisecond,
		MaxRetries:         5, // SPI can handle more retries
	}

	switch strategy {
	case PollStrategyAutoPoll:
		params.PollPeriod = 1 // 150ms - SPI can handle fastest polling
	case PollStrategyAuto, PollStrategyLegacy, PollStrategyManual:
		params.PollPeriod = 2 // 300ms
	default:
		params.PollPeriod = 2 // 300ms
	}

	return params
}

// getDefaultOptimizedParams returns default polling parameters
func (*Device) getDefaultOptimizedParams(PollStrategy) *OptimizedPollParams {
	return &OptimizedPollParams{
		PollPeriod:         3, // 450ms - conservative default
		TargetTypes:        []AutoPollTarget{AutoPollGeneric106kbps},
		StabilizationDelay: 50 * time.Millisecond,
		RetryDelay:         500 * time.Millisecond,
		MaxRetries:         3,
	}
}
