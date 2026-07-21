package remote_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestRedirectAuthGuardStripsHeaderOnSchemeDowngrade(t *testing.T) {
	// A same-host redirect from https to http is a protocol downgrade to
	// cleartext. The guard used to compare only Host, so this case had an
	// identical Host string and was (wrongly) treated as same-origin,
	// leaving the auth header on the request to be sent in the clear.
	//
	// A genuine end-to-end TLS -> cleartext hop on the *same* host:port is
	// not producible with httptest (or any real listener): a single TCP
	// port either speaks TLS or plaintext HTTP, never both, so two servers
	// sharing an authority string is impossible. Per the task instructions,
	// this instead unit-tests the guard's decision function directly
	// against constructed *http.Request values with differing schemes,
	// via the RedirectAuthGuardForTest test-only export.
	repo, err := remote.ParseRepo("https://gitlab.com/t0mer/cfg", "main", "")
	require.NoError(t, err)
	repo.Token = "s3cret"

	guard := remote.RedirectAuthGuardForTest(repo, nil)

	origURL, err := url.Parse("https://x.example.com/manifest.yaml")
	require.NoError(t, err)
	origReq := &http.Request{URL: origURL}

	redirURL, err := url.Parse("http://x.example.com/manifest.yaml")
	require.NoError(t, err)
	redirReq := &http.Request{URL: redirURL, Header: make(http.Header)}
	redirReq.Header.Set("PRIVATE-TOKEN", "s3cret")
	require.Equal(t, "s3cret", redirReq.Header.Get("PRIVATE-TOKEN"), "sanity check: header must be set before the guard runs")

	err = guard(redirReq, []*http.Request{origReq})

	require.NoError(t, err)
	assert.Empty(t, redirReq.Header.Get("PRIVATE-TOKEN"),
		"an https -> http same-host redirect must strip the auth header, not just a cross-host one")
}

func TestRedirectAuthGuardTreatsPortChangeAsCrossHost(t *testing.T) {
	// Guard against regressing the reviewed fail-closed behavior: a port
	// change on an otherwise identical scheme+hostname must still be
	// treated as cross-host and strip the header.
	repo, err := remote.ParseRepo("https://gitlab.com/t0mer/cfg", "main", "")
	require.NoError(t, err)
	repo.Token = "s3cret"

	guard := remote.RedirectAuthGuardForTest(repo, nil)

	origURL, err := url.Parse("https://x.example.com/manifest.yaml")
	require.NoError(t, err)
	origReq := &http.Request{URL: origURL}

	redirURL, err := url.Parse("https://x.example.com:8443/manifest.yaml")
	require.NoError(t, err)
	redirReq := &http.Request{URL: redirURL, Header: make(http.Header)}
	redirReq.Header.Set("PRIVATE-TOKEN", "s3cret")
	require.Equal(t, "s3cret", redirReq.Header.Get("PRIVATE-TOKEN"), "sanity check: header must be set before the guard runs")

	err = guard(redirReq, []*http.Request{origReq})

	require.NoError(t, err)
	assert.Empty(t, redirReq.Header.Get("PRIVATE-TOKEN"), "a port change must still be treated as cross-host")
}

func TestFetchStripsAuthHeaderOnCrossHostRedirectEvenWhenCallerAllowsIt(t *testing.T) {
	// The guard layers on top of any caller-supplied CheckRedirect: even
	// when the caller's own policy explicitly permits the redirect (returns
	// nil), the guard must still strip the header on a cross-host hop.
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

	client := *origin.Client()
	var callerInvoked bool
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		callerInvoked = true
		return nil // caller explicitly allows the redirect
	}

	f := remote.NewHTTPFetcher(repo, t.TempDir(), &client)
	body, err := f.Fetch(context.Background(), "abc123", "manifest.yaml")

	require.NoError(t, err)
	assert.Equal(t, "schema: 1", string(body))
	assert.True(t, callerInvoked, "the caller's CheckRedirect must still run")
	assert.Equal(t, "s3cret", originGotToken)
	assert.Empty(t, targetGotToken,
		"the guard must strip the header on a cross-host hop even though the caller's CheckRedirect allowed it")
}

