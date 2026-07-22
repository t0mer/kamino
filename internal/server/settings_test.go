package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/config"
	"github.com/t0mer/kamino/internal/server"
)

const testToken = "test-api-token"

// newTestServer builds a server over a temp data dir.
func newTestServer(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	srv := server.New(server.Deps{
		DataDir:  dir,
		APIToken: testToken,
	})
	return srv.Handler(), dir
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set(server.TokenHeader, testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestHealthzIsOpen(t *testing.T) {
	h, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "probes must reach healthz without a token")
}

func TestGetSystemReportsTheHost(t *testing.T) {
	h, _ := newTestServer(t)

	rec := do(t, h, http.MethodGet, "/api/v1/system", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.NotEmpty(t, got["arch"])
	assert.NotEmpty(t, got["hostname"])
}

func TestGetSettingsOnAFreshInstall(t *testing.T) {
	h, _ := newTestServer(t)

	rec := do(t, h, http.MethodGet, "/api/v1/settings", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, false, got["configured"])
	assert.Equal(t, false, got["has_repo_token"])
}

func TestGetSettingsNeverReturnsEitherToken(t *testing.T) {
	h, dir := newTestServer(t)
	require.NoError(t, config.Save(dir, config.Settings{
		RepoURL:   "https://github.com/t0mer/cfg",
		Ref:       "main",
		RepoToken: "repo-s3cret",
		APIToken:  "api-s3cret",
	}))

	rec := do(t, h, http.MethodGet, "/api/v1/settings", "")

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.NotContains(t, body, "repo-s3cret")
	assert.NotContains(t, body, "api-s3cret")
	assert.Contains(t, body, `"has_repo_token":true`)
}

func TestPutSettingsRejectsAMalformedBody(t *testing.T) {
	h, _ := newTestServer(t)

	rec := do(t, h, http.MethodPut, "/api/v1/settings", "{not json")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPutSettingsRejectsANonHTTPSRepo(t *testing.T) {
	h, dir := newTestServer(t)

	rec := do(t, h, http.MethodPut, "/api/v1/settings",
		`{"repo_url":"http://github.com/t0mer/cfg","ref":"main"}`)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	saved, err := config.Load(dir)
	require.NoError(t, err)
	assert.Empty(t, saved.RepoURL, "a rejected repo must not be persisted")
}

func TestPutSettingsKeepsPreviousSettingsOnFailure(t *testing.T) {
	h, dir := newTestServer(t)
	require.NoError(t, config.Save(dir, config.Settings{
		RepoURL: "https://github.com/t0mer/good", Ref: "main",
	}))

	rec := do(t, h, http.MethodPut, "/api/v1/settings",
		`{"repo_url":"http://bad","ref":"main"}`)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	saved, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/t0mer/good", saved.RepoURL,
		"a failed update must leave the working config in place")
}

func TestPutSettingsPreservesTheAPIToken(t *testing.T) {
	h, dir := newTestServer(t)
	require.NoError(t, config.Save(dir, config.Settings{APIToken: "keep-me"}))

	// A bad repo is fine here; the assertion is about the token surviving.
	_ = do(t, h, http.MethodPut, "/api/v1/settings", `{"repo_url":"http://bad"}`)

	saved, err := config.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "keep-me", saved.APIToken,
		"updating repo settings must not clear the API token the caller is using")
}
