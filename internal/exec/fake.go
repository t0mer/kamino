package exec

import (
	"context"
	"strings"
	"sync"
)

// scripted is one programmed response.
type scripted struct {
	match  string
	result Result
	err    error
}

// FakeExecutor returns programmed results and records every command it was
// asked to run, so runner tests can assert on the exact command sequence
// without installing anything.
type FakeExecutor struct {
	mu      sync.Mutex
	scripts []scripted
	calls   []Command
}

// NewFakeExecutor builds a fake that succeeds at everything by default.
func NewFakeExecutor() *FakeExecutor { return &FakeExecutor{} }

// Script programs a result for any command whose line contains match.
func (f *FakeExecutor) Script(match string, r Result) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts = append(f.scripts, scripted{match: match, result: r})
}

// ScriptErr programs an execution failure for commands containing match.
func (f *FakeExecutor) ScriptErr(match string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts = append(f.scripts, scripted{match: match, err: err})
}

// Calls returns every command run, in order.
func (f *FakeExecutor) Calls() []Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Command{}, f.calls...)
}

// CommandLines returns every command run, rendered as a line.
func (f *FakeExecutor) CommandLines() []string {
	out := []string{}
	for _, c := range f.Calls() {
		out = append(out, c.Line())
	}
	return out
}

// Run returns the first scripted match, or success.
func (f *FakeExecutor) Run(_ context.Context, c Command, out LineSink) (Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	line := c.Line()
	var hit *scripted
	for i := range f.scripts {
		if strings.Contains(line, f.scripts[i].match) {
			hit = &f.scripts[i]
			break
		}
	}
	f.mu.Unlock()

	if hit == nil {
		return Result{}, nil
	}
	if hit.err != nil {
		return Result{}, hit.err
	}
	if out != nil {
		for _, l := range hit.result.Stdout {
			out("stdout", l)
		}
		for _, l := range hit.result.Stderr {
			out("stderr", l)
		}
	}
	return hit.result, nil
}
