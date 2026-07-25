//nolint:paralleltest // Tests modify package-level debug state, cannot run in parallel
package pn532

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveDebugState saves the current debug state for restoration.
func saveDebugState() (enabled bool, writer any) {
	return debugEnabled, sessionLogWriter
}

// restoreDebugState restores saved debug state.
func restoreDebugState(enabled bool, writer any) {
	debugEnabled = enabled
	if writer == nil {
		sessionLogWriter = nil
	} else if buf, ok := writer.(*bytes.Buffer); ok {
		sessionLogWriter = buf
	}
}

func TestDebugf_WritesToSessionLog(t *testing.T) {
	origEnabled, origWriter := saveDebugState()
	t.Cleanup(func() {
		restoreDebugState(origEnabled, origWriter)
	})

	// Set up a buffer as the session log writer
	var buf bytes.Buffer
	sessionLogWriter = &buf
	debugEnabled = false // Disable console output

	Debugf("test message %d", 42)

	content := buf.String()
	assert.Contains(t, content, "DEBUG: test message 42")
	assert.Contains(t, content, "\n") // Should have newline
}

func TestDebugf_IncludesTimestamp(t *testing.T) {
	origEnabled, origWriter := saveDebugState()
	t.Cleanup(func() {
		restoreDebugState(origEnabled, origWriter)
	})

	var buf bytes.Buffer
	sessionLogWriter = &buf
	debugEnabled = false

	Debugf("test message")

	content := buf.String()

	// Verify timestamp format: HH:MM:SS.mmm
	matched, err := regexp.MatchString(`\d{2}:\d{2}:\d{2}\.\d{3} DEBUG:`, content)
	require.NoError(t, err)
	assert.True(t, matched, "Should include timestamp in format HH:MM:SS.mmm, got: %s", content)
}

func TestDebugf_NilSessionWriter(t *testing.T) {
	origEnabled, origWriter := saveDebugState()
	t.Cleanup(func() {
		restoreDebugState(origEnabled, origWriter)
	})

	sessionLogWriter = nil
	debugEnabled = false

	// Should not panic when sessionLogWriter is nil
	Debugf("test message %d", 42)
}

func TestDebugln_WritesToSessionLog(t *testing.T) {
	origEnabled, origWriter := saveDebugState()
	t.Cleanup(func() {
		restoreDebugState(origEnabled, origWriter)
	})

	var buf bytes.Buffer
	sessionLogWriter = &buf
	debugEnabled = false

	Debugln("test message")

	content := buf.String()
	assert.Contains(t, content, "DEBUG: test message")
}

func TestDebugln_IncludesTimestamp(t *testing.T) {
	origEnabled, origWriter := saveDebugState()
	t.Cleanup(func() {
		restoreDebugState(origEnabled, origWriter)
	})

	var buf bytes.Buffer
	sessionLogWriter = &buf
	debugEnabled = false

	Debugln("test message")

	content := buf.String()

	// Verify timestamp format: HH:MM:SS.mmm
	matched, err := regexp.MatchString(`\d{2}:\d{2}:\d{2}\.\d{3} DEBUG:`, content)
	require.NoError(t, err)
	assert.True(t, matched, "Should include timestamp in format HH:MM:SS.mmm, got: %s", content)
}

func TestDebugln_NilSessionWriter(t *testing.T) {
	origEnabled, origWriter := saveDebugState()
	t.Cleanup(func() {
		restoreDebugState(origEnabled, origWriter)
	})

	sessionLogWriter = nil
	debugEnabled = false

	// Should not panic when sessionLogWriter is nil
	Debugln("test", "message")
}

func TestSetDebugEnabled(t *testing.T) {
	origEnabled, origWriter := saveDebugState()
	t.Cleanup(func() {
		restoreDebugState(origEnabled, origWriter)
	})

	// Test enabling debug
	SetDebugEnabled(true)
	assert.True(t, debugEnabled)

	// Test disabling debug
	SetDebugEnabled(false)
	assert.False(t, debugEnabled)

	// Test toggling
	SetDebugEnabled(true)
	assert.True(t, debugEnabled)
}

func TestSetDebugWriter(t *testing.T) {
	origEnabled, origWriter := saveDebugState()
	t.Cleanup(func() {
		SetDebugWriter(nil)
		restoreDebugState(origEnabled, origWriter)
	})

	var sessionBuf bytes.Buffer
	var externalBuf bytes.Buffer
	sessionLogWriter = &sessionBuf
	debugEnabled = false
	SetDebugWriter(&externalBuf)

	Debugf("formatted %d", 42)
	Debugln("plain message")

	assert.Contains(t, sessionBuf.String(), "DEBUG: formatted 42")
	assert.Contains(t, sessionBuf.String(), "DEBUG: plain message")
	assert.Contains(t, externalBuf.String(), "DEBUG: formatted 42")
	assert.Contains(t, externalBuf.String(), "DEBUG: plain message")

	beforeDisable := externalBuf.String()
	SetDebugWriter(nil)
	Debugf("session only")
	assert.Equal(t, beforeDisable, externalBuf.String())
	assert.Contains(t, sessionBuf.String(), "DEBUG: session only")
}

func TestDebugf_MultipleMessages(t *testing.T) {
	origEnabled, origWriter := saveDebugState()
	t.Cleanup(func() {
		restoreDebugState(origEnabled, origWriter)
	})

	var buf bytes.Buffer
	sessionLogWriter = &buf
	debugEnabled = false

	Debugf("message 1")
	Debugf("message 2")
	Debugf("message 3")

	content := buf.String()
	lines := strings.Split(strings.TrimSpace(content), "\n")
	assert.Len(t, lines, 3, "Should have 3 log lines")

	assert.Contains(t, lines[0], "message 1")
	assert.Contains(t, lines[1], "message 2")
	assert.Contains(t, lines[2], "message 3")
}

func TestDebugf_FormatSpecifiers(t *testing.T) {
	origEnabled, origWriter := saveDebugState()
	t.Cleanup(func() {
		restoreDebugState(origEnabled, origWriter)
	})

	var buf bytes.Buffer
	sessionLogWriter = &buf
	debugEnabled = false

	Debugf("int: %d, string: %s, hex: %02X", 42, "test", 0xAB)

	content := buf.String()
	assert.Contains(t, content, "int: 42")
	assert.Contains(t, content, "string: test")
	assert.Contains(t, content, "hex: AB")
}

func TestDebugln_MultipleArgs(t *testing.T) {
	origEnabled, origWriter := saveDebugState()
	t.Cleanup(func() {
		restoreDebugState(origEnabled, origWriter)
	})

	var buf bytes.Buffer
	sessionLogWriter = &buf
	debugEnabled = false

	Debugln("value1", 42, "value2", true)

	content := buf.String()
	// fmt.Sprint concatenates without spaces
	assert.Contains(t, content, "value142value2true")
}
