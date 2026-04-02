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

//nolint:dupl,varnamelen,gocritic,funlen // Test file - duplicate structures, short vars, and long funcs acceptable
package testing

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Frame building helper - constructs a valid PN532 command frame
func buildCommandFrame(cmd byte, params []byte) []byte {
	dataLen := 2 + len(params) // TFI + CMD + params
	lcs := byte(0 - dataLen)   //nolint:gosec // intentional two's complement checksum

	frameData := append([]byte{tfiHostToPN532, cmd}, params...)

	dcs := byte(0)
	for _, b := range frameData {
		dcs += b
	}
	dcs = byte(0 - int(dcs))

	frame := make([]byte, 0, 5+len(frameData)+2)
	frame = append(frame, pn532Preamble, pn532StartCode1, pn532StartCode2)
	frame = append(frame, byte(dataLen), lcs) //nolint:gosec // PN532 frame length fits in byte
	frame = append(frame, frameData...)
	frame = append(frame, dcs, pn532Postamble)

	return frame
}

// Frame parsing helper - extracts response data from a complete response
func parseResponseFrame(t *testing.T, data []byte) (cmd byte, responseData []byte) {
	t.Helper()

	// Skip ACK frame if present
	data = bytes.TrimPrefix(data, ACKFrame)

	require.GreaterOrEqual(t, len(data), 6, "response too short")

	// Find start code
	startIdx := -1
	for i := range len(data) - 1 {
		if data[i] == pn532StartCode1 && data[i+1] == pn532StartCode2 {
			startIdx = i
			break
		}
	}
	require.GreaterOrEqual(t, startIdx, 0, "no start code found")

	offset := startIdx + 2
	frameLen := int(data[offset])
	lcs := data[offset+1]

	// Validate length checksum
	lenByte := byte(frameLen) //nolint:gosec // intentional truncation for checksum
	require.Equal(t, byte(0), (lenByte+lcs)&0xFF, "length checksum error")

	// Extract frame data
	frameData := data[offset+2 : offset+2+frameLen]

	// Validate TFI
	require.Equal(t, byte(tfiPN532ToHost), frameData[0], "invalid TFI")

	return frameData[1], frameData[2:]
}

func TestVirtualPN532_FrameFormat(t *testing.T) {
	t.Parallel()

	t.Run("Valid_Frame_Accepted", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// GetFirmwareVersion command: D4 02
		frame := buildCommandFrame(cmdGetFirmwareVersion, nil)

		bytesWritten, err := sim.Write(frame)
		require.NoError(t, err)
		assert.Equal(t, len(frame), bytesWritten)

		// Read response
		buf := make([]byte, 256)
		bytesRead, err := sim.Read(buf)
		require.NoError(t, err)
		assert.Positive(t, bytesRead)

		// Parse and verify response
		cmd, data := parseResponseFrame(t, buf[:bytesRead])
		assert.Equal(t, byte(cmdGetFirmwareVersion+1), cmd)
		assert.Len(t, data, 4) // IC + Ver + Rev + Support
	})

	t.Run("Invalid_Start_Code_Rejected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Invalid start code
		badFrame := []byte{0x00, 0x00, 0x02, 0xFE, 0xD4, 0x02, 0x2A, 0x00}

		_, err := sim.Write(badFrame)
		require.NoError(t, err) // Write succeeds but frame is discarded

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Equal(t, 0, n) // No response generated
	})

	t.Run("Invalid_Length_Checksum_Rejected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Bad LCS
		badFrame := []byte{0x00, 0x00, 0xFF, 0x02, 0x00, 0xD4, 0x02, 0x2A, 0x00}

		_, err := sim.Write(badFrame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Equal(t, 0, n) // Frame rejected
	})

	t.Run("Invalid_Data_Checksum_Rejected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Valid structure but bad DCS
		badFrame := []byte{0x00, 0x00, 0xFF, 0x02, 0xFE, 0xD4, 0x02, 0x00, 0x00}

		_, err := sim.Write(badFrame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Equal(t, 0, n)
	})
}

func TestVirtualPN532_ACK_NACK(t *testing.T) {
	t.Parallel()

	t.Run("ACK_Sent_Before_Response", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		frame := buildCommandFrame(cmdGetFirmwareVersion, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		// Response should start with ACK
		assert.True(t, bytes.HasPrefix(buf[:n], ACKFrame), "response should start with ACK")
	})

	t.Run("NACK_Triggers_Retransmit", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// First, send a command and get response
		frame := buildCommandFrame(cmdGetFirmwareVersion, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		bytesRead, err := sim.Read(buf)
		require.NoError(t, err)
		firstResponse := make([]byte, bytesRead)
		copy(firstResponse, buf[:bytesRead])

		// Send NACK to request retransmission
		_, err = sim.Write(NACKFrame)
		require.NoError(t, err)

		bytesRead, err = sim.Read(buf)
		require.NoError(t, err)

		// Should get the same response again (minus ACK since NACK triggers retransmit)
		// The response frame should match
		assert.Positive(t, bytesRead)
	})

	t.Run("DropNextACK_Works", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()
		sim.DropNextACK()

		frame := buildCommandFrame(cmdGetFirmwareVersion, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		// Response should NOT start with ACK
		assert.False(t, bytes.HasPrefix(buf[:n], ACKFrame), "ACK should have been dropped")
	})
}

func TestVirtualPN532_GetFirmwareVersion(t *testing.T) {
	t.Parallel()

	t.Run("Default_Firmware_Version", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		frame := buildCommandFrame(cmdGetFirmwareVersion, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x03), cmd) // GetFirmwareVersion response
		require.Len(t, data, 4)
		assert.Equal(t, byte(0x32), data[0]) // IC = PN532
		assert.Equal(t, byte(0x01), data[1]) // Ver
		assert.Equal(t, byte(0x06), data[2]) // Rev
		assert.Equal(t, byte(0x07), data[3]) // Support
	})

	t.Run("Custom_Firmware_Version", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()
		sim.SetFirmwareVersion(0x32, 0x02, 0x08, 0x0F)

		frame := buildCommandFrame(cmdGetFirmwareVersion, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x03), cmd)
		require.Len(t, data, 4)
		assert.Equal(t, byte(0x32), data[0])
		assert.Equal(t, byte(0x02), data[1])
		assert.Equal(t, byte(0x08), data[2])
		assert.Equal(t, byte(0x0F), data[3])
	})
}

