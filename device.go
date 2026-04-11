// Copyright 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pn532

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	// MaxFastReadPages limits the number of pages in a single FastRead operation
	// Set to 0 to use platform-specific defaults (16 pages on Windows UART, unlimited elsewhere)
	// This helps avoid PN532 firmware lockups with large InCommunicateThru payloads
	MaxFastReadPages int
}

// DefaultDeviceConfig returns default device configuration
func DefaultDeviceConfig() *DeviceConfig {
	return &DeviceConfig{
		RetryConfig:      DefaultRetryConfig(),
		Timeout:          1 * time.Second,
		MaxFastReadPages: 0, // Use platform-specific defaults
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
}

// hasCapability checks if the transport has the specified capability
func (d *Device) hasCapability(capability TransportCapability) bool {
	if checker, ok := d.transport.(TransportCapabilityChecker); ok {
		return checker.HasCapability(capability)
	}
	return false
}

// Option is a functional option for configuring a Device
type Option func(*Device) error

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
	deviceDetector         func(context.Context, *detection.Options) ([]detection.DeviceInfo, error)
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
func WithDeviceDetector(
	detector func(context.Context, *detection.Options) ([]detection.DeviceInfo, error),
) ConnectOption {
	return func(c *connectConfig) error {
		c.deviceDetector = detector
		return nil
	}
}

func applyConnectOptions(opts []ConnectOption) (*connectConfig, error) {
	config := &connectConfig{
		autoDetect:             false,
		deviceOptions:          nil,
		timeout:                30 * time.Second,
		transportFactory:       nil,
		transportDeviceFactory: nil,
		connectionRetries:      DefaultConnectionRetries,
	}

	for _, opt := range opts {
		if err := opt(config); err != nil {
			return nil, fmt.Errorf("failed to apply connect option: %w", err)
		}
	}

	return config, nil
}

func setupDevice(ctx context.Context, transport Transport, config *connectConfig) (*Device, error) {
	device, err := New(transport, config.deviceOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to create device: %w", err)
	}

	if config.timeout > 0 {
		if err := device.SetTimeout(config.timeout); err != nil {
			return nil, fmt.Errorf("failed to set timeout: %w", err)
		}
	}

	if err := device.Init(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize device: %w", err)
	}

	return device, nil
}

// setupDeviceWithRetry wraps setupDevice with retry logic for manual
// connections. Auto-detection does not use this path — it tries each detected
// candidate exactly once inside connectAutoDetected so a wedged transport
// can't burn a retry budget on top of iteration.
func setupDeviceWithRetry(ctx context.Context, transport Transport, config *connectConfig) (*Device, error) {
	retryConfig := &RetryConfig{
		MaxAttempts:       config.connectionRetries,
		InitialBackoff:    ConnectionInitialBackoff,
		MaxBackoff:        ConnectionMaxBackoff,
		BackoffMultiplier: ConnectionBackoffMultiplier,
		Jitter:            ConnectionJitter,
		RetryTimeout:      ConnectionRetryTimeout,
	}

	var device *Device
	err := RetryWithConfig(ctx, retryConfig, func() error {
		var err error
		device, err = setupDevice(ctx, transport, config)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to setup device after %d attempts: %w", config.connectionRetries, err)
	}
	if device == nil {
		return nil, errors.New("device setup succeeded but device is nil")
	}

	return device, nil
}

// ConnectDevice creates and initializes a PN532 device from a path or via
// auto-detection. It handles transport creation, device initialization, and
// optional validation setup.
//
// The pn532 package does not bundle a default transport — callers must wire up
// whichever transports they want to link (uart, i2c, spi, or a custom one) and
// pass the corresponding factory option. This keeps consumers that only need
// one transport from pulling in the others (and their dependencies).
//
// For an explicit device path, pass [WithTransportFactory] with a function that
// returns the right [Transport] for the given path string:
//
//	import (
//	    pn532 "github.com/ZaparooProject/go-pn532"
//	    "github.com/ZaparooProject/go-pn532/transport/uart"
//	)
//
//	newTransport := func(path string) (pn532.Transport, error) {
//	    return uart.New(path)
//	}
//	device, err := pn532.ConnectDevice(ctx, "/dev/ttyUSB0",
//	    pn532.WithTransportFactory(newTransport))
//
// For auto-detection, register the detectors you care about via blank import
// and pass [WithAutoDetection] together with [WithTransportFromDeviceFactory]:
//
//	import (
//	    pn532 "github.com/ZaparooProject/go-pn532"
//	    "github.com/ZaparooProject/go-pn532/detection"
//	    _ "github.com/ZaparooProject/go-pn532/detection/uart"
//	    "github.com/ZaparooProject/go-pn532/transport/uart"
//	)
//
//	newFromDevice := func(info detection.DeviceInfo) (pn532.Transport, error) {
//	    return uart.New(info.Path)
//	}
//	device, err := pn532.ConnectDevice(ctx, "",
//	    pn532.WithAutoDetection(),
//	    pn532.WithTransportFromDeviceFactory(newFromDevice))
//
// See cmd/reader/main.go for a complete example that supports all three
// transports.
func ConnectDevice(ctx context.Context, path string, opts ...ConnectOption) (*Device, error) {
	config, err := applyConnectOptions(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to apply connect options: %w", err)
	}

	if config.autoDetect || path == "" {
		return connectAutoDetected(ctx, config)
	}
	return connectManual(ctx, path, config)
}

// connectManual builds a transport for an explicit path and runs the retry
// loop against it. Used when the caller knows exactly which device to open.
func connectManual(ctx context.Context, path string, config *connectConfig) (*Device, error) {
	if config.transportFactory == nil {
		return nil, errors.New("transport factory not provided")
	}

	transport, err := config.transportFactory(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create transport for path %s: %w", path, err)
	}

	device, err := setupDeviceWithRetry(ctx, transport, config)
	if err != nil {
		_ = transport.Close()
		return nil, err
	}
	return device, nil
}

// connectAutoDetected runs auto-detection and tries each returned candidate
// in turn, returning the first one that initialises successfully. Detection
// handing back a Low-confidence candidate (e.g. an I2C bus on a host where
// no PN532 is actually wired up) must not kill the whole connection attempt
// — the next candidate gets a chance, and only if every candidate fails do
// we return a single aggregated error explaining which transports were
// tried and why each failed.
//
// Per-candidate failures are written to the session log via Debugf so they
// show up when something unexpected happens, but they do not propagate to
// the caller as long as another candidate succeeds.
func connectAutoDetected(ctx context.Context, config *connectConfig) (*Device, error) {
	if config.transportDeviceFactory == nil {
		return nil, errors.New("transport device factory not provided")
	}

	devices, err := runAutoDetection(ctx, config)
	if err != nil {
		return nil, err
	}

	// Prefer quiet transports first. UART and SPI open cleanly on desktop
	// Linux; periph.io's host.Init() (called on first I2C/SPI transport
	// construction) loudly logs a /dev/gpiochip0 permission warning via
	// gpioioctl whenever the user isn't in the gpio group. If UART is
	// present we never want to touch that code path, so we try UART
	// candidates before I2C/SPI. This is a transport-kind preference, not
	// a confidence sort: any working candidate will still win.
	sortCandidatesByTransportPreference(devices)

	var candidateErrs []error
	for i := range devices {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		candidate := devices[i]
		device, tryErr := tryAutoDetectedCandidate(ctx, candidate, config)
		if tryErr == nil {
			return device, nil
		}

		Debugf("auto-detect: skipping %s: %v", candidate.String(), tryErr)
		candidateErrs = append(candidateErrs, fmt.Errorf("%s: %w", candidate.String(), tryErr))
	}

	return nil, fmt.Errorf("all %d auto-detected candidate(s) failed to initialise: %w",
		len(devices), errors.Join(candidateErrs...))
}

// transportPreferenceRank returns the preference rank of a detection
// Transport string, used to order auto-detect candidates. Lower is tried
// first. UART comes before SPI comes before I2C so that, when a UART PN532
// is present, we never construct an I2C/SPI transport and therefore never
// trigger periph.io's host.Init() (which loudly logs a gpiochip
// permission-denied warning on hosts where the user isn't in the gpio
// group). Unknown transports sort last.
//
// Matching is case-insensitive to tolerate detector implementations that
// use different casing conventions for their Transport field.
func transportPreferenceRank(transport string) int {
	switch strings.ToLower(transport) {
	case "uart":
		return 0
	case "spi":
		return 1
	case "i2c":
		return 2
	default:
		return 3
	}
}

// sortCandidatesByTransportPreference reorders the detected devices in
// place so UART candidates are tried before SPI, and SPI before I2C. This
// is a transport-kind preference only — it does not re-rank devices of the
// same transport among themselves (a stable sort preserves the order the
// detectors returned them in).
func sortCandidatesByTransportPreference(devices []detection.DeviceInfo) {
	sort.SliceStable(devices, func(i, j int) bool {
		return transportPreferenceRank(devices[i].Transport) < transportPreferenceRank(devices[j].Transport)
	})
}

// runAutoDetection invokes whichever detector the caller configured (custom
// or the default registry-based DetectAll) and normalises the "no devices"
// outcome so connectAutoDetected's caller always sees a clear error rather
// than an empty slice.
func runAutoDetection(ctx context.Context, config *connectConfig) ([]detection.DeviceInfo, error) {
	opts := detection.DefaultOptions()
	opts.Mode = detection.Safe

	var (
		devices []detection.DeviceInfo
		err     error
	)
	if config.deviceDetector != nil {
		devices, err = config.deviceDetector(ctx, &opts)
	} else {
		devices, err = detection.DetectAll(ctx, &opts)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to detect devices: %w", err)
	}
	if len(devices) == 0 {
		return nil, errors.New("no PN532 devices found")
	}
	return devices, nil
}

// tryAutoDetectedCandidate builds a transport for a single detected device
// and initialises a Device on it. On any failure the transport is closed
// before returning so failed candidates leak neither file descriptors nor
// bus locks.
func tryAutoDetectedCandidate(
	ctx context.Context, candidate detection.DeviceInfo, config *connectConfig,
) (*Device, error) {
	transport, err := config.transportDeviceFactory(candidate)
	if err != nil {
		return nil, fmt.Errorf("transport creation: %w", err)
	}

	device, err := setupDevice(ctx, transport, config)
	if err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("device init: %w", err)
	}
	return device, nil
}

// Transport returns the underlying transport
func (d *Device) Transport() Transport {
	return d.transport
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
}

// IsAutoPollSupported returns true if the transport supports native InAutoPoll
func (d *Device) IsAutoPollSupported() bool {
	return d.hasCapability(CapabilityAutoPollNative)
}

// SetPassiveActivationRetries configures the maximum number of retries for passive activation
// to prevent infinite waiting that can cause the PN532 to lock up. A finite number like 10 (0x0A)
// is recommended instead of 0xFF (infinite) to avoid stuck states requiring power cycling.
func (d *Device) SetPassiveActivationRetries(ctx context.Context, maxRetries byte) error {
	// RF Configuration item 0x05 - MaxRetries
	// Payload: [MxRtyATR, MxRtyPSL, MxRtyPassiveActivation]
	configPayload := []byte{
		0x05,       // CfgItem: MaxRetries
		0x00,       // MxRtyATR (use default)
		0x00,       // MxRtyPSL (use default)
		maxRetries, // MxRtyPassiveActivation
	}

	_, err := d.transport.SendCommand(ctx, cmdRFConfiguration, configPayload)
	if err != nil {
		return fmt.Errorf("failed to set passive activation retries: %w", err)
	}

	return nil
}

// SetPollingRetries configures the MxRtyATR parameter for passive target detection retries.
// This controls how many times the PN532 will retry detecting a passive target before giving up.
// Each retry is approximately 150ms according to the PN532 datasheet.
//
// Parameters:
//   - mxRtyATR: Number of retries (0x00 = immediate, 0x01-0xFE = retry count, 0xFF = infinite)
//
// Common values:
//   - 0x00: Immediate return (no retries)
//   - 0x10: ~2.4 seconds (16 retries)
//   - 0x20: ~4.8 seconds (32 retries)
//   - 0xFF: Infinite retries (use with caution)
func (d *Device) SetPollingRetries(ctx context.Context, mxRtyATR byte) error {
	// RF Configuration item 0x05 - MaxRetries
	// Payload: [MxRtyATR, MxRtyPSL, MxRtyPassiveActivation]
	configPayload := []byte{
		0x05,     // CfgItem: MaxRetries
		mxRtyATR, // MxRtyATR (retry count for passive target detection)
		0x01,     // MxRtyPSL (default)
		0xFF,     // MxRtyPassiveActivation (infinite)
	}

	_, err := d.transport.SendCommand(ctx, cmdRFConfiguration, configPayload)
	if err != nil {
		return fmt.Errorf("failed to set polling retries: %w", err)
	}

	return nil
}

// CycleRFField turns the RF field off and back on to reset PN532 state.
// This clears stuck conditions after authentication failures.
// Timing: 50ms off, 50ms on, 50ms settle (per docs/hardware-mifare-clone-research.md)
func (d *Device) CycleRFField(ctx context.Context) error {
	// Turn RF field OFF
	_, err := d.transport.SendCommand(ctx, cmdRFConfiguration, []byte{0x01, 0x00})
	if err != nil {
		return fmt.Errorf("failed to turn RF field off: %w", err)
	}

	select {
	case <-time.After(50 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	// Turn RF field ON
	_, err = d.transport.SendCommand(ctx, cmdRFConfiguration, []byte{0x01, 0x01})
	if err != nil {
		return fmt.Errorf("failed to turn RF field on: %w", err)
	}

	select {
	case <-time.After(50 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	// Settle time
	select {
	case <-time.After(50 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
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
