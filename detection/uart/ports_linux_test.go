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

//nolint:paralleltest // Tests mutate package-level globPorts and statPort
package uart

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubPortLookup points the enumerator at a fixed set of paths instead of the
// host's device nodes, and restores the real functions when the test ends.
func stubPortLookup(t *testing.T, present ...string) {
	t.Helper()

	origGlob, origStat := globPorts, statPort
	t.Cleanup(func() { globPorts, statPort = origGlob, origStat })

	set := make(map[string]bool, len(present))
	for _, p := range present {
		set[p] = true
	}

	globPorts = func(pattern string) ([]string, error) {
		var matched []string
		for p := range set {
			if ok, err := filepath.Match(pattern, p); err == nil && ok {
				matched = append(matched, p)
			}
		}
		return matched, nil
	}
	statPort = func(name string) (os.FileInfo, error) {
		if set[name] {
			return nil, nil //nolint:nilnil // only the error is read
		}
		return nil, fs.ErrNotExist
	}
}

func portByPath(ports []serialPort, path string) *serialPort {
	for i := range ports {
		if ports[i].Path == path {
			return &ports[i]
		}
	}
	return nil
}

func TestGetSerialPortsFallback_MarksOnBoardUARTsBuiltin(t *testing.T) {
	// The Builtin flag is what keeps the detector from writing PN532 frames at
	// a serial console, so the classification is worth pinning down: USB
	// adapters are probed speculatively in Safe mode, on-board UARTs are not.
	stubPortLookup(t, "/dev/ttyUSB0", "/dev/ttyACM0", "/dev/ttyS0", "/dev/ttyAMA0")

	ports, err := getSerialPortsFallback(context.Background())
	require.NoError(t, err)
	require.Len(t, ports, 4)

	for _, tc := range []struct {
		path    string
		builtin bool
	}{
		{path: "/dev/ttyUSB0", builtin: false},
		{path: "/dev/ttyACM0", builtin: false},
		{path: "/dev/ttyS0", builtin: true},
		{path: "/dev/ttyAMA0", builtin: true},
	} {
		port := portByPath(ports, tc.path)
		require.NotNil(t, port, "expected %s to be enumerated", tc.path)
		assert.Equal(t, tc.builtin, port.Builtin, "Builtin for %s", tc.path)
		assert.Equal(t, filepath.Base(tc.path), port.Name)
	}
}

func TestGetBuiltinSerialPorts_ReturnsOnlyOnBoardUARTs(t *testing.T) {
	stubPortLookup(t, "/dev/ttyUSB0", "/dev/ttyS0", "/dev/ttyAMA0")

	ports, err := getBuiltinSerialPorts(context.Background())
	require.NoError(t, err)
	require.Len(t, ports, 2, "USB adapters belong to the sysfs enumerator, not this one")

	for _, port := range ports {
		assert.True(t, port.Builtin, "%s should be marked built-in", port.Path)
	}
	assert.Nil(t, portByPath(ports, "/dev/ttyUSB0"))
}

func TestPortsMatching_SkipsPathsThatVanishBeforeStat(t *testing.T) {
	origGlob, origStat := globPorts, statPort
	t.Cleanup(func() { globPorts, statPort = origGlob, origStat })

	globPorts = func(string) ([]string, error) { return []string{"/dev/ttyUSB9"}, nil }
	statPort = func(string) (os.FileInfo, error) { return nil, fs.ErrNotExist }

	assert.Empty(t, portsMatching(fallbackPortPatterns))
}

func TestPortsMatching_SkipsPatternsThatFailToGlob(t *testing.T) {
	origGlob, origStat := globPorts, statPort
	t.Cleanup(func() { globPorts, statPort = origGlob, origStat })

	globPorts = func(string) ([]string, error) { return nil, errors.New("bad pattern") }
	statPort = func(string) (os.FileInfo, error) { return nil, nil } //nolint:nilnil // only the error is read

	assert.Empty(t, portsMatching(fallbackPortPatterns))
}
