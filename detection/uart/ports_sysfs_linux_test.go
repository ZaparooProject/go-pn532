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

//go:build linux

package uart

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// linkTTY builds a /sys/class/tty-shaped symlink pointing at target.
func linkTTY(t *testing.T, dir, name, target string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.Symlink(target, path))
	return path
}

func TestClassLinkIsUSB(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	tests := []struct {
		name   string
		tty    string
		target string
		isUSB  bool
		known  bool
	}{
		{
			name:   "usb serial adapter",
			tty:    "ttyUSB0",
			target: "../../devices/pci0000:00/0000:00:14.0/usb1/1-2/1-2:1.0/ttyUSB0",
			isUSB:  true,
			known:  true,
		},
		{
			name:   "usb cdc acm",
			tty:    "ttyACM0",
			target: "../../devices/platform/soc/ffb40000.usb/usb1/1-1/1-1:1.0/tty/ttyACM0",
			isUSB:  true,
			known:  true,
		},
		{
			name:   "on-board 8250 uart",
			tty:    "ttyS0",
			target: "../../devices/platform/serial8250/serial8250:0/serial8250:0.0/tty/ttyS0",
			isUSB:  false,
			known:  true,
		},
		{
			name:   "virtual console",
			tty:    "tty1",
			target: "../../devices/virtual/tty/tty1",
			isUSB:  false,
			known:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := linkTTY(t, dir, tt.tty, tt.target)

			isUSB, known := classLinkIsUSB(path)

			assert.Equal(t, tt.known, known)
			assert.Equal(t, tt.isUSB, isUSB)
		})
	}
}

func TestClassLinkIsUSB_UnknownWhenNotASymlink(t *testing.T) {
	t.Parallel()

	// A kernel built with CONFIG_SYSFS_DEPRECATED exposes real directories
	// here. The caller must fall back to resolving <tty>/device rather than
	// treating an unanswerable question as "not USB".
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "ttyUSB0"), 0o750))

	isUSB, known := classLinkIsUSB(filepath.Join(dir, "ttyUSB0"))

	assert.False(t, known, "a non-symlink entry must not be answered")
	assert.False(t, isUSB)
}

func TestClassLinkIsUSB_UnknownWhenMissing(t *testing.T) {
	t.Parallel()

	isUSB, known := classLinkIsUSB(filepath.Join(t.TempDir(), "nope"))

	assert.False(t, known)
	assert.False(t, isUSB)
}

func BenchmarkProcessUSBDevice(b *testing.B) {
	// Measures the real /sys/class/tty walk on the host running the benchmark.
	for b.Loop() {
		if _, err := processUSBDevice(b.Context(), "/sys/class/tty"); err != nil {
			b.Fatal(err)
		}
	}
}

// fakeSysfs builds a sysfs-shaped fixture tree and points the enumerator at it.
// The USB branch carries a "usb1" component because processUSBDeviceEntry
// decides a device is USB by looking for "/usb" in the resolved device path.
//
// Layout, mirroring how the kernel lays out a CH340 on a USB port:
//
//	<root>/class/tty/ttyUSB0 -> ../../devices/usb1/1-2/1-2:1.0/ttyUSB0
//	<root>/class/tty/ttyS0   -> ../../devices/platform/serial8250/tty/ttyS0
//	<root>/devices/usb1/1-2/{idVendor,idProduct,manufacturer,product,serial}
//	<root>/devices/usb1/1-2/1-2:1.0/ttyUSB0/device -> ..
func fakeSysfs(t *testing.T) (root, ttyDir string) {
	t.Helper()

	root = t.TempDir()

	origRoot, origClass := sysfsRoot, sysfsClassTTY
	t.Cleanup(func() { sysfsRoot, sysfsClassTTY = origRoot, origClass })

	ttyDir = filepath.Join(root, "class", "tty")
	require.NoError(t, os.MkdirAll(ttyDir, 0o750))

	// USB serial adapter: attributes live on the device one level above the
	// interface, which is what forces readUSBAttributes to walk upwards.
	usbDevice := filepath.Join(root, "devices", "usb1", "1-2")
	usbTTY := filepath.Join(usbDevice, "1-2:1.0", "ttyUSB0")
	require.NoError(t, os.MkdirAll(usbTTY, 0o750))
	for name, content := range map[string]string{
		"idVendor":     "1a86\n",
		"idProduct":    "7523\n",
		"manufacturer": "QinHeng Electronics\n",
		"product":      "USB Serial\n",
		"serial":       "ABC123\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(usbDevice, name), []byte(content), 0o600))
	}
	require.NoError(t, os.Symlink("..", filepath.Join(usbTTY, "device")))
	require.NoError(t, os.Symlink(usbTTY, filepath.Join(ttyDir, "ttyUSB0")))

	// On-board UART, which must be rejected without reading any attributes.
	builtinTTY := filepath.Join(root, "devices", "platform", "serial8250", "tty", "ttyS0")
	require.NoError(t, os.MkdirAll(builtinTTY, 0o750))
	require.NoError(t, os.Symlink("..", filepath.Join(builtinTTY, "device")))
	require.NoError(t, os.Symlink(builtinTTY, filepath.Join(ttyDir, "ttyS0")))

	sysfsRoot, sysfsClassTTY = root, ttyDir
	return root, ttyDir
}

