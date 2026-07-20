// Package download fetches and verifies remote artifacts.
package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrChecksumMismatch reports that a download did not match its declared sha256.
var ErrChecksumMismatch = errors.New("sha256 mismatch")

// Downloader fetches a remote artifact to a local path, verifying it when a
// checksum is declared.
type Downloader interface {
	Fetch(ctx context.Context, url, dest, sha256Hex string) error
}

// HTTPDownloader downloads over HTTPS.
type HTTPDownloader struct {
	client *http.Client
}

// NewHTTPDownloader builds a downloader. A nil client uses a 10 minute
// timeout, generous enough for large artifacts on a slow link.
func NewHTTPDownloader(client *http.Client) *HTTPDownloader {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	return &HTTPDownloader{client: client}
}

// Fetch downloads url to dest, verifying against sha256Hex when non-empty.
// It refuses any URL that is not https before making a request: installer
// steps run as root, and a plain-http source is trivially tamperable by
// anything on the network path.
func (d *HTTPDownloader) Fetch(ctx context.Context, url, dest, sha256Hex string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("refusing to download %q: sources must use https", url)
	}
	return d.fetch(ctx, url, dest, sha256Hex)
}

func (d *HTTPDownloader) fetch(ctx context.Context, url, dest, sha256Hex string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("building request for %s: %w", url, err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: unexpected status %d", url, resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("creating download dir: %w", err)
	}

	// Download to a temp file and hash on the way through, so an artifact that
	// fails verification is never visible at its final path.
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("creating %s: %w", tmp, err)
	}

	hasher := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, hasher), resp.Body)
	closeErr := f.Close()

	if copyErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("writing %s: %w", dest, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing %s: %w", tmp, closeErr)
	}

	if sha256Hex != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(got, sha256Hex) {
			_ = os.Remove(tmp)
			return fmt.Errorf("verifying %s: %w: want %s, got %s", url, ErrChecksumMismatch, sha256Hex, got)
		}
	}

	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("moving download into place: %w", err)
	}
	return nil
}
