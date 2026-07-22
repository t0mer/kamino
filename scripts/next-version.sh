#!/usr/bin/env bash
# Compute the next release version in the date-based YYYY.M.PATCH scheme (no
# leading zero on the month, e.g. 2026.7.0). Takes today's YYYY.M, finds the
# latest matching tag, and increments PATCH; the first release in a month is .0.
set -euo pipefail
TODAY="$(date +%Y.%-m)" # %-m = month with no leading zero
LATEST="$(git tag --list "${TODAY}.*" 2>/dev/null | sort -V | tail -1)"
if [ -z "$LATEST" ]; then
	echo "${TODAY}.0"
else
	PATCH="${LATEST##*.}"
	echo "${TODAY}.$((PATCH + 1))"
fi
