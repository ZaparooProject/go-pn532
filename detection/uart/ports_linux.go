//go:build linux

package uart

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// getSerialPorts returns available serial ports on Linux
func getSerialPorts(ctx context.Context) ([]serialPort, error) {
	var ports []serialPort

	// First try to get USB serial devices with full metadata
	usbPorts, err := processUSBDevice(ctx, "/sys/bus/usb/devices")
	if err == nil {
		ports = append(ports, usbPorts...)
	}

	// Then get built-in serial ports
	builtinPorts, err := getBuiltinSerialPorts(ctx)
	if err == nil {
		ports = append(ports, builtinPorts...)
	}

	// If we still have no ports, fallback to basic enumeration
	if len(ports) == 0 {
		return getSerialPortsFallback(ctx)
	}

	return ports, nil
}

// getSerialPortsFallback returns serial ports without metadata
// processUSBDevice checks if a tty entry is a USB device and returns its port info
func processUSBDevice(_ context.Context, _ string) ([]serialPort, error) {
	var ports []serialPort

	ttyDir := "/sys/class/tty"
	entries, err := os.ReadDir(ttyDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", ttyDir, err)
	}

	for _, entry := range entries {
		if port, ok := processUSBDeviceEntry(ttyDir, entry); ok {
			ports = append(ports, port)
		}
	}

	return ports, nil
}

// classLinkNames reports whether the tty's class symlink target names a USB
// device, and whether the question could be answered at all.
//
// /sys/class/tty/<name> is itself a symlink to the device directory, so one
// readlink answers the USB question without resolving anything. That directory
// is a descendant of whatever <name>/device resolves to, so it contains every
// path component the slower check sees: this can skip work, never turn a USB
// device into a non-USB one. When the entry is not a symlink at all (a kernel
// built with CONFIG_SYSFS_DEPRECATED) the caller falls back to the slow path.
func classLinkIsUSB(ttyPath string) (isUSB, known bool) {
	target, err := os.Readlink(ttyPath)
	if err != nil {
		return false, false
	}
	return strings.Contains(target, "/usb"), true
}

func processUSBDeviceEntry(ttyDir string, entry os.DirEntry) (serialPort, bool) {
	if entry.IsDir() {
		return serialPort{}, false
	}

	ttyPath := filepath.Join(ttyDir, entry.Name())

	// Reject non-USB entries before the expensive calls below. os.Stat
	// resolves the whole symlink chain and EvalSymlinks lstats every component
	// of the result, and on a host with no USB serial adapter attached all of
	// that is done for every tty only to be discarded.
	if isUSB, known := classLinkIsUSB(ttyPath); known && !isUSB {
		return serialPort{}, false
	}

	// Check if it's a USB device by looking for the device symlink
	devicePath := filepath.Join(ttyPath, "device")
	if _, err := os.Stat(devicePath); err != nil {
		return serialPort{}, false
	}

	// Resolve the device symlink to find the USB device
	resolved, err := filepath.EvalSymlinks(devicePath)
	if err != nil {
		return serialPort{}, false
	}

	// Check if it's a USB device
	if !strings.Contains(resolved, "/usb") {
		return serialPort{}, false
	}

	port := serialPort{
		Path: "/dev/" + entry.Name(),
		Name: entry.Name(),
	}

	// Try to read USB attributes
	readUSBAttributes(&port, resolved)
	return port, true
}

// readUSBAttributes reads USB device attributes by walking up the device tree
func readUSBAttributes(port *serialPort, devicePath string) {
	current := devicePath
	for range 10 { // Limit iterations to prevent infinite loops
		if readUSBIdentifiers(port, current) {
			break
		}

		// Move up one level
		current = filepath.Dir(current)
		if current == "/" || current == "." {
			break
		}
	}
}

// readUSBIdentifiers reads vendor/product IDs and descriptors from USB device
func readUSBIdentifiers(port *serialPort, path string) bool {
	// Validate path is under /sys/
	cleanPath := filepath.Clean(path)
	if !strings.HasPrefix(cleanPath, "/sys/") {
		return false
	}

	vidPath := filepath.Clean(filepath.Join(path, "idVendor"))
	pidPath := filepath.Clean(filepath.Join(path, "idProduct"))

	vidBytes, vidErr := os.ReadFile(vidPath) // #nosec G304 -- Path is validated to be under /sys/
	if vidErr != nil {
		return false
	}

	pidBytes, pidErr := os.ReadFile(pidPath) // #nosec G304 -- Path is validated to be under /sys/
	if pidErr != nil {
		return false
	}

	vid := strings.TrimSpace(string(vidBytes))
	pid := strings.TrimSpace(string(pidBytes))
	port.VIDPID = strings.ToUpper(vid + ":" + pid)

	// Try to read manufacturer and product
	readUSBDescriptors(port, path)
	return true
}

// readUSBDescriptors reads manufacturer, product, and serial number
func readUSBDescriptors(port *serialPort, path string) {
	// Validate path is under /sys/
	cleanPath := filepath.Clean(path)
	if !strings.HasPrefix(cleanPath, "/sys/") {
		return
	}

	// #nosec G304 -- Path is validated to be under /sys/
	if mfgBytes, err := os.ReadFile(filepath.Clean(filepath.Join(path, "manufacturer"))); err == nil {
		port.Manufacturer = strings.TrimSpace(string(mfgBytes))
	}
	// #nosec G304 -- Path is validated to be under /sys/
	if prodBytes, err := os.ReadFile(filepath.Clean(filepath.Join(path, "product"))); err == nil {
		port.Product = strings.TrimSpace(string(prodBytes))
	}
	// #nosec G304 -- Path is validated to be under /sys/
	if serialBytes, err := os.ReadFile(filepath.Clean(filepath.Join(path, "serial"))); err == nil {
		port.SerialNumber = strings.TrimSpace(string(serialBytes))
	}
}

// portPattern is a device glob together with whether the ports it matches are
// UARTs wired into the board rather than removable adapters. ttyS and ttyAMA
// are on-board, and the detector will not probe those speculatively.
type portPattern struct {
	glob    string
	builtin bool
}

var builtinPortPatterns = []portPattern{
	{glob: "/dev/ttyS*", builtin: true},
	{glob: "/dev/ttyAMA*", builtin: true},
}

var fallbackPortPatterns = []portPattern{
	{glob: "/dev/ttyUSB*"},
	{glob: "/dev/ttyACM*"},
	{glob: "/dev/ttyS*", builtin: true},
	{glob: "/dev/ttyAMA*", builtin: true},
}

// globPorts and statPort are indirected so the pattern tables can be tested
// against fixture paths rather than whatever device nodes the host happens to
// have.
var (
	globPorts = filepath.Glob
	statPort  = os.Stat
)

// portsMatching expands the patterns into ports, skipping anything that no
// longer exists by the time it is stat'd.
func portsMatching(patterns []portPattern) []serialPort {
	var ports []serialPort

	for _, pattern := range patterns {
		matches, err := globPorts(pattern.glob)
		if err != nil {
			continue
		}

		for _, path := range matches {
			if _, err := statPort(path); err != nil {
				continue
			}
			ports = append(ports, serialPort{
				Path:    path,
				Name:    filepath.Base(path),
				Builtin: pattern.builtin,
			})
		}
	}

	return ports
}

// getBuiltinSerialPorts returns non-USB serial ports
func getBuiltinSerialPorts(_ context.Context) ([]serialPort, error) {
	return portsMatching(builtinPortPatterns), nil
}

func getSerialPortsFallback(_ context.Context) ([]serialPort, error) {
	return portsMatching(fallbackPortPatterns), nil
}
