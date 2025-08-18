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
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/go-pn532/detection"
)

// Device errors
var (
	ErrNoTagDetected  = errors.New("no tag detected")
	ErrTimeout        = errors.New("operation timeout")
	ErrInvalidTag     = errors.New("invalid tag type")
	ErrNotImplemented = errors.New("not implemented")
)

// DeviceConfig contains configuration options for the Device
type DeviceConfig struct {
	// RetryConfig configures retry behavior for transport operations
	RetryConfig *RetryConfig
	// Timeout is the default timeout for operations
	Timeout time.Duration
}

// DefaultDeviceConfig returns default device configuration
func DefaultDeviceConfig() *DeviceConfig {
	return &DeviceConfig{
		RetryConfig: DefaultRetryConfig(),
		Timeout:     1 * time.Second,
	}
}

// Device represents a PN532 NFC reader device
//
// Thread Safety: Device is NOT thread-safe. All methods must be called from
// a single goroutine or protected with external synchronization. The underlying
// transport may have its own concurrency limitations. For concurrent access,
// wrap the Device with a mutex or use separate Device instances with separate
// transports.
type Device struct {
	transport       Transport
	config          *DeviceConfig
	firmwareVersion *FirmwareVersion
	currentTarget   byte
}

// setCurrentTarget sets the active target number for data exchange operations
func (d *Device) setCurrentTarget(targetNumber byte) {
	d.currentTarget = targetNumber
}

// hasCapability checks if the transport has the specified capability
func (d *Device) hasCapability(capability TransportCapability) bool {
	if checker, ok := d.transport.(TransportCapabilityChecker); ok {
		return checker.HasCapability(capability)
	}
	return false
}

// selectTarget performs explicit target selection using InSelect command
func (d *Device) selectTarget(targetNumber byte) error {
	// Send InSelect command to explicitly select the target
	resp, err := d.transport.SendCommand(cmdInSelect, []byte{targetNumber})
	if err != nil {
		// Check if this is the specific clone device empty response issue
		if strings.Contains(err.Error(), "clone device returned empty response") {
			debugln("InSelect failed due to clone device compatibility issue, falling back to direct target usage")
			// Fall back to direct target usage like non-InSelect devices
			d.setCurrentTarget(targetNumber)
			debugf("Using target %d directly without InSelect (clone device fallback)", targetNumber)
			return nil
		}
		return fmt.Errorf("InSelect failed: %w", err)
	}

	// Check response
	if len(resp) < 2 {
		return fmt.Errorf("InSelect response too short: %d bytes", len(resp))
	}

	// Response format: [response_cmd, status, ...]
	if resp[0] != cmdInSelect+1 {
		return fmt.Errorf("unexpected InSelect response command: %02X", resp[0])
	}

	if resp[1] != 0x00 {
		return fmt.Errorf("InSelect failed with status: %02X", resp[1])
	}

	// Set the current target after successful selection
	d.setCurrentTarget(targetNumber)
	debugf("InSelect successful for target %d", targetNumber)
	return nil
}

// getCurrentTarget returns the active target number (defaults to 1 if not set)
func (*Device) getCurrentTarget() byte {
	// LIBNFC COMPATIBILITY: Always use target number 1 for InDataExchange
	// libnfc research shows that InDataExchange always uses hardcoded target number 1:
	// abtCmd[0] = InDataExchange; abtCmd[1] = 1; /* target number */
	// This is regardless of what target number was returned by InListPassiveTarget
	return 1
}

// New creates a new PN532 device with the given transport
func New(transport Transport, opts ...Option) (*Device, error) {
	device := &Device{
		transport: transport,
		config:    DefaultDeviceConfig(),
	}

	// Apply options
	for _, opt := range opts {
		if err := opt(device); err != nil {
			return nil, err
		}
	}

	return device, nil
}

// TransportFactory is a function type for creating transports
type TransportFactory func(path string) (Transport, error)

// TransportFromDeviceFactory is a function type for creating transports from detected devices
type TransportFromDeviceFactory func(device detection.DeviceInfo) (Transport, error)

// ConnectOption represents a functional option for ConnectDevice
type ConnectOption func(*connectConfig) error

// connectConfig holds configuration options for device connection
type connectConfig struct {
	transportFactory       TransportFactory
	transportDeviceFactory TransportFromDeviceFactory
	deviceDetector         func(*detection.Options) ([]detection.DeviceInfo, error)
	deviceOptions          []Option
	timeout                time.Duration
	autoDetect             bool
	connectionRetries      int
}

// WithAutoDetection enables automatic device detection instead of using a specific path
func WithAutoDetection() ConnectOption {
	return func(c *connectConfig) error {
		c.autoDetect = true
		return nil
	}
}

// WithDeviceOptions adds device-level options
func WithDeviceOptions(opts ...Option) ConnectOption {
	return func(c *connectConfig) error {
		c.deviceOptions = append(c.deviceOptions, opts...)
		return nil
	}
}

// WithConnectTimeout sets the device connection timeout
func WithConnectTimeout(timeout time.Duration) ConnectOption {
	return func(c *connectConfig) error {
		c.timeout = timeout
		return nil
	}
}

// WithTransportFactory sets the transport factory function
func WithTransportFactory(factory TransportFactory) ConnectOption {
	return func(c *connectConfig) error {
		c.transportFactory = factory
		return nil
	}
}

// WithTransportFromDeviceFactory sets the transport from device factory function
func WithTransportFromDeviceFactory(factory TransportFromDeviceFactory) ConnectOption {
	return func(c *connectConfig) error {
		c.transportDeviceFactory = factory
		return nil
	}
}

