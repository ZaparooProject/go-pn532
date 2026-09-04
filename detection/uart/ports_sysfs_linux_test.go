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
