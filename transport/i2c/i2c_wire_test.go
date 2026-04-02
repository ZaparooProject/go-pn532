//nolint:paralleltest // Test file - parallel tests add complexity
package i2c

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/ZaparooProject/go-pn532"
	virt "github.com/ZaparooProject/go-pn532/internal/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/physic"
)

var errBusClosed = errors.New("bus is closed")

// MockI2CBus implements i2c.Bus interface backed by VirtualPN532.
type MockI2CBus struct {
	sim       *virt.VirtualPN532
	closed    bool
	lastReady byte // For ready status simulation
}

// NewMockI2CBus creates a new mock I2C bus wrapping the VirtualPN532 simulator.
func NewMockI2CBus(sim *virt.VirtualPN532) *MockI2CBus {
	return &MockI2CBus{
		sim:       sim,
		lastReady: 0x00, // Not ready initially
	}
}

// Tx implements i2c.Bus.Tx - performs I2C transaction.
// For PN532, we need to handle the ready status check and frame exchanges.
//
//nolint:gocognit,gocyclo,revive,cyclop,varnamelen // Mock implementation requires complex logic to simulate PN532 behavior
func (m *MockI2CBus) Tx(_ uint16, w, r []byte) error {
	if m.closed {
		return errBusClosed
	}

	// Handle ready status check (read-only with single byte)
	if len(w) == 0 && len(r) == 1 {
		// Check if simulator has data ready
		if m.sim.HasPendingResponse() {
			r[0] = pn532Ready // Ready
		} else {
			r[0] = 0x00 // Not ready
		}
		return nil
	}

	// Handle write operation (sending frame to PN532)
	if len(w) > 0 && len(r) == 0 {
		_, err := m.sim.Write(w)
		if err != nil {
			return fmt.Errorf("mock i2c write: %w", err)
		}
		return nil
	}

	// Handle read operation (receiving frame from PN532)
	if len(w) == 0 && len(r) > 0 {
		n, err := m.sim.Read(r)
		if err != nil {
			return fmt.Errorf("mock i2c read: %w", err)
		}
		// Clear remaining bytes if we read less
		for i := n; i < len(r); i++ {
			r[i] = 0x00
		}
		return nil
	}

	// Handle combined write-read (not typically used by PN532)
	if len(w) > 0 && len(r) > 0 {
		_, err := m.sim.Write(w)
		if err != nil {
			return fmt.Errorf("mock i2c write: %w", err)
		}
		n, err := m.sim.Read(r)
		if err != nil {
			return fmt.Errorf("mock i2c read: %w", err)
		}
		for i := n; i < len(r); i++ {
			r[i] = 0x00
		}
		return nil
	}

	return nil
}

// SetSpeed implements i2c.Bus (no-op for mock).
func (*MockI2CBus) SetSpeed(_ physic.Frequency) error {
	return nil
}

// Close closes the mock bus.
func (m *MockI2CBus) Close() error {
	m.closed = true
	return nil
}

// String returns the bus name.
func (*MockI2CBus) String() string {
	return "mock://i2c"
}

var _ i2c.Bus = (*MockI2CBus)(nil)

// newTestI2CTransport creates a Transport using the mock I2C bus.
func newTestI2CTransport(sim *virt.VirtualPN532) *Transport {
	mockBus := NewMockI2CBus(sim)
	dev := &i2c.Dev{Addr: pn532Addr, Bus: mockBus}
	return &Transport{
		dev:     dev,
		busName: "mock://i2c",
		timeout: 100 * time.Millisecond,
	}
}

// --- Basic Protocol Tests ---

