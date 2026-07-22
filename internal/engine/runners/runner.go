// Package runners implements per-item-type installers consumed by the
// engine. It is deliberately a leaf package: it must not import
// internal/engine, because internal/engine imports runners for the Runner
// interface and RunnerFor lookup type — importing engine back from here
// would form an import cycle. ResolvedItem is therefore defined here, and
// internal/engine.ResolvedItem is a type alias to it (see
// internal/engine/item.go), so callers on either side of the package
// boundary see one identical type.
package runners

import (
	"context"
	"strings"
	"time"

	"github.com/t0mer/kamino/internal/download"
	kexec "github.com/t0mer/kamino/internal/exec"
	"github.com/t0mer/kamino/internal/manifest"
)

// ResolvedItem is an item with its architecture chosen and every placeholder
// expanded. Runners only ever see this type, so templating is implemented
// and tested in exactly one place (internal/engine.Resolve).
type ResolvedItem struct {
	Ref           string
	Name          string
	Type          manifest.ItemType
	Version       string
	Source        string
	SHA256        string
	Packages      []string
	Repo          string
	Python        string
	InstallDir    string
	PathExport    string
	Path          string
	Files         []string
	EnvFile       string
	Secrets       []string
	Check         string
	CheckContains string
	PreInstall    []string
	PostInstall   []string
	Timeout       time.Duration
}

// Runner installs one item type. Concrete implementations (apt, deb,
// tarball, binary, pip, script, compose_stack) are added in a later task;
// this interface is the seam the engine depends on.
type Runner interface {
	// Check reports whether the item is already installed.
	Check(ctx context.Context, item ResolvedItem) (bool, error)
	// Install installs the item.
	Install(ctx context.Context, item ResolvedItem) error
}

// Deps are the collaborators every runner needs to talk to the host: a
// command executor, a downloader for remote artifacts, and a directory for
// any files a step must write to disk before it can execute.
type Deps struct {
	Exec     kexec.CommandExecutor
	Download download.Downloader
	TempDir  string
}

// CheckProbe is the shared idempotency probe used by every runner's Check.
//
// One rule, no exit-code special-casing: only a zero exit whose output
// contains it.CheckContains counts as installed. Everything else — a
// non-zero exit (including 127 for a missing binary), a zero exit whose
// output does not contain CheckContains, an empty Check (we cannot tell), or
// the probe failing to run at all — means "not installed, go ahead". A probe
// that cannot run is treated as an answer, not a step failure: the installer
// that follows will produce a more useful error than the probe could.
func CheckProbe(ctx context.Context, d Deps, it ResolvedItem) (bool, error) {
	if it.Check == "" {
		return false, nil
	}

	c := kexec.Shell(it.Check)
	c.Timeout = it.Timeout

	res, err := d.Exec.Run(ctx, c, nil)
	if err != nil {
		return false, nil
	}
	if res.ExitCode != 0 {
		return false, nil
	}
	if it.CheckContains == "" {
		return true, nil
	}
	return strings.Contains(strings.Join(res.Output(), "\n"), it.CheckContains), nil
}
