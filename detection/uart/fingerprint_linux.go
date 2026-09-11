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
	"strconv"
)

// devDir is where device nodes are created. It is a variable so tests can
// point the fingerprint at a directory they control.
var devDir = "/dev"

// enumerationFingerprint returns a value that changes whenever a device node
// is created or removed. Plugging or unplugging a serial adapter adds or
// removes its node under /dev, which updates the directory's modification
// time, so an unchanged fingerprint means enumeration would see the same
// ports.
func enumerationFingerprint() (string, bool) {
	info, err := os.Stat(devDir)
	if err != nil {
		return "", false
	}
	return strconv.FormatInt(info.ModTime().UnixNano(), 10), true
}
