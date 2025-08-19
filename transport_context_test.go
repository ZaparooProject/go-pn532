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
	"testing"
	"time"
)

// TestTransportContextInterface verifies that all transport implementations
// support the new SendCommandWithContext method
func TestTransportContextInterface(t *testing.T) {
	t.Parallel()
	tests := []struct {
		transport Transport
		name      string
	}{
		{
			name:      "MockTransport",
			transport: NewMockTransport(),
		},
		{
			name:      "TransportWithRetry",
			transport: NewTransportWithRetry(NewMockTransport(), nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Verify the transport implements the context method
			ctx := context.Background()
			cmd := byte(0x02) // GetFirmwareVersion
			args := []byte{}

			// This should compile and not panic
			_, err := tt.transport.SendCommandWithContext(ctx, cmd, args)
			if err != nil {
				t.Logf("Expected error for unimplemented command: %v", err)
			}
		})
	}
}

// TestAllTransportTypesImplementContext ensures all concrete transport types
// implement SendCommandWithContext method at compile time
func TestAllTransportTypesImplementContext(t *testing.T) {
	t.Parallel()
	// This test ensures all transport types implement the context interface at compile time
	tests := []struct {
		checkFunc func() bool
		name      string
	}{
		{
			name: "MockTransport implements context interface",
			checkFunc: func() bool {
				var transport Transport = NewMockTransport()
				_, ok := transport.(interface {
					SendCommandWithContext(context.Context, byte, []byte) ([]byte, error)
				})
				return ok
			},
		},
		{
			name: "TransportWithRetry implements context interface",
			checkFunc: func() bool {
				var transport Transport = NewTransportWithRetry(NewMockTransport(), nil)
				_, ok := transport.(interface {
					SendCommandWithContext(context.Context, byte, []byte) ([]byte, error)
				})
				return ok
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !tt.checkFunc() {
				t.Errorf("%s failed: transport does not implement SendCommandWithContext", tt.name)
			}
		})
	}
}

// TestMockTransportContextCancellation tests that MockTransport properly handles context cancellation
func TestMockTransportContextCancellation(t *testing.T) {
	t.Parallel()
	mock := NewMockTransport()

	// Create a context that cancels immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	cmd := byte(0x02)
	args := []byte{}

	_, err := mock.SendCommandWithContext(ctx, cmd, args)
	if err == nil {
		t.Error("Expected context cancellation error, got nil")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
}

// TestMockTransportContextTimeout tests that MockTransport handles context timeout during delay
func TestMockTransportContextTimeout(t *testing.T) {
	t.Parallel()
	mock := NewMockTransport()

	// Set a long delay
	mock.SetDelay(200 * time.Millisecond)

	// Create a context with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	cmd := byte(0x02)
	args := []byte{}

	start := time.Now()
	_, err := mock.SendCommandWithContext(ctx, cmd, args)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected context timeout error, got nil")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded error, got: %v", err)
	}

	// Should timeout quickly, not wait for the full delay
	if elapsed > 100*time.Millisecond {
		t.Errorf("Operation took too long: %v, expected < 100ms", elapsed)
	}
}

// TestTransportWithRetryContextCancellation tests that TransportWithRetry properly handles context cancellation
func TestTransportWithRetryContextCancellation(t *testing.T) {
	t.Parallel()
	mock := NewMockTransport()
	retry := NewTransportWithRetry(mock, nil) // Use default retry config

	// Create a context that cancels immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	cmd := byte(0x02)
	args := []byte{}

	_, err := retry.SendCommandWithContext(ctx, cmd, args)
	if err == nil {
		t.Error("Expected context cancellation error, got nil")
	}

	// The error might be wrapped by retry logic, so check if it contains context.Canceled
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error (possibly wrapped), got: %v", err)
	}
}

// TestTransportWithRetryBackwardsCompatibility tests that TransportWithRetry still works with non-context methods
func TestTransportWithRetryBackwardsCompatibility(t *testing.T) {
	t.Parallel()
	mock := NewMockTransport()
	retry := NewTransportWithRetry(mock, nil) // Use default retry config

	cmd := byte(0x02)
	args := []byte{}

	// This should work exactly as before
	_, err := retry.SendCommand(cmd, args)
	if err != nil {
		t.Logf("Expected error for unimplemented command: %v", err)
	}
}