func TestI2C_GetFirmwareVersion(t *testing.T) {
	sim := virt.NewVirtualPN532()
	sim.SetFirmwareVersion(0x32, 0x01, 0x06, 0x07)
	transport := newTestI2CTransport(sim)

	resp, err := transport.SendCommand(context.Background(), 0x02, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Response format: [ResponseCode (cmd+1), IC, Ver, Rev, Support]
	assert.Len(t, resp, 5)
	assert.Equal(t, byte(0x03), resp[0], "Response code should be 0x03")
	assert.Equal(t, byte(0x32), resp[1], "IC should be 0x32 (PN532)")
	assert.Equal(t, byte(0x01), resp[2], "Version should be 0x01")
	assert.Equal(t, byte(0x06), resp[3], "Revision should be 0x06")
	assert.Equal(t, byte(0x07), resp[4], "Support should be 0x07")
}

func TestI2C_SAMConfiguration(t *testing.T) {
	sim := virt.NewVirtualPN532()
	transport := newTestI2CTransport(sim)

	// SAMConfiguration: Mode=0x01 (Normal), Timeout=0x14, IRQ=0x01
	resp, err := transport.SendCommand(context.Background(), 0x14, []byte{0x01, 0x14, 0x01})
	require.NoError(t, err)

	// Response: [ResponseCode] only
	assert.Len(t, resp, 1)
	assert.Equal(t, byte(0x15), resp[0], "Response code should be 0x15")
}

func TestI2C_InListPassiveTarget_NoTags(t *testing.T) {
	sim := virt.NewVirtualPN532()
	transport := newTestI2CTransport(sim)

	// InListPassiveTarget: MaxTg=1, BrTy=0x00 (106kbps Type A)
	resp, err := transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x00})
	require.NoError(t, err)

	// Response: [ResponseCode, NbTg=0]
	assert.Len(t, resp, 2)
	assert.Equal(t, byte(0x4B), resp[0], "Response code should be 0x4B")
	assert.Equal(t, byte(0x00), resp[1], "NbTg should be 0 (no tags)")
}

func TestI2C_InListPassiveTarget_WithTag(t *testing.T) {
	sim := virt.NewVirtualPN532()
	tag := virt.NewVirtualNTAG213([]byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	sim.SetTag(tag)
	transport := newTestI2CTransport(sim)

	// InListPassiveTarget: MaxTg=1, BrTy=0x00 (106kbps Type A)
	resp, err := transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x00})
	require.NoError(t, err)

	// Response: [ResponseCode, NbTg, Tg, SENS_RES(2), SEL_RES, NFCIDLength, NFCID...]
	assert.GreaterOrEqual(t, len(resp), 8)
	assert.Equal(t, byte(0x4B), resp[0], "Response code should be 0x4B")
	assert.Equal(t, byte(0x01), resp[1], "NbTg should be 1")
	assert.Equal(t, byte(0x01), resp[2], "Tg should be 1")
}

func TestI2C_InDataExchange_Read(t *testing.T) {
	sim := virt.NewVirtualPN532()
	tag := virt.NewVirtualNTAG213([]byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	sim.SetTag(tag)
	transport := newTestI2CTransport(sim)

	// First select the tag
	_, err := transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x00})
	require.NoError(t, err)

	// InDataExchange: Tg=1, READ command (0x30), page 0
	resp, err := transport.SendCommand(context.Background(), 0x40, []byte{0x01, 0x30, 0x00})
	require.NoError(t, err)

	// Response: [ResponseCode, Status, Data...]
	assert.GreaterOrEqual(t, len(resp), 2)
	assert.Equal(t, byte(0x41), resp[0], "Response code should be 0x41")
	assert.Equal(t, byte(0x00), resp[1], "Status should be 0x00 (success)")
}

func TestI2C_InDataExchange_Write(t *testing.T) {
	sim := virt.NewVirtualPN532()
	tag := virt.NewVirtualNTAG213([]byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	sim.SetTag(tag)
	transport := newTestI2CTransport(sim)

	// Select tag
	_, err := transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x00})
	require.NoError(t, err)

	// InDataExchange: Tg=1, WRITE command (0xA2), page 4, data
	resp, err := transport.SendCommand(context.Background(), 0x40, []byte{0x01, 0xA2, 0x04, 0xDE, 0xAD, 0xBE, 0xEF})
	require.NoError(t, err)

	// Response: [ResponseCode, Status]
	assert.GreaterOrEqual(t, len(resp), 2)
	assert.Equal(t, byte(0x41), resp[0])
	assert.Equal(t, byte(0x00), resp[1], "Write should succeed")
}

