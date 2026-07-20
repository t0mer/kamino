package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// ErrNotFound reports that a path does not exist in the config repo.
var ErrNotFound = errors.New("not found in config repo")

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
}

// NewHTTPFetcher builds a fetcher for repo. A nil client uses a 30s default.
func NewHTTPFetcher(repo *Repo, cacheDir string, client *http.Client) *HTTPFetcher {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPFetcher{
		repo:   repo,
		client: client,
		cache:  diskCache{dir: cacheDir},
		mem:    map[string][]byte{},
	}
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return body, nil
}
