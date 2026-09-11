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
	// TransportUART identifies UART/serial PN532 detection.
	TransportUART = "uart"
	// TransportSPI identifies SPI PN532 detection.
	TransportSPI = "spi"
	// TransportI2C identifies I2C PN532 detection. I2C probing can be unsafe on
	// some platforms, so it is not part of the default auto-detect transports.
	TransportI2C = "i2c"
)

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
	// Device paths to explicitly ignore (e.g., ["/dev/ttyUSB0", "COM2"])
	IgnorePaths []string
	// Which transports to check. Empty means the safe default, UART only;
	// include TransportSPI or TransportI2C explicitly to opt into bus probing.
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

// DefaultTransports returns the safe default transports used for auto-detect.
// SPI and I2C are intentionally excluded because probing shared buses may be
// unsafe on some platforms; include them explicitly when bus detection is wanted.
func DefaultTransports() []string {
	return []string{TransportUART}
}

// DefaultOptions returns sensible default detection options.
func DefaultOptions() Options {
	return Options{
		Mode:        Safe,
		Timeout:     5 * time.Second,
		Blocklist:   DefaultBlocklist(),
		Transports:  DefaultTransports(),
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
		transports = DefaultTransports()
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

type detectionResult struct {
	err     error
	devices []DeviceInfo
}

// DetectAll searches for PN532 devices with custom context
func DetectAll(ctx context.Context, opts *Options) ([]DeviceInfo, error) {
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
			// Filter cached results through IgnorePaths and Blocklist,
			// since the original Detect() call applied these but cached
			// results bypass Detect() entirely.
			filtered := filterDevices(cached, opts)
			return detectionResult{devices: filtered}
		}
	}

	// Run detection
	devices, err := detector.Detect(ctx, opts)
	if err != nil && !errors.Is(err, ErrNoDevicesFound) {
		return detectionResult{err: err}
	}

	// Cache results if enabled
	if opts.EnableCache {
		if len(devices) > 0 {
			setCached(detector.Transport(), devices)
		} else {
			// Clear stale cache when no devices found. Without this,
			// a cached result for a now-disconnected device persists
			// until TTL expiry, causing consumers to attempt connections
			// to dead paths.
			clearCacheForTransport(detector.Transport())
		}
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

	for range numDetectors {
		select {
		case res := <-results:
			if res.err != nil {
				errs = append(errs, res.err)
			} else {
				allDevices = append(allDevices, res.devices...)
			}
		case <-ctx.Done():
			// Detectors that have already answered are not thrown away with
			// the one that has not; a wedged transport costs its own results,
			// not everyone's.
			if len(allDevices) > 0 {
				return allDevices, nil
			}
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

// filterDevices applies IgnorePaths and Blocklist filtering to a device list.
// This ensures cached results respect the same filtering as fresh detection.
func filterDevices(devices []DeviceInfo, opts *Options) []DeviceInfo {
	if len(opts.IgnorePaths) == 0 && len(opts.Blocklist) == 0 {
		return devices
	}

	var filtered []DeviceInfo
	for _, device := range devices {
		if IsPathIgnored(device.Path, opts.IgnorePaths) {
			continue
		}
		if vidpid, ok := device.Metadata["vidpid"]; ok && IsBlocked(vidpid, opts.Blocklist) {
			continue
		}
		filtered = append(filtered, device)
	}
	return filtered
}

// ClearDetectionCache removes all cached detection results
func ClearDetectionCache() {
	clearCache()
}

// ClearDetectionCacheForTransport removes cached results for a specific transport
func ClearDetectionCacheForTransport(transport string) {
	clearCacheForTransport(transport)
}
