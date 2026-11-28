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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cardPresentReply is an InListPassiveTarget reply for an NTAG, as a PN532
// sends it.
var cardPresentReply = []byte{
	0x4B, 0x01, 0x01, 0x00, 0x44, 0x00, 0x07, 0x04, 0x7A, 0x7E, 0x0A, 0xC0, 0x1C, 0x91,
}

// cardReaderTransport answers InListPassiveTarget like a reader with a card
// that can be placed and lifted. With no card, a poll blocks until its
// context ends, as a PN532 waiting out its activation retries does.
type cardReaderTransport struct {
	pn532.Transport
	present atomic.Bool
}

func (c *cardReaderTransport) SendCommand(ctx context.Context, cmd byte, data []byte) ([]byte, error) {
	if cmd != cmdInListPassiveTarget {
		//nolint:wrapcheck // test passthrough
		return c.Transport.SendCommand(ctx, cmd, data)
	}
	if c.present.Load() {
		return cardPresentReply, nil
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func removalTestConfig() *Config {
	cfg := DefaultConfig()
	cfg.PollInterval = 50 * time.Millisecond
	cfg.CardRemovalTimeout = 200 * time.Millisecond
	cfg.SleepRecovery.Enabled = false
	return cfg
}

type cardReaderSession struct {
	session  *Session
	reader   *cardReaderTransport
	detected atomic.Int32
	removed  atomic.Int32
}

func newCardReaderSession(t *testing.T) *cardReaderSession {
	t.Helper()
	crs := &cardReaderSession{reader: &cardReaderTransport{Transport: pn532.NewMockTransport()}}
	crs.reader.present.Store(true)
	device, err := pn532.New(crs.reader)
	require.NoError(t, err)

	crs.session = NewSession(device, removalTestConfig())
	crs.session.SetOnCardDetected(func(context.Context, *pn532.DetectedTag) error {
		crs.detected.Add(1)
		return nil
	})
	crs.session.SetOnCardRemoved(func() { crs.removed.Add(1) })
	return crs
}

func startSession(t *testing.T, session *Session) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_ = session.Start(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-finished
		_ = session.Close()
	})
}

// A card left on the reader while polling stops, as when the system is too
// busy to send the next poll, must not be reported removed: no poll has
// missed it.
func TestSession_CardNotRemovedWhilePollingStalls(t *testing.T) {
	t.Parallel()
	crs := newCardReaderSession(t)
	session, reader, detected, removed := crs.session, crs.reader, &crs.detected, &crs.removed
	startSession(t, session)

	require.Eventually(t, func() bool { return detected.Load() == 1 }, 5*time.Second, 5*time.Millisecond)
	// Let the first read's grace period pass so the steady-state timer runs.
	time.Sleep(3 * session.config.CardRemovalTimeout)
	require.Zero(t, removed.Load())

	session.Pause()
	time.Sleep(5 * session.config.CardRemovalTimeout)
	assert.Zero(t, removed.Load(), "card reported removed while no poll was sent")

	session.Resume()
	time.Sleep(3 * session.config.CardRemovalTimeout)
	assert.Zero(t, removed.Load(), "card reported removed after polling resumed")
	assert.Equal(t, int32(1), detected.Load(), "card detected again")

	// Lifting the card is still reported.
	reader.present.Store(false)
	assert.Eventually(t, func() bool { return removed.Load() == 1 }, 5*time.Second, 5*time.Millisecond)
}

// Lifting a card is reported once a poll has gone unanswered for
// CardRemovalTimeout, without waiting for the poll to run out its retries.
func TestSession_CardRemovalStillPrompt(t *testing.T) {
	t.Parallel()
	crs := newCardReaderSession(t)
	session, reader, detected, removed := crs.session, crs.reader, &crs.detected, &crs.removed
	startSession(t, session)

	require.Eventually(t, func() bool { return detected.Load() == 1 }, 5*time.Second, 5*time.Millisecond)
	time.Sleep(3 * session.config.CardRemovalTimeout)
	require.Zero(t, removed.Load())

	lifted := time.Now()
	reader.present.Store(false)
	require.Eventually(t, func() bool { return removed.Load() == 1 }, 5*time.Second, time.Millisecond)

	// The poll in flight when the card is lifted may still find it, then the
	// next poll starts within PollInterval and must go unanswered for
	// CardRemovalTimeout, checked every removalRecheckInterval.
	limit := 2*session.config.PollInterval + session.config.CardRemovalTimeout + removalRecheckInterval
	assert.Less(t, time.Since(lifted), limit+100*time.Millisecond)
}

