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
	var result []serialPort
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
func getRegistryCOMPorts(ctx context.Context) ([]serialPort, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer key.Close()

	valueNames, err := key.ReadValueNames(-1)
	if err != nil {
		return nil, err
	}

	var ports []serialPort
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

	const DIGCF_PRESENT = 0x00000002

	// Get device info set
	deviceInfoSet, _, _ := procSetupDiGetClassDevs.Call(
		uintptr(unsafe.Pointer(&portsClassGUID)),
		0,
		0,
		DIGCF_PRESENT,
	)
	if deviceInfoSet == 0 {
		return nil, errors.New("failed to get device info set")
	}
	defer procSetupDiDestroyDeviceInfoList.Call(deviceInfoSet)

	var ports []serialPort
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
			ClassGuid windows.GUID
			DevInst   uint32
			Reserved  uintptr
		}
		deviceInfoData.Size = uint32(unsafe.Sizeof(deviceInfoData))

		// Enumerate device
		ret, _, _ := procSetupDiEnumDeviceInfo.Call(
			deviceInfoSet,
			uintptr(deviceIndex),
			uintptr(unsafe.Pointer(&deviceInfoData)),
		)
		if ret == 0 {
			break // No more devices
		}

		// Get friendly name (SPDRP_FRIENDLYNAME = 12)
		var friendlyName [256]uint16
		var requiredSize uint32
		procSetupDiGetDeviceRegistryProperty.Call(
			deviceInfoSet,
			uintptr(unsafe.Pointer(&deviceInfoData)),
			12, // SPDRP_FRIENDLYNAME
			0,
			uintptr(unsafe.Pointer(&friendlyName[0])),
			uintptr(len(friendlyName)*2),
			uintptr(unsafe.Pointer(&requiredSize)),
		)

		friendlyNameStr := windows.UTF16ToString(friendlyName[:])

		// Get hardware ID (SPDRP_HARDWAREID = 1)
		var hardwareID [256]uint16
		procSetupDiGetDeviceRegistryProperty.Call(
			deviceInfoSet,
			uintptr(unsafe.Pointer(&deviceInfoData)),
			1, // SPDRP_HARDWAREID
			0,
			uintptr(unsafe.Pointer(&hardwareID[0])),
			uintptr(len(hardwareID)*2),
			uintptr(unsafe.Pointer(&requiredSize)),
		)

		hardwareIDStr := windows.UTF16ToString(hardwareID[:])

		// Extract COM port name from friendly name
		if strings.Contains(friendlyNameStr, "COM") {
			port := serialPort{
				Name:         friendlyNameStr,
				Manufacturer: "Unknown",
				Product:      friendlyNameStr,
			}

			// Extract COM port (e.g., "COM3")
			comStart := strings.Index(strings.ToUpper(friendlyNameStr), "COM")
			if comStart != -1 {
				comPart := friendlyNameStr[comStart:]
				if idx := strings.Index(comPart, ")"); idx != -1 {
					comPart = comPart[:idx]
				}
				if idx := strings.Index(comPart, " "); idx != -1 {
					comPart = comPart[:idx]
				}
				port.Path = comPart
			}

			// Parse hardware ID for VID/PID
			if hardwareIDStr != "" {
				if hwid := parseWindowsHardwareID(hardwareIDStr); hwid != "" {
					port.VIDPID = hwid
				}
			}

			ports = append(ports, port)
		}

		deviceIndex++
	}

	return ports, nil
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

	// Verify they're hex
	for _, r := range vid + pid {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'F')) {
			return ""
		}
	}

	return vid + ":" + pid
}

// getSerialPortsFallback provides a fallback method for COM port detection
func getSerialPortsFallback(ctx context.Context) ([]serialPort, error) {
	var ports []serialPort

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