func TestVirtualPN532_SAMConfiguration(t *testing.T) {
	t.Parallel()

	t.Run("Normal_Mode", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// SAMConfiguration: Mode=0x01 (Normal)
		frame := buildCommandFrame(cmdSAMConfiguration, []byte{0x01})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x15), cmd) // SAMConfiguration response
		assert.Empty(t, data)            // Empty response = success

		state := sim.GetState()
		assert.True(t, state.SAMConfigured)
		assert.Equal(t, PowerModeNormal, state.PowerMode)
	})

	t.Run("Virtual_Card_Mode", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// SAMConfiguration: Mode=0x02 (Virtual Card), Timeout=0x14 (1 sec)
		frame := buildCommandFrame(cmdSAMConfiguration, []byte{0x02, 0x14})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, _ := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x15), cmd)

		state := sim.GetState()
		assert.True(t, state.SAMConfigured)
	})
}

//nolint:funlen // Test functions often need many sub-tests
func TestVirtualPN532_InListPassiveTarget(t *testing.T) {
	t.Parallel()

	t.Run("No_Tags_Present", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// InListPassiveTarget: MaxTg=1, BrTy=0x00 (106 kbps Type A)
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd) // InListPassiveTarget response
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(0x00), data[0]) // NbTg = 0
	})

	t.Run("Single_NTAG213_Detected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213([]byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
		sim.AddTag(tag)

		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(0x01), data[0]) // NbTg = 1

		// Verify target data
		require.GreaterOrEqual(t, len(data), 6)
		assert.Equal(t, byte(0x01), data[1])           // Tg = 1
		assert.Equal(t, []byte{0x44, 0x00}, data[2:4]) // SENS_RES for NTAG
		assert.Equal(t, byte(0x00), data[4])           // SEL_RES for NTAG
		assert.Equal(t, byte(0x07), data[5])           // NFCID length
	})

	t.Run("MIFARE_1K_Detected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualMIFARE1K([]byte{0x12, 0x34, 0x56, 0x78})
		sim.AddTag(tag)

		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		require.GreaterOrEqual(t, len(data), 6)
		assert.Equal(t, byte(0x01), data[0])           // NbTg = 1
		assert.Equal(t, byte(0x01), data[1])           // Tg = 1
		assert.Equal(t, []byte{0x04, 0x00}, data[2:4]) // SENS_RES for MIFARE 1K
		assert.Equal(t, byte(0x08), data[4])           // SEL_RES for MIFARE 1K
	})

	t.Run("Tag_Not_Present", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		tag.Remove() // Mark as not present
		sim.AddTag(tag)

		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		assert.Equal(t, byte(0x00), data[0]) // NbTg = 0
	})

	t.Run("Invalid_MaxTg", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// MaxTg = 0 is invalid
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x00, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		// Should have ACK and error frame
		assert.Positive(t, n)
	})
}

func TestVirtualPN532_InDataExchange(t *testing.T) {
	t.Parallel()

	t.Run("No_Target_Selected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Try data exchange without selecting a target
		frame := buildCommandFrame(cmdInDataExchange, []byte{0x01, 0x30, 0x04})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x41), cmd) // InDataExchange response
		require.GreaterOrEqual(t, len(data), 1)
		assert.NotEqual(t, byte(0x00), data[0]) // Error status
	})

	t.Run("Read_Block_Success", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// First detect the tag
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Now read block 4
		frame = buildCommandFrame(cmdInDataExchange, []byte{0x01, 0x30, 0x04})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x41), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(0x00), data[0]) // Success status
	})
}

func TestVirtualPN532_InRelease(t *testing.T) {
	t.Parallel()

	t.Run("Release_Target", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// Detect tag
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		assert.Equal(t, 1, sim.GetState().SelectedTarget)

		// Release target 1
		frame = buildCommandFrame(cmdInRelease, []byte{0x01})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x53), cmd) // InRelease response
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(0x00), data[0]) // Success

		assert.Equal(t, -1, sim.GetState().SelectedTarget)
	})
}

func TestVirtualPN532_PowerDown(t *testing.T) {
	t.Parallel()

	t.Run("Enter_PowerDown", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// PowerDown: WakeUpEnable = 0x20 (RF)
		frame := buildCommandFrame(cmdPowerDown, []byte{0x20})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x17), cmd) // PowerDown response
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(0x00), data[0]) // Success

		state := sim.GetState()
		assert.Equal(t, PowerModePowerDown, state.PowerMode)
		assert.False(t, state.RFFieldOn)
	})
}

func TestVirtualPN532_RFConfiguration(t *testing.T) {
	t.Parallel()

	t.Run("RF_Field_On", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// RFConfiguration: CfgItem=0x01 (RF Field), On
		frame := buildCommandFrame(cmdRFConfiguration, []byte{0x01, 0x01})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, _ := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x33), cmd) // RFConfiguration response

		assert.True(t, sim.GetState().RFFieldOn)
	})

	t.Run("RF_Field_Off", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()
		sim.state.RFFieldOn = true

		frame := buildCommandFrame(cmdRFConfiguration, []byte{0x01, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		assert.False(t, sim.GetState().RFFieldOn)
	})
}

