#!/usr/bin/env bash
# Builds F1 Party for every platform into dist/.
set -euo pipefail
cd "$(dirname "$0")/app"
mkdir -p ../dist
for t in windows/amd64 darwin/arm64 darwin/amd64 linux/amd64; do
	case $t in windows/*) n=windows.exe ;; darwin/arm64) n=macos-apple-silicon ;; darwin/amd64) n=macos-intel ;; *) n=linux ;; esac
	CGO_ENABLED=0 GOOS=${t%/*} GOARCH=${t#*/} go build -trimpath -ldflags='-s -w' -o "../dist/F1Party-$n" .
done
ls -la ../dist