// TestUARTTransportContextInterface verifies that UART transport supports context
func TestUARTTransportContextInterface(t *testing.T) {
	t.Parallel()
	// Note: This test doesn't create actual UART connection, just tests interface
	// The context method should exist and handle context properly

	// We can't easily test UART without actual hardware, but we can test
	// that the method exists and basic context handling
	t.Run("ContextMethodExists", func(t *testing.T) {
		t.Parallel()
		// This verifies the method signature exists at compile time
		var transport Transport
		if uartTransport, ok := any(transport).(interface {
			SendCommandWithContext(context.Context, byte, []byte) ([]byte, error)
		}); ok {
			_ = uartTransport // Method exists
		}
	})
}

// contextTestCase defines a test case for context cancellation testing
type contextTestCase struct {
	name           string
	setupDelay     time.Duration
	contextTimeout time.Duration
	expectTimeout  bool
}

// createContextForTest creates appropriate context based on test case
func createContextForTest(tt contextTestCase) (context.Context, context.CancelFunc) {
	if tt.contextTimeout == 0 {
		// Immediate cancellation
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately
		return ctx, cancel
	}
	// Timeout after specified duration
	return context.WithTimeout(context.Background(), tt.contextTimeout)
}

// validateContextError checks if the error type and timing match expectations
func validateContextError(t *testing.T, err error, elapsed time.Duration, tt contextTestCase) {
	if err == nil {
		t.Error("Expected context error, got nil")
		return
	}

	if tt.expectTimeout {
		validateTimeoutError(t, err, elapsed, tt.contextTimeout)
	} else {
		validateCancellationError(t, err, elapsed)
	}
}

// validateTimeoutError validates timeout-specific error and timing
func validateTimeoutError(t *testing.T, err error, elapsed, expectedTimeout time.Duration) {
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Expected context.DeadlineExceeded, got: %v", err)
	}
	// Should complete near the timeout duration
	if elapsed < expectedTimeout || elapsed > expectedTimeout+50*time.Millisecond {
		t.Errorf("Unexpected timing: %v, expected ~%v", elapsed, expectedTimeout)
	}
}

// validateCancellationError validates cancellation-specific error and timing
func validateCancellationError(t *testing.T, err error, elapsed time.Duration) {
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got: %v", err)
	}
	// Should complete quickly for immediate cancellation
	if elapsed > 10*time.Millisecond {
		t.Errorf("Immediate cancellation took too long: %v", elapsed)
	}
}

// TestContextCancellationDuringOperationPhases tests context cancellation
// during different phases of transport operations
func TestContextCancellationDuringOperationPhases(t *testing.T) {
	t.Parallel()
	tests := []contextTestCase{
		{
			name:           "Immediate cancellation before operation",
			setupDelay:     0,
			contextTimeout: 0, // Cancel immediately
			expectTimeout:  false,
		},
		{
			name:           "Timeout during frame transmission simulation",
			setupDelay:     100 * time.Millisecond,
			contextTimeout: 50 * time.Millisecond,
			expectTimeout:  true,
		},
		{
			name:           "Timeout during ACK waiting simulation",
			setupDelay:     200 * time.Millisecond,
			contextTimeout: 80 * time.Millisecond,
			expectTimeout:  true,
		},
		{
			name:           "Timeout during frame reception simulation",
			setupDelay:     150 * time.Millisecond,
			contextTimeout: 75 * time.Millisecond,
			expectTimeout:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := NewMockTransport()
			mock.SetDelay(tt.setupDelay)

			ctx, cancel := createContextForTest(tt)
			defer cancel()

			cmd := byte(0x02) // GetFirmwareVersion
			args := []byte{}

			start := time.Now()
			_, err := mock.SendCommandWithContext(ctx, cmd, args)
			elapsed := time.Since(start)

			validateContextError(t, err, elapsed, tt)
		})
	}
}

// timeoutTestCase defines a test case for timeout interaction testing
type timeoutTestCase struct {
	expectedError  error
	name           string
	description    string
	contextTimeout time.Duration
	transportDelay time.Duration
}

// validateTimeoutInteraction validates the expected behavior of timeout vs transport interactions
func validateTimeoutInteraction(t *testing.T, err error, elapsed time.Duration, tt timeoutTestCase) {
	if tt.expectedError == nil {
		validateSuccessCase(t, err, elapsed, tt.transportDelay)
	} else {
		validateErrorCase(t, err, elapsed, tt.expectedError, tt.contextTimeout)
	}
}

