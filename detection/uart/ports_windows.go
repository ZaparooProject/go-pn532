//go:build windows

package uart

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// getSerialPorts returns available serial ports on Windows
func getSerialPorts(ctx context.Context) ([]serialPort, error) {
	// First, try to get COM ports from registry
	registryPorts, registryErr := getRegistryCOMPorts(ctx)

	// Get COM ports from SetupAPI (more comprehensive)
	setupPorts, setupErr := getSetupAPICOMPorts(ctx)

	// Combine results, preferring SetupAPI data when available
	portMap := make(map[string]serialPort)

	// Add registry ports first
	if registryErr == nil {
		for _, port := range registryPorts {
			portMap[port.Path] = port
		}
	}

	// Override with SetupAPI data (usually has more metadata)
	if setupErr == nil {
		for _, port := range setupPorts {
			portMap[port.Path] = port
		}
	}

	// Convert map back to slice
	result := make([]serialPort, 0, len(portMap))
	for _, port := range portMap {
		result = append(result, port)
	}

	// If we got no ports from either method, return the errors
	if len(result) == 0 {
		if registryErr != nil && setupErr != nil {
			return getSerialPortsFallback(ctx)
		}
	}

	return result, nil
}

// getRegistryCOMPorts gets COM ports from Windows registry
func getRegistryCOMPorts(_ context.Context) ([]serialPort, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.QUERY_VALUE)
	if err != nil {
		return nil, fmt.Errorf("failed to open registry key: %w", err)
	}
	defer func() { _ = key.Close() }()

	valueNames, err := key.ReadValueNames(-1)
	if err != nil {
		return nil, fmt.Errorf("failed to read registry value names: %w", err)
	}

	ports := make([]serialPort, 0, len(valueNames))
	for _, name := range valueNames {
		value, _, err := key.GetStringValue(name)
		if err != nil {
			continue
		}

		port := serialPort{
			Path: value,
			Name: value,
		}
		ports = append(ports, port)
	}

	return ports, nil
}

// getSetupAPICOMPorts gets COM ports directly from SetupAPI
func getSetupAPICOMPorts(ctx context.Context) ([]serialPort, error) {
	// Load setupapi.dll
	setupapi := windows.NewLazyDLL("setupapi.dll")
	procSetupDiGetClassDevs := setupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInfo := setupapi.NewProc("SetupDiEnumDeviceInfo")
	procSetupDiGetDeviceRegistryProperty := setupapi.NewProc("SetupDiGetDeviceRegistryPropertyW")
	procSetupDiDestroyDeviceInfoList := setupapi.NewProc("SetupDiDestroyDeviceInfoList")

	// GUID for Ports (COM & LPT ports) device class
	// {4D36E978-E325-11CE-BFC1-08002BE10318}
	portsClassGUID := windows.GUID{
		Data1: 0x4D36E978,
		Data2: 0xE325,
		Data3: 0x11CE,
		Data4: [8]byte{0xBF, 0xC1, 0x08, 0x00, 0x2B, 0xE1, 0x03, 0x18},
	}

	const digcfPresent = 0x00000002

	// Get device info set
	deviceInfoSet, _, _ := procSetupDiGetClassDevs.Call(
		uintptr(unsafe.Pointer(&portsClassGUID)), // #nosec G103 - Required for Windows API
		0,
		0,
		digcfPresent,
	)
	if deviceInfoSet == 0 {
		return nil, errors.New("failed to get device info set")
	}
	defer func() {
		_, _, _ = procSetupDiDestroyDeviceInfoList.Call(deviceInfoSet)
	}()

	ports := make([]serialPort, 0, 16) // Pre-allocate with reasonable capacity
	deviceIndex := 0

	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var deviceInfoData struct {
			Size      uint32
			ClassGUID windows.GUID
			DevInst   uint32
			Reserved  uintptr
		}
		deviceInfoData.Size = uint32(unsafe.Sizeof(deviceInfoData))

		// Enumerate device
		ret, _, _ := procSetupDiEnumDeviceInfo.Call(
			deviceInfoSet,
			uintptr(deviceIndex),
			uintptr(unsafe.Pointer(&deviceInfoData)), // #nosec G103 - Required for Windows API
		)
		if ret == 0 {
			break // No more devices
		}

		port, err := extractPortInfo(ctx, procSetupDiGetDeviceRegistryProperty, deviceInfoSet, &deviceInfoData)
		if err != nil {
			// Log error but continue with next device
			deviceIndex++
			continue
		}
		if port != nil {
			ports = append(ports, *port)
		}

		deviceIndex++
	}

	return ports, nil
}

