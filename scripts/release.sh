#!/usr/bin/env bash
#
# Cut a Capper release: build the Windows + Linux artifacts and publish a GitHub
# release that the in-app self-updater (and `capper update`) pulls from.
#
#   ./scripts/release.sh 1.3.0
#
# It produces and uploads:
#   capper.exe              <- Windows self-update asset (must keep this name)
#   capper-linux-amd64      <- Linux self-update asset   (must keep this name)
#   capper-win64.zip        <- CPU bundle (ffmpeg + whisper.cpp, no model)
#   capper-win64-cuda.zip   <- CUDA/GPU bundle (CPU fallback, no model)
#
# Requires: go, gh (authenticated: `gh auth login`), plus the tools
# build-windows.sh needs (curl, unzip, zip, python3) and network access.
#
set -euo pipefail

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
    echo "usage: $0 X.Y.Z   (e.g. 1.3.0)" >&2
    exit 1
fi
if [[ ! "$VERSION" =~ ^v?[0-9] ]]; then
    echo "!! version should look like 1.3.0 (or v1.3.0)" >&2
    exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/dist"
LDFLAGS="-s -w -X capper/cmd.Version=$VERSION"

# A release should be cut from a clean, committed tree so the tag matches the
# artifacts. Set ALLOW_DIRTY=1 to override (e.g. for a test build).
if [[ -n "$(cd "$ROOT" && git status --porcelain)" && "${ALLOW_DIRTY:-}" != "1" ]]; then
    echo "!! working tree is dirty — commit first, or re-run with ALLOW_DIRTY=1" >&2
    exit 1
fi

mkdir -p "$DIST"

echo ">> Building Linux binary (linux/amd64)"
( cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "$LDFLAGS" -o "$DIST/capper-linux-amd64" . )

echo ">> Building Windows GPU (CUDA) bundle"
VERSION="$VERSION" GPU=1 MODEL=none "$ROOT/scripts/build-windows.sh"
mv "$DIST/capper-win64.zip" "$DIST/capper-win64-cuda.zip"

echo ">> Building Windows CPU bundle + update exe"
VERSION="$VERSION" MODEL=none "$ROOT/scripts/build-windows.sh"

echo ">> Tagging $VERSION"
if ! git -C "$ROOT" rev-parse "$VERSION" >/dev/null 2>&1; then
    git -C "$ROOT" tag -a "$VERSION" -m "capper $VERSION"
    git -C "$ROOT" push origin "$VERSION"
fi

echo ">> Publishing GitHub release $VERSION"
gh release create "$VERSION" \
    "$DIST/capper.exe" \
    "$DIST/capper-linux-amd64" \
    "$DIST/capper-win64.zip" \
    "$DIST/capper-win64-cuda.zip" \
    --title "capper $VERSION" \
    --generate-notes

echo ">> Done: https://github.com/xy3/capper/releases/tag/$VERSION"
