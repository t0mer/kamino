package remote_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/remote"
)

// dirFetcher serves the testdata config tree, standing in for a real repo.
type dirFetcher struct {
	root    string
	fetched []string
}

func (d *dirFetcher) Fetch(_ context.Context, _ string, path string) ([]byte, error) {
	d.fetched = append(d.fetched, path)
	b, err := os.ReadFile(filepath.Join(d.root, filepath.FromSlash(path)))
	if os.IsNotExist(err) {
		return nil, remote.ErrNotFound
	}
	return b, err
}

func TestLoadResolvesWholeRepo(t *testing.T) {
	f := &dirFetcher{root: filepath.Join("..", "..", "testdata", "config")}

	got, err := remote.Load(context.Background(), f, "abc123")

	require.NoError(t, err)
	assert.Equal(t, "kamino-testdata", got.Manifest.Name)
	assert.Equal(t, "abc123", got.SHA)
	assert.Len(t, got.Categories, 3)
	assert.Len(t, got.Profiles, 3)
	assert.False(t, got.FetchedAt.IsZero())
}

func TestLoadFetchesManifestFirst(t *testing.T) {
	f := &dirFetcher{root: filepath.Join("..", "..", "testdata", "config")}

	_, err := remote.Load(context.Background(), f, "abc123")

	require.NoError(t, err)
	assert.Equal(t, "manifest.yaml", f.fetched[0])
}

func TestLoadDoesNotFetchScripts(t *testing.T) {
	f := &dirFetcher{root: filepath.Join("..", "..", "testdata", "config")}

	_, err := remote.Load(context.Background(), f, "abc123")

	require.NoError(t, err)
	for _, p := range f.fetched {
		assert.NotContains(t, p, "scripts/", "scripts must be fetched lazily, at step time")
	}
}

// TestLoadNamesManifestOnParseError asserts the manifest.yaml parse error is
// wrapped with the file name, just like category and profile parse errors
// already are a few lines below in load.go — an operator debugging a broken
// config repo needs to know which file is broken without guessing.
func TestLoadNamesManifestOnParseError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(`not: [valid: yaml`), 0o644))

	_, err := remote.Load(context.Background(), &dirFetcher{root: dir}, "abc123")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest.yaml")
}

func TestLoadErrorsOnMissingCategory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(`
schema: 1
name: broken
categories:
  - categories/nope.yaml
`), 0o644))

	_, err := remote.Load(context.Background(), &dirFetcher{root: dir}, "abc123")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "categories/nope.yaml")
}
