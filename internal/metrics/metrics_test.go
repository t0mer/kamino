package metrics_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/metrics"
	"github.com/t0mer/kamino/internal/state"
)

func TestRunFinishedCountsByStatus(t *testing.T) {
	m := metrics.New()
	m.RunFinished(state.StatusSuccess)
	m.RunFinished(state.StatusSuccess)
	m.RunFinished(state.StatusFailed)

	body := scrape(t, m.Handler())
	assert.Contains(t, body, `kamino_runs_total{status="success"} 2`)
	assert.Contains(t, body, `kamino_runs_total{status="failed"} 1`)
}

func TestStepSinkCountsTerminalStatusesNotRunning(t *testing.T) {
	m := metrics.New()
	sink := m.NewStepSink()

	// A success step: running then success. Running must not be counted.
	sink.StepStatus("run", "dev/go", state.StatusRunning, 0)
	sink.StepStatus("run", "dev/go", state.StatusSuccess, 0)
	// A skipped step, and a blocked step that never ran.
	sink.StepStatus("run", "dev/python", state.StatusRunning, 0)
	sink.StepStatus("run", "dev/python", state.StatusSkipped, 0)
	sink.StepStatus("run", "dev/pip", state.StatusBlocked, 0)

	body := scrape(t, m.Handler())
	assert.Contains(t, body, `kamino_steps_total{status="success"} 1`)
	assert.Contains(t, body, `kamino_steps_total{status="skipped"} 1`)
	assert.Contains(t, body, `kamino_steps_total{status="blocked"} 1`)
	assert.NotContains(t, body, `status="running"`)
}

func TestStepSinkObservesDurationOnlyForStepsThatRan(t *testing.T) {
	m := metrics.New()
	sink := m.NewStepSink()

	// Two steps that ran (running -> terminal) and one that never did.
	sink.StepStatus("run", "a", state.StatusRunning, 0)
	sink.StepStatus("run", "a", state.StatusSuccess, 0)
	sink.StepStatus("run", "b", state.StatusRunning, 0)
	sink.StepStatus("run", "b", state.StatusFailed, 1)
	sink.StepStatus("run", "c", state.StatusBlocked, 0)

	// The histogram's sample count is the number of steps that actually ran.
	body := scrape(t, m.Handler())
	assert.Contains(t, body, `kamino_step_duration_seconds_count 2`)
}

func TestLogLineIsInert(t *testing.T) {
	m := metrics.New()
	sink := m.NewStepSink()
	sink.LogLine("run", "a", "stdout", "hello")

	// A CounterVec never touched emits no series, so no value line appears.
	body := scrape(t, m.Handler())
	assert.NotContains(t, body, "kamino_steps_total{")
}

func scrape(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	b, err := io.ReadAll(rec.Result().Body)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain"),
		"prometheus text exposition, got %q", rec.Header().Get("Content-Type"))
	return string(b)
}
