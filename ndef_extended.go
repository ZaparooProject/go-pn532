// go-pn532
// Copyright (c) 2025 The Zaparoo Project Contributors.
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of go-pn532.
//
// go-pn532 is free software; you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public
// License as published by the Free Software Foundation; either
// version 3 of the License, or (at your option) any later version.
//
// go-pn532 is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
// Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with go-pn532; if not, write to the Free Software Foundation,
// Inc., 51 Franklin Street, Fifth Floor, Boston, MA  02110-1301, USA.

package pn532

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/hsanjuan/go-ndef"
)

// Security constants for NDEF Extended validation
const (
	maxWiFiSSIDLength       = 32   // Maximum WiFi SSID length (IEEE 802.11 standard)
	maxWiFiNetworkKeyLength = 64   // Maximum WPA2/WPA3 key length
	maxWiFiMACAddressLength = 17   // Maximum MAC address string length (XX:XX:XX:XX:XX:XX)
	maxVCardFieldLength     = 1024 // Maximum vCard field length
	maxVCardAddressCount    = 10   // Maximum addresses per vCard
	maxVCardPhoneCount      = 20   // Maximum phone numbers per vCard
	maxVCardEmailCount      = 10   // Maximum email addresses per vCard
)

// Extended NDEF record types
const (
	// NDEFTypeWiFi represents a WiFi credential record
	NDEFTypeWiFi NDEFRecordType = "wifi"
	// NDEFTypeVCard represents a vCard contact record
	NDEFTypeVCard NDEFRecordType = "vcard"
	// NDEFTypeBluetooth represents a Bluetooth pairing record
	NDEFTypeBluetooth NDEFRecordType = "bluetooth"

	// vCard constants
	beginVCard = "BEGIN:VCARD"

	// NDEF media types
	mediaTypeWiFi  = "application/vnd.wfa.wsc"
	mediaTypeVCard = "text/vcard"
)

// WiFi authentication and encryption types
const (
	AuthTypeOpen    = 0x0001
	AuthTypeWPA     = 0x0002
	AuthTypeWPAPSK  = 0x0004
	AuthTypeWPA2    = 0x0008
	AuthTypeWPA2PSK = 0x0020

	EncryptTypeNone = 0x0001
	EncryptTypeWEP  = 0x0002
	EncryptTypeTKIP = 0x0004
	EncryptTypeAES  = 0x0008
)

// WiFiCredential represents WiFi network credentials
type WiFiCredential struct {
	SSID           string
	NetworkKey     string
	MACAddress     string // Optional
	AuthType       uint16
	EncryptionType uint16
	Hidden         bool // Hidden SSID
}

// VCardContact represents contact information
type VCardContact struct {
	Version        string
	FormattedName  string
	FirstName      string
	LastName       string
	Organization   string
	Title          string
	PhoneNumbers   map[string]string // Type -> Number
	EmailAddresses map[string]string // Type -> Email
	Addresses      map[string]Address
	URL            string
	Note           string
}

// Address represents a physical address
type Address struct {
	Street     string
	City       string
	State      string
	PostalCode string
	Country    string
}

// BuildWiFiRecord creates an NDEF record with WiFi credentials
func BuildWiFiRecord(cred WiFiCredential) (*ndef.Record, error) {
	// WiFi Simple Configuration (WSC) uses application/vnd.wfa.wsc MIME type
	payload, err := encodeWiFiCredential(cred)
	if err != nil {
		return nil, fmt.Errorf("failed to encode WiFi credential: %w", err)
	}

	// Create a media type record using go-ndef API
	rec := ndef.NewMediaRecord(mediaTypeWiFi, payload)
	// Clear message flags - they will be set by the message builder
	rec.SetMB(false)
	rec.SetME(false)

	return rec, nil
}

