package remote_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/remote"
)

func TestAPIPinnerResolvesGitHubBranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/t0mer/cfg/commits/main", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sha":"abc123def456"}`))
	}))
	defer srv.Close()

	repo, err := remote.ParseRepo("https://github.com/t0mer/cfg", "main", "")
	require.NoError(t, err)

	p := remote.NewAPIPinner(srv.Client(), srv.URL)
	sha, err := p.Pin(context.Background(), repo)

	require.NoError(t, err)
	assert.Equal(t, "abc123def456", sha)
}

func TestAPIPinnerSendsAuthHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"sha":"abc123"}`))
	}))
	defer srv.Close()

	repo, err := remote.ParseRepo("https://github.com/t0mer/cfg", "main", "")
	require.NoError(t, err)
	repo.Token = "s3cret"

	_, err = remote.NewAPIPinner(srv.Client(), srv.URL).Pin(context.Background(), repo)

	require.NoError(t, err)
	assert.Equal(t, "token s3cret", got)
}

func TestAPIPinnerGenericProviderReturnsRef(t *testing.T) {
	repo, err := remote.ParseRepo("https://git.example.com/t0mer/cfg", "main", "https://x/{ref}/{path}")
	require.NoError(t, err)

	sha, err := remote.NewAPIPinner(nil, "").Pin(context.Background(), repo)

	require.NoError(t, err)
	assert.Equal(t, "main", sha, "generic hosts have no SHA API; the ref is used as-is")
}

func TestAPIPinnerErrorsOnBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	repo, _ := remote.ParseRepo("https://github.com/t0mer/cfg", "main", "")
	_, err := remote.NewAPIPinner(srv.Client(), srv.URL).Pin(context.Background(), repo)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
