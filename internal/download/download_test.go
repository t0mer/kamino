package download_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