// sessionWithSeenCard returns a session whose card was last seen at seen,
// with the steady-state removal timer armed far in the future so the test
// drives onRemovalTimer itself.
func sessionWithSeenCard(t *testing.T, seen time.Time) (*Session, *atomic.Int32) {
	t.Helper()
	device, _ := createMockDeviceWithTransport(t)
	session := NewSession(device, removalTestConfig())
	var removed atomic.Int32
	session.SetOnCardRemoved(func() { removed.Add(1) })

	session.stateMutex.Lock()
	session.state.Present = true
	session.armRemovalTimer(func(callback func()) {
		session.state.TransitionToDetected(time.Hour, callback)
	})
	session.state.LastSeenTime = seen
	session.stateMutex.Unlock()
	t.Cleanup(func() { _ = session.Close() })
	return session, &removed
}

// startPoll marks a poll as begun at started.
func startPoll(session *Session, started time.Time) {
	session.stateMutex.Lock()
	session.pollStartedAt = started
	session.noReplyCheckedAt = started
	session.noReplyWaited = 0
	session.stateMutex.Unlock()
}

// fireRemovalTimer runs the armed removal timer's check as if the timer was
// armed armedFor ago and fired late after its due time.
func fireRemovalTimer(session *Session, armedFor, late time.Duration) {
	now := time.Now()
	session.stateMutex.Lock()
	seq := session.state.removalSeq
	session.state.removalArmedAt = now.Add(-armedFor)
	session.state.removalDue = now.Add(-late)
	session.stateMutex.Unlock()
	session.onRemovalTimer(&seq)
}

func noReplyWaited(session *Session) time.Duration {
	session.stateMutex.RLock()
	defer session.stateMutex.RUnlock()
	return session.noReplyWaited
}

// The removal timer waits while nothing shows the card has gone.
func TestSession_RemovalTimerWaits(t *testing.T) {
	t.Parallel()

	t.Run("no poll since the card was seen", func(t *testing.T) {
		t.Parallel()
		// The process stalled for seconds and the timer fired late, before
		// the next poll was sent.
		session, removed := sessionWithSeenCard(t, time.Now().Add(-4*time.Second))
		fireRemovalTimer(session, 5*time.Second, 4*time.Second)
		assert.Zero(t, removed.Load())
		session.stateMutex.RLock()
		defer session.stateMutex.RUnlock()
		assert.True(t, session.state.Present)
		assert.NotNil(t, session.state.RemovalTimer, "timer armed again")
	})

	t.Run("poll sent moments ago", func(t *testing.T) {
		t.Parallel()
		// The poll went out late and the timer fired 38ms into it.
		now := time.Now()
		session, removed := sessionWithSeenCard(t, now.Add(-600*time.Millisecond))
		startPoll(session, now.Add(-38*time.Millisecond))

		fireRemovalTimer(session, 600*time.Millisecond, 0)
		assert.Zero(t, removed.Load())
		assert.Less(t, noReplyWaited(session), session.config.CardRemovalTimeout)
	})

	t.Run("reply arrived but not handled yet", func(t *testing.T) {
		t.Parallel()
		session, removed := sessionWithSeenCard(t, time.Now().Add(-time.Second))
		startPoll(session, time.Now().Add(-time.Second+time.Millisecond))

		// The reader answers, then the session stalls before it handles the
		// answer, longer than CardRemovalTimeout.
		_, err := session.device.InListPassiveTarget(context.Background(), 0x00)
		require.NoError(t, err)

		fireRemovalTimer(session, time.Second, 0)
		assert.Zero(t, removed.Load())
	})

	t.Run("wait during a stall does not count", func(t *testing.T) {
		t.Parallel()
		// The process stalled with the poll in flight. The timer fires late,
		// and the poll's answer may be waiting unread.
		now := time.Now()
		session, removed := sessionWithSeenCard(t, now.Add(-5*time.Second))
		startPoll(session, now.Add(-4*time.Second))

		fireRemovalTimer(session, 4*time.Second, 3*time.Second)
		assert.Zero(t, removed.Load())
		assert.Zero(t, noReplyWaited(session))

		// The next check is on time, but only the time since the stall ended
		// counts.
		fireRemovalTimer(session, 0, 0)
		assert.Zero(t, removed.Load())
		assert.Less(t, noReplyWaited(session), session.config.CardRemovalTimeout)
	})

	t.Run("stall while the timer was checking does not count", func(t *testing.T) {
		t.Parallel()
		// A check ran, then the process stalled for 2s before the timer was
		// armed again. The next check fires on time 100ms later.
		now := time.Now()
		session, removed := sessionWithSeenCard(t, now.Add(-3*time.Second))
		startPoll(session, now.Add(-2500*time.Millisecond))
		session.stateMutex.Lock()
		session.noReplyCheckedAt = now.Add(-2100 * time.Millisecond)
		session.stateMutex.Unlock()

		fireRemovalTimer(session, removalRecheckInterval, 0)
		assert.Zero(t, removed.Load())
		assert.Less(t, noReplyWaited(session), 2*removalRecheckInterval)
	})
}

