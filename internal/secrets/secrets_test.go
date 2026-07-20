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
	cases := []struct {
		name      string
		shortName string
		shortVal  string
		longName  string
		longVal   string
		line      string
	}{
		{
			name:      "short secret is a prefix of the long one",
			shortName: "AAA_PREFIX_SHORT",
			shortVal:  "abc",
			longName:  "ZZZ_PREFIX_LONG",
			longVal:   "abc123",
			line:      "value abc123 here",
		},
		{
			name:      "short secret is a suffix of the long one",
			shortName: "AAA_SUFFIX_SHORT",
			shortVal:  "xyz",
			longName:  "ZZZ_SUFFIX_LONG",
			longVal:   "789xyz",
			line:      "value 789xyz here",
		},
		{
			name:      "short secret is an infix of the long one",
			shortName: "AAA_INFIX_SHORT",
			shortVal:  "mid",
			longName:  "ZZZ_INFIX_LONG",
			longVal:   "prefmidsuf",
			line:      "value prefmidsuf here",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := secrets.New()
			// Names are chosen so alphabetical order (AAA_... < ZZZ_...) is
			// the OPPOSITE of the correct redaction order (longest value
			// first). If NewRedactor ever stopped sorting by length and fell
			// back to Store.Names()'s alphabetical order, the short secret
			// would be masked first, leaving a fragment of the long secret
			// visible in the output — exactly what this test must catch.
			s.Set(tc.shortName, tc.shortVal)
			s.Set(tc.longName, tc.longVal)

			got := secrets.NewRedactor(s).Redact(tc.line)

			assert.Equal(t, "value *** here", got,
				"the longer secret must be masked first, or a fragment of it would leak")
		})
	}
}

func TestRedactorIgnoresEmptySecrets(t *testing.T) {
	s := secrets.New()
	s.Set("EMPTY", "")

	got := secrets.NewRedactor(s).Redact("unchanged line")

	assert.Equal(t, "unchanged line", got)
}
