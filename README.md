# Capper — Video Captioning & Animation CLI

Automatically transcribe video audio and overlay animated, customizable captions using OpenAI Whisper and FFmpeg.

Supports both the **OpenAI Whisper API** and a **local whisper installation** (openai-whisper or whisper.cpp).

## Features

- **Word-level transcription** via Whisper with exact timestamps
- **API or local** — use OpenAI's cloud API or a local whisper binary
- **Configurable word grouping** — control how many words appear per on-screen frame
- **Multiple animation types** — fade-in, pop-in (scale bounce), slide-in (directional), or none
- **Per-word karaoke highlighting** — words light up as they are spoken
- **Full text styling** — font family, size, color, bold, italic, stroke/outline, drop shadow
- **10 alignment positions** — any corner, edge center, or screen center
- **YAML or JSON** configuration files
- **Auto-detects video resolution** via ffprobe

## Prerequisites

- **Go 1.23+**
- **FFmpeg** (with libass enabled)

### FFmpeg

```bash
# macOS
brew install ffmpeg

# Ubuntu/Debian
sudo apt install ffmpeg

# Fedora
sudo dnf install ffmpeg

# Arch
sudo pacman -S ffmpeg
```

Verify FFmpeg has libass support:
```bash
ffmpeg -filters 2>&1 | grep ass
```

### Whisper (choose one)

**Option A — OpenAI API key:**
Set `OPENAI_API_KEY` env var or pass `--api-key`.

**Option B — Local whisper (Python openai-whisper):**
```bash
pip install openai-whisper
# Verify:
whisper --help
```

**Option C — Local whisper.cpp:**
```bash
git clone https://github.com/ggerganov/whisper.cpp
cd whisper.cpp && make
# Download a model:
./models/download-ggml-model.sh base
# The binary is named 'main' (or 'whisper-cli')
```

## Installation

```bash
git clone https://github.com/anomalyco/capper.git
cd capper
go build -o capper .
```

Or:
```bash
go install .
```

### Windows package

Capper runs on Windows as well as Linux/macOS. To produce a **fully
self-contained** Windows bundle — transcription *and* rendering work offline,
with no Python, no separate FFmpeg, and no API key — run from Linux or macOS:

```bash
./scripts/build-windows.sh
```

This cross-compiles `capper.exe` and downloads a static win64 FFmpeg,
whisper.cpp, and a speech model, assembling:

```
dist/capper-win64.zip   (~286 MB)
└── capper-win64/
    ├── capper.exe        # CLI + embedded web UI
    ├── ffmpeg.exe        # render & preview
    ├── ffprobe.exe
    ├── whisper-cli.exe   # offline transcription (whisper.cpp)
    ├── *.dll             # whisper.cpp + OpenBLAS runtime
    ├── ggml-base.bin     # bundled speech model (~148 MB)
    ├── run.bat           # double-click to launch the styling UI
    └── my_config.json    # caption style, pre-wired to the bundled whisper
```

The user unzips it anywhere and double-clicks **`run.bat`** to open the UI at
`http://localhost:8080`. `capper.exe` prepends its own folder to `PATH` on
startup, so it finds the bundled FFmpeg and whisper with no setup. For CLI use,
run `capper.exe --input video.mp4 ...` from that folder.

The model is overridable at build time — e.g. `MODEL=ggml-small.bin
./scripts/build-windows.sh` for higher accuracy, or `ggml-base.en.bin` for a
smaller English-only model. (You can still switch the config to the OpenAI API
or Python whisper instead; the bundle just defaults to offline whisper.cpp.)

**GPU build:** `GPU=1 ./scripts/build-windows.sh` bundles the CUDA (cuBLAS)
whisper.cpp build, which uses an NVIDIA GPU when present and **falls back to CPU
automatically** if there's no usable GPU. It adds ~150 MB of CUDA runtime DLLs
and defaults to the `medium` model (a GPU handles it easily). Requires a
reasonably recent NVIDIA driver on the user's machine; no CUDA install needed.

### Updating Windows users

Updates ship as just the ~18 MB `capper.exe` — the bundled FFmpeg, whisper, and
model never change, so users never re-download the full bundle. The version is
baked into the binary and the app self-updates from GitHub Releases.

**To publish an update — just push a tag:**

```bash
git tag 1.3.0 && git push --tags
```