// The removal timer reports the card removed once a poll has missed it.
func TestSession_RemovalTimerRemoves(t *testing.T) {
	t.Parallel()

	t.Run("poll unanswered for CardRemovalTimeout", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		session, removed := sessionWithSeenCard(t, now.Add(-time.Second))
		startPoll(session, now.Add(-session.config.CardRemovalTimeout))

		fireRemovalTimer(session, time.Second, 0)
		assert.Equal(t, int32(1), removed.Load())
	})

	t.Run("unanswered time adds up across checks", func(t *testing.T) {
		t.Parallel()
		session, removed := sessionWithSeenCard(t, time.Now().Add(-time.Second))
		startPoll(session, time.Now())

		deadline := time.Now().Add(5 * time.Second)
		for removed.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(removalRecheckInterval / 2)
			fireRemovalTimer(session, removalRecheckInterval/2, 0)
		}
		assert.Equal(t, int32(1), removed.Load())
	})

	t.Run("poll since the card was seen missed it", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		session, removed := sessionWithSeenCard(t, now.Add(-time.Second))
		session.stateMutex.Lock()
		session.lastMissAt = now.Add(-time.Millisecond)
		session.stateMutex.Unlock()

		fireRemovalTimer(session, time.Second, 0)
		assert.Equal(t, int32(1), removed.Load())
	})
}

// Timers that cannot show the card has gone leave it alone.
func TestSession_RemovalTimerIgnored(t *testing.T) {
	t.Parallel()

	t.Run("miss before the card was seen", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		session, removed := sessionWithSeenCard(t, now.Add(-time.Second))
		session.stateMutex.Lock()
		session.lastMissAt = now.Add(-2 * time.Second)
		session.stateMutex.Unlock()

		fireRemovalTimer(session, time.Second, 0)
		assert.Zero(t, removed.Load())
	})

	t.Run("stale timer does nothing", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		session, removed := sessionWithSeenCard(t, now.Add(-time.Second))
		session.stateMutex.Lock()
		session.lastMissAt = now
		stale := session.state.removalSeq - 1
		timer := session.state.RemovalTimer
		session.stateMutex.Unlock()

		session.onRemovalTimer(&stale)
		assert.Zero(t, removed.Load())
		session.stateMutex.RLock()
		defer session.stateMutex.RUnlock()
		assert.Same(t, timer, session.state.RemovalTimer, "stale callback must not re-arm")
	})
}
