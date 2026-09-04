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
	"testing"

	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/stretchr/testify/assert"
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

func TestPN532KillerIsLikelyAndRetainsMetadata(t *testing.T) {
	port := &serialPort{
		Path:         "/dev/ttyACM0",
		Name:         "ttyACM0",
		VIDPID:       "1A86:55D3",
		Manufacturer: "wch.cn",
		Product:      "PN532Killer-UART",
		SerialNumber: "00000001",
	}

	assert.True(t, isLikelyPN532(port))

	device := (&detector{}).createDeviceInfo(port, detection.Medium)
	assert.Equal(t, "1A86:55D3", device.Metadata["vidpid"])
	assert.Equal(t, "PN532Killer-UART", device.Metadata["product"])
	assert.Equal(t, "00000001", device.Metadata["serial"])
}
