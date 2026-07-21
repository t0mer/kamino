package runners

import (
	"context"
	"fmt"
	"path/filepath"
)

// DefaultBinDir is where single-file binaries are installed when the item
// does not declare an explicit path.
const DefaultBinDir = "/usr/local/bin"

// Binary installs a single downloaded executable.
type Binary struct{ d Deps }

var _ Runner = (*Binary)(nil)

// NewBinary builds a binary runner.
func NewBinary(d Deps) *Binary { return &Binary{d: d} }

// Check probes whether the binary is already present.
func (b *Binary) Check(ctx context.Context, it ResolvedItem) (bool, error) {
	return CheckProbe(ctx, b.d, it)
}

// Install downloads the binary, marks it executable and moves it into place
// at it.Path (or DefaultBinDir/<name> when Path is unset).
func (b *Binary) Install(ctx context.Context, it ResolvedItem) error {
	if it.Source == "" {
		return fmt.Errorf("%s: binary item has no source for this architecture", it.Ref)
	}

	name := itemName(it)
	dest := it.Path
	if dest == "" {
		dest = filepath.Join(DefaultBinDir, name)
	}
	dest, err := safeBinaryTarget(dest)
	if err != nil {
		return fmt.Errorf("%s: %w", it.Ref, err)
	}

	staged := filepath.Join(b.d.TempDir, name)
	if err := b.d.Download.Fetch(ctx, it.Source, staged, it.SHA256); err != nil {
		return fmt.Errorf("%s: %w", it.Ref, err)
	}

	if err := runArgv(ctx, b.d, it, "/bin/chmod", "0755", staged); err != nil {
		return err
	}
	if err := runArgv(ctx, b.d, it, "/bin/mkdir", "-p", filepath.Dir(dest)); err != nil {
		return err
	}
	return runArgv(ctx, b.d, it, "/bin/mv", "-f", staged, dest)
}

// safeBinaryTarget resolves the final destination path for a binary install
// (it.Path, or DefaultBinDir/<name> when unset — DefaultBinDir is always
// absolute, so only an explicit it.Path can fail this) and rejects the two
// always-invalid shapes: an empty path and a path that is not absolute.
//
// Like safeInstallTarget in tarball.go, this is a path sanity check only,
// not a shell-injection defense — chmod, mkdir and mv all run as direct
// argv commands via runArgv (see argv.go), never through /bin/sh -c, so a
// `;`, backtick, `$(...)`, quote or space in the path is just one odd argv
// element to those commands, never a second command.
func safeBinaryTarget(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("refusing to use an empty binary install path")
	}
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return "", fmt.Errorf("refusing to use %q as a binary install path: not an absolute path", path)
	}
	return clean, nil
}
