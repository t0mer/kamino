// Package server exposes Kamino's engine over an authenticated HTTP API.
//
// Handlers here parse a request, delegate to the run manager or the existing
// config/plan packages, and render a response. They contain no install logic:
// anything that decides what to install belongs in internal/plan or
// internal/engine, where it is testable without a socket.
package server

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/t0mer/kamino/internal/engine/runners"
	"github.com/t0mer/kamino/internal/events"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/runmgr"
	"github.com/t0mer/kamino/internal/state"
)

// Deps are the collaborators a Server needs.
type Deps struct {
	DB       *state.Store
	Bus      *events.Bus
	Runs     *runmgr.Manager
	DataDir  string
	APIToken string
	// LoadConfig fetches and parses the configured repo. It is injected so
	// handlers can be tested without a network or a real config repo.
	LoadConfig func(ctx context.Context) (*manifest.Resolved, error)
	// ConfigSource fetches script/stack files from the configured repo for
	// handlers that need to hand them to the engine (Task 8).
	ConfigSource runners.ScriptSource
}

// Server serves the JSON API.
type Server struct {
	d Deps
}

// New builds a server from its dependencies.
func New(d Deps) *Server { return &Server{d: d} }

// Handler returns the router. /healthz is deliberately outside the
// authenticated group so a probe or load balancer can reach it.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)

	r.Get("/healthz", s.handleHealthz)

	r.Route("/api/v1", func(api chi.Router) {
		api.Use(RequireToken(s.d.APIToken))

		api.Get("/system", s.handleSystem)

		api.Get("/settings", s.handleGetSettings)
		api.Put("/settings", s.handlePutSettings)
		api.Post("/settings/test", s.handleTestSettings)

		api.Get("/config", s.handleGetConfig)
		api.Post("/config/refresh", s.handleRefreshConfig)
		api.Post("/plan", s.handlePlan)

		api.Get("/runs", s.handleListRuns)
		api.Post("/runs", s.handleCreateRun)
		api.Get("/runs/{id}", s.handleGetRun)
		api.Get("/runs/{id}/events", s.handleRunEvents)
		api.Post("/runs/{id}/cancel", s.handleCancelRun)
	})

	return r
}

// handleHealthz reports that the process is up. It is unauthenticated by
// design so orchestration can probe it.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
