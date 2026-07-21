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
	"time"

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