func TestFetchNilClientStillGuardsCrossHostRedirect(t *testing.T) {
	// NewHTTPFetcher builds its own default *http.Client when given nil.
	// That default-built client must get the same redirect guard as one
	// supplied by the caller. Proven behaviorally: pass nil and confirm the
	// header still doesn't reach a cross-host redirect target.
	var targetGotToken string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetGotToken = r.Header.Get("PRIVATE-TOKEN")
		_, _ = w.Write([]byte("schema: 1"))
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/redirected", http.StatusFound)
	}))
	defer origin.Close()

	repo, err := remote.ParseRepo("https://gitlab.com/t0mer/cfg", "main",
		origin.URL+"/{ref}/{path}")
	require.NoError(t, err)
	repo.Token = "s3cret"

	f := remote.NewHTTPFetcher(repo, t.TempDir(), nil)
	body, err := f.Fetch(context.Background(), "abc123", "manifest.yaml")

	require.NoError(t, err)
	assert.Equal(t, "schema: 1", string(body))
	assert.Empty(t, targetGotToken, "the fetcher's own default client must still have the redirect guard installed")
}

func TestFetchStopsAfterMaxRedirectsWithNoCallerPolicy(t *testing.T) {
	// Guards against an unbounded follower: with no caller CheckRedirect set,
	// the guard must re-enforce net/http's normal 10-redirect cap rather than
	// following an infinite redirect loop forever.
	var hits int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Redirect(w, r, srv.URL+"/loop", http.StatusFound)
	}))
	defer srv.Close()

	repo, err := remote.ParseRepo("https://github.com/t0mer/cfg", "main",
		srv.URL+"/{ref}/{path}")
	require.NoError(t, err)

	f := remote.NewHTTPFetcher(repo, t.TempDir(), srv.Client())
	_, err = f.Fetch(context.Background(), "abc123", "manifest.yaml")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "stopped after 10 redirects")
	assert.LessOrEqual(t, atomic.LoadInt32(&hits), int32(12),
		"the redirect chain must actually terminate, not run unbounded")
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

// TestFetchAllowsBodyUnderSizeLimit guards against over-rejecting: a body
// comfortably under the cap must still succeed.
func TestFetchAllowsBodyUnderSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("short"))
	}))
	defer srv.Close()

	f := remote.NewHTTPFetcher(testRepo(t, srv), t.TempDir(), srv.Client())
	restore := remote.SetMaxConfigFileSizeForTest(f, 16)
	defer restore()

	body, err := f.Fetch(context.Background(), "abc123", "manifest.yaml")

	require.NoError(t, err)
	assert.Equal(t, "short", string(body))
}

// TestFetchAllowsBodyExactlyAtSizeLimit asserts a body of exactly the cap's
// byte count is accepted, not wrongly rejected as "over". The limit-plus-one
// read is what makes this distinguishable from an over-limit body.
func TestFetchAllowsBodyExactlyAtSizeLimit(t *testing.T) {
	const limit = 16
	body := strings.Repeat("a", limit)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := remote.NewHTTPFetcher(testRepo(t, srv), t.TempDir(), srv.Client())
	restore := remote.SetMaxConfigFileSizeForTest(f, limit)
	defer restore()

	got, err := f.Fetch(context.Background(), "abc123", "manifest.yaml")

	require.NoError(t, err)
	assert.Equal(t, body, string(got))
}

// TestFetchRejectsBodyOverSizeLimit asserts a body one byte over the cap is
// rejected with a clear error naming the file and the limit, rather than
// being silently truncated and parsed as if it were the whole file.
func TestFetchRejectsBodyOverSizeLimit(t *testing.T) {
	const limit = 16
	body := strings.Repeat("a", limit+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	f := remote.NewHTTPFetcher(testRepo(t, srv), t.TempDir(), srv.Client())
	restore := remote.SetMaxConfigFileSizeForTest(f, limit)
	defer restore()

	_, err := f.Fetch(context.Background(), "abc123", "manifest.yaml")

	require.Error(t, err)
	assert.ErrorIs(t, err, remote.ErrTooLarge)
	assert.Contains(t, err.Error(), "manifest.yaml", "error should name the offending file")
	assert.Contains(t, err.Error(), "16", "error should name the enforced limit")
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
