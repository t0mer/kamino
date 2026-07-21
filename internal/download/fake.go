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

// Fetch writes the programmed content for url to dest.
func (f *FakeDownloader) Fetch(_ context.Context, url, dest, _ string) error {
	f.mu.Lock()
	f.requests = append(f.requests, url)
	body, ok := f.content[url]
	f.mu.Unlock()

	if !ok {
		body = "fake content for " + url
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("creating dir for %s: %w", dest, err)
	}
	return os.WriteFile(dest, []byte(body), 0o644)
}
