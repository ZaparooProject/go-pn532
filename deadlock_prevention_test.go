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
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	defer goleak.VerifyTestMain(m)
	m.Run()
}

// TestMutexReleaseOnTransportFailure verifies that transport mutexes are properly
// released when operations fail, preventing deadlocks
func TestMutexReleaseOnTransportFailure(t *testing.T) {
	t.Parallel()

	mockTransport := NewBlockingMockTransport()
	// Set a short timeout to prevent long goroutine cleanup waits
	_ = mockTransport.SetTimeout(20 * time.Millisecond)
	defer func() { _ = mockTransport.Close() }()

	transportCtx := AsTransportContext(mockTransport)

	const numGoroutines = 3

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start multiple operations that will be cancelled quickly
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()

			// This should timeout quickly and not deadlock
			_, err := transportCtx.SendCommandContext(ctx, 0x01, []byte{0x02})
			if err == nil {
				t.Error("Expected timeout error, got nil")
			}
		}()
	}

	// Verify all operations complete without deadlock
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Deadlock detected - operations did not complete")
	}
}

// TestContextCancellationDuringBlockedOperation verifies that context cancellation
// properly terminates operations and cleans up goroutines
func TestContextCancellationDuringBlockedOperation(t *testing.T) {
	t.Parallel()

	mockTransport := NewBlockingMockTransport()
	// Set a short timeout to prevent long goroutine cleanup waits
	_ = mockTransport.SetTimeout(20 * time.Millisecond)
	defer func() { _ = mockTransport.Close() }()

	transportCtx := AsTransportContext(mockTransport)

	ctx, cancel := context.WithCancel(context.Background())

	// Start an operation that will block
	done := make(chan error, 1)
	go func() {
		_, err := transportCtx.SendCommandContext(ctx, 0x01, []byte{0x02})
		done <- err
	}()

	// Give the operation time to start
	time.Sleep(10 * time.Millisecond)

	// Cancel the context
	cancel()

	// Verify the operation is cancelled promptly
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Expected cancellation error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context cancellation error, got: %v", err)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Operation did not respond to context cancellation")
	}
}

// TestConcurrentTransportAccess verifies that multiple goroutines can safely
// access the same transport without causing deadlocks
func TestConcurrentTransportAccess(t *testing.T) {
	t.Parallel()

	mockTransport := NewBlockingMockTransport()
	// Set a short timeout to prevent long goroutine cleanup waits
	_ = mockTransport.SetTimeout(50 * time.Millisecond)
	defer func() { _ = mockTransport.Close() }()

	transportCtx := AsTransportContext(mockTransport)

	const numGoroutines = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Start multiple concurrent operations
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()

			// Randomly unblock some operations to create a mix of success/failure
			if id%3 == 0 {
				go func() {
					time.Sleep(5 * time.Millisecond)
					mockTransport.Unblock()
				}()
			}

			_, err := transportCtx.SendCommandContext(ctx, byte(id), []byte{byte(id)})
			// We expect timeout/cancellation errors but not deadlocks
			if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				t.Errorf("Unexpected error type: %v", err)
			}
		}(i)
	}

	// Verify all operations complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Deadlock detected during concurrent access")
	}
}

// setupEchoMockTransport creates a mock transport that echoes commands for testing
func setupEchoMockTransport() *BlockingMockTransport {
	transport := NewBlockingMockTransportWithFunc(func(cmd byte, data []byte) ([]byte, error) {
		// Echo back the command and data for verification
		result := append([]byte{cmd}, data...)
		return result, nil
	})
	// Set a short timeout to prevent long goroutine cleanup waits
	_ = transport.SetTimeout(20 * time.Millisecond)
	return transport
}

