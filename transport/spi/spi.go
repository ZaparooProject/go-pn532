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

// Package spi provides SPI transport implementation for PN532
package spi

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/internal/frame"
	"periph.io/x/conn/v3/physic"
	"periph.io/x/conn/v3/spi"
	"periph.io/x/conn/v3/spi/spireg"
	"periph.io/x/host/v3"
)

const (
	// SPI protocol constants
	spiStatRead  = 0x02
	spiDataWrite = 0x01
	spiDataRead  = 0x03
	spiReady     = 0x01

	// Protocol constants
	hostToPn532 = 0xD4
	pn532ToHost = 0xD5

	// Default SPI settings
	defaultFreq = 1 * physic.MegaHertz
	mode        = spi.Mode0 // CPOL=0, CPHA=0 (LSB first is handled by bit reversal)
)

var (
	ackFrame  = []byte{0x00, 0x00, 0xFF, 0x00, 0xFF, 0x00}
	nackFrame = []byte{0x00, 0x00, 0xFF, 0xFF, 0x00, 0x00}
)

// Transport implements the pn532.Transport interface for SPI communication
type Transport struct {
	port     spi.PortCloser
	conn     spi.Conn
	portName string
	timeout  time.Duration
}

// New creates a new SPI transport
func New(portName string) (*Transport, error) {
	// Initialize host
	if _, err := host.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize periph host: %w", err)
	}

	// Open SPI port
	port, err := spireg.Open(portName)
	if err != nil {
		return nil, fmt.Errorf("failed to open SPI port %s: %w", portName, err)
	}

	// Connect with SPI parameters
	conn, err := port.Connect(defaultFreq, mode, 8)
	if err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("failed to connect SPI: %w", err)
	}

	transport := &Transport{
		port:     port,
		conn:     conn,
		portName: portName,
		timeout:  50 * time.Millisecond,
	}

	// Wake up the PN532
	transport.wakeup()

	return transport, nil
}

// wakeup sends the wake up sequence to PN532
func (t *Transport) wakeup() {
	// Send a dummy byte to wake up the PN532
	time.Sleep(1 * time.Millisecond)
	_ = t.conn.Tx([]byte{0x00}, nil) // Ignore error for wakeup
	time.Sleep(1 * time.Millisecond)
}

// reverseBit reverses the bits in a byte (LSB <-> MSB)
// PN532 uses LSB first, but most SPI implementations are MSB first
func reverseBit(b byte) byte {
	var result byte
	for i := 0; i < 8; i++ {
		result <<= 1
		result |= b & 1
		b >>= 1
	}
	return result
}

// reverseBytes reverses bits in all bytes of a slice using buffer pool
func reverseBytes(data []byte) []byte {
	// Use buffer pool for reversed data - optimization for frequent bit reversal operations
	reversed := frame.GetBuffer(len(data))
	// Note: caller is responsible for calling frame.PutBuffer(reversed) when done
	for i, b := range data {
		reversed[i] = reverseBit(b)
	}
	return reversed[:len(data)] // Slice to exact size needed
}

// waitReady polls the PN532 status until it's ready
func (t *Transport) waitReady() error {
	deadline := time.Now().Add(t.timeout)
	statusCmd := []byte{reverseBit(spiStatRead), 0}

	// Use buffer pool for status response
	statusResp := frame.GetSmallBuffer(2)
	defer frame.PutBuffer(statusResp)

	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Millisecond) // Small delay between status checks

		if err := t.conn.Tx(statusCmd, statusResp); err != nil {
			return fmt.Errorf("SPI status read failed: %w", err)
		}

		if reverseBit(statusResp[1]) == spiReady {
			return nil
		}

		time.Sleep(5 * time.Millisecond) // Wait before next check
	}

	return pn532.NewTransportNotReadyError("waitReady", t.portName)
}

// SendCommand sends a command to the PN532 and waits for response
func (t *Transport) SendCommand(cmd byte, args []byte) ([]byte, error) {
	if err := t.sendFrame(cmd, args); err != nil {
		return nil, err
	}

	if err := t.waitAck(); err != nil {
		return nil, err
	}

	// Small delay for PN532 to process command
	time.Sleep(6 * time.Millisecond)

	return t.receiveFrame()
}

// SendCommandWithContext sends a command to the PN532 with context support
func (t *Transport) SendCommandWithContext(ctx context.Context, cmd byte, args []byte) ([]byte, error) {
	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// For now, delegate to existing implementation
	// TODO: Add context-aware operations
	return t.SendCommand(cmd, args)
}

