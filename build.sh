#!/usr/bin/env bash
# Builds F1 Party for this computer into app/build/bin/ (needs Go and the Wails CLI:
#   go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1).
# Builds for all platforms run on GitHub Actions (.github/workflows/build.yml).
set -euo pipefail
cd "$(dirname "$0")/app"
tags=""
[ "$(uname -s)" = Linux ] && tags="-tags webkit2_41"
wails build -clean -trimpath $tags
ls -la build/bin
