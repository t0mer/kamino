// Package engine executes an ordered plan against a host.
package engine

import (
	"fmt"
	"time"

	"github.com/t0mer/kamino/internal/engine/runners"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/secrets"
)

// DefaultTimeout applies when neither the item nor the manifest sets one.
const DefaultTimeout = 15 * time.Minute

// ResolvedItem is an item with its architecture chosen and every placeholder
// expanded. Runners only ever see this type, so templating is implemented and
// tested in exactly one place.
//
// This is a type alias, not a new type: the struct is defined in
// internal/engine/runners (a leaf package with no dependency on engine) so
// that package can declare its Runner interface over the same type engine
// uses, without an import cycle. See runners.ResolvedItem's doc comment.
type ResolvedItem = runners.ResolvedItem

// Resolve turns a manifest item into an executable one for arch.
//
// A nil store is treated as an empty one: an item that references a secret
// then fails with the usual missing-secret error rather than panicking, so a
// caller that passes nil for an item it believes has no secrets gets a clear
// message instead of a crash.
func Resolve(it manifest.Item, arch string, defaults manifest.Defaults, s *secrets.Store) (ResolvedItem, error) {
	if s == nil {
		s = secrets.New()
	}
	expand := func(in string) (string, error) {
		out, err := secrets.Expand(in, it.Version, s)
		if err != nil {
			return "", fmt.Errorf("item %s: %w", it.Ref(), err)
		}
		return out, nil
	}
	expandAll := func(in []string) ([]string, error) {
		if in == nil {
			return nil, nil
		}
		out := make([]string, 0, len(in))
		for _, line := range in {
			v, err := expand(line)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}

	timeout := it.Timeout
	if timeout == 0 {
		timeout = defaults.Timeout
	}
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	out := ResolvedItem{
		Ref:        it.Ref(),
		Name:       it.Name,
		Type:       it.Type,
		Version:    it.Version,
		SHA256:     it.SHA256[arch],
		Repo:       it.Repo,
		Python:     it.Python,
		InstallDir: it.InstallDir,
		PathExport: it.PathExport,
		Path:       it.Path,
		Files:      it.Files,
		Timeout:    timeout,
	}

	var err error
	if out.Source, err = expand(it.Source[arch]); err != nil {
		return ResolvedItem{}, err
	}
	if out.Check, err = expand(it.Check); err != nil {
		return ResolvedItem{}, err
	}
	if out.CheckContains, err = expand(it.CheckContains); err != nil {
		return ResolvedItem{}, err
	}
	if out.Packages, err = expandAll(it.Packages); err != nil {
		return ResolvedItem{}, err
	}
	if out.PreInstall, err = expandAll(it.PreInstall); err != nil {
		return ResolvedItem{}, err
	}
	if out.PostInstall, err = expandAll(it.PostInstall); err != nil {
		return ResolvedItem{}, err
	}
	return out, nil
}
