package runners

import (
	"context"
	"fmt"
	"strings"
)

// Apt installs Debian packages with apt-get.
type Apt struct{ d Deps }

// NewApt builds an apt runner.
func NewApt(d Deps) *Apt { return &Apt{d: d} }

// Check probes whether the packages are already present.
func (a *Apt) Check(ctx context.Context, it ResolvedItem) (bool, error) {
	return CheckProbe(ctx, a.d, it)
}

// Install adds any declared repo, then installs the packages.
//
// DEBIAN_FRONTEND=noninteractive is always set on the install command so a
// package's postinst script cannot open a dialog and hang the run until the
// step timeout fires.
func (a *Apt) Install(ctx context.Context, it ResolvedItem) error {
	if len(it.Packages) == 0 {
		return fmt.Errorf("%s: apt item declares no packages", it.Ref)
	}

	if it.Repo != "" {
		if err := run(ctx, a.d, it, "add-apt-repository -y "+it.Repo); err != nil {
			return err
		}
		// A freshly added repo has no package index yet, so the install that
		// follows would otherwise fail with "unable to locate package".
		if err := run(ctx, a.d, it, "apt-get update"); err != nil {
			return err
		}
	}

	install := fmt.Sprintf("DEBIAN_FRONTEND=noninteractive apt-get install -y %s",
		strings.Join(it.Packages, " "))
	return run(ctx, a.d, it, install)
}
