package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/t0mer/kamino/internal/server"
)

// TestCatchAllDoesNotShadowTheAPI proves that mounting the SPA catch-all leaves
// the API and healthz reachable. Before a frontend build there is no SPA to
// serve, so an unknown UI route 404s — but /api and /healthz must be unchanged.
func TestCatchAllLeavesAPIAndHealthzIntact(t *testing.T) {
	h := server.New(server.Deps{DataDir: t.TempDir(), APIToken: testToken}).Handler()

	// healthz still open
	assertStatus(t, h, http.MethodGet, "/healthz", "", http.StatusOK)
	// api still requires a token (401 without, not a SPA 200)
	assertStatus(t, h, http.MethodGet, "/api/v1/system", "", http.StatusUnauthorized)
}

func assertStatus(t *testing.T, h http.Handler, method, path, body string, want int) {
	t.Helper()
	rec := doNoToken(t, h, method, path, body)
	assert.Equal(t, want, rec.Code)
}

func doNoToken(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}
