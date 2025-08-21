package pn532

// IsNDEFFormatted checks if the tag is NDEF formatted with the default NDEF KeyA
func (t *MIFARETag) IsNDEFFormatted() bool {
	ndefKeyBytes := t.ndefKey.bytes()
	defer clear(ndefKeyBytes)

	return t.Authenticate(1, MIFAREKeyA, ndefKeyBytes) == nil
}