// sendFrame sends a command frame to the PN532
func (t *Transport) sendFrame(cmd byte, args []byte) error {
	// Calculate frame size
	length := byte(len(args) + 2)                      // +2 for TFI and CMD
	frameSize := 3 + 1 + 1 + 1 + 1 + len(args) + 1 + 1 // preamble + len + lcs + tfi + cmd + args + dcs + postamble

	// Use buffer pool for frame construction - major optimization
	frameBuf := frame.GetBuffer(frameSize)
	defer frame.PutBuffer(frameBuf)

	// Build frame manually for better performance
	offset := 0
	// Preamble and start code
	frameBuf[offset] = 0x00
	frameBuf[offset+1] = 0x00
	frameBuf[offset+2] = 0xFF
	offset += 3

	// Length and checksum
	frameBuf[offset] = length
	frameBuf[offset+1] = ^length + 1
	offset += 2

	// TFI (Frame identifier)
	frameBuf[offset] = hostToPn532
	offset++

	// Command
	frameBuf[offset] = cmd
	offset++

	// Arguments
	copy(frameBuf[offset:offset+len(args)], args)
	offset += len(args)

	// Data checksum
	checksum := hostToPn532 + cmd
	for _, arg := range args {
		checksum += arg
	}
	frameBuf[offset] = ^checksum + 1
	offset++

	// Postamble
	frameBuf[offset] = 0x00

	// Prepare for SPI transmission using buffer pool
	spiDataBuf := frame.GetBuffer(frameSize + 1) // +1 for SPI command
	defer frame.PutBuffer(spiDataBuf)

	spiDataBuf[0] = reverseBit(spiDataWrite)

	// Reverse bytes and copy to SPI buffer
	reversedFrame := reverseBytes(frameBuf[:frameSize])
	defer frame.PutBuffer(reversedFrame) // Clean up reversed frame buffer
	copy(spiDataBuf[1:], reversedFrame)

	// Send the frame
	time.Sleep(2 * time.Millisecond) // Required delay
	if err := t.conn.Tx(spiDataBuf[:frameSize+1], nil); err != nil {
		return pn532.NewTransportWriteError("sendFrame", t.portName)
	}

	return nil
}

// waitAck waits for ACK frame from PN532
func (t *Transport) waitAck() error {
	// Wait for device to be ready
	if err := t.waitReady(); err != nil {
		return err
	}

	// Read ACK frame using buffer pool
	readCmd := []byte{reverseBit(spiDataRead)}
	readData := frame.GetSmallBuffer(len(ackFrame) + 1) // +1 for read command
	defer frame.PutBuffer(readData)

	if err := t.conn.Tx(readCmd, readData); err != nil {
		return pn532.NewTransportReadError("waitAck", t.portName)
	}

	// Convert from LSB to MSB and skip the first byte (status)
	ack := reverseBytes(readData[1:])
	defer frame.PutBuffer(ack)

	if !bytes.Equal(ack, ackFrame) {
		if bytes.Equal(ack, nackFrame) {
			return pn532.NewNACKReceivedError("waitAck", t.portName)
		}
		return pn532.NewInvalidResponseError("waitAck", t.portName)
	}

	return nil
}

// receiveFrame receives a response frame from the PN532
func (t *Transport) receiveFrame() ([]byte, error) {
	// Wait for device to be ready
	if err := t.waitReady(); err != nil {
		return nil, err
	}

	// First read to get the frame length using buffer pool
	readCmd := []byte{reverseBit(spiDataRead)}
	headerData := frame.GetSmallBuffer(8) // Read header to determine length
	defer frame.PutBuffer(headerData)

	if err := t.conn.Tx(readCmd, headerData); err != nil {
		return nil, pn532.NewTransportReadError("receiveFrame", t.portName)
	}

	// Convert from LSB to MSB and skip the first byte
	header := reverseBytes(headerData[1:])
	defer frame.PutBuffer(header)

	// Validate preamble and start code
	if !bytes.Equal(header[0:3], []byte{0x00, 0x00, 0xFF}) {
		return nil, pn532.NewFrameCorruptedError("receiveFrame", t.portName)
	}

	// Get length
	length := header[3]
	lengthChecksum := header[4]

	// Validate length checksum
	if (length + lengthChecksum) != 0 {
		return nil, pn532.NewInvalidResponseError("receiveFrame", t.portName)
	}

	// Now read the full frame including data and checksums using buffer pool
	fullLength := int(length) + 2               // +2 for data checksum and postamble
	fullData := frame.GetBuffer(fullLength + 1) // +1 for read command
	defer frame.PutBuffer(fullData)

	// Read the remaining data
	readCmd2 := []byte{reverseBit(spiDataRead)}
	if err := t.conn.Tx(readCmd2, fullData); err != nil {
		return nil, pn532.NewTransportReadError("receiveFrame", t.portName)
	}

	// Convert and skip first byte
	data := reverseBytes(fullData[1:])
	defer frame.PutBuffer(data)

	// Skip TFI (first byte) and extract response data
	if len(data) < int(length)+1 {
		return nil, pn532.NewFrameCorruptedError("receiveFrame", t.portName)
	}

	// data includes: TFI + response code + actual data
	// We want to skip TFI (data[0]) and response code (data[1])
	responseLen := int(length) - 2 // Subtract TFI and response code
	if responseLen < 0 {
		responseLen = 0
	}

	// Copy response data to new buffer since we'll release the pooled buffer
	responseData := make([]byte, responseLen)
	if responseLen > 0 {
		copy(responseData, data[2:2+responseLen])
	}

	// Verify data checksum
	checksum := frame.CalculateChecksum(data[:length])
	dataChecksum := data[length]
	if (checksum + dataChecksum) != 0 {
		return nil, pn532.NewChecksumMismatchError("receiveFrame", t.portName)
	}

	return responseData, nil
}

// SetTimeout sets the read timeout for the transport
func (t *Transport) SetTimeout(timeout time.Duration) error {
	t.timeout = timeout
	return nil
}

// Close closes the transport connection
func (t *Transport) Close() error {
	if t.port != nil {
		err := t.port.Close()
		if err != nil {
			return fmt.Errorf("SPI close failed: %w", err)
		}
	}
	return nil
}

// IsConnected returns true if the transport is connected
func (t *Transport) IsConnected() bool {
	return t.port != nil
}

// Type returns the transport type
func (*Transport) Type() pn532.TransportType {
	return pn532.TransportSPI
}
