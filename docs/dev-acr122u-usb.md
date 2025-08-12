# ACR122U USB Support

This document describes the USB support for ACR122U readers in go-pn532, which is especially important for embedded Linux systems that don't have PC/SC support.

## Overview

The ACR122U transport supports two communication modes:
- **PC/SC** (default): Uses the PC/SC API, available on Windows, macOS, and desktop Linux
- **USB**: Direct USB communication using libusb/gousb, works on all platforms including embedded Linux

## Mode Selection

### Automatic Mode (Default)
```go
transport, err := acr122u.New()
```
This tries PC/SC first and automatically falls back to USB if PC/SC is not available.

### Force USB Mode
```go
config := &acr122u.Config{
    PreferredMode: acr122u.ModeUSB,
    Fallback:      false,
}
transport, err := acr122u.NewWithConfig(config)
```
This forces USB mode, which is useful for:
- Embedded Linux systems without pcscd
- Testing USB communication
- Working around PC/SC issues

### Force PC/SC Mode
```go
config := &acr122u.Config{
    PreferredMode: acr122u.ModePCSC,
    Fallback:      false,
}
transport, err := acr122u.NewWithConfig(config)
```

## Clone ACR122U Support ✅ ENHANCED (2025-08-05)

Clone ACR122U readers are **fully supported** with automatic detection and optimization:

### ✅ Automatic Clone Detection
- **Firmware-based detection**: Identifies clones via "_Clone" suffix in firmware version
- **Automatic SAM bypass**: Skips problematic SAM configuration for clone devices
- **Zero configuration required**: Works transparently with existing code

```
DEBUG: ACR122U HasCapability checking firmware: fw=PN532_V1.6_Clone, err=<nil>
DEBUG: ACR122U isClone=true for firmware=PN532_V1.6_Clone
DEBUG: Transport reports CapabilitySkipsSAMConfig = true
```

### Enhanced USB Reliability
- **Zero-Length Packet handling**: Full USB bulk transfer compliance
- **Tiered recovery system**: Automatic error recovery with exponential backoff
- **Buffer management**: Prevents stale response interference
- **Proper USB reset**: Complete device reset with configuration management

Known differences:
- LED and buzzer may behave differently (active without connection)
- Same USB VID:PID (072F:2200) as genuine devices
- Firmware variations handled automatically

## Supported Devices

| Device | VID:PID | Notes |
|--------|---------|--------|
| ACR122U | 072F:2200 | Most common, includes clones |
| Touchatag | 072F:90CC | Older variant |
| ACR1222 | 072F:2214 | Newer variant |

## USB Permissions on Linux

On Linux systems, you need proper permissions to access USB devices. Create a udev rule:

```bash
# Create /etc/udev/rules.d/99-acr122u.rules
SUBSYSTEM=="usb", ATTRS{idVendor}=="072f", ATTRS{idProduct}=="2200", MODE="0666", GROUP="plugdev"
SUBSYSTEM=="usb", ATTRS{idVendor}=="072f", ATTRS{idProduct}=="90cc", MODE="0666", GROUP="plugdev"
SUBSYSTEM=="usb", ATTRS{idVendor}=="072f", ATTRS{idProduct}=="2214", MODE="0666", GROUP="plugdev"

# Reload rules
sudo udevadm control --reload-rules
sudo udevadm trigger
```

Then add your user to the plugdev group:
```bash
sudo usermod -a -G plugdev $USER
```

## Detection

The ACR122U detector automatically finds devices via both PC/SC and USB:

```go
devices, err := detection.DetectAll(detection.DefaultOptions())
for _, device := range devices {
    if device.Transport == "acr122u" {
        fmt.Printf("Found: %s (mode: %s)\n", device.Name, device.Metadata["mode"])
    }
}
```

## Example Usage

See `examples/acr122u_usb_test/main.go` for a complete example that demonstrates:
- Forcing USB mode
- Reading tags without PC/SC
- Working with clone readers

## Troubleshooting

### USB Mode Not Working
1. Check USB permissions (see above)
2. Ensure libusb is installed:
   - Debian/Ubuntu: `sudo apt-get install libusb-1.0-0`
   - Alpine: `apk add libusb`
3. Try running as root to test permissions

### Clone Reader Issues ✅ RESOLVED
1. ✅ **Automatic detection and configuration** - No manual intervention required
2. ✅ **Enhanced timeout handling** - Intelligent timeout strategy with safety margins
3. ✅ **SAM configuration bypass** - Automatic skip for clone devices
4. LED/buzzer behavior may differ from genuine readers (cosmetic only)

### Performance ✅ IMPROVED
- **USB mode reliability**: Dramatically improved with libnfc-based enhancements
- **Automatic recovery**: Transparent error recovery prevents failures
- **Clone optimization**: Automatic parameter adjustment for clone devices
- Use PC/SC when available for potentially better performance
- USB mode now highly reliable for all systems