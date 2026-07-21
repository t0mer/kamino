package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer

	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()

	return out.String(), err
}

func fixtureDir() string { return filepath.Join("..", "..", "testdata", "config") }

func TestValidateAcceptsCleanFixtures(t *testing.T) {
	out, err := runCmd(t, "validate", "--config-dir", fixtureDir())

	require.NoError(t, err)
	assert.Contains(t, out, "kamino-testdata")
	assert.Contains(t, out, "3 categories")
}

func TestValidateReportsWarnings(t *testing.T) {
	out, err := runCmd(t, "validate", "--config-dir", fixtureDir())

	require.NoError(t, err)
	assert.Contains(t, out, "warning")
	assert.Contains(t, out, "sha256")
}

func TestValidateFailsOnBrokenConfig(t *testing.T) {
	_, err := runCmd(t, "validate", "--config-dir", filepath.Join("..", "..", "testdata", "broken"))

	require.Error(t, err)
}

func TestPlanTextOutputIsOrdered(t *testing.T) {
	out, err := runCmd(t, "plan", "--config-dir", fixtureDir(), "--profile", "dev", "--arch", "amd64")

	require.NoError(t, err)
	goAt := strings.Index(out, "dev/go")
	pypiAt := strings.Index(out, "dev/pypi-base")
	require.NotEqual(t, -1, goAt)
	require.NotEqual(t, -1, pypiAt)
	assert.Less(t, goAt, pypiAt)
}

func TestPlanJSONOutput(t *testing.T) {
	out, err := runCmd(t, "plan", "--config-dir", fixtureDir(), "--profile", "dev", "--arch", "amd64", "--json")

	require.NoError(t, err)

	var got struct {
		Profile string `json:"profile"`
		Arch    string `json:"arch"`
		Steps   []struct {
			Ref      string `json:"ref"`
			Implicit bool   `json:"implicit"`
		} `json:"steps"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got))

	assert.Equal(t, "dev", got.Profile)
	assert.Equal(t, "amd64", got.Arch)
	require.Len(t, got.Steps, 4)
	assert.Equal(t, "dev/go", got.Steps[0].Ref)
}

func TestPlanUnknownProfileFails(t *testing.T) {
	_, err := runCmd(t, "plan", "--config-dir", fixtureDir(), "--profile", "nope")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope")
}

func TestPlanMarksImplicitStepsInText(t *testing.T) {
	// testdata/config's profiles (dev, production, test) never actually
	// exercise implicit-dependency marking: every dependency they need is
	// already covered by an explicit include/glob, so plan.Select never adds
	// one on their behalf (see internal/plan/resolve.go). testdata/implicit
	// is a small dedicated fixture — profile "compose-only" includes just
	// tools/docker-compose, which depends on tools/docker without including
	// it directly, so tools/docker is pulled in and marked implicit.
	out, err := runCmd(t, "plan", "--config-dir", filepath.Join("..", "..", "testdata", "implicit"), "--profile", "compose-only", "--arch", "amd64")

	require.NoError(t, err)
	assert.Contains(t, out, "implicit")
}