// Helper function to write TLV (Type-Length-Value) to buffer
func writeTLV(buf *bytes.Buffer, typ uint16, data []byte) error {
	if err := binary.Write(buf, binary.BigEndian, typ); err != nil {
		return fmt.Errorf("failed to write TLV type: %w", err)
	}
	if len(data) > 0xFFFF {
		return fmt.Errorf("TLV data too long: %d bytes", len(data))
	}
	// Safe to convert after length check above
	//nolint:gosec // length check above ensures safe conversion
	if err := binary.Write(buf, binary.BigEndian, uint16(len(data))); err != nil {
		return fmt.Errorf("failed to write TLV length: %w", err)
	}
	if _, err := buf.Write(data); err != nil {
		return fmt.Errorf("failed to write TLV data: %w", err)
	}
	return nil
}

// encodeWiFiCredential encodes WiFi credentials in WSC format
func encodeWiFiCredential(cred WiFiCredential) ([]byte, error) {
	var buf bytes.Buffer

	if err := writeCredentialHeader(&buf); err != nil {
		return nil, err
	}

	credLenPos, credStart, err := reserveCredentialLength(&buf)
	if err != nil {
		return nil, err
	}

	if err := writeCredentialTLVs(&buf, cred); err != nil {
		return nil, err
	}

	return finalizeCredentialData(&buf, credLenPos, credStart)
}

func writeCredentialHeader(buf *bytes.Buffer) error {
	if err := binary.Write(buf, binary.BigEndian, uint16(0x100E)); err != nil {
		return fmt.Errorf("failed to write credential TLV header: %w", err)
	}
	return nil
}

func reserveCredentialLength(buf *bytes.Buffer) (credLenPos, credStart int, err error) {
	credLenPos = buf.Len()
	if err = binary.Write(buf, binary.BigEndian, uint16(0)); err != nil {
		return 0, 0, fmt.Errorf("failed to write credential length placeholder: %w", err)
	}
	credStart = buf.Len()
	return credLenPos, credStart, nil
}

func writeCredentialTLVs(buf *bytes.Buffer, cred WiFiCredential) error {
	// Network Index (fixed to 1)
	if err := writeTLV(buf, 0x1026, []byte{0x01}); err != nil {
		return err
	}

	// SSID
	if cred.SSID != "" {
		if err := writeTLV(buf, 0x1045, []byte(cred.SSID)); err != nil {
			return err
		}
	}

	// Authentication and Encryption types
	if err := writeAuthAndEncTypes(buf, cred); err != nil {
		return err
	}

	// Network Key
	if cred.NetworkKey != "" {
		if err := writeTLV(buf, 0x1027, []byte(cred.NetworkKey)); err != nil {
			return err
		}
	}

	// MAC Address (optional)
	return writeMACAddress(buf, cred.MACAddress)
}

func writeAuthAndEncTypes(buf *bytes.Buffer, cred WiFiCredential) error {
	// Authentication Type
	authData := make([]byte, 2)
	binary.BigEndian.PutUint16(authData, cred.AuthType)
	if err := writeTLV(buf, 0x1003, authData); err != nil {
		return err
	}

	// Encryption Type
	encData := make([]byte, 2)
	binary.BigEndian.PutUint16(encData, cred.EncryptionType)
	return writeTLV(buf, 0x100F, encData)
}

func writeMACAddress(buf *bytes.Buffer, macAddress string) error {
	if macAddress != "" {
		if macBytes, err := parseMACAddress(macAddress); err == nil {
			return writeTLV(buf, 0x1020, macBytes)
		}
	}
	return nil
}

func finalizeCredentialData(buf *bytes.Buffer, credLenPos, credStart int) ([]byte, error) {
	credLen := buf.Len() - credStart
	data := buf.Bytes()
	if credLen > 0xFFFF {
		return nil, errors.New("credential data too long")
	}
	// Safe to convert after length check above
	//nolint:gosec // length check above ensures safe conversion
	binary.BigEndian.PutUint16(data[credLenPos:], uint16(credLen))
	return data, nil
}

