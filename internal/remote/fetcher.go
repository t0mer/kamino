package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// maxRedirects mirrors net/http's default redirect cap (10). Installing a
// custom CheckRedirect disables that built-in default, so it must be
// re-enforced explicitly whenever the caller hasn't supplied their own policy.
const maxRedirects = 10

// maxConfigFileSize caps how many bytes of a single config-repo file
// (manifest.yaml, a category file, a profile file) HTTPFetcher will hold in
// memory. These are hand-authored YAML documents — even a manifest listing
// hundreds of items across every category comes to a few hundred KiB at
// most. 10 MiB is roughly two orders of magnitude above that, comfortably
// clear of anything a legitimate config repo would ever produce, while still
// bounding how much a compromised or merely misconfigured host can force a
// root-running process to read into memory.
const maxConfigFileSize = 10 << 20 // 10 MiB

// ErrNotFound reports that a path does not exist in the config repo.
var ErrNotFound = errors.New("not found in config repo")

// ErrTooLarge reports that a config-repo file exceeded the size limit this
// fetcher enforces.
var ErrTooLarge = errors.New("config file too large")

// Fetcher retrieves config repo files as raw content.
type Fetcher interface {
	Fetch(ctx context.Context, ref, path string) ([]byte, error)
}

// HTTPFetcher fetches raw files over HTTPS, caching in memory for the session
// and mirroring to disk as an offline fallback.
type HTTPFetcher struct {
	repo   *Repo
	client *http.Client
	cache  diskCache

	mu    sync.Mutex
	mem   map[string][]byte
	stale bool

	maxSize int64
}

// NewHTTPFetcher builds a fetcher for repo. A nil client uses a 30s default.
//
// The client is never mutated in place: NewHTTPFetcher takes a shallow copy
// and installs a CheckRedirect on the copy that strips repo's auth header
// (e.g. GitLab's PRIVATE-TOKEN) whenever a redirect crosses to a different
// origin (scheme or host) than the one originally requested. net/http only
// does this automatically for a hardcoded list of headers (Authorization,
// WWW-Authenticate, Cookie, Cookie2); provider-specific auth headers are not
// on that list and would otherwise be forwarded to whatever origin a redirect
// points at — including a same-host scheme downgrade from https to http,
// which would transmit the header in the clear. Any CheckRedirect already
// set on the caller's client is called first and its decision is honored
// before the header is stripped.
func NewHTTPFetcher(repo *Repo, cacheDir string, client *http.Client) *HTTPFetcher {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	guarded := *client
	guarded.CheckRedirect = redirectAuthGuard(repo, client.CheckRedirect)
	return &HTTPFetcher{
		repo:    repo,
		client:  &guarded,
		cache:   diskCache{dir: cacheDir},
		mem:     map[string][]byte{},
		maxSize: maxConfigFileSize,
	}
}

// redirectAuthGuard builds a CheckRedirect function that prevents repo's
// configured auth header from following a redirect to a different origin
// than the one the operator originally configured. "Origin" here means both
// scheme and host: a same-host redirect that downgrades from https to http
// is just as much a leak as a redirect to a different host, since it would
// put the header on the wire in the clear. next, when non-nil, is the
// caller's own CheckRedirect policy: it runs first and its decision (error or
// nil) is honored unchanged. When next is nil, the default net/http
// redirect-count limit is re-enforced, since setting CheckRedirect at all
// bypasses net/http's built-in default.
func redirectAuthGuard(repo *Repo, next func(req *http.Request, via []*http.Request) error) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if next != nil {
			if err := next(req, via); err != nil {
				return err
			}
		} else if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}

		if len(via) == 0 {
			return nil
		}
		if name, _, ok := repo.AuthHeader(); ok && !sameOrigin(req.URL, via[0].URL) {
			req.Header.Del(name)
		}
		return nil
	}
}

// sameOrigin reports whether a and b share both scheme and host (which
// includes the port, so https://x.com and https://x.com:8443 are different
// origins). Comparing host alone is not enough to gate a credential header:
// a redirect from https://x.com to http://x.com has an identical Host string
// but strips the transport encryption the header's confidentiality relies
// on, so it must be treated the same as a cross-host redirect.
func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

// Stale reports whether any fetch in this session was served from the disk
// cache, meaning the resolved config may not reflect the live repo.
func (f *HTTPFetcher) Stale() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stale
}

// Fetch retrieves path at ref.
func (f *HTTPFetcher) Fetch(ctx context.Context, ref, path string) ([]byte, error) {
	key := ref + "/" + path

	f.mu.Lock()
	if b, ok := f.mem[key]; ok {
		f.mu.Unlock()
		return b, nil
	}
	f.mu.Unlock()

	body, err := f.get(ctx, ref, path)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// A 404 is a real answer, not an outage. Falling back to a cached
			// copy here would resurrect a file the operator deleted.
			return nil, err
		}
		cached, cacheErr := f.cache.read(ref, path)
		if cacheErr != nil {
			return nil, err
		}
		slog.Warn("serving config from cache", "path", path, "ref", ref, "error", err)
		f.mu.Lock()
		f.stale = true
		f.mem[key] = cached
		f.mu.Unlock()
		return cached, nil
	}

	if err := f.cache.write(ref, path, body); err != nil {
		slog.Warn("mirroring config to cache failed", "path", path, "error", err)
	}

	f.mu.Lock()
	f.mem[key] = body
	f.mu.Unlock()
	return body, nil
}

func (f *HTTPFetcher) get(ctx context.Context, ref, path string) ([]byte, error) {
	rawURL := f.repo.RawURL(ref, path)
	if rawURL == "" {
		return nil, fmt.Errorf("cannot build a raw URL for host %q: set a raw base URL template", f.repo.Host)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %s: %w", path, err)
	}
	if name, value, ok := f.repo.AuthHeader(); ok {
		req.Header.Set(name, value)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("fetching %s: %w", path, ErrNotFound)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("fetching %s: unexpected status %d", path, resp.StatusCode)
	}

	// Read at most maxSize+1 bytes so an over-limit response can be told apart
	// from one that lands exactly on the limit: ReadAll only returns fewer
	// than maxSize+1 bytes if the body itself was shorter, in which case a
	// body of exactly maxSize bytes is correctly accepted. Reading with a cap
	// (rather than io.ReadAll with none) is the fix itself — an unbounded
	// read here is how a compromised or misconfigured config-repo host OOMs
	// a process that runs the rest of this plan as root.
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if int64(len(body)) > f.maxSize {
		return nil, fmt.Errorf("fetching %s: %w: exceeds %d byte limit", path, ErrTooLarge, f.maxSize)
	}
	return body, nil
}
