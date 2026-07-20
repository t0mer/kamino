package exec_test

import (
	"context"
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