// --- MIFARE Tests ---

func TestI2C_MIFARE_Authentication(t *testing.T) {
	sim := virt.NewVirtualPN532()
	tag := virt.NewVirtualMIFARE1K([]byte{0x01, 0x02, 0x03, 0x04})
	sim.SetTag(tag)
	transport := newTestI2CTransport(sim)

	// Select tag
	_, err := transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x00})
	require.NoError(t, err)

	// InDataExchange: MIFARE Auth with Key A
	authCmd := []byte{
		0x01,                               // Tg
		0x60,                               // Auth Key A
		0x04,                               // Block number
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // Key (default)
		0x01, 0x02, 0x03, 0x04, // UID
	}
	resp, err := transport.SendCommand(context.Background(), 0x40, authCmd)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(resp), 2)
	assert.Equal(t, byte(0x41), resp[0])
	assert.Equal(t, byte(0x00), resp[1], "Auth should succeed")
}

func TestI2C_MIFARE_ReadAfterAuth(t *testing.T) {
	sim := virt.NewVirtualPN532()
	tag := virt.NewVirtualMIFARE1K([]byte{0x01, 0x02, 0x03, 0x04})
	sim.SetTag(tag)
	transport := newTestI2CTransport(sim)

	// Select tag
	_, err := transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x00})
	require.NoError(t, err)

	// Authenticate
	authCmd := []byte{
		0x01, 0x60, 0x04,
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		0x01, 0x02, 0x03, 0x04,
	}
	_, err = transport.SendCommand(context.Background(), 0x40, authCmd)
	require.NoError(t, err)

	// Read block 4
	resp, err := transport.SendCommand(context.Background(), 0x40, []byte{0x01, 0x30, 0x04})
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(resp), 2)
	assert.Equal(t, byte(0x41), resp[0])
	assert.Equal(t, byte(0x00), resp[1])
}

// --- FeliCa Tests ---

func TestI2C_FeliCa_Detection(t *testing.T) {
	sim := virt.NewVirtualPN532()
	tag := &virt.VirtualTag{
		Type:    "FeliCa",
		UID:     []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
		Present: true,
	}
	sim.SetTag(tag)
	transport := newTestI2CTransport(sim)

	// InListPassiveTarget for FeliCa: BrTy=0x01 (212kbps FeliCa)
	resp, err := transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x01, 0x00, 0xFF, 0xFF, 0x00, 0x00})
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(resp), 2)
	assert.Equal(t, byte(0x4B), resp[0])
	assert.Equal(t, byte(0x01), resp[1], "Should detect FeliCa tag")
}

// --- Control Commands ---

func TestI2C_RFConfiguration(t *testing.T) {
	sim := virt.NewVirtualPN532()
	transport := newTestI2CTransport(sim)

	// RFConfiguration: MaxRetries
	resp, err := transport.SendCommand(context.Background(), 0x32, []byte{0x05, 0xFF, 0x01, 0x01})
	require.NoError(t, err)

	assert.Len(t, resp, 1)
	assert.Equal(t, byte(0x33), resp[0])
}

func TestI2C_InRelease(t *testing.T) {
	sim := virt.NewVirtualPN532()
	tag := virt.NewVirtualNTAG213([]byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	sim.SetTag(tag)
	transport := newTestI2CTransport(sim)

	// Select tag first
	_, err := transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x00})
	require.NoError(t, err)

	// InRelease: Tg=0 (release all)
	resp, err := transport.SendCommand(context.Background(), 0x52, []byte{0x00})
	require.NoError(t, err)

	assert.Len(t, resp, 2)
	assert.Equal(t, byte(0x53), resp[0])
	assert.Equal(t, byte(0x00), resp[1])
}