// ParseWiFiRecord parses a WiFi credential from an NDEF record
func ParseWiFiRecord(rec *ndef.Record) (*WiFiCredential, error) {
	// SECURITY: Validate record before processing
	if rec == nil {
		return nil, fmt.Errorf("%w: nil WiFi record", ErrSecurityViolation)
	}

	if rec.TNF() != ndef.MediaType || rec.Type() != mediaTypeWiFi {
		return nil, errors.New("not a WiFi credential record")
	}

	payload, err := rec.Payload()
	if err != nil {
		return nil, fmt.Errorf("failed to get WiFi record payload: %w", err)
	}

	// SECURITY: Validate payload size before processing
	payloadData := payload.Marshal()
	if len(payloadData) > MaxNDEFPayloadSize {
		return nil, fmt.Errorf("%w: WiFi payload size %d exceeds maximum %d",
			ErrSecurityViolation, len(payloadData), MaxNDEFPayloadSize)
	}

	return parseWiFiCredential(payloadData)
}

// parseWiFiCredential parses WSC format WiFi credentials
func parseWiFiCredential(data []byte) (*WiFiCredential, error) {
	cred := &WiFiCredential{}
	reader := bytes.NewReader(data)

	if err := validateCredentialHeader(reader); err != nil {
		return nil, err
	}

	lastErr := parseCredentialAttributes(reader, cred)
	return cred, lastErr
}

func validateCredentialHeader(reader *bytes.Reader) error {
	var credType, credLen uint16
	if err := binary.Read(reader, binary.BigEndian, &credType); err != nil {
		return fmt.Errorf("failed to read credential type: %w", err)
	}
	if err := binary.Read(reader, binary.BigEndian, &credLen); err != nil {
		return fmt.Errorf("failed to read credential length: %w", err)
	}

	if credType != 0x100E {
		return fmt.Errorf("not a credential TLV: got 0x%04X", credType)
	}
	return nil
}

func parseCredentialAttributes(reader *bytes.Reader, cred *WiFiCredential) error {
	var lastErr error
	for reader.Len() > 4 {
		attrType, value, err := readAttribute(reader)
		if err != nil {
			lastErr = err
			break
		}
		processWiFiAttribute(cred, attrType, value)
	}
	return lastErr
}

func readAttribute(reader *bytes.Reader) (attrType uint16, data []byte, err error) {
	var attrLen uint16
	if typeErr := binary.Read(reader, binary.BigEndian, &attrType); typeErr != nil {
		return 0, nil, fmt.Errorf("failed to read WSC attribute type: %w", typeErr)
	}
	if lenErr := binary.Read(reader, binary.BigEndian, &attrLen); lenErr != nil {
		return 0, nil, fmt.Errorf("failed to read WSC attribute length: %w", lenErr)
	}

	data = make([]byte, attrLen)
	if _, err = reader.Read(data); err != nil {
		return 0, nil, fmt.Errorf("failed to read WSC attribute data: %w", err)
	}
	return attrType, data, nil
}

// validateWiFiSSID validates WiFi SSID length and content
func validateWiFiSSID(ssid string) error {
	if len(ssid) > maxWiFiSSIDLength {
		return fmt.Errorf("%w: WiFi SSID length %d exceeds maximum %d",
			ErrSecurityViolation, len(ssid), maxWiFiSSIDLength)
	}
	return nil
}

// validateWiFiNetworkKey validates WiFi network key length
func validateWiFiNetworkKey(key string) error {
	if len(key) > maxWiFiNetworkKeyLength {
		return fmt.Errorf("%w: WiFi network key length %d exceeds maximum %d",
			ErrSecurityViolation, len(key), maxWiFiNetworkKeyLength)
	}
	return nil
}

// validateWiFiMACAddress validates MAC address format and length
func validateWiFiMACAddress(mac string) error {
	if len(mac) > maxWiFiMACAddressLength {
		return fmt.Errorf("%w: WiFi MAC address length %d exceeds maximum %d",
			ErrSecurityViolation, len(mac), maxWiFiMACAddressLength)
	}
	return nil
}

