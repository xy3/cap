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
#   GPU=1 ./scripts/build-windows.sh    # NVIDIA-GPU build (falls back to CPU)
#   MODEL=ggml-small.bin ...            # pick a different speech model
#   MODEL=none ...                      # bundle no model (download it in-app)
#
# Requires: go, curl, unzip, zip, python3 (build host only). Needs network
# access to fetch ffmpeg, whisper.cpp and the model.
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/dist/capper-win64"

# Resilient download: retry on transient failures including HTTP 429
# (HuggingFace rate-limits the model) and 5xx, with backoff.
dl() {
    curl -L --fail --retry 6 --retry-delay 5 --retry-all-errors --retry-connrefused "$@"
}

# A static win64 ffmpeg build (GPL). Override FFMPEG_URL to pin a version.
FFMPEG_URL="${FFMPEG_URL:-https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip}"

# GPU=1 bundles the CUDA (cuBLAS) whisper.cpp build, which uses an NVIDIA GPU
# when one is present and falls back to CPU automatically (ggml picks the CUDA
# backend at runtime, else the CPU backend). It's bigger (+~150 MB of CUDA
# runtime DLLs). Default is the smaller, universally-safe CPU-only BLAS build.
GPU="${GPU:-0}"
if [[ "$GPU" == "1" ]]; then
    # The 12.x build bundles cuBLAS (cublas64_12/cublasLt64_12), so it is fully
    # self-contained — no system CUDA install needed. (The 11.8 build omits
    # cuBLAS and fails with a missing-DLL error on machines without CUDA.)
    # Requires a reasonably recent NVIDIA driver (CUDA 12.x minor-version compat).
    WHISPER_URL="${WHISPER_URL:-https://github.com/ggml-org/whisper.cpp/releases/download/v1.8.6/whisper-cublas-12.4.0-bin-x64.zip}"
    # Ship the small "base" model so the app works out of the box; users grab
    # bigger models (medium/large) from the in-app model manager.
    MODEL="${MODEL:-ggml-base.bin}"
else
    WHISPER_URL="${WHISPER_URL:-https://github.com/ggml-org/whisper.cpp/releases/download/v1.8.6/whisper-blas-bin-x64.zip}"
    # Multilingual "base" (~148 MB). Override e.g. MODEL=ggml-small.bin.
    MODEL="${MODEL:-ggml-base.bin}"
fi
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
dl -o "$TMP/ffmpeg.zip" "$FFMPEG_URL"
unzip -q "$TMP/ffmpeg.zip" -d "$TMP/ffmpeg"
FFMPEG_EXE="$(find "$TMP/ffmpeg" -name ffmpeg.exe | head -n1)"
FFPROBE_EXE="$(find "$TMP/ffmpeg" -name ffprobe.exe | head -n1)"
if [[ -z "$FFMPEG_EXE" || -z "$FFPROBE_EXE" ]]; then
    echo "!! could not find ffmpeg.exe/ffprobe.exe in the downloaded archive" >&2
    exit 1
fi
cp "$FFMPEG_EXE" "$FFPROBE_EXE" "$DIST/"

echo ">> Fetching whisper.cpp: $WHISPER_URL"
dl -o "$TMP/whisper.zip" "$WHISPER_URL"
unzip -q "$TMP/whisper.zip" -d "$TMP/whisper"
WHISPER_EXE="$(find "$TMP/whisper" -name whisper-cli.exe | head -n1)"
if [[ -z "$WHISPER_EXE" ]]; then
    echo "!! could not find whisper-cli.exe in the downloaded archive" >&2
    exit 1
fi
WHISPER_DIR="$(dirname "$WHISPER_EXE")"
cp "$WHISPER_EXE" "$DIST/"
# Copy all runtime DLLs it needs. For the CPU build that's whisper.dll, ggml*.dll
# and libopenblas.dll; for the GPU build it also pulls ggml-cuda.dll and the CUDA
# runtime (cudart*, nvrtc*), so the same binary runs on GPU or falls back to CPU.
cp "$WHISPER_DIR"/*.dll "$DIST/"

if [[ "$MODEL" == "none" ]]; then
    echo ">> Skipping model download (MODEL=none) — user downloads one in the app"
else
    echo ">> Fetching model: $MODEL_URL"
    dl -o "$DIST/$MODEL" "$MODEL_URL"
fi

echo ">> Writing run.bat and bundled config"
cp "$ROOT/scripts/run.bat" "$DIST/run.bat"

# Start from the repo config (or the example) and point whisper at the bundled
# whisper.cpp binary + model. binary_path resolves via PATH (capper prepends its
# own folder at startup); model_path is relative to the launch folder (run.bat
# cd's into the bundle).
# When no model is bundled, default the config to "base" so the model manager
# shows it pre-selected and one click away.
CFG_MODEL="$MODEL"
[[ "$MODEL" == "none" ]] && CFG_MODEL="ggml-base.bin"
BASE_CFG="$ROOT/my_config.json"
[[ -f "$BASE_CFG" ]] || BASE_CFG="$ROOT/examples/config.json"
MODEL="$CFG_MODEL" python3 - "$BASE_CFG" "$DIST/my_config.json" <<'PY'
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

MODE="CPU"; [[ "$GPU" == "1" ]] && MODE="GPU (NVIDIA/cuBLAS, CPU fallback)"
echo ">> Done ($VERSION, $MODE, model: $MODEL):"
echo "   $DIST"
echo "   $ROOT/dist/capper-win64.zip   (full bundle, first install)"
echo "   $ROOT/dist/capper.exe         (update asset for GitHub release)"
du -sh "$DIST" "$ROOT/dist/capper-win64.zip" "$ROOT/dist/capper.exe" 2>/dev/null || true
