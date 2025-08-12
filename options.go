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

// Option is a functional option for configuring a Device
type Option func(*Device) error

// WithRetryConfig sets the retry configuration for the device
func WithRetryConfig(config *RetryConfig) Option {
	return func(d *Device) error {
		d.SetRetryConfig(config)
		return nil
	}
}

// WithTimeout sets the default timeout for device operations
func WithTimeout(timeout time.Duration) Option {
	return func(d *Device) error {
		return d.SetTimeout(timeout)
	}
}

// WithMaxRetries sets the maximum number of retries for device operations
func WithMaxRetries(maxAttempts int) Option {
	return func(device *Device) error {
		if device.config.RetryConfig == nil {
			device.config.RetryConfig = DefaultRetryConfig()
		}
		device.config.RetryConfig.MaxAttempts = maxAttempts
		if tr, ok := device.transport.(*TransportWithRetry); ok {
			tr.SetRetryConfig(device.config.RetryConfig)
		}
		return nil
	}
}

// WithRetryBackoff sets the initial backoff duration for retries
func WithRetryBackoff(initialBackoff time.Duration) Option {
	return func(device *Device) error {
		if device.config.RetryConfig == nil {
			device.config.RetryConfig = DefaultRetryConfig()
		}
		device.config.RetryConfig.InitialBackoff = initialBackoff
		if tr, ok := device.transport.(*TransportWithRetry); ok {
			tr.SetRetryConfig(device.config.RetryConfig)
		}
		return nil
	}
}

// NewWithOptions creates a new PN532 device with the given transport and options
func NewWithOptions(transport Transport, opts ...Option) (*Device, error) {
	device := &Device{
		transport: transport,
		config:    DefaultDeviceConfig(),
	}

	// Apply all options
	for _, opt := range opts {
		if err := opt(device); err != nil {
			return nil, err
		}
	}

	return device, nil
}
