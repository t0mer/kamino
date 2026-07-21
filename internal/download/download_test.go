package download_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/t0mer/kamino/internal/download"
)

func sum(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}

func TestFetchWritesFile(t *testing.T) {
	body := "binary content"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact.tar.gz")
	d := download.NewHTTPDownloader(srv.Client())

	err := d.Fetch(context.Background(), srv.URL, dest, sum(body))

	require.NoError(t, err)
	got, readErr := os.ReadFile(dest)
	require.NoError(t, readErr)
	assert.Equal(t, body, string(got))
}

func TestFetchVerifiesChecksum(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("actual content"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	d := download.NewHTTPDownloader(srv.Client())

	err := d.Fetch(context.Background(), srv.URL, dest, sum("different content"))

	require.ErrorIs(t, err, download.ErrChecksumMismatch)
	assert.NoFileExists(t, dest, "a file that failed verification must not be left behind")
	assert.NoFileExists(t, dest+".tmp", "the temp file must be cleaned up on verification failure")
}

func TestFetchSkipsVerificationWhenNoChecksum(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("unverified"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	err := download.NewHTTPDownloader(srv.Client()).Fetch(context.Background(), srv.URL, dest, "")

	require.NoError(t, err)
	assert.FileExists(t, dest)
}

// TestFetchRefusesPlainHTTP asserts the https-only check runs before any
// network request is made: it points Fetch at a real, listening plain-http
// server and confirms the server never receives a request.
func TestFetchRefusesPlainHTTP(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	err := download.NewHTTPDownloader(srv.Client()).Fetch(context.Background(), srv.URL, dest, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
	assert.Zero(t, atomic.LoadInt32(&requests), "no request should reach the server for a non-https URL")
	assert.NoFileExists(t, dest)
}

func TestFetchErrorsOnBadStatus(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	err := download.NewHTTPDownloader(srv.Client()).Fetch(context.Background(), srv.URL, dest, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.NoFileExists(t, dest)
}

// TestFetchCleansUpTempFileOnRenameFailure constructs a rename failure by
// pointing dest at an existing directory: the final os.Rename(tmp, dest)
// fails because a file cannot be renamed onto a directory, even though the
// download and checksum verification both succeeded. The temp file must not
// survive that failure.
func TestFetchCleansUpTempFileOnRenameFailure(t *testing.T) {
	body := "binary content"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	require.NoError(t, os.Mkdir(dest, 0o755))

	err := download.NewHTTPDownloader(srv.Client()).Fetch(context.Background(), srv.URL, dest, sum(body))

	require.Error(t, err)
	assert.NoFileExists(t, dest+".tmp", "the temp file must be cleaned up when the final rename fails")
}

// TestFetchRefusesRedirectToPlainHTTP asserts that an https URL redirecting
// to a plain-http Location header fails the download rather than silently
// fetching over cleartext. The initial-URL check alone cannot catch this:
// net/http follows redirects automatically, so without a CheckRedirect guard
// this would return err == nil after downloading from the http target.
func TestFetchRefusesRedirectToPlainHTTP(t *testing.T) {
	var requests int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer plain.Close()

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	defer secure.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	err := download.NewHTTPDownloader(secure.Client()).Fetch(context.Background(), secure.URL, dest, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "https", "error should name the enforced scheme")
	assert.Contains(t, err.Error(), plain.URL, "error should name the offending redirect target")
	assert.Zero(t, atomic.LoadInt32(&requests), "the plain-http redirect target must never be requested")
	assert.NoFileExists(t, dest)
	assert.NoFileExists(t, dest+".tmp", "no orphaned temp file should remain after a rejected redirect")
}

// TestFetchAllowsHTTPSToHTTPSRedirect guards against over-rejecting: a
// legitimate https-to-https redirect (as real download hosts issue
// constantly, e.g. to a CDN URL) must still succeed and write the correct
// content.
func TestFetchAllowsHTTPSToHTTPSRedirect(t *testing.T) {
	body := "redirected content"
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	err := download.NewHTTPDownloader(srv.Client()).Fetch(context.Background(), srv.URL+"/start", dest, sum(body))

	require.NoError(t, err)
	got, readErr := os.ReadFile(dest)
	require.NoError(t, readErr)
	assert.Equal(t, body, string(got))
}

// TestFetchNilClientRejectsRedirectDowngrade proves, by observed behavior
// rather than by reading the source, that NewHTTPDownloader(nil) — the
// production path used whenever callers don't supply their own client — gets
// the same https-redirect protection as an explicit client. It swaps
// http.DefaultTransport for the duration of the test so the nil-built
// client's TLS handshake trusts the test server's certificate; that is the
// only way to control the client's TLS trust store without passing one in.
func TestFetchNilClientRejectsRedirectDowngrade(t *testing.T) {
	var requests int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer plain.Close()

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL, http.StatusFound)
	}))
	defer secure.Close()

	originalTransport := http.DefaultTransport
	http.DefaultTransport = secure.Client().Transport
	defer func() { http.DefaultTransport = originalTransport }()

	dest := filepath.Join(t.TempDir(), "artifact")
	err := download.NewHTTPDownloader(nil).Fetch(context.Background(), secure.URL, dest, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "https")
	assert.Zero(t, atomic.LoadInt32(&requests), "the plain-http redirect target must never be requested")
	assert.NoFileExists(t, dest)
	assert.NoFileExists(t, dest+".tmp")
}

