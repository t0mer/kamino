package exec

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// KillGrace is how long a process group has to exit after SIGTERM before it is
// sent SIGKILL.
const KillGrace = 5 * time.Second

// maxLineBytes caps how much of a single output line is retained and passed
// to the LineSink. A line longer than this is truncated (and marked as such)
// rather than dropped, so one abnormally long line — a wrapped apt/dpkg
// progress line, for example — can never make the collector silently lose
// the rest of the stream.
const maxLineBytes = 1 << 20 // 1MiB

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
		mu     sync.Mutex // guards result.Stdout / result.Stderr
		sinkMu sync.Mutex // serializes calls into the caller-supplied LineSink
		result Result
		wg     sync.WaitGroup
	)
	collect := func(stream string, r io.Reader, dest *[]string) {
		defer wg.Done()
		br := bufio.NewReaderSize(r, 64*1024)
		for {
			line, truncated, err := readLine(br)
			if truncated {
				line += " ...[kamino: line truncated, exceeded 1MiB]"
				slog.Warn("command output line truncated", "stream", stream, "limit_bytes", maxLineBytes)
			}
			// A line is only absent when readLine hit EOF/error with nothing
			// buffered; err == nil always carries a real (possibly empty) line.
			if err == nil || line != "" {
				mu.Lock()
				*dest = append(*dest, line)
				mu.Unlock()
				if out != nil {
					sinkMu.Lock()
					out(stream, line)
					sinkMu.Unlock()
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					slog.Warn("reading command output failed", "stream", stream, "error", err)
				}
				return
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

// readLine reads a single line from br, up to maxLineBytes. Unlike
// bufio.Scanner, an over-long line does not abort the stream: bytes beyond
// the cap are discarded and truncated is reported true, but br is left
// positioned right after the line's newline so the next call resumes
// cleanly with the following line. A final line with no trailing newline is
// still returned (with err set to whatever the reader gave, typically
// io.EOF); err is io.EOF once nothing more remains to read.
func readLine(br *bufio.Reader) (line string, truncated bool, err error) {
	var buf []byte
	for {
		chunk, e := br.ReadSlice('\n')
		if room := maxLineBytes - len(buf); room > 0 {
			if room > len(chunk) {
				room = len(chunk)
			}
			buf = append(buf, chunk[:room]...)
			if room < len(chunk) {
				truncated = true
			}
		} else if len(chunk) > 0 {
			truncated = true
		}

		switch {
		case e == nil:
			// ReadSlice found the delimiter: the line is complete.
			return dropNewline(buf), truncated, nil
		case errors.Is(e, bufio.ErrBufferFull):
			// No delimiter within this internal buffer's worth of data yet;
			// keep reading the same logical line.
			continue
		default:
			// io.EOF or a genuine read error: return whatever was captured.
			return dropNewline(buf), truncated, e
		}
	}
}

// dropNewline strips a single trailing "\n" and, if present, a preceding
// "\r" — matching bufio.ScanLines' handling of CRLF line endings.
func dropNewline(buf []byte) string {
	s := strings.TrimSuffix(string(buf), "\n")
	s = strings.TrimSuffix(s, "\r")
	return s
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