// WithConnectionRetries sets the number of connection retry attempts
func WithConnectionRetries(maxAttempts int) ConnectOption {
	return func(c *connectConfig) error {
		if maxAttempts < 1 {
			return fmt.Errorf("connection retries must be at least 1, got %d", maxAttempts)
		}
		c.connectionRetries = maxAttempts
		return nil
	}
}

// WithDeviceDetector sets a custom device detector function for auto-detection
func WithDeviceDetector(detector func(*detection.Options) ([]detection.DeviceInfo, error)) ConnectOption {
	return func(c *connectConfig) error {
		c.deviceDetector = detector
		return nil
	}
}

// ConnectDevice creates and initializes a PN532 device from a path or auto-detection.
// This is a high-level convenience function that handles transport creation, device
// initialization, and optional validation setup.
//
// Example usage:
//
//	// Connect to specific device
//	device, err := pn532.ConnectDevice("/dev/ttyUSB0")
//
//
//	// Auto-detect device
//	device, err := pn532.ConnectDevice("", pn532.WithAutoDetection())
func applyConnectOptions(opts []ConnectOption) (*connectConfig, error) {
	config := &connectConfig{
		autoDetect:             false,
		deviceOptions:          nil,
		timeout:                30 * time.Second,
		transportFactory:       nil,
		transportDeviceFactory: nil,
		connectionRetries:      3, // Default to 3 attempts for manual connections
	}

	for _, opt := range opts {
		if err := opt(config); err != nil {
			return nil, fmt.Errorf("failed to apply connect option: %w", err)
		}
	}

	return config, nil
}

func createTransport(path string, config *connectConfig) (Transport, error) {
	if config.autoDetect || path == "" {
		return createAutoDetectedTransport(config.transportDeviceFactory, config.deviceDetector)
	}
	return createManualTransport(path, config.transportFactory)
}

func setupDevice(transport Transport, config *connectConfig) (*Device, error) {
	device, err := New(transport, config.deviceOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to create device: %w", err)
	}

	if config.timeout > 0 {
		if err := device.SetTimeout(config.timeout); err != nil {
			return nil, fmt.Errorf("failed to set timeout: %w", err)
		}
	}

	if err := device.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize device: %w", err)
	}

	return device, nil
}

// setupDeviceWithRetry wraps setupDevice with retry logic for connection attempts
func setupDeviceWithRetry(transport Transport, config *connectConfig) (*Device, error) {
	// Auto-detection should bypass retry logic (single attempt only)
	if config.autoDetect {
		return setupDevice(transport, config)
	}

	// Manual connections use retry logic
	retryConfig := &RetryConfig{
		MaxAttempts:       config.connectionRetries,
		InitialBackoff:    50 * time.Millisecond,
		MaxBackoff:        500 * time.Millisecond,
		BackoffMultiplier: 2.0,
		Jitter:            0.1,
		RetryTimeout:      10 * time.Second,
	}

	var device *Device
	err := RetryWithConfig(context.Background(), retryConfig, func() error {
		var err error
		device, err = setupDevice(transport, config)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to setup device after %d attempts: %w", config.connectionRetries, err)
	}

	return device, nil
}

func ConnectDevice(path string, opts ...ConnectOption) (*Device, error) {
	config, err := applyConnectOptions(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to apply connect options: %w", err)
	}

	transport, err := createTransport(path, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport: %w", err)
	}

	device, err := setupDeviceWithRetry(transport, config)
	if err != nil {
		_ = transport.Close()
		return nil, err
	}

	return device, nil
}

// createManualTransport handles creation of transport for a specific path
func createManualTransport(path string, factory TransportFactory) (Transport, error) {
	if factory == nil {
		return nil, errors.New("transport factory not provided")
	}

	transport, err := factory(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport for path %s: %w", path, err)
	}

	return transport, nil
}

// createAutoDetectedTransport handles auto-detection of devices
func createAutoDetectedTransport(
	factory TransportFromDeviceFactory,
	detector func(*detection.Options) ([]detection.DeviceInfo, error),
) (Transport, error) {
	opts := detection.DefaultOptions()
	opts.Mode = detection.Safe

	var devices []detection.DeviceInfo
	var err error

	if detector != nil {
		devices, err = detector(&opts)
	} else {
		devices, err = detection.DetectAll(&opts)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to detect devices: %w", err)
	}

	if len(devices) == 0 {
		return nil, errors.New("no PN532 devices found")
	}

	// Use the first detected device
	device := devices[0]
	if factory == nil {
		return nil, errors.New("transport device factory not provided")
	}
	return factory(device)
}

// Transport returns the underlying transport
func (d *Device) Transport() Transport {
	return d.transport
}

// Init initializes the PN532 device
func (d *Device) Init() error {
	return d.InitContext(context.Background())
}

// SetTimeout sets the default timeout for operations
func (d *Device) SetTimeout(timeout time.Duration) error {
	d.config.Timeout = timeout
	if err := d.transport.SetTimeout(timeout); err != nil {
		return fmt.Errorf("failed to set timeout on transport: %w", err)
	}
	return nil
}

// SetRetryConfig updates the retry configuration
func (d *Device) SetRetryConfig(config *RetryConfig) {
	d.config.RetryConfig = config
	if tr, ok := d.transport.(*TransportWithRetry); ok {
		tr.SetRetryConfig(config)
	}
}

// IsAutoPollSupported returns true if the transport supports native InAutoPoll
func (d *Device) IsAutoPollSupported() bool {
	return d.hasCapability(CapabilityAutoPollNative)
}

// Close closes the device connection
func (d *Device) Close() error {
	if d.transport != nil {
		if err := d.transport.Close(); err != nil {
			return fmt.Errorf("failed to close transport: %w", err)
		}
	}
	return nil
}