func TestI2C_PowerDown(t *testing.T) {
	sim := virt.NewVirtualPN532()
	transport := newTestI2CTransport(sim)

	// Send PowerDown with WakeUpEnable byte set to 0x00
	resp, err := transport.SendCommand(context.Background(), 0x16, []byte{0x00})
	require.NoError(t, err)

	assert.Len(t, resp, 2)
	assert.Equal(t, byte(0x17), resp[0])
	assert.Equal(t, byte(0x00), resp[1])
}

// --- Error Handling Tests ---

func TestI2C_BusClosed(t *testing.T) {
	sim := virt.NewVirtualPN532()
	transport := newTestI2CTransport(sim)

	// Close the underlying bus
	if bus, ok := transport.dev.Bus.(*MockI2CBus); ok {
		_ = bus.Close()
	}

	_, err := transport.SendCommand(context.Background(), 0x02, nil)
	require.Error(t, err)
}

func TestI2C_IsConnected(t *testing.T) {
	sim := virt.NewVirtualPN532()
	transport := newTestI2CTransport(sim)

	assert.True(t, transport.IsConnected())
}

func TestI2C_Type(t *testing.T) {
	sim := virt.NewVirtualPN532()
	transport := newTestI2CTransport(sim)

	assert.Equal(t, pn532.TransportI2C, transport.Type())
}

func TestI2C_SetTimeout(t *testing.T) {
	sim := virt.NewVirtualPN532()
	transport := newTestI2CTransport(sim)

	err := transport.SetTimeout(200 * time.Millisecond)
	require.NoError(t, err)
	assert.Equal(t, 200*time.Millisecond, transport.timeout)
}

func TestI2C_Close(t *testing.T) {
	sim := virt.NewVirtualPN532()
	transport := newTestI2CTransport(sim)

	err := transport.Close()
	require.NoError(t, err)
}

// --- Jittery Transport Tests ---
// These tests verify I2C robustness under simulated real-world conditions
// like USB-I2C bridges (FT232H, MCP2221) with unpredictable timing.

// JitteryMockI2CBus wraps a VirtualPN532 with jittery read behavior.
type JitteryMockI2CBus struct {
	jittery   *virt.BufferedJitteryConnection
	sim       *virt.VirtualPN532
	closed    bool
	lastReady byte
}

// NewJitteryMockI2CBus creates a jittery I2C bus for stress testing.
func NewJitteryMockI2CBus(sim *virt.VirtualPN532, config virt.JitterConfig) *JitteryMockI2CBus {
	return &JitteryMockI2CBus{
		jittery:   virt.NewBufferedJitteryConnection(sim, config),
		sim:       sim,
		lastReady: 0x00,
	}
}

// Tx implements i2c.Bus.Tx with jittery read behavior.
//
//nolint:gocognit,gocyclo,revive,cyclop,varnamelen // Mock implementation requires complex logic
func (m *JitteryMockI2CBus) Tx(_ uint16, w, r []byte) error {
	if m.closed {
		return errBusClosed
	}

	// Handle ready status check (read-only with single byte)
	if len(w) == 0 && len(r) == 1 {
		if m.sim.HasPendingResponse() {
			r[0] = pn532Ready
		} else {
			r[0] = 0x00
		}
		return nil
	}

	// Handle write operation
	if len(w) > 0 && len(r) == 0 {
		_, err := m.jittery.Write(w)
		if err != nil {
			return fmt.Errorf("jittery i2c write: %w", err)
		}
		return nil
	}

	// Handle read operation with jittery behavior
	if len(w) == 0 && len(r) > 0 {
		totalRead := 0
		for totalRead < len(r) {
			n, err := m.jittery.Read(r[totalRead:])
			if err != nil {
				return fmt.Errorf("jittery i2c read: %w", err)
			}
			if n == 0 {
				break // No more data
			}
			totalRead += n
		}
		// Clear remaining bytes
		for i := totalRead; i < len(r); i++ {
			r[i] = 0x00
		}
		return nil
	}

	// Handle combined write-read
	if len(w) > 0 && len(r) > 0 {
		_, err := m.jittery.Write(w)
		if err != nil {
			return fmt.Errorf("jittery i2c write: %w", err)
		}
		totalRead := 0
		for totalRead < len(r) {
			n, err := m.jittery.Read(r[totalRead:])
			if err != nil {
				return fmt.Errorf("jittery i2c read: %w", err)
			}
			if n == 0 {
				break
			}
			totalRead += n
		}
		for i := totalRead; i < len(r); i++ {
			r[i] = 0x00
		}
		return nil
	}

	return nil
}