func processWiFiAttribute(cred *WiFiCredential, attrType uint16, value []byte) {
	switch attrType {
	case 0x1026: // Network Index - skip
	case 0x1045: // SSID
		ssid := string(value)
		if err := validateWiFiSSID(ssid); err == nil {
			cred.SSID = ssid
		}
	case 0x1003: // Authentication Type
		if len(value) >= 2 {
			cred.AuthType = binary.BigEndian.Uint16(value)
		}
	case 0x100F: // Encryption Type
		if len(value) >= 2 {
			cred.EncryptionType = binary.BigEndian.Uint16(value)
		}
	case 0x1027: // Network Key
		key := string(value)
		if err := validateWiFiNetworkKey(key); err == nil {
			cred.NetworkKey = key
		}
	case 0x1020: // MAC Address
		if len(value) == 6 {
			mac := formatMACAddress(value)
			if err := validateWiFiMACAddress(mac); err == nil {
				cred.MACAddress = mac
			}
		}
	}
}

// BuildVCardRecord creates an NDEF record with vCard data
func BuildVCardRecord(contact *VCardContact) (*ndef.Record, error) {
	// SECURITY: Validate contact before processing
	if contact == nil {
		return nil, fmt.Errorf("%w: nil vCard contact", ErrSecurityViolation)
	}

	// vCard uses text/vcard MIME type
	vcard := formatVCard(contact)

	rec := ndef.NewMediaRecord(mediaTypeVCard, []byte(vcard))
	// Clear message flags - they will be set by the message builder
	rec.SetMB(false)
	rec.SetME(false)

	return rec, nil
}

// formatVCard formats contact information as vCard
func formatVCard(contact *VCardContact) string {
	var buf strings.Builder

	_, _ = buf.WriteString("BEGIN:VCARD\r\n")

	// Version (default to 3.0 if not specified)
	version := contact.Version
	if version == "" {
		version = "3.0"
	}
	_, _ = buf.WriteString(fmt.Sprintf("VERSION:%s\r\n", version))

	// Name
	if contact.FormattedName != "" {
		_, _ = buf.WriteString(fmt.Sprintf("FN:%s\r\n", contact.FormattedName))
	}
	if contact.FirstName != "" || contact.LastName != "" {
		_, _ = buf.WriteString(fmt.Sprintf("N:%s;%s;;;\r\n", contact.LastName, contact.FirstName))
	}

	// Organization and title
	if contact.Organization != "" {
		_, _ = buf.WriteString(fmt.Sprintf("ORG:%s\r\n", contact.Organization))
	}
	if contact.Title != "" {
		_, _ = buf.WriteString(fmt.Sprintf("TITLE:%s\r\n", contact.Title))
	}

	// Phone numbers
	for phoneType, number := range contact.PhoneNumbers {
		_, _ = buf.WriteString(fmt.Sprintf("TEL;TYPE=%s:%s\r\n", phoneType, number))
	}

	// Email addresses
	for emailType, email := range contact.EmailAddresses {
		_, _ = buf.WriteString(fmt.Sprintf("EMAIL;TYPE=%s:%s\r\n", emailType, email))
	}

	// Addresses
	for addrType, addr := range contact.Addresses {
		_, _ = buf.WriteString(fmt.Sprintf("ADR;TYPE=%s:;;%s;%s;%s;%s;%s\r\n",
			addrType, addr.Street, addr.City, addr.State, addr.PostalCode, addr.Country))
	}

	// URL
	if contact.URL != "" {
		_, _ = buf.WriteString(fmt.Sprintf("URL:%s\r\n", contact.URL))
	}

	// Note
	if contact.Note != "" {
		_, _ = buf.WriteString(fmt.Sprintf("NOTE:%s\r\n", contact.Note))
	}

	_, _ = buf.WriteString("END:VCARD\r\n")

	return buf.String()
}

