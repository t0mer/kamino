// Package webui embeds the built web frontend and serves it as a
// single-page-application: real asset paths are served from the embedded
// filesystem, and any other path returns index.html so the client-side router
// owns navigation.
package webui

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// ErrNoBuild reports that the frontend has not been built into dist yet — the
// directory holds only its tracked placeholder. The server treats this as a
// warning and still serves the API.
var ErrNoBuild = errors.New("web ui not built: dist/index.html is missing")

// Handler serves the embedded SPA. It returns ErrNoBuild if the frontend has
// not been built, so a developer running the binary without a frontend build
// gets a clear message rather than a 404 mystery.
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil, fmt.Errorf("opening embedded dist: %w", err)
	}
	return handlerFor(sub)
}

// handlerFor builds the SPA handler over any filesystem, so the serving
// behaviour can be tested without a real embedded build.
func handlerFor(sub fs.FS) (http.Handler, error) {
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil, ErrNoBuild
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve a real embedded file when one exists at this path; otherwise
		// fall through to index.html so React Router can handle the route.
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "" {
			serveIndex(w, index)
			return
		}
		if _, statErr := fs.Stat(sub, clean); statErr == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w, index)
	}), nil
}

func serveIndex(w http.ResponseWriter, index []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The SPA shell must not be cached: a new build changes the hashed asset
	// names index.html references, and a stale shell would point at assets
	// that no longer exist.
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(index)
}
