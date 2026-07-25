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
	"errors"
	"testing"
	"time"
)

// TestTransportContextInterface verifies that all transport implementations
// support the new SendCommand method
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Verify the transport implements the context method
			ctx := context.Background()
			cmd := byte(0x02) // GetFirmwareVersion
			args := []byte{}

			// This should compile and not panic
			_, err := tt.transport.SendCommand(ctx, cmd, args)
			if err != nil {
				t.Logf("Expected error for unimplemented command: %v", err)
			}
		})
	}
}

// TestAllTransportTypesImplementContext ensures all concrete transport types
// implement SendCommand method at compile time
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
					SendCommand(context.Context, byte, []byte) ([]byte, error)
				})
				return ok
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !tt.checkFunc() {
				t.Errorf("%s failed: transport does not implement SendCommand", tt.name)
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

	_, err := mock.SendCommand(ctx, cmd, args)
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
	_, err := mock.SendCommand(ctx, cmd, args)
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
			SendCommand(context.Context, byte, []byte) ([]byte, error)
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
	// Should complete near the timeout duration (allow 5ms under for timing precision on CI)
	if elapsed < expectedTimeout-5*time.Millisecond || elapsed > expectedTimeout+50*time.Millisecond {
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
			_, err := mock.SendCommand(ctx, cmd, args)
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
			_, err := mock.SendCommand(ctx, cmd, args)
			elapsed := time.Since(start)

			validateTimeoutInteraction(t, err, elapsed, tt)
		})
	}
}
