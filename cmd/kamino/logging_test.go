package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogHandlerJSONProducesParseableLines(t *testing.T) {
	var buf bytes.Buffer
	h, err := newLogHandler(&buf, "json", slog.LevelInfo)
	require.NoError(t, err)

	slog.New(h).Info("provisioning started", "profile", "test")

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded), "json format must emit one parseable JSON object per line")
	assert.Equal(t, "provisioning started", decoded["msg"])
	assert.Equal(t, "test", decoded["profile"])
}

func TestNewLogHandlerTextProducesKeyValueLines(t *testing.T) {
	var buf bytes.Buffer
	h, err := newLogHandler(&buf, "text", slog.LevelInfo)
	require.NoError(t, err)

	slog.New(h).Info("provisioning started", "profile", "test")

	out := buf.String()
	assert.Contains(t, out, "msg=\"provisioning started\"")
	assert.Contains(t, out, "profile=test")
	// A text line must not itself be valid JSON, or a caller sniffing the
	// format could confuse the two.
	assert.False(t, json.Valid(bytes.TrimSpace(buf.Bytes())))
}

func TestNewLogHandlerAutoIsJSONWhenNotATerminal(t *testing.T) {
	var buf bytes.Buffer

	h, err := newLogHandler(&buf, "auto", slog.LevelInfo)
	require.NoError(t, err)

	slog.New(h).Info("hello")

	require.True(t, json.Valid(bytes.TrimSpace(buf.Bytes())),
		"a non-terminal writer (e.g. redirected to a file, as under cloud-init/systemd) must default to json")
}

func TestNewLogHandlerDefaultFormatBehavesLikeAuto(t *testing.T) {
	var buf bytes.Buffer

	h, err := newLogHandler(&buf, "", slog.LevelInfo)
	require.NoError(t, err)

	slog.New(h).Info("hello")

	assert.True(t, json.Valid(bytes.TrimSpace(buf.Bytes())))
}

func TestNewLogHandlerRejectsUnknownFormat(t *testing.T) {
	_, err := newLogHandler(&bytes.Buffer{}, "yaml", slog.LevelInfo)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "yaml")
}

func TestNewLogHandlerHonoursLevel(t *testing.T) {
	var buf bytes.Buffer
	h, err := newLogHandler(&buf, "json", slog.LevelWarn)
	require.NoError(t, err)

	logger := slog.New(h)
	logger.Info("dropped: below the configured level")
	logger.Warn("kept: at the configured level")

	out := buf.String()
	assert.NotContains(t, out, "dropped")
	assert.Contains(t, out, "kept")
}

func TestIsTerminalIsFalseForNonFileWriters(t *testing.T) {
	assert.False(t, isTerminal(&bytes.Buffer{}))
	assert.False(t, isTerminal(io.Discard))
}

func TestSetupLoggingRejectsInvalidLevel(t *testing.T) {
	err := setupLogging("verbose", "text")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "verbose")
}

func TestSetupLoggingRejectsInvalidFormat(t *testing.T) {
	err := setupLogging("info", "csv")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "csv")
}

func TestSetupLoggingAcceptsEveryDocumentedFormat(t *testing.T) {
	for _, format := range []string{"json", "text", "auto"} {
		require.NoError(t, setupLogging("debug", format), "format %q must be accepted", format)
	}
}

// TestLogFormatFlagDoesNotLeakIntoCommandOutput pins that --log-format only
// ever affects slog diagnostics, never the human-facing plan/progress text a
// command writes to cmd.OutOrStdout: `apply --dry-run`'s plan render must
// stay identical prose regardless of --log-format, so scripts parsing that
// output (or a human reading it) never see it flip to JSON.
func TestLogFormatFlagDoesNotLeakIntoCommandOutput(t *testing.T) {
	out, err := runCmd(t, "apply", "--config-dir", fixtureDir(), "--profile", "test",
		"--arch", "amd64", "--dry-run", "--yes", "--log-format", "json", "--log-level", "debug")

	require.NoError(t, err)
	assert.Contains(t, out, "dry run: nothing was installed")
	assert.False(t, json.Valid(bytes.TrimSpace([]byte(out))),
		"command output must remain human-readable text, never JSON, however --log-format is set")
}
