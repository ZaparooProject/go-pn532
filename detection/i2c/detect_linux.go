//go:build linux

package i2c

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/ZaparooProject/go-pn532/detection"
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

// detectLinux searches for PN532 devices on Linux I2C buses.
// Only probes the known PN532 address (0x24) on each bus — does NOT scan
// all addresses, which is slow and risks locking devices on designware controllers.
func detectLinux(ctx context.Context, opts *detection.Options) ([]detection.DeviceInfo, error) {
	buses, err := findI2CBuses()
	if err != nil {
		return nil, err
	}

	if len(buses) == 0 {
		return nil, detection.ErrNoDevicesFound
	}

	var devices []detection.DeviceInfo

	for _, bus := range buses {
		select {
		case <-ctx.Done():
			return devices, detection.ErrDetectionTimeout
		default:
		}

		busPath := bus.Path

		if detection.IsPathIgnored(busPath, opts.IgnorePaths) {
			continue
		}

		// Return every valid I2C bus as a candidate with Low confidence.
		// Actual PN532 confirmation happens when the transport opens and
		// sends SAMConfiguration — probing here risks bus contention with
		// the designware I2C controller.
		devices = append(devices, detection.DeviceInfo{
			Transport:  "i2c",
			Path:       busPath,
			Name:       fmt.Sprintf("I2C bus %s", busPath),
			Confidence: detection.Low,
			Metadata: map[string]string{
				"bus":     busPath,
				"address": fmt.Sprintf("0x%02X", DefaultPN532Address),
			},
		})
	}

	if len(devices) == 0 {
		return nil, detection.ErrNoDevicesFound
	}

	return devices, nil
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
