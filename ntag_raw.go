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
	"bytes"
	"context"
	"errors"
	"fmt"
)

// errRawType2ShortResponse identifies a raw Type 2 response that did not
// contain the requested payload.
var errRawType2ShortResponse = errors.New("raw Type 2 response too short")

// requiresRawType2Commands reports whether the transport requires Type 2
// commands to include CRC-A and use InCommunicateThru.
func (t *NTAGTag) requiresRawType2Commands() bool {
	return t.device.hasCapability(CapabilityRequiresRawType2Commands)
}

// calculateCRCA calculates the ISO/IEC 14443-A CRC and returns it in wire order.
func calculateCRCA(data []byte) [2]byte {
	crc := uint16(0x6363)
	for _, value := range data {
		ch := value ^ byte(crc)
		ch ^= ch << 4
		crc = (crc >> 8) ^ (uint16(ch) << 8) ^ (uint16(ch) << 3) ^ (uint16(ch) >> 4)
	}
	return [2]byte{byte(crc), byte(crc >> 8)}
}

// appendCRCA returns a copy of data with its CRC-A bytes appended.
func appendCRCA(data []byte) []byte {
	command := make([]byte, len(data), len(data)+2)
	copy(command, data)
	crc := calculateCRCA(data)
	return append(command, crc[0], crc[1])
}

// sendRawType2Command appends CRC-A and sends a Type 2 command through the
// transport's raw command path.
func (t *NTAGTag) sendRawType2Command(ctx context.Context, command []byte) ([]byte, error) {
	// Raw Type 2 transports preserve the active target themselves. Some acknowledge
	// InSelect without ever returning its response, which stalls and disrupts the
	// following exchange.
	return t.device.SendRawCommand(ctx, appendCRCA(command))
}

// readRawType2Pages reads Type 2 data and removes any bytes beyond the requested
// payload, including an optional response CRC.
func (t *NTAGTag) readRawType2Pages(ctx context.Context, startPage uint8, expectedBytes int) ([]byte, error) {
	data, err := t.sendRawType2Command(ctx, []byte{ntagCmdRead, startPage})
	if err != nil {
		return nil, err
	}
	if len(data) < expectedBytes {
		return nil, fmt.Errorf("%w: invalid raw READ response length %d (expected at least %d)",
			errRawType2ShortResponse, len(data), expectedBytes)
	}
	return data[:expectedBytes], nil
}

// isRetryableRawType2ReadError reports whether a raw Type 2 read failed because
// of a short response or a transient RF error.
func isRetryableRawType2ReadError(err error) bool {
	return errors.Is(err, errRawType2ShortResponse) || isRetryableRFError(err)
}

// isRawType2WriteFramingError reports whether err is the framing status observed
// when a raw transport receives the four-bit Type 2 write acknowledgment.
func isRawType2WriteFramingError(err error) bool {
	var pn532Err *PN532Error
	return errors.As(err, &pn532Err) &&
		pn532Err.Command == "InCommunicateThru" && pn532Err.ErrorCode == 0x05
}

// verifyRawType2WriteAfterFramingError confirms an ambiguous raw write by reading
// the page back and requiring an exact data match.
func (t *NTAGTag) verifyRawType2WriteAfterFramingError(
	ctx context.Context, block uint8, expected []byte, writeErr error,
) error {
	actual, err := t.readRawType2Pages(ctx, block, ntagBlockSize)
	if err != nil {
		return fmt.Errorf("%w (block %d): ambiguous Type 2 ACK (%v); verification read failed: %w",
			ErrTagWriteFailed, block, writeErr, err)
	}
	if !bytes.Equal(actual, expected) {
		return fmt.Errorf("%w (block %d): %w after ambiguous Type 2 ACK: expected %X, got %X",
			ErrTagWriteFailed, block, ErrWriteVerificationFailed, expected, actual)
	}

	Debugf("NTAG raw Type 2 write block %d verified after framing status", block)
	return nil
}