func TestVirtualPN532_ChecksumCalculation(t *testing.T) {
	t.Parallel()

	t.Run("Manual_Frame_Verification", func(t *testing.T) {
		t.Parallel()

		// Example from PN532 manual: GetFirmwareVersion
		// Host sends: 00 00 FF 02 FE D4 02 2A 00
		manualFrame := []byte{0x00, 0x00, 0xFF, 0x02, 0xFE, 0xD4, 0x02, 0x2A, 0x00}

		// Verify LCS: 0x02 + 0xFE = 0x100, lower byte = 0x00 ✓
		lcs := (0x02 + 0xFE) & 0xFF
		assert.Equal(t, 0x00, lcs)

		// Verify DCS: 0xD4 + 0x02 + 0x2A = 0x100, lower byte = 0x00 ✓
		dcs := (0xD4 + 0x02 + 0x2A) & 0xFF
		assert.Equal(t, 0x00, dcs)

		// Our buildCommandFrame should produce the same thing
		builtFrame := buildCommandFrame(cmdGetFirmwareVersion, nil)
		assert.Equal(t, manualFrame, builtFrame)
	})

	t.Run("Response_Checksum_Validation", func(t *testing.T) {
		t.Parallel()

		// Example from manual: GetFirmwareVersion response
		// 00 00 FF 06 FA D5 03 32 01 05 07 E9 00
		// LCS: 0x06 + 0xFA = 0x100 ✓
		// DCS: 0xD5 + 0x03 + 0x32 + 0x01 + 0x05 + 0x07 + 0xE9 = 0x200 ✓

		sim := NewVirtualPN532()
		sim.SetFirmwareVersion(0x32, 0x01, 0x05, 0x07)

		frame := buildCommandFrame(cmdGetFirmwareVersion, nil)
		_, _ = sim.Write(frame)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)

		// Skip ACK
		response := buf[len(ACKFrame):n]

		// Verify structure
		assert.Equal(t, byte(0x00), response[0]) // Preamble
		assert.Equal(t, byte(0x00), response[1]) // Start code 1
		assert.Equal(t, byte(0xFF), response[2]) // Start code 2

		frameLen := response[3]
		lcs := response[4]
		assert.Equal(t, byte(0x00), (frameLen+lcs)&0xFF, "LCS validation failed")

		// Verify DCS
		frameData := response[5 : 5+int(frameLen)]
		dcs := response[5+int(frameLen)]
		sum := byte(0)
		for _, b := range frameData {
			sum += b
		}
		sum += dcs
		assert.Equal(t, byte(0x00), sum, "DCS validation failed")
	})
}

func TestVirtualPN532_ErrorInjection(t *testing.T) {
	t.Parallel()

	t.Run("Inject_Checksum_Error", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()
		sim.InjectChecksumError()

		frame := buildCommandFrame(cmdGetFirmwareVersion, nil)
		_, _ = sim.Write(frame)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)

		// Skip ACK
		response := buf[len(ACKFrame):n]

		// DCS should be corrupted
		frameLen := response[3]
		frameData := response[5 : 5+int(frameLen)]
		dcs := response[5+int(frameLen)]
		sum := byte(0)
		for _, b := range frameData {
			sum += b
		}
		sum += dcs
		assert.NotEqual(t, byte(0x00), sum, "checksum should be corrupted")
	})
}

func TestVirtualPN532_Reset(t *testing.T) {
	t.Parallel()

	t.Run("Reset_Clears_State", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Set up some state
		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)
		sim.state.SAMConfigured = true
		sim.state.RFFieldOn = true

		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Reset
		sim.Reset()

		state := sim.GetState()
		assert.Equal(t, PowerModeNormal, state.PowerMode)
		assert.False(t, state.SAMConfigured)
		assert.False(t, state.RFFieldOn)
		assert.Equal(t, -1, state.SelectedTarget)
	})
}

func TestVirtualPN532_RemoveAllTags(t *testing.T) {
	t.Parallel()

	t.Run("Removes_All_Tags_And_Clears_Selection", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Add multiple tags
		sim.AddTag(NewVirtualNTAG213(nil))
		sim.AddTag(NewVirtualMIFARE1K(nil))

		// Detect to select a tag
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x02, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		assert.Equal(t, 1, sim.GetState().SelectedTarget)

		// Remove all tags
		sim.RemoveAllTags()

		assert.Equal(t, -1, sim.GetState().SelectedTarget)

		// Verify no tags detected
		frame = buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		n, _ := sim.Read(buf)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		assert.Equal(t, byte(0x00), data[0]) // NbTg = 0
	})
}

func TestVirtualPN532_InjectNACK(t *testing.T) {
	t.Parallel()

	t.Run("NACK_Injection_Retransmits", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()
		sim.InjectNACK()

		// Send command
		frame := buildCommandFrame(cmdGetFirmwareVersion, nil)
		_, _ = sim.Write(frame)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		firstResponse := make([]byte, n)
		copy(firstResponse, buf[:n])

		// The inject flag should cause expected NACK handling
		// Send another NACK to trigger retransmit
		_, _ = sim.Write(NACKFrame)
		n, _ = sim.Read(buf)

		assert.Positive(t, n)
	})
}

// Helper to build extended command frame for testing
func buildExtendedCommandFrame(cmd byte, params []byte) []byte {
	dataLen := 2 + len(params) // TFI + CMD + params
	lenM := byte(dataLen >> 8) //nolint:gosec // intentional truncation for extended frame
	lenL := byte(dataLen & 0xFF)
	lcs := byte(0 - int(lenM) - int(lenL)) //nolint:gosec // intentional two's complement checksum

	frameData := append([]byte{tfiHostToPN532, cmd}, params...)

	dcs := byte(0)
	for _, b := range frameData {
		dcs += b
	}
	dcs = byte(0 - int(dcs))

	frame := make([]byte, 0, 8+len(frameData)+2)
	frame = append(frame, pn532Preamble, pn532StartCode1, pn532StartCode2)
	frame = append(frame, extendedFrameMarker, extendedFrameMarker)
	frame = append(frame, lenM, lenL, lcs)
	frame = append(frame, frameData...)
	frame = append(frame, dcs, pn532Postamble)

	return frame
}

