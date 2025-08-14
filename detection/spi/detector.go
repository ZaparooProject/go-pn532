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

package spi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/ZaparooProject/go-pn532/transport/spi"
)

// Config represents SPI device configuration
type Config struct {
	// Additional metadata
	Metadata map[string]string `json:"metadata,omitempty"`
	// Device path (e.g., "/dev/spidev0.0")
	Device string `json:"device"`
	// Human-readable name
	Name string `json:"name,omitempty"`
	// Chip select pin (if using GPIO CS)
	CSPin int `json:"cs_pin,omitempty"`
}

// detector implements the Detector interface for SPI devices
type detector struct{}

// New creates a new SPI detector
func New() detection.Detector {
	return &detector{}
}

// init registers the detector on package import
func init() {
	detection.RegisterDetector(New())
}

// Transport returns the transport type
func (*detector) Transport() string {
	return "spi"
}

// gatherConfigs collects SPI configurations from all sources
func gatherConfigs() []Config {
	var configs []Config

	// 1. Load from config file
	if fileConfigs := loadConfigFile(); fileConfigs != nil {
		configs = append(configs, fileConfigs...)
	}

	// 2. Check environment variable
	if envConfig := loadEnvConfig(); envConfig != nil {
		configs = append(configs, *envConfig)
	}

	// 3. Platform-specific detection
	if platformConfigs := detectPlatformDevices(); platformConfigs != nil {
		configs = append(configs, platformConfigs...)
	}

	// 4. Common SPI device paths (Linux)
	if runtime.GOOS == "linux" {
		configs = append(configs, detectLinuxSPIDevices()...)
	}

	return deduplicateConfigs(configs)
}

// createDeviceInfo creates a DeviceInfo from a Config
func createDeviceInfo(config Config) detection.DeviceInfo {
	device := detection.DeviceInfo{
		Transport:  "spi",
		Path:       config.Device,
		Name:       config.Name,
		Confidence: detection.Low, // Start with low confidence
		Metadata:   make(map[string]string),
	}

	// Copy metadata
	for k, v := range config.Metadata {
		device.Metadata[k] = v
	}

	if config.CSPin > 0 {
		device.Metadata["cs_pin"] = fmt.Sprintf("%d", config.CSPin)
	}

	// If name not provided, generate one
	if device.Name == "" {
		device.Name = fmt.Sprintf("SPI device at %s", config.Device)
	}

	return device
}

// probeAndUpdateDevice probes a device and updates its confidence if successful
func probeAndUpdateDevice(
	ctx context.Context,
	config Config,
	device *detection.DeviceInfo,
	opts *detection.Options,
) bool {
	if opts.Mode == detection.Passive {
		return true
	}

	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	confirmed := probeSPIDevice(probeCtx, config, opts.Mode)
	if confirmed {
		device.Confidence = detection.High
		return true
	}

	return false
}

// Detect searches for PN532 devices on SPI buses
func (*detector) Detect(ctx context.Context, opts *detection.Options) ([]detection.DeviceInfo, error) {
	configs := gatherConfigs()
	if len(configs) == 0 {
		return nil, detection.ErrNoDevicesFound
	}

	var devices []detection.DeviceInfo

	// Test each configured device
	for _, config := range configs {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return devices, detection.ErrDetectionTimeout
		default:
		}

		// Skip explicitly ignored device paths
		if detection.IsPathIgnored(config.Device, opts.IgnorePaths) {
			continue
		}

		device := createDeviceInfo(config)

		if probeAndUpdateDevice(ctx, config, &device, opts) {
			devices = append(devices, device)
		}
	}

	if len(devices) == 0 {
		return nil, detection.ErrNoDevicesFound
	}

	return devices, nil
}

// loadConfigFile loads SPI configurations from a JSON file
func loadConfigFile() []Config {
	// Check multiple possible config locations
	configPaths := []string{
		"pn532-spi.json",
		".pn532-spi.json",
		filepath.Join(os.Getenv("HOME"), ".config", "pn532", "spi.json"),
		"/etc/pn532/spi.json",
	}

	for _, path := range configPaths {
		// #nosec G304 -- paths are hardcoded above, not user input
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var configs []Config
		if err := json.Unmarshal(data, &configs); err != nil {
			// Try single config format
			var config Config
			if err := json.Unmarshal(data, &config); err == nil {
				return []Config{config}
			}
			continue
		}

		return configs
	}

	return nil
}

// loadEnvConfig loads SPI configuration from environment variable
func loadEnvConfig() *Config {
	// Check PN532_SPI_DEVICE environment variable
	device := os.Getenv("PN532_SPI_DEVICE")
	if device == "" {
		return nil
	}

	config := Config{
		Device: device,
		Name:   "SPI device from environment",
	}

	// Check for CS pin
	if csPin := os.Getenv("PN532_SPI_CS_PIN"); csPin != "" {
		var pin int
		if _, err := fmt.Sscanf(csPin, "%d", &pin); err == nil {
			config.CSPin = pin
		}
	}

	return &config
}

// detectPlatformDevices attempts platform-specific SPI device detection
func detectPlatformDevices() []Config {
	switch runtime.GOOS {
	case "linux":
		// On Linux, check device tree for SPI devices
		return checkLinuxDeviceTree()
	default:
		return nil
	}
}

// detectLinuxSPIDevices returns common Linux SPI device paths
func detectLinuxSPIDevices() []Config {
	var configs []Config

	// Check for /dev/spidev* devices
	matches, err := filepath.Glob("/dev/spidev*")
	if err != nil {
		return configs
	}

	for _, path := range matches {
		// Check if device exists and is accessible
		if _, err := os.Stat(path); err == nil {
			configs = append(configs, Config{
				Device: path,
				Name:   fmt.Sprintf("SPI device %s", filepath.Base(path)),
			})
		}
	}

	return configs
}

// checkLinuxDeviceTree checks Linux device tree for configured SPI devices
func checkLinuxDeviceTree() []Config {
	// This is a simplified check - in practice, parsing device tree
	// requires more complex handling
	var configs []Config

	// Check for device tree overlays mentioning PN532
	dtPath := "/proc/device-tree"
	if _, err := os.Stat(dtPath); err != nil {
		return configs
	}

	// Look for SPI nodes with PN532 compatible string
	// This is platform-specific and would need proper implementation
	// for production use

	return configs
}

// deduplicateConfigs removes duplicate SPI configurations
func deduplicateConfigs(configs []Config) []Config {
	seen := make(map[string]bool)
	var unique []Config

	for _, config := range configs {
		if !seen[config.Device] {
			seen[config.Device] = true
			unique = append(unique, config)
		}
	}

	return unique
}

// probeSPIDevice attempts to verify an SPI device is a PN532
func probeSPIDevice(_ context.Context, config Config, _ detection.Mode) bool {
	// Create SPI transport
	transport, err := spi.New(config.Device)
	if err != nil {
		return false
	}
	defer func() { _ = transport.Close() }()

	// Create PN532 device
	device, err := pn532.New(transport)
	if err != nil {
		return false
	}

	// Try to get firmware version
	_, err = device.GetFirmwareVersion()
	return err == nil
}
