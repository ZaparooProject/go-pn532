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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // mutates package-level devDir
func TestEnumerationFingerprint_ChangesWhenANodeIsAdded(t *testing.T) {
	orig := devDir
	t.Cleanup(func() { devDir = orig })
	devDir = t.TempDir()

	// Start from an old modification time so the new node's update is
	// visible even on a filesystem with coarse timestamps.
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(devDir, past, past))

	before, ok := enumerationFingerprint()
	require.True(t, ok)
	require.NoError(t, os.WriteFile(filepath.Join(devDir, "ttyUSB0"), nil, 0o600))
	after, ok := enumerationFingerprint()
	require.True(t, ok)

	assert.NotEqual(t, before, after)
}

//nolint:paralleltest // mutates package-level devDir
func TestEnumerationFingerprint_UnavailableWithoutTheDirectory(t *testing.T) {
	orig := devDir
	t.Cleanup(func() { devDir = orig })
	devDir = filepath.Join(t.TempDir(), "missing")

	_, ok := enumerationFingerprint()
	assert.False(t, ok)
}