func TestVirtualPN532_ExtendedFrames(t *testing.T) {
	t.Parallel()

	t.Run("Extended_Frame_Parsed_Correctly", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Build extended frame for GetFirmwareVersion
		frame := buildExtendedCommandFrame(cmdGetFirmwareVersion, nil)

		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)
		assert.Positive(t, n)

		// Should get valid response
		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x03), cmd)
		assert.Len(t, data, 4)
	})

	t.Run("Extended_Frame_With_Large_Payload", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Build extended frame with larger params (SAMConfiguration with extra data)
		params := make([]byte, 260) // Large payload to trigger extended frame
		params[0] = 0x01            // Normal mode
		frame := buildExtendedCommandFrame(cmdSAMConfiguration, params)

		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 512)
		n, err := sim.Read(buf)
		require.NoError(t, err)
		assert.Positive(t, n)
	})

	t.Run("Extended_Frame_Incomplete_Returns_No_Response", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Send partial extended frame (missing data)
		partialFrame := []byte{0x00, 0x00, 0xFF, 0xFF, 0xFF, 0x01, 0x00, 0xFF}

		_, err := sim.Write(partialFrame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Equal(t, 0, n) // No response - waiting for more data
	})

	t.Run("Extended_Frame_Bad_LCS_Rejected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Extended frame with bad LCS
		badFrame := []byte{
			0x00, 0x00, 0xFF, 0xFF, 0xFF, // Preamble + start + extended marker
			0x00, 0x02, // Length = 2
			0x00,       // Bad LCS (should be 0xFE)
			0xD4, 0x02, // TFI + cmd
			0x2A, // DCS
			0x00, // Postamble
		}

		_, err := sim.Write(badFrame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Equal(t, 0, n) // Frame rejected
	})
}

func TestVirtualPN532_BuildExtendedFrame(t *testing.T) {
	t.Parallel()

	t.Run("Large_Response_Uses_Extended_Frame", func(t *testing.T) {
		t.Parallel()

		// Create frame data > 255 bytes to trigger extended frame
		frameData := make([]byte, 300)
		frameData[0] = tfiPN532ToHost
		frameData[1] = 0x03 // GetFirmwareVersion response

		frame := buildExtendedFrame(frameData)

		// Verify extended frame structure
		assert.Equal(t, byte(pn532Preamble), frame[0])
		assert.Equal(t, byte(pn532StartCode1), frame[1])
		assert.Equal(t, byte(pn532StartCode2), frame[2])
		assert.Equal(t, byte(extendedFrameMarker), frame[3])
		assert.Equal(t, byte(extendedFrameMarker), frame[4])

		// Verify length bytes
		lenM := frame[5]
		lenL := frame[6]
		actualLen := int(lenM)*256 + int(lenL)
		assert.Equal(t, 300, actualLen)

		// Verify LCS
		lcs := frame[7]
		assert.Equal(t, byte(0), (lenM+lenL+lcs)&0xFF)

		// Verify postamble at end
		assert.Equal(t, byte(pn532Postamble), frame[len(frame)-1])
	})
}

func TestVirtualPN532_InSelect(t *testing.T) {
	t.Parallel()

	t.Run("Select_Valid_Target", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// InSelect: Tg=1
		frame := buildCommandFrame(cmdInSelect, []byte{0x01})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x55), cmd) // InSelect response
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(0x00), data[0]) // Success

		assert.Equal(t, 1, sim.GetState().SelectedTarget)
	})

	t.Run("Select_Invalid_Target", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// InSelect: Tg=5 (doesn't exist)
		frame := buildCommandFrame(cmdInSelect, []byte{0x05})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x55), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(errTarget), data[0]) // Error: target not found
	})

	t.Run("Select_Disappeared_Target", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		tag.Remove() // Mark as not present
		sim.AddTag(tag)

		frame := buildCommandFrame(cmdInSelect, []byte{0x01})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x55), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(errCardDisappeared), data[0])
	})

	t.Run("Select_Missing_Params", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// InSelect with no params
		frame := buildCommandFrame(cmdInSelect, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Positive(t, n) // Should get ACK + error frame
	})
}

func TestVirtualPN532_GetGeneralStatus(t *testing.T) {
	t.Parallel()

	t.Run("No_Target_Selected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		frame := buildCommandFrame(cmdGetGeneralStatus, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x05), cmd) // GetGeneralStatus response
		require.GreaterOrEqual(t, len(data), 4)
		assert.Equal(t, byte(0x00), data[0]) // Err = no error
		assert.Equal(t, byte(0x00), data[1]) // Field off
		assert.Equal(t, byte(0x00), data[2]) // NbTg = 0
	})

	t.Run("With_Target_And_RF_Field", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// Detect tag (turns on RF field and selects target)
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Now get general status
		frame = buildCommandFrame(cmdGetGeneralStatus, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x05), cmd)
		require.GreaterOrEqual(t, len(data), 4)
		assert.Equal(t, byte(0x00), data[0]) // Err = no error
		assert.Equal(t, byte(0x01), data[1]) // Field on
		assert.Equal(t, byte(0x01), data[2]) // NbTg = 1
		assert.Equal(t, byte(0x01), data[3]) // Tg = 1
	})
}

func TestVirtualPN532_SetParameters(t *testing.T) {
	t.Parallel()

	t.Run("Set_Parameters_Accepted", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// SetParameters with various flags
		frame := buildCommandFrame(cmdSetParameters, []byte{0x14})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x13), cmd) // SetParameters response
		assert.Empty(t, data)            // Empty = success
	})
}

func TestVirtualPN532_InCommunicateThru(t *testing.T) {
	t.Parallel()

	t.Run("No_Target_Selected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		frame := buildCommandFrame(cmdInCommunicateThru, []byte{0x30, 0x04})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x43), cmd) // InCommunicateThru response
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(errTarget), data[0]) // No target
	})

	t.Run("Communicate_With_Target", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// Detect tag first
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Communicate through - READ command
		frame = buildCommandFrame(cmdInCommunicateThru, []byte{0x30, 0x04})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x43), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(0x00), data[0]) // Success
	})

	t.Run("Target_Disappeared", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// Detect tag
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Remove the tag
		tag.Remove()

		// Try to communicate
		frame = buildCommandFrame(cmdInCommunicateThru, []byte{0x30, 0x04})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x43), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(errCardDisappeared), data[0])
	})
}

