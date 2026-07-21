package exec_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kexec "github.com/t0mer/kamino/internal/exec"
)

func TestRealExecutorCapturesStdout(t *testing.T) {
	var lines []string
	e := kexec.NewRealExecutor()

	got, err := e.Run(context.Background(), kexec.Shell("echo hello"), func(stream, line string) {
		lines = append(lines, stream+":"+line)
	})

	require.NoError(t, err)
	assert.Equal(t, 0, got.ExitCode)
	assert.Equal(t, []string{"hello"}, got.Stdout)
	assert.Equal(t, []string{"stdout:hello"}, lines)
}

func TestRealExecutorCapturesStderrSeparately(t *testing.T) {
	e := kexec.NewRealExecutor()

	got, err := e.Run(context.Background(), kexec.Shell("echo oops >&2"), nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"oops"}, got.Stderr)
	assert.Empty(t, got.Stdout)
}

func TestRealExecutorNonZeroExitIsNotAnError(t *testing.T) {
	e := kexec.NewRealExecutor()

	got, err := e.Run(context.Background(), kexec.Shell("exit 3"), nil)

	require.NoError(t, err, "a command that ran and failed is a Result, not an error")
	assert.Equal(t, 3, got.ExitCode)
}

func TestRealExecutorMissingBinaryIsAnError(t *testing.T) {
	e := kexec.NewRealExecutor()

	_, err := e.Run(context.Background(), kexec.Command{Path: "/nonexistent/kamino-test"}, nil)

	require.Error(t, err)
}

func TestRealExecutorEnforcesTimeout(t *testing.T) {
	e := kexec.NewRealExecutor()
	c := kexec.Shell("sleep 10")
	c.Timeout = 200 * time.Millisecond

	start := time.Now()
	_, err := e.Run(context.Background(), c, nil)

	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second, "timeout must kill the command promptly")
}

func TestRealExecutorRespectsContextCancellation(t *testing.T) {
	e := kexec.NewRealExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := e.Run(ctx, kexec.Shell("sleep 10"), nil)

	require.Error(t, err)
}

func TestRealExecutorPassesEnv(t *testing.T) {
	e := kexec.NewRealExecutor()
	c := kexec.Shell("echo $KAMINO_TEST_VAR")
	c.Env = append(c.Env, "KAMINO_TEST_VAR=present")

	got, err := e.Run(context.Background(), c, nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"present"}, got.Stdout)
}

// TestRealExecutorOverLongLineDoesNotDropSubsequentOutput is the regression
// test for the truncation bug: an over-long line used to make the collector
// goroutine exit for good (bufio.Scanner + ErrTooLong), silently discarding
// every line written after it while Run still reported success. What matters
// most here is that "after1"/"after2" survive; the exact truncation marker
// is secondary.
func TestRealExecutorOverLongLineDoesNotDropSubsequentOutput(t *testing.T) {
	e := kexec.NewRealExecutor()
	// One line far beyond the internal per-line cap (1MiB), followed by two
	// short, ordinary lines.
	cmd := kexec.Shell(`head -c 1500000 /dev/zero | tr '\0' 'a'; echo; echo after1; echo after2`)

	got, err := e.Run(context.Background(), cmd, nil)

	require.NoError(t, err, "a command that merely produces a long line must not become a failed run")
	require.Len(t, got.Stdout, 3, "the over-long line must not swallow the lines that come after it")
	assert.Less(t, len(got.Stdout[0]), 1500000, "the over-long line should have been truncated, not dropped")
	assert.Contains(t, got.Stdout[0], "truncated", "a truncated line must be marked as such, not silently shortened")
	assert.Equal(t, "after1", got.Stdout[1])
	assert.Equal(t, "after2", got.Stdout[2])
}

// TestRealExecutorLineSinkIsNeverInvokedConcurrently guards the serialization
// guarantee documented on LineSink: stdout and stderr are collected by
// separate goroutines, but the sink itself must only ever be entered by one
// of them at a time. Reentrancy is detected with a non-blocking
// compare-and-swap flag (not -race) because a benign-looking concurrent
// callback is not something the race detector is guaranteed to flag.
func TestRealExecutorLineSinkIsNeverInvokedConcurrently(t *testing.T) {
	e := kexec.NewRealExecutor()
	cmd := kexec.Shell(`for i in $(seq 1 150); do echo "out$i"; echo "err$i" >&2; done`)

	var busy int32
	var reentered int32
	sink := func(stream, line string) {
		if !atomic.CompareAndSwapInt32(&busy, 0, 1) {
			atomic.StoreInt32(&reentered, 1)
			return
		}
		// Hold the "in the sink" state briefly to widen the window in which
		// a concurrent call from the other stream's collector would land.
		time.Sleep(time.Millisecond)
		atomic.StoreInt32(&busy, 0)
	}

	got, err := e.Run(context.Background(), cmd, sink)

	require.NoError(t, err)
	assert.Equal(t, int32(0), atomic.LoadInt32(&reentered), "LineSink must never be invoked concurrently")
	assert.Len(t, got.Stdout, 150)
	assert.Len(t, got.Stderr, 150)
}

// TestRealExecutorCancellationReturnsPromptly confirms that centralizing the
// sink call and switching the collector off bufio.Scanner did not reintroduce
// the earlier bug where cancellation blocked on draining the collectors: a
// cancelled long-running command must still return quickly, not hang until
// the process would have exited on its own.
func TestRealExecutorCancellationReturnsPromptly(t *testing.T) {
	e := kexec.NewRealExecutor()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := e.Run(ctx, kexec.Shell("sleep 10"), nil)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Less(t, elapsed, 3*time.Second, "cancellation must not block on a drain that never finishes")
}
