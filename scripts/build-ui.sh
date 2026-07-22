#!/usr/bin/env bash
# Build the web UI into the Go embed directory. Run before `go build`/`make
# build` whenever the frontend changed; the binary embeds whatever is currently
# in internal/webui/dist. A clean checkout ships only dist/.gitkeep there, so
# the binary builds UI-less until this has run at least once.
set -euo pipefail
cd "$(dirname "$0")/../web"
npm ci
npm run build
echo "built web ui into internal/webui/dist"
