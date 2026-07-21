package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyRefusesUnknownProfile(t *testing.T) {
	_, err := runCmd(t, "apply", "--config-dir", fixtureDir(), "--profile", "nope",
		"--dry-run", "--yes")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope")
}

func TestApplyDryRunPrintsPlanWithoutInstalling(t *testing.T) {
	out, err := runCmd(t, "apply", "--config-dir", fixtureDir(), "--profile", "test",
		"--arch", "amd64", "--dry-run", "--yes")

	require.NoError(t, err)
	assert.Contains(t, out, "tools/jq")
	assert.Contains(t, out, "dry run")
}

func TestApplyReportsMissingSecrets(t *testing.T) {
	_, err := runCmd(t, "apply", "--config-dir", fixtureDir(), "--profile", "production",
		"--arch", "amd64", "--dry-run", "--yes")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "CF_TUNNEL_TOKEN",
		"a run must not start when a declared secret has no value")
}

func TestApplyAcceptsSecretFlag(t *testing.T) {
	out, err := runCmd(t, "apply", "--config-dir", fixtureDir(), "--profile", "production",
		"--arch", "amd64", "--dry-run", "--yes", "--secret", "CF_TUNNEL_TOKEN=abc123")

	require.NoError(t, err)
	assert.NotContains(t, out, "abc123", "a secret value must never be echoed")
}

func TestApplyRejectsMalformedSecretFlag(t *testing.T) {
	_, err := runCmd(t, "apply", "--config-dir", fixtureDir(), "--profile", "test",
		"--dry-run", "--yes", "--secret", "NOEQUALS")

	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "key=value")
}
