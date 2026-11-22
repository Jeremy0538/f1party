#!/usr/bin/env bash
# Builds F1 Party for this computer into app/build/bin/ (needs Go and the Wails CLI:
#   go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1).
# Builds for all platforms run on GitHub Actions (.github/workflows/build.yml).
set -euo pipefail
cd "$(dirname "$0")/app"
tags=""
[ "$(uname -s)" = Linux ] && tags="-tags webkit2_41"
# The version the app shows: the newest release tag plus one minor, as the next release will be called
# (the release workflow sets the real one).
last=$(git tag -l 'v[0-9]*.[0-9]*.[0-9]*' --sort=-v:refname | head -1); last=${last#v}; last=${last:-0.0.0}
IFS=. read -r major minor _ <<< "$last"
wails build -clean -trimpath $tags -ldflags "-X main.version=$major.$((minor + 1)).0"
ls -la build/bin
