package runners

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultInstallDir is where tarballs are extracted when the item does not
// declare an install_dir.
const DefaultInstallDir = "/usr/local"

// Tarball installs a .tar.gz archive by extracting it into an install dir.
type Tarball struct{ d Deps }

var _ Runner = (*Tarball)(nil)

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

	if err := runArgv(ctx, t.d, it, "/bin/rm", "-rf", target); err != nil {
		return err
	}
	if err := runArgv(ctx, t.d, it, "/bin/mkdir", "-p", installDir); err != nil {
		return err
	}
	if err := runArgv(ctx, t.d, it, "/bin/tar", "-C", installDir, "-xzf", archive); err != nil {
		return err
	}

	if it.PathExport != "" {
		if err := t.writePathExport(ctx, it, name); err != nil {
			return err
		}
	}
	return nil
}

// writePathExport creates /etc/profile.d/kamino-<name>.sh so a login shell
// picks up it.PathExport on $PATH.
//
// The line this used to run was `printf '%s\n' 'export PATH=$PATH:VALUE' >
// profile && chmod 0644 profile` via /bin/sh -c, with VALUE interpolated
// unquoted inside the single-quoted printf argument. A PathExport value
// containing a single quote (e.g. "/x'; rm -rf /home; echo 'y") closed that
// quote early and appended a second, attacker-chosen shell command that ran
// as root at install time — independent of, and not covered by,
// safeInstallTarget.
//
// The fix eliminates the shell for this write rather than trying to escape
// it correctly: the file's content is built with fmt.Sprintf (plain string
// concatenation, no shell parser involved, so a quote or `;` in PathExport
// is just a character in the resulting text) and staged with os.WriteFile
// in TempDir, which is a real filesystem write with no command line at all
// to inject into. Only placing the finished file at its real destination
// runs a command, and it does so via runArgv (direct argv, not a shell), so
// the destination path — already derived from the item id validated by
// safeInstallTarget — cannot be used to inject a second command either.
//
// This staged-then-move shape (rather than os.WriteFile straight to
// /etc/profile.d) keeps the write behind the same Deps.Exec seam every
// other install action goes through, so runner tests can assert on it with
// FakeExecutor instead of mutating the real host filesystem from `go test`.
func (t *Tarball) writePathExport(ctx context.Context, it ResolvedItem, name string) error {
	profile := fmt.Sprintf("/etc/profile.d/kamino-%s.sh", name)
	content := fmt.Sprintf("export PATH=$PATH:%s\n", it.PathExport)

	staged := filepath.Join(t.d.TempDir, name+".profile.sh")
	if err := os.WriteFile(staged, []byte(content), 0644); err != nil {
		return fmt.Errorf("%s: writing %s: %w", it.Ref, staged, err)
	}

	if err := runArgv(ctx, t.d, it, "/bin/mv", "-f", staged, profile); err != nil {
		return err
	}
	return runArgv(ctx, t.d, it, "/bin/chmod", "0644", profile)
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
// about to `rm -rf` as root, and rejects a handful of always-invalid shapes
// that would make that removal wrong or catastrophic: an install dir that
// is empty, ".", "/", or not absolute, and an item id that is empty, ".",
// "..", or contains a path separator (which could let the target escape
// installDir — the filepath.Rel check below catches any remaining case
// filepath.Join's normalization does not).
//
// What this function does NOT do — and does not need to — is defend
// against shell injection. rm -rf, mkdir -p and tar -C all run as direct
// argv commands via runArgv (see argv.go), never through /bin/sh -c, so a
// `;`, backtick, `$(...)`, quote or space anywhere in installDir or name
// is just one odd argv element, not a second command, regardless of
// whether this function would otherwise allow it through. This function's
// only job is rejecting a mistaken *path* (a typo, a relative dir, a ".."
// escape); it has nothing to say about malicious *content*, because there
// is no shell here for content to be malicious to.
//
// The config repo is trusted by design in this project's threat model (see
// CLAUDE.md §7) — an operator who points Kamino at a malicious repo has
// already lost via `type: script`. This guard exists for the narrower case
// of an honest typo (a blank install_dir, an install_dir left relative or
// at "/", or an item id that is empty or contains a path separator).
func safeInstallTarget(installDir, name string) (string, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("refusing to build an install path from item id %q", name)
	}

	clean := filepath.Clean(installDir)
	if clean == "" || clean == "." || clean == "/" {
		return "", fmt.Errorf("refusing to use %q as an install dir: too broad", installDir)
	}
	if !filepath.IsAbs(clean) {
		return "", fmt.Errorf("refusing to use %q as an install dir: not an absolute path", installDir)
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
