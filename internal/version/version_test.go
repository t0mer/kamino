package version_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/t0mer/kamino/internal/version"
)

func TestStringUsesInjectedVersion(t *testing.T) {
	original := version.Version
	t.Cleanup(func() { version.Version = original })

	version.Version = "2026.7.0"
	assert.Equal(t, "kamino 2026.7.0", version.String())
}

func TestStringFallsBackToDev(t *testing.T) {
	original := version.Version
	t.Cleanup(func() { version.Version = original })

	version.Version = ""
	assert.Equal(t, "kamino dev", version.String())
}