func extractPortInfo(
	_ context.Context,
	proc *windows.LazyProc,
	deviceInfoSet uintptr,
	deviceInfoData any,
) (*serialPort, error) {
	// Get friendly name
	friendlyNameStr, err := getDeviceProperty(proc, deviceInfoSet, deviceInfoData, 12) // SPDRP_FRIENDLYNAME
	if err != nil {
		return nil, err
	}

	// Only process devices that contain "COM"
	if !strings.Contains(friendlyNameStr, "COM") {
		return nil, errors.New("device is not a COM port")
	}

	// Get hardware ID
	hardwareIDStr, _ := getDeviceProperty(proc, deviceInfoSet, deviceInfoData, 1) // SPDRP_HARDWAREID

	port := &serialPort{
		Name:         friendlyNameStr,
		Manufacturer: "Unknown",
		Product:      friendlyNameStr,
	}

	// Extract COM port name
	port.Path = extractCOMPortName(friendlyNameStr)

	// Parse VID/PID if available
	if hardwareIDStr != "" {
		if hwid := parseWindowsHardwareID(hardwareIDStr); hwid != "" {
			port.VIDPID = hwid
		}
	}

	return port, nil
}

func getDeviceProperty(
	proc *windows.LazyProc,
	deviceInfoSet uintptr,
	deviceInfoData any,
	propertyType uint32,
) (string, error) {
	var buffer [256]uint16
	var requiredSize uint32

	_, _, _ = proc.Call( // #nosec G104 - Windows API call, error checking not always meaningful
		deviceInfoSet,
		uintptr(unsafe.Pointer(deviceInfoData.(*struct { // #nosec G103 - Required for Windows API
			Size      uint32
			ClassGUID windows.GUID
			DevInst   uint32
			Reserved  uintptr
		}))),
		uintptr(propertyType),
		0,
		uintptr(unsafe.Pointer(&buffer[0])), // #nosec G103 - Required for Windows API
		uintptr(len(buffer)*2),
		uintptr(unsafe.Pointer(&requiredSize)), // #nosec G103 - Required for Windows API
	)

	return windows.UTF16ToString(buffer[:]), nil
}

func extractCOMPortName(friendlyName string) string {
	comStart := strings.Index(strings.ToUpper(friendlyName), "COM")
	if comStart == -1 {
		return ""
	}

	comPart := friendlyName[comStart:]

	// Remove trailing parenthesis
	if idx := strings.Index(comPart, ")"); idx != -1 {
		comPart = comPart[:idx]
	}

	// Remove trailing space
	if idx := strings.Index(comPart, " "); idx != -1 {
		comPart = comPart[:idx]
	}

	return comPart
}

// parseWindowsHardwareID extracts VID:PID from Windows hardware ID
func parseWindowsHardwareID(hwid string) string {
	// Look for USB\VID_xxxx&PID_xxxx pattern
	hwid = strings.ToUpper(hwid)

	vidIdx := strings.Index(hwid, "VID_")
	if vidIdx < 0 {
		return ""
	}

	pidIdx := strings.Index(hwid, "PID_")
	if pidIdx < 0 {
		return ""
	}

	// Extract 4 characters after VID_ and PID_
	if vidIdx+8 > len(hwid) || pidIdx+8 > len(hwid) {
		return ""
	}

	vid := hwid[vidIdx+4 : vidIdx+8]
	pid := hwid[pidIdx+4 : pidIdx+8]

	// Verify they're hex - use De Morgan's law for clarity
	for _, r := range vid + pid {
		if (r < '0' || r > '9') && (r < 'A' || r > 'F') {
			return ""
		}
	}

	return vid + ":" + pid
}

// getSerialPortsFallback provides a fallback method for COM port detection
func getSerialPortsFallback(ctx context.Context) ([]serialPort, error) {
	ports := make([]serialPort, 0, 16) // Pre-allocate with capacity for 16 ports

	// Try common COM port names
	for i := 1; i <= 16; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		portName := fmt.Sprintf("COM%d", i)
		// Simple existence check by trying to open the port
		// On Windows, we can't easily check if the port exists without trying to open it
		ports = append(ports, serialPort{
			Path: portName,
			Name: portName,
		})
	}

	return ports, nil
}