// ParseVCardRecord parses a vCard from an NDEF record
func ParseVCardRecord(rec *ndef.Record) (*VCardContact, error) {
	// SECURITY: Validate record before processing
	if rec == nil {
		return nil, fmt.Errorf("%w: nil vCard record", ErrSecurityViolation)
	}

	if rec.TNF() != ndef.MediaType ||
		(rec.Type() != mediaTypeVCard && rec.Type() != "text/x-vcard") {
		return nil, errors.New("not a vCard record")
	}

	payload, err := rec.Payload()
	if err != nil {
		return nil, fmt.Errorf("failed to get vCard record payload: %w", err)
	}

	// SECURITY: Validate payload size before processing
	payloadData := payload.Marshal()
	if len(payloadData) > MaxNDEFPayloadSize {
		return nil, fmt.Errorf("%w: vCard payload size %d exceeds maximum %d",
			ErrSecurityViolation, len(payloadData), MaxNDEFPayloadSize)
	}

	return parseVCard(string(payloadData))
}

// parseVCard parses vCard format contact information
func parseVCard(vcard string) (*VCardContact, error) {
	contact := &VCardContact{
		PhoneNumbers:   make(map[string]string),
		EmailAddresses: make(map[string]string),
		Addresses:      make(map[string]Address),
	}

	lines := strings.Split(strings.ReplaceAll(vcard, "\r\n", "\n"), "\n")
	inVCard := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if inVCard = parseVCardControlLine(line, inVCard); !inVCard && line != beginVCard {
			continue
		}

		if line == "END:VCARD" {
			break
		}

		if inVCard && line != beginVCard {
			parseVCardProperty(line, contact)
		}
	}

	return contact, nil
}

func parseVCardControlLine(line string, inVCard bool) bool {
	if line == beginVCard {
		return true
	}
	return inVCard
}

func parseVCardProperty(line string, contact *VCardContact) {
	colonIdx := strings.Index(line, ":")
	if colonIdx == -1 {
		return
	}

	property := line[:colonIdx]
	value := line[colonIdx+1:]

	propName, params := parseVCardPropertyName(property)
	setVCardProperty(contact, propName, value, params)
}

// validateVCardField validates vCard field length
func validateVCardField(field, value string) error {
	if len(value) > maxVCardFieldLength {
		return fmt.Errorf("%w: vCard %s field length %d exceeds maximum %d",
			ErrSecurityViolation, field, len(value), maxVCardFieldLength)
	}
	return nil
}

// validateVCardCollectionCount validates collection sizes
func validateVCardCollectionCount(contact *VCardContact) error {
	if len(contact.PhoneNumbers) > maxVCardPhoneCount {
		return fmt.Errorf("%w: vCard phone count %d exceeds maximum %d",
			ErrSecurityViolation, len(contact.PhoneNumbers), maxVCardPhoneCount)
	}
	if len(contact.EmailAddresses) > maxVCardEmailCount {
		return fmt.Errorf("%w: vCard email count %d exceeds maximum %d",
			ErrSecurityViolation, len(contact.EmailAddresses), maxVCardEmailCount)
	}
	if len(contact.Addresses) > maxVCardAddressCount {
		return fmt.Errorf("%w: vCard address count %d exceeds maximum %d",
			ErrSecurityViolation, len(contact.Addresses), maxVCardAddressCount)
	}
	return nil
}

func parseVCardPropertyName(property string) (propName string, params map[string]string) {
	if semicolonIdx := strings.Index(property, ";"); semicolonIdx != -1 {
		propName = property[:semicolonIdx]
		params = parseVCardParams(property[semicolonIdx+1:])
		return propName, params
	}
	return property, make(map[string]string)
}

