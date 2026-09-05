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

//nolint:paralleltest // Tests mutate package-level probeDeviceFn
package uart

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessPort_SafeMode_FailedProbeDiscardsLikelyDevice(t *testing.T) {
	// Regression test: in Safe mode, a device matching isLikelyPN532 (e.g. CH340
	// VID:PID) must be discarded when the probe fails. Previously the
	// isLikelyPN532 guard caused these devices to be returned as false positives,
	// blocking detection of real PN532 devices that enumerate later.
	// See zaparoo-core#505, zaparoo-core#474.
	origProbe := probeDeviceFn
	defer func() { probeDeviceFn = origProbe }()

	probeDeviceFn = func(context.Context, string, detection.Mode) bool {
		return false
	}

	det := &detector{}
	port := &serialPort{
		Path:   "/dev/ttyUSB0",
		Name:   "USB Serial",
		VIDPID: "1A86:7523", // CH340 — isLikelyPN532 returns true
	}
	opts := &detection.Options{Mode: detection.Safe}

	_, included := det.processPort(context.Background(), port, opts)
	assert.False(t, included, "Safe mode must discard device when probe fails, even if isLikelyPN532")
}

func TestProcessPort_SafeMode_SuccessfulProbeReturnsDevice(t *testing.T) {
	origProbe := probeDeviceFn
	defer func() { probeDeviceFn = origProbe }()

	probeDeviceFn = func(context.Context, string, detection.Mode) bool {
		return true
	}

	det := &detector{}
	port := &serialPort{
		Path:   "/dev/ttyUSB0",
		Name:   "USB Serial",
		VIDPID: "1A86:7523",
	}
	opts := &detection.Options{Mode: detection.Safe}

	device, included := det.processPort(context.Background(), port, opts)
	assert.True(t, included)
	assert.Equal(t, detection.High, device.Confidence)
}

func TestProcessPort_SafeMode_FailedProbeDiscardsUnknownDevice(t *testing.T) {
	origProbe := probeDeviceFn
	defer func() { probeDeviceFn = origProbe }()

	probeDeviceFn = func(context.Context, string, detection.Mode) bool {
		return false
	}

	det := &detector{}
	port := &serialPort{
		Path:   "/dev/ttyUSB0",
		Name:   "USB Serial",
		VIDPID: "AAAA:BBBB", // Unknown device — isLikelyPN532 returns false
	}
	opts := &detection.Options{Mode: detection.Safe}

	_, included := det.processPort(context.Background(), port, opts)
	assert.False(t, included, "Safe mode must discard unknown device when probe fails")
}

func TestFilterPorts_SafeModeProbesPortsWithoutDescriptorEvidence(t *testing.T) {
	// Regression test: on Linux the goodPatterns are macOS device names and can
	// never match /dev/ttyUSB0, so a PN532 on any USB-serial bridge outside the
	// four VID:PIDs in isLikelyPN532 was discarded before it was ever probed.
	// Safe mode discards a port that fails its probe, so no descriptor evidence
	// is needed to earn one. See zaparoo-core#1400.
	det := &detector{}
	opts := &detection.Options{Mode: detection.Safe}

	ports := []serialPort{
		{Path: "/dev/ttyUSB0", Name: "ttyUSB0"},                      // descriptors unreadable
		{Path: "/dev/ttyUSB1", Name: "ttyUSB1", VIDPID: "1A86:55D4"}, // CH9102, not in isLikelyPN532
	}

	filtered := det.filterPorts(ports, opts)

	assert.Len(t, filtered, 2, "Safe mode must probe USB serial ports it cannot identify")
}

func TestFilterPorts_SafeModeSkipsBuiltinUARTWithoutEvidence(t *testing.T) {
	// Built-in UARTs are frequently a serial console. Writing PN532 frames at
	// one is disruptive whether or not anything answers, so they still need to
	// look like a PN532 before they are probed.
	det := &detector{}
	opts := &detection.Options{Mode: detection.Safe}

	ports := []serialPort{
		{Path: "/dev/ttyS0", Name: "ttyS0", Builtin: true},
		{Path: "/dev/ttyAMA0", Name: "ttyAMA0", Builtin: true, Product: "PN532 breakout"},
	}

	filtered := det.filterPorts(ports, opts)

	assert.Len(t, filtered, 1)
	assert.Equal(t, "/dev/ttyAMA0", filtered[0].Path, "a built-in UART that names a PN532 is still a candidate")
}

func TestFilterPorts_NonSafeModesStillRequireEvidence(t *testing.T) {
	// Passive never probes and Full reports a port even when its probe fails,
	// so neither can lean on the probe to reject an unknown port.
	det := &detector{}
	ports := []serialPort{
		{Path: "/dev/ttyUSB0", Name: "ttyUSB0"},
		{Path: "/dev/ttyUSB1", Name: "ttyUSB1", VIDPID: "1A86:7523"},
	}

	for _, mode := range []detection.Mode{detection.Passive, detection.Full} {
		filtered := det.filterPorts(ports, &detection.Options{Mode: mode})
		assert.Len(t, filtered, 1)
		assert.Equal(t, "/dev/ttyUSB1", filtered[0].Path)
	}
}

