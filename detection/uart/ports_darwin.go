//go:build darwin

package uart

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// extractDevicePath extracts the IOCalloutDevice path from ioreg output
func extractDevicePath(device string) (path, name string, ok bool) {
	pathRegex := regexp.MustCompile(`"IOCalloutDevice"\s*=\s*"([^"]+)"`)
	if pathMatch := pathRegex.FindStringSubmatch(device); len(pathMatch) >= 2 {
		path = pathMatch[1]
		name = filepath.Base(path)
		return path, name, true
	}
	return "", "", false
}

// extractVIDPID extracts VID and PID from ioreg output and formats as VID:PID
func extractVIDPID(device string) string {
	vidRegex := regexp.MustCompile(`"idVendor"\s*=\s*(\d+)`)
	pidRegex := regexp.MustCompile(`"idProduct"\s*=\s*(\d+)`)

	vidMatch := vidRegex.FindStringSubmatch(device)
	pidMatch := pidRegex.FindStringSubmatch(device)

	if len(vidMatch) >= 2 && len(pidMatch) >= 2 {
		var vidInt, pidInt int
		if _, err := fmt.Sscanf(vidMatch[1], "%d", &vidInt); err == nil {
			if _, err := fmt.Sscanf(pidMatch[1], "%d", &pidInt); err == nil {
				return fmt.Sprintf("%04X:%04X", vidInt, pidInt)
			}
		}
	}
	return ""
}

// extractUSBMetadata extracts manufacturer, product, and serial number from ioreg output
func extractUSBMetadata(device string) (manufacturer, product, serialNumber string) {
	mfgRegex := regexp.MustCompile(`"USB Vendor Name"\s*=\s*"([^"]+)"`)
	if mfgMatch := mfgRegex.FindStringSubmatch(device); len(mfgMatch) >= 2 {
		manufacturer = mfgMatch[1]
	}

	prodRegex := regexp.MustCompile(`"USB Product Name"\s*=\s*"([^"]+)"`)
	if prodMatch := prodRegex.FindStringSubmatch(device); len(prodMatch) >= 2 {
		product = prodMatch[1]
	}

	serialRegex := regexp.MustCompile(`"USB Serial Number"\s*=\s*"([^"]+)"`)
	if serialMatch := serialRegex.FindStringSubmatch(device); len(serialMatch) >= 2 {
		serialNumber = serialMatch[1]
	}

	return manufacturer, product, serialNumber
}

// getSerialPorts returns available serial ports on macOS
func getSerialPorts(ctx context.Context) ([]serialPort, error) {
	// Use ioreg to get USB device information
	cmd := exec.CommandContext(ctx, "ioreg", "-r", "-c", "IOSerialBSDClient", "-a")
	output, err := cmd.Output()
	if err != nil {
		return getSerialPortsFallback(ctx)
	}

	devices := strings.Split(string(output), "+-o ")
	var ports []serialPort

	for _, device := range devices {
		if !strings.Contains(device, "IOSerialBSDClient") ||
			!strings.Contains(device, "IOCalloutDevice") {
			continue
		}

		var port serialPort

		// Extract device path
		path, name, ok := extractDevicePath(device)
		if !ok {
			continue
		}
		port.Path = path
		port.Name = name

		// Extract VID/PID information
		port.VIDPID = extractVIDPID(device)

		// Extract manufacturer information
		port.Manufacturer, port.Product, port.SerialNumber = extractUSBMetadata(device)

		if shouldIncludeMacOSDevice(port.Name) {
			ports = append(ports, port)
		}
	}

	if len(ports) == 0 {
		return getSerialPortsFallback(ctx)
	}

	return ports, nil
}

// processCUDevices processes /dev/cu.* devices and adds them to ports
func processCUDevices(ports []serialPort) []serialPort {
	matches, err := filepath.Glob("/dev/cu.*")
	if err != nil {
		return ports
	}

	for _, path := range matches {
		name := filepath.Base(path)
		if strings.HasPrefix(name, "cu.Bluetooth") {
			continue
		}

		if shouldIncludeMacOSDevice(name) {
			ports = append(ports, serialPort{
				Path: path,
				Name: name,
			})
		}
	}

	return ports
}

// hasCUEquivalent checks if a cu.* equivalent exists for a tty.* path
func hasCUEquivalent(ttyPath string, ports []serialPort) bool {
	cuPath := strings.Replace(ttyPath, "/dev/tty.", "/dev/cu.", 1)
	for _, p := range ports {
		if p.Path == cuPath {
			return true
		}
	}
	return false
}

// processTTYDevices processes /dev/tty.* devices, avoiding duplicates with cu.* devices
func processTTYDevices(ports []serialPort) []serialPort {
	ttyMatches, err := filepath.Glob("/dev/tty.*")
	if err != nil {
		return ports
	}

	for _, path := range ttyMatches {
		name := filepath.Base(path)
		if strings.HasPrefix(name, "tty.Bluetooth") {
			continue
		}

		if !hasCUEquivalent(path, ports) && shouldIncludeMacOSDevice(name) {
			ports = append(ports, serialPort{
				Path: path,
				Name: name,
			})
		}
	}

	return ports
}

// getSerialPortsFallback returns serial ports without metadata
func getSerialPortsFallback(_ context.Context) ([]serialPort, error) {
	var ports []serialPort

	// Prefer /dev/cu.* devices over /dev/tty.* for exclusive access on macOS
	ports = processCUDevices(ports)

	// Also check /dev/tty.* devices, but only add if no cu.* equivalent exists
	ports = processTTYDevices(ports)

	return ports, nil
}

// shouldIncludeMacOSDevice applies macOS-specific device filtering
func shouldIncludeMacOSDevice(deviceName string) bool {
	lowerName := strings.ToLower(deviceName)

	// Prioritize devices with usbserial pattern (most likely to be USB-serial adapters)
	if strings.Contains(lowerName, "usbserial") {
		return true
	}

	// Include other known good patterns
	goodPatterns := []string{
		"slab_usbtouart", // Silicon Labs CP210x
		"usbmodem",       // Arduino and similar devices
		"wchusbserial",   // WinChipHead CH340/CH341
	}

	for _, pattern := range goodPatterns {
		if strings.Contains(lowerName, pattern) {
			return true
		}
	}

	// For now, include all other devices that aren't obviously system devices
	// This maintains backward compatibility while preferring the good patterns above
	systemPatterns := []string{
		"console", "debug", "system", "kernel",
	}

	for _, sysPattern := range systemPatterns {
		if strings.Contains(lowerName, sysPattern) {
			return false
		}
	}

	return true
}
