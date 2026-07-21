package runners

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// DefaultInstallDir is where tarballs are extracted when the item does not
// declare an install_dir.
const DefaultInstallDir = "/usr/local"

// Tarball installs a .tar.gz archive by extracting it into an install dir.
type Tarball struct{ d Deps }

// NewTarball builds a tarball runner.
func NewTarball(d Deps) *Tarball { return &Tarball{d: d} }

// Check probes whether the tool is already at the wanted version.
func (t *Tarball) Check(ctx context.Context, it ResolvedItem) (bool, error) {
	return CheckProbe(ctx, t.d, it)
}

// Install downloads, verifies and extracts the archive.
//
// The previous install at <install_dir>/<name> is removed before
// extraction: `tar -xzf` merges into whatever already exists at the
// destination, so upgrading (say) Go 1.23 -> 1.24 without clearing the
// target first would leave stale files from the old version mixed in with
// the new.
func (t *Tarball) Install(ctx context.Context, it ResolvedItem) error {
	if it.Source == "" {
		return fmt.Errorf("%s: tarball item has no source for this architecture", it.Ref)
	}

	installDir := it.InstallDir
	if installDir == "" {
		installDir = DefaultInstallDir
	}

	name := itemName(it)
	target, err := safeInstallTarget(installDir, name)
	if err != nil {
		return fmt.Errorf("%s: %w", it.Ref, err)
	}

	archive := filepath.Join(t.d.TempDir, name+".tar.gz")
	if err := t.d.Download.Fetch(ctx, it.Source, archive, it.SHA256); err != nil {
		return fmt.Errorf("%s: %w", it.Ref, err)
	}

	if err := run(ctx, t.d, it, fmt.Sprintf("rm -rf %s", target)); err != nil {
		return err
	}
	if err := run(ctx, t.d, it, fmt.Sprintf("mkdir -p %s", installDir)); err != nil {
		return err
	}
	if err := run(ctx, t.d, it, fmt.Sprintf("tar -C %s -xzf %s", installDir, archive)); err != nil {
		return err
	}

	if it.PathExport != "" {
		profile := fmt.Sprintf("/etc/profile.d/kamino-%s.sh", name)
		line := fmt.Sprintf("printf '%%s\\n' 'export PATH=$PATH:%s' > %s && chmod 0644 %s",
			it.PathExport, profile, profile)
		if err := run(ctx, t.d, it, line); err != nil {
			return err
		}
	}
	return nil
}

// itemName is the item's own id — the part of Ref after the category slash
// — used to name install dirs, temp archives and profile.d scripts. A Ref
// with no slash yields the whole Ref unchanged; an empty Ref (or one ending
// in "/") yields "". Callers that use the result to build a path passed to
// `rm -rf` must treat that empty case as invalid — see safeInstallTarget,
// which every caller here routes through.
func itemName(it ResolvedItem) string {
	_, name, found := strings.Cut(it.Ref, "/")
	if !found {
		return it.Ref
	}
	return name
}

// safeInstallTarget builds the <installDir>/<name> path that Install is
// about to `rm -rf` as root, and rejects anything that could make that
// removal catastrophic.
//
// The config repo is trusted by design in this project's threat model (see
// CLAUDE.md §7) — an operator who points Kamino at a malicious repo has
// already lost. But a plain typo (a blank install_dir, install_dir left at
// "/", or an item id that is empty or contains a path separator) is a
// different class of mistake, and this guard is cheap insurance against
// exactly that: it never has to reason about anything an attacker crafted,
// only about a handful of always-invalid shapes.
func safeInstallTarget(installDir, name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("refusing to build an install path from item id %q", name)
	}

	clean := filepath.Clean(installDir)
	if clean == "" || clean == "." || clean == "/" {
		return "", fmt.Errorf("refusing to use %q as an install dir: too broad", installDir)
	}

	target := filepath.Join(clean, name)
	if target == "" || target == "/" {
		return "", fmt.Errorf("refusing to remove %q: resolves to the filesystem root", target)
	}

	rel, err := filepath.Rel(clean, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("refusing to remove %q: escapes install dir %q", target, installDir)
	}

	return target, nil
}
