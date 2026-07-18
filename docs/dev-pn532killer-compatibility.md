# PN532Killer Compatibility

`go-pn532` supports PN532Killer readers through the existing UART transport. Applications use the normal constructor and do not need a separate driver or configuration option:

```go
transport, err := uart.New("/dev/ttyACM0")
```

## Identification

The compatibility profile is selected automatically only when the serial device reports all of the following USB metadata:

- Vendor ID `1A86`
- Product ID `55D3`
- Product string `PN532Killer-UART`

Matching is case-insensitive and ignores surrounding whitespace. Serial-device symlinks are resolved before matching. If enumeration fails or any identity field is missing or different, the generic PN532 UART profile is used intentionally.

The PN532Killer profile opens the port with DTR and RTS deasserted and omits the host ACK normally sent after a PN532 response. Reconnects reuse the profile selected during the initial open.

## Supported operations

- Reader initialization and automatic detection
- NFC Forum Type 2 and NTAG UID detection
- NTAG page reads and NDEF reads with automatic fallback when `FAST_READ` is unavailable
- NTAG page writes with Type 2 ACK validation
- NDEF reads and writes on supported NTAG21x tags
- NTAG version and password-authentication commands

PN532Killer Type 2 commands use `InCommunicateThru` with ISO/IEC 14443-A CRC bytes. This is selected through a transport capability, keeping the standard PN532 UART command path unchanged.

## Limitations

- MIFARE operations continue to use the existing implementation but are not included in the PN532Killer compatibility claim.
- The observed firmware acknowledges `InSelect` without returning a response and reports framing error `0x05` for `FAST_READ`. The compatibility path preserves selection without `InSelect` and uses standard Type 2 `READ` commands for NDEF data.
- Card emulation, traffic sniffing, ISO15693, and PN532Killer-specific extended features are not supported by this integration.
- There is no firmware-version gate. Compatibility assumes firmware with the same initialization and raw Type 2 behavior as the supported hardware.
- There is no manual override for incomplete USB metadata. This prevents an unrelated device with the same USB bridge from receiving incompatible serial and tag-command behavior.

## Troubleshooting

If a reader opens as a generic PN532 or is not detected:

1. Confirm the application is using the device path that belongs to the reader. Persistent `/dev/serial/by-id` symlinks are supported.
2. Inspect the USB metadata and confirm all three identity fields above are present. On Linux, `udevadm info --query=property --name=/dev/ttyACM0` can show the enumerated vendor, product ID, and product string.
3. Confirm the process has read/write permission for the serial device.
4. Disconnect and reconnect the reader after changing permissions or udev rules.

If the metadata differs on otherwise compatible hardware, capture the reported values and firmware details before proposing another identity profile. Do not broaden the existing match to VID/PID alone.
