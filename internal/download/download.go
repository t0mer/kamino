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

// ErrTooLarge reports that a downloaded artifact exceeded the size limit
// this downloader enforces.
var ErrTooLarge = errors.New("artifact too large")

// maxRedirects mirrors net/http's default redirect cap (10). Installing a
// custom CheckRedirect disables that built-in default, so it must be
// re-enforced explicitly whenever the caller hasn't supplied their own policy.
const maxRedirects = 10

// maxArtifactSize caps how many bytes HTTPDownloader will stream to disk for
// a single artifact. These are real .deb packages, tarballs and binaries —
// unlike the config-repo YAML capped in internal/remote, a legitimate
// artifact can genuinely be hundreds of MB (a full toolchain tarball, a
// browser .deb). 2 GiB comfortably covers the largest realistic installer
// artifact this tool downloads while still bounding how much disk a
// malicious or misbehaving source can force a root-running process to write
// before an explicit error stops it.
const maxArtifactSize = 2 << 30 // 2 GiB

// Downloader fetches a remote artifact to a local path, verifying it when a
// checksum is declared.
type Downloader interface {
	Fetch(ctx context.Context, url, dest, sha256Hex string) error
}

// HTTPDownloader downloads over HTTPS.
type HTTPDownloader struct {
	client  *http.Client
	maxSize int64
}

// NewHTTPDownloader builds a downloader. A nil client uses a 10 minute
// timeout, generous enough for large artifacts on a slow link.
//
// The client is never mutated in place: NewHTTPDownloader takes a shallow
// copy and installs a CheckRedirect on the copy that fails any redirect hop
// whose target is not https. Checking only the initial URL (as Fetch does)
// is not enough on its own: net/http follows redirects automatically, so a
// server answering an https request with a 302 to a plain http:// Location
// would otherwise have the artifact downloaded over cleartext with
// err == nil. These artifacts are tarballs, .deb packages and binaries that
// Kamino installs as root, so a scheme downgrade here is a direct path to
// running attacker-controlled code. Any CheckRedirect already set on the
// caller's client is called first and its decision is honored before the
// scheme check runs.
func NewHTTPDownloader(client *http.Client) *HTTPDownloader {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	guarded := *client
	guarded.CheckRedirect = redirectHTTPSGuard(client.CheckRedirect)
	return &HTTPDownloader{client: &guarded, maxSize: maxArtifactSize}
}

// redirectHTTPSGuard builds a CheckRedirect function that fails any redirect
// whose target URL is not https, naming the offending URL in the error. next,
// when non-nil, is the caller's own CheckRedirect policy: it runs first and
// its decision (error or nil) is honored unchanged. When next is nil, the
// default net/http redirect-count limit is re-enforced, since setting
// CheckRedirect at all bypasses net/http's built-in default.
func redirectHTTPSGuard(next func(req *http.Request, via []*http.Request) error) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if next != nil {
			if err := next(req, via); err != nil {
				return err
			}
		} else if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}

		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing redirect to %q: sources must use https", req.URL)
		}
		return nil
	}
}

// Fetch downloads url to dest, verifying against sha256Hex when non-empty.
// It refuses any URL that is not https before making a request: installer
// steps run as root, and a plain-http source is trivially tamperable by
// anything on the network path. The same https requirement is enforced on
// every subsequent redirect hop (see NewHTTPDownloader).
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
	// Cap the copy at maxSize+1 bytes so an over-limit body can be told apart
	// from one that lands exactly on the limit: io.Copy only returns fewer
	// than maxSize+1 bytes if the body itself was shorter, so an artifact of
	// exactly maxSize bytes is still accepted. Without this cap a hostile or
	// misbehaving source could stream unbounded bytes to disk under a
	// process running as root.
	n, copyErr := io.Copy(io.MultiWriter(f, hasher), io.LimitReader(resp.Body, d.maxSize+1))
	closeErr := f.Close()

	if copyErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("writing %s: %w", dest, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("closing %s: %w", tmp, closeErr)
	}
	if n > d.maxSize {
		_ = os.Remove(tmp)
		return fmt.Errorf("downloading %s: %w: exceeds %d byte limit", url, ErrTooLarge, d.maxSize)
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
