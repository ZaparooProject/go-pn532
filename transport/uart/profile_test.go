// Copyright 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: Apache-2.0

//nolint:paralleltest // Tests replace package-level serial enumeration/open functions.
package uart

import (
	"errors"
	"testing"

	pn532 "github.com/ZaparooProject/go-pn532"
	virt "github.com/ZaparooProject/go-pn532/internal/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

func TestResolveDeviceProfile(t *testing.T) {
	original := getDetailedPortsList
	t.Cleanup(func() { getDetailedPortsList = original })

	tests := []struct {
		name    string
		port    *enumerator.PortDetails
		listErr error
		want    deviceProfile
	}{
		{
			name: "exact PN532Killer metadata",
			port: &enumerator.PortDetails{
				Name: "/dev/ttyACM0", VID: "1a86", PID: "55d3", Product: "PN532Killer-UART",
			},
			want: profilePN532Killer,
		},
		{
			name: "missing product",
			port: &enumerator.PortDetails{Name: "/dev/ttyACM0", VID: "1A86", PID: "55D3"},
			want: profileGeneric,
		},
		{
			name: "wrong product",
			port: &enumerator.PortDetails{
				Name: "/dev/ttyACM0", VID: "1A86", PID: "55D3", Product: "USB Serial",
			},
			want: profileGeneric,
		},
		{
			name: "wrong VID PID",
			port: &enumerator.PortDetails{
				Name: "/dev/ttyACM0", VID: "1A86", PID: "7523", Product: "PN532Killer-UART",
			},
			want: profileGeneric,
		},
		{
			name:    "enumeration failure",
			listErr: errors.New("enumeration failed"),
			want:    profileGeneric,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getDetailedPortsList = func() ([]*enumerator.PortDetails, error) {
				if tt.port == nil {
					return nil, tt.listErr
				}
				return []*enumerator.PortDetails{tt.port}, tt.listErr
			}

			assert.Equal(t, tt.want, resolveDeviceProfile("/dev/ttyACM0"))
		})
	}
}

func TestSerialModeProfileSettings(t *testing.T) {
	genericMode := serialMode(profileGeneric)
	assert.Nil(t, genericMode.InitialStatusBits)

	killerMode := serialMode(profilePN532Killer)
	require.NotNil(t, killerMode.InitialStatusBits)
	assert.False(t, killerMode.InitialStatusBits.DTR)
	assert.False(t, killerMode.InitialStatusBits.RTS)
}

func TestPN532KillerProfilePersistsAcrossReconnect(t *testing.T) {
	originalEnumerate := getDetailedPortsList
	originalOpen := openSerialPort
	t.Cleanup(func() {
		getDetailedPortsList = originalEnumerate
		openSerialPort = originalOpen
	})

	getDetailedPortsList = func() ([]*enumerator.PortDetails, error) {
		return []*enumerator.PortDetails{{
			Name: "/dev/ttyACM0", VID: "1A86", PID: "55D3", Product: "PN532Killer-UART",
		}}, nil
	}

	var modes []*serial.Mode
	openSerialPort = func(_ string, mode *serial.Mode) (serial.Port, error) {
		modes = append(modes, mode)
		return NewMockSerialPort(virt.NewVirtualPN532()), nil
	}

	transport, err := New("/dev/ttyACM0")
	require.NoError(t, err)
	require.NoError(t, transport.Reconnect())
	require.Len(t, modes, 2)
	for _, mode := range modes {
		require.NotNil(t, mode.InitialStatusBits)
		assert.False(t, mode.InitialStatusBits.DTR)
		assert.False(t, mode.InitialStatusBits.RTS)
	}
	assert.True(t, transport.HasCapability(pn532.CapabilityRequiresRawType2Commands))
}
