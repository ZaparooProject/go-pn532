//go:build darwin

package uart

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// getSerialPorts returns available serial ports on macOS
func getSerialPorts() ([]serialPort, error) {
	// Use ioreg to get USB device information
	cmd := exec.Command("ioreg", "-r", "-c", "IOSerialBSDClient", "-a")
	output, err := cmd.Output()
	if err != nil {
		// Fallback to simple ls if ioreg fails
		return getSerialPortsFallback()
	}

	// Parse the plist output to extract device information
	// This is a simplified parser - in production you might want to use a proper plist parser

	// Split by device entries
	devices := strings.Split(string(output), "IOSerialBSDClient")

	// Pre-allocate slice with capacity equal to the number of device entries
	ports := make([]serialPort, 0, len(devices))

	for _, device := range devices {
		// Extract TTY path
		ttyMatch := regexp.MustCompile(`"IOTTYDevice"\s*=\s*"([^"]+)"`).FindStringSubmatch(device)
		if len(ttyMatch) < 2 {
			continue
		}

		port := serialPort{
			Path: "/dev/" + ttyMatch[1],
			Name: ttyMatch[1],
		}

		// Try to extract USB information
		// Look for parent USB device info
		if vidMatch := regexp.MustCompile(`"idVendor"\s*=\s*(\d+)`).FindStringSubmatch(device); len(vidMatch) >= 2 {
			if pidMatch := regexp.MustCompile(`"idProduct"\s*=\s*(\d+)`).FindStringSubmatch(device); len(pidMatch) >= 2 {
				// Convert decimal to hex
				vid := vidMatch[1]
				pid := pidMatch[1]
				var vidInt, pidInt int
				if _, err := fmt.Sscanf(vid, "%d", &vidInt); err == nil {
					if _, err := fmt.Sscanf(pid, "%d", &pidInt); err == nil {
						port.VIDPID = fmt.Sprintf("%04X:%04X", vidInt, pidInt)
					}
				}
			}
		}

		// Extract manufacturer and product strings
		if mfgMatch := regexp.MustCompile(`"USB Vendor Name"\s*=\s*"([^"]+)"`).FindStringSubmatch(device); len(mfgMatch) >= 2 {
			port.Manufacturer = mfgMatch[1]
		}
		if prodMatch := regexp.MustCompile(`"USB Product Name"\s*=\s*"([^"]+)"`).FindStringSubmatch(device); len(prodMatch) >= 2 {
			port.Product = prodMatch[1]
		}
		if serialMatch := regexp.MustCompile(`"USB Serial Number"\s*=\s*"([^"]+)"`).FindStringSubmatch(device); len(serialMatch) >= 2 {
			port.SerialNumber = serialMatch[1]
		}

		ports = append(ports, port)
	}

	if len(ports) == 0 {
		return getSerialPortsFallback()
	}

	return ports, nil
}

// getSerialPortsFallback returns serial ports without metadata
func getSerialPortsFallback() ([]serialPort, error) {
	var ports []serialPort

	// Prefer /dev/cu.* devices over /dev/tty.* for exclusive access on macOS
	// Check /dev/cu.* devices first
	matches, err := filepath.Glob("/dev/cu.*")
	if err == nil {
		for _, path := range matches {
			name := filepath.Base(path)
			if strings.HasPrefix(name, "cu.Bluetooth") {
				continue
			}

			// Apply macOS-specific filtering - prefer usbserial devices
			if shouldIncludeMacOSDevice(name) {
				ports = append(ports, serialPort{
					Path: path,
					Name: name,
				})
			}
		}
	}

	// Also check /dev/tty.* devices, but only add if no cu.* equivalent exists
	ttyMatches, err := filepath.Glob("/dev/tty.*")
	if err == nil {
		for _, path := range ttyMatches {
			name := filepath.Base(path)
			if strings.HasPrefix(name, "tty.Bluetooth") {
				continue
			}

			// Check if we already have the cu.* version
			cuPath := strings.Replace(path, "/dev/tty.", "/dev/cu.", 1)
			found := false
			for _, p := range ports {
				if p.Path == cuPath {
					found = true
					break
				}
			}

			if !found && shouldIncludeMacOSDevice(name) {
				ports = append(ports, serialPort{
					Path: path,
					Name: name,
				})
			}
		}
	}

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
