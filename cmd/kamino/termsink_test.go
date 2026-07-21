package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/t0mer/kamino/internal/state"
)

func TestTermSinkPrintsProgressCounter(t *testing.T) {
	var buf bytes.Buffer
	names := map[string]string{"tools/docker": "Docker Engine"}
	sink := newTermSink(&buf, 2, names, false)

	sink.StepStatus("run-1", "tools/docker", state.StatusRunning)
	sink.StepStatus("run-1", "tools/docker", state.StatusSuccess)

	out := buf.String()
	assert.Contains(t, out, "[1/2] Docker Engine")
	assert.Contains(t, out, "Docker Engine — done")
}

func TestTermSinkFallsBackToRefWhenNameUnknown(t *testing.T) {
	var buf bytes.Buffer
	sink := newTermSink(&buf, 1, map[string]string{}, false)

	sink.StepStatus("run-1", "tools/jq", state.StatusFailed)

	assert.Contains(t, buf.String(), "tools/jq — FAILED")
}

func TestTermSinkLogLineOnlyPrintsWhenVerbose(t *testing.T) {
	var quiet, verbose bytes.Buffer
	newTermSink(&quiet, 1, nil, false).LogLine("run-1", "tools/jq", "stdout", "installing jq")
	newTermSink(&verbose, 1, nil, true).LogLine("run-1", "tools/jq", "stdout", "installing jq")

	assert.Empty(t, quiet.String())
	assert.Contains(t, verbose.String(), "installing jq")
}
