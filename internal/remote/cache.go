package remote

import (
	"fmt"
	"os"
	"path/filepath"
)

// diskCache mirrors fetched files so a provider outage cannot block a rebuild.
// It is an offline fallback, not a working copy.
type diskCache struct {
	dir string
}

// path resolves ref and file to a location under c.dir, rejecting any
// combination that would escape it. ref and file ultimately come from the
// config repo, which is untrusted content — a path containing ".." must not
// be able to make the fetcher (running as root) write or read files outside
// its own cache directory.
func (c diskCache) path(ref, file string) (string, error) {
	p, err := SafeJoin(c.dir, ref, filepath.FromSlash(file))
	if err != nil {
		return "", fmt.Errorf("cache path escapes cache dir: ref=%q file=%q: %w", ref, file, err)
	}
	return p, nil
}

func (c diskCache) read(ref, file string) ([]byte, error) {
	if c.dir == "" {
		return nil, fmt.Errorf("no cache dir configured")
	}
	p, err := c.path(ref, file)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

func (c diskCache) write(ref, file string, data []byte) error {
	if c.dir == "" {
		return nil
	}
	p, err := c.path(ref, file)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}
	// Write-then-rename so a crash mid-write cannot leave a truncated file
	// that would later be served as if it were good.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing cache file: %w", err)
	}
	return os.Rename(tmp, p)
}