//nolint:paralleltest // mutates package-level sysfsRoot and sysfsClassTTY
func TestProcessUSBDevice_ReturnsUSBSerialWithDescriptors(t *testing.T) {
	_, ttyDir := fakeSysfs(t)

	ports, err := processUSBDevice(t.Context(), ttyDir)
	require.NoError(t, err)
	require.Len(t, ports, 1, "only the USB adapter should be returned; ttyS0 is not USB")

	port := ports[0]
	assert.Equal(t, "/dev/ttyUSB0", port.Path)
	assert.Equal(t, "ttyUSB0", port.Name)
	// Read from the device one level above the interface, so this also pins
	// down readUSBAttributes walking up the tree rather than only looking once.
	assert.Equal(t, "1A86:7523", port.VIDPID)
	assert.Equal(t, "QinHeng Electronics", port.Manufacturer)
	assert.Equal(t, "USB Serial", port.Product)
	assert.Equal(t, "ABC123", port.SerialNumber)
}

//nolint:paralleltest // mutates package-level readLink, sysfsRoot and sysfsClassTTY
func TestProcessUSBDevice_KeepsUSBDeviceWhenClassLinkCannotAnswer(t *testing.T) {
	// The readlink pre-filter is only safe if an unanswerable check falls
	// through to resolving <tty>/device instead of counting as a rejection.
	// With readLink always failing, the USB adapter must still be found with
	// its descriptors intact.
	_, ttyDir := fakeSysfs(t)

	origReadLink := readLink
	t.Cleanup(func() { readLink = origReadLink })
	readLink = func(string) (string, error) { return "", errors.New("unanswerable") }

	ports, err := processUSBDevice(t.Context(), ttyDir)
	require.NoError(t, err)

	require.Len(t, ports, 1, "the slow path must still find the adapter")
	assert.Equal(t, "/dev/ttyUSB0", ports[0].Path)
	assert.Equal(t, "1A86:7523", ports[0].VIDPID)
}

//nolint:paralleltest // mutates package-level sysfsRoot and sysfsClassTTY
func TestProcessUSBDevice_SkipsPlainDirectoryEntries(t *testing.T) {
	// Entries that are real directories rather than symlinks are skipped by
	// the IsDir guard before the pre-filter is consulted at all.
	_, ttyDir := fakeSysfs(t)
	require.NoError(t, os.MkdirAll(filepath.Join(ttyDir, "ttyUSB9"), 0o750))

	ports, err := processUSBDevice(t.Context(), ttyDir)
	require.NoError(t, err)

	for _, port := range ports {
		assert.NotEqual(t, "/dev/ttyUSB9", port.Path)
	}
}

//nolint:paralleltest // mutates package-level sysfsRoot and sysfsClassTTY
func TestProcessUSBDevice_SkipsTTYWithoutDeviceLink(t *testing.T) {
	_, ttyDir := fakeSysfs(t)

	// A virtual console has no device link at all.
	require.NoError(t, os.Symlink(
		filepath.Join(ttyDir, "..", "..", "devices", "virtual", "tty", "tty1"),
		filepath.Join(ttyDir, "tty1"),
	))

	ports, err := processUSBDevice(t.Context(), ttyDir)
	require.NoError(t, err)

	for _, port := range ports {
		assert.NotEqual(t, "/dev/tty1", port.Path)
	}
}

