package server

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"

	"github.com/t0mer/kamino/internal/manifest"
	"github.com/t0mer/kamino/internal/plan"
)

// configItem is one installable item as the UI needs it.
type configItem struct {
	Ref       string   `json:"ref"`
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	Version   string   `json:"version,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"`
	Secrets   []string `json:"secrets,omitempty"`
}

type configCategory struct {
	ID    string       `json:"id"`
	Name  string       `json:"name"`
	Order int          `json:"order"`
	Items []configItem `json:"items"`
}

type configProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type configResponse struct {
	Name  string `json:"name"`
	SHA   string `json:"sha"`
	Stale bool   `json:"stale"`
	// FetchedAt is when this config was retrieved. The UI pairs it with Stale
	// to show "stale config (SHA …, fetched …)" when a fetch fell back to the
	// cache, so an operator knows how old the repo they are about to install
	// from actually is.
	FetchedAt  string           `json:"fetched_at,omitempty"`
	Categories []configCategory `json:"categories"`
	Profiles   []configProfile  `json:"profiles"`
	Warnings   []string         `json:"warnings,omitempty"`
}

// handleGetConfig returns the resolved config repo: categories, items, and
// profiles, plus the SHA the UI must display so an operator always knows
// which version of their repo a plan will be built from.
func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	resolved, err := s.d.LoadConfig(r.Context())
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "loading config repo: "+err.Error())
		return
	}

	problems := manifest.Validate(resolved)
	if problems.HasErrors() {
		msgs := make([]string, 0, len(problems.Errors()))
		for _, p := range problems.Errors() {
			msgs = append(msgs, p.String())
		}
		writeError(w, http.StatusUnprocessableEntity, "config repo is invalid", msgs...)
		return
	}

	out := configResponse{
		Name:  resolved.Manifest.Name,
		SHA:   resolved.SHA,
		Stale: resolved.Stale,
	}
	if !resolved.FetchedAt.IsZero() {
		out.FetchedAt = resolved.FetchedAt.UTC().Format(time.RFC3339)
	}
	for _, w := range problems.Warnings() {
		out.Warnings = append(out.Warnings, w.String())
	}
	for _, c := range resolved.Categories {
		cat := configCategory{ID: c.ID, Name: c.Name, Order: c.Order}
		for _, it := range c.Items {
			cat.Items = append(cat.Items, configItem{
				Ref: it.Ref(), ID: it.ID, Name: it.Name, Type: string(it.Type),
				Version: it.Version, DependsOn: it.DependsOn, Secrets: it.Secrets,
			})
		}
		out.Categories = append(out.Categories, cat)
	}
	for _, p := range resolved.Profiles {
		out.Profiles = append(out.Profiles, configProfile{ID: p.ID, Name: p.Name})
	}

	writeJSON(w, http.StatusOK, out)
}

// handleRefreshConfig re-fetches the repo. LoadConfig already performs a fetch
// on every call, so this differs from GET only in intent — it exists so the UI
// has an explicit action rather than relying on a cache-busting GET.
func (s *Server) handleRefreshConfig(w http.ResponseWriter, r *http.Request) {
	s.handleGetConfig(w, r)
}

type planRequest struct {
	Profile string `json:"profile"`
	Arch    string `json:"arch"`
}

type planStep struct {
	Ref      string `json:"ref"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Version  string `json:"version,omitempty"`
	Implicit bool   `json:"implicit,omitempty"`
}

type planResponse struct {
	Profile   string     `json:"profile"`
	Arch      string     `json:"arch"`
	ConfigSHA string     `json:"config_sha"`
	Stale     bool       `json:"stale"`
	Steps     []planStep `json:"steps"`
	Warnings  []string   `json:"warnings,omitempty"`
	Secrets   []string   `json:"secrets,omitempty"`
}

// handlePlan resolves a profile into an ordered, executable plan.
//
// The response always carries warnings alongside the steps: they are the
// only place a browser operator will see "no sha256, unverified download"
// or "runs a shell script as root" before authorising a run, so they must
// never be computed and then dropped on the way out.
func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	var req planRequest
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

	out := planResponse{
		Profile:   built.ProfileID,
		Arch:      built.Arch,
		ConfigSHA: built.ConfigSHA,
		Stale:     built.Stale,
		Warnings:  built.Warnings,
	}
	seen := map[string]bool{}
	for _, st := range built.Steps {
		out.Steps = append(out.Steps, planStep{
			Ref: st.Ref, Name: st.Name, Type: st.Type,
			Version: st.Version, Implicit: st.Implicit,
		})
		for _, name := range st.Item.Secrets {
			if !seen[name] {
				seen[name] = true
				out.Secrets = append(out.Secrets, name)
			}
		}
	}

	writeJSON(w, http.StatusOK, out)
}

// findProfile looks a profile up by id.
func findProfile(r *manifest.Resolved, id string) (manifest.Profile, bool) {
	for _, p := range r.Profiles {
		if p.ID == id {
			return p, true
		}
	}
	return manifest.Profile{}, false
}
