package runners

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// SecretSource looks up run-scoped secret values by name. *secrets.Store
// satisfies it; runners declares its own narrow copy so it stays a leaf
// package with no dependency on internal/secrets (see runner.go).
type SecretSource interface {
	Get(name string) (string, bool)
}

// Compose installs a docker compose stack. Its files are vendored inside the
// config repo under it.Path and fetched lazily at the run's pinned ref (only
// now that the step is actually running), materialised into a per-run temp
// dir, and brought up with `docker compose up -d`.
//
// Declared secrets are injected as process environment variables so the
// compose file can interpolate ${NAME}; they are never written to disk
// (CLAUDE.md §4.6) and never placed in argv (where ps and /proc/<pid>/cmdline
// would expose them) — only in Command.Env, which Command.Line never renders.
type Compose struct {
	d       Deps
	src     ScriptSource
	ref     string
	secrets SecretSource
}

var _ Runner = (*Compose)(nil)

// composeFileRE matches the file that `docker compose -f` is pointed at.
var composeFileRE = regexp.MustCompile(`^(docker-compose|compose)\.ya?ml$`)

// NewCompose builds a compose_stack runner. ref is the config repo ref this
// run is pinned to; secrets supplies values for the item's declared secrets.
func NewCompose(d Deps, src ScriptSource, ref string, secrets SecretSource) *Compose {
	return &Compose{d: d, src: src, ref: ref, secrets: secrets}
}

// Check probes idempotency. An explicit it.Check is honoured (a host-level
// probe such as `docker compose ls | grep -q monitoring`); with none declared
// it returns false and relies on `docker compose up -d` reconciling to the
// desired state.
func (c *Compose) Check(ctx context.Context, it ResolvedItem) (bool, error) {
	return CheckProbe(ctx, c.d, it)
}

// Install fetches the stack's files, materialises them, and runs `up -d`.
func (c *Compose) Install(ctx context.Context, it ResolvedItem) error {
	if it.Path == "" {
		return fmt.Errorf("%s: compose_stack item has no path", it.Ref)
	}
	if len(it.Files) == 0 {
		return fmt.Errorf("%s: compose_stack item declares no files", it.Ref)
	}

	stackDir, err := safeStackDir(c.d.TempDir, it)
	if err != nil {
		return err
	}

	composeRel, err := pickComposeFile(it)
	if err != nil {
		return err
	}

	for _, file := range it.Files {
		local, err := safeStackFile(stackDir, it, file)
		if err != nil {
			return err
		}
		body, err := c.src.Fetch(ctx, c.ref, path.Join(it.Path, file))
		if err != nil {
			return fmt.Errorf("%s: fetching %s: %w", it.Ref, path.Join(it.Path, file), err)
		}
		if err := os.MkdirAll(filepath.Dir(local), 0o700); err != nil {
			return fmt.Errorf("%s: creating stack dir: %w", it.Ref, err)
		}
		if err := os.WriteFile(local, body, 0o600); err != nil {
			return fmt.Errorf("%s: writing %s to disk: %w", it.Ref, file, err)
		}
	}

	env, err := c.secretEnv(it)
	if err != nil {
		return err
	}

	composePath := filepath.Join(stackDir, filepath.FromSlash(composeRel))
	return runArgvEnv(ctx, c.d, it, env,
		"docker", "compose", "-p", projectName(itemName(it)), "-f", composePath, "up", "-d")
}

// secretEnv builds NAME=value entries for every declared secret. A declared
// secret with no value is an error: the stack would come up mis-configured.
// The name is safe to show; the value never appears in the message.
func (c *Compose) secretEnv(it ResolvedItem) ([]string, error) {
	if len(it.Secrets) == 0 {
		return nil, nil
	}
	env := make([]string, 0, len(it.Secrets))
	for _, name := range it.Secrets {
		var val string
		var ok bool
		if c.secrets != nil {
			val, ok = c.secrets.Get(name)
		}
		if !ok {
			return nil, fmt.Errorf("%s: secret %q has no value", it.Ref, name)
		}
		env = append(env, name+"="+val)
	}
	return env, nil
}

// pickComposeFile returns the first declared file whose base name is a compose
// file. The stack cannot come up without one.
func pickComposeFile(it ResolvedItem) (string, error) {
	for _, f := range it.Files {
		if composeFileRE.MatchString(path.Base(f)) {
			return f, nil
		}
	}
	return "", fmt.Errorf("%s: no docker-compose file among files", it.Ref)
}

// safeStackDir resolves the per-run directory the stack is materialised into,
// named after the item id (never templated, unique per item). Same containment
// spirit as safeScriptTarget (script.go): reject a mistaken path, not a
// malicious command (the config repo is trusted by design, CLAUDE.md §7).
func safeStackDir(tempDir string, it ResolvedItem) (string, error) {
	name := itemName(it)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("%s: refusing to materialise stack: item id is not a usable dir name", it.Ref)
	}
	clean := filepath.Clean(tempDir)
	target := filepath.Join(clean, name)
	if filepath.Dir(target) != clean {
		return "", fmt.Errorf("%s: refusing to materialise stack outside the run temp dir", it.Ref)
	}
	return target, nil
}

// safeStackFile resolves where one declared file is written, rejecting any
// entry that would escape the stack dir (absolute path or a cleaned form that
// climbs out with "..").
func safeStackFile(stackDir string, it ResolvedItem, file string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(file))
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("%s: refusing an absolute file entry %q", it.Ref, file)
	}
	target := filepath.Join(stackDir, clean)
	rel, err := filepath.Rel(stackDir, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s: refusing to materialise file outside the stack dir: %q", it.Ref, file)
	}
	return target, nil
}

// projectName sanitises an item id to docker compose's project-name charset
// ([a-z0-9][a-z0-9_-]*), so a re-run reconciles the same project.
func projectName(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	s := strings.TrimLeft(b.String(), "-_")
	if s == "" {
		return "kamino"
	}
	return s
}
