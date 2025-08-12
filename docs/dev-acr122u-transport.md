# ACR122U Transport Documentation

## Overview

The ACR122U transport provides dual-mode communication with ACR122U NFC readers, supporting both PC/SC and direct USB modes. This transport automatically selects the best available communication method, ensuring maximum compatibility while maintaining flexibility for embedded systems.

## Features

- **Automatic Mode Selection**: Prefers PC/SC for compatibility, falls back to USB
- **PC/SC Support**: Works with system smart card services
- **Direct USB Support**: Bypasses PC/SC for embedded systems
- **Reader Selection**: Choose specific readers by name or index
- **Cross-Platform**: Windows, macOS, and Linux support
- **Transparent Operation**: Same API regardless of mode

## Supported Devices

- ACS ACR122U (VID: 0x072F, PID: 0x2200)
- ACS ACR1222 (VID: 0x072F, PID: 0x2214)
- Touchatag RFID Reader (VID: 0x072F, PID: 0x90CC)

## Usage

### Basic Usage (Automatic Mode)

```go
import (
    "github.com/ZaparooProject/go-pn532"
    "github.com/ZaparooProject/go-pn532/transport/acr122u"
)

// Create transport with automatic mode selection
transport, err := acr122u.New()
if err != nil {
    log.Fatal(err)
}
defer transport.Close()

// Check which mode was selected
fmt.Printf("Using %s mode\n", transport.GetMode())

// Use with PN532 device
device := pn532.New(transport)
```

### Advanced Configuration

```go
// Force PC/SC mode only
config := &acr122u.Config{
    PreferredMode: acr122u.ModePCSC,
    Fallback:      false,
}

// Force USB mode only
config := &acr122u.Config{
    PreferredMode: acr122u.ModeUSB,
    Fallback:      false,
}

// Select specific reader by name (PC/SC mode)
config := &acr122u.Config{
    PreferredMode: acr122u.ModePCSC,
    ReaderName:    "ACR122", // Partial match
}

// Select reader by index (PC/SC mode)
config := &acr122u.Config{
    PreferredMode: acr122u.ModePCSC,
    ReaderIndex:   0, // First ACR122U reader
}

transport, err := acr122u.NewWithConfig(config)
```

## Mode Selection

The transport uses the following logic for mode selection:

1. **ModeAuto** (default):
   - Try PC/SC first
   - If PC/SC fails, try USB
   - Return error if both fail

2. **ModePCSC**:
   - Use only PC/SC
   - Can specify reader by name or index
   - Fail if PC/SC not available

3. **ModeUSB**:
   - Use only direct USB
   - Requires appropriate permissions
   - May conflict with system services

## Platform-Specific Behavior

### Windows
- Both PC/SC and USB modes typically work
- May need driver installation for USB mode
- PC/SC usually preferred for stability

### macOS
- System smart card service claims exclusive USB access
- PC/SC mode works without issues
- USB mode will fail with permission errors (expected)
- Automatic mode handles this gracefully

### Linux
- Both modes work with proper permissions
- USB mode requires udev rules (see below)
- PC/SC requires pcscd service

## USB Permissions (Linux)

For USB mode on Linux, create `/etc/udev/rules.d/99-acr122u.rules`:

```
# ACS ACR122U
SUBSYSTEM=="usb", ATTRS{idVendor}=="072f", ATTRS{idProduct}=="2200", MODE="0666"
# ACS ACR1222
SUBSYSTEM=="usb", ATTRS{idVendor}=="072f", ATTRS{idProduct}=="2214", MODE="0666"
```

Then reload rules:
```bash
sudo udevadm control --reload-rules
sudo udevadm trigger
```

## Error Handling

The transport provides specific error types:

- `ErrDeviceNotFound`: No ACR122U device found
- `ErrNotConnected`: Transport not connected
- `ErrTimeout`: Communication timeout
- `ErrInvalidResponse`: Unexpected response format

## Example Applications

### Tag Reading with Mode Display

```go
transport, err := acr122u.New()
if err != nil {
    log.Fatal(err)
}
defer transport.Close()

fmt.Printf("Connected via %s mode\n", transport.GetMode())

device := pn532.New(transport)

// Get firmware versions
if fw, err := transport.GetFirmwareVersion(); err == nil {
    fmt.Printf("ACR122U firmware: %s\n", fw)
}

if ver, err := device.GetFirmwareVersion(); err == nil {
    fmt.Printf("PN532 version: %s\n", ver.Version)
}

// Detect and read tags
tag, err := device.DetectTag()
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Tag UID: %s\n", tag.UID)
```

### Command-Line Mode Selection

See `examples/acr122u_read/main.go` for a complete example with command-line flags:

```bash
# Auto mode (default)
./acr122u_read

# Force PC/SC mode
./acr122u_read -mode pcsc

# Force USB mode
./acr122u_read -mode usb

# Select specific reader
./acr122u_read -mode pcsc -reader "ACR122U PICC"
```

## Troubleshooting

### "Device not found" errors
- Ensure reader is connected
- Check if another application is using the reader
- Try specifying mode explicitly

### PC/SC mode issues
- Verify pcscd service is running (Linux)
- Check reader appears in PC/SC tool listings
- Try `pcsctest` or similar utilities

### USB mode permission errors
- Expected on macOS due to system restrictions
- Check udev rules on Linux
- Ensure no other services are claiming the device

### Communication timeouts
- Some operations may be slower in PC/SC mode
- Adjust timeout if needed
- Check USB cable and connections

## Technical Details

### PC/SC Implementation
- Uses standard PC/SC API via `github.com/ebfe/scard`
- Wraps PN532 commands in APDU format
- Handles response parsing and status words

### USB Implementation
- Direct USB communication via `github.com/google/gousb`
- Implements minimal CCID protocol
- Bypasses system smart card services

### Protocol Stack

PC/SC Mode:
```
PN532 Command → APDU Wrapper → PC/SC API → USB Driver
```

USB Mode:
```
PN532 Command → APDU Wrapper → CCID Frame → USB Bulk Transfer
```

## See Also

- [ACR122U API Manual](hardware-acr122u-manual.md)
- [Transport README](../transport/acr122u/README.md)
- [Example Code](../examples/acr122u_read/)