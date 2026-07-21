package runners

import (
	"context"
	"fmt"
	"strings"

	kexec "github.com/t0mer/kamino/internal/exec"
)

// runArgv executes path with args as a direct argv command — never through
// /bin/sh -c — and turns a non-zero exit into an error carrying the
// command's output, mirroring run's error handling (see runner.go).
//
// Because there is no shell involved, a value that lands in one of args (an
// item id, an install dir, a downloaded file's path) reaches the operating
// system as exactly one argv element. A `;`, backtick, `$(...)`, quote or
// space in that value is just an odd character in a filename — never a way
// to run a second command. That is what makes runArgv the right tool for
// `rm -rf`, `mkdir -p`, `tar -C ... -xzf`, `chmod` and `mv -f`: none of
// those operations need a shell's features (globbing, redirection, `&&`),
// so there is no reason to pay for a shell's risks.
//
// run (runner.go) remains the right tool for lines that genuinely need
// shell features, such as this package's idempotency Check probes, which
// are themselves declared by the config repo as free-form shell — that
// trust boundary is unchanged by this helper (see CLAUDE.md §7).
func runArgv(ctx context.Context, d Deps, it ResolvedItem, path string, args ...string) error {
	c := kexec.Command{Path: path, Args: args, Timeout: it.Timeout}

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
