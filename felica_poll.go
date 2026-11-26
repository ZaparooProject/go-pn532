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

package pn532

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	// brTyFeliCa212 is the InListPassiveTarget baud rate for FeliCa at 212 kbps.
	brTyFeliCa212 byte = 0x01

	// feliCaPollResponseCode starts a FeliCa polling response (POL_RES).
	feliCaPollResponseCode byte = 0x01

	// feliCaPollResLen is the shortest POL_RES length byte: itself, the
	// response code, IDm and PMm. A reply with the system code is 2 longer.
	feliCaPollResLen = 18

	// feliCaPollTimeout bounds one FeliCa poll on the host side. The PN532
	// answers within a few milliseconds when MxRtyPassiveActivation is
	// finite.
	feliCaPollTimeout = time.Second
)

// feliCaPollingRequest is the InListPassiveTarget initiator data for FeliCa:
// the polling command, the wildcard system code FFFF, request code 01 (reply
// with the system code) and a single time slot.
var feliCaPollingRequest = []byte{0x00, 0xFF, 0xFF, 0x01, 0x00}

// InListPassiveFeliCaTarget polls once for a FeliCa target at 212 kbps. It
// returns nil and no error when none answers. The tag's IDm is its UID.
//
// With the default MxRtyPassiveActivation of 0xFF the PN532 waits until a
// FeliCa target answers. Set a finite count with
// SetPollingRetriesWithActivation to use this in a polling loop.
func (d *Device) InListPassiveFeliCaTarget(ctx context.Context) (*DetectedTag, error) {
	const maxTg byte = 1
	data := make([]byte, 0, 2+len(feliCaPollingRequest))
	data = append(data, maxTg, brTyFeliCa212)
	data = append(data, feliCaPollingRequest...)

	var prevTimeout time.Duration
	if d.config != nil {
		prevTimeout = d.config.Timeout
	}
	if err := d.transport.SetTimeout(d.getContextTimeoutOrFallback(ctx, feliCaPollTimeout)); err != nil {
		Debugf("InListPassiveFeliCaTarget: SetTimeout failed: %v", err)
	}
	defer func() { _ = d.transport.SetTimeout(prevTimeout) }()

	res, err := d.transport.SendCommand(ctx, cmdInListPassiveTarget, data)
	if err != nil {
		return nil, fmt.Errorf("FeliCa InListPassiveTarget failed: %w", err)
	}
	Debugf("InListPassiveFeliCaTarget response (%d bytes): %X", len(res), res)

	if err := d.validateInListPassiveTargetResponse(res); err != nil {
		return nil, err
	}
	return parseFeliCaTargetResponse(res)
}

// parseFeliCaTargetResponse parses an InListPassiveTarget reply for FeliCa:
// 4B, NbTg, then Tg and the POL_RES (length, response code, IDm, PMm and
// the optional system code).
func parseFeliCaTargetResponse(res []byte) (*DetectedTag, error) {
	if len(res) < 2 {
		return nil, fmt.Errorf("response too short: %d bytes", len(res))
	}
	if res[1] == 0 {
		return nil, nil //nolint:nilnil // nil tag, nil error is the "no tag detected" response
	}

	const polResOffset = 3 // 4B, NbTg, Tg
	if len(res) <= polResOffset {
		return nil, fmt.Errorf("FeliCa target truncated: %d bytes", len(res))
	}
	polResLen := int(res[polResOffset])
	if polResLen < feliCaPollResLen || len(res) < polResOffset+polResLen {
		return nil, fmt.Errorf("FeliCa POL_RES length %d does not fit %d byte response", polResLen, len(res))
	}
	polRes := res[polResOffset+1 : polResOffset+polResLen]
	if polRes[0] != feliCaPollResponseCode {
		return nil, fmt.Errorf("unexpected FeliCa response code 0x%02X", polRes[0])
	}

	idm := make([]byte, feliCaIDmLength)
	copy(idm, polRes[1:1+feliCaIDmLength])
	targetData := make([]byte, len(polRes))
	copy(targetData, polRes)

	return &DetectedTag{
		Type:       TagTypeFeliCa,
		UID:        hex.EncodeToString(idm),
		UIDBytes:   idm,
		DetectedAt: time.Now(),
		// From the response code on, as NewFeliCaTag reads it.
		TargetData: targetData,
	}, nil
}
