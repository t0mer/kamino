package remote

import (
	"context"
	"fmt"
	"time"

	"github.com/t0mer/kamino/internal/manifest"
)

// Load fetches and parses an entire config repo at sha. Scripts and stack
// files are deliberately not fetched: they are pulled lazily by the step that
// needs them, so a plan does not pay for files it will never run.
func Load(ctx context.Context, f Fetcher, sha string) (*manifest.Resolved, error) {
	raw, err := f.Fetch(ctx, sha, "manifest.yaml")
	if err != nil {
		return nil, fmt.Errorf("fetching manifest.yaml: %w", err)
	}
	m, err := manifest.ParseManifest(raw)
	if err != nil {
		return nil, err
	}

	out := &manifest.Resolved{
		Manifest:  *m,
		SHA:       sha,
		FetchedAt: time.Now().UTC(),
	}

	for _, path := range m.Categories {
		b, err := f.Fetch(ctx, sha, path)
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", path, err)
		}
		c, err := manifest.ParseCategory(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out.Categories = append(out.Categories, *c)
	}

	for _, path := range m.Profiles {
		b, err := f.Fetch(ctx, sha, path)
		if err != nil {
			return nil, fmt.Errorf("fetching %s: %w", path, err)
		}
		p, err := manifest.ParseProfile(b)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out.Profiles = append(out.Profiles, *p)
	}

	if hf, ok := f.(*HTTPFetcher); ok {
		out.Stale = hf.Stale()
	}
	return out, nil
}
