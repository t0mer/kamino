package runners

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ScriptSource fetches files from the config repo. remote.Fetcher satisfies
// it; this package declares its own narrow copy of the method instead of
// importing internal/remote, since runners is a leaf package (see
// runner.go's package comment on the import-cycle constraint).
type ScriptSource interface {
	Fetch(ctx context.Context, ref, path string) ([]byte, error)
}

// Script installs an item by materialising a shell script to disk and
// running it. The script comes from one of two places: an https:// URL
// (downloaded like any other artifact) or a path inside the config repo
// (fetched lazily, only when this step actually runs, via ScriptSource at
// the ref this run is pinned to). Either way the script ends up on disk and
// is executed with /bin/sh — this is the item type that most obviously runs
// arbitrary code as root, which is why plan surfaces every script item as a
// warning before a run is approved (see CLAUDE.md §7).
type Script struct {
	d   Deps
	src ScriptSource
	ref string
}

var _ Runner = (*Script)(nil)

// NewScript builds a script runner. ref is the config repo ref (commit SHA
// or branch) this run is pinned to, used only for the repo-relative-path
// branch of Install.
func NewScript(d Deps, src ScriptSource, ref string) *Script {
	return &Script{d: d, src: src, ref: ref}
}

// Check probes whether the script's product is already installed.
func (s *Script) Check(ctx context.Context, it ResolvedItem) (bool, error) {
	return CheckProbe(ctx, s.d, it)
}

// Install materialises the script to Deps.TempDir and runs it.
//
// it.Source may carry a secret expanded by internal/engine.Resolve (see
// runner.go's comment on run/it.Source), so it is never included in an
// error or log here — except the one narrow, deliberate case of the
// repo-relative fetch failing, where the repo-relative path is exactly what
// an operator needs to find the broken reference in their own config repo,
// and is not the shape a secret would appear in (an https:// URL is the
// shape that can carry one, via query-string templating).
func (s *Script) Install(ctx context.Context, it ResolvedItem) error {
	if it.Source == "" {
		return fmt.Errorf("%s: script item has no source", it.Ref)
	}

	local, err := safeScriptTarget(s.d.TempDir, it)
	if err != nil {
		return err
	}

	if strings.HasPrefix(it.Source, "https://") {
		if err := s.d.Download.Fetch(ctx, it.Source, local, it.SHA256); err != nil {
			return fmt.Errorf("%s: %w", it.Ref, err)
		}
	} else {
		// Repo-relative: fetched lazily, only now that the step is actually
		// running, at the ref pinned for this run — planning must not pay to
		// fetch scripts it may never run.
		body, err := s.src.Fetch(ctx, s.ref, it.Source)
		if err != nil {
			return fmt.Errorf("%s: fetching %s: %w", it.Ref, it.Source, err)
		}
		// 0o700, and left that way: `sh <file>` reads the script, it does not
		// need the execute bit, so widening the mode would only open a window
		// in a shared temp dir for another local user to tamper with a file
		// that is about to run as root.
		if err := os.WriteFile(local, body, 0o700); err != nil {
			return fmt.Errorf("%s: writing script to disk: %w", it.Ref, err)
		}
	}

	return runArgv(ctx, s.d, it, "/bin/sh", local)
}

// safeScriptTarget resolves where a script is materialised inside
// Deps.TempDir.
//
// The config repo is trusted by design (CLAUDE.md §7) — an operator who
// points Kamino at a malicious repo has already lost via type: script — so
// this is not a defense against that trust boundary. It is a cheap
// containment guard in the same spirit as safeInstallTarget and
// safeBinaryTarget (tarball.go, binary.go): reject a mistaken *path*, not a
// malicious *command*. The file this returns is later passed to /bin/sh via
// runArgv, never through /bin/sh -c, so whatever the path contains reaches
// the OS as one argv element rather than a second command.
//
// The name comes from the item's ref, never from it.Source. Resolve expands
// {secret:NAME} into Source, so an https source carrying a token in its query
// string would otherwise bake that token into the on-disk filename and into
// the argv of the commands run against it — where it is visible in ps and
// /proc/<pid>/cmdline. Redaction cannot reach either of those. The ref is
// built from category and item ids, which are never templated, and it is
// unique per item, so it also cannot collide within a run. This matches how
// tarball.go and binary.go name their artifacts.
func safeScriptTarget(tempDir string, it ResolvedItem) (string, error) {
	name := itemName(it)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("%s: refusing to materialise script: item id is not a usable file name", it.Ref)
	}

	clean := filepath.Clean(tempDir)
	target := filepath.Join(clean, name+".sh")
	if filepath.Dir(target) != clean {
		return "", fmt.Errorf("%s: refusing to materialise script outside the run temp dir", it.Ref)
	}

	return target, nil
}
