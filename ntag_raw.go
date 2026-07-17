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
	"fmt"
)

func (t *NTAGTag) requiresRawType2Commands() bool {
	return t.device.hasCapability(CapabilityRequiresRawType2Commands)
}

func calculateCRCA(data []byte) [2]byte {
	crc := uint16(0x6363)
	for _, value := range data {
		ch := value ^ byte(crc)
		ch ^= ch << 4
		crc = (crc >> 8) ^ (uint16(ch) << 8) ^ (uint16(ch) << 3) ^ (uint16(ch) >> 4)
	}
	return [2]byte{byte(crc), byte(crc >> 8)}
}

func appendCRCA(data []byte) []byte {
	command := make([]byte, len(data), len(data)+2)
	copy(command, data)
	crc := calculateCRCA(data)
	return append(command, crc[0], crc[1])
}

func (t *NTAGTag) sendRawType2Command(ctx context.Context, command []byte) ([]byte, error) {
	data, err := t.device.SendRawCommand(ctx, appendCRCA(command))
	if selectErr := t.device.InSelect(ctx); selectErr != nil {
		Debugln("NTAG raw Type 2 command: InSelect failed:", selectErr)
	}
	return data, err
}

func (t *NTAGTag) readRawType2Pages(ctx context.Context, startPage uint8, expectedBytes int) ([]byte, error) {
	data, err := t.sendRawType2Command(ctx, []byte{ntagCmdRead, startPage})
	if err != nil {
		return nil, err
	}
	if len(data) < expectedBytes {
		return nil, fmt.Errorf("invalid raw READ response length %d (expected at least %d)", len(data), expectedBytes)
	}
	return data[:expectedBytes], nil
}
