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

package polling

import (
	"context"
	"errors"
	"testing"
	"time"

	pn532 "github.com/ZaparooProject/go-pn532"
)

// createMockDeviceWithTransportForBench creates a device with mock transport for benchmarks
func createMockDeviceWithTransportForBench(b *testing.B) (*pn532.Device, *pn532.MockTransport) {
	mockTransport := pn532.NewMockTransport()
	device, err := pn532.New(mockTransport)
	if err != nil {
		b.Fatalf("Failed to create device: %v", err)
	}
	return device, mockTransport
}

// BenchmarkSession_TraditionalVsActor compares performance between traditional and actor-based sessions
func BenchmarkSession_TraditionalVsActor(b *testing.B) {
	b.Run("Traditional_Session", func(b *testing.B) {
		benchmarkTraditionalSession(b)
	})

	b.Run("Actor_Based_Session", func(b *testing.B) {
		benchmarkActorBasedSession(b)
	})
}

// benchmarkTraditionalSession measures traditional Session.Start() performance
func benchmarkTraditionalSession(b *testing.B) {
	benchmarkSessionImpl(b, NewSession)
}

// benchmarkActorBasedSession measures actor-based Session.Start() performance
func benchmarkActorBasedSession(b *testing.B) {
	benchmarkSessionImpl(b, NewActorBasedSession)
}

// benchmarkSessionImpl contains the shared benchmark implementation
func benchmarkSessionImpl(b *testing.B, sessionFactory func(device *pn532.Device, config *Config) *Session) {
	device, mockTransport := createMockDeviceWithTransportForBench(b)

	// Setup mock to return a card on poll
	mockTransport.SetResponse(0x4A, []byte{
		0xD5, 0x4B, 0x01, 0x01, 0x00, 0x04, 0x00, 0x07, 0x04,
		0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
	})

	config := &Config{
		PollInterval:       1 * time.Millisecond, // Fast polling for benchmark
		CardRemovalTimeout: 10 * time.Millisecond,
	}

	session := sessionFactory(device, config)

	// Pre-allocate context for benchmark accuracy
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	b.ResetTimer() // Start timing after setup

	for i := 0; i < b.N; i++ {
		// Measure Session.Start() call latency
		err := session.Start(ctx)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			b.Fatalf("Session.Start() failed: %v", err)
		}
	}

	_ = session.Close()
}

// BenchmarkWriteLatency measures write operation latency
func BenchmarkWriteLatency(b *testing.B) {
	device, mockTransport := createMockDeviceWithTransportForBench(b)

	// Setup successful write responses
	mockTransport.SetResponse(0x4A, []byte{
		0xD5, 0x4B, 0x01, 0x01, 0x00, 0x04, 0x00, 0x07, 0x04,
		0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
	})
	mockTransport.SetResponse(0x40, []byte{0xD5, 0x41, 0x00}) // Successful write

	config := &Config{
		PollInterval:       10 * time.Millisecond,
		CardRemovalTimeout: 100 * time.Millisecond,
	}

	b.Run("Traditional_Session_Write", func(b *testing.B) {
		benchmarkWriteOperation(b, NewSession(device, config))
	})

	b.Run("Actor_Based_Session_Write", func(b *testing.B) {
		benchmarkWriteOperation(b, NewActorBasedSession(device, config))
	})
}

// benchmarkWriteOperation measures WriteToTag latency
func benchmarkWriteOperation(b *testing.B, session *Session) {
	// Create mock detected tag
	detectedTag := &pn532.DetectedTag{
		UID:  "12345678",
		Type: pn532.TagTypeNTAG,
	}

	ctx := context.Background()

	// Simple write function
	writeFn := func(_ pn532.Tag) error {
		return nil // Mock successful write
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := session.WriteToTag(ctx, detectedTag, writeFn)
		if err != nil {
			b.Fatalf("WriteToTag failed: %v", err)
		}
	}

	_ = session.Close()
}

// BenchmarkMemoryAllocation measures memory efficiency
func BenchmarkMemoryAllocation(b *testing.B) {
	device, mockTransport := createMockDeviceWithTransportForBench(b)
	mockTransport.SetResponse(0x4A, []byte{
		0xD5, 0x4B, 0x01, 0x01, 0x00, 0x04, 0x00, 0x07, 0x04,
		0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC,
	})

	config := &Config{
		PollInterval:       10 * time.Millisecond,
		CardRemovalTimeout: 100 * time.Millisecond,
	}

	b.Run("Traditional_Session_Allocation", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			session := NewSession(device, config)
			_ = session.Close()
		}
	})

	b.Run("Actor_Based_Session_Allocation", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			session := NewActorBasedSession(device, config)
			_ = session.Close()
		}
	})
}