//nolint:paralleltest // mutates package-level sysfsRoot and sysfsClassTTY
func TestProcessUSBDevice_ErrorsWhenClassDirectoryIsMissing(t *testing.T) {
	_, ttyDir := fakeSysfs(t)

	_, err := processUSBDevice(t.Context(), filepath.Join(ttyDir, "does-not-exist"))

	require.Error(t, err)
}

//nolint:paralleltest // mutates package-level sysfsRoot
func TestReadUSBIdentifiers_RefusesPathsOutsideSysfsRoot(t *testing.T) {
	// The path handed to these readers comes from resolving a symlink, so the
	// root check is what stops a stray link making the detector read arbitrary
	// files. Losing it would not fail any other test.
	root, _ := fakeSysfs(t)

	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "idVendor"), []byte("dead"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "idProduct"), []byte("beef"), 0o600))

	var port serialPort
	assert.False(t, readUSBIdentifiers(&port, outside), "a path outside sysfsRoot must be refused")
	assert.Empty(t, port.VIDPID)

	// The same files inside the root are read, so the refusal above is the
	// root check and not a missing fixture.
	inside := filepath.Join(root, "devices", "usb1", "1-2")
	require.True(t, readUSBIdentifiers(&port, inside))
	assert.Equal(t, "1A86:7523", port.VIDPID)
}

//nolint:paralleltest // mutates package-level sysfsRoot
func TestReadUSBAttributes_StopsWalkingAtTheRoot(t *testing.T) {
	// No idVendor anywhere above the starting point: the walk has to terminate
	// rather than climbing past the filesystem root.
	root, _ := fakeSysfs(t)

	deep := filepath.Join(root, "devices", "platform", "serial8250", "tty", "ttyS0")
	var port serialPort
	readUSBAttributes(&port, deep)

	assert.Empty(t, port.VIDPID, "an on-board UART carries no USB identifiers")
}

//nolint:paralleltest // mutates package-level sysfs and glob seams
func TestGetSerialPorts_CombinesUSBAndBuiltinWithoutFallback(t *testing.T) {
	_, _ = fakeSysfs(t)

	origGlob, origStat := globPorts, statPort
	t.Cleanup(func() { globPorts, statPort = origGlob, origStat })
	globPorts = func(pattern string) ([]string, error) {
		if pattern == "/dev/ttyS*" {
			return []string{"/dev/ttyS0"}, nil
		}
		return nil, nil
	}
	statPort = func(string) (os.FileInfo, error) { return nil, nil } //nolint:nilnil // only the error is read

	ports, err := getSerialPorts(t.Context())
	require.NoError(t, err)

	require.Len(t, ports, 2)
	usb := portByPath(ports, "/dev/ttyUSB0")
	require.NotNil(t, usb, "the sysfs walk should contribute the USB adapter")
	assert.Equal(t, "1A86:7523", usb.VIDPID)

	builtin := portByPath(ports, "/dev/ttyS0")
	require.NotNil(t, builtin, "the built-in enumerator should contribute ttyS0")
	assert.True(t, builtin.Builtin)
}

//nolint:paralleltest // mutates package-level sysfs and glob seams
func TestGetSerialPorts_FallsBackOnlyWhenNothingWasFound(t *testing.T) {
	// The fallback globs /dev directly and yields no USB metadata, so it only
	// runs when both richer sources came back empty. A host with any on-board
	// UART therefore never reaches it — which is why the fallback does not
	// rescue a device the sysfs walk has rejected.
	_, ttyDir := fakeSysfs(t)
	sysfsClassTTY = filepath.Join(ttyDir, "empty")
	require.NoError(t, os.MkdirAll(sysfsClassTTY, 0o750))

	origGlob, origStat := globPorts, statPort
	t.Cleanup(func() { globPorts, statPort = origGlob, origStat })

	var patternsSeen []string
	globPorts = func(pattern string) ([]string, error) {
		patternsSeen = append(patternsSeen, pattern)
		if pattern == "/dev/ttyUSB*" {
			return []string{"/dev/ttyUSB7"}, nil
		}
		return nil, nil
	}
	statPort = func(string) (os.FileInfo, error) { return nil, nil } //nolint:nilnil // only the error is read

	ports, err := getSerialPorts(t.Context())
	require.NoError(t, err)

	require.Len(t, ports, 1)
	assert.Equal(t, "/dev/ttyUSB7", ports[0].Path)
	assert.False(t, ports[0].Builtin)
	assert.Contains(t, patternsSeen, "/dev/ttyACM*", "the fallback patterns should have been tried")
}

