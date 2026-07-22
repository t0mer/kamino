package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"runtime"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/t0mer/kamino/internal/plan"
	"github.com/t0mer/kamino/internal/runmgr"
	"github.com/t0mer/kamino/internal/secrets"
	"github.com/t0mer/kamino/internal/state"
)

type createRunRequest struct {
	Profile         string            `json:"profile"`
	Arch            string            `json:"arch"`
	Secrets         map[string]string `json:"secrets"`
	ContinueOnError bool              `json:"continue_on_error"`
}

type runSummary struct {
	ID         string `json:"id"`
	Profile    string `json:"profile"`
	ConfigSHA  string `json:"config_sha"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type runStep struct {
	ID       string `json:"id"`
	ItemRef  string `json:"item_ref"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	ExitCode int    `json:"exit_code"`
}

type runDetail struct {
	runSummary
	Steps []runStep `json:"steps"`
}

// handleCreateRun starts an install. It returns as soon as the run exists;
// progress is watched over SSE.
func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	var req createRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body")
		return
	}
	if req.Arch == "" {
		req.Arch = runtime.GOARCH
	}

	resolved, err := s.d.LoadConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "loading config repo: "+err.Error())
		return
	}

	profile, ok := findProfile(resolved, req.Profile)
	if !ok {
		writeError(w, http.StatusNotFound, "profile "+req.Profile+" not found")
		return
	}

	built, err := plan.Build(resolved, profile, req.Arch)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// Register every secret before the engine is constructed: the redactor
	// snapshots the store, so a value added later would never be masked and
	// would reach sqlite in plaintext.
	store := secrets.New()
	for k, v := range req.Secrets {
		store.Set(k, v)
	}

	var declared []string
	for _, st := range built.Steps {
		declared = append(declared, st.Item.Secrets...)
	}
	// Fail before touching the machine. Discovering a missing secret mid-run
	// leaves a half-provisioned host.
	if missing := secrets.Missing(declared, store); len(missing) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "missing required secrets", missing...)
		return
	}

	runID, err := s.d.Runs.Start(r.Context(), runmgr.StartRequest{
		Plan:            built,
		Resolved:        resolved,
		Secrets:         store,
		ContinueOnError: req.ContinueOnError,
		ConfigSource:    s.d.ConfigSource,
		Exec:            s.d.Exec,
		Download:        s.d.Download,
	})
	var busy *runmgr.RunInProgressError
	if errors.As(err, &busy) {
		writeError(w, http.StatusConflict, "a run is already in progress", busy.ActiveID)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "starting run: "+err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"run_id": runID})
}

func (s *Server) handleListRuns(w http.ResponseWriter, _ *http.Request) {
	runs, err := s.d.DB.ListRuns(50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing runs: "+err.Error())
		return
	}
	out := make([]runSummary, 0, len(runs))
	for _, r := range runs {
		out = append(out, toSummary(r))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	run, steps, err := s.d.DB.GetRun(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "run "+id+" not found")
		return
	}

	detail := runDetail{runSummary: toSummary(run)}
	for _, st := range steps {
		detail.Steps = append(detail.Steps, runStep{
			ID: st.ID, ItemRef: st.ItemRef, Name: st.Name,
			Status: string(st.Status), ExitCode: st.ExitCode,
		})
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.d.Runs.Cancel(id); err != nil {
		writeError(w, http.StatusNotFound, "run "+id+" is not running")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"run_id": id, "status": "cancelling"})
}

// toSummary renders a stored run for the API. Times are formatted with
// time.RFC3339 (matching handleGetConfig in config.go) rather than a
// package-local layout constant, so the two handlers don't carry two copies
// of the same format string.
func toSummary(r state.Run) runSummary {
	out := runSummary{
		ID: r.ID, Profile: r.Profile, ConfigSHA: r.ConfigSHA,
		Status: string(r.Status), StartedAt: r.StartedAt.UTC().Format(time.RFC3339),
	}
	if r.FinishedAt != nil {
		out.FinishedAt = r.FinishedAt.UTC().Format(time.RFC3339)
	}
	return out
}
