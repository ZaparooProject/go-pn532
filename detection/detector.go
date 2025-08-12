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

package detection

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Mode represents the level of invasiveness for device detection
type Mode int

const (
	// Passive mode only checks device descriptors without any communication
	Passive Mode = iota
	// Safe mode performs minimal probing with GetFirmwareVersion command
	Safe
	// Full mode performs complete verification including SAM connection test
	Full
)

// Confidence represents the confidence level of device detection
type Confidence int

const (
	// Low confidence - device might be PN532 (e.g., I2C ACK only)
	Low Confidence = iota
	// Medium confidence - device responds to basic commands
	Medium
	// High confidence - device confirmed as PN532
	High
)

// DeviceInfo represents a detected PN532 device
type DeviceInfo struct {
	// Additional metadata (e.g., VID:PID for USB devices)
	Metadata map[string]string
	// Transport type: "uart", "i2c", "spi"
	Transport string
	// Connection path (e.g., "/dev/ttyUSB0", "/dev/i2c-1")
	Path string
	// Human-readable device name
	Name string
	// Detection confidence level
	Confidence Confidence
}

// String returns a human-readable representation of the device
func (d DeviceInfo) String() string {
	confidence := "unknown"
	switch d.Confidence {
	case Low:
		confidence = "low"
	case Medium:
		confidence = "medium"
	case High:
		confidence = "high"
	}
	return fmt.Sprintf("%s device at %s (confidence: %s)", d.Transport, d.Path, confidence)
}

// Options configures the detection behavior
type Options struct {
	// USB VID:PID pairs to skip (e.g., ["1234:5678", "ABCD:EF01"])
	Blocklist []string
	// Which transports to check (empty = all)
	Transports []string
	// Cache TTL duration
	CacheTTL time.Duration
	// Maximum time to wait for detection
	Timeout time.Duration
	// Detection invasiveness level
	Mode Mode
	// Enable result caching
	EnableCache bool
}

// DefaultOptions returns sensible default detection options
func DefaultOptions() Options {
	return Options{
		Mode:        Safe,
		Timeout:     5 * time.Second,
		Blocklist:   DefaultBlocklist(),
		EnableCache: true,
		CacheTTL:    30 * time.Second,
	}
}

// Detector interface for transport-specific device detection
type Detector interface {
	// Detect searches for devices using the given options
	Detect(ctx context.Context, opts *Options) ([]DeviceInfo, error)
	// Transport returns the transport type this detector handles
	Transport() string
}

// Errors
var (
	// ErrNoDevicesFound indicates no PN532 devices were detected
	ErrNoDevicesFound = errors.New("no PN532 devices found")
	// ErrDetectionTimeout indicates detection timed out
	ErrDetectionTimeout = errors.New("detection timeout")
	// ErrUnsupportedPlatform indicates the platform doesn't support this detection method
	ErrUnsupportedPlatform = errors.New("platform not supported")
)

// registry holds all registered detectors
var registry []Detector

// RegisterDetector adds a detector to the registry
func RegisterDetector(d Detector) {
	registry = append(registry, d)
}

// getDetectors returns detectors filtered by transport types
func getDetectors(transports []string) []Detector {
	if len(transports) == 0 {
		return registry
	}

	var filtered []Detector
	for _, d := range registry {
		for _, t := range transports {
			if d.Transport() == t {
				filtered = append(filtered, d)
				break
			}
		}
	}
	return filtered
}

// DetectAll searches for PN532 devices across all registered transports
func DetectAll(opts *Options) ([]DeviceInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	return DetectAllContext(ctx, opts)
}

type detectionResult struct {
	err     error
	devices []DeviceInfo
}

// DetectAllContext searches for PN532 devices with custom context
func DetectAllContext(ctx context.Context, opts *Options) ([]DeviceInfo, error) {
	detectors := getDetectors(opts.Transports)
	if len(detectors) == 0 {
		return nil, errors.New("no detectors available for specified transports")
	}

	results := make(chan detectionResult, len(detectors))
	runDetectorsInParallel(ctx, detectors, opts, results)
	return collectDetectionResults(ctx, results, len(detectors))
}

// runDetectorsInParallel starts detection goroutines for all detectors
func runDetectorsInParallel(ctx context.Context, detectors []Detector, opts *Options, results chan detectionResult) {
	for _, detector := range detectors {
		go func(d Detector) {
			results <- runSingleDetector(ctx, d, opts)
		}(detector)
	}
}

// runSingleDetector performs detection for a single detector
func runSingleDetector(ctx context.Context, detector Detector, opts *Options) detectionResult {
	// Check cache if enabled
	if opts.EnableCache {
		if cached, found := getCached(detector.Transport(), opts.CacheTTL); found {
			return detectionResult{devices: cached}
		}
	}

	// Run detection
	devices, err := detector.Detect(ctx, opts)
	if err != nil && !errors.Is(err, ErrNoDevicesFound) {
		return detectionResult{err: err}
	}

	// Cache results if enabled
	if opts.EnableCache && len(devices) > 0 {
		setCached(detector.Transport(), devices)
	}

	return detectionResult{devices: devices}
}

// collectDetectionResults gathers results from all detector goroutines
func collectDetectionResults(
	ctx context.Context,
	results chan detectionResult,
	numDetectors int,
) ([]DeviceInfo, error) {
	var allDevices []DeviceInfo
	var errs []error

	for i := 0; i < numDetectors; i++ {
		select {
		case res := <-results:
			if res.err != nil {
				errs = append(errs, res.err)
			} else {
				allDevices = append(allDevices, res.devices...)
			}
		case <-ctx.Done():
			return nil, ErrDetectionTimeout
		}
	}

	return processDetectionResults(allDevices, errs)
}

// processDetectionResults processes the final detection results
func processDetectionResults(allDevices []DeviceInfo, errs []error) ([]DeviceInfo, error) {
	// Return devices even if some detectors failed
	if len(allDevices) > 0 {
		return allDevices, nil
	}

	// If no devices found and errors occurred, return first error
	if len(errs) > 0 {
		return nil, errs[0]
	}

	return nil, ErrNoDevicesFound
}

// ClearDetectionCache removes all cached detection results
func ClearDetectionCache() {
	clearCache()
}

// ClearDetectionCacheForTransport removes cached results for a specific transport
func ClearDetectionCacheForTransport(transport string) {
	clearCacheForTransport(transport)
}
