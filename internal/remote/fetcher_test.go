package remote_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/t0mer/kamino/internal/remote"
)

func testRepo(t *testing.T, server *httptest.Server) *remote.Repo {
	t.Helper()
	r, err := remote.ParseRepo("https://github.com/t0mer/cfg", "main",
		server.URL+"/{ref}/{path}")
	require.NoError(t, err)
	return r
}

func TestFetchReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/abc123/manifest.yaml", r.URL.Path)
		_, _ = w.Write([]byte("schema: 1"))
	}))
	defer srv.Close()

	f := remote.NewHTTPFetcher(testRepo(t, srv), t.TempDir(), srv.Client())
	body, err := f.Fetch(context.Background(), "abc123", "manifest.yaml")

	require.NoError(t, err)
	assert.Equal(t, "schema: 1", string(body))
	assert.False(t, f.Stale())
}

func TestFetchCachesInMemory(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = w.Write([]byte("schema: 1"))
	}))
	defer srv.Close()

	f := remote.NewHTTPFetcher(testRepo(t, srv), t.TempDir(), srv.Client())
	for i := 0; i < 3; i++ {
		_, err := f.Fetch(context.Background(), "abc123", "manifest.yaml")
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "should fetch each path once per session")
}

func TestFetchSendsAuthHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	repo := testRepo(t, srv)
	repo.Token = "s3cret"
	f := remote.NewHTTPFetcher(repo, t.TempDir(), srv.Client())
	_, err := f.Fetch(context.Background(), "abc123", "manifest.yaml")

	require.NoError(t, err)
	assert.Equal(t, "token s3cret", got)
}

func TestFetchStripsAuthHeaderOnCrossHostRedirect(t *testing.T) {
	// GitLab authenticates with PRIVATE-TOKEN, a header that is NOT in
	// net/http's hardcoded cross-host-redirect-strip list (that list only
	// covers Authorization/WWW-Authenticate/Cookie/Cookie2). Without the
	// fetcher's own guard, a redirect from the configured host to a
	// different host would leak the operator's token to that third party.
	var targetGotToken string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetGotToken = r.Header.Get("PRIVATE-TOKEN")
		_, _ = w.Write([]byte("schema: 1"))
	}))
	defer target.Close()

	var originGotToken string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originGotToken = r.Header.Get("PRIVATE-TOKEN")
		http.Redirect(w, r, target.URL+"/redirected", http.StatusFound)
	}))
	defer origin.Close()

	repo, err := remote.ParseRepo("https://gitlab.com/t0mer/cfg", "main",
		origin.URL+"/{ref}/{path}")
	require.NoError(t, err)
	repo.Token = "s3cret"

	f := remote.NewHTTPFetcher(repo, t.TempDir(), origin.Client())
	body, err := f.Fetch(context.Background(), "abc123", "manifest.yaml")

	require.NoError(t, err)
	assert.Equal(t, "schema: 1", string(body))
	assert.Equal(t, "s3cret", originGotToken, "the operator-configured host should still receive the token")
	assert.Empty(t, targetGotToken, "a cross-host redirect target must never receive the token")
}

func TestFetchKeepsAuthHeaderOnSameHostRedirect(t *testing.T) {
	var finalGotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/main/manifest.yaml" {
			http.Redirect(w, r, "/main/other.yaml", http.StatusFound)
			return
		}
		finalGotToken = r.Header.Get("PRIVATE-TOKEN")
		_, _ = w.Write([]byte("schema: 1"))
	}))
	defer srv.Close()

	repo, err := remote.ParseRepo("https://gitlab.com/t0mer/cfg", "main",
		srv.URL+"/{ref}/{path}")
	require.NoError(t, err)
	repo.Token = "s3cret"

	f := remote.NewHTTPFetcher(repo, t.TempDir(), srv.Client())
	body, err := f.Fetch(context.Background(), "main", "manifest.yaml")

	require.NoError(t, err)
	assert.Equal(t, "schema: 1", string(body))
	assert.Equal(t, "s3cret", finalGotToken, "a same-host redirect must still carry the auth header")
}

func TestFetchHonorsCallerCheckRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("schema: 1"))
	}))
	defer target.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/redirected", http.StatusFound)
	}))
	defer srv.Close()

	client := *srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("no redirects allowed")
	}

	f := remote.NewHTTPFetcher(testRepo(t, srv), t.TempDir(), &client)
	_, err := f.Fetch(context.Background(), "abc123", "manifest.yaml")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no redirects allowed")
}

func TestFetch404IsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	f := remote.NewHTTPFetcher(testRepo(t, srv), t.TempDir(), srv.Client())
	_, err := f.Fetch(context.Background(), "abc123", "missing.yaml")

	require.ErrorIs(t, err, remote.ErrNotFound)
}

func TestFetchFallsBackToDiskCache(t *testing.T) {
	cacheDir := t.TempDir()
	body := "schema: 1"

	// First fetcher succeeds and mirrors to disk.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	repo := testRepo(t, srv)
	f1 := remote.NewHTTPFetcher(repo, cacheDir, srv.Client())
	_, err := f1.Fetch(context.Background(), "abc123", "manifest.yaml")
	require.NoError(t, err)
	srv.Close()

	// Second fetcher hits a dead server and must serve the mirrored copy.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadRepo := testRepo(t, dead)
	dead.Close()

	f2 := remote.NewHTTPFetcher(deadRepo, cacheDir, dead.Client())
	got, err := f2.Fetch(context.Background(), "abc123", "manifest.yaml")

	require.NoError(t, err)
	assert.Equal(t, body, string(got))
	assert.True(t, f2.Stale(), "serving from cache must mark the session stale")
}

func TestFetchRejectsCachePathTraversal(t *testing.T) {
	// A malicious or misconfigured config repo could reference a path like
	// "../sentinel.txt". The disk cache must never write or read outside its
	// own cache directory, since the fetcher runs as root.
	base := t.TempDir()
	cacheDir := filepath.Join(base, "cache")
	require.NoError(t, os.MkdirAll(cacheDir, 0o755))
	sentinel := filepath.Join(base, "sentinel.txt")
	require.NoError(t, os.WriteFile(sentinel, []byte("safe"), 0o644))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("evil"))
	}))
	defer srv.Close()

	f := remote.NewHTTPFetcher(testRepo(t, srv), cacheDir, srv.Client())
	body, err := f.Fetch(context.Background(), "..", "sentinel.txt")

	// The fetch itself still succeeds (network is authoritative); only the
	// cache mirror step is blocked.
	require.NoError(t, err)
	assert.Equal(t, "evil", string(body))

	got, err := os.ReadFile(sentinel)
	require.NoError(t, err)
	assert.Equal(t, "safe", string(got), "cache write must not escape the cache dir")
}

func TestFetchServerErrorWithoutCacheFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := remote.NewHTTPFetcher(testRepo(t, srv), t.TempDir(), srv.Client())
	_, err := f.Fetch(context.Background(), "abc123", "manifest.yaml")

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "500"))
}
