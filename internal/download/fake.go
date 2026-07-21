package download

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FakeDownloader serves programmed content from memory and records every URL
// requested.
type FakeDownloader struct {
	mu       sync.Mutex
	content  map[string]string
	requests []string
}

// NewFakeDownloader builds an empty fake.
func NewFakeDownloader() *FakeDownloader {
	return &FakeDownloader{content: map[string]string{}}
}

// Content programs the bytes served for url.
func (f *FakeDownloader) Content(url, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.content[url] = body
}

// Requests returns every URL fetched, in order.
func (f *FakeDownloader) Requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.requests...)
}

// Fetch writes the programmed content for url to dest. An url that was never
// registered via Content is treated as a test bug — a typo, a bad
// {version}/{arch} template expansion, a wrong per-arch source — and fails
// loudly rather than silently synthesizing plausible-looking bytes. A fake
// that fabricates content for an unregistered URL lets a test that fetched
// the wrong URL pass anyway, which is worse than no fake at all.
func (f *FakeDownloader) Fetch(_ context.Context, url, dest, _ string) error {
	f.mu.Lock()
	f.requests = append(f.requests, url)
	body, ok := f.content[url]
	f.mu.Unlock()

	if !ok {
		return fmt.Errorf("fake downloader: no content registered for %q; call Content(url, body) before Fetch", url)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("creating dir for %s: %w", dest, err)
	}
	return os.WriteFile(dest, []byte(body), 0o644)
}
