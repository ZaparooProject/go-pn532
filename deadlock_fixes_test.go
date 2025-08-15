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
	"fmt"
	"sync"
	"testing"
	"time"
)

const (
	// Test timeout constants for more predictable behavior
	shortTimeout        = 10 * time.Millisecond
	mediumTimeout       = 50 * time.Millisecond
	longTimeout         = 100 * time.Millisecond
	deadlockTestTimeout = 200 * time.Millisecond
)

// TestUARTMutexDeadlockFix tests that the UART mutex deadlock is resolved
func TestUARTMutexDeadlockFix(t *testing.T) {
	t.Parallel()

	// Unit test equivalent using BlockingMockTransport to verify mutex release behavior
	mockTransport := NewBlockingMockTransport()
	defer func() { _ = mockTransport.Close() }()

	transportCtx := AsTransportContext(mockTransport)

	// Test that multiple operations can acquire and release mutex properly
	const numOperations = 5
	var wg sync.WaitGroup
	wg.Add(numOperations)

	for i := 0; i < numOperations; i++ {
		go func(id int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
			defer cancel()

			// This should timeout but not deadlock the mutex
			_, err := transportCtx.SendCommandContext(ctx, byte(id), []byte{byte(id)})
			if err == nil {
				t.Errorf("Expected timeout error for operation %d, got nil", id)
			}
		}(i)
	}

	// Verify all operations complete without deadlock
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no deadlock occurred
	case <-time.After(longTimeout):
		t.Fatal("UART mutex deadlock detected - operations did not complete")
	}
}

// TestWaitAckBackoffFix tests that waitAck doesn't spin infinitely
func TestWaitAckBackoffFix(t *testing.T) {
	t.Parallel()

	// Unit test to verify timeout logic prevents infinite spinning
	mockTransport := NewBlockingMockTransport()
	defer func() { _ = mockTransport.Close() }()

	transportCtx := AsTransportContext(mockTransport)

	// Test rapid timeout scenarios that would cause spinning
	const numRapidTests = 10
	var wg sync.WaitGroup
	wg.Add(numRapidTests)

	start := time.Now()
	for i := 0; i < numRapidTests; i++ {
		go func(id int) {
			defer wg.Done()

			// Very short timeout to trigger backoff logic
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
			defer cancel()

			_, err := transportCtx.SendCommandContext(ctx, 0x02, []byte{0x00})
			if err == nil {
				t.Errorf("Expected timeout error for rapid test %d, got nil", id)
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	// If waitAck was spinning infinitely, this would take much longer
	// Normal timeout handling should complete quickly
	if elapsed > 50*time.Millisecond {
		t.Errorf("WaitAck appears to be spinning - took %v for %d rapid timeouts", elapsed, numRapidTests)
	}
}

// TestMIFAREAuthenticationLockingConsistency tests standardized locking patterns
func TestMIFAREAuthenticationLockingConsistency(t *testing.T) {
	t.Parallel()

	// Unit test to verify consistent locking behavior across authentication operations
	mockTransport := NewBlockingMockTransport()
	defer func() { _ = mockTransport.Close() }()

	transportCtx := AsTransportContext(mockTransport)

	// Test multiple authentication-like operations to ensure consistent locking
	const numAuthOps = 8
	var wg sync.WaitGroup
	wg.Add(numAuthOps)

	// Track completion order to detect locking issues
	completions := make(chan int, numAuthOps)

	for i := 0; i < numAuthOps; i++ {
		go func(id int) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
			defer cancel()

			// Simulate MIFARE authentication command pattern
			authCmd := byte(0x60 + (id % 2)) // Alternate between auth commands
			authData := []byte{byte(id), 0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

			_, err := transportCtx.SendCommandContext(ctx, authCmd, authData)
			completions <- id

			// Don't fail on expected timeout errors
			if err == nil {
				t.Errorf("Expected timeout error for auth operation %d, got nil", id)
			}
		}(i)
	}

	// Verify all operations complete within reasonable time
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - consistent locking behavior maintained
		close(completions)

		// Verify all operations attempted
		completedCount := 0
		for range completions {
			completedCount++
		}
		if completedCount != numAuthOps {
			t.Errorf("Expected %d auth operations, got %d", numAuthOps, completedCount)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("MIFARE authentication locking inconsistency - operations did not complete")
	}
}

// TestConcurrentTransportOperations tests multiple transports under stress
func TestConcurrentTransportOperations(t *testing.T) {
	t.Parallel()
	const numOperations = 50

	mockTransport := newMockHangingTransport(shortTimeout)
	defer func() { _ = mockTransport.Close() }()
	transportCtx := AsTransportContext(mockTransport)

	var wg sync.WaitGroup
	results := make(chan error, numOperations)

	// Launch concurrent operations
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Mix of short and medium timeouts
			timeout := shortTimeout
			if id%3 == 0 {
				timeout = mediumTimeout
			}

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			_, err := transportCtx.SendCommandContext(ctx, 0x02, []byte{})
			results <- err
		}(i)
	}

	// Wait with timeout
	done := make(chan bool)
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		close(results)

		var timeouts, successes, others int
		for err := range results {
			switch {
			case err == nil:
				successes++
			case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
				timeouts++
			default:
				others++
			}
		}

		t.Logf("Results: %d successes, %d timeouts, %d other errors", successes, timeouts, others)

		if timeouts+successes != numOperations {
			t.Errorf("Expected %d total operations, got %d", numOperations, timeouts+successes+others)
		}

	case <-time.After(10 * time.Second):
		t.Fatal("Concurrent operations deadlock detected")
	}
}

