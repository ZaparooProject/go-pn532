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

//go:build !darwin || cgo

//nolint:paralleltest // Tests replace package-level serial enumeration/open functions.
package uart

import (
	"context"
	"errors"
	"testing"
	"time"

	virt "github.com/ZaparooProject/go-pn532/internal/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

var errModeRejected = errors.New("mode rejected")

// modeRecordingPort records the mode and read timeout a transport applies.
type modeRecordingPort struct {
	*MockSerialPort
	modeErr    error
	modes      []*serial.Mode
	timeouts   []time.Duration
	closeCalls int
}

func newModeRecordingPort() *modeRecordingPort {
	return &modeRecordingPort{MockSerialPort: NewMockSerialPort(virt.NewVirtualPN532())}
}

func (p *modeRecordingPort) SetMode(mode *serial.Mode) error {
	p.modes = append(p.modes, mode)
	return p.modeErr
}

func (p *modeRecordingPort) SetReadTimeout(timeout time.Duration) error {
	p.timeouts = append(p.timeouts, timeout)
	return p.MockSerialPort.SetReadTimeout(timeout)
}

func (p *modeRecordingPort) Close() error {
	p.closeCalls++
	return p.MockSerialPort.Close()
}

// stubNoPorts keeps profile resolution off the host's serial ports.
func stubNoPorts(t *testing.T) {
	t.Helper()
	original := getDetailedPortsList
	t.Cleanup(func() { getDetailedPortsList = original })
	getDetailedPortsList = func() ([]*enumerator.PortDetails, error) { return nil, nil }
}

func TestNewFromPortAppliesModeAndTimeout(t *testing.T) {
	stubNoPorts(t)
	port := newModeRecordingPort()

	transport, err := NewFromPort(port, "usbserial:1-1", nil)
	require.NoError(t, err)

	require.Len(t, port.modes, 1)
	assert.Equal(t, 115200, port.modes[0].BaudRate)
	assert.Equal(t, 8, port.modes[0].DataBits)
	assert.Equal(t, serial.NoParity, port.modes[0].Parity)
	assert.Equal(t, serial.OneStopBit, port.modes[0].StopBits)
	assert.Nil(t, port.modes[0].InitialStatusBits)
	assert.Equal(t, []time.Duration{getReadTimeout()}, port.timeouts)
	assert.True(t, transport.IsConnected())
}

func TestNewFromPortTalksToPN532(t *testing.T) {
	stubNoPorts(t)
	port := newModeRecordingPort()

	transport, err := NewFromPort(port, "usbserial:1-1", nil)
	require.NoError(t, err)

	// GetFirmwareVersion through the caller's port.
	res, err := transport.SendCommand(context.Background(), 0x02, nil)
	require.NoError(t, err)
	require.NotEmpty(t, res)
	assert.Equal(t, byte(0x03), res[0])
}

func TestNewFromPortRejectsNilPort(t *testing.T) {
	_, err := NewFromPort(nil, "usbserial:1-1", nil)
	require.Error(t, err)
}

func TestNewFromPortClosesPortOnModeFailure(t *testing.T) {
	stubNoPorts(t)
	port := newModeRecordingPort()
	port.modeErr = errModeRejected

	_, err := NewFromPort(port, "usbserial:1-1", nil)
	require.ErrorIs(t, err, errModeRejected)
	assert.Equal(t, 1, port.closeCalls)
}

func TestNewFromPortCloseClosesPort(t *testing.T) {
	stubNoPorts(t)
	port := newModeRecordingPort()

	transport, err := NewFromPort(port, "usbserial:1-1", nil)
	require.NoError(t, err)
	require.NoError(t, transport.Close())
	assert.Equal(t, 1, port.closeCalls)
	assert.False(t, transport.IsConnected())
}

func TestNewFromPortReconnectUsesReopen(t *testing.T) {
	stubNoPorts(t)
	first := newModeRecordingPort()
	second := newModeRecordingPort()

	var opened []string
	var modes []*serial.Mode
	reopen := func(name string, mode *serial.Mode) (serial.Port, error) {
		opened = append(opened, name)
		modes = append(modes, mode)
		return second, nil
	}

	transport, err := NewFromPort(first, "usbserial:1-1", reopen)
	require.NoError(t, err)
	require.NoError(t, transport.Reconnect())

	assert.Equal(t, []string{"usbserial:1-1"}, opened)
	require.Len(t, modes, 1)
	assert.Equal(t, 115200, modes[0].BaudRate)
	assert.Equal(t, 1, first.closeCalls)
	assert.Equal(t, []time.Duration{getReadTimeout()}, second.timeouts)

	res, err := transport.SendCommand(context.Background(), 0x02, nil)
	require.NoError(t, err)
	require.NotEmpty(t, res)
}

func TestNewFromPortReconnectWithoutReopenFails(t *testing.T) {
	stubNoPorts(t)
	port := newModeRecordingPort()

	transport, err := NewFromPort(port, "usbserial:1-1", nil)
	require.NoError(t, err)

	err = transport.Reconnect()
	require.ErrorIs(t, err, ErrNoReopen)
	// The original port stays in use.
	assert.Zero(t, port.closeCalls)
	assert.True(t, transport.IsConnected())
}

func TestNewReconnectStillUsesOpenSerialPort(t *testing.T) {
	stubNoPorts(t)
	originalOpen := openSerialPort
	t.Cleanup(func() { openSerialPort = originalOpen })

	opens := 0
	openSerialPort = func(_ string, _ *serial.Mode) (serial.Port, error) {
		opens++
		return NewMockSerialPort(virt.NewVirtualPN532()), nil
	}

	transport, err := New("/dev/ttyUSB9")
	require.NoError(t, err)
	require.NoError(t, transport.Reconnect())
	assert.Equal(t, 2, opens)
}

func TestNewFromPortAppliesPN532KillerProfile(t *testing.T) {
	original := getDetailedPortsList
	t.Cleanup(func() { getDetailedPortsList = original })
	getDetailedPortsList = func() ([]*enumerator.PortDetails, error) {
		return []*enumerator.PortDetails{{
			Name: "/dev/ttyACM7", VID: "1A86", PID: "55D3", Product: "PN532Killer-UART",
		}}, nil
	}
	port := newModeRecordingPort()

	_, err := NewFromPort(port, "/dev/ttyACM7", nil)
	require.NoError(t, err)
	require.Len(t, port.modes, 1)
	require.NotNil(t, port.modes[0].InitialStatusBits)
	assert.False(t, port.modes[0].InitialStatusBits.DTR)
}
