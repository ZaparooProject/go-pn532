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

package uart

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/ZaparooProject/go-pn532/internal/syncutil"
	"github.com/ZaparooProject/go-pn532/transport/uart"
)

// detector implements the Detector interface for UART devices.
type detector struct {
	// inflight holds the paths whose probe goroutine has not returned. A probe
	// can block inside write(2) or tcdrain on a port whose device never reads
	// its serial input, and nothing can interrupt a thread parked in the
	// kernel like that, so the goroutine is abandoned and the path is left
	// alone until it comes back.
	inflight map[string]struct{}
	mu       syncutil.Mutex
}

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

		// Skip a port whose earlier probe is still parked in the kernel;
		// opening it again would park another goroutine on the same device.
		if d.probeInflight(port.Path) {
			continue
		}

		// Copy the loop variable to avoid memory aliasing
		portCopy := port
		// Apply platform-specific positive filtering
		if d.shouldIncludePort(&portCopy, opts.Mode) {
			filtered = append(filtered, port)
		}
	}
	return filtered
}

// shouldIncludePort determines whether a port is worth handing to processPort.
//
// A port that carries recognisable evidence is always a candidate. Without that
// evidence the answer depends on the mode: Safe verifies by probing and drops a
// port that does not answer, so requiring a descriptor match first can only hide
// real devices. The name patterns below are macOS device names, so on Linux no
// port could ever match them and detection rested entirely on the four VID:PIDs
// in isLikelyPN532; a PN532 on any other USB-serial bridge, or one whose sysfs
// descriptors could not be read, was discarded without ever being probed.
//
// Built-in UARTs are the exception. They are frequently a serial console, and
// writing PN532 frames at one is disruptive whether or not anything answers, so
// those still have to look like a PN532 to earn a probe.
//
// So is a serial port on a USB device that also presents a HID interface. That
// is a gamepad adapter, lightgun, keyboard or spinner with a serial port on the
// side, not a PN532, and its manufacturer string is often "Arduino", which is
// exactly the evidence matchesGoodPatterns would accept. Writing at it is worse
// than useless: an Arduino sketch that never reads its serial input stops
// accepting data after about 128 bytes, and from then on the probe is parked in
// the kernel with no way to interrupt it.
func (d *detector) shouldIncludePort(port *serialPort, mode detection.Mode) bool {
	if isLikelyPN532(port) {
		return true
	}
	if port.HID {
		return false
	}
	if d.matchesGoodPatterns(port) {
		return true
	}
	return mode == detection.Safe && !port.Builtin
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
		} else if opts.Mode == detection.Safe {
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

// probeTimeout bounds one port's probe. A PN532 answers GetFirmwareVersion well
// inside this even with the wake-up retries. Tests shorten it.
var probeTimeout = 500 * time.Millisecond

// probePortWithTimeout probes one port without letting it hold up the pass.
//
// The probe's syscalls are not interruptible: a device that enumerates a serial
// port but never reads it leaves write(2) and tcdrain blocked in the kernel, and
// closing the fd from another goroutine does not unblock them. So the probe runs
// on its own goroutine and the pass moves on when the deadline passes. The
// abandoned goroutine keeps its path in inflight until it returns, which happens
// once the device is unplugged and the kernel fails the write, so at most one
// goroutine and one fd are parked per such device.
func (d *detector) probePortWithTimeout(ctx context.Context, path string, mode detection.Mode) bool {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	d.markInflight(path)
	result := make(chan bool, 1)
	go func() {
		ok := probeDeviceFn(probeCtx, path, mode)
		// Cleared before the answer is delivered, so a caller that receives
		// it never sees the path still marked.
		d.clearInflight(path)
		result <- ok
	}()

	select {
	case ok := <-result:
		return ok
	case <-probeCtx.Done():
		// A late answer is discarded; the port is probed again on a later
		// pass once its goroutine has come back.
		return false
	}
}

func (d *detector) markInflight(path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.inflight == nil {
		d.inflight = make(map[string]struct{})
	}
	d.inflight[path] = struct{}{}
}

func (d *detector) clearInflight(path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.inflight, path)
}

func (d *detector) probeInflight(path string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.inflight[path]
	return ok
}

// serialPort represents a serial port with metadata
type serialPort struct {
	Path         string
	Name         string
	VIDPID       string
	Manufacturer string
	Product      string
	SerialNumber string
	// Builtin marks a UART wired into the board rather than a removable
	// adapter. These are commonly consoles, so they are never probed
	// speculatively.
	Builtin bool
	// HID marks a port on a USB device that also presents a HID interface:
	// a controller adapter or lightgun with a serial port on the side. Only
	// the Linux enumerator can tell; elsewhere it stays false.
	HID bool
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

// probeDeviceFn is the function used to probe devices during detection.
// Defaults to probeDeviceImpl; overridden in tests.
var probeDeviceFn = probeDeviceImpl

// probeDeviceImpl attempts to communicate with a device to verify it's a PN532.
//
// NO RETRY POLICY: This function intentionally performs only a single attempt
// to communicate with each device. Retrying failed connections during auto-detection
// could overwhelm devices that are not actually PN532 readers, potentially causing:
// - Hardware stress on non-PN532 devices
// - Delayed detection process
// - Resource exhaustion on busy/restricted devices
//
// Connection retries are handled at the device level for known PN532 paths,
// not during the auto-detection phase.
func probeDeviceImpl(ctx context.Context, path string, mode detection.Mode) bool {
	// Try to open the port (single attempt only)
	transport, err := uart.New(path)
	if err != nil {
		return false
	}
	defer func() { _ = transport.Close() }()

	// Create a PN532 device (single attempt only)
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
		_, err := device.GetFirmwareVersion(ctx)
		return err == nil

	case detection.Full:
		// Try full initialization (SAM configuration)
		err := device.Init(ctx)
		return err == nil

	default:
		return false
	}
}
