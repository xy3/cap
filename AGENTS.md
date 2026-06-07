# AGENTS.md — Capper

## Overview

Capper is a Go CLI tool that transcribes video audio and overlays animated captions. It extracts audio via FFmpeg, transcribes with OpenAI Whisper (API or local binary), groups words into timed frames, generates ASS subtitle files with animation tags, and renders the final video via FFmpeg.

### Pipeline
```
input.mp4 → audio extract (FFmpeg) → transcription (Whisper) → frame grouping → ASS generation → output.mp4 (FFmpeg)
```

## Project Structure

```
capper/
├── main.go              # Entry point, calls cmd.Execute()
├── go.mod/go.sum        # Module: capper, Go 1.23
├── cmd/root.go           # Cobra CLI, orchestration, cache logic
├── config/config.go      # Config structs, YAML/JSON loading, validation, defaults
├── whisper/whisper.go    # Transcriber interface, OpenAI API client, local whisper client
├── caption/caption.go    # Word grouping into Frame structs, timing-based splits
├── animation/animation.go # ASS override tag generation (fade/pop/slide/underline)
├── render/render.go      # ASS subtitle builder, ffprobe, FFmpeg video rendering
├── examples/             # Sample YAML/JSON config files
└── README.md
```

## Architecture

### Config (`config/config.go`)
- `Config` struct holds all user-facing settings
- `DefaultConfig()` returns sensible defaults
- `LoadConfig(path)` reads YAML or JSON, merges with defaults, validates
- Colors: stored as `#RRGGBB`, converted to ASS BGR format via `HexToASSBGR()`
- Validation checks enum values, required fields, ranges

### Whisper (`whisper/whisper.go`)
- `Transcriber` interface: `Transcribe(ctx, videoPath, Params) (*TranscriptionResult, error)`
- `OpenAIClient`: uses `go-openai` library, calls `/v1/audio/transcriptions` with `verbose_json` + word-level timestamps
- `LocalClient`: runs `whisper` or `whisper.cpp` binary via `os/exec`. Auto-detects which variant by binary name (`isWhisperCPP()`)
- Local client has 3-tier fallback: GPU+word_timestamps → CPU+word_timestamps → segment-level
- Cache: `SaveCache()`/`LoadCache()` write/read `<video>.capper.json` to skip re-transcription

### Caption (`caption/caption.go`)
- `Frame`: Words, Text, Start, End time, Index
- `GroupWords(words, cfg)`: splits words into frames based on `chars_per_frame` (per-line character budget) and `max_gap_ms` (timing gap threshold). A word is flushed onto a new line before its addition would push the line past `chars_per_frame`; a single over-long word still goes alone. When `max_gap_ms > 0`, a pause exceeding that duration forces a split too
- `GroupWordsKaraoke(words, cfg)`: groups per-word frames into lines up to `chars_per_frame` characters for ASS `\k` timing

### Animation (`animation/anim.go`)
- `GenerateOverrideTags(frame, cfg)`: returns ASS inline tags like `{\fad(300,0)}`, `{\move(...)}`, or `{\fscx0\fscy0\t(...)}`
- `GenerateKaraokeTags(frame, cfg)`: returns ASS text with `{\k}` and `{\kf}` timing tags per word
- `WordPosEvents(frame, cfg)`: for underline/both highlight mode — generates per-word normal events (full frame span) and highlight events (word duration) with `\pos(x,y)` for exact positioning. Uses monospace font char-width estimation (`font_size * 0.6`), centers the full line horizontally
- `UnderlineTags(frame, cfg)`: returns `{\u1}text` for simple underline

### Render (`render/render.go`)
- `ASSBuilder`: accumulates styles and dialogue events, then `Build()` returns a complete `.ass` file
- Methods: `AddStyle()`, `AddKaraokeStyle()`, `AddStaticEvent()`, `AddKaraokeEvent()`, `AddUnderlineEvent()`, `AddPosWordEvents()`
- `WriteASSFile(content, path)`: writes the ASS file to disk
- `DetectResolution(videoPath)`: uses ffprobe to get video width/height
- `RenderVideo(input, assPath, output)`: runs FFmpeg to burn ASS subtitles (`-vf ass=...`), copies audio

### CLI (`cmd/root.go`)
- Flags: `-i/--input` (required), `-c/--config` (default: config.yaml), `-o/--output`, `-k/--api-key`, `-f/--force`
- Orchestration order: validate input → load config → detect resolution → check cache → transcribe (if needed) → save cache → group words → build ASS → render video
- Selects `Transcriber` based on `whisper.mode` (api vs local)

## ASS Format Notes

- Colors are ABGR hex: `&H00BBGGRR&` (e.g., white = `&H00FFFFFF&`)
- `\fad(in_ms, out_ms)`: fade in/out
- `\move(x1,y1,x2,y2[,t1,t2])`: movement animation
- `\t(accel, t1, t2, modifiers)`: time-transform (scale, color, etc.)
- `\pos(x,y)`: absolute positioning, `\an7` for top-left alignment
- `\k(duration_cs)`: karaoke timing, swaps Primary→Secondary colour
- `\u1`/`\u0`: underline on/off
- `\alpha&HFF&`: fully transparent, `\alpha&H00&`: fully opaque
- `\fs`: font size, `\bord`: outline width, `\shad`: shadow depth

## Display Modes

| Mode | Config | Behavior |
|------|--------|----------|
| static | `display_mode: "static"` | Words grouped into frames, entrance animation only. `chars_per_frame` and `max_gap_ms` control grouping |
| karaoke (color) | `display_mode: "karaoke"` + `highlight: "color"` | ASS karaoke timing, active word uses `active_color`, completed words use `inactive_color` |
| karaoke (underline) | `display_mode: "karaoke"` + `highlight: "underline"` | Per-word `\pos(x,y)` events, current word gets bigger font + underline via overlay at exact same position. No karaoke progress bar |
| karaoke (both) | `display_mode: "karaoke"` + `highlight: "both"` | Underline + color shift combined |

## Build & Run

```bash
go build -o capper .
./capper --input video.mp4 --config my_config.json
```

### Key environment variables
- `OPENAI_API_KEY` — required for `whisper.mode: "api"`
- None required for `whisper.mode: "local"` (needs `whisper` or `whisper-cli` in PATH, or explicit `binary_path`)

### External dependencies
- FFmpeg with libass support (`ffmpeg -filters | grep ass`)
- For local mode: `openai-whisper` (Python) or `whisper.cpp` binary
- For API mode: OpenAI API key with Whisper access

## Code Style

- No comments unless asked
- Follow existing patterns for new code (same error wrapping, same naming)
- Use `//` comments when adding explanatory notes
- Default config values are in `DefaultConfig()`, not hardcoded elsewhere
- Cache files are `video.mp4 → video.mp4.capper.json`
- Build check: `go build -o /dev/null . && go vet ./...`

## Known Caveats

- `openai-whisper` + triton >= 3.x crashes with `--word_timestamps True` on GPU. The local client falls back to CPU automatically
- ASS karaoke cannot selectively underline one word — the underline/both modes use per-word positioned events instead of karaoke timing
- Word positioning in underline mode assumes monospace font (char width = `font_size * 0.6`). Switch to a monospace font family for accurate alignment
- The italic styling config field (`font.italic`) is parsed but not applied in ASS (could be added via `\i1` tags)
