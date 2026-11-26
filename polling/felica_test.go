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

package polling

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/internal/syncutil"
	testutil "github.com/ZaparooProject/go-pn532/internal/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	cmdRFConfiguration     byte = 0x32
	cmdInListPassiveTarget byte = 0x4A
)

type sentCommand struct {
	data []byte
	cmd  byte
}

// recordingTransport records every command a session sends, and can fail
// the FeliCa poll as a reader without FeliCa support does.
type recordingTransport struct {
	pn532.Transport
	feliCaErr error
	sent      []sentCommand
	mu        syncutil.Mutex
}

func (r *recordingTransport) SendCommand(ctx context.Context, cmd byte, data []byte) ([]byte, error) {
	r.mu.Lock()
	r.sent = append(r.sent, sentCommand{cmd: cmd, data: append([]byte(nil), data...)})
	feliCaErr := r.feliCaErr
	r.mu.Unlock()

	if cmd == cmdInListPassiveTarget && len(data) > 1 && data[1] == 0x01 && feliCaErr != nil {
		return nil, feliCaErr
	}
	//nolint:wrapcheck // test passthrough
	return r.Transport.SendCommand(ctx, cmd, data)
}

func (r *recordingTransport) commands(cmd byte) [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out [][]byte
	for _, c := range r.sent {
		if c.cmd == cmd {
			out = append(out, c.data)
		}
	}
	return out
}

type simTransport struct {
	*testutil.SimulatorTransport
}

func (*simTransport) Type() pn532.TransportType {
	return pn532.TransportMock
}

func newSimDevice(t *testing.T, tags ...*testutil.VirtualTag) (*pn532.Device, *recordingTransport) {
	t.Helper()
	sim := testutil.NewVirtualPN532()
	for _, tag := range tags {
		sim.AddTag(tag)
	}
	rec := &recordingTransport{Transport: &simTransport{testutil.NewSimulatorTransport(sim)}}
	device, err := pn532.New(rec)
	require.NoError(t, err)
	return device, rec
}

func newMockDevice(t *testing.T) (*pn532.Device, *recordingTransport) {
	t.Helper()
	mock := pn532.NewMockTransport()
	mock.SetResponse(cmdInListPassiveTarget, []byte{0x4B, 0x00})
	rec := &recordingTransport{Transport: mock}
	device, err := pn532.New(rec)
	require.NoError(t, err)
	return device, rec
}

func feliCaTestConfig(feliCa bool) *Config {
	cfg := DefaultConfig()
	cfg.PollInterval = 5 * time.Millisecond
	cfg.SleepRecovery.Enabled = false
	cfg.FeliCa = feliCa
	return cfg
}

// runSession polls until done returns true or the test times out, then stops
// the session and waits for it to end.
func runSession(t *testing.T, session *Session, done func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = session.Start(ctx)
	}()
	assert.Eventually(t, done, 5*time.Second, 5*time.Millisecond)
	cancel()
	<-finished
	_ = session.Close()
}

func TestDefaultConfig_FeliCaOff(t *testing.T) {
	t.Parallel()
	assert.False(t, DefaultConfig().FeliCa)
}

// With FeliCa off, a session sends exactly the commands it always has:
// activation retries left at 0xFF and only the ISO14443A poll.
func TestSession_FeliCaOff_PollingUnchanged(t *testing.T) {
	t.Parallel()
	device, rec := newMockDevice(t)
	session := NewSession(device, feliCaTestConfig(false))

	runSession(t, session, func() bool {
		return len(rec.commands(cmdInListPassiveTarget)) >= 5
	})

	assert.Equal(t, [][]byte{{0x05, 0x20, 0x01, 0xFF}}, rec.commands(cmdRFConfiguration))
	for _, data := range rec.commands(cmdInListPassiveTarget) {
		assert.Equal(t, []byte{0x01, 0x00}, data)
	}
}

func TestSession_FeliCaOn_DetectsFeliCaCard(t *testing.T) {
	t.Parallel()
	idm := []byte{0x01, 0x2E, 0x4C, 0xD3, 0x8A, 0x1B, 0x66, 0x07}
	device, rec := newSimDevice(t, testutil.NewVirtualFeliCa(idm))
	session := NewSession(device, feliCaTestConfig(true))

	var detected atomic.Pointer[pn532.DetectedTag]
	session.SetOnCardDetected(func(_ context.Context, tag *pn532.DetectedTag) error {
		detected.CompareAndSwap(nil, tag)
		return nil
	})

	runSession(t, session, func() bool { return detected.Load() != nil })

	tag := detected.Load()
	require.NotNil(t, tag)
	assert.Equal(t, pn532.TagTypeFeliCa, tag.Type)
	assert.Equal(t, "012e4cd38a1b6607", tag.UID)
	assert.Equal(t, idm, tag.UIDBytes)

	assert.Equal(t, []byte{0x05, 0x20, 0x01, 0x02}, rec.commands(cmdRFConfiguration)[0])
	polls := rec.commands(cmdInListPassiveTarget)
	// Each cycle is the ISO14443A poll, then the FeliCa poll. More cycles can
	// run before the session stops.
	require.GreaterOrEqual(t, len(polls), 2)
	assert.Equal(t, []byte{0x01, 0x00}, polls[0])
	assert.Equal(t, []byte{0x01, 0x01, 0x00, 0xFF, 0xFF, 0x01, 0x00}, polls[1])
}

// An ISO14443A tag is found by the first poll, so no FeliCa poll is sent.
func TestSession_FeliCaOn_TypeATagSkipsFeliCaPoll(t *testing.T) {
	t.Parallel()
	device, rec := newSimDevice(t, testutil.NewVirtualNTAG213(nil))
	session := NewSession(device, feliCaTestConfig(true))

	var detected atomic.Pointer[pn532.DetectedTag]
	session.SetOnCardDetected(func(_ context.Context, tag *pn532.DetectedTag) error {
		detected.CompareAndSwap(nil, tag)
		return nil
	})

	runSession(t, session, func() bool { return detected.Load() != nil })

	assert.Equal(t, pn532.TagTypeNTAG, detected.Load().Type)
	for _, data := range rec.commands(cmdInListPassiveTarget) {
		assert.Equal(t, []byte{0x01, 0x00}, data)
	}
}

// A reader that keeps failing the FeliCa poll has it turned off for the
// session, once, while ISO14443A polling carries on.
func TestSession_FeliCaOn_ReaderRejectsFeliCa(t *testing.T) {
	t.Parallel()
	device, rec := newMockDevice(t)
	rec.feliCaErr = pn532.ErrCommandNotSupported
	session := NewSession(device, feliCaTestConfig(true))

	var unavailable atomic.Int32
	var lastErr atomic.Pointer[error]
	session.SetOnFeliCaUnavailable(func(err error) {
		unavailable.Add(1)
		lastErr.Store(&err)
	})

	feliCaPolls := func() int {
		count := 0
		for _, data := range rec.commands(cmdInListPassiveTarget) {
			if data[1] == 0x01 {
				count++
			}
		}
		return count
	}

	runSession(t, session, func() bool {
		return unavailable.Load() == 1 && len(rec.commands(cmdInListPassiveTarget)) >= maxFeliCaFailures*2+5
	})

	assert.Equal(t, int32(1), unavailable.Load())
	require.NotNil(t, lastErr.Load())
	require.ErrorIs(t, *lastErr.Load(), pn532.ErrCommandNotSupported)
	assert.Equal(t, maxFeliCaFailures, feliCaPolls())
}
