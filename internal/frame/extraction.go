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

package frame

import (
	"sync"

	"github.com/ZaparooProject/go-pn532"
)

// BufferPool manages reusable byte slices for different size categories
// This reduces allocations in the hot paths of frame processing
type BufferPool struct {
	// Small buffers for ACK/NACK processing (1-16 bytes)
	smallPool sync.Pool
	// Medium buffers for standard frames (17-255 bytes)
	mediumPool sync.Pool
	// Large buffers for extended frames (256-512 bytes)
	largePool sync.Pool
	// Frame buffers for complete frame operations (up to 262 bytes + overhead)
	framePool sync.Pool
}

// Size thresholds for buffer categories
const (
	SmallBufferSize  = 16  // ACK processing, small responses
	MediumBufferSize = 255 // Standard PN532 frame data
	LargeBufferSize  = 512 // Extended frames with overhead
	FrameBufferSize  = 270 // Complete frame with all overhead (262 + 8)
)

// Global buffer pool instance
var defaultPool = NewBufferPool()

// NewBufferPool creates a new buffer pool with optimized allocations
func NewBufferPool() *BufferPool {
	return &BufferPool{
		smallPool: sync.Pool{
			New: func() any {
				buf := make([]byte, SmallBufferSize)
				return &buf
			},
		},
		mediumPool: sync.Pool{
			New: func() any {
				buf := make([]byte, MediumBufferSize)
				return &buf
			},
		},
		largePool: sync.Pool{
			New: func() any {
				buf := make([]byte, LargeBufferSize)
				return &buf
			},
		},
		framePool: sync.Pool{
			New: func() any {
				buf := make([]byte, FrameBufferSize)
				return &buf
			},
		},
	}
}

// GetBuffer acquires a buffer of appropriate size for the requested capacity
// Returns a buffer that is at least 'size' bytes long
// The returned buffer should be returned via PutBuffer when done
func (p *BufferPool) GetBuffer(size int) []byte {
	switch {
	case size <= SmallBufferSize:
		bufPtr, ok := p.smallPool.Get().(*[]byte)
		if !ok {
			return make([]byte, size)
		}
		buf := (*bufPtr)[:size] // Slice to requested size
		return buf
	case size <= MediumBufferSize:
		bufPtr, ok := p.mediumPool.Get().(*[]byte)
		if !ok {
			return make([]byte, size)
		}
		buf := (*bufPtr)[:size]
		return buf
	case size <= FrameBufferSize:
		bufPtr, ok := p.framePool.Get().(*[]byte)
		if !ok {
			return make([]byte, size)
		}
		buf := (*bufPtr)[:size]
		return buf
	case size <= LargeBufferSize:
		bufPtr, ok := p.largePool.Get().(*[]byte)
		if !ok {
			return make([]byte, size)
		}
		buf := (*bufPtr)[:size]
		return buf
	default:
		// For oversized requests, allocate directly to avoid pool pollution
		return make([]byte, size)
	}
}

// PutBuffer returns a buffer to the pool for reuse
// The buffer must not be used after calling this function
// The buffer will be reset (length set to capacity) and cleared of sensitive data
func (p *BufferPool) PutBuffer(buf []byte) {
	if buf == nil {
		return
	}

	// Clear sensitive data before returning to pool
	for i := range buf {
		buf[i] = 0
	}

	// Determine original pool based on capacity
	bufCap := cap(buf)
	switch bufCap {
	case SmallBufferSize:
		// Reset to full capacity and return to small pool
		fullBuf := buf[:SmallBufferSize]
		p.smallPool.Put(&fullBuf)
	case MediumBufferSize:
		fullBuf := buf[:MediumBufferSize]
		p.mediumPool.Put(&fullBuf)
	case LargeBufferSize:
		fullBuf := buf[:LargeBufferSize]
		p.largePool.Put(&fullBuf)
	case FrameBufferSize:
		fullBuf := buf[:FrameBufferSize]
		p.framePool.Put(&fullBuf)
	default:
		// Buffer was directly allocated (oversized), let GC handle it
		return
	}
}

// GetFrameBuffer is a convenience function for getting frame-sized buffers
// This is the most common case in UART transport operations
func (p *BufferPool) GetFrameBuffer() []byte {
	return p.GetBuffer(FrameBufferSize)
}

// GetSmallBuffer is a convenience function for getting small buffers
// Used for ACK processing and small command responses
func (p *BufferPool) GetSmallBuffer(size int) []byte {
	if size > SmallBufferSize {
		return p.GetBuffer(size)
	}
	return p.GetBuffer(size)
}

// Default pool functions for convenience

// GetBuffer acquires a buffer from the default pool
func GetBuffer(size int) []byte {
	return defaultPool.GetBuffer(size)
}

// PutBuffer returns a buffer to the default pool
func PutBuffer(buf []byte) {
	defaultPool.PutBuffer(buf)
}

