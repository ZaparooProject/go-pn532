//go:build linux

package i2c

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/ZaparooProject/go-pn532/detection"
)

const (
	// I2CSlave is the ioctl command to set slave address
	I2CSlave = 0x0703

	// I2CFuncs is the ioctl command to get adapter functionality
	I2CFuncs = 0x0705

	// I2CFuncI2C indicates plain I2C support
	I2CFuncI2C = 0x00000001
)

// detectLinux searches for PN532 devices on Linux I2C buses
func detectLinux(ctx context.Context, opts *detection.Options) ([]detection.DeviceInfo, error) {
	// Find all I2C buses
	buses, err := findI2CBuses()
	if err != nil {
		return nil, err
	}

	if len(buses) == 0 {
		return nil, detection.ErrNoDevicesFound
	}

	var devices []detection.DeviceInfo

	for _, bus := range buses {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return devices, detection.ErrDetectionTimeout
		default:
		}

		busDevices, err := detectBusDevices(ctx, bus, opts)
		if err != nil {
			continue // Skip this bus on error
		}
		devices = append(devices, busDevices...)
	}

	if len(devices) == 0 {
		return nil, detection.ErrNoDevicesFound
	}

	return devices, nil
}

// detectBusDevices scans a single I2C bus for PN532 devices
func detectBusDevices(ctx context.Context, bus i2cBusInfo, opts *detection.Options) ([]detection.DeviceInfo, error) {
	// Scan bus for devices
	addresses := scanI2CBus(bus.Path)

	// Pre-allocate slice with capacity equal to the number of found addresses
	devices := make([]detection.DeviceInfo, 0, len(addresses))

	for _, addr := range addresses {
		device, skip := createDeviceInfo(ctx, bus.Path, addr, opts)
		if skip {
			continue
		}
		devices = append(devices, device)
	}

	return devices, nil
}

// createDeviceInfo creates a DeviceInfo for a single address
func createDeviceInfo(ctx context.Context, busPath string, addr uint8, opts *detection.Options) (
	detection.DeviceInfo, bool,
) {
	devicePath := fmt.Sprintf("%s:0x%02X", busPath, addr)

	// Skip explicitly ignored device paths
	if detection.IsPathIgnored(devicePath, opts.IgnorePaths) {
		return detection.DeviceInfo{}, true
	}

	// Check if this could be a PN532
	if addr != DefaultPN532Address && opts.Mode == detection.Passive {
		// In passive mode, only consider default PN532 address
		return detection.DeviceInfo{}, true
	}

	device := detection.DeviceInfo{
		Transport: "i2c",
		Path:      devicePath,
		Name:      fmt.Sprintf("I2C device at %s address 0x%02X", busPath, addr),
		Metadata: map[string]string{
			"bus":     busPath,
			"address": fmt.Sprintf("0x%02X", addr),
		},
	}

	// Determine confidence based on address and probing
	if addr == DefaultPN532Address {
		device.Confidence = detection.Medium
	} else {
		device.Confidence = detection.Low
	}

	// Probe if needed
	if opts.Mode != detection.Passive {
		probeCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		confirmed, metadata := probeI2CDevice(probeCtx, busPath, addr, opts.Mode)
		cancel()

		if confirmed {
			device.Confidence = detection.High
			// Merge metadata
			for k, v := range metadata {
				device.Metadata[k] = v
			}
		} else if device.Confidence == detection.Low {
			// Skip low confidence devices that don't respond
			return detection.DeviceInfo{}, true
		}
	}

	return device, false
}

// findI2CBuses discovers available I2C buses on the system
func findI2CBuses() ([]i2cBusInfo, error) {
	// Look for /dev/i2c-* devices
	matches, err := filepath.Glob("/dev/i2c-*")
	if err != nil {
		return nil, fmt.Errorf("failed to scan for I2C devices: %w", err)
	}

	// Pre-allocate slice with capacity equal to the number of glob matches
	buses := make([]i2cBusInfo, 0, len(matches))

	for _, path := range matches {
		// Extract bus number from path
		var busNum int
		if _, err := fmt.Sscanf(filepath.Base(path), "i2c-%d", &busNum); err != nil {
			continue
		}

		// Check if device is accessible
		if _, err := os.Stat(path); err != nil {
			continue
		}

		// Try to open to verify it's a valid I2C device
		fileDescriptor, err := syscall.Open(path, syscall.O_RDWR, 0)
		if err != nil {
			continue
		}

		// Check I2C functionality
		var funcs uint32
		// #nosec G103 -- unsafe pointer required for ioctl system call
		if err := ioctl(fileDescriptor, I2CFuncs, uintptr(unsafe.Pointer(&funcs))); err != nil {
			_ = syscall.Close(fileDescriptor)
			continue
		}
		_ = syscall.Close(fileDescriptor)

		// Check if it supports I2C
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

// scanI2CBus performs a quick scan to find devices on an I2C bus
func scanI2CBus(busPath string) []uint8 {
	var devices []uint8

	// Open I2C bus
	fileDescriptor, err := syscall.Open(busPath, syscall.O_RDWR, 0)
	if err != nil {
		return devices
	}
	defer func() { _ = syscall.Close(fileDescriptor) }()

	// Scan common I2C addresses (0x08 to 0x77)
	// Skip reserved addresses
	for addr := uint8(0x08); addr <= 0x77; addr++ {
		// Set slave address
		if err := ioctl(fileDescriptor, I2CSlave, uintptr(addr)); err != nil {
			continue
		}

		// Try to read one byte
		buf := make([]byte, 1)
		if _, err := syscall.Read(fileDescriptor, buf); err == nil {
			devices = append(devices, addr)
		}
	}

	return devices
}

// ioctl performs an ioctl system call
func ioctl(fd int, request uint, arg uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(request), arg)
	if errno != 0 {
		return errno
	}
	return nil
}
