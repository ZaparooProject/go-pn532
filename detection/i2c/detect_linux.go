//go:build linux

package i2c

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	pn532 "github.com/ZaparooProject/go-pn532"
	"github.com/ZaparooProject/go-pn532/detection"
	i2ctransport "github.com/ZaparooProject/go-pn532/transport/i2c"
)

const (
	I2CSlave   = 0x0703
	I2CFuncs   = 0x0705
	I2CFuncI2C = 0x00000001
)

type i2cBusInfo struct {
	Path   string
	Number int
}

// probeDeviceFn is the function used to probe I2C devices during detection.
// Defaults to probeDeviceImpl; overridden in tests.
var probeDeviceFn = probeDeviceImpl

// findI2CBusesFn is the function used to enumerate I2C buses.
// Defaults to findI2CBuses; overridden in tests.
var findI2CBusesFn = findI2CBuses

// detectLinux searches for PN532 devices on Linux I2C buses.
// In Passive mode, returns all valid buses as Low confidence candidates.
// In Safe/Full modes, probes each bus in parallel and returns only confirmed
// devices as High confidence candidates.
func detectLinux(ctx context.Context, opts *detection.Options) ([]detection.DeviceInfo, error) {
	buses, err := findI2CBusesFn()
	if err != nil {
		return nil, err
	}

	if len(buses) == 0 {
		return nil, detection.ErrNoDevicesFound
	}

	filtered := filterBusesByIgnorePaths(buses, opts.IgnorePaths)
	if len(filtered) == 0 {
		return nil, detection.ErrNoDevicesFound
	}

	var devices []detection.DeviceInfo
	if opts.Mode >= detection.Safe {
		devices = probeI2CBuses(ctx, filtered, opts.Mode)
	} else {
		devices = buildPassiveDevices(filtered)
	}

	if len(devices) == 0 {
		if ctx.Err() != nil {
			return nil, detection.ErrDetectionTimeout
		}
		return nil, detection.ErrNoDevicesFound
	}

	return devices, nil
}

// filterBusesByIgnorePaths removes buses whose paths are in the ignore list.
func filterBusesByIgnorePaths(buses []i2cBusInfo, ignorePaths []string) []i2cBusInfo {
	filtered := make([]i2cBusInfo, 0, len(buses))
	for _, bus := range buses {
		if !detection.IsPathIgnored(bus.Path, ignorePaths) {
			filtered = append(filtered, bus)
		}
	}
	return filtered
}

// buildPassiveDevices returns all buses as Low confidence candidates without probing.
func buildPassiveDevices(buses []i2cBusInfo) []detection.DeviceInfo {
	devices := make([]detection.DeviceInfo, 0, len(buses))
	for _, bus := range buses {
		devices = append(devices, makeDeviceInfo(bus.Path, detection.Low))
	}
	return devices
}

// probeI2CBuses probes each bus in parallel and returns only confirmed devices.
// Context cancellation is best-effort: the initial transport open does not accept
// a context, so goroutines may block on kernel I2C setup until it completes.
func probeI2CBuses(ctx context.Context, buses []i2cBusInfo, mode detection.Mode) []detection.DeviceInfo {
	results := make(chan detection.DeviceInfo, len(buses))

	var wg sync.WaitGroup
	for _, bus := range buses {
		wg.Add(1)
		go func(busPath string) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				return
			default:
			}

			if probeDeviceFn(ctx, busPath, mode) {
				pn532.Debugf("i2c: probe succeeded on %s", busPath)
				results <- makeDeviceInfo(busPath, detection.High)
			} else {
				pn532.Debugf("i2c: probe failed on %s, skipping", busPath)
			}
		}(bus.Path)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var devices []detection.DeviceInfo
	for device := range results {
		devices = append(devices, device)
	}
	return devices
}

// makeDeviceInfo builds a DeviceInfo for an I2C bus.
func makeDeviceInfo(busPath string, confidence detection.Confidence) detection.DeviceInfo {
	return detection.DeviceInfo{
		Transport:  "i2c",
		Path:       busPath,
		Name:       fmt.Sprintf("I2C bus %s", busPath),
		Confidence: confidence,
		Metadata: map[string]string{
			"bus":     busPath,
			"address": fmt.Sprintf("0x%02X", DefaultPN532Address),
		},
	}
}

// probeDeviceImpl attempts to communicate with a device to verify it's a PN532.
//
// NO RETRY POLICY: This function intentionally performs only a single attempt.
// Connection retries are handled at the device level for known PN532 paths,
// not during auto-detection.
func probeDeviceImpl(ctx context.Context, busPath string, mode detection.Mode) bool {
	transport, err := i2ctransport.New(busPath)
	if err != nil {
		return false
	}
	defer func() { _ = transport.Close() }()

	device, err := pn532.New(transport)
	if err != nil {
		return false
	}

	switch mode {
	case detection.Safe:
		_, err = device.GetFirmwareVersion(ctx)
		return err == nil
	case detection.Full:
		return device.Init(ctx) == nil
	case detection.Passive:
		return false
	}

	return false
}

// findI2CBuses discovers available I2C buses on the system.
func findI2CBuses() ([]i2cBusInfo, error) {
	matches, err := filepath.Glob("/dev/i2c-*")
	if err != nil {
		return nil, fmt.Errorf("failed to scan for I2C devices: %w", err)
	}

	buses := make([]i2cBusInfo, 0, len(matches))

	for _, path := range matches {
		var busNum int
		if _, err := fmt.Sscanf(filepath.Base(path), "i2c-%d", &busNum); err != nil {
			continue
		}

		if _, err := os.Stat(path); err != nil {
			continue
		}

		fd, err := syscall.Open(path, syscall.O_RDWR, 0)
		if err != nil {
			continue
		}

		var funcs uint64
		// #nosec G103 -- unsafe pointer required for ioctl system call
		if err := ioctl(fd, I2CFuncs, uintptr(unsafe.Pointer(&funcs))); err != nil {
			_ = syscall.Close(fd)
			continue
		}
		_ = syscall.Close(fd)

		if funcs&I2CFuncI2C == 0 {
			continue
		}

		buses = append(buses, i2cBusInfo{
			Path:   path,
			Number: busNum,
		})
	}

	return buses, nil
}

func ioctl(fd int, request uint, arg uintptr) error {
	// #nosec G115 -- fd is a valid file descriptor (non-negative)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(request), arg)
	if errno != 0 {
		return errno
	}
	return nil
}
