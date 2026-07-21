#!/usr/bin/env bash
# Real end-to-end test: installs the `test` profile inside a clean Ubuntu
# container, then asserts idempotency by running it a second time.
#
# Every runner test elsewhere in this repo uses a fake command executor —
# that proves Kamino builds the right command line, never that the command
# actually works against real apt/dpkg/tar. This script is the only thing
# that exercises a real install.
#
# Not wired into CI: it needs Docker and pulls packages from the network.
# Run it by hand after touching any runner (`make smoke`).
set -euo pipefail

cd "$(dirname "$0")/.."

IMAGE="${IMAGE:-ubuntu:24.04}"

echo "==> building linux/amd64 binary"
mkdir -p dist
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o dist/kamino-smoke ./cmd/kamino

echo "==> running apply in $IMAGE"
set +e
OUTPUT="$(docker run --rm \
    -v "$PWD/dist/kamino-smoke:/usr/local/bin/kamino:ro" \
    -v "$PWD/testdata/config:/config:ro" \
    "$IMAGE" \
    bash -euo pipefail -c '
        export DEBIAN_FRONTEND=noninteractive

        echo "--- first apply: expect a real install ---"
        kamino apply --config-dir /config --profile test --arch amd64 --yes --verbose \
            --data-dir /var/lib/kamino

        echo "--- asserting jq is actually installed ---"
        command -v jq >/dev/null || { echo "FAIL: jq not on PATH"; exit 1; }
        jq --version

        echo "--- second apply: expect every step skipped ---"
        output="$(kamino apply --config-dir /config --profile test --arch amd64 --yes \
            --data-dir /var/lib/kamino)"
        echo "$output"
        echo "$output" | grep -q "already installed, skipped" \
            || { echo "FAIL: second run did not skip; idempotency is broken"; exit 1; }

        echo "--- asserting run history was persisted ---"
        test -f /var/lib/kamino/kamino.db || { echo "FAIL: no state database"; exit 1; }

        echo "PASS"
    ' 2>&1)"
STATUS=$?
set -e

echo "$OUTPUT"

if [ "$STATUS" -ne 0 ]; then
    if echo "$OUTPUT" | grep -qiE "Could not resolve|Temporary failure resolving|Failed to fetch|Connection timed out|Network is unreachable"; then
        echo "==> DIAGNOSIS: apt-get could not reach the network from inside the container." >&2
        echo "    This is an environment/network problem, not a Kamino bug. Check the" >&2
        echo "    container's outbound DNS/HTTP access and retry." >&2
    elif echo "$OUTPUT" | grep -q "FAIL: jq not on PATH"; then
        echo "==> DIAGNOSIS: the apt runner reported success but jq is not actually on" >&2
        echo "    PATH afterwards — it built a command that ran but installed nothing." >&2
    elif echo "$OUTPUT" | grep -q "FAIL: second run did not skip"; then
        echo "==> DIAGNOSIS: idempotency is broken — the check/check_contains probe did" >&2
        echo "    not detect the already-installed package, so Kamino would reinstall" >&2
        echo "    everything on every run." >&2
    elif echo "$OUTPUT" | grep -q "FAIL: no state database"; then
        echo "==> DIAGNOSIS: run history was not persisted — no sqlite database found" >&2
        echo "    at /var/lib/kamino/kamino.db inside the container." >&2
    else
        echo "==> DIAGNOSIS: apply failed for an unrecognised reason; see output above." >&2
    fi
    echo "==> smoke test FAILED" >&2
    exit 1
fi

echo "==> smoke test PASSED"