func TestVirtualPN532_SAMConfiguration_AllModes(t *testing.T) {
	t.Parallel()

	t.Run("Wired_Card_Mode", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		frame := buildCommandFrame(cmdSAMConfiguration, []byte{0x03})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x15), cmd)
		assert.Empty(t, data)

		assert.True(t, sim.GetState().SAMConfigured)
	})

	t.Run("Dual_Card_Mode", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		frame := buildCommandFrame(cmdSAMConfiguration, []byte{0x04})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x15), cmd)
		assert.Empty(t, data)

		assert.True(t, sim.GetState().SAMConfigured)
	})

	t.Run("Invalid_Mode_Rejected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		frame := buildCommandFrame(cmdSAMConfiguration, []byte{0x05}) // Invalid mode
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)

		// Should return error frame (ACK + error)
		assert.Positive(t, n)
		assert.False(t, sim.GetState().SAMConfigured)
	})

	t.Run("Missing_Params_Rejected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		frame := buildCommandFrame(cmdSAMConfiguration, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Positive(t, n) // ACK + error
	})
}

func TestVirtualPN532_InRelease_AllPaths(t *testing.T) {
	t.Parallel()

	t.Run("Release_All_Targets", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag1 := NewVirtualMIFARE1K(nil)
		tag2 := NewVirtualMIFARE1K(nil)
		sim.AddTag(tag1)
		sim.AddTag(tag2)

		// Detect tags
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x02, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Release all (Tg=0)
		frame = buildCommandFrame(cmdInRelease, []byte{0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x53), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(0x00), data[0])

		assert.Equal(t, -1, sim.GetState().SelectedTarget)
	})

	t.Run("Release_Missing_Params", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		frame := buildCommandFrame(cmdInRelease, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Positive(t, n) // ACK + error
	})
}

//nolint:funlen // Test functions often need many subtests
func TestVirtualPN532_TagCommands(t *testing.T) {
	t.Parallel()

	t.Run("Write_Block_Success", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// Detect tag
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Write to block 4: A2 04 [4 bytes data]
		writeData := []byte{0x01, 0xA2, 0x04, 0xDE, 0xAD, 0xBE, 0xEF}
		frame = buildCommandFrame(cmdInDataExchange, writeData)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x41), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(0x00), data[0]) // Success
	})

	t.Run("MIFARE_Auth_Success", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualMIFARE1K(nil)
		sim.AddTag(tag)

		// Detect tag
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Auth block 4 with key A: 60 04 [6 byte key]
		authData := []byte{0x01, 0x60, 0x04, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
		frame = buildCommandFrame(cmdInDataExchange, authData)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x41), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(0x00), data[0]) // Success
	})

	t.Run("MIFARE_Auth_Key_B", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualMIFARE1K(nil)
		sim.AddTag(tag)

		// Detect tag
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Auth block 4 with key B: 61 04 [6 byte key]
		authData := []byte{0x01, 0x61, 0x04, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
		frame = buildCommandFrame(cmdInDataExchange, authData)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x41), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(0x00), data[0]) // Success
	})

	t.Run("Invalid_Write_Command", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// Detect tag
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Invalid write (too short): A2 04 [only 2 bytes instead of 4]
		writeData := []byte{0x01, 0xA2, 0x04, 0xDE, 0xAD}
		frame = buildCommandFrame(cmdInDataExchange, writeData)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x41), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.NotEqual(t, byte(0x00), data[0]) // Error
	})

	t.Run("Invalid_Auth_Command", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualMIFARE1K(nil)
		sim.AddTag(tag)

		// Detect tag
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Invalid auth (too short): 60 04 [only 4 bytes instead of 6]
		authData := []byte{0x01, 0x60, 0x04, 0xFF, 0xFF, 0xFF, 0xFF}
		frame = buildCommandFrame(cmdInDataExchange, authData)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x41), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.NotEqual(t, byte(0x00), data[0]) // Error
	})

	t.Run("Empty_Command", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// Detect tag
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Empty data exchange (just target number, no actual command to tag)
		frame = buildCommandFrame(cmdInDataExchange, []byte{0x01})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		// This returns an error because processTagCommand gets empty cmd slice
		// Should get ACK + response with error status
		assert.Positive(t, n)

		// Skip ACK frame and check we got a response (error or frame)
		response := buf[len(ACKFrame):n]
		assert.GreaterOrEqual(t, len(response), 6, "should have response frame")
	})
}

func TestVirtualPN532_FeliCaDetection(t *testing.T) {
	t.Parallel()

	t.Run("FeliCa_212kbps_Detection", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Create FeliCa tag with 8-byte IDm
		tag := &VirtualTag{
			Type:    "FeliCa",
			UID:     []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
			Present: true,
		}
		sim.AddTag(tag)

		// InListPassiveTarget with BrTy=0x01 (212 kbps FeliCa)
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x01, 0x00, 0xFF, 0xFF, 0x00, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(0x01), data[0]) // NbTg = 1
	})

	t.Run("FeliCa_424kbps_Detection", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := &VirtualTag{
			Type:    "FeliCa",
			UID:     []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
			Present: true,
		}
		sim.AddTag(tag)

		// InListPassiveTarget with BrTy=0x02 (424 kbps FeliCa)
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x02, 0x00, 0xFF, 0xFF, 0x00, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.Equal(t, byte(0x01), data[0]) // NbTg = 1
	})

	t.Run("FeliCa_Short_UID_Rejected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// FeliCa tag with too-short UID (needs 8 bytes)
		tag := &VirtualTag{
			Type:    "FeliCa",
			UID:     []byte{0x01, 0x02, 0x03, 0x04},
			Present: true,
		}
		sim.AddTag(tag)

		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x01, 0x00, 0xFF, 0xFF, 0x00, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		assert.Equal(t, byte(0x00), data[0]) // NbTg = 0 (tag rejected)
	})
}

