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

// tfiOffset is the distance from the frame length field to the TFI byte,
// skipping the length and length-checksum bytes.
const tfiOffset = 2

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
	// Validate inputs to prevent panics from malformed hardware data
	// frameLen must be > 0 since we compute dataLen = frameLen - 1
	if off < 0 || frameLen <= 0 {
		return nil, false, &pn532.TransportError{
			Op:        "extractFrameData",
			Err:       pn532.ErrFrameCorrupted,
			Type:      pn532.ErrorTypeTransient,
			Retryable: true,
		}
	}

	// Move to TFI position (skip length and length checksum).
	// Compare against the space remaining in buf rather than adding to off
	// first: a corrupt offset close to the maximum int would wrap negative and
	// then index buf out of bounds.
	if off >= len(buf)-tfiOffset {
		return nil, false, &pn532.TransportError{
			Op:        "extractFrameData",
			Err:       pn532.ErrFrameCorrupted,
			Type:      pn532.ErrorTypeTransient,
			Retryable: true,
		}
	}

	off += tfiOffset

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

	// Bound dataLen by the bytes left in buf. Written as a subtraction so a
	// corrupt frameLen close to the maximum int cannot overflow the sum and
	// pass this check, which would size the buffer below from unvalidated
	// hardware data.
	dataLen := frameLen - 1
	if dataLen > len(buf)-off {
		return nil, false, &pn532.TransportError{
			Op:        "extractFrameData",
			Err:       pn532.ErrFrameCorrupted,
			Type:      pn532.ErrorTypeTransient,
			Retryable: true,
		}
	}

	// Use buffer pool to reduce allocations - this is a major optimization
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
