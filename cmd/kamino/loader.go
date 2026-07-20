package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/t0mer/kamino/internal/config"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/remote"
)

// dirFetcher reads a config repo from a local directory. It exists so that
// development and tests can run with no network and no repo, using the exact
// same load path as the real fetcher.
type dirFetcher struct{ root string }

func (d dirFetcher) Fetch(_ context.Context, _ string, path string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(d.root, filepath.FromSlash(path)))
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%s: %w", path, remote.ErrNotFound)
	}
	return b, err
}

// loadConfig resolves settings, pins the ref, and fetches the whole config
// repo. It is shared by validate, plan and apply so all three see identical
// config resolution behaviour.
func loadConfig(ctx context.Context) (*manifest.Resolved, error) {
	if flags.configDir != "" {
		return remote.Load(ctx, dirFetcher{root: flags.configDir}, "local")
	}

	saved, err := config.Load(flags.dataDir)
	if err != nil {
		return nil, err
	}
	settings := config.Resolve(saved, config.Overrides{
		RepoURL:         flags.repo,
		Ref:             flags.ref,
		Token:           flags.token,
		RawBaseTemplate: flags.rawBase,
	})
	if !settings.Configured() {
		return nil, fmt.Errorf("no config repo configured: pass --repo, set KAMINO_REPO, or save one via the web UI")
	}

	repo, err := remote.ParseRepo(settings.RepoURL, settings.Ref, settings.RawBaseTemplate)
	if err != nil {
		return nil, err
	}
	repo.Token = settings.Token

	sha, err := remote.NewAPIPinner(nil, "").Pin(ctx, repo)
	if err != nil {
		return nil, err
	}

	fetcher := remote.NewHTTPFetcher(repo, filepath.Join(flags.dataDir, "cache"), nil)
	return remote.Load(ctx, fetcher, sha)
}

// findProfile looks up a profile by id.
func findProfile(r *manifest.Resolved, id string) (manifest.Profile, error) {
	for _, p := range r.Profiles {
		if p.ID == id {
			return p, nil
		}
	}
	available := make([]string, 0, len(r.Profiles))
	for _, p := range r.Profiles {
		available = append(available, p.ID)
	}
	return manifest.Profile{}, fmt.Errorf("profile %q not found; available: %v", id, available)
}