func TestVirtualPN532_MIFARE4K_Detection(t *testing.T) {
	t.Parallel()

	t.Run("MIFARE_4K_Detected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := &VirtualTag{
			Type:    "MIFARE4K",
			UID:     []byte{0x12, 0x34, 0x56, 0x78},
			Present: true,
		}
		sim.AddTag(tag)

		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		require.GreaterOrEqual(t, len(data), 6)
		assert.Equal(t, byte(0x01), data[0])           // NbTg = 1
		assert.Equal(t, []byte{0x02, 0x00}, data[2:4]) // SENS_RES for MIFARE 4K
		assert.Equal(t, byte(0x18), data[4])           // SEL_RES for MIFARE 4K
	})
}

func TestVirtualPN532_InvalidBrTy(t *testing.T) {
	t.Parallel()

	t.Run("BrTy_Out_Of_Range", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// BrTy = 0x05 is invalid (max is 0x04)
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x05})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)

		// Should return error
		assert.Positive(t, n)
	})
}

func TestVirtualPN532_InvalidTFI(t *testing.T) {
	t.Parallel()

	t.Run("Wrong_TFI_Rejected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Frame with wrong TFI (0xD5 instead of 0xD4)
		badFrame := []byte{0x00, 0x00, 0xFF, 0x02, 0xFE, 0xD5, 0x02, 0x29, 0x00}

		_, err := sim.Write(badFrame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Equal(t, 0, n) // Frame rejected
	})
}

func TestVirtualPN532_ReadEmptyBuffer(t *testing.T) {
	t.Parallel()

	t.Run("Read_Empty_Returns_Zero", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)
		assert.Equal(t, 0, n)
	})
}

func TestVirtualPN532_ProcessCommand_TooShort(t *testing.T) {
	t.Parallel()

	t.Run("Single_Byte_Frame_Data", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Valid frame structure but only TFI, no command byte
		// LEN=1, LCS=0xFF, TFI=0xD4, DCS=0x2C
		frame := []byte{0x00, 0x00, 0xFF, 0x01, 0xFF, 0xD4, 0x2C, 0x00}

		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)

		// Should get ACK + error frame
		assert.Positive(t, n)
	})
}

func TestVirtualPN532_TypeB_ISO14443B(t *testing.T) {
	t.Parallel()

	t.Run("TypeB_Not_Supported", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// InListPassiveTarget with BrTy=0x03 (106 kbps Type B)
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x03})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		assert.Equal(t, byte(0x00), data[0]) // NbTg = 0 (Type B returns nil)
	})
}

func TestVirtualPN532_JewelDetection(t *testing.T) {
	t.Parallel()

	t.Run("Jewel_Tag_Detected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := &VirtualTag{
			Type:    "Jewel",
			UID:     []byte{0x01, 0x02, 0x03, 0x04},
			Present: true,
		}
		sim.AddTag(tag)

		// InListPassiveTarget with BrTy=0x04 (Jewel)
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x04})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		assert.Equal(t, byte(0x01), data[0]) // NbTg = 1
	})

	t.Run("Jewel_Short_UID_Uses_Zeros", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := &VirtualTag{
			Type:    "Jewel",
			UID:     []byte{0x01, 0x02}, // Too short
			Present: true,
		}
		sim.AddTag(tag)

		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x04})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		assert.Equal(t, byte(0x01), data[0]) // NbTg = 1
	})
}

func TestVirtualPN532_InDataExchange_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("Wrong_Target_Number", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// Detect tag (selects target 1)
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Try data exchange with wrong target number (2 instead of 1)
		frame = buildCommandFrame(cmdInDataExchange, []byte{0x02, 0x30, 0x04})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x41), cmd)
		assert.Equal(t, byte(errTarget), data[0]) // Wrong target error
	})

	t.Run("Invalid_Read_Command", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// Detect tag
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Invalid read (just 0x30, no block number)
		frame = buildCommandFrame(cmdInDataExchange, []byte{0x01, 0x30})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x41), cmd)
		require.GreaterOrEqual(t, len(data), 1)
		assert.NotEqual(t, byte(0x00), data[0]) // Error
	})

	t.Run("Target_Index_Out_Of_Range", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Manually set selected target to a non-existent index
		sim.state.SelectedTarget = 5 // No tag at index 5

		frame := buildCommandFrame(cmdInDataExchange, []byte{0x05, 0x30, 0x04})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x41), cmd)
		assert.Equal(t, byte(errTarget), data[0])
	})
}

func TestVirtualPN532_RFConfiguration_NonFieldItems(t *testing.T) {
	t.Parallel()

	t.Run("Other_Config_Item", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// RFConfiguration with CfgItem=0x02 (various timings)
		frame := buildCommandFrame(cmdRFConfiguration, []byte{0x02, 0x00, 0x00, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x33), cmd)
		assert.Empty(t, data) // Success
	})

	t.Run("RF_Field_Missing_Data", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// RFConfiguration with CfgItem=0x01 but no field data
		frame := buildCommandFrame(cmdRFConfiguration, []byte{0x01})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Positive(t, n) // ACK + error frame
	})

	t.Run("Empty_Params", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		frame := buildCommandFrame(cmdRFConfiguration, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Positive(t, n) // ACK + error
	})
}

func TestVirtualPN532_PowerDown_MissingParams(t *testing.T) {
	t.Parallel()

	t.Run("Missing_WakeUpEnable", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		frame := buildCommandFrame(cmdPowerDown, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Positive(t, n) // ACK + error
	})
}

func TestVirtualPN532_ExtendedFrame_BadDCS(t *testing.T) {
	t.Parallel()

	t.Run("Extended_Frame_DCS_Error", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Extended frame with correct LCS but bad DCS
		badFrame := []byte{
			0x00, 0x00, 0xFF, 0xFF, 0xFF, // Preamble + start + extended marker
			0x00, 0x02, // Length = 2
			0xFE,       // Correct LCS
			0xD4, 0x02, // TFI + cmd
			0x00, // Bad DCS (should be 0x2A)
			0x00, // Postamble
		}

		_, err := sim.Write(badFrame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Equal(t, 0, n) // Frame rejected due to DCS error
	})

	t.Run("Extended_Frame_Bad_TFI", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Extended frame with wrong TFI
		badFrame := []byte{
			0x00, 0x00, 0xFF, 0xFF, 0xFF, // Preamble + start + extended marker
			0x00, 0x02, // Length = 2
			0xFE,       // Correct LCS
			0xD5, 0x02, // Wrong TFI (0xD5 instead of 0xD4)
			0x29, // DCS for 0xD5 + 0x02
			0x00, // Postamble
		}

		_, err := sim.Write(badFrame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)
		assert.Equal(t, 0, n) // Frame rejected due to wrong TFI
	})
}

