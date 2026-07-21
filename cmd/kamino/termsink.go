package main

import (
	"fmt"
	"io"
	"sync"

	"github.com/t0mer/kamino/internal/state"
)

// termSink prints run progress to the terminal. It satisfies engine.Sink.
type termSink struct {
	mu      sync.Mutex
	out     io.Writer
	verbose bool
	total   int
	done    int
	names   map[string]string
}

// newTermSink builds a terminal sink. names maps an item ref to its display
// name, used so progress lines read "Installing Docker Engine" rather than
// "Installing tools/docker".
func newTermSink(out io.Writer, total int, names map[string]string, verbose bool) *termSink {
	return &termSink{out: out, total: total, names: names, verbose: verbose}
}

// StepStatus prints a one-line transition per step.
func (t *termSink) StepStatus(_, stepRef string, s state.Status) {
	t.mu.Lock()
	defer t.mu.Unlock()

	name := t.names[stepRef]
	if name == "" {
		name = stepRef
	}

	switch s {
	case state.StatusRunning:
		t.done++
		fmt.Fprintf(t.out, "[%d/%d] %s…\n", t.done, t.total, name)
	case state.StatusSkipped:
		// No increment: a skipped step was already counted when the engine
		// announced it as running. Counting it twice printed "[2/1]".
		fmt.Fprintf(t.out, "[%d/%d] %s — already installed, skipped\n", t.done, t.total, name)
	case state.StatusSuccess:
		fmt.Fprintf(t.out, "        %s — done\n", name)
	case state.StatusFailed:
		fmt.Fprintf(t.out, "        %s — FAILED\n", name)
	case state.StatusCancelled:
		// Like blocked, a step cancelled before it started arrives without a
		// preceding running transition. Without a case here it printed
		// nothing at all, so a Ctrl-C left the operator staring at a run that
		// simply stopped saying anything.
		t.done++
		fmt.Fprintf(t.out, "[%d/%d] %s — cancelled\n", t.done, t.total, name)
	case state.StatusBlocked:
		// Blocked steps never run, so the engine emits this without a
		// preceding running transition — this is where they get counted.
		t.done++
		fmt.Fprintf(t.out, "[%d/%d] %s — blocked by a failed dependency\n", t.done, t.total, name)
	}
}

// LogLine prints captured output when --verbose is set.
func (t *termSink) LogLine(_, _, _, line string) {
	if !t.verbose {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprintf(t.out, "        | %s\n", line)
}