// testImmediateCancellation tests that already-cancelled contexts are handled properly
func testImmediateCancellation(t *testing.T, transportCtx TransportContext) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := transportCtx.SendCommandContext(ctx, 0x01, []byte{0x02})
	if err == nil {
		t.Fatal("Expected cancellation error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
}

// testCancellationDuringBlocking tests that operations respond to cancellation while blocked
func testCancellationDuringBlocking(t *testing.T, transportCtx TransportContext) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	start := time.Now()
	done := make(chan error, 1)

	go func() {
		_, err := transportCtx.SendCommandContext(ctx, 0x03, []byte{0x04})
		done <- err
	}()

	// Let the operation start and block
	time.Sleep(5 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if err == nil {
			t.Fatal("Expected cancellation error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled, got: %v", err)
		}
		// Should respond quickly to cancellation, not wait for transport timeout
		if elapsed > 50*time.Millisecond {
			t.Errorf("Cancellation took too long: %v", elapsed)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Operation did not respond to context cancellation")
	}
}

// testSuccessfulOperation tests that operations complete successfully when unblocked
func testSuccessfulOperation(t *testing.T, transportCtx TransportContext, mockTransport *BlockingMockTransport) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start operation and unblock it
	done := make(chan struct {
		err  error
		data []byte
	}, 1)

	go func() {
		data, err := transportCtx.SendCommandContext(ctx, 0x05, []byte{0x06, 0x07})
		done <- struct {
			err  error
			data []byte
		}{err, data}
	}()

	// Unblock after a short delay
	time.Sleep(5 * time.Millisecond)
	mockTransport.Unblock()

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("Expected success, got error: %v", result.err)
		}
		// Verify the echo response
		expected := []byte{0x05, 0x06, 0x07}
		if !bytes.Equal(result.data, expected) {
			t.Errorf("Expected response %v, got %v", expected, result.data)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("Operation did not complete")
	}
}

// TestSendCommandContextCancellationBehavior explicitly verifies that SendCommandContext
// respects context cancellation and doesn't rely on transport timeouts
func TestSendCommandContextCancellationBehavior(t *testing.T) {
	t.Parallel()

	mockTransport := setupEchoMockTransport()
	t.Cleanup(func() { _ = mockTransport.Close() })

	transportCtx := AsTransportContext(mockTransport)

	// Test immediate cancellation
	t.Run("ImmediateCancellation", func(t *testing.T) {
		t.Parallel()
		testImmediateCancellation(t, transportCtx)
	})

	// Test cancellation during blocked operation
	t.Run("CancellationDuringBlocking", func(t *testing.T) {
		t.Parallel()
		testCancellationDuringBlocking(t, transportCtx)
	})

	// Test successful operation after unblocking
	t.Run("SuccessfulOperation", func(t *testing.T) {
		t.Parallel()
		testSuccessfulOperation(t, transportCtx, mockTransport)
	})
}

// TestRandomContextCancellation is a simple chaos test that randomly cancels
// contexts during operations to detect hanging goroutines
func TestRandomContextCancellation(t *testing.T) {
	t.Parallel()

	mockTransport := NewBlockingMockTransport()
	// Set a short timeout to prevent long goroutine cleanup waits
	_ = mockTransport.SetTimeout(30 * time.Millisecond)
	defer func() { _ = mockTransport.Close() }()

	transportCtx := AsTransportContext(mockTransport)

	const numOperations = 20

	var wg sync.WaitGroup
	wg.Add(numOperations)

	for i := 0; i < numOperations; i++ {
		go func(id int) {
			defer wg.Done()

			// Random timeout between 1-50ms
			timeout := time.Duration(1+id%50) * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			// Randomly unblock some operations
			if id%5 == 0 {
				go func() {
					time.Sleep(time.Duration(id%10) * time.Millisecond)
					mockTransport.Unblock()
				}()
			}

			_, err := transportCtx.SendCommandContext(ctx, byte(id), []byte{byte(id)})
			// Allow timeout/cancellation errors in random test
			_ = err
		}(i)
	}

	// All operations should complete within reasonable time
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Some operations did not complete - possible goroutine leak")
	}
}
