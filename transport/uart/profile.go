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
	"path/filepath"
	"strings"

	"go.bug.st/serial/enumerator"
)

const (
	pn532KillerVID     = "1A86"
	pn532KillerPID     = "55D3"
	pn532KillerProduct = "PN532Killer-UART"
)

type deviceProfile uint8

const (
	profileGeneric deviceProfile = iota
	profilePN532Killer
)

var getDetailedPortsList = enumerator.GetDetailedPortsList

func resolveDeviceProfile(portName string) deviceProfile {
	ports, err := getDetailedPortsList()
	if err != nil {
		return profileGeneric
	}

	wantedName := canonicalPortName(portName)
	for _, port := range ports {
		if port == nil || !strings.EqualFold(canonicalPortName(port.Name), wantedName) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(port.VID), pn532KillerVID) &&
			strings.EqualFold(strings.TrimSpace(port.PID), pn532KillerPID) &&
			strings.EqualFold(strings.TrimSpace(port.Product), pn532KillerProduct) {
			return profilePN532Killer
		}
	}

	return profileGeneric
}

func canonicalPortName(portName string) string {
	resolved, err := filepath.EvalSymlinks(portName)
	if err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(portName)
}