The `release` GitHub Action (`.github/workflows/release.yml`) then builds the
Linux binary and the full Windows bundle and publishes a GitHub Release with the
right assets — nothing is built on your machine. (`v1.3.0`-style tags work too.)
To build a release locally instead, run `./scripts/release.sh 1.3.0`.

**What the user sees:** when they open the UI, capper checks the latest release.
If a newer version exists, a green **"⬆ Update to 1.3.0"** button appears in the
header. Clicking it downloads the new `capper.exe`, swaps it in place, and — when
launched via `run.bat` — capper restarts itself on the same port and the page
reloads into the new version automatically. No manual download, no reinstall.

(There's also a CLI: `capper.exe update` to update in place, and
`capper.exe version` to print the current version.)

## Usage

```bash
# API mode (default) — requires OPENAI_API_KEY
capper --input video.mp4 --api-key sk-...

# API mode with custom config
capper --input video.mp4 --config my_style.yaml

# Local whisper (Python)
capper --input video.mp4 --config examples/config.yaml

# Local whisper with custom binary and model
capper --input video.mp4 --config my_local.yaml

# Override output path
capper --input video.mp4 --config config.yaml --output final.mp4
```

## Configuration Reference

### API mode (`whisper.mode: "api"`)

```yaml
whisper:
  mode: "api"                 # Use OpenAI API
  model: "whisper-1"          # API model name
  language: "en"
  prompt: ""                  # Optional context for better accuracy
  temperature: 0.0

  # Not needed for API mode:
  # binary_path: ""
  # model_path: ""
```

### Local mode (`whisper.mode: "local"`)

For Python `openai-whisper`:
```yaml
whisper:
  mode: "local"
  binary_path: "whisper"      # Path to the whisper command
  model_path: "base"          # Model name: tiny, base, small, medium, large
  language: "en"
  prompt: ""
  temperature: 0.0
```

For `whisper.cpp`:
```yaml
whisper:
  mode: "local"
  binary_path: "/path/to/whisper.cpp/main"   # Or 'whisper-cli', 'whisper-cpp'
  model_path: "/path/to/models/ggml-base.bin" # Full path to model file
  language: "en"
```

Capper auto-detects which whisper variant you are using based on the binary name.

### Full configuration

```yaml
words_per_frame: 4
display_mode: "static"        # "static" or "karaoke"
output_path: "output.mp4"

font:
  family: "Arial"
  size: 48
  color: "#FFFFFF"
  bold: false
  italic: false
  underline: false

stroke:
  color: "#000000"
  width: 2.0

shadow:
  color: "#000000"
  depth: 2.0

animation:
  type: "fade-in"             # fade-in | pop-in | slide-in | none
  duration_ms: 300
  slide_direction: "bottom"   # left | right | top | bottom
  slide_distance: 50

position:
  alignment: 2                # 2 = bottom center
  margin_left: 60
  margin_right: 60
  margin_top: 20
  margin_bottom: 100

whisper:
  mode: "api"                 # "api" or "local"
  model: "whisper-1"
  language: "en"
  prompt: ""
  temperature: 0.0
  # Local mode only:
  # binary_path: "whisper"
  # model_path: "base"

karaoke:
  active_color: "#FFFF00"
  inactive_color: "#FFFFFF"
```

### Alignment Values

| Value | Position        |
|-------|-----------------|
| 1     | Bottom left     |
| 2     | Bottom center   |
| 3     | Bottom right    |
| 4     | Middle left     |
| 5     | Middle center   |
| 6     | Middle right    |
| 7     | Top left        |
| 8     | Top center      |
| 9     | Top right       |

## How It Works

1. **Audio extraction** — FFmpeg extracts mono 16kHz WAV audio from the input video
2. **Transcription** — Audio is sent to OpenAI Whisper API or processed by a local whisper binary to get word-level timestamps
3. **Frame grouping** — Words are grouped into on-screen frames based on `words_per_frame`
4. **ASS generation** — An Advanced SubStation Alpha subtitle file is generated with all styles, positions, and animation override tags
5. **Rendering** — FFmpeg burns the ASS subtitles directly into the video stream, copying the original audio

## Examples

See the `examples/` directory for sample configuration files:

- `examples/config.yaml` — Standard API-mode fade-in captions
- `examples/config.json` — Bold pop-in style with JSON format
- `examples/config-local.json` — Local whisper mode

## Environment Variables

| Variable          | Description                     |
|-------------------|---------------------------------|
| `OPENAI_API_KEY`  | OpenAI API key (API mode only)  |

## License

MIT