func (*JitteryMockI2CBus) SetSpeed(_ physic.Frequency) error { return nil }
func (m *JitteryMockI2CBus) Close() error                    { m.closed = true; return nil }
func (*JitteryMockI2CBus) String() string                    { return "mock://jittery-i2c" }

var _ i2c.Bus = (*JitteryMockI2CBus)(nil)

// newJitteryTestI2CTransport creates a Transport with jittery I2C bus.
func newJitteryTestI2CTransport(sim *virt.VirtualPN532, config virt.JitterConfig) *Transport {
	mockBus := NewJitteryMockI2CBus(sim, config)
	dev := &i2c.Dev{Addr: pn532Addr, Bus: mockBus}
	// Use longer timeout on Windows due to less predictable goroutine scheduling
	timeout := 500 * time.Millisecond
	if runtime.GOOS == "windows" {
		timeout = 1500 * time.Millisecond
	}
	return &Transport{
		dev:     dev,
		busName: "mock://jittery-i2c",
		timeout: timeout,
	}
}

// defaultI2CJitterConfig returns a jitter config for I2C stress testing.
func defaultI2CJitterConfig() virt.JitterConfig {
	return virt.JitterConfig{
		MaxLatencyMs:     0,
		FragmentReads:    true,
		FragmentMinBytes: 1,
		Seed:             12345,
	}
}

func TestI2C_Jittery_GetFirmwareVersion(t *testing.T) {
	sim := virt.NewVirtualPN532()
	sim.SetFirmwareVersion(0x32, 0x01, 0x06, 0x07)
	transport := newJitteryTestI2CTransport(sim, defaultI2CJitterConfig())

	resp, err := transport.SendCommand(context.Background(), 0x02, nil)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Len(t, resp, 5)
	assert.Equal(t, byte(0x03), resp[0])
	assert.Equal(t, byte(0x32), resp[1])
}

func TestI2C_Jittery_SAMConfiguration(t *testing.T) {
	sim := virt.NewVirtualPN532()
	transport := newJitteryTestI2CTransport(sim, defaultI2CJitterConfig())

	resp, err := transport.SendCommand(context.Background(), 0x14, []byte{0x01, 0x14, 0x01})
	require.NoError(t, err)
	require.NotNil(t, resp)

	state := sim.GetState()
	assert.True(t, state.SAMConfigured)
}

func TestI2C_Jittery_TagDetection(t *testing.T) {
	sim := virt.NewVirtualPN532()
	tag := virt.NewVirtualNTAG213([]byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	sim.AddTag(tag)
	transport := newJitteryTestI2CTransport(sim, defaultI2CJitterConfig())

	// Detect tag
	resp, err := transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x00})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(resp), 7)
	assert.Equal(t, byte(0x4B), resp[0])
	assert.Equal(t, byte(0x01), resp[1])
}

func TestI2C_Jittery_ReadWriteCycle(t *testing.T) {
	sim := virt.NewVirtualPN532()
	tag := virt.NewVirtualNTAG213([]byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	sim.AddTag(tag)
	transport := newJitteryTestI2CTransport(sim, defaultI2CJitterConfig())

	// Detect tag
	_, err := transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x00})
	require.NoError(t, err)

	// Write data
	resp, err := transport.SendCommand(context.Background(), 0x40, []byte{0x01, 0xA2, 0x04, 0xAA, 0xBB, 0xCC, 0xDD})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(resp), 2)
	assert.Equal(t, byte(0x00), resp[1])

	// Read it back
	resp, err = transport.SendCommand(context.Background(), 0x40, []byte{0x01, 0x30, 0x04})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(resp), 6)
	assert.Equal(t, byte(0xAA), resp[2])
	assert.Equal(t, byte(0xBB), resp[3])
	assert.Equal(t, byte(0xCC), resp[4])
	assert.Equal(t, byte(0xDD), resp[5])
}

