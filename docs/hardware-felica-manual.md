# FeliCa Hardware Manual

**go-pn532 Library - FeliCa NFC Type 3 Tag Support**

## Overview

FeliCa is a contactless RFID smart card system developed by Sony, widely used in Japan for transit cards, payment systems, and access control. This library provides comprehensive support for FeliCa cards through the PN532 NFC controller.

## FeliCa Specifications

### Technical Characteristics
- **Frequency**: 13.56 MHz (NFC Type 3)
- **Data Rates**: 212 kbps, 424 kbps
- **Block Size**: 16 bytes (vs 4 bytes for MIFARE)
- **Address Space**: 16-bit block addressing (up to 65,536 blocks)
- **Max Range**: ~10cm (depending on antenna design)

### Card Structure
- **IDm (Manufacture ID)**: 8 bytes - unique card identifier
- **PMm (Manufacture Parameter)**: 8 bytes - card capabilities and parameters
- **System Code**: 16-bit identifier for card applications
- **Service Code**: 16-bit identifier for specific services within a system

## Supported System Codes

| System Code | Description | Usage |
|-------------|-------------|-------|
| `0x12FC` | NFC Forum Type 3 Tag | NDEF applications |
| `0xFFFF` | Wildcard/Common | General polling |
| Custom | Application-specific | Transit, payment, etc. |

## Supported Service Codes

| Service Code | Access | Description |
|--------------|--------|-------------|
| `0x000B` | Read | NDEF data reading |
| `0x0009` | Write | NDEF data writing |
| Custom | Varies | Application-specific services |

## FeliCa Commands

### Core Commands
- **Polling (0x00)**: Detect and select FeliCa cards
- **Read Without Encryption (0x06)**: Read data blocks
- **Write Without Encryption (0x08)**: Write data blocks
- **Request Service (0x02)**: Query available services

### NDEF Operations
- **ReadNDEF()**: Read NFC Forum Type 3 Tag data
- **WriteNDEF()**: Write NFC Forum Type 3 Tag data with atomic operations

## Memory Layout

### Attribute Information Block (AIB) - Block 0
```
Byte | Field | Description
-----|-------|------------
0    | Ver   | Version (0x10 for v1.0)
1    | Nbr   | Number of blocks readable at once
2    | Nbw   | Number of blocks writable at once
3-4  | Nmaxb | Maximum number of blocks (big endian)
5-8  | RFU   | Reserved for Future Use
9    | WriteF| Write flag (0x00=allowed, 0x0F=prohibited)
10   | RWFlag| Read/Write flag (0x00=R/W, 0x01=RO)
11-13| Ln    | NDEF data length (3 bytes, big endian)
14-15| Check | Checksum of bytes 0-13 (big endian)
```

### NDEF Data Blocks - Block 1+
```
Block 1: First 16 bytes of NDEF data
Block 2: Next 16 bytes of NDEF data
...
Block N: Remaining NDEF data (zero-padded)
```

## Transport Compatibility

FeliCa is supported across all transport types:

- **UART**: `/dev/ttyUSB0`, `/dev/ttyACM0`, `COM3`
- **I2C**: Standard I2C bus communication
- **SPI**: SPI bus communication
- **ACR122U**: USB NFC reader (PC/SC mode)

## Error Handling

### Common Error Codes
- **No Tag Detected**: No FeliCa card in field
- **Transport Timeout**: Communication timeout with PN532
- **Invalid Response**: Malformed response from card
- **Write Protected**: Card or NDEF area is write-protected
- **Invalid Block**: Block number out of range
- **Checksum Error**: AIB checksum validation failed

### Status Flags
FeliCa responses include status flags:
- `0x00 0x00`: Success
- `0xFF 0xFF`: General error
- Other values: Specific error conditions

## Performance Considerations

### Optimization Tips
1. **System Code Selection**: Use specific system codes rather than wildcard
2. **Service Code Caching**: Cache service codes to avoid repeated requests
3. **Block Alignment**: Write full 16-byte blocks when possible
4. **Polling Frequency**: Balance detection speed vs power consumption

### Typical Performance
- **Detection Time**: 50-150ms
- **Block Read**: 10-20ms per block
- **Block Write**: 15-30ms per block
- **NDEF Read**: 100-500ms (depending on size)
- **NDEF Write**: 200-800ms (depending on size)

## Security Considerations

### Access Control
- FeliCa supports authentication and encryption (not implemented in this library)
- This library only supports "without encryption" operations
- Suitable for public/read-only applications and development

### Data Protection
- Always validate checksums for critical data
- Implement application-level data validation
- Consider card removal during operations

## Troubleshooting

### Common Issues

**Card Not Detected**
- Check antenna positioning and distance
- Verify system code compatibility
- Try wildcard polling (0xFFFF)

**Read/Write Failures**
- Verify service code permissions
- Check card write protection
- Ensure stable card positioning

**NDEF Parsing Errors**
- Validate AIB checksum
- Check NDEF data length
- Verify Type 3 Tag compliance

**Transport Errors**
- Check PN532 initialization
- Verify transport configuration
- Monitor for electromagnetic interference

### Debug Tips
1. Enable verbose logging for command/response analysis
2. Use mock transport for protocol testing
3. Validate responses against JIS X 6319-4 specification
4. Test with multiple card types if available

## References

- **JIS X 6319-4**: FeliCa command specification
- **NFC Forum Type 3 Tag**: NDEF format specification
- **PN532 User Manual**: Hardware interface details
- **Sony FeliCa Documentation**: Official specifications

## Examples

See the `examples/` directory for practical usage examples:
- Basic FeliCa reading
- NDEF operations
- Service discovery
- Error handling