// TestFetchHonorsCallerCheckRedirect asserts that a caller-supplied
// CheckRedirect still runs and its decision is respected: a policy that
// refuses every redirect must make the fetch fail, and the redirect target
// must never be reached.
func TestFetchHonorsCallerCheckRedirect(t *testing.T) {
	var followed int32
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&followed, 1)
		_, _ = w.Write([]byte("ok"))
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	refuseAll := errors.New("caller refuses all redirects")
	client := srv.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return refuseAll
	}

	dest := filepath.Join(t.TempDir(), "artifact")
	err := download.NewHTTPDownloader(client).Fetch(context.Background(), srv.URL+"/start", dest, "")

	require.Error(t, err)
	assert.ErrorIs(t, err, refuseAll, "the caller's own CheckRedirect decision must be honored")
	assert.Zero(t, atomic.LoadInt32(&followed), "the redirect target must never be reached once the caller refuses it")
	assert.NoFileExists(t, dest)
}

// TestFetchAllowsArtifactUnderSizeLimit guards against over-rejecting: an
// artifact comfortably under the cap must still succeed and be written to
// disk in full.
func TestFetchAllowsArtifactUnderSizeLimit(t *testing.T) {
	body := "short artifact"
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	d := download.NewHTTPDownloader(srv.Client())
	restore := download.SetMaxSizeForTest(d, 32)
	defer restore()

	err := d.Fetch(context.Background(), srv.URL, dest, sum(body))

	require.NoError(t, err)
	got, readErr := os.ReadFile(dest)
	require.NoError(t, readErr)
	assert.Equal(t, body, string(got))
}

// TestFetchAllowsArtifactExactlyAtSizeLimit asserts an artifact of exactly
// the cap's byte count is accepted, not wrongly rejected as "over". The
// limit-plus-one read is what makes this distinguishable from an over-limit
// artifact.
func TestFetchAllowsArtifactExactlyAtSizeLimit(t *testing.T) {
	const limit = 32
	body := strings.Repeat("b", limit)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	d := download.NewHTTPDownloader(srv.Client())
	restore := download.SetMaxSizeForTest(d, limit)
	defer restore()

	err := d.Fetch(context.Background(), srv.URL, dest, sum(body))

	require.NoError(t, err)
	got, readErr := os.ReadFile(dest)
	require.NoError(t, readErr)
	assert.Equal(t, body, string(got))
}

// TestFetchRejectsArtifactOverSizeLimit asserts an artifact one byte over the
// cap is rejected with a clear error naming the URL and the limit, and that
// neither the destination file nor its .tmp survives — matching every other
// failure path in Fetch, which never leaves a partial file at dest.
func TestFetchRejectsArtifactOverSizeLimit(t *testing.T) {
	const limit = 32
	body := strings.Repeat("b", limit+1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact")
	d := download.NewHTTPDownloader(srv.Client())
	restore := download.SetMaxSizeForTest(d, limit)
	defer restore()

	err := d.Fetch(context.Background(), srv.URL, dest, "")

	require.Error(t, err)
	assert.ErrorIs(t, err, download.ErrTooLarge)
	assert.Contains(t, err.Error(), srv.URL, "error should name the offending URL")
	assert.Contains(t, err.Error(), "32", "error should name the enforced limit")
	assert.NoFileExists(t, dest, "an artifact that exceeded the size limit must not be left at dest")
	assert.NoFileExists(t, dest+".tmp", "the temp file must be cleaned up when the size limit is exceeded")
}

func TestFakeDownloaderRecordsRequests(t *testing.T) {
	d := download.NewFakeDownloader()
	d.Content("https://example.com/go.tar.gz", "tarball bytes")

	dest := filepath.Join(t.TempDir(), "go.tar.gz")
	err := d.Fetch(context.Background(), "https://example.com/go.tar.gz", dest, "")

	require.NoError(t, err)
	assert.Equal(t, []string{"https://example.com/go.tar.gz"}, d.Requests())
	got, _ := os.ReadFile(dest)
	assert.Equal(t, "tarball bytes", string(got))
}

// TestFakeDownloaderFailsOnUnregisteredURL asserts an URL that was never
// programmed via Content fails loudly instead of synthesizing plausible
// bytes. The old permissive behavior let a test that fetched the wrong URL
// (a typo, a bad {version}/{arch} template expansion, a wrong per-arch
// source) pass anyway, which is exactly the class of bug this fake exists to
// catch, not hide.
func TestFakeDownloaderFailsOnUnregisteredURL(t *testing.T) {
	d := download.NewFakeDownloader()

	dest := filepath.Join(t.TempDir(), "go.tar.gz")
	err := d.Fetch(context.Background(), "https://example.com/unregistered.tar.gz", dest, "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "https://example.com/unregistered.tar.gz")
	assert.NoFileExists(t, dest)
}
