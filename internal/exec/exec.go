// Package exec is the seam between Kamino and the shell. Runners depend on the
// CommandExecutor interface, never on os/exec, so their logic is testable
// without a machine to install onto.
package exec

import (
	"context"
	"time"
)

// Command is a single command to run.
type Command struct {
	Path    string
	Args    []string
	Env     []string
	Dir     string
	Timeout time.Duration
}

// Line renders the command for logging and for fake matching.
func (c Command) Line() string {
	out := c.Path
	for _, a := range c.Args {
		out += " " + a
	}
	return out
}

// Result is the outcome of a command that ran to completion.
type Result struct {
	ExitCode int
	Stdout   []string
	Stderr   []string
}

// Output returns stdout and stderr combined, in capture order per stream.
func (r Result) Output() []string {
	return append(append([]string{}, r.Stdout...), r.Stderr...)
}

// LineSink receives each captured output line as it is produced, so callers
// can stream progress without waiting for the command to finish.
//
// Concurrency guarantee: implementations of CommandExecutor in this package
// never call a LineSink from more than one goroutine at a time, even though
// stdout and stderr are collected concurrently — calls are serialized
// centrally. A LineSink therefore does not need its own locking to be safe
// against concurrent invocation from this package.
type LineSink func(stream, line string)

// CommandExecutor runs commands.
//
// A non-nil error means the command could not be run or could not complete
// (missing binary, timeout, cancellation). A command that ran and exited
// non-zero returns a nil error and a non-zero Result.ExitCode.
type CommandExecutor interface {
	Run(ctx context.Context, c Command, out LineSink) (Result, error)
}

// Shell wraps a shell line as a command.
func Shell(line string) Command {
	return Command{Path: "/bin/sh", Args: []string{"-c", line}}
}
