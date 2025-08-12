# Tag Operation Patterns Documentation

This document captures the working patterns extracted from cmd/pn532record/main.go for both NTAG and MIFARE Classic operations.

## NTAG Fast Read Pattern

### Key Implementation Details

1. **Fast Read Command**: `0x3A` (FAST_READ)
   - Syntax: `[]byte{0x3A, startPage, endPage}`
   - Can read up to 16 pages at once
   - More efficient than single page reads

2. **Working Implementation**:
```go
// Create NTAG tag instance
ntag := pn532.NewNTAGTag(device, tag.UIDBytes)

// Detect type to get total pages
err := ntag.DetectType()
totalPages := int(ntag.GetTotalPages())

// Fast read for efficiency (reads 4 pages at a time)
startPage := 4
for startPage < totalPages {
    endPage := startPage + 15 // Fast read can read up to 16 pages
    if endPage >= totalPages {
        endPage = totalPages - 1
    }
    
    // Use SendRawCommand for fast read
    data, err := device.SendRawCommand([]byte{0x3A, byte(startPage), byte(endPage)})
    if err != nil {
        // Fallback to individual page reads
        for page := startPage; page <= endPage && page < totalPages; page++ {
            pageData, err := device.SendDataExchange([]byte{0x30, byte(page)})
        }
    }
    
    startPage = endPage + 1
}
```

3. **Important Notes**:
   - Fast read uses `SendRawCommand`, not `SendDataExchange`
   - Fallback to single page reads (command `0x30`) if fast read fails
   - Read in chunks of 16 pages for efficiency

## MIFARE Classic Authentication Pattern

### Key Implementation Details

1. **Key Provider System**:
   - Use `pn532.NewStaticKeyProvider` for key management
   - Set provider before authentication: `mifare.SetKeyProvider(provider)`
   - Use `AuthenticateWithProvider` for robust authentication

2. **Working Implementation**:
```go
// Create MIFARE tag instance
mifare := pn532.NewMIFARETag(device, tag.UIDBytes)

// Common keys to try
keys := []struct {
    name string
    key  []byte
}{
    {"NDEF", []byte{0xD3, 0xF7, 0xD3, 0xF7, 0xD3, 0xF7}}, // Try NDEF key first
    {"Default (FF)", []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}},
    {"All zeros", []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
    {"MAD", []byte{0xA0, 0xA1, 0xA2, 0xA3, 0xA4, 0xA5}},
}

// Try authentication with each key
var successfulKey []byte
for _, k := range keys {
    // Set the key provider first
    provider := pn532.NewStaticKeyProvider(k.key, k.key)
    mifare.SetKeyProvider(provider)
    
    // Try authentication using the key provider system
    err := mifare.AuthenticateWithProvider(1, 0x00) // Sector 1, Key A
    if err == nil {
        successfulKey = k.key
        break
    }
}

// Once authenticated, use auto methods
if successfulKey != nil {
    provider := pn532.NewStaticKeyProvider(successfulKey, successfulKey)
    mifare.SetKeyProvider(provider)
    
    // ReadBlockAuto handles re-authentication automatically
    data, err := mifare.ReadBlockAuto(block)
    
    // WriteBlockAuto handles re-authentication automatically
    err := mifare.WriteBlockAuto(block, data)
}
```

3. **Important Notes**:
   - Always use key provider system, not direct authentication
   - Use `ReadBlockAuto` and `WriteBlockAuto` for automatic re-authentication
   - NDEF key (0xD3F7D3F7D3F7) should be tried first for NDEF-formatted cards

## Error Handling Patterns

1. **Graceful Fallback**: If fast read fails, fall back to single page reads
2. **Key Discovery**: Try multiple common keys in order of likelihood
3. **Silent Failures**: Some operations (like version read) may fail on certain tags - handle gracefully

## Key Differences from Common Mistakes

1. **NTAG**: 
   - Use `SendRawCommand` for fast read, not `SendDataExchange`
   - Read in chunks of 16 pages, not entire tag at once
   
2. **MIFARE**:
   - Use key provider system, not direct auth methods
   - Use `*Auto` methods for read/write to handle re-auth
   - Always set key provider before operations