#!/usr/bin/env bash
# Strip secrets that ship inside the upstream Ludusavi manifest
# (e.g. hardcoded AWS GameLift creds embedded in launch arguments).
# Run this after `curl`ing a fresh manifest.yaml from upstream.
set -euo pipefail
F="${1:-internal/match/data/manifest.yaml}"
# AWS access key + GameLift secret pattern observed in Squids from Space
sed -i -E \
  -e 's|AKIA[A-Z0-9]{16}|REDACTED_BY_GAMESAVER|g' \
  -e 's|-GameLiftSecretKey=[A-Za-z0-9+/=]+|-GameLiftSecretKey=REDACTED_BY_GAMESAVER|g' \
  "$F"
echo "Sanitized $F"
