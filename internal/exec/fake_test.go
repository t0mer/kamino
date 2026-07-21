package exec_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kexec "github.com/t0mer/kamino/internal/exec"
)

func TestFakeReturnsScriptedResult(t *testing.T) {
	f := kexec.NewFakeExecutor()
	f.Script("docker --version", kexec.Result{ExitCode: 0, Stdout: []string{"Docker version 27.5"}})

	got, err := f.Run(context.Background(), kexec.Shell("docker --version"), nil)

	require.NoError(t, err)
	assert.Equal(t, []string{"Docker version 27.5"}, got.Stdout)
}

func TestFakeDefaultsToSuccess(t *testing.T) {
	f := kexec.NewFakeExecutor()

	got, err := f.Run(context.Background(), kexec.Shell("anything at all"), nil)

	require.NoError(t, err)
	assert.Equal(t, 0, got.ExitCode)
}

func TestFakeRecordsCommandLines(t *testing.T) {
	f := kexec.NewFakeExecutor()

	_, _ = f.Run(context.Background(), kexec.Shell("apt-get install -y jq"), nil)
	_, _ = f.Run(context.Background(), kexec.Shell("jq --version"), nil)

	assert.Equal(t, []string{
		"/bin/sh -c apt-get install -y jq",
		"/bin/sh -c jq --version",
	}, f.CommandLines())
}

func TestFakeScriptedError(t *testing.T) {
	f := kexec.NewFakeExecutor()
	f.ScriptErr("sleep", errors.New("timed out"))

	_, err := f.Run(context.Background(), kexec.Shell("sleep 10"), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestFakeStreamsToLineSink(t *testing.T) {
	f := kexec.NewFakeExecutor()
	f.Script("go version", kexec.Result{Stdout: []string{"go version go1.24.5"}})

	var lines []string
	_, err := f.Run(context.Background(), kexec.Shell("go version"), func(_, line string) {
		lines = append(lines, line)
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"go version go1.24.5"}, lines)
}

func TestFakeFirstMatchWins(t *testing.T) {
	f := kexec.NewFakeExecutor()
	f.Script("apt-get", kexec.Result{ExitCode: 1})
	f.Script("apt-get install", kexec.Result{ExitCode: 0})

	got, err := f.Run(context.Background(), kexec.Shell("apt-get install -y jq"), nil)

	require.NoError(t, err)
	assert.Equal(t, 1, got.ExitCode, "the first registered match wins")
}
