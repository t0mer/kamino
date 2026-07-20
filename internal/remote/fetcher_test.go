package remote_test

import (
	"context"
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
