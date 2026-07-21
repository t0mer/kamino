package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validCategoryYAML is a minimal, schema-valid category file. Its presence
// in test output (via the "1 categories" success message) is a strong signal
// that the fetcher actually read the underlying file, which is exactly what
// the escape and absolute-path tests must prove did NOT happen.
const validCategoryYAML = `
id: exfil
name: Exfiltrated
order: 1
items: []
`

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func writeMinimalManifest(t *testing.T, dir, categoryPath string) {
	t.Helper()
	manifest := "schema: 1\n" +
		"name: poc\n" +
		"defaults:\n" +
		"  apt_update_before_run: true\n" +
		"categories:\n" +
		"  - \"" + categoryPath + "\"\n" +
		"profiles: []\n"
	writeFile(t, filepath.Join(dir, "manifest.yaml"), manifest)
}

// TestValidateRejectsCategoryPathEscape proves that a manifest.yaml whose
// categories list points outside the configured --config-dir root (via a
// "../" path) is rejected outright, and that the escaping file's content is
// never parsed. This is the exact proof-of-concept reported by the reviewer:
// "categories: [\"../secret.yaml\"]" must not let a config repo read files
// outside its own root.
func TestValidateRejectsCategoryPathEscape(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "cfg")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))

	// A file that sits one directory above the configured root.
	writeFile(t, filepath.Join(base, "secret.yaml"), validCategoryYAML)
	writeMinimalManifest(t, cfgDir, "../secret.yaml")

	out, err := runCmd(t, "validate", "--config-dir", cfgDir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "../secret.yaml")
	assert.Contains(t, err.Error(), "escapes")
	// If the file HAD been read, validate would have succeeded and printed
	// a "1 categories ... OK" summary instead of failing.
	assert.NotContains(t, out, "OK")
	assert.NotContains(t, out, "poc:")
}

// TestValidateRejectsAbsoluteCategoryPath proves that an absolute path in
// the categories list is rejected too, even when the absolute path happens
// to point at a valid, readable file. Absolute paths bypass the configured
// root entirely, so they must never be honoured regardless of what they
// point to.
func TestValidateRejectsAbsoluteCategoryPath(t *testing.T) {
	base := t.TempDir()
	cfgDir := filepath.Join(base, "cfg")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))

	absTarget := filepath.Join(base, "restricted.yaml")
	writeFile(t, absTarget, validCategoryYAML)
	writeMinimalManifest(t, cfgDir, absTarget)

	out, err := runCmd(t, "validate", "--config-dir", cfgDir)

	require.Error(t, err)
	assert.Contains(t, err.Error(), absTarget)
	assert.Contains(t, err.Error(), "is absolute")
	assert.NotContains(t, out, "OK")
}

// TestValidateAcceptsNormalNestedCategoryPath is the over-rejection guard:
// an ordinary category path nested under the config dir root (the shape
// every real config repo uses) must keep loading successfully after the
// containment check was added.
func TestValidateAcceptsNormalNestedCategoryPath(t *testing.T) {
	cfgDir := t.TempDir()
	writeFile(t, filepath.Join(cfgDir, "categories", "dev.yaml"), validCategoryYAML)
	writeMinimalManifest(t, cfgDir, "categories/dev.yaml")

	out, err := runCmd(t, "validate", "--config-dir", cfgDir)

	require.NoError(t, err)
	assert.Contains(t, out, "poc:")
	assert.Contains(t, out, "1 categories")
	assert.Contains(t, out, "OK")
}
