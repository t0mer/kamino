package secrets_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/secrets"
)

func TestExpandVersion(t *testing.T) {
	got, err := secrets.Expand("https://go.dev/dl/go{version}.linux-amd64.tar.gz", "1.24.5", secrets.New())

	require.NoError(t, err)
	assert.Equal(t, "https://go.dev/dl/go1.24.5.linux-amd64.tar.gz", got)
}

func TestExpandVersionMultipleTimes(t *testing.T) {
	got, err := secrets.Expand("go{version} and go{version}", "1.24.5", secrets.New())

	require.NoError(t, err)
	assert.Equal(t, "go1.24.5 and go1.24.5", got)
}

func TestExpandSecret(t *testing.T) {
	s := secrets.New()
	s.Set("CF_TUNNEL_TOKEN", "abc123")

	got, err := secrets.Expand("cloudflared service install {secret:CF_TUNNEL_TOKEN}", "", s)

	require.NoError(t, err)
	assert.Equal(t, "cloudflared service install abc123", got)
}

func TestExpandUnknownSecretIsAnError(t *testing.T) {
	_, err := secrets.Expand("install {secret:MISSING}", "", secrets.New())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "MISSING")
}

func TestExpandLeavesUnrelatedBracesAlone(t *testing.T) {
	got, err := secrets.Expand("echo {not-a-placeholder}", "1.0", secrets.New())

	require.NoError(t, err)
	assert.Equal(t, "echo {not-a-placeholder}", got)
}

func TestMissingReportsUnsetSecrets(t *testing.T) {
	s := secrets.New()
	s.Set("PRESENT", "x")

	got := secrets.Missing([]string{"PRESENT", "ABSENT", "ALSO_ABSENT"}, s)

	assert.Equal(t, []string{"ABSENT", "ALSO_ABSENT"}, got)
}

func TestRedactorMasksSecretValues(t *testing.T) {
	s := secrets.New()
	s.Set("TOKEN", "sup3rs3cret")

	got := secrets.NewRedactor(s).Redact("installing with token sup3rs3cret now")

	assert.Equal(t, "installing with token *** now", got)
	assert.NotContains(t, got, "sup3rs3cret")
}

func TestRedactorMasksEveryOccurrence(t *testing.T) {
	s := secrets.New()
	s.Set("TOKEN", "abc")

	got := secrets.NewRedactor(s).Redact("abc and abc and abc")

	assert.Equal(t, "*** and *** and ***", got)
}

func TestRedactorHandlesOverlappingSecrets(t *testing.T) {
	s := secrets.New()
	s.Set("SHORT", "abc")
	s.Set("LONG", "abc123")

	got := secrets.NewRedactor(s).Redact("value abc123 here")

	assert.Equal(t, "value *** here", got,
		"the longer secret must be masked first, or its tail would leak")
}

func TestRedactorIgnoresEmptySecrets(t *testing.T) {
	s := secrets.New()
	s.Set("EMPTY", "")

	got := secrets.NewRedactor(s).Redact("unchanged line")

	assert.Equal(t, "unchanged line", got)
}
