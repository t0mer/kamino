#!/usr/bin/env bash
# Build the web UI into the Go embed directory. Run before `go build`/`make
# build` whenever the frontend changed; the binary embeds whatever is currently
# in internal/webui/dist. A clean checkout ships only dist/.gitkeep there, so
# the binary builds UI-less until this has run at least once.
set -euo pipefail
cd "$(dirname "$0")/../web"

# Prefer the reproducible, lockfile-exact install. If the lockfile has drifted
# out of sync — which happens when a transitive, caret-ranged dep of the
# optional wasm rolldown binding (e.g. @emnapi/*) publishes a new patch after
# the lockfile was generated, so a clean-cache CI resolves a version the
# lockfile doesn't pin — fall back to resolving straight from package.json.
# --no-package-lock means the fallback never rewrites the tracked lockfile, so
# a release build stays clean (goreleaser validates git state before hooks).
npm ci || {
	echo "npm ci failed (lockfile drift); resolving from package.json without the lockfile" >&2
	npm install --no-package-lock --no-audit --no-fund
}
npm run build
echo "built web ui into internal/webui/dist"
