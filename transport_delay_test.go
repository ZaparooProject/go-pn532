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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAwaitDelay_PrefersCancellationWhenBothAreReady(t *testing.T) {
	t.Parallel()

	// Regression test: when a goroutine is descheduled past both the simulated
	// delay and the context deadline, select sees both cases ready and picks
	// one at random. Taking the delay branch made a cancelled SendCommand
	// return a response, which failed TestDiagnoseCancellation and its siblings
	// intermittently on loaded CI runners.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	// select picks uniformly at random among ready cases, so one call proves
	// nothing: a delay branch that ignored the context passed half the time.
	// Repeating makes the old behaviour a certain failure.
	for range 1000 {
		delay := make(chan time.Time, 1)
		delay <- time.Now()

		err := awaitDelay(ctx, delay)

		require.Error(t, err, "a cancelled context must report an error even when the delay has elapsed")
		require.ErrorIs(t, err, context.DeadlineExceeded)
	}
}

func TestAwaitDelay_ReturnsNilWhenDelayElapsesFirst(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	delay := make(chan time.Time, 1)
	delay <- time.Now()

	assert.NoError(t, awaitDelay(ctx, delay))
}

func TestAwaitDelay_ReturnsCancellationWhileDelayPending(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := awaitDelay(ctx, make(chan time.Time))

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