// GetFrameBuffer gets a frame-sized buffer from the default pool
func GetFrameBuffer() []byte {
	return defaultPool.GetFrameBuffer()
}

// GetSmallBuffer gets a small buffer from the default pool
func GetSmallBuffer(size int) []byte {
	return defaultPool.GetSmallBuffer(size)
}

// ExtractFrameData extracts the data payload from a validated PN532 frame
// Returns the frame data, whether a retry is needed (NACK should be sent), and any error
// This consolidates the extractFrameData logic from UART and I2C transports
func ExtractFrameData(buf []byte, off, frameLen int, tfiExpected byte) (data []byte, retry bool, err error) {
	// Move to TFI position (skip length and length checksum)
	off += 2

	if off >= len(buf) {
		return nil, false, &pn532.TransportError{
			Op:        "extractFrameData",
			Err:       pn532.ErrFrameCorrupted,
			Type:      pn532.ErrorTypeTransient,
			Retryable: true,
		}
	}

	tfi := buf[off]

	// Check for error frame (TFI = 0x7F)
	if tfi == 0x7F {
		return HandleErrorFrame(buf, off)
	}

	// Validate TFI
	if tfi != tfiExpected {
		return nil, true, nil
	}

	// Extract frame data (skip TFI)
	off++
	if off+frameLen-1 > len(buf) {
		return nil, false, &pn532.TransportError{
			Op:        "extractFrameData",
			Err:       pn532.ErrFrameCorrupted,
			Type:      pn532.ErrorTypeTransient,
			Retryable: true,
		}
	}

	// Use buffer pool to reduce allocations - this is a major optimization
	dataLen := frameLen - 1
	data = GetBuffer(dataLen)
	copy(data, buf[off:off+dataLen])

	return data, false, nil
}

// HandleErrorFrame processes a PN532 error frame (TFI = 0x7F)
// Returns the error frame data and any processing error
func HandleErrorFrame(buf []byte, off int) (data []byte, retry bool, err error) {
	// Move past TFI to error code
	off++
	if off >= len(buf) {
		return nil, false, &pn532.TransportError{
			Op:        "handleErrorFrame",
			Err:       pn532.ErrFrameCorrupted,
			Type:      pn532.ErrorTypeTransient,
			Retryable: true,
		}
	}

	errorCode := buf[off]

	// Return the error frame data (including TFI and error code)
	return []byte{0x7F, errorCode}, false, nil
}

// BuildDataFrame constructs a PN532 data frame with proper framing
// Returns the complete frame ready for transmission
func BuildDataFrame(tfi byte, data []byte) []byte {
	frameLen := len(data) + 1 // +1 for TFI
	// +7 for framing overhead (preamble + start1 + start2 + len + lenchk + checksum + postamble)
	totalSize := frameLen + 7

	// Use buffer pool for frame construction to reduce allocations
	frame := GetBuffer(totalSize)

	// Build frame manually for better performance
	frame[0] = Preamble
	frame[1] = StartCode1
	frame[2] = StartCode2
	frame[3] = byte(frameLen)
	frame[4] = CalculateLengthChecksum(byte(frameLen))
	frame[5] = tfi

	// Copy data
	copy(frame[6:6+len(data)], data)

	// Add checksum and postamble
	frame[6+len(data)] = CalculateDataChecksum(tfi, data)
	frame[7+len(data)] = Postamble

	return frame[:totalSize]
}

// ExtractFrameDataPooled is a pooled version of ExtractFrameData that returns the buffer when done
// Caller must call PutBuffer on the returned data when finished
// This version provides maximum performance by avoiding the final copy
func ExtractFrameDataPooled(buf []byte, off, frameLen int, tfiExpected byte) (data []byte, retry bool, err error) {
	// Move to TFI position (skip length and length checksum)
	off += 2

	if off >= len(buf) {
		return nil, false, &pn532.TransportError{
			Op:        "extractFrameDataPooled",
			Err:       pn532.ErrFrameCorrupted,
			Type:      pn532.ErrorTypeTransient,
			Retryable: true,
		}
	}

	tfi := buf[off]

	// Check for error frame (TFI = 0x7F)
	if tfi == 0x7F {
		return HandleErrorFrame(buf, off)
	}

	// Validate TFI
	if tfi != tfiExpected {
		return nil, true, nil
	}

	// Extract frame data (skip TFI)
	off++
	if off+frameLen-1 > len(buf) {
		return nil, false, &pn532.TransportError{
			Op:        "extractFrameDataPooled",
			Err:       pn532.ErrFrameCorrupted,
			Type:      pn532.ErrorTypeTransient,
			Retryable: true,
		}
	}

	// Return a slice directly from the buffer - caller must handle pooling
	dataLen := frameLen - 1
	data = buf[off : off+dataLen : off+dataLen] // Use full slice expression to prevent accidents

	return data, false, nil
}
