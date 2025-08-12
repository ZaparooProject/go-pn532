# PN532 InAutoPoll Response Format Documentation

## Overview

The PN532 InAutoPoll command (0x60) is used to automatically poll for NFC tags in the field. This document provides comprehensive documentation of the response format, particularly for ISO14443A tags, including the critical differences between mock implementations and real hardware behavior.

## Command Structure

The InAutoPoll command has the following structure:
```
Command: 0x60
Parameters: [PollCount] [PollPeriod] [TargetType1] [TargetType2] ...
```

## Response Format

### Overall Response Structure
```
[ResponseCode] [NumTargets] [Target1Data] [Target2Data] ...
```

Where:
- `ResponseCode`: Always 0x61 for InAutoPoll
- `NumTargets`: Number of targets found (0x00 if none)
- `TargetData`: Target-specific data for each found target

### Target Data Structure
Each target entry has the following format:
```
[TargetType] [DataLength] [TargetSpecificData]
```

## ISO14443A Target Data Format

### The Critical Discovery: Unknown/Reserved Byte

Through hardware protocol investigation, we discovered that **real PN532 hardware includes an extra "unknown/reserved" byte** in InAutoPoll responses for ISO14443A targets that is not documented in standard references.

#### Mock/Expected Format (Incorrect)
```
ATQ(2) + SAK(1) + UID_LENGTH(1) + UID(n)
```

#### Real Hardware Format (Correct)
```
ATQ(2) + SAK(1) + UNKNOWN(1) + UID_LENGTH(1) + UID(n)
```

### Detailed Format Breakdown

| Offset | Size | Field | Description |
|--------|------|-------|-------------|
| 0-1    | 2    | ATQ   | SENS_RES - Answer to Request (Anti-collision) |
| 2      | 1    | SAK   | SEL_RES - Select Acknowledge |
| 3      | 1    | UNKNOWN | **Undocumented byte** - Purpose unknown, always present in real hardware |
| 4      | 1    | UID_LENGTH | Length of the UID that follows |
| 5+     | n    | UID   | The tag's unique identifier |

### Real Hardware Example

From NTAG215 hardware testing:
```
Raw Data: 01 00 44 00 07 04 c6 3a 63 11 01 89

Parsed as:
- ATQ:        01 00    (SENS_RES)
- SAK:        44       (SEL_RES) 
- UNKNOWN:    00       (Undocumented byte)
- UID_LENGTH: 07       (7-byte UID)
- UID:        04 c6 3a 63 11 01 89
```

## Implementation Details

### Code Implementation

The library correctly handles this format in `device_context.go`:

```go
// parseISO14443AData parses ISO14443 Type A target data
// Format: ATQ(2) + SAK(1) + UNKNOWN(1) + UID_LENGTH(1) + UID(n)
func (d *Device) parseISO14443AData(targetData []byte) (uid []byte, atq []byte, sak byte) {
    // ... validation code ...
    
    // Parse ATQ and SAK from the first 3 bytes
    atq = targetData[0:2]   // SENS_RES (ATQ)
    sak = targetData[2]     // SEL_RES (SAK)
    
    // Parse UID length at offset 4 (real hardware format)
    // Format: ATQ(2) + SAK(1) + UNKNOWN(1) + UID_LENGTH(1) + UID(n)
    uidLen := targetData[4]
    if uidLen > 0 && len(targetData) >= 5+int(uidLen) {
        uid = targetData[5 : 5+int(uidLen)]
    }
    
    return uid, atq, sak
}
```

### Mock Transport Implementation

The mock transport correctly simulates the real hardware format:

```go
// Build target data using real hardware format: ATQ + SAK + UNKNOWN + UID_LENGTH + UID
targetData := make([]byte, 0, len(atq)+1+1+1+len(uid))
targetData = append(targetData, atq...)
targetData = append(targetData, sak)
targetData = append(targetData, 0x00)            // UNKNOWN byte (real hardware includes this)
targetData = append(targetData, byte(len(uid)))  // UID length
targetData = append(targetData, uid...)          // Add UID
```

## Why This Matters

### Compatibility Issues

1. **Hardware vs Mock Differences**: Libraries that don't account for this extra byte will fail when used with real PN532 hardware
2. **Parsing Errors**: Incorrect parsing leads to:
   - Wrong UID extraction (off by one byte)
   - Invalid UID length interpretation
   - Complete parsing failure

### Historical Context

This discrepancy likely exists because:
1. Early documentation may have omitted this implementation detail
2. The byte may be reserved for future use or internal hardware state
3. Different firmware versions may have varying behavior

## Transport-Specific Considerations

### All Transports Affected

This format applies to all PN532 transports:
- UART
- I2C  
- SPI
- USB/PC-SC (ACR122U)

The unknown byte is part of the PN532 chip's response format, not transport-specific framing.

### Testing Implications

When creating mock responses for testing:

```go
// Correct format for test data
response := []byte{
    0x61,                   // InAutoPoll response code
    0x01,                   // 1 target found
    0x10,                   // Target type: ISO14443 Type A
    byte(len(targetData)),  // Data length
}
// targetData should include the UNKNOWN byte at offset 3
```

## Recommendations

### For Library Developers

1. **Always include the unknown byte** in mock implementations
2. **Parse UID at offset 5**, not offset 4
3. **Test with real hardware** to validate response parsing
4. **Document this discrepancy** in API documentation

### For Application Developers

1. Use this library's `parseISO14443AData` function rather than custom parsing
2. Be aware that raw InAutoPoll responses include this extra byte
3. Don't rely on theoretical documentation alone - test with real hardware

## Related Files

- `/autopoll_test.go`: Contains `TestDevice_InAutoPoll_RealHardwareFormat` test
- `/device_context.go`: Contains `parseISO14443AData` implementation  
- `/internal/mocktest/mock_transport.go`: Mock transport with correct format
- `/docs/hardware-pn532-manual.md`: General PN532 documentation

## Conclusion

The "unknown byte" in PN532 InAutoPoll responses for ISO14443A targets is a real implementation detail that must be accounted for in any library that works with actual PN532 hardware. This library correctly handles this format, ensuring compatibility with real-world deployments.

**Key Takeaway**: Always test NFC libraries with real hardware, as undocumented implementation details can cause significant compatibility issues.