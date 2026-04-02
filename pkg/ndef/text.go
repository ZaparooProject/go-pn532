// Copyright 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ndef

import (
	"errors"
	"fmt"
)

// Text record constants.
const (
	TextRecordType    = "T"
	textUTF16Flag     = 0x80
	textLangCodeMask  = 0x3F
	maxLanguageLength = 63 // 6 bits max
)

// Text record errors.
var (
	ErrTextPayloadTooShort  = errors.New("ndef: text payload too short")
	ErrTextInvalidLangLen   = errors.New("ndef: invalid language code length")
	ErrTextLanguageTooLong  = errors.New("ndef: language code too long")
	ErrTextPayloadTruncated = errors.New("ndef: text payload truncated")
)

// TextRecord represents parsed text record data.
type TextRecord struct {
	Text     string
	Language string
	UTF16    bool // true if UTF-16 encoded (rare)
}

// NewTextRecord creates a new NDEF Text record.
// The language parameter should be an IANA language code (e.g., "en", "en-US").
func NewTextRecord(text, language string) *Record {
	if language == "" {
		language = "en"
	}

	// Truncate language if too long
	if len(language) > maxLanguageLength {
		language = language[:maxLanguageLength]
	}

	// Build payload: status byte + language code + text
	payload := make([]byte, 1+len(language)+len(text))
	payload[0] = byte(len(language)) //nolint:gosec // capped at maxLanguageLength (63)
	copy(payload[1:], language)
	copy(payload[1+len(language):], text)

	return &Record{
		TNF:     TNFWellKnown,
		Type:    TextRecordType,
		Payload: payload,
	}
}

// ParseTextRecord extracts text content from a Text record payload.
func ParseTextRecord(payload []byte) (*TextRecord, error) {
	if len(payload) < 1 {
		return nil, ErrTextPayloadTooShort
	}

	status := payload[0]
	langLen := int(status & textLangCodeMask)
	isUTF16 := (status & textUTF16Flag) != 0

	if langLen > maxLanguageLength {
		return nil, ErrTextInvalidLangLen
	}

	if len(payload) < 1+langLen {
		return nil, ErrTextPayloadTruncated
	}

	language := string(payload[1 : 1+langLen])
	text := string(payload[1+langLen:])

	return &TextRecord{
		Text:     text,
		Language: language,
		UTF16:    isUTF16,
	}, nil
}

// DecodeTextPayload is a convenience function that extracts just the text string.
func DecodeTextPayload(payload []byte) (string, error) {
	rec, err := ParseTextRecord(payload)
	if err != nil {
		return "", err
	}
	return rec.Text, nil
}

// EncodeTextPayload creates a text record payload from text and language.
func EncodeTextPayload(text, language string) ([]byte, error) {
	if len(language) > maxLanguageLength {
		return nil, fmt.Errorf("%w: %d bytes", ErrTextLanguageTooLong, len(language))
	}

	if language == "" {
		language = "en"
	}

	payload := make([]byte, 1+len(language)+len(text))
	payload[0] = byte(len(language)) //nolint:gosec // capped above
	copy(payload[1:], language)
	copy(payload[1+len(language):], text)

	return payload, nil
}
