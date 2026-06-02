#!/usr/bin/env bash
#
# Build a fully self-contained Windows package for Capper:
#
#   dist/capper-win64/
#     ├─ capper.exe        the cross-compiled CLI + embedded web UI
#     ├─ ffmpeg.exe        bundled so render/preview work out of the box
#     ├─ ffprobe.exe
#     ├─ whisper-cli.exe   bundled whisper.cpp for offline transcription
#     ├─ *.dll             whisper.cpp + OpenBLAS runtime libraries
#     ├─ ggml-base.bin     bundled speech model
#     ├─ run.bat           double-click to launch the UI
#     └─ my_config.json    starting caption style, wired to the bundled whisper
#
# ...and zips it to dist/capper-win64.zip
#
# Everything transcription + render is offline once unzipped — no Python,
# no separate ffmpeg install, no API key.
#
# Requires: go, curl, unzip, zip, python3 (build host only). Needs network
# access to fetch ffmpeg, whisper.cpp and the model.
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/dist/capper-win64"

# A static win64 ffmpeg build (GPL). Override FFMPEG_URL to pin a version.
FFMPEG_URL="${FFMPEG_URL:-https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip}"

# whisper.cpp prebuilt win64 binaries (BLAS build = much faster CPU inference).
WHISPER_URL="${WHISPER_URL:-https://github.com/ggml-org/whisper.cpp/releases/download/v1.8.6/whisper-blas-bin-x64.zip}"

# ggml speech model. MODEL picks the file; default is multilingual "base"
# (~148 MB). Use e.g. MODEL=ggml-base.en.bin or ggml-small.bin to change it.
MODEL="${MODEL:-ggml-base.bin}"
MODEL_URL="${MODEL_URL:-https://huggingface.co/ggerganov/whisper.cpp/resolve/main/$MODEL}"

echo ">> Cleaning $DIST"
rm -rf "$DIST"
mkdir -p "$DIST"

# Version baked into the binary; the self-updater compares this to the latest
# GitHub release tag. Override with VERSION=v1.2.3; defaults to git describe.
VERSION="${VERSION:-$(cd "$ROOT" && git describe --tags --always --dirty 2>/dev/null || echo dev)}"
echo ">> Version: $VERSION"

echo ">> Building capper.exe (windows/amd64)"
( cd "$ROOT" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w -X capper/cmd.Version=$VERSION" -o "$DIST/capper.exe" . )

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo ">> Fetching ffmpeg: $FFMPEG_URL"
curl -L --fail -o "$TMP/ffmpeg.zip" "$FFMPEG_URL"
unzip -q "$TMP/ffmpeg.zip" -d "$TMP/ffmpeg"
FFMPEG_EXE="$(find "$TMP/ffmpeg" -name ffmpeg.exe | head -n1)"
FFPROBE_EXE="$(find "$TMP/ffmpeg" -name ffprobe.exe | head -n1)"
if [[ -z "$FFMPEG_EXE" || -z "$FFPROBE_EXE" ]]; then
    echo "!! could not find ffmpeg.exe/ffprobe.exe in the downloaded archive" >&2
    exit 1
fi
cp "$FFMPEG_EXE" "$FFPROBE_EXE" "$DIST/"

echo ">> Fetching whisper.cpp: $WHISPER_URL"
curl -L --fail -o "$TMP/whisper.zip" "$WHISPER_URL"
unzip -q "$TMP/whisper.zip" -d "$TMP/whisper"
WHISPER_EXE="$(find "$TMP/whisper" -name whisper-cli.exe | head -n1)"
if [[ -z "$WHISPER_EXE" ]]; then
    echo "!! could not find whisper-cli.exe in the downloaded archive" >&2
    exit 1
fi
WHISPER_DIR="$(dirname "$WHISPER_EXE")"
cp "$WHISPER_EXE" "$DIST/"
# Copy the runtime DLLs (whisper.dll, ggml*.dll, libopenblas.dll, ...) it needs.
cp "$WHISPER_DIR"/*.dll "$DIST/"

echo ">> Fetching model: $MODEL_URL"
curl -L --fail -o "$DIST/$MODEL" "$MODEL_URL"

echo ">> Writing run.bat and bundled config"
cp "$ROOT/scripts/run.bat" "$DIST/run.bat"

# Start from the repo config (or the example) and point whisper at the bundled
# whisper.cpp binary + model. binary_path resolves via PATH (capper prepends its
# own folder at startup); model_path is relative to the launch folder (run.bat
# cd's into the bundle).
BASE_CFG="$ROOT/my_config.json"
[[ -f "$BASE_CFG" ]] || BASE_CFG="$ROOT/examples/config.json"
MODEL="$MODEL" python3 - "$BASE_CFG" "$DIST/my_config.json" <<'PY'
import json, os, sys
src, dst = sys.argv[1], sys.argv[2]
cfg = json.load(open(src))
cfg["whisper"] = {
    **cfg.get("whisper", {}),
    "mode": "local",
    "binary_path": "whisper-cli",
    "model_path": os.environ["MODEL"],
    "language": cfg.get("whisper", {}).get("language", "en"),
}
json.dump(cfg, open(dst, "w"), indent=2)
PY

echo ">> Zipping full bundle"
( cd "$ROOT/dist" && rm -f capper-win64.zip && zip -qr capper-win64.zip capper-win64 )

# Standalone exe = the self-update asset. Attach this to the GitHub release as
# "capper.exe" so the in-app updater (and `capper update`) can fetch it.
cp "$DIST/capper.exe" "$ROOT/dist/capper.exe"

echo ">> Done ($VERSION):"
echo "   $DIST"
echo "   $ROOT/dist/capper-win64.zip   (full bundle, first install)"
echo "   $ROOT/dist/capper.exe         (update asset for GitHub release)"
du -sh "$DIST" "$ROOT/dist/capper-win64.zip" "$ROOT/dist/capper.exe" 2>/dev/null || true
