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

package uart

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/ZaparooProject/go-pn532/transport/uart"
)

// detector implements the Detector interface for UART devices.
type detector struct{}

// New creates a new UART detector
func New() detection.Detector {
	return &detector{}
}

// init registers the detector on package import
func init() {
	detection.RegisterDetector(New())
}

// Transport returns the transport type
func (*detector) Transport() string {
	return "uart"
}

// Detect searches for PN532 devices on serial ports
func (d *detector) Detect(ctx context.Context, opts *detection.Options) ([]detection.DeviceInfo, error) {
	ports, err := d.enumeratePorts(ctx)
	if err != nil {
		return nil, err
	}

	filteredPorts := d.filterPorts(ports, opts)
	devices := d.processPortsToDevices(ctx, filteredPorts, opts)

	if len(devices) == 0 {
		return nil, detection.ErrNoDevicesFound
	}

	return devices, nil
}

// enumeratePorts gets the list of available serial ports
func (*detector) enumeratePorts(ctx context.Context) ([]serialPort, error) {
	ports, err := getSerialPorts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate serial ports: %w", err)
	}

	if len(ports) == 0 {
		return nil, detection.ErrNoDevicesFound
	}

	return ports, nil
}

// filterPorts removes blocked devices from the port list
func (d *detector) filterPorts(ports []serialPort, opts *detection.Options) []serialPort {
	var filtered []serialPort
	for _, port := range ports {
		// Skip blocked devices (existing functionality)
		if port.VIDPID != "" && detection.IsBlocked(port.VIDPID, opts.Blocklist) {
			continue
		}

		// Skip explicitly ignored device paths
		if detection.IsPathIgnored(port.Path, opts.IgnorePaths) {
			continue
		}

		// Copy the loop variable to avoid memory aliasing
		portCopy := port
		// Apply platform-specific positive filtering
		if d.shouldIncludePort(&portCopy) {
			filtered = append(filtered, port)
		}
	}
	return filtered
}

// shouldIncludePort determines if a port should be included based on platform-specific filtering
func (d *detector) shouldIncludePort(port *serialPort) bool {
	// Apply platform-specific positive filtering patterns
	if d.matchesGoodPatterns(port) {
		return true
	}

	// If no positive patterns match, fall back to existing isLikelyPN532 logic
	return isLikelyPN532(port)
}

// matchesGoodPatterns checks if the port matches known good device patterns
func (*detector) matchesGoodPatterns(port *serialPort) bool {
	// Known good device patterns for macOS (and other platforms)
	goodPatterns := []string{
		"usbserial",      // FTDI and similar USB-serial adapters
		"SLAB_USBtoUART", // Silicon Labs CP210x
		"usbmodem",       // Arduino and similar devices
	}

	// Known manufacturers for PN532-compatible devices
	goodManufacturers := []string{
		"FTDI", "Silicon Labs", "Prolific", "Arduino", "Future Technology Devices International",
	}

	// Check device name patterns
	lowerName := strings.ToLower(port.Name)
	lowerPath := strings.ToLower(port.Path)

	for _, pattern := range goodPatterns {
		if strings.Contains(lowerName, strings.ToLower(pattern)) ||
			strings.Contains(lowerPath, strings.ToLower(pattern)) {
			return true
		}
	}

	// Check manufacturer strings
	lowerManuf := strings.ToLower(port.Manufacturer)
	for _, manufacturer := range goodManufacturers {
		if strings.Contains(lowerManuf, strings.ToLower(manufacturer)) {
			return true
		}
	}

	return false
}

// processPortsToDevices converts ports to device infos with probing
func (d *detector) processPortsToDevices(ctx context.Context, ports []serialPort,
	opts *detection.Options,
) []detection.DeviceInfo {
	var devices []detection.DeviceInfo

	for i := range ports {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return devices
		default:
		}

		device, shouldInclude := d.processPort(ctx, &ports[i], opts)
		if shouldInclude {
			devices = append(devices, device)
		}
	}

	return devices
}

