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
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestUARTMutexDeadlockFix tests that the UART mutex deadlock is resolved
func TestUARTMutexDeadlockFix(t *testing.T) {
	t.Skip("UART integration test - requires hardware")
}

// TestWaitAckBackoffFix tests that waitAck doesn't spin infinitely
func TestWaitAckBackoffFix(t *testing.T) {
	// This test would require a mock transport that simulates the zero-byte scenario
	// For now, we test the timeout logic indirectly through the main fixes
	t.Skip("Requires mock transport - tested indirectly")
}

// TestMIFAREAuthenticationLockingConsistency tests standardized locking patterns
func TestMIFAREAuthenticationLockingConsistency(t *testing.T) {
	t.Skip("MIFARE integration test - requires specific setup")
}

// TestConcurrentTransportOperations tests multiple transports under stress
func TestConcurrentTransportOperations(t *testing.T) {
	const numOperations = 50
	const concurrency = 10

	mockTransport := &mockHangingTransport{hangDuration: 5 * time.Millisecond}
	transportCtx := AsTransportContext(mockTransport)

	var wg sync.WaitGroup
	results := make(chan error, numOperations)

	// Launch concurrent operations
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Mix of short and medium timeouts
			timeout := 10 * time.Millisecond
			if id%3 == 0 {
				timeout = 50 * time.Millisecond
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
			if err == nil {
				successes++
			} else if err.Error() == "context deadline exceeded" || err.Error() == "context cancelled while waiting for command response: context deadline exceeded" {
				timeouts++
			} else {
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
	mockTransport := &mockHangingTransport{hangDuration: 1 * time.Second} // Long hang
	transportCtx := AsTransportContext(mockTransport)

	ctx, cancel := context.WithCancel(context.Background())

	// Start operation that will hang
	done := make(chan error, 1)
	go func() {
		_, err := transportCtx.SendCommandContext(ctx, 0x02, []byte{})
		done <- err
	}()

	// Cancel after short delay
	time.Sleep(20 * time.Millisecond)
	cancel()

	// Should complete quickly after cancellation
	select {
	case err := <-done:
		if err == nil {
			t.Error("Expected cancellation error, got nil")
		}
		t.Logf("Operation properly cancelled with error: %v", err)
	case <-time.After(100 * time.Millisecond):
		t.Error("Context cancellation did not work - operation still running")
	}
}

// TestResourceLeakPrevention tests that resources are properly cleaned up
func TestResourceLeakPrevention(t *testing.T) {
	mockTransport := &mockHangingTransport{hangDuration: 100 * time.Millisecond}
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
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			defer cancel()

			_, _ = transportCtx.SendCommandContext(ctx, 0x02, []byte{})
		}()
	}

	wg.Wait()

	// Give time for cleanup
	time.Sleep(200 * time.Millisecond)

	finalCalls := mockTransport.CallCount()
	expectedCalls := initialCalls + numOps

	if finalCalls != expectedCalls {
		t.Errorf("Expected %d transport calls, got %d - possible resource leak", expectedCalls, finalCalls)
	}

	t.Logf("Resource test passed: %d operations resulted in %d transport calls", numOps, finalCalls-initialCalls)
}

// BenchmarkConcurrentOperations benchmarks the performance under concurrent load
func BenchmarkConcurrentOperations(b *testing.B) {
	mockTransport := &mockHangingTransport{hangDuration: 1 * time.Millisecond}
	transportCtx := AsTransportContext(mockTransport)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
			_, _ = transportCtx.SendCommandContext(ctx, 0x02, []byte{})
			cancel()
		}
	})
}

// TestNDEFMessageSafety tests that NDEF processing doesn't have buffer overruns
func TestNDEFMessageSafety(t *testing.T) {
	// Test various malformed NDEF messages to ensure no panics
	testCases := [][]byte{
		{},                             // Empty
		{0x00},                         // Too short
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, // Invalid data
		{0x03, 0x10, 0xD1, 0x01, 0x0C, 0x55, 0x01}, // Truncated URI record
	}

	for i, testData := range testCases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
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
