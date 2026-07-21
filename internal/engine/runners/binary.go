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

	staged := filepath.Join(b.d.TempDir, name)
	if err := b.d.Download.Fetch(ctx, it.Source, staged, it.SHA256); err != nil {
		return fmt.Errorf("%s: %w", it.Ref, err)
	}

	if err := run(ctx, b.d, it, fmt.Sprintf("chmod 0755 %s", staged)); err != nil {
		return err
	}
	if err := run(ctx, b.d, it, fmt.Sprintf("mkdir -p %s", filepath.Dir(dest))); err != nil {
		return err
	}
	return run(ctx, b.d, it, fmt.Sprintf("mv -f %s %s", staged, dest))
}
