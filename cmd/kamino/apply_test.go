package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/plan"
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

// TestApplyRealPathSurfacesSafetyWarnings pins Finding 1: the headless apply
// path (no --dry-run) computes the same operator-facing safety warnings
// `validate` and `plan` show, but used to throw them away — a real
// `sudo kamino apply --yes` installed unverified downloads and root-executed
// scripts without printing a word about it. This intentionally omits --yes,
// so the run stops at the confirmation gate (or, on a non-Ubuntu/non-root
// test runner, at the earlier host check) and never reaches execute(); the
// warnings must already be in the output by the time either gate fires.
func TestApplyRealPathSurfacesSafetyWarnings(t *testing.T) {
	out, err := runCmd(t, "apply", "--config-dir", fixtureDir(), "--profile", "production",
		"--arch", "amd64", "--secret", "CF_TUNNEL_TOKEN=abc123")

	require.Error(t, err, "must not proceed without --yes")
	assert.Contains(t, out, "warning:",
		"the real apply path must surface plan safety warnings before executing, matching validate/plan")
}

// TestPrintApplyWarningsShowsStaleAndPlanWarnings covers printApplyWarnings
// directly (Findings 1 and 2): a stale cached config and any per-item plan
// warning (missing sha256, a root-executed script) must both render, using
// the same wording validate.go and plan.go already use.
func TestPrintApplyWarningsShowsStaleAndPlanWarnings(t *testing.T) {
	var buf bytes.Buffer
	resolved := &manifest.Resolved{
		SHA:       "abc123",
		Stale:     true,
		FetchedAt: time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC),
	}
	built := &plan.Plan{
		Warnings: []string{"tools/docker: runs a shell script as root from https://get.docker.com"},
	}

	printApplyWarnings(&buf, resolved, built)

	out := buf.String()
	assert.Contains(t, out, "warning: stale config (SHA abc123, fetched 2026-07-20T12:00Z)")
	assert.Contains(t, out, "warning: tools/docker: runs a shell script as root from https://get.docker.com")
}