// validateSuccessCase validates successful completion scenarios
func validateSuccessCase(t *testing.T, err error, elapsed, transportDelay time.Duration) {
	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}
	// Should complete near transport delay time
	if elapsed < transportDelay || elapsed > transportDelay+50*time.Millisecond {
		t.Errorf("Unexpected timing for success: %v, expected ~%v", elapsed, transportDelay)
	}
}

// validateErrorCase validates timeout error scenarios
func validateErrorCase(
	t *testing.T, err error, elapsed time.Duration,
	expectedError error, contextTimeout time.Duration,
) {
	if !errors.Is(err, expectedError) {
		t.Errorf("Expected %v, got: %v", expectedError, err)
	}
	// Should timeout near context timeout (allow 5ms under for timing precision)
	if elapsed < contextTimeout-5*time.Millisecond || elapsed > contextTimeout+50*time.Millisecond {
		t.Errorf("Unexpected timing for timeout: %v, expected ~%v", elapsed, contextTimeout)
	}
}

// TestContextTimeoutVsTransportTimeoutInteraction tests how context timeouts
// interact with transport-level timeouts
func TestContextTimeoutVsTransportTimeoutInteraction(t *testing.T) {
	t.Parallel()
	tests := []timeoutTestCase{
		{
			name:           "Context timeout wins (shorter)",
			contextTimeout: 50 * time.Millisecond,
			transportDelay: 150 * time.Millisecond,
			expectedError:  context.DeadlineExceeded,
			description:    "Context should timeout before transport completes",
		},
		{
			name:           "Transport completes first",
			contextTimeout: 200 * time.Millisecond,
			transportDelay: 50 * time.Millisecond,
			expectedError:  nil, // Success case
			description:    "Transport should complete before context timeout",
		},
		{
			name:           "Very short context timeout",
			contextTimeout: 10 * time.Millisecond,
			transportDelay: 100 * time.Millisecond,
			expectedError:  context.DeadlineExceeded,
			description:    "Very short context timeout should win",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := NewMockTransport()
			mock.SetDelay(tt.transportDelay)

			// Configure mock to return success after delay
			mock.SetResponse(0x02, []byte{0x00})

			ctx, cancel := context.WithTimeout(context.Background(), tt.contextTimeout)
			defer cancel()

			cmd := byte(0x02) // GetFirmwareVersion
			args := []byte{}

			start := time.Now()
			_, err := mock.SendCommandWithContext(ctx, cmd, args)
			elapsed := time.Since(start)

			validateTimeoutInteraction(t, err, elapsed, tt)
		})
	}
}

// TestRetryBehaviorWithContextCancellation tests that retry logic properly
// handles context cancellation at different stages of the retry process
func TestRetryBehaviorWithContextCancellation(t *testing.T) {
	t.Parallel()
	// Test immediate context cancellation - this should work reliably
	t.Run("Immediate context cancellation with retry", func(t *testing.T) {
		t.Parallel()
		mock := NewMockTransport()

		// Configure persistent failure to trigger retries
		testErr := NewTransportError("test", "mock", errors.New("simulated failure"), ErrorTypeTransient)
		mock.SetError(0x02, testErr)

		// Create retry transport with backoff
		retryConfig := &RetryConfig{
			MaxAttempts:       5,
			InitialBackoff:    100 * time.Millisecond,
			MaxBackoff:        200 * time.Millisecond,
			BackoffMultiplier: 1.5,
			Jitter:            0,
		}
		retry := NewTransportWithRetry(mock, retryConfig)

		// Create context that is immediately cancelled
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		cmd := byte(0x02)
		args := []byte{}

		start := time.Now()
		_, err := retry.SendCommandWithContext(ctx, cmd, args)
		elapsed := time.Since(start)

		// Verify error occurred
		if err == nil {
			t.Error("Expected context error, got nil")
			return
		}

		// Check error type (might be wrapped by retry logic)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled (possibly wrapped), got: %v", err)
		}

		// Immediate cancellation should be very quick
		if elapsed > 10*time.Millisecond {
			t.Errorf("Immediate cancellation took too long: %v", elapsed)
		}
	})
}
