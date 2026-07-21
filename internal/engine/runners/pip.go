package runners

import (
	"context"
	"fmt"
)

// Pip installs Python packages system-wide.
type Pip struct{ d Deps }

// NewPip builds a pip runner.
func NewPip(d Deps) *Pip { return &Pip{d: d} }

// Check probes whether the packages are already installed.
func (p *Pip) Check(ctx context.Context, it ResolvedItem) (bool, error) {
	return CheckProbe(ctx, p.d, it)
}

// Install installs the declared packages with python{Python} -m pip (or
// python3 -m pip when Python is unset).
//
// --break-system-packages is required: Ubuntu 24.04 marks the system Python
// as externally managed (PEP 668) and refuses a system-wide install without
// it. Kamino provisions the whole machine, so a system-wide install is the
// intent, not an accident.
//
// Runs via runArgv, not a shell line: each package name lands as its own
// argv element, so a package value containing a space or `;` is inert — one
// odd argument to pip, never a second command.
func (p *Pip) Install(ctx context.Context, it ResolvedItem) error {
	if len(it.Packages) == 0 {
		return fmt.Errorf("%s: pip item declares no packages", it.Ref)
	}

	python := "python3"
	if it.Python != "" {
		python = "python" + it.Python
	}

	args := append([]string{"-m", "pip", "install", "--break-system-packages"}, it.Packages...)
	return runArgv(ctx, p.d, it, python, args...)
}