func TestI2C_Jittery_MIFAREAuth(t *testing.T) {
	sim := virt.NewVirtualPN532()
	tag := virt.NewVirtualMIFARE1K([]byte{0x01, 0x02, 0x03, 0x04})
	sim.AddTag(tag)
	transport := newJitteryTestI2CTransport(sim, defaultI2CJitterConfig())

	// Detect tag
	_, err := transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x00})
	require.NoError(t, err)

	// Authenticate
	authCmd := []byte{
		0x01, 0x60, 0x04,
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		0x01, 0x02, 0x03, 0x04,
	}
	resp, err := transport.SendCommand(context.Background(), 0x40, authCmd)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(resp), 2)
	assert.Equal(t, byte(0x00), resp[1])
}

func TestI2C_Jittery_MultipleCommands(t *testing.T) {
	sim := virt.NewVirtualPN532()
	tag := virt.NewVirtualNTAG213([]byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	sim.AddTag(tag)
	transport := newJitteryTestI2CTransport(sim, defaultI2CJitterConfig())

	// Run 10 command cycles
	for i := range 10 {
		// Get firmware version
		resp, err := transport.SendCommand(context.Background(), 0x02, nil)
		require.NoError(t, err, "GetFirmwareVersion failed on iteration %d", i)
		assert.Len(t, resp, 5)

		// Detect tag
		resp, err = transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x00})
		require.NoError(t, err, "InListPassiveTarget failed on iteration %d", i)
		assert.Equal(t, byte(0x01), resp[1])

		// Release tag
		_, err = transport.SendCommand(context.Background(), 0x52, []byte{0x01})
		require.NoError(t, err, "InRelease failed on iteration %d", i)
	}
}

func TestI2C_Jittery_USBBoundaryStress(t *testing.T) {
	sim := virt.NewVirtualPN532()
	tag := virt.NewVirtualNTAG213([]byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	sim.AddTag(tag)

	config := virt.JitterConfig{
		MaxLatencyMs:      0,
		FragmentReads:     false,
		USBBoundaryStress: true,
		Seed:              54321,
	}
	transport := newJitteryTestI2CTransport(sim, config)

	// Detect tag
	resp, err := transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x00})
	require.NoError(t, err)
	assert.Equal(t, byte(0x01), resp[1])

	// Read data
	resp, err = transport.SendCommand(context.Background(), 0x40, []byte{0x01, 0x30, 0x04})
	require.NoError(t, err)
	assert.Equal(t, byte(0x00), resp[1])
}

func TestI2C_Jittery_AggressiveFragmentation(t *testing.T) {
	sim := virt.NewVirtualPN532()
	tag := virt.NewVirtualNTAG213([]byte{0x04, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06})
	sim.AddTag(tag)

	config := virt.JitterConfig{
		MaxLatencyMs:     0,
		FragmentReads:    true,
		FragmentMinBytes: 1,
		Seed:             11111,
	}
	transport := newJitteryTestI2CTransport(sim, config)

	// Get firmware
	resp, err := transport.SendCommand(context.Background(), 0x02, nil)
	require.NoError(t, err)
	assert.Len(t, resp, 5)

	// Detect tag
	resp, err = transport.SendCommand(context.Background(), 0x4A, []byte{0x01, 0x00})
	require.NoError(t, err)
	assert.Equal(t, byte(0x01), resp[1])

	// Read from tag
	resp, err = transport.SendCommand(context.Background(), 0x40, []byte{0x01, 0x30, 0x04})
	require.NoError(t, err)
	assert.Equal(t, byte(0x00), resp[1])
}
