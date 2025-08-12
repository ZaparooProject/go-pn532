# ACR122U PC/SC Implementation Research

## Table of Contents
1. [Introduction and Overview](#introduction-and-overview)
2. [Technical Architecture](#technical-architecture)
3. [SAM Configuration Protocol](#sam-configuration-protocol)
4. [Implementation Examples](#implementation-examples)
5. [Common Issues and Debugging](#common-issues-and-debugging)
6. [Best Practices](#best-practices)
7. [References and Resources](#references-and-resources)

## Introduction and Overview

### What is ACR122U?

The ACR122U is a USB-connected NFC/RFID reader manufactured by Advanced Card Systems (ACS). It incorporates a PN532 NFC controller chip and provides two primary communication modes:

1. **PC/SC Mode**: Standard smart card interface using system smart card services
2. **USB Mode**: Direct USB communication bypassing PC/SC

### PC/SC vs USB Modes

| Aspect | PC/SC Mode | USB Mode |
|--------|------------|----------|
| **Interface** | Standard PC/SC API | Direct USB bulk transfers |
| **Compatibility** | High - works with existing smart card infrastructure | Platform-dependent |
| **Permissions** | Uses system services | Requires direct USB access |
| **Performance** | Slightly higher latency due to abstraction | Lower latency, direct communication |
| **Platform Support** | Universal (Windows/macOS/Linux) | Varies by platform and permissions |
| **Protocol** | APDU commands wrapped by PC/SC | CCID frames over USB |

### When to Use Each Mode

**PC/SC Mode (Recommended)**:
- Production applications requiring stability
- Systems where PC/SC service is available
- Cross-platform compatibility is important
- Multiple applications may access the reader

**USB Mode**:
- Embedded systems without PC/SC
- Performance-critical applications
- Systems where PC/SC conflicts occur
- Development and debugging scenarios

## Technical Architecture

### APDU Command Structure

PC/SC mode communicates with the ACR122U using Application Protocol Data Units (APDUs) following the ISO 7816-4 standard:

```
┌─────┬─────┬─────┬─────┬─────┬──────────────────┬─────┐
│ CLA │ INS │ P1  │ P2  │ Lc  │      Data        │ Le  │
├─────┼─────┼─────┼─────┼─────┼──────────────────┼─────┤
│ FF  │ 00  │ 00  │ 00  │ XX  │ D4 [CMD] [ARGS]  │ 00  │
└─────┴─────┴─────┴─────┴─────┴──────────────────┴─────┘
```

**Field Definitions**:
- **CLA (0xFF)**: Proprietary class for ACR122U direct commands
- **INS (0x00)**: Direct transmit instruction to forward data to PN532
- **P1, P2 (0x00)**: Parameter bytes (not used for direct transmit)
- **Lc**: Length of data field (2 + length of PN532 arguments)
- **Data**: PN532 command prefixed with 0xD4 direction byte
- **Le (0x00)**: Expected response length (0 = any length)

### PN532 Command Encapsulation

The ACR122U acts as a bridge between PC/SC and the PN532 chip:

```
PC/SC Application
       ↓ APDU
┌─────────────────┐
│    ACR122U      │
│  (PC/SC Layer)  │
├─────────────────┤
│     PN532       │
│  (NFC Controller)│
└─────────────────┘
       ↓ RF
    NFC Tags/Cards
```

**Command Flow**:
1. Application sends APDU via PC/SC
2. ACR122U extracts PN532 command from APDU data field
3. PN532 processes the command
4. Response is wrapped back into APDU format
5. PC/SC returns response to application

### Status Word Handling

APDU responses include two-byte status words (SW1/SW2) indicating operation results:

| SW1/SW2 | Meaning | Description |
|---------|---------|-------------|
| 90 00   | Success | Command completed successfully |
| 61 XX   | More Data | XX bytes of additional data available |
| 9F XX   | More Data | XX bytes of additional data available (alternative) |
| 63 00   | Failed  | Operation failed |
| 6X XX   | Error   | Various error conditions |

## SAM Configuration Protocol

### Overview

Security Access Module (SAM) configuration is typically the first command sent to initialize the PN532 for normal mode operation. This command configures the PN532's security and communication parameters.

### Expected Command Format

```
APDU: FF 00 00 00 04 D4 14 01 00 00
```

**Breakdown**:
- `FF 00 00 00`: Standard APDU header for direct transmit
- `04`: Data length (4 bytes)
- `D4`: Direction byte (Host to PN532)
- `14`: SAM Configuration command (0x14)
- `01`: Mode (0x01 = Normal mode)
- `00`: Timeout (0x00 = no timeout)
- `00`: IRQ (0x00 = no IRQ)

### Expected Response

```
Response: D5 15 [STATUS_WORDS]
```

**Breakdown**:
- `D5`: Direction byte (PN532 to Host)
- `15`: SAM Configuration response (0x14 + 1)
- Status words typically `90 00` for success

### SAM Configuration Modes

| Mode | Value | Description |
|------|-------|-------------|
| Normal | 0x01 | Standard operation mode |
| Virtual Card | 0x02 | Card emulation mode |
| Wired Card | 0x03 | Wired card mode |
| Dual | 0x04 | Dual mode operation |

### Common Error Patterns

1. **Empty Response**: Some readers return empty responses for successful SAM config
2. **Status Word Only**: Response contains only `90 00` without PN532 data
3. **Timeout Issues**: SAM config may timeout on first attempt after reader connection
4. **Wrong Mode**: Using incorrect mode for intended application

## Implementation Examples

### Go Implementation (Current Codebase)

The project includes a robust PC/SC implementation in `transport/acr122u/pcsc.go`:

```go
// Build APDU command for PN532 communication
func buildAPDU(pn532Cmd byte, args []byte) []byte {
    dataLen := 2 + len(args) // 0xD4 + command + args
    
    apdu := make([]byte, APDUHeaderSize+dataLen+1) // +1 for Le
    apdu[0] = CLAProprietary    // CLA = 0xFF
    apdu[1] = INSDirectTransmit // INS = 0x00
    apdu[2] = 0x00              // P1
    apdu[3] = 0x00              // P2
    apdu[4] = byte(dataLen)     // Lc
    
    // Data: direction byte + PN532 command + arguments
    apdu[5] = PN532DirectionHostToPN532 // 0xD4
    apdu[6] = pn532Cmd
    if len(args) > 0 {
        copy(apdu[7:], args)
    }
    
    // Add Le (expected response length) - 0x00 means any length
    apdu[APDUHeaderSize+dataLen] = 0x00
    
    return apdu
}
```

**Key Features**:
- Lazy connection (connects on first command)
- Automatic reader detection
- Status word validation
- Response parsing with direction byte handling
- Timeout management

### Python Implementation Example

```python
from smartcard.System import readers
from smartcard.util import toHexString, toBytes

def send_pn532_command(connection, cmd, args=[]):
    """Send PN532 command via PC/SC APDU."""
    # Build APDU
    data_len = 2 + len(args)
    apdu = [0xFF, 0x00, 0x00, 0x00, data_len, 0xD4, cmd] + args + [0x00]
    
    # Send command
    response, sw1, sw2 = connection.transmit(apdu)
    
    # Check status
    if sw1 == 0x90 and sw2 == 0x00:
        # Remove direction byte if present
        if response and response[0] == 0xD5:
            return response[1:]
        return response
    else:
        raise Exception(f"APDU Error: SW1={sw1:02X} SW2={sw2:02X}")

# Example usage
reader_list = readers()
connection = reader_list[0].createConnection()
connection.connect()

# SAM Configuration
try:
    response = send_pn532_command(connection, 0x14, [0x01, 0x00, 0x00])
    print(f"SAM Config Response: {toHexString(response)}")
except Exception as e:
    print(f"SAM Config Failed: {e}")
```

### C++ Implementation Example

```cpp
#include <PCSC/winscard.h>
#include <vector>

class ACR122U_PCSC {
private:
    SCARDCONTEXT context;
    SCARDHANDLE card;
    
public:
    std::vector<uint8_t> buildAPDU(uint8_t cmd, const std::vector<uint8_t>& args) {
        std::vector<uint8_t> apdu;
        
        // APDU header
        apdu.push_back(0xFF);  // CLA
        apdu.push_back(0x00);  // INS
        apdu.push_back(0x00);  // P1
        apdu.push_back(0x00);  // P2
        apdu.push_back(2 + args.size());  // Lc
        
        // Data
        apdu.push_back(0xD4);  // Direction
        apdu.push_back(cmd);   // PN532 command
        apdu.insert(apdu.end(), args.begin(), args.end());
        
        // Le
        apdu.push_back(0x00);
        
        return apdu;
    }
    
    bool samConfiguration() {
        std::vector<uint8_t> args = {0x01, 0x00, 0x00};
        auto apdu = buildAPDU(0x14, args);
        
        uint8_t response[256];
        DWORD responseLen = sizeof(response);
        
        LONG result = SCardTransmit(card, SCARD_PCI_T1,
                                   apdu.data(), apdu.size(),
                                   nullptr, response, &responseLen);
        
        if (result != SCARD_S_SUCCESS) return false;
        
        // Check status words
        if (responseLen >= 2) {
            uint8_t sw1 = response[responseLen - 2];
            uint8_t sw2 = response[responseLen - 1];
            return (sw1 == 0x90 && sw2 == 0x00);
        }
        
        return false;
    }
};
```

## Common Issues and Debugging

### PC/SC Service Availability

**Symptoms**:
- "Failed to establish PC/SC context" errors
- Reader not found despite being connected

**Troubleshooting**:
```bash
# Linux: Check if pcscd is running
systemctl status pcscd

# Start if not running
sudo systemctl start pcscd

# Windows: Check Smart Card service
sc query SCardSvr

# macOS: PC/SC should be available by default
```

### Reader Access Conflicts

**Symptoms**:
- Reader found but connection fails
- "Device busy" or "Exclusive access" errors

**Common Causes**:
1. Multiple applications accessing the same reader
2. Previous connection not properly closed
3. System smart card service holding exclusive access

**Solutions**:
```go
// Use shared access mode
card, err := context.Connect(reader, scard.ShareShared, scard.ProtocolAny)

// Ensure proper cleanup with defer
defer card.Disconnect(scard.LeaveCard)
```

### USB vs PC/SC Mode Conflicts

**Symptoms**:
- PC/SC works but USB mode fails (or vice versa)
- Reader appears "busy" when switching modes

**Platform-Specific Issues**:

**macOS**:
- System claims exclusive USB access
- PC/SC mode works, USB mode fails with permission errors
- This is expected behavior

**Linux**:
- Both modes can work with proper permissions
- USB mode requires udev rules for device access
- PC/SC and USB modes should not run simultaneously

**Windows**:
- Both modes typically work
- May need driver installation for direct USB access

### Response Parsing Issues

**Common Problems**:

1. **Missing Direction Byte**:
```go
// Some responses may not include 0xD5 direction byte
if len(data) > 0 && data[0] == 0xD5 {
    data = data[1:] // Remove direction byte
}
```

2. **Status Word Confusion**:
```go
// Check for various success conditions
if (sw1 == 0x90 && sw2 == 0x00) || (sw1 == 0x61) || (sw1 == 0x9F) {
    // Success - remove status words
    data := resp[:len(resp)-2]
}
```

3. **Empty SAM Config Response**:
```go
// SAM config may return empty response on success
if len(resp) == 0 {
    // This might be normal for SAM config
    return nil // Assume success
}
```

### Debugging Tools and Techniques

**PC/SC Testing Tools**:
```bash
# Linux
pcsc_scan          # List readers and monitor card insertion
opensc-tool -l     # List readers
opensc-tool -a     # Get ATR from inserted card

# Windows
smartcard_list.exe # Windows SDK tool
```

**APDU Debugging**:
```go
// Log APDU commands and responses
fmt.Printf("Sending APDU: %X\n", apdu)
resp, err := card.Transmit(apdu)
fmt.Printf("Response: %X, Error: %v\n", resp, err)
```

**Common Debug Scenarios**:

1. **Reader Detection**:
```go
readers, err := context.ListReaders()
fmt.Printf("Available readers: %v\n", readers)
```

2. **Connection Status**:
```go
status, err := card.Status()
fmt.Printf("Card status: %v\n", status)
```

3. **Raw Command Testing**:
```go
// Test with raw APDU
samConfig := []byte{0xFF, 0x00, 0x00, 0x00, 0x04, 0xD4, 0x14, 0x01, 0x00, 0x00}
resp, err := card.Transmit(samConfig)
```

## Best Practices

### Initialization Sequences

**Recommended Startup Sequence**:
1. Establish PC/SC context
2. List and select appropriate reader
3. Connect to card/reader
4. Perform SAM configuration
5. Initialize PN532 for intended operations

```go
func initializeACR122U(readerName string) error {
    // 1. Establish context
    ctx, err := scard.EstablishContext()
    if err != nil {
        return fmt.Errorf("PC/SC context: %w", err)
    }
    
    // 2. Connect to reader
    card, err := ctx.Connect(readerName, scard.ShareShared, scard.ProtocolAny)
    if err != nil {
        return fmt.Errorf("connect: %w", err)
    }
    
    // 3. Optional: Disable buzzer
    buzzerCmd := []byte{0xFF, 0x00, 0x52, 0x00, 0x00}
    card.Transmit(buzzerCmd) // Ignore errors
    
    // 4. SAM Configuration
    samCmd := []byte{0xFF, 0x00, 0x00, 0x00, 0x04, 0xD4, 0x14, 0x01, 0x00, 0x00}
    _, err = card.Transmit(samCmd)
    if err != nil {
        return fmt.Errorf("SAM config: %w", err)
    }
    
    return nil
}
```

### Error Handling Patterns

**Robust Error Handling**:
```go
func sendCommand(cmd byte, args []byte) ([]byte, error) {
    apdu := buildAPDU(cmd, args)
    
    resp, err := card.Transmit(apdu)
    if err != nil {
        return nil, fmt.Errorf("transmit failed: %w", err)
    }
    
    if len(resp) < 2 {
        return nil, ErrInvalidResponse
    }
    
    // Parse status words
    sw1, sw2 := resp[len(resp)-2], resp[len(resp)-1]
    
    switch {
    case sw1 == 0x90 && sw2 == 0x00:
        // Success
        return parseResponse(resp[:len(resp)-2])
    case sw1 == 0x61:
        // More data available
        return parseResponse(resp[:len(resp)-2])
    case sw1 == 0x63 && sw2 == 0x00:
        return nil, ErrOperationFailed
    default:
        return nil, fmt.Errorf("APDU error: SW1=%02X SW2=%02X", sw1, sw2)
    }
}
```

### Connection Management

**Connection Lifecycle**:
```go
type PCScTransport struct {
    context *scard.Context
    card    *scard.Card
    mu      sync.Mutex
}

func (t *PCScTransport) ensureConnected() error {
    t.mu.Lock()
    defer t.mu.Unlock()
    
    if t.card != nil {
        // Check if connection is still valid
        if _, err := t.card.Status(); err == nil {
            return nil // Still connected
        }
        // Connection lost, clean up
        t.card.Disconnect(scard.LeaveCard)
        t.card = nil
    }
    
    // Establish new connection
    card, err := t.context.Connect(t.readerName, 
                                  scard.ShareShared, 
                                  scard.ProtocolAny)
    if err != nil {
        return fmt.Errorf("reconnect failed: %w", err)
    }
    
    t.card = card
    return nil
}
```

### Cross-Platform Considerations

**Platform-Specific Handling**:
```go
func detectBestMode() Mode {
    switch runtime.GOOS {
    case "darwin":
        // macOS: PC/SC preferred due to system restrictions
        return ModePCSC
    case "linux":
        // Linux: Try PC/SC first, USB as fallback
        return ModeAuto
    case "windows":
        // Windows: Both modes typically work
        return ModeAuto
    default:
        return ModePCSC // Conservative default
    }
}
```

**Permission Handling**:
```go
func checkUSBPermissions() error {
    if runtime.GOOS == "linux" {
        // Check if udev rules are in place
        if _, err := os.Stat("/etc/udev/rules.d/99-acr122u.rules"); err != nil {
            return fmt.Errorf("USB permissions not configured: %w", err)
        }
    }
    return nil
}
```

## References and Resources

### Official Documentation
- [ACS ACR122U API Manual](https://www.acs.com.hk/en/products/3/acr122u-usb-nfc-reader/) - Official hardware documentation
- [PC/SC Specification](https://pcscworkgroup.com/) - PC/SC standards and specifications
- [PN532 User Manual](https://www.nxp.com/docs/en/user-guide/141520.pdf) - NXP PN532 documentation

### Useful Libraries and Tools

**Go Libraries**:
- `github.com/ebfe/scard` - PC/SC wrapper for Go
- `github.com/google/gousb` - USB communication library
- `github.com/ZaparooProject/go-pn532` - This project's PN532 implementation

**Python Libraries**:
- `pyscard` - Python PC/SC wrapper
- `smartcard` - High-level smart card library

**Testing Tools**:
- `pcsc_scan` - Monitor PC/SC reader activity (Linux)
- `pcsctest` - Test PC/SC functionality
- `opensc-tools` - OpenSC smart card utilities
- `GlobalPlatform` - Smart card testing framework

### Community Resources
- [PC/SC Developer Resources](https://ludovic.rousseau.free.fr/softwares/pcsc-lite/) - Comprehensive PC/SC information
- [NFC Tools and Libraries](https://github.com/nfc-tools) - Open source NFC development tools
- [ACR122U Community Forum](https://www.acs.com.hk/en/support/) - Official support and documentation

### Debugging and Development
- [APDU Command Reference](https://cardwerk.com/smart-card-standard-iso7816-4-section-5-basic-organizations/#chap5_4) - ISO 7816-4 APDU specification
- [Smart Card Development](https://www.cardlogix.com/smart-card-developer-information/) - General smart card development resources
- [NFC Development Guide](https://developer.android.com/guide/topics/connectivity/nfc) - Android NFC development (concepts apply generally)

---

**Document Version**: 1.0  
**Last Updated**: 2025-07-31  
**Maintainer**: go-pn532 project team

This document is part of the go-pn532 project documentation. For updates and corrections, please refer to the project repository.