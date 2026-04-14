//go:build linux

//nolint:paralleltest // Tests mutate package-level probeDeviceFn and findI2CBusesFn
package i2c

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/go-pn532/detection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveFns saves the package-level test hooks and registers a cleanup to restore them.
func saveFns(t *testing.T) {
	t.Helper()
	origProbe := probeDeviceFn
	origFind := findI2CBusesFn
	t.Cleanup(func() {
		probeDeviceFn = origProbe
		findI2CBusesFn = origFind
	})
}

func fakeBuses(paths ...string) func() ([]i2cBusInfo, error) {
	return func() ([]i2cBusInfo, error) {
		buses := make([]i2cBusInfo, len(paths))
		for idx, path := range paths {
			buses[idx] = i2cBusInfo{Path: path, Number: idx}
		}
		return buses, nil
	}
}

func TestDetectLinux_PassiveMode_ReturnsAllBusesWithoutProbing(t *testing.T) {
	saveFns(t)

	var probeCalled atomic.Bool
	findI2CBusesFn = fakeBuses("/dev/i2c-0", "/dev/i2c-1", "/dev/i2c-2")
	probeDeviceFn = func(context.Context, string, detection.Mode) bool {
		probeCalled.Store(true)
		return false
	}

	opts := &detection.Options{Mode: detection.Passive}
	devices, err := detectLinux(context.Background(), opts)
	require.NoError(t, err)
	assert.Len(t, devices, 3)
	for _, device := range devices {
		assert.Equal(t, detection.Low, device.Confidence)
	}
	assert.False(t, probeCalled.Load(), "probe must not be called in Passive mode")
}

func TestDetectLinux_PassiveMode_IgnorePathsRespected(t *testing.T) {
	saveFns(t)

	findI2CBusesFn = fakeBuses("/dev/i2c-0", "/dev/i2c-1", "/dev/i2c-2")
	probeDeviceFn = func(context.Context, string, detection.Mode) bool {
		return false
	}

	opts := &detection.Options{
		Mode:        detection.Passive,
		IgnorePaths: []string{"/dev/i2c-1"},
	}
	devices, err := detectLinux(context.Background(), opts)
	require.NoError(t, err)
	assert.Len(t, devices, 2)
	for _, device := range devices {
		assert.NotEqual(t, "/dev/i2c-1", device.Path)
	}
}

func TestDetectLinux_SafeMode_OnlyReturnsSuccessfulProbes(t *testing.T) {
	saveFns(t)

	findI2CBusesFn = fakeBuses("/dev/i2c-0", "/dev/i2c-1", "/dev/i2c-2")
	probeDeviceFn = func(_ context.Context, path string, _ detection.Mode) bool {
		return path == "/dev/i2c-1"
	}

	opts := &detection.Options{Mode: detection.Safe}
	devices, err := detectLinux(context.Background(), opts)
	require.NoError(t, err)
	require.Len(t, devices, 1)
	assert.Equal(t, "/dev/i2c-1", devices[0].Path)
	assert.Equal(t, detection.High, devices[0].Confidence)
}

func TestDetectLinux_SafeMode_ProbesInParallel(t *testing.T) {
	saveFns(t)

	const busCount = 5
	findI2CBusesFn = fakeBuses("/dev/i2c-0", "/dev/i2c-1", "/dev/i2c-2", "/dev/i2c-3", "/dev/i2c-4")

	// Barrier: all probes must arrive before any can return.
	// If probes run sequentially, the first blocks forever waiting for the
	// rest — the test timeout catches that.
	var arrived atomic.Int32
	gate := make(chan struct{})
	probeDeviceFn = func(_ context.Context, _ string, _ detection.Mode) bool {
		if arrived.Add(1) == int32(busCount) {
			close(gate)
		}
		<-gate
		return true
	}

	opts := &detection.Options{Mode: detection.Safe}
	devices, err := detectLinux(context.Background(), opts)
	require.NoError(t, err)
	assert.Len(t, devices, busCount)
}

func TestDetectLinux_SafeMode_IgnorePathsRespected(t *testing.T) {
	saveFns(t)

	findI2CBusesFn = fakeBuses("/dev/i2c-0", "/dev/i2c-1", "/dev/i2c-2")
	var probeCount atomic.Int32
	probeDeviceFn = func(_ context.Context, _ string, _ detection.Mode) bool {
		probeCount.Add(1)
		return true
	}

	opts := &detection.Options{
		Mode:        detection.Safe,
		IgnorePaths: []string{"/dev/i2c-1"},
	}
	devices, err := detectLinux(context.Background(), opts)
	require.NoError(t, err)
	assert.Len(t, devices, 2)
	assert.Equal(t, int32(2), probeCount.Load(), "ignored bus must not be probed")

	for _, device := range devices {
		assert.NotEqual(t, "/dev/i2c-1", device.Path)
	}
}

func TestDetectLinux_SafeMode_ContextCancellation(t *testing.T) {
	saveFns(t)

	findI2CBusesFn = fakeBuses("/dev/i2c-0", "/dev/i2c-1", "/dev/i2c-2", "/dev/i2c-3", "/dev/i2c-4")
	probeDeviceFn = func(ctx context.Context, _ string, _ detection.Mode) bool {
		<-ctx.Done()
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	opts := &detection.Options{Mode: detection.Safe}
	start := time.Now()
	_, err := detectLinux(ctx, opts)
	elapsed := time.Since(start)

	require.ErrorIs(t, err, detection.ErrDetectionTimeout)
	assert.Less(t, elapsed, 500*time.Millisecond,
		"should return promptly after context cancellation")
}

func TestDetectLinux_SafeMode_AllProbesFail_ReturnsError(t *testing.T) {
	saveFns(t)

	findI2CBusesFn = fakeBuses("/dev/i2c-0", "/dev/i2c-1", "/dev/i2c-2")
	probeDeviceFn = func(context.Context, string, detection.Mode) bool {
		return false
	}

	opts := &detection.Options{Mode: detection.Safe}
	_, err := detectLinux(context.Background(), opts)
	assert.ErrorIs(t, err, detection.ErrNoDevicesFound)
}

func TestDetectLinux_FullMode_ProbesDevices(t *testing.T) {
	saveFns(t)

	findI2CBusesFn = fakeBuses("/dev/i2c-0")
	var receivedMode detection.Mode
	probeDeviceFn = func(_ context.Context, _ string, mode detection.Mode) bool {
		receivedMode = mode
		return true
	}

	opts := &detection.Options{Mode: detection.Full}
	devices, err := detectLinux(context.Background(), opts)
	require.NoError(t, err)
	require.Len(t, devices, 1)
	assert.Equal(t, detection.High, devices[0].Confidence)
	assert.Equal(t, detection.Full, receivedMode)
}

func TestDetectLinux_NoBuses_ReturnsError(t *testing.T) {
	saveFns(t)

	findI2CBusesFn = fakeBuses()

	opts := &detection.Options{Mode: detection.Safe}
	_, err := detectLinux(context.Background(), opts)
	assert.ErrorIs(t, err, detection.ErrNoDevicesFound)
}
