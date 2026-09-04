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
	usbPorts, err := processUSBDevice(ctx, sysfsClassTTY)
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
func processUSBDevice(_ context.Context, ttyDir string) ([]serialPort, error) {
	var ports []serialPort

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

// sysfsClassTTY is where the tty class lives, and sysfsRoot bounds the paths the
// USB attribute readers are willing to open. Both are variables so tests can
// point the walk at a fixture tree; nothing outside tests reassigns them.
var (
	sysfsClassTTY = "/sys/class/tty"
	sysfsRoot     = "/sys"
)

// underSysfsRoot reports whether a path is inside sysfsRoot. The attribute
// readers open files by a path derived from a symlink target, so this bounds
// what a malformed or hostile link can reach.
func underSysfsRoot(path string) bool {
	return strings.HasPrefix(filepath.Clean(path), sysfsRoot+string(filepath.Separator))
}

// readLink is os.Readlink, indirected so a test can make the class-link check
// unanswerable and prove the slow path still finds the device.
var readLink = os.Readlink

// classLinkIsUSB reports whether the tty's class symlink target names a USB
// device, and whether the question could be answered at all.
//
// /sys/class/tty/<name> is itself a symlink to the device directory, so one
// readlink answers the USB question without resolving anything. That directory
// is a descendant of whatever <name>/device resolves to, so it contains every
// path component the slower check sees: this can skip work, never turn a USB
// device into a non-USB one.
//
// A readlink that fails is reported as unanswered rather than as "not USB", so
// the caller falls through to resolving <name>/device. Treating "do not know"
// as a rejection is the one way this optimisation could lose a real device.
func classLinkIsUSB(ttyPath string) (isUSB, known bool) {
	target, err := readLink(ttyPath)
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
	if !underSysfsRoot(path) {
		return false
	}

	vidPath := filepath.Clean(filepath.Join(path, "idVendor"))
	pidPath := filepath.Clean(filepath.Join(path, "idProduct"))

	vidBytes, vidErr := os.ReadFile(vidPath) // #nosec G304 -- Path is validated to be under sysfsRoot
	if vidErr != nil {
		return false
	}

	pidBytes, pidErr := os.ReadFile(pidPath) // #nosec G304 -- Path is validated to be under sysfsRoot
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
	if !underSysfsRoot(path) {
		return
	}

	// #nosec G304 -- Path is validated to be under sysfsRoot
	if mfgBytes, err := os.ReadFile(filepath.Clean(filepath.Join(path, "manufacturer"))); err == nil {
		port.Manufacturer = strings.TrimSpace(string(mfgBytes))
	}
	// #nosec G304 -- Path is validated to be under sysfsRoot
	if prodBytes, err := os.ReadFile(filepath.Clean(filepath.Join(path, "product"))); err == nil {
		port.Product = strings.TrimSpace(string(prodBytes))
	}
	// #nosec G304 -- Path is validated to be under sysfsRoot
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
