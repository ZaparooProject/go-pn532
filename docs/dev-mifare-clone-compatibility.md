# MIFARE Classic Clone Compatibility Notes

## Issue: Authentication Error 0x14

When attempting to authenticate with MIFARE Classic tags, you may encounter the error:
```
authentication failed: data exchange error: 14
```

## Root Cause

Error code `0x14` from the PN532 indicates a MIFARE authentication error. This occurs when the tag rejects the authentication attempt. If this happens with ALL standard keys (default, NDEF, zeros, MAD), it strongly indicates that the tag is a MIFARE Classic clone or compatible tag with limited functionality.

## Technical Details

1. **Authentication Command Structure**: The implementation correctly sends:
   - Command: `0x60` (Key A) or `0x61` (Key B)
   - Block number
   - First 4 bytes of UID
   - 6-byte authentication key

2. **PN532 Protocol**: The command is properly wrapped using `InDataExchange` (0x40) with target number 0x01.

3. **Clone Limitations**: Many cheap MIFARE Classic clones:
   - Don't implement the full authentication protocol
   - May only support direct block read/write without authentication
   - Report as MIFARE Classic 1K (SAK 0x08) but lack proper crypto implementation

## Detection and Handling

The `readtag` tool now includes enhanced detection for clone tags:

```go
// Try authentication with common keys
keys := []struct {
    name string
    key  []byte
}{
    {"Default (FF)", []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}},
    {"NDEF", []byte{0xD3, 0xF7, 0xD3, 0xF7, 0xD3, 0xF7}},
    {"All zeros", []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
    {"MAD", []byte{0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5}},
}
```

If none of these keys work, the tool will display a warning about potential clone compatibility issues.

## Recommendations

1. **For Development**: Use genuine MIFARE Classic tags from reputable suppliers (NXP or authorized distributors)

2. **For Testing**: Keep both genuine and clone tags to ensure compatibility testing

3. **For Production**: 
   - Clearly specify genuine MIFARE Classic requirements
   - Consider implementing fallback mechanisms for clone tags if needed
   - Test with various tag sources before deployment

## Clone Tag Capabilities

Clone tags have varying levels of functionality:

1. **Read-Only Clones**: Support reading blocks directly but not writing
2. **Limited Clones**: Support both reading and writing without authentication
3. **UID-Only Clones**: Only respond to UID queries, no block access

The library now automatically tests clone capabilities:

```go
canRead, canWrite := mifareTag.TestCloneCapabilities()
if !canWrite {
    if canRead {
        fmt.Println("Clone tag is read-only")
    } else {
        fmt.Println("Clone tag has very limited functionality")
    }
}
```

## Alternative Approaches

If you must support MIFARE Classic clones:

1. **Direct Block Access**: Some clones support direct read/write without authentication (not recommended for security)

2. **Different Tag Types**: Consider using NTAG21x series which have better clone detection and more consistent implementations

3. **UID-Only Applications**: If only the UID is needed, clones can still be detected and read without authentication

4. **Read-Only Applications**: If clone tags are read-only, consider applications that only need to read existing data

## Debug Output Example

When a clone is detected, you'll see:
```
=== MIFARE Classic Tag ===

Checking tag authentication...

⚠️  WARNING: Cannot authenticate with any standard key!
This tag may be:
  • A MIFARE Classic clone with limited functionality
  • Using non-standard authentication keys
  • Damaged or malfunctioning

MIFARE Classic clones often don't support proper authentication.
Consider using a genuine MIFARE Classic tag for full compatibility.
```

## References

- PN532 User Manual: Error code 0x14 - "Mifare: Authentication error"
- MIFARE Classic specifications require proper 3-pass authentication
- Clone tags often implement only basic functionality to reduce costs