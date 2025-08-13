//go:build windows

package uart

import (
	"errors"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// getSerialPorts returns available serial ports on Windows
func getSerialPorts() ([]serialPort, error) {
	// First, try to get COM ports from registry
	registryPorts, registryErr := getRegistryCOMPorts()

	// Get COM ports from SetupAPI (more comprehensive)
	setupAPIPorts, setupErr := getSetupAPICOMPorts()

	// If both methods failed, return combined error information
	if registryErr != nil && setupErr != nil {
		return nil, errors.Join(registryErr, setupErr)
	}

	// Merge ports from both sources, preferring SetupAPI data
	portMap := make(map[string]serialPort)

	// Add registry ports first
	if registryErr == nil {
		for _, port := range registryPorts {
			portMap[port.Path] = port
		}
	}

	// Add/overwrite with SetupAPI ports (they have more metadata)
	if setupErr == nil {
		for _, port := range setupAPIPorts {
			portMap[port.Path] = port
		}
	}

	// Convert map back to slice
	ports := make([]serialPort, 0, len(portMap))
	for _, port := range portMap {
		ports = append(ports, port)
	}

	return ports, nil
}

// getRegistryCOMPorts gets COM ports from Windows registry
func getRegistryCOMPorts() ([]serialPort, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `HARDWARE\DEVICEMAP\SERIALCOMM`, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer key.Close()

	values, err := key.ReadValueNames(-1)
	if err != nil {
		return nil, err
	}

	// Pre-allocate slice with capacity equal to the number of registry values
	ports := make([]serialPort, 0, len(values))
	for _, value := range values {
		portName, _, err := key.GetStringValue(value)
		if err != nil {
			continue
		}

		ports = append(ports, serialPort{
			Path: portName,
			Name: portName,
		})
	}

	return ports, nil
}

// getSetupAPICOMPorts gets COM ports directly from SetupAPI
func getSetupAPICOMPorts() ([]serialPort, error) {
	// Load setupapi.dll
	setupapi := windows.NewLazySystemDLL("setupapi.dll")
	setupDiGetClassDevs := setupapi.NewProc("SetupDiGetClassDevsW")
	setupDiEnumDeviceInfo := setupapi.NewProc("SetupDiEnumDeviceInfo")
	setupDiGetDeviceRegistryProperty := setupapi.NewProc("SetupDiGetDeviceRegistryPropertyW")
	setupDiDestroyDeviceInfoList := setupapi.NewProc("SetupDiDestroyDeviceInfoList")

	// GUID for Ports class
	guidPorts := windows.GUID{
		Data1: 0x4d36e978,
		Data2: 0xe325,
		Data3: 0x11ce,
		Data4: [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18},
	}

	// Get device info set
	const DIGCF_PRESENT = 0x00000002
	devInfo, _, _ := setupDiGetClassDevs.Call(
		uintptr(unsafe.Pointer(&guidPorts)),
		0,
		0,
		DIGCF_PRESENT,
	)

	if devInfo == uintptr(windows.InvalidHandle) {
		return nil, windows.GetLastError()
	}
	defer setupDiDestroyDeviceInfoList.Call(devInfo)

	var ports []serialPort

	// Enumerate devices
	type spDevinfoData struct {
		cbSize    uint32
		classGuid windows.GUID
		devInst   uint32
		reserved  uintptr
	}

	var devInfoData spDevinfoData
	devInfoData.cbSize = uint32(unsafe.Sizeof(devInfoData))

	for i := uint32(0); ; i++ {
		ret, _, _ := setupDiEnumDeviceInfo.Call(
			devInfo,
			uintptr(i),
			uintptr(unsafe.Pointer(&devInfoData)),
		)

		if ret == 0 {
			break
		}

		// Get friendly name (includes COM port) using two-call pattern
		const SPDRP_FRIENDLYNAME = 0x0000000C
		var propertyType uint32
		var requiredSize uint32

		// First call to get the required buffer size
		setupDiGetDeviceRegistryProperty.Call(
			devInfo,
			uintptr(unsafe.Pointer(&devInfoData)),
			SPDRP_FRIENDLYNAME,
			0, // propertyType is optional on first call
			0, // nil buffer
			0, // buffer size 0
			uintptr(unsafe.Pointer(&requiredSize)),
		)

		if requiredSize == 0 {
			continue
		}

		// Allocate buffer of the required size and call again
		friendlyNameBuf := make([]uint16, requiredSize/2)
		ret, _, _ := setupDiGetDeviceRegistryProperty.Call(
			devInfo,
			uintptr(unsafe.Pointer(&devInfoData)),
			SPDRP_FRIENDLYNAME,
			uintptr(unsafe.Pointer(&propertyType)),
			uintptr(unsafe.Pointer(&friendlyNameBuf[0])),
			uintptr(requiredSize),
			0, // requiredSize is optional on second call
		)

		if ret == 0 {
			continue
		}

		name := windows.UTF16ToString(friendlyNameBuf)

		// Extract COM port from friendly name
		var comPort string
		if n := strings.LastIndex(name, "(COM"); n >= 0 {
			if m := strings.Index(name[n:], ")"); m >= 0 {
				comPort = name[n+1 : n+m]
			}
		}

		if comPort == "" {
			continue
		}

		// Create new port entry
		port := serialPort{
			Path: comPort,
			Name: name,
		}

		// Get hardware ID for VID/PID using two-call pattern
		const SPDRP_HARDWAREID = 0x00000001
		var hwRequiredSize uint32

		// First call to get the required buffer size
		setupDiGetDeviceRegistryProperty.Call(
			devInfo,
			uintptr(unsafe.Pointer(&devInfoData)),
			SPDRP_HARDWAREID,
			0, // propertyType is optional on first call
			0, // nil buffer
			0, // buffer size 0
			uintptr(unsafe.Pointer(&hwRequiredSize)),
		)

		if hwRequiredSize > 0 {
			// Allocate buffer of the required size and call again
			hardwareIDBuf := make([]uint16, hwRequiredSize/2)
			ret, _, _ = setupDiGetDeviceRegistryProperty.Call(
				devInfo,
				uintptr(unsafe.Pointer(&devInfoData)),
				SPDRP_HARDWAREID,
				uintptr(unsafe.Pointer(&propertyType)),
				uintptr(unsafe.Pointer(&hardwareIDBuf[0])),
				uintptr(hwRequiredSize),
				0, // hwRequiredSize is optional on second call
			)

			if ret != 0 {
				hwid := windows.UTF16ToString(hardwareIDBuf)
				// Parse VID/PID from hardware ID (format: USB\VID_xxxx&PID_xxxx)
				if vidpid := parseWindowsHardwareID(hwid); vidpid != "" {
					port.VIDPID = vidpid
				}
			}
		}

		// Get manufacturer using two-call pattern
		const SPDRP_MFG = 0x0000000B
		var mfgRequiredSize uint32

		// First call to get the required buffer size
		setupDiGetDeviceRegistryProperty.Call(
			devInfo,
			uintptr(unsafe.Pointer(&devInfoData)),
			SPDRP_MFG,
			0, // propertyType is optional on first call
			0, // nil buffer
			0, // buffer size 0
			uintptr(unsafe.Pointer(&mfgRequiredSize)),
		)

		if mfgRequiredSize > 0 {
			// Allocate buffer of the required size and call again
			mfgBuf := make([]uint16, mfgRequiredSize/2)
			ret, _, _ = setupDiGetDeviceRegistryProperty.Call(
				devInfo,
				uintptr(unsafe.Pointer(&devInfoData)),
				SPDRP_MFG,
				uintptr(unsafe.Pointer(&propertyType)),
				uintptr(unsafe.Pointer(&mfgBuf[0])),
				uintptr(mfgRequiredSize),
				0, // mfgRequiredSize is optional on second call
			)

			if ret != 0 {
				port.Manufacturer = windows.UTF16ToString(mfgBuf)
			}
		}

		// Extract product from friendly name
		if n := strings.Index(name, " ("); n > 0 {
			port.Product = name[:n]
		}

		// Add port to results
		ports = append(ports, port)
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
func getSerialPortsFallback() ([]serialPort, error) {
	// Try SetupAPI first as it's more comprehensive
	if ports, err := getSetupAPICOMPorts(); err == nil {
		return ports, nil
	}

	// Fallback to registry method
	return getRegistryCOMPorts()
}
