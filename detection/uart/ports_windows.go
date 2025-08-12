//go:build windows

package uart

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// getSerialPorts returns available serial ports on Windows
func getSerialPorts() ([]serialPort, error) {
	// First, get COM ports from registry
	comPorts, err := getRegistryCOMPorts()
	if err != nil {
		return nil, err
	}

	// Then enrich with USB information using SetupAPI
	return enrichWithUSBInfo(comPorts)
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

// enrichWithUSBInfo adds USB device information to COM ports
func enrichWithUSBInfo(ports []serialPort) ([]serialPort, error) {
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

	if devInfo == 0 {
		return ports, nil // Return original ports without enrichment
	}
	defer setupDiDestroyDeviceInfoList.Call(devInfo)

	// Create a map for quick lookup
	portMap := make(map[string]*serialPort)
	for i := range ports {
		portMap[ports[i].Path] = &ports[i]
	}

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

		// Get friendly name (includes COM port)
		const SPDRP_FRIENDLYNAME = 0x0000000C
		var friendlyName [256]uint16
		var propertyType uint32
		var size uint32

		ret, _, _ = setupDiGetDeviceRegistryProperty.Call(
			devInfo,
			uintptr(unsafe.Pointer(&devInfoData)),
			SPDRP_FRIENDLYNAME,
			uintptr(unsafe.Pointer(&propertyType)),
			uintptr(unsafe.Pointer(&friendlyName[0])),
			uintptr(uint32(len(friendlyName)*2)),
			uintptr(unsafe.Pointer(&size)),
		)

		if ret == 0 {
			continue
		}

		name := windows.UTF16ToString(friendlyName[:])

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

		// Find matching port
		port, exists := portMap[comPort]
		if !exists {
			continue
		}

		// Get hardware ID for VID/PID
		const SPDRP_HARDWAREID = 0x00000001
		var hardwareID [512]uint16

		ret, _, _ = setupDiGetDeviceRegistryProperty.Call(
			devInfo,
			uintptr(unsafe.Pointer(&devInfoData)),
			SPDRP_HARDWAREID,
			uintptr(unsafe.Pointer(&propertyType)),
			uintptr(unsafe.Pointer(&hardwareID[0])),
			uintptr(uint32(len(hardwareID)*2)),
			uintptr(unsafe.Pointer(&size)),
		)

		if ret != 0 {
			hwid := windows.UTF16ToString(hardwareID[:])
			// Parse VID/PID from hardware ID (format: USB\VID_xxxx&PID_xxxx)
			if vidpid := parseWindowsHardwareID(hwid); vidpid != "" {
				port.VIDPID = vidpid
			}
		}

		// Get manufacturer
		const SPDRP_MFG = 0x0000000B
		var mfg [256]uint16

		ret, _, _ = setupDiGetDeviceRegistryProperty.Call(
			devInfo,
			uintptr(unsafe.Pointer(&devInfoData)),
			SPDRP_MFG,
			uintptr(unsafe.Pointer(&propertyType)),
			uintptr(unsafe.Pointer(&mfg[0])),
			uintptr(uint32(len(mfg)*2)),
			uintptr(unsafe.Pointer(&size)),
		)

		if ret != 0 {
			port.Manufacturer = windows.UTF16ToString(mfg[:])
		}

		// Extract product from friendly name
		if n := strings.Index(name, " ("); n > 0 {
			port.Product = name[:n]
		}
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

// getSerialPortsFallback not needed on Windows as registry always works
func getSerialPortsFallback() ([]serialPort, error) {
	return getRegistryCOMPorts()
}