func TestVirtualPN532_InCommunicateThru_InvalidTargetIndex(t *testing.T) {
	t.Parallel()

	t.Run("Target_Index_Out_Of_Range", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Manually set invalid target index
		sim.state.SelectedTarget = 10

		frame := buildCommandFrame(cmdInCommunicateThru, []byte{0x30, 0x04})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x43), cmd)
		assert.Equal(t, byte(errTarget), data[0])
	})
}

func TestVirtualPN532_UnknownCommand(t *testing.T) {
	t.Parallel()

	t.Run("Unknown_Command_Passthrough", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// Detect tag
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Send unknown tag command (0x99)
		frame = buildCommandFrame(cmdInDataExchange, []byte{0x01, 0x99})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x41), cmd)
		assert.Equal(t, byte(0x00), data[0]) // Unknown commands pass through with success
	})
}

// Tests for Power Glitch Simulation (Item 10)
func TestVirtualPN532_PowerGlitch(t *testing.T) {
	t.Parallel()

	t.Run("Power_Glitch_Truncates_Response", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Configure power glitch to occur after 3 bytes (middle of preamble/start)
		sim.SetPowerGlitch(3)

		// Send GetFirmwareVersion command
		frame := buildCommandFrame(cmdGetFirmwareVersion, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		// Should get ACK (6 bytes) + truncated response (3 bytes) = 9 bytes total
		assert.Equal(t, 9, n, "Expected ACK frame (6 bytes) + truncated response (3 bytes)")

		// Verify we got the ACK frame intact
		assert.True(t, bytes.HasPrefix(buf[:n], ACKFrame), "ACK frame should be complete")

		// The remaining 3 bytes should be truncated response (just preamble + start codes)
		truncatedPart := buf[6:9]
		expectedTruncated := []byte{0x00, 0x00, 0xFF} // Preamble + start codes
		assert.Equal(t, expectedTruncated, truncatedPart, "Response should be truncated after 3 bytes")
	})

	t.Run("Power_Glitch_Is_One_Shot", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Configure power glitch
		sim.SetPowerGlitch(5)

		// First command - should be truncated
		frame := buildCommandFrame(cmdGetFirmwareVersion, nil)
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		n1, _ := sim.Read(buf)

		// Second command - should be complete (glitch was one-shot)
		_, _ = sim.Write(frame)
		n2, _ := sim.Read(buf)

		// First response should be shorter (truncated)
		// Second response should be longer (complete: ACK + full response frame)
		assert.Greater(t, n2, n1, "Second response should be longer (not truncated)")

		// Verify second response has valid firmware version
		// ACK (6 bytes) + response frame (13 bytes: preamble+start1+start2+len+lcs+tfi+cmd+4data+dcs+postamble)
		// Frame: 1+1+1+1+1+6(data)+1+1 = 13 bytes (where data = TFI+CMD+IC+Ver+Rev+Support)
		expectedCompleteLen := 6 + 13
		assert.Equal(t, expectedCompleteLen, n2, "Second response should be complete")
	})

	t.Run("Power_Glitch_Zero_Disables", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Set and then disable power glitch
		sim.SetPowerGlitch(3)
		sim.SetPowerGlitch(0)

		// Command should complete normally
		frame := buildCommandFrame(cmdGetFirmwareVersion, nil)
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		// Should get complete response (ACK + response frame)
		// ACK (6 bytes) + response frame (13 bytes)
		expectedCompleteLen := 6 + 13
		assert.Equal(t, expectedCompleteLen, n, "Response should be complete when glitch disabled")
	})

	t.Run("Power_Glitch_At_Various_Points", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			name          string
			glitchAfter   int
			expectedTotal int // ACK (6) + truncated bytes
		}{
			{"Glitch_After_1_Byte", 1, 7},
			{"Glitch_After_5_Bytes", 5, 11},
			{"Glitch_After_8_Bytes", 8, 14},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				sim := NewVirtualPN532()
				sim.SetPowerGlitch(tc.glitchAfter)

				frame := buildCommandFrame(cmdGetFirmwareVersion, nil)
				_, _ = sim.Write(frame)

				buf := make([]byte, 256)
				n, _ := sim.Read(buf)

				assert.Equal(t, tc.expectedTotal, n, "Response should be truncated at expected point")
			})
		}
	})

	t.Run("Power_Glitch_Reset_Clears", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		sim.SetPowerGlitch(3)
		sim.Reset()

		// After reset, power glitch should be disabled
		frame := buildCommandFrame(cmdGetFirmwareVersion, nil)
		_, _ = sim.Write(frame)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)

		// Should get complete response
		// ACK (6 bytes) + response frame (13 bytes)
		expectedCompleteLen := 6 + 13
		assert.Equal(t, expectedCompleteLen, n, "Response should be complete after reset")
	})
}

