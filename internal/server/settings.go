package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/t0mer/kamino/internal/config"
	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/remote"
)

// settingsResponse is what GET /settings returns. Neither the repo token nor
// the API token appears: the repo token is reported only as a boolean, and the
// API token not at all, since a caller that reached this handler already holds
// it.
type settingsResponse struct {
	RepoURL         string `json:"repo_url"`
	Ref             string `json:"ref"`
	RawBaseTemplate string `json:"raw_base_template,omitempty"`
	HasRepoToken    bool   `json:"has_repo_token"`
	Configured      bool   `json:"configured"`
}

// settingsRequest is the accepted body for PUT /settings and POST
// /settings/test. An absent repo_token on PUT keeps the stored one, so a UI
// that never receives the token can still save an unrelated change.
type settingsRequest struct {
	RepoURL         string  `json:"repo_url"`
	Ref             string  `json:"ref"`
	RepoToken       *string `json:"repo_token"`
	RawBaseTemplate string  `json:"raw_base_template"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	saved, err := config.Load(s.d.DataDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading settings: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse{
		RepoURL:         saved.RepoURL,
		Ref:             saved.Ref,
		RawBaseTemplate: saved.RawBaseTemplate,
		HasRepoToken:    saved.HasRepoToken(),
		Configured:      saved.Configured(),
	})
}

// handlePutSettings validates a candidate config repo by actually fetching it
// before persisting. A repo that cannot be fetched or does not parse is
// rejected and the previous settings are left untouched — an operator who
// typos a URL must not lose the working one.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body")
		return
	}

	saved, err := config.Load(s.d.DataDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading settings: "+err.Error())
		return
	}

	candidate := saved
	candidate.RepoURL = req.RepoURL
	candidate.Ref = req.Ref
	candidate.RawBaseTemplate = req.RawBaseTemplate
	if req.RepoToken != nil {
		candidate.RepoToken = *req.RepoToken
	}

	if _, problems, err := probeRepo(r.Context(), candidate); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	} else if len(problems) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "config repo is invalid", problems...)
		return
	}

	if err := config.Save(s.d.DataDir, candidate); err != nil {
		writeError(w, http.StatusInternalServerError, "saving settings: "+err.Error())
		return
	}
	s.handleGetSettings(w, r)
}

// testResponse is what POST /settings/test reports back, so the Connect screen
// can show what it found before the operator commits.
type testResponse struct {
	OK         bool   `json:"ok"`
	Name       string `json:"name"`
	Categories int    `json:"categories"`
	Profiles   int    `json:"profiles"`
	SHA        string `json:"sha"`
}

func (s *Server) handleTestSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request body")
		return
	}

	candidate := config.Settings{
		RepoURL:         req.RepoURL,
		Ref:             req.Ref,
		RawBaseTemplate: req.RawBaseTemplate,
	}
	if req.RepoToken != nil {
		candidate.RepoToken = *req.RepoToken
	}

	resolved, problems, err := probeRepo(r.Context(), candidate)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(problems) > 0 {
		writeError(w, http.StatusUnprocessableEntity, "config repo is invalid", problems...)
		return
	}

	writeJSON(w, http.StatusOK, testResponse{
		OK:         true,
		Name:       resolved.Manifest.Name,
		Categories: len(resolved.Categories),
		Profiles:   len(resolved.Profiles),
		SHA:        resolved.SHA,
	})
}

// probeRepo fetches and validates a candidate config repo without persisting
// anything. It returns the resolved config, the aggregated validation problems
// (empty when valid), and an error for a failure to reach or parse the repo at
// all.
func probeRepo(ctx context.Context, s config.Settings) (*manifest.Resolved, []string, error) {
	repo, err := remote.ParseRepo(s.RepoURL, s.Ref, s.RawBaseTemplate)
	if err != nil {
		return nil, nil, err
	}
	repo.Token = s.RepoToken

	sha, err := remote.NewAPIPinner(nil, "").Pin(ctx, repo)
	if err != nil {
		return nil, nil, err
	}

	fetcher := remote.NewHTTPFetcher(repo, filepath.Join(cacheDirFor(s), "cache"), nil)
	resolved, err := remote.Load(ctx, fetcher, sha)
	if err != nil {
		return nil, nil, err
	}

	problems := manifest.Validate(resolved)
	msgs := make([]string, 0, len(problems.Errors()))
	for _, p := range problems.Errors() {
		msgs = append(msgs, p.String())
	}
	return resolved, msgs, nil
}

// cacheDirFor returns a scratch cache location for a probe. A candidate repo
// that is about to be rejected must not pollute the live cache, so probes use
// a temp dir the OS reclaims.
func cacheDirFor(config.Settings) string { return os.TempDir() }
