package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/server"
)

func protected(t *testing.T, token string) http.Handler {
	t.Helper()
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return server.RequireToken(token)(inner)
}

func TestRequireTokenAcceptsTheCorrectToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system", nil)
	req.Header.Set("X-API-Token", "s3cret")
	rec := httptest.NewRecorder()

	protected(t, "s3cret").ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRequireTokenRejectsAWrongToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system", nil)
	req.Header.Set("X-API-Token", "wrong")
	rec := httptest.NewRecorder()

	protected(t, "s3cret").ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireTokenRejectsAMissingToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system", nil)
	rec := httptest.NewRecorder()

	protected(t, "s3cret").ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireTokenNeverEchoesTheToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system", nil)
	req.Header.Set("X-API-Token", "wrong")
	rec := httptest.NewRecorder()

	protected(t, "sup3rs3cret").ServeHTTP(rec, req)

	assert.NotContains(t, rec.Body.String(), "sup3rs3cret",
		"a rejection must not leak the expected token")
	assert.NotContains(t, rec.Body.String(), "wrong",
		"a rejection must not echo the supplied token either")
}

func TestRequireTokenRejectsAPrefixOfTheToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system", nil)
	req.Header.Set("X-API-Token", "s3c")
	rec := httptest.NewRecorder()

	protected(t, "s3cret").ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireTokenFailsClosedWhenConfiguredTokenIsEmpty(t *testing.T) {
	// A misconfigured empty expected token must never mean "no auth
	// required": subtle.ConstantTimeCompare treats two empty slices as
	// equal, so without an explicit guard an empty-header request would
	// otherwise sail through unauthenticated.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system", nil)
	rec := httptest.NewRecorder()

	protected(t, "").ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
