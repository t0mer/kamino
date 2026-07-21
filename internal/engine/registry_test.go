package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/download"
	"github.com/t0mer/kamino/internal/engine"
	"github.com/t0mer/kamino/internal/engine/runners"
	kexec "github.com/t0mer/kamino/internal/exec"
	"github.com/t0mer/kamino/internal/manifest"
)

type fakeScriptSource struct{}

func (fakeScriptSource) Fetch(context.Context, string, string) ([]byte, error) {
	return nil, nil
}

func TestNewRegistryResolvesEveryImplementedType(t *testing.T) {
	deps := runners.Deps{
		Exec:     kexec.NewFakeExecutor(),
		Download: download.NewFakeDownloader(),
		TempDir:  t.TempDir(),
	}
	lookup := engine.NewRegistry(deps, fakeScriptSource{}, "main")

	for _, typ := range []manifest.ItemType{
		manifest.ItemApt, manifest.ItemDeb, manifest.ItemTarball,
		manifest.ItemBinary, manifest.ItemPip, manifest.ItemScript,
	} {
		runner, ok := lookup(typ)
		require.True(t, ok, "expected a runner for %q", typ)
		assert.NotNil(t, runner)
	}
}

func TestNewRegistryHasNoRunnerForComposeStackOrSnap(t *testing.T) {
	deps := runners.Deps{
		Exec:     kexec.NewFakeExecutor(),
		Download: download.NewFakeDownloader(),
		TempDir:  t.TempDir(),
	}
	lookup := engine.NewRegistry(deps, fakeScriptSource{}, "main")

	for _, typ := range []manifest.ItemType{manifest.ItemComposeStack, manifest.ItemSnap} {
		_, ok := lookup(typ)
		assert.False(t, ok, "expected no runner registered for %q", typ)
	}
}
