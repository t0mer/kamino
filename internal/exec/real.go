package exec

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// KillGrace is how long a process group has to exit after SIGTERM before it is
// sent SIGKILL.
const KillGrace = 5 * time.Second

// RealExecutor runs commands with os/exec.
type RealExecutor struct{}

// NewRealExecutor builds an executor that runs real commands.
func NewRealExecutor() *RealExecutor { return &RealExecutor{} }

// Run executes c, streaming each output line to out.
func (e *RealExecutor) Run(ctx context.Context, c Command, out LineSink) (Result, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	cmd := exec.Command(c.Path, c.Args...)
	cmd.Dir = c.Dir
	if len(c.Env) > 0 {
		cmd.Env = append(os.Environ(), c.Env...)
	}
	// Own process group: script items spawn children, and a cancel must take
	// the whole tree, not just the shell that started it.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("opening stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, fmt.Errorf("opening stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("starting %s: %w", c.Path, err)
	}

	var (
		mu     sync.Mutex
		result Result
		wg     sync.WaitGroup
	)
	collect := func(stream string, r io.Reader, dest *[]string) {
		defer wg.Done()
		s := bufio.NewScanner(r)
		s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for s.Scan() {
			line := s.Text()
			mu.Lock()
			*dest = append(*dest, line)
			mu.Unlock()
			if out != nil {
				out(stream, line)
			}
		}
	}
	wg.Add(2)
	go collect("stdout", stdout, &result.Stdout)
	go collect("stderr", stderr, &result.Stderr)

	// cmd.Wait closes the stdout/stderr pipes as soon as it sees the process
	// exit. Per the os/exec docs, it is incorrect to call Wait before all
	// reads from the pipes have completed, so the collectors are drained
	// first and only then is Wait called.
	done := make(chan error, 1)
	go func() {
		wg.Wait()
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		killGroup(cmd)
		<-done
		return result, fmt.Errorf("running %s: %w", c.Path, ctx.Err())
	case waitErr := <-done:
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			// Ran and failed: that is a Result, not an error.
			result.ExitCode = exitErr.ExitCode()
			return result, nil
		}
		if waitErr != nil {
			return result, fmt.Errorf("running %s: %w", c.Path, waitErr)
		}
		return result, nil
	}
}

// killGroup terminates the command's whole process group: SIGTERM, then
// SIGKILL after a grace period.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pgid := -cmd.Process.Pid
	_ = syscall.Kill(pgid, syscall.SIGTERM)

	time.AfterFunc(KillGrace, func() {
		_ = syscall.Kill(pgid, syscall.SIGKILL)
	})
}
