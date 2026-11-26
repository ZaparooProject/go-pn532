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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testIDm = []byte{0x01, 0x2E, 0x4C, 0xD3, 0x8A, 0x1B, 0x66, 0x07}
	testPMm = []byte{0x00, 0xF1, 0x00, 0x00, 0x00, 0x01, 0x43, 0x00}
)

func feliCaReply(polResLen, code byte, tail ...byte) []byte {
	res := []byte{0x4B, 0x01, 0x01, polResLen, code}
	res = append(res, testIDm...)
	res = append(res, testPMm...)
	return append(res, tail...)
}

func TestParseFeliCaTargetResponse(t *testing.T) {
	t.Parallel()

	t.Run("no target", func(t *testing.T) {
		t.Parallel()
		tag, err := parseFeliCaTargetResponse([]byte{0x4B, 0x00})
		require.NoError(t, err)
		assert.Nil(t, tag)
	})

	t.Run("with system code", func(t *testing.T) {
		t.Parallel()
		tag, err := parseFeliCaTargetResponse(feliCaReply(20, 0x01, 0x88, 0xB4))
		require.NoError(t, err)
		require.NotNil(t, tag)
		assert.Equal(t, TagTypeFeliCa, tag.Type)
		assert.Equal(t, "012e4cd38a1b6607", tag.UID)
		assert.Equal(t, testIDm, tag.UIDBytes)

		// TargetData is what NewFeliCaTag reads.
		felica, err := NewFeliCaTag(nil, tag.TargetData)
		require.NoError(t, err)
		assert.Equal(t, testIDm, felica.GetIDm())
		assert.Equal(t, testPMm, felica.GetPMm())
		assert.Equal(t, uint16(0x88B4), felica.GetSystemCode())
	})

	t.Run("without system code", func(t *testing.T) {
		t.Parallel()
		tag, err := parseFeliCaTargetResponse(feliCaReply(18, 0x01))
		require.NoError(t, err)
		require.NotNil(t, tag)
		assert.Equal(t, testIDm, tag.UIDBytes)
	})

	t.Run("truncated", func(t *testing.T) {
		t.Parallel()
		_, err := parseFeliCaTargetResponse(feliCaReply(20, 0x01))
		require.Error(t, err)
		_, err = parseFeliCaTargetResponse([]byte{0x4B, 0x01, 0x01})
		require.Error(t, err)
	})

	t.Run("short length byte", func(t *testing.T) {
		t.Parallel()
		_, err := parseFeliCaTargetResponse(feliCaReply(4, 0x01))
		require.Error(t, err)
	})

	t.Run("wrong response code", func(t *testing.T) {
		t.Parallel()
		_, err := parseFeliCaTargetResponse(feliCaReply(18, 0x07))
		require.Error(t, err)
	})
}

func TestInListPassiveFeliCaTarget_SendsPollingRequest(t *testing.T) {
	t.Parallel()
	mock := NewMockTransport()
	mock.SetResponse(cmdInListPassiveTarget, feliCaReply(18, 0x01))
	device, err := New(mock)
	require.NoError(t, err)

	tag, err := device.InListPassiveFeliCaTarget(context.Background())
	require.NoError(t, err)
	require.NotNil(t, tag)
	assert.Equal(t, TagTypeFeliCa, tag.Type)
	assert.Equal(t, 1, mock.GetCallCount(cmdInListPassiveTarget))
}

func TestSetPollingRetries_KeepsInfiniteActivation(t *testing.T) {
	t.Parallel()
	mock := NewMockTransport()
	device, err := New(mock)
	require.NoError(t, err)
	require.NoError(t, device.SetPollingRetries(context.Background(), 0x20))
	require.NoError(t, device.SetPollingRetriesWithActivation(context.Background(), 0x20, 0x02))
	assert.Equal(t, 2, mock.GetCallCount(cmdRFConfiguration))
}