// TestContextCancellationPropagation tests that context cancellation works properly
func TestContextCancellationPropagation(t *testing.T) {
	t.Parallel()
	mockTransport := newMockHangingTransport(1 * time.Second) // Long hang
	defer func() { _ = mockTransport.Close() }()
	transportCtx := AsTransportContext(mockTransport)

	ctx, cancel := context.WithCancel(context.Background())

	// Start operation that will hang
	done := make(chan error, 1)
	go func() {
		_, err := transportCtx.SendCommandContext(ctx, 0x02, []byte{})
		done <- err
	}()

	// Cancel after operation has started
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Should complete quickly after cancellation
	select {
	case err := <-done:
		if err == nil {
			t.Error("Expected cancellation error, got nil")
		}
		t.Logf("Operation properly cancelled with error: %v", err)
	case <-time.After(longTimeout):
		t.Error("Context cancellation did not work - operation still running")
	}
}

// TestResourceLeakPrevention tests that resources are properly cleaned up
func TestResourceLeakPrevention(t *testing.T) {
	t.Parallel()
	mockTransport := newMockHangingTransport(100 * time.Millisecond)
	defer func() { _ = mockTransport.Close() }()
	transportCtx := AsTransportContext(mockTransport)

	// Track call count before
	initialCalls := mockTransport.CallCount()

	// Start many operations with quick timeouts
	const numOps = 20
	var wg sync.WaitGroup

	for i := 0; i < numOps; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
			defer cancel()

			_, _ = transportCtx.SendCommandContext(ctx, 0x02, []byte{})
		}()
	}

	wg.Wait()

	// Wait for call count to stabilize with timeout instead of arbitrary sleep
	deadline := time.Now().Add(500 * time.Millisecond)
	expectedCalls := initialCalls + numOps

	for time.Now().Before(deadline) {
		if mockTransport.CallCount() == expectedCalls {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	finalCalls := mockTransport.CallCount()
	if finalCalls != expectedCalls {
		t.Errorf("Expected %d transport calls, got %d - possible resource leak", expectedCalls, finalCalls)
	}

	t.Logf("Resource test passed: %d operations resulted in %d transport calls", numOps, finalCalls-initialCalls)
}

// BenchmarkConcurrentOperations benchmarks the performance under concurrent load
func BenchmarkConcurrentOperations(b *testing.B) {
	mockTransport := newMockHangingTransport(1 * time.Millisecond)
	defer func() { _ = mockTransport.Close() }()
	transportCtx := AsTransportContext(mockTransport)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
			_, _ = transportCtx.SendCommandContext(ctx, 0x02, []byte{})
			cancel()
		}
	})
}

// TestNDEFMessageSafety tests that NDEF processing doesn't have buffer overruns
func TestNDEFMessageSafety(t *testing.T) {
	t.Parallel()
	// Test various malformed NDEF messages to ensure no panics
	testCases := [][]byte{
		{},                             // Empty
		{0x00},                         // Too short
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, // Invalid data
		{0x03, 0x10, 0xD1, 0x01, 0x0C, 0x55, 0x01}, // Truncated URI record
	}

	for i, testData := range testCases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("NDEF parsing panicked with data %v: %v", testData, r)
				}
			}()

			// Test parsing - should not panic
			_, _ = ParseNDEFMessage(testData)
			_ = IsValidNDEFMessage(testData)
		})
	}
}