func setVCardProperty(contact *VCardContact, propName, value string, params map[string]string) {
	// SECURITY: Validate field before setting
	if err := validateVCardField(propName, value); err != nil {
		return // Skip invalid fields silently to avoid breaking parsing
	}

	switch propName {
	case "VERSION":
		contact.Version = value
	case "FN":
		contact.FormattedName = value
	case "N":
		parseVCardName(contact, value)
	case "ORG":
		contact.Organization = value
	case "TITLE":
		contact.Title = value
	case "TEL":
		// SECURITY: Validate collection size before adding
		if len(contact.PhoneNumbers) < maxVCardPhoneCount {
			parseVCardPhone(contact, value, params)
		}
	case "EMAIL":
		// SECURITY: Validate collection size before adding
		if len(contact.EmailAddresses) < maxVCardEmailCount {
			parseVCardEmail(contact, value, params)
		}
	case "ADR":
		// SECURITY: Validate collection size before adding
		if len(contact.Addresses) < maxVCardAddressCount {
			parseVCardAddress(contact, value, params)
		}
	case "URL":
		contact.URL = value
	case "NOTE":
		contact.Note = value
	}

	// SECURITY: Final validation of collection counts
	_ = validateVCardCollectionCount(contact)
}

func parseVCardName(contact *VCardContact, value string) {
	// SECURITY: Validate name components
	if err := validateVCardField("name", value); err != nil {
		return
	}

	parts := strings.Split(value, ";")
	if len(parts) >= 2 {
		if err := validateVCardField("lastName", parts[0]); err == nil {
			contact.LastName = parts[0]
		}
		if err := validateVCardField("firstName", parts[1]); err == nil {
			contact.FirstName = parts[1]
		}
	}
}

func parseVCardPhone(contact *VCardContact, value string, params map[string]string) {
	// SECURITY: Validate phone number
	if err := validateVCardField("phone", value); err != nil {
		return
	}

	phoneType := params["TYPE"]
	if phoneType == "" {
		phoneType = "VOICE"
	}
	contact.PhoneNumbers[phoneType] = value
}

func parseVCardEmail(contact *VCardContact, value string, params map[string]string) {
	// SECURITY: Validate email address
	if err := validateVCardField("email", value); err != nil {
		return
	}

	emailType := params["TYPE"]
	if emailType == "" {
		emailType = "INTERNET"
	}
	contact.EmailAddresses[emailType] = value
}

func parseVCardAddress(contact *VCardContact, value string, params map[string]string) {
	// SECURITY: Validate address
	if err := validateVCardField("address", value); err != nil {
		return
	}

	addrType := params["TYPE"]
	if addrType == "" {
		addrType = "HOME"
	}

	parts := strings.Split(value, ";")
	if len(parts) < 7 {
		return
	}

	address := Address{
		Street:     validateAndGetField("street", parts[2]),
		City:       validateAndGetField("city", parts[3]),
		State:      validateAndGetField("state", parts[4]),
		PostalCode: validateAndGetField("postalCode", parts[5]),
		Country:    validateAndGetField("country", parts[6]),
	}

	contact.Addresses[addrType] = address
}

func validateAndGetField(fieldType, value string) string {
	if err := validateVCardField(fieldType, value); err == nil {
		return value
	}
	return ""
}

// Helper functions

func parseMACAddress(mac string) ([]byte, error) {
	// Remove common delimiters
	mac = strings.ReplaceAll(mac, ":", "")
	mac = strings.ReplaceAll(mac, "-", "")
	mac = strings.ReplaceAll(mac, ".", "")

	if len(mac) != 12 {
		return nil, errors.New("invalid MAC address length")
	}

	macBytes := make([]byte, 6)
	for i := 0; i < 6; i++ {
		var b byte
		_, err := fmt.Sscanf(mac[i*2:i*2+2], "%02x", &b)
		if err != nil {
			return nil, fmt.Errorf("failed to parse MAC address byte %d: %w", i, err)
		}
		macBytes[i] = b
	}

	return macBytes, nil
}

func formatMACAddress(mac []byte) string {
	if len(mac) != 6 {
		return ""
	}
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X",
		mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
}

func parseVCardParams(params string) map[string]string {
	result := make(map[string]string)
	parts := strings.Split(params, ";")

	for _, part := range parts {
		if eqIdx := strings.Index(part, "="); eqIdx != -1 {
			key := strings.ToUpper(part[:eqIdx])
			value := part[eqIdx+1:]
			result[key] = value
		}
	}

	return result
}
