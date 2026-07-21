package runners

import (
	"context"
	"fmt"
)

// Apt installs Debian packages with apt-get.
type Apt struct{ d Deps }

var _ Runner = (*Apt)(nil)

// NewApt builds an apt runner.
func NewApt(d Deps) *Apt { return &Apt{d: d} }

// Check probes whether the packages are already present.
func (a *Apt) Check(ctx context.Context, it ResolvedItem) (bool, error) {
	return CheckProbe(ctx, a.d, it)
}

// Install adds any declared repo, then installs the packages.
//
// Every command runs as argv rather than through /bin/sh, so a package name or
// repo string from the config repo reaches apt as exactly one argument — a
// stray ";" or backtick in a typo'd entry is an odd package name, never a
// second command. The package list is preceded by "--" so a name beginning
// with "-" is treated as a package rather than as a flag to apt-get.
//
// DEBIAN_FRONTEND=noninteractive is set on the install command so a package's
// postinst script cannot open a dialog and hang the run until the step timeout
// fires. It is carried in the command's environment, which RealExecutor
// appends to os.Environ(), rather than as a shell prefix.
func (a *Apt) Install(ctx context.Context, it ResolvedItem) error {
	if len(it.Packages) == 0 {
		return fmt.Errorf("%s: apt item declares no packages", it.Ref)
	}

	if it.Repo != "" {
		if err := runArgv(ctx, a.d, it, addAptRepositoryPath, "-y", it.Repo); err != nil {
			return err
		}
		// A freshly added repo has no package index yet, so the install that
		// follows would otherwise fail with "unable to locate package".
		if err := runArgvEnv(ctx, a.d, it, debianFrontendEnv, aptGetPath, "update"); err != nil {
			return err
		}
	}

	args := append([]string{"install", "-y", "--"}, it.Packages...)
	return runArgvEnv(ctx, a.d, it, debianFrontendEnv, aptGetPath, args...)
}
