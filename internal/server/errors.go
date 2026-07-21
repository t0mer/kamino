package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// errorBody is the single error shape every endpoint returns, so a client has
// exactly one thing to render.
type errorBody struct {
	Error   string   `json:"error"`
	Details []string `json:"details,omitempty"`
}

// writeJSON renders v as JSON with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already sent, so this can only be logged.
		slog.Warn("writing json response failed", "error", err)
	}
}

// writeError renders the standard error body. details carries the aggregated
// validation problems from a bad config repo, which is the one case where a
// caller needs more than a sentence.
func writeError(w http.ResponseWriter, status int, msg string, details ...string) {
	writeJSON(w, status, errorBody{Error: msg, Details: details})
}