// addHIDComposite adds an Arduino-style composite device to a fakeSysfs tree:
// a CDC interface carrying ttyACM0 beside a HID interface, with the identity
// attributes on the device above both, mirroring how the kernel lays out a
// Pro Micro gamepad adapter.
//
//	<root>/class/tty/ttyACM0 -> ../../devices/usb1/1-3/1-3:1.0/tty/ttyACM0
//	<root>/devices/usb1/1-3/{idVendor,idProduct,manufacturer,product}
//	<root>/devices/usb1/1-3/1-3:1.0/bInterfaceClass = 02
//	<root>/devices/usb1/1-3/1-3:1.1/bInterfaceClass = 03
func addHIDComposite(t *testing.T, root, ttyDir string) {
	t.Helper()

	device := filepath.Join(root, "devices", "usb1", "1-3")
	cdc := filepath.Join(device, "1-3:1.0")
	hid := filepath.Join(device, "1-3:1.1")
	tty := filepath.Join(cdc, "tty", "ttyACM0")
	require.NoError(t, os.MkdirAll(tty, 0o750))
	require.NoError(t, os.MkdirAll(hid, 0o750))
	for name, content := range map[string]string{
		"idVendor":     "2341\n",
		"idProduct":    "8036\n",
		"manufacturer": "Arduino LLC\n",
		"product":      "Arduino Leonardo\n",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(device, name), []byte(content), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(cdc, "bInterfaceClass"), []byte("02\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(hid, "bInterfaceClass"), []byte("03\n"), 0o600))
	require.NoError(t, os.Symlink(filepath.Join("..", ".."), filepath.Join(tty, "device")))
	require.NoError(t, os.Symlink(tty, filepath.Join(ttyDir, "ttyACM0")))
}

//nolint:paralleltest // mutates package-level sysfsRoot and sysfsClassTTY
func TestProcessUSBDevice_MarksPortsOnHIDDevices(t *testing.T) {
	root, ttyDir := fakeSysfs(t)
	addHIDComposite(t, root, ttyDir)

	ports, err := processUSBDevice(t.Context(), ttyDir)
	require.NoError(t, err)
	require.Len(t, ports, 2)

	byPath := make(map[string]serialPort, len(ports))
	for _, port := range ports {
		byPath[port.Path] = port
	}

	acm, ok := byPath["/dev/ttyACM0"]
	require.True(t, ok, "the composite device's tty must still be enumerated")
	assert.True(t, acm.HID, "a device with a HID interface beside its CDC one must be marked")
	assert.Equal(t, "2341:8036", acm.VIDPID)
	assert.Equal(t, "Arduino LLC", acm.Manufacturer)

	usb, ok := byPath["/dev/ttyUSB0"]
	require.True(t, ok)
	assert.False(t, usb.HID, "a plain USB-serial bridge has no HID interface")
}

//nolint:paralleltest // mutates package-level sysfsRoot
func TestHasHIDInterface_RefusesPathsOutsideSysfsRoot(t *testing.T) {
	root := t.TempDir()
	origRoot := sysfsRoot
	t.Cleanup(func() { sysfsRoot = origRoot })
	sysfsRoot = filepath.Join(root, "sys")

	outside := filepath.Join(root, "elsewhere", "1-3")
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "1-3:1.1"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "1-3:1.1", "bInterfaceClass"), []byte("03\n"), 0o600))

	assert.False(t, hasHIDInterface(outside), "a path outside sysfsRoot must not be read")
}
