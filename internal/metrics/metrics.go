// Package metrics exposes Kamino's Prometheus instrumentation.
//
// It owns a private registry rather than the global default one, so nothing
// leaks in from a dependency's init() and two Metrics values (as tests build)
// never fight over the same collector registration. The engine reaches these
// counters through a Sink added to the run's MultiSink, so instrumentation is
// just another observer of the same step transitions the database and the
// event bus already see — no separate code path that could drift from reality.
package metrics

import (
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/t0mer/kamino/internal/state"
)

// Metrics holds the collectors and the registry serving them.
type Metrics struct {
	reg *prometheus.Registry

	runsTotal    *prometheus.CounterVec
	stepsTotal   *prometheus.CounterVec
	stepDuration prometheus.Histogram
}

// New builds the collectors and registers them on a fresh registry.
func New() *Metrics {
	m := &Metrics{
		reg: prometheus.NewRegistry(),
		runsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kamino_runs_total",
			Help: "Total install runs that reached a terminal status, by status.",
		}, []string{"status"}),
		stepsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "kamino_steps_total",
			Help: "Total step transitions to a terminal status, by status.",
		}, []string{"status"}),
		stepDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "kamino_step_duration_seconds",
			Help: "Wall-clock duration of steps that ran, from running to terminal.",
			// Installs span sub-second checks (already-installed skips) to
			// multi-minute apt/tarball fetches, so the buckets stretch from
			// 100ms out past a minute.
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		}),
	}
	m.reg.MustRegister(m.runsTotal, m.stepsTotal, m.stepDuration)
	return m
}

// Handler serves the registry in the Prometheus text exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// RunFinished records a run reaching a terminal status.
func (m *Metrics) RunFinished(status state.Status) {
	m.runsTotal.WithLabelValues(string(status)).Inc()
}

// NewStepSink returns a Sink that records step counters and durations for one
// run. Each run gets its own so the running-start timestamps it holds cannot
// collide across concurrent runs — though only one run executes at a time
// today, the sink does not assume it.
func (m *Metrics) NewStepSink() *Sink {
	return &Sink{m: m, starts: map[string]time.Time{}}
}

// Sink observes step transitions to record counters and durations. It
// satisfies engine.Sink structurally; the engine only ever holds it behind
// that interface.
type Sink struct {
	m *Metrics

	mu     sync.Mutex
	starts map[string]time.Time
}

// StepStatus records the transition. On running it stamps a start time; on any
// terminal status it counts the step and, if a start was seen, observes the
// elapsed duration. Steps that never ran (blocked, or cancelled before start)
// are counted without a duration — there is nothing to time.
func (s *Sink) StepStatus(_, stepRef string, status state.Status, _ int) {
	if status == state.StatusRunning {
		s.mu.Lock()
		s.starts[stepRef] = time.Now()
		s.mu.Unlock()
		return
	}

	s.m.stepsTotal.WithLabelValues(string(status)).Inc()

	s.mu.Lock()
	start, ok := s.starts[stepRef]
	delete(s.starts, stepRef)
	s.mu.Unlock()
	if ok {
		s.m.stepDuration.Observe(time.Since(start).Seconds())
	}
}

// LogLine is required by engine.Sink but carries nothing this package meters.
func (s *Sink) LogLine(_, _, _, _ string) {}
