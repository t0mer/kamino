package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/t0mer/kamino/internal/config"
	"github.com/t0mer/kamino/internal/engine/runners"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/remote"
)

// dirFetcher reads a config repo from a local directory. It exists so that
// development and tests can run with no network and no repo, using the exact
// same load path as the real fetcher.
type dirFetcher struct{ root string }

func (d dirFetcher) Fetch(_ context.Context, _ string, path string) ([]byte, error) {
	// path comes from the config repo's own manifest.yaml (its categories/
	// profiles lists), which is untrusted content — it must not be able to
	// walk this fetcher (running as root) outside the configured root dir.
	p, err := remote.SafeJoin(d.root, filepath.FromSlash(path))
	if err != nil {
		return nil, fmt.Errorf("config path %q escapes config dir %q: %w", path, d.root, err)
	}

	b, err := os.ReadFile(p)
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
		RepoToken:       flags.token,
		RawBaseTemplate: flags.rawBase,
	})
	if !settings.Configured() {
		return nil, fmt.Errorf("no config repo configured: pass --repo, set KAMINO_REPO, or save one via the web UI")
	}

	repo, err := remote.ParseRepo(settings.RepoURL, settings.Ref, settings.RawBaseTemplate)
	if err != nil {
		return nil, err
	}
	repo.Token = settings.RepoToken

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

// configSource returns a fetcher the script runner can use to pull scripts
// lazily from the config repo, honouring --config-dir for local development.
//
// The ref passed to Fetch at call time (built.ConfigSHA, pinned once at plan
// time by loadConfig) always wins over anything resolved here, so a fresh
// HTTPFetcher with its own Repo.Ref is safe to build independently: it never
// causes a script step to read a different commit than the rest of the plan.
//
// Any failure to resolve settings here returns a source that fails closed
// (every Fetch call errors) rather than falling back to dirFetcher with an
// empty root. That fallback would resolve a repo-relative script path (e.g.
// "scripts/x.sh") against the process's current working directory instead of
// the pinned config repo — silently executing whatever unrelated file
// happens to sit at that relative path, as root. --config-dir is the only
// case where reading from a local directory is intentional, and it is
// handled above before any of these fallible calls run.
func configSource(_ context.Context) runners.ScriptSource {
	if flags.configDir != "" {
		return dirFetcher{root: flags.configDir}
	}

	saved, err := config.Load(flags.dataDir)
	if err != nil {
		return errSource{err: fmt.Errorf("loading settings: %w", err)}
	}
	settings := config.Resolve(saved, config.Overrides{
		RepoURL:         flags.repo,
		Ref:             flags.ref,
		RepoToken:       flags.token,
		RawBaseTemplate: flags.rawBase,
	})
	repo, err := remote.ParseRepo(settings.RepoURL, settings.Ref, settings.RawBaseTemplate)
	if err != nil {
		return errSource{err: fmt.Errorf("resolving config repo: %w", err)}
	}
	repo.Token = settings.RepoToken
	return remote.NewHTTPFetcher(repo, filepath.Join(flags.dataDir, "cache"), nil)
}

// errSource is a runners.ScriptSource that always fails. configSource
// returns it when settings cannot be resolved, so a script step fails loudly
// with a clear error instead of silently reading from an unintended location.
type errSource struct{ err error }

// Fetch always returns the wrapped error.
func (e errSource) Fetch(context.Context, string, string) ([]byte, error) {
	return nil, e.err
}
