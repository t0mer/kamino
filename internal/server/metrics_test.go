package server_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/t0mer/kamino/internal/metrics"
	"github.com/t0mer/kamino/internal/server"
)

// metricSeries is a series that is always exported, even before any run:
// a Histogram emits its buckets from creation, whereas the CounterVecs stay
// empty until a run or step is recorded.
const metricSeries = "kamino_step_duration_seconds"

// TestMetricsServedUnauthenticated proves the Prometheus endpoint is reachable
// without the API token, like /healthz, when a handler is supplied.
func TestMetricsServedUnauthenticated(t *testing.T) {
	mx := metrics.New()
	h := server.New(server.Deps{
		DataDir:  t.TempDir(),
		APIToken: testToken,
		Metrics:  mx.Handler(),
	}).Handler()

	rec := doNoToken(t, h, http.MethodGet, "/metrics", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), metricSeries)
}

// TestMetricsNotMountedWhenAbsent proves a nil handler leaves the route
// unmounted rather than panicking: /metrics then falls through to the SPA
// catch-all and never emits Prometheus output. Tests and headless runs never
// set a metrics handler.
func TestMetricsNotMountedWhenAbsent(t *testing.T) {
	h := server.New(server.Deps{DataDir: t.TempDir(), APIToken: testToken}).Handler()

	rec := doNoToken(t, h, http.MethodGet, "/metrics", "")
	assert.NotContains(t, rec.Body.String(), metricSeries)
}