// Tests for Multi-Tag Collision Mode (Item 11)
func TestVirtualPN532_CollisionMode(t *testing.T) {
	t.Parallel()

	t.Run("Collision_Mode_With_Multiple_Tags", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Add two tags
		tag1 := NewVirtualNTAG213([]byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
		tag2 := NewVirtualNTAG213([]byte{0x04, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})
		sim.AddTag(tag1)
		sim.AddTag(tag2)

		// Enable collision mode
		sim.SetCollisionMode(true)

		// Attempt to detect tags
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd, "Should be InListPassiveTarget response")
		assert.Equal(t, byte(0x00), data[0], "NbTg should be 0 due to collision")
	})

	t.Run("Collision_Mode_With_Single_Tag", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Add single tag
		tag := NewVirtualNTAG213(nil)
		sim.AddTag(tag)

		// Enable collision mode (should not affect single tag)
		sim.SetCollisionMode(true)

		// Detect tag - should succeed with single tag
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd, "Should be InListPassiveTarget response")
		assert.Equal(t, byte(0x01), data[0], "NbTg should be 1 - single tag detected")
	})

	t.Run("Collision_Mode_Disabled_Detects_Multiple", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Add two tags
		tag1 := NewVirtualNTAG213([]byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
		tag2 := NewVirtualNTAG213([]byte{0x04, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})
		sim.AddTag(tag1)
		sim.AddTag(tag2)

		// Keep collision mode disabled (default)

		// Request MaxTg=2 to detect both tags
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x02, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd, "Should be InListPassiveTarget response")
		assert.Equal(t, byte(0x02), data[0], "NbTg should be 2 - both tags detected")
	})

	t.Run("Collision_Mode_Reset_Clears", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		sim.SetCollisionMode(true)
		sim.Reset()

		// Add two tags after reset
		tag1 := NewVirtualNTAG213([]byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
		tag2 := NewVirtualNTAG213([]byte{0x04, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})
		sim.AddTag(tag1)
		sim.AddTag(tag2)

		// After reset, collision mode should be disabled
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x02, 0x00})
		_, _ = sim.Write(frame)

		buf := make([]byte, 256)
		n, _ := sim.Read(buf)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		assert.Equal(t, byte(0x02), data[0], "Should detect 2 tags after reset (collision mode cleared)")
	})
}

// Tests for Multi-Tag Detection (Item 11)
func TestVirtualPN532_MultiTagDetection(t *testing.T) {
	t.Parallel()

	t.Run("MaxTg_2_Detects_Two_Tags", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Add two NTAG213 tags with different UIDs
		tag1 := NewVirtualNTAG213([]byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
		tag2 := NewVirtualNTAG213([]byte{0x04, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})
		sim.AddTag(tag1)
		sim.AddTag(tag2)

		// Request MaxTg=2
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x02, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd, "Should be InListPassiveTarget response")
		assert.Equal(t, byte(0x02), data[0], "NbTg should be 2")

		// Parse first target data: Tg(1) + SENS_RES(2) + SEL_RES(1) + NFCIDLen(1) + UID(7)
		offset := 1 // Skip NbTg
		assert.Equal(t, byte(0x01), data[offset], "First tag number should be 1")

		// Skip to second tag (after first tag's data)
		// First tag: Tg(1) + SENS_RES(2) + SEL_RES(1) + NFCIDLen(1) + UID(7) = 12 bytes
		offset += 12
		assert.Equal(t, byte(0x02), data[offset], "Second tag number should be 2")
	})

	t.Run("MaxTg_1_Detects_First_Tag_Only", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Add two tags
		tag1 := NewVirtualNTAG213([]byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
		tag2 := NewVirtualNTAG213([]byte{0x04, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})
		sim.AddTag(tag1)
		sim.AddTag(tag2)

		// Request MaxTg=1
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x01, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		assert.Equal(t, byte(0x01), data[0], "NbTg should be 1 even though 2 tags present")
	})

	t.Run("Mixed_Tag_Types_Detection", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Add one NTAG and one MIFARE
		ntag := NewVirtualNTAG213([]byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
		mifare := NewVirtualMIFARE1K([]byte{0x12, 0x34, 0x56, 0x78})
		sim.AddTag(ntag)
		sim.AddTag(mifare)

		// Request MaxTg=2
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x02, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		assert.Equal(t, byte(0x02), data[0], "NbTg should be 2 with mixed tag types")
	})

	t.Run("Non_Present_Tags_Not_Detected", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Add two tags, but remove one
		tag1 := NewVirtualNTAG213([]byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
		tag2 := NewVirtualNTAG213([]byte{0x04, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})
		sim.AddTag(tag1)
		sim.AddTag(tag2)

		// Remove the second tag
		tag2.Remove()

		// Request MaxTg=2
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x02, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		assert.Equal(t, byte(0x01), data[0], "NbTg should be 1 - only present tags counted")
	})

	t.Run("Three_Tags_MaxTg_2_Returns_Only_Two", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Add three tags
		tag1 := NewVirtualNTAG213([]byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
		tag2 := NewVirtualNTAG213([]byte{0x04, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})
		tag3 := NewVirtualNTAG213([]byte{0x04, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44})
		sim.AddTag(tag1)
		sim.AddTag(tag2)
		sim.AddTag(tag3)

		// Request MaxTg=2 (PN532 max)
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x02, 0x00})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		buf := make([]byte, 256)
		n, err := sim.Read(buf)
		require.NoError(t, err)

		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x4B), cmd)
		assert.Equal(t, byte(0x02), data[0], "NbTg should be 2 even with 3 tags present")
	})

	t.Run("InSelect_Switches_Between_Tags", func(t *testing.T) {
		t.Parallel()
		sim := NewVirtualPN532()

		// Add two tags
		tag1 := NewVirtualNTAG213([]byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66})
		tag2 := NewVirtualNTAG213([]byte{0x04, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF})
		sim.AddTag(tag1)
		sim.AddTag(tag2)

		// Detect both tags
		frame := buildCommandFrame(cmdInListPassiveTarget, []byte{0x02, 0x00})
		_, _ = sim.Write(frame)
		buf := make([]byte, 256)
		_, _ = sim.Read(buf)

		// Initially tag 1 is selected
		assert.Equal(t, 1, sim.GetState().SelectedTarget)

		// Select tag 2
		frame = buildCommandFrame(cmdInSelect, []byte{0x02})
		_, err := sim.Write(frame)
		require.NoError(t, err)

		n, _ := sim.Read(buf)
		cmd, data := parseResponseFrame(t, buf[:n])
		assert.Equal(t, byte(0x55), cmd, "Should be InSelect response")
		assert.Equal(t, byte(0x00), data[0], "Status should be success")

		// Verify tag 2 is now selected
		assert.Equal(t, 2, sim.GetState().SelectedTarget)
	})
}
