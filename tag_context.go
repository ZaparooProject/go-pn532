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
)

// TagContext represents an NFC tag interface with context support
type TagContext interface {
	Tag

	// ReadBlockContext reads a block of data from the tag with context support
	ReadBlockContext(ctx context.Context, block uint8) ([]byte, error)

	// WriteBlockContext writes a block of data to the tag with context support
	WriteBlockContext(ctx context.Context, block uint8, data []byte) error

	// ReadNDEFContext reads NDEF data from the tag with context support
	ReadNDEFContext(ctx context.Context) (*NDEFMessage, error)

	// WriteNDEFContext writes NDEF data to the tag with context support
	WriteNDEFContext(ctx context.Context, message *NDEFMessage) error
}

// tagContextAdapter wraps a Tag to provide context support
type tagContextAdapter struct {
	Tag
	device *Device
}

// AsTagContext converts a Tag to TagContext
func AsTagContext(tag Tag, device *Device) TagContext {
	if tc, ok := tag.(TagContext); ok {
		return tc
	}
	return &tagContextAdapter{Tag: tag, device: device}
}

// ReadBlockContext implements TagContext
func (t *tagContextAdapter) ReadBlockContext(_ context.Context, block uint8) ([]byte, error) {
	// For now, we'll use the non-context version
	// In a real implementation, we'd need to modify the underlying tag implementations
	return t.ReadBlock(block)
}

// WriteBlockContext implements TagContext
func (t *tagContextAdapter) WriteBlockContext(_ context.Context, block uint8, data []byte) error {
	// For now, we'll use the non-context version
	return t.WriteBlock(block, data)
}

// ReadNDEFContext implements TagContext
func (t *tagContextAdapter) ReadNDEFContext(_ context.Context) (*NDEFMessage, error) {
	// For now, we'll use the non-context version
	return t.ReadNDEF()
}

// WriteNDEFContext implements TagContext
func (t *tagContextAdapter) WriteNDEFContext(_ context.Context, message *NDEFMessage) error {
	// For now, we'll use the non-context version
	return t.WriteNDEF(message)
}
