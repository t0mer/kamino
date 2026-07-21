package runners

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	kexec "github.com/t0mer/kamino/internal/exec"
)

// debianFrontendEnv sets DEBIAN_FRONTEND=noninteractive on dpkg/apt-get
// commands run via argv, so a package's postinst script cannot open a dialog
// and hang the run until the step timeout fires. This is carried as a
// Command.Env entry (RealExecutor appends it to os.Environ()) rather than a
// shell-string prefix, since these commands run through runArgv, never
// /bin/sh -c.
var debianFrontendEnv = []string{"DEBIAN_FRONTEND=noninteractive"}

// Deb installs a downloaded .deb package.
type Deb struct{ d Deps }

// NewDeb builds a deb runner.
func NewDeb(d Deps) *Deb { return &Deb{d: d} }

// Check probes whether the package is already installed.
func (r *Deb) Check(ctx context.Context, it ResolvedItem) (bool, error) {
	return CheckProbe(ctx, r.d, it)
}

// Install downloads the package, verifies it against SHA256, installs it
// with dpkg, then runs the apt-get dependency fix-up pass.
//
// dpkg -i installs the package but does not resolve its dependencies; a
// package left half-configured this way fails confusingly at first use
// rather than at install time. The apt-get install -f -y pass that follows
// is therefore not optional.
func (r *Deb) Install(ctx context.Context, it ResolvedItem) error {
	if it.Source == "" {
		return fmt.Errorf("%s: deb item has no source for this architecture", it.Ref)
	}

	pkg := filepath.Join(r.d.TempDir, itemName(it)+".deb")
	if err := r.d.Download.Fetch(ctx, it.Source, pkg, it.SHA256); err != nil {
		return fmt.Errorf("%s: %w", it.Ref, err)
	}

	if err := runArgvEnv(ctx, r.d, it, debianFrontendEnv, "dpkg", "-i", pkg); err != nil {
		return err
	}

	return runArgvEnv(ctx, r.d, it, debianFrontendEnv, "apt-get", "install", "-f", "-y")
}

// runArgvEnv is runArgv (argv.go) plus additional environment variables on
// the command. It is defined here rather than in argv.go because dpkg/apt-get
// are, so far, the only commands in this package that need an env override;
// every other runArgv caller is fine with the executor's default
// (RealExecutor appends Command.Env to os.Environ(), so env only ever adds
// entries, never removes the ambient environment).
func runArgvEnv(ctx context.Context, d Deps, it ResolvedItem, env []string, path string, args ...string) error {
	c := kexec.Command{Path: path, Args: args, Env: env, Timeout: it.Timeout}

	res, err := d.Exec.Run(ctx, c, nil)
	if err != nil {
		return fmt.Errorf("%s: running %q: %w", it.Ref, c.Line(), err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%s: %q exited %d: %s",
			it.Ref, c.Line(), res.ExitCode, strings.Join(res.Output(), "; "))
	}
	return nil
}
