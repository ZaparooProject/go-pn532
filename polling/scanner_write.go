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
	"time"

	"github.com/ZaparooProject/go-pn532"
)

// WriteToNextTag waits for the next detected tag and executes the write operation
// This method blocks until a tag is detected and the operation completes, times out, or is cancelled
func (s *Scanner) WriteToNextTag(ctx context.Context, timeout time.Duration, operation func(pn532.Tag) error) error {
	// Validate scanner state
	if !s.running.Load() {
		return ErrScannerNotRunning
	}

	// Serialize write requests to prevent concurrent writes
	s.writeMutex.Lock()
	defer s.writeMutex.Unlock()

	// Check for existing pending write
	if s.pendingWrite.Load() != nil {
		return ErrWriteAlreadyPending
	}

	// Create timeout context
	writeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Queue write request
	result := make(chan error, 1)
	req := &WriteRequest{
		operation: operation,
		result:    result,
		ctx:       writeCtx,
		createdAt: time.Now(),
	}

	s.pendingWrite.Store(req)
	defer s.pendingWrite.Store(nil)

	// Wait for completion, timeout, or cancellation
	select {
	case err := <-result:
		return err
	case <-writeCtx.Done():
		return writeCtx.Err()
	}
}

// WriteToCurrentTag immediately attempts to write to currently detected tag
// Returns an error if no tag is currently detected or if the scanner is not running
func (s *Scanner) WriteToCurrentTag(_ func(pn532.Tag) error) error {
	if !s.running.Load() {
		return ErrScannerNotRunning
	}

	// The Monitor doesn't store current tag state, so this operation is not supported
	// Users should use WriteToNextTag instead for coordinated write operations
	return errors.New("WriteToCurrentTag not supported - use WriteToNextTag instead")
}

// processPendingWrites handles queued write operations when tags are detected
// This is called internally by the polling loop when tags are detected
func (s *Scanner) processPendingWrites(detectedTag *pn532.DetectedTag) {
	req := s.pendingWrite.Load()
	if req == nil {
		return // No pending write
	}

	// Check if request is still valid (not cancelled/timed out)
	select {
	case <-req.ctx.Done():
		// Request was cancelled/timed out
		s.sendWriteResult(req, req.ctx.Err())
		return
	default:
		// Request is still active
	}

	// Execute write operation using the underlying monitor for safety
	err := s.monitor.WriteToTag(detectedTag, req.operation)
	s.sendWriteResult(req, err)
}

// sendWriteResult safely sends the result of a write operation back to the waiting goroutine
func (*Scanner) sendWriteResult(req *WriteRequest, err error) {
	select {
	case req.result <- err:
		// Result sent successfully
	default:
		// Channel was closed or full - this shouldn't happen but handle gracefully
	}
}
