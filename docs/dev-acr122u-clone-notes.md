# Clone ACR122U Notes

This document tracks observations and quirks specific to clone ACR122U readers.

## Clone Characteristics

### 1. LED/Buzzer Behavior
- **Clone**: LED and buzzer remain active even without an active PC/SC connection
- **Genuine**: LED and buzzer only activate during an active session
- **Impact**: No functional impact, just cosmetic difference

### 2. PC/SC Behavior  
- **All devices (genuine and clones)** work perfectly via PC/SC
- All report as "ACS ACR122U PICC Interface"
- No functional differences detected

## Known Issues

### gousb on macOS Bug
- **Issue**: gousb v1.1.3 returns incorrect VID:PID values on macOS
- **Affects**: ALL ACR122U devices (genuine and clones)
- **Symptom**: VID=0x30373266, PID=0x32323030 instead of VID=0x072F, PID=0x2200
- **Analysis**: These are ASCII representations:
  - 0x30373266 = "072f" in ASCII (truncated to 0x3266 in uint16)
  - 0x32323030 = "2200" in ASCII (truncated to 0x3030 in uint16)
- **Verification**: macOS system_profiler shows correct values (0x072F:0x2200)
- **Impact**: 
  - Direct USB detection via gousb is broken on macOS
  - PC/SC works perfectly as it doesn't rely on USB enumeration
  - Linux systems may not be affected (needs testing)
- No issues with PC/SC communication

## Detection Strategy

1. **PC/SC**: Works normally, no special handling needed
2. **USB**: Need to check for both numeric and ASCII-encoded VID:PID values

## Recommended Approach

### Current Status:
1. **PC/SC**: Works perfectly for all ACR122U devices (genuine and clones)
2. **Direct USB on macOS**: Broken due to gousb bug (affects all devices)
3. **Direct USB on Linux**: Needs testing (may work correctly)

### Best Practices:
1. **Always use PC/SC when available** - Most reliable option
2. **For embedded Linux**: 
   - Test if gousb works correctly on your platform
   - Consider PC/SC lite if USB detection fails
   - May need to patch gousb or use alternative USB library
3. **Auto mode (default)**: Tries PC/SC first, falls back to USB
   - This is the recommended approach
   - Handles all devices well on systems with PC/SC

## Open Questions

1. ~~Is this ASCII encoding specific to certain clone manufacturers?~~ No, it's a gousb bug on macOS
2. Does gousb work correctly on Linux for USB enumeration?
3. Are there alternative Go USB libraries that work correctly on macOS?
4. Should we report this bug to the gousb project?

## Testing Needed

1. Test USB communication with clone (not just enumeration)
2. Check if libnfc has similar issues with these clones
3. Test on Linux to see if behavior is OS-specific
4. Compare USB descriptors between genuine and clone devices

## References

- libnfc has known issues with clone ACR122U direct USB
- This ASCII VID:PID issue might be related to why libnfc fails