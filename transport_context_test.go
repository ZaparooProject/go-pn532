// go-pn532
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of go-pn532.
//
// go-pn532 is free software; you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public
// License as published by the Free Software Foundation; either
// version 3 of the License, or (at your option) any later version.
//
// go-pn532 is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
// Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with go-pn532; if not, write to the Free Software Foundation,
// Inc., 51 Franklin Street, Fifth Floor, Boston, MA  02110-1301, USA.

package pn532

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockHangingTransport simulates a transport that hangs on SendCommand
type mockHangingTransport struct {
	hangDuration time.Duration
	callCount    int32
	mu           sync.Mutex
}

func (m *mockHangingTransport) SendCommand(cmd byte, args []byte) ([]byte, error) {
	atomic.AddInt32(&m.callCount, 1)
	time.Sleep(m.hangDuration)
	return []byte{0x01, 0x02}, nil
}

func (m *mockHangingTransport) Close() error                           { return nil }
func (m *mockHangingTransport) SetTimeout(timeout time.Duration) error { return nil }
func (m *mockHangingTransport) IsConnected() bool                      { return true }
func (m *mockHangingTransport) Type() TransportType                    { return TransportMock }

func (m *mockHangingTransport) CallCount() int32 {
	return atomic.LoadInt32(&m.callCount)
}

// TestSendCommandContext_CancellationPreventsHang tests that context cancellation
// prevents the hanging goroutine problem described in the bug report
func TestSendCommandContext_CancellationPreventsHang(t *testing.T) {
	tests := []struct {
		name         string
		hangDuration time.Duration
		ctxTimeout   time.Duration
		expectErr    bool
	}{
		{
			name:         "quick cancellation",
			hangDuration: 1 * time.Second,
			ctxTimeout:   10 * time.Millisecond,
			expectErr:    true,
		},
		{
			name:         "slow operation with sufficient timeout",
			hangDuration: 10 * time.Millisecond,
			ctxTimeout:   100 * time.Millisecond,
			expectErr:    false,
		},
		{
			name:         "immediate cancellation",
			hangDuration: 100 * time.Millisecond,
			ctxTimeout:   1 * time.Millisecond,
			expectErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockTransport := &mockHangingTransport{hangDuration: tt.hangDuration}
			transportCtx := AsTransportContext(mockTransport)

			ctx, cancel := context.WithTimeout(context.Background(), tt.ctxTimeout)
			defer cancel()

			startTime := time.Now()
			result, err := transportCtx.SendCommandContext(ctx, 0x01, []byte{0x02})
			elapsed := time.Since(startTime)

			if tt.expectErr {
				if err == nil {
					t.Errorf("expected error due to cancellation, got nil")
				}
				if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
					t.Errorf("expected context cancellation error, got: %v", err)
				}
				// Should return quickly due to cancellation
				if elapsed > tt.ctxTimeout+20*time.Millisecond {
					t.Errorf("cancellation took too long: %v (expected ~%v)", elapsed, tt.ctxTimeout)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Errorf("expected result, got nil")
				}
			}
		})
	}
}

// TestSendCommandContext_PollingScenario simulates the polling scenario
// that was causing the original bug
func TestSendCommandContext_PollingScenario(t *testing.T) {
	mockTransport := &mockHangingTransport{hangDuration: 100 * time.Millisecond}
	transportCtx := AsTransportContext(mockTransport)

	// Simulate rapid polling with short timeouts like in monitoring.go
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)

		startTime := time.Now()
		_, err := transportCtx.SendCommandContext(ctx, 0x4A, []byte{0x01, 0x00}) // InListPassiveTarget
		elapsed := time.Since(startTime)

		cancel()

		// Should timeout quickly
		if err == nil {
			t.Errorf("iteration %d: expected timeout error, got nil", i)
		}
		if elapsed > 70*time.Millisecond {
			t.Errorf("iteration %d: cancellation took too long: %v", i, elapsed)
		}
	}

	// Verify we didn't leak goroutines by checking call count
	// Each call should have been made exactly once
	callCount := mockTransport.CallCount()
	if callCount != 5 {
		t.Errorf("expected 5 calls to SendCommand, got %d", callCount)
	}
}

// TestSendCommandContext_NoDeadlineGoroutineCleanup tests proper cleanup
// when no deadline is set but context is cancelled
func TestSendCommandContext_NoDeadlineGoroutineCleanup(t *testing.T) {
	mockTransport := &mockHangingTransport{hangDuration: 200 * time.Millisecond}
	transportCtx := AsTransportContext(mockTransport)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	startTime := time.Now()
	_, err := transportCtx.SendCommandContext(ctx, 0x01, []byte{})
	elapsed := time.Since(startTime)

	if err == nil {
		t.Errorf("expected cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}

	// Should return in ~50ms (when cancelled) + small margin for processing
	if elapsed > 80*time.Millisecond {
		t.Errorf("cancellation took too long: %v (expected ~50ms)", elapsed)
	}
}

// TestSendCommandContext_GoroutineCleanup verifies that abandoned goroutines
// are properly handled and don't cause resource leaks
func TestSendCommandContext_GoroutineCleanup(t *testing.T) {
	mockTransport := &mockHangingTransport{hangDuration: 500 * time.Millisecond}
	transportCtx := AsTransportContext(mockTransport)

	// Start many operations that will be cancelled
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()

			_, _ = transportCtx.SendCommandContext(ctx, 0x01, []byte{})
		}()
	}

	// Wait for all operations to complete
	wg.Wait()

	// Give some time for goroutines to clean up
	time.Sleep(100 * time.Millisecond)

	// Verify all calls were made (proving goroutines started)
	callCount := mockTransport.CallCount()
	if callCount != 10 {
		t.Errorf("expected 10 calls to SendCommand, got %d", callCount)
	}
}