func TestFilterPorts_SafeModeStillHonoursBlocklistAndIgnorePaths(t *testing.T) {
	det := &detector{}
	opts := &detection.Options{
		Mode:        detection.Safe,
		Blocklist:   []string{"16C0:0F38"},
		IgnorePaths: []string{"/dev/ttyUSB2"},
	}

	ports := []serialPort{
		{Path: "/dev/ttyUSB0", Name: "ttyUSB0", VIDPID: "16C0:0F38"},
		{Path: "/dev/ttyUSB2", Name: "ttyUSB2"},
		{Path: "/dev/ttyUSB3", Name: "ttyUSB3"},
	}

	filtered := det.filterPorts(ports, opts)

	assert.Len(t, filtered, 1)
	assert.Equal(t, "/dev/ttyUSB3", filtered[0].Path)
}

func TestFilterPorts_SkipsPortsOnHIDDevicesWithoutPN532Evidence(t *testing.T) {
	// A serial port on a USB device that also presents a HID interface is a
	// controller adapter, not a reader. An Arduino Pro Micro gamepad adapter
	// enumerates a CDC port its sketch never reads, and probing it parks the
	// probe in the kernel for good, so the "Arduino" manufacturer evidence
	// that matchesGoodPatterns accepts must not qualify it. Known PN532
	// bridge IDs and descriptors that name a PN532 still do.
	// See zaparoo-core#1425, zaparoo-core#548.
	det := &detector{}
	opts := &detection.Options{Mode: detection.Safe}

	ports := []serialPort{
		{Path: "/dev/ttyACM0", Name: "ttyACM0", VIDPID: "2341:8036", Manufacturer: "Arduino LLC", HID: true},
		{Path: "/dev/ttyACM1", Name: "ttyACM1", VIDPID: "1A86:7523", HID: true},
		{Path: "/dev/ttyACM2", Name: "ttyACM2", Product: "PN532 NFC", HID: true},
		{Path: "/dev/ttyUSB0", Name: "ttyUSB0", Manufacturer: "Arduino LLC"},
	}

	filtered := det.filterPorts(ports, opts)

	paths := make([]string, 0, len(filtered))
	for _, port := range filtered {
		paths = append(paths, port.Path)
	}
	assert.Equal(t, []string{"/dev/ttyACM1", "/dev/ttyACM2", "/dev/ttyUSB0"}, paths)
}

func TestProcessPortsToDevices_ParkedProbeDoesNotHoldUpThePass(t *testing.T) {
	// A probe that blocks in the kernel cannot be interrupted, so the pass has
	// to move on without it: the reader on the next port must still be found
	// inside the same pass, later passes must leave the parked port alone,
	// and once its goroutine returns the port is eligible again.
	origProbe, origTimeout := probeDeviceFn, probeTimeout
	defer func() { probeDeviceFn, probeTimeout = origProbe, origTimeout }()
	probeTimeout = 20 * time.Millisecond

	release := make(chan struct{})
	probeDeviceFn = func(_ context.Context, path string, _ detection.Mode) bool {
		if path == "/dev/ttyACM0" {
			// Ignores its context, the way write(2) does.
			<-release
			return false
		}
		return true
	}

	det := &detector{}
	opts := &detection.Options{Mode: detection.Safe}
	ports := []serialPort{
		{Path: "/dev/ttyACM0", Name: "ttyACM0"},
		{Path: "/dev/ttyUSB0", Name: "ttyUSB0"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	devices := det.processPortsToDevices(ctx, det.filterPorts(ports, opts), opts)
	require.Len(t, devices, 1, "the port after the parked one must still be probed in the same pass")
	assert.Equal(t, "/dev/ttyUSB0", devices[0].Path)

	filtered := det.filterPorts(ports, opts)
	require.Len(t, filtered, 1, "a port whose probe is still parked must not be opened again")
	assert.Equal(t, "/dev/ttyUSB0", filtered[0].Path)

	close(release)
	assert.Eventually(t, func() bool {
		return len(det.filterPorts(ports, opts)) == 2
	}, time.Second, time.Millisecond, "the port must become eligible once its probe goroutine returns")
}

func TestProbePortWithTimeout_ClearsInflightBeforeAnswering(t *testing.T) {
	// A prompt probe must leave no trace: the caller that receives the answer
	// never sees the path still marked in flight.
	origProbe := probeDeviceFn
	defer func() { probeDeviceFn = origProbe }()
	probeDeviceFn = func(context.Context, string, detection.Mode) bool { return true }

	det := &detector{}
	assert.True(t, det.probePortWithTimeout(context.Background(), "/dev/ttyUSB0", detection.Safe))
	assert.False(t, det.probeInflight("/dev/ttyUSB0"))
}

func TestProbePortWithTimeout_OnlyOneProbeStartsPerPath(t *testing.T) {
	// Two passes probing the same port at once must open it once. Admission
	// is a single check-and-mark under the lock; the loser reports the port
	// as not answering rather than parking a second goroutine on it.
	origProbe, origTimeout := probeDeviceFn, probeTimeout
	defer func() { probeDeviceFn, probeTimeout = origProbe, origTimeout }()
	probeTimeout = 20 * time.Millisecond

	var started atomic.Int32
	release := make(chan struct{})
	probeDeviceFn = func(context.Context, string, detection.Mode) bool {
		started.Add(1)
		<-release
		return true
	}

	det := &detector{}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			det.probePortWithTimeout(context.Background(), "/dev/ttyACM0", detection.Safe)
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), started.Load(), "the same port must not be opened twice at once")
	assert.True(t, det.probeInflight("/dev/ttyACM0"), "the one probe that started is still parked")

	close(release)
	assert.Eventually(t, func() bool {
		return !det.probeInflight("/dev/ttyACM0")
	}, time.Second, time.Millisecond, "the parked probe must release the path when it returns")
}