// processPort handles a single port's detection logic
func (d *detector) processPort(ctx context.Context, port *serialPort,
	opts *detection.Options,
) (detection.DeviceInfo, bool) {
	confidence, shouldProbe := d.determinePortHandling(port, opts.Mode)

	// Skip port entirely if passive mode and not likely PN532
	if opts.Mode == detection.Passive && confidence == 0 {
		return detection.DeviceInfo{}, false
	}

	device := d.createDeviceInfo(port, confidence)

	if shouldProbe {
		probeSuccess := d.probePortWithTimeout(ctx, port.Path, opts.Mode)
		if probeSuccess {
			device.Confidence = detection.High
		} else if opts.Mode == detection.Safe && !isLikelyPN532(port) {
			// In safe mode, skip unlikely devices that don't respond
			return detection.DeviceInfo{}, false
		}
	}

	return device, true
}

// determinePortHandling decides confidence level and whether to probe based on mode
func (*detector) determinePortHandling(port *serialPort, mode detection.Mode) (detection.Confidence, bool) {
	switch mode {
	case detection.Passive:
		if isLikelyPN532(port) {
			return detection.Medium, false
		}
		return 0, false // Signal to skip this port

	case detection.Safe:
		if isLikelyPN532(port) {
			return detection.Medium, true
		}
		return detection.Low, true

	case detection.Full:
		return detection.Low, true

	default:
		return detection.Low, false
	}
}

// createDeviceInfo builds a DeviceInfo struct from port data
func (d *detector) createDeviceInfo(port *serialPort, confidence detection.Confidence) detection.DeviceInfo {
	device := detection.DeviceInfo{
		Transport:  "uart",
		Path:       port.Path,
		Name:       port.Name,
		Confidence: confidence,
		Metadata:   make(map[string]string),
	}

	d.addPortMetadata(&device, port)
	return device
}

// addPortMetadata adds available port metadata to the device
func (*detector) addPortMetadata(device *detection.DeviceInfo, port *serialPort) {
	if port.VIDPID != "" {
		device.Metadata["vidpid"] = port.VIDPID
	}
	if port.Manufacturer != "" {
		device.Metadata["manufacturer"] = port.Manufacturer
	}
	if port.Product != "" {
		device.Metadata["product"] = port.Product
	}
	if port.SerialNumber != "" {
		device.Metadata["serial"] = port.SerialNumber
	}
}

// probePortWithTimeout performs device probing with timeout
func (*detector) probePortWithTimeout(ctx context.Context, path string, mode detection.Mode) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	return probeDevice(probeCtx, path, mode)
}

// serialPort represents a serial port with metadata
type serialPort struct {
	Path         string
	Name         string
	VIDPID       string
	Manufacturer string
	Product      string
	SerialNumber string
}

// isLikelyPN532 checks if a serial port is likely to be a PN532 device
func isLikelyPN532(port *serialPort) bool {
	// Check known PN532 VID:PIDs
	knownPN532 := []string{
		"067B:2303", // Prolific PL2303 (common in PN532 boards)
		"0403:6001", // FTDI FT232 (common in PN532 boards)
		"10C4:EA60", // Silicon Labs CP210x (common in PN532 boards)
		"1A86:7523", // QinHeng CH340 (common in PN532 boards)
	}

	upperVIDPID := strings.ToUpper(port.VIDPID)
	for _, known := range knownPN532 {
		if upperVIDPID == known {
			return true
		}
	}

	// Check product/manufacturer strings
	lowerProduct := strings.ToLower(port.Product)
	lowerManuf := strings.ToLower(port.Manufacturer)

	pn532Keywords := []string{"pn532", "nfc", "rfid", "13.56"}
	for _, keyword := range pn532Keywords {
		if strings.Contains(lowerProduct, keyword) || strings.Contains(lowerManuf, keyword) {
			return true
		}
	}

	return false
}

// probeDevice attempts to communicate with a device to verify it's a PN532
func probeDevice(_ context.Context, path string, mode detection.Mode) bool {
	// Try to open the port
	transport, err := uart.New(path)
	if err != nil {
		return false
	}
	defer func() { _ = transport.Close() }()

	// Create a PN532 device
	device, err := pn532.New(transport)
	if err != nil {
		return false
	}

	switch mode {
	case detection.Passive:
		// Passive mode doesn't probe
		return false

	case detection.Safe:
		// Just try to get firmware version
		_, err := device.GetFirmwareVersion()
		return err == nil

	case detection.Full:
		// Try full initialization (SAM configuration)
		err := device.Init()
		return err == nil

	default:
		return false
	}
}
