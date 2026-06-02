package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"capper/caption"
	"capper/config"
	"capper/render"
	"capper/whisper"

	"github.com/spf13/cobra"
)

var (
	inputPath  string
	configPath string
	outputPath string
	apiKey     string
	force      bool
)

var rootCmd = &cobra.Command{
	Use:   "capper",
	Short: "Video captioning and animation CLI",
	Long: `Capper transcribes video audio using OpenAI Whisper and overlays
animated, customizable text captions using FFmpeg.

Supports both the OpenAI Whisper API and a local whisper installation
(openai-whisper or whisper.cpp).`,
	RunE: runCapper,
}

func init() {
	rootCmd.Flags().StringVarP(&inputPath, "input", "i", "", "Path to input video file (required)")
	rootCmd.Flags().StringVarP(&configPath, "config", "c", "config.yaml", "Path to configuration file (YAML or JSON)")
	rootCmd.Flags().StringVarP(&outputPath, "output", "o", "", "Path to output video file (overrides config)")
	rootCmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "OpenAI API key (overrides OPENAI_API_KEY env var)")
	rootCmd.Flags().BoolVarP(&force, "force", "f", false, "Force re-transcription, ignore cache")
	rootCmd.MarkFlagRequired("input")
}

func Execute() error {
	prependExecDirToPath()
	return rootCmd.Execute()
}

// prependExecDirToPath ensures helper binaries bundled alongside the capper
// executable (e.g. ffmpeg.exe / ffprobe.exe in the Windows package) are
// discoverable on PATH, so both direct exec calls and the ffmpeg-go library
// find them even when the user double-clicks the binary outside a shell.
// On Linux this is a harmless no-op unless ffmpeg actually sits next to the
// binary, so the normal system ffmpeg on PATH continues to be used.
func prependExecDirToPath() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	dir := filepath.Dir(exe)
	sep := string(os.PathListSeparator)
	path := os.Getenv("PATH")
	if path == "" {
		os.Setenv("PATH", dir)
		return
	}
	if strings.HasPrefix(path, dir+sep) {
		return
	}
	os.Setenv("PATH", dir+sep+path)
}

func runCapper(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(inputPath); os.IsNotExist(err) {
		return fmt.Errorf("input video not found: %s", inputPath)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if outputPath != "" {
		cfg.OutputPath = outputPath
	}

	output, err := RunPipeline(cfg, inputPath, apiKey, force, true)
	if err != nil {
		return err
	}

	fmt.Printf("Output: %s\n", output)
	return nil
}

type ProgressEmitter struct {
	OnStage    func(stage string)
	OnProgress func(stage string, value float64)
}

func (p *ProgressEmitter) stage(s string) {
	if p != nil && p.OnStage != nil {
		p.OnStage(s)
	}
}
func (p *ProgressEmitter) progress(s string, v float64) {
	if p != nil && p.OnProgress != nil {
		p.OnProgress(s, v)
	}
}

func RunPipeline(cfg *config.Config, input, apiKeyArg string, forceRecache, verbose bool) (string, error) {
	return RunPipelineProgress(cfg, input, apiKeyArg, forceRecache, verbose, nil)
}

func RunPipelineProgress(cfg *config.Config, input, apiKeyArg string, forceRecache, verbose bool, emit *ProgressEmitter) (string, error) {
	resW, resH, err := render.DetectResolution(input)
	if err == nil && resW > 0 && resH > 0 {
		cfg.Resolution = fmt.Sprintf("%dx%d", resW, resH)
		if verbose {
			fmt.Printf("Detected video resolution: %dx%d\n", resW, resH)
		}
	}

	result, err := transcribeOrCacheProgress(cfg, input, apiKeyArg, forceRecache, verbose, emit)
	if err != nil {
		return "", err
	}

	if len(result.Words) == 0 {
		return "", fmt.Errorf("no words found in transcription")
	}

	emit.stage("rendering")
	assContent := BuildASS(result.Words, cfg)

	tmpDir := filepath.Dir(cfg.OutputPath)
	if tmpDir == "" {
		tmpDir = "."
	}
	assPath := filepath.Join(tmpDir, "capper_subtitles.ass")
	if err := render.WriteASSFile(assContent, assPath); err != nil {
		return "", fmt.Errorf("writing ASS file: %w", err)
	}
	defer os.Remove(assPath)

	if verbose {
		fmt.Println("Rendering final video with FFmpeg...")
	}
	renderStart := time.Now()

	onProg := func(v float64) { emit.progress("rendering", v) }
	if emit == nil {
		onProg = nil
	}
	if err := render.RenderVideoWithProgress(input, assPath, cfg.OutputPath, onProg); err != nil {
		return "", fmt.Errorf("rendering video: %w", err)
	}

	if verbose {
		fmt.Printf("Video rendered in %s\n", time.Since(renderStart).Round(time.Millisecond))
	}

	return cfg.OutputPath, nil
}

func TranscribeOrCache(cfg *config.Config, input, apiKeyArg string, forceRecache, verbose bool) (*whisper.TranscriptionResult, error) {
	return transcribeOrCacheProgress(cfg, input, apiKeyArg, forceRecache, verbose, nil)
}

func transcribeOrCacheProgress(cfg *config.Config, input, apiKeyArg string, forceRecache, verbose bool, emit *ProgressEmitter) (*whisper.TranscriptionResult, error) {
	if !forceRecache {
		if cached, err := whisper.LoadCache(input); err == nil {
			if verbose {
				fmt.Printf("Using cached transcription (%s)\n", whisper.CachePath(input))
			}
			return cached, nil
		}
	}

	emit.stage("transcribing")

	var transcriber whisper.Transcriber
	switch cfg.Whisper.Mode {
	case config.WhisperModeLocal:
		if verbose {
			fmt.Printf("Using local whisper (%s) with model %s\n", cfg.Whisper.BinaryPath, cfg.Whisper.ModelPath)
		}
		transcriber = whisper.NewLocalClient(cfg.Whisper.BinaryPath, resolveModelPath(cfg.Whisper.ModelPath), cfg.Whisper.Language)
	default:
		key := apiKeyArg
		if key == "" {
			key = os.Getenv("OPENAI_API_KEY")
		}
		if key == "" {
			return nil, fmt.Errorf("OpenAI API key required: set OPENAI_API_KEY env var or pass --api-key")
		}
		transcriber = whisper.NewOpenAIClient(key)
	}

	if verbose {
		fmt.Println("Extracting audio and transcribing with Whisper...")
	}
	startTime := time.Now()

	var onTranscribeProgress func(float64)
	if emit != nil {
		onTranscribeProgress = func(v float64) { emit.progress("transcribing", v) }
	}
	result, err := transcriber.Transcribe(context.Background(), input, whisper.Params{
		Model:       cfg.Whisper.Model,
		Language:    cfg.Whisper.Language,
		Prompt:      cfg.Whisper.Prompt,
		Temperature: cfg.Whisper.Temperature,
		BinaryPath:  cfg.Whisper.BinaryPath,
		ModelPath:   cfg.Whisper.ModelPath,
		Progress:    onTranscribeProgress,
	})
	if err != nil {
		return nil, fmt.Errorf("transcription failed: %w", err)
	}

	if verbose {
		fmt.Printf("Transcription complete (%d words, %.1fs audio) in %s\n",
			len(result.Words), result.Duration, time.Since(startTime).Round(time.Millisecond))
	}

	if err := whisper.SaveCache(input, result); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write cache: %v\n", err)
	}

	return result, nil
}

func BuildASS(words []whisper.Word, cfg *config.Config) string {
	builder := render.NewASSBuilder(cfg)

	switch cfg.DisplayMode {
	case config.DisplayKaraoke:
		if cfg.Karaoke.Highlight == config.HighlightUnderline || cfg.Karaoke.Highlight == config.HighlightBoth {
			frames := caption.GroupWords(words, cfg)
			builder.AddStyle("Default")
			for _, f := range frames {
				builder.AddPosWordEvents(f)
			}
		} else {
			frames := caption.GroupWordsKaraoke(words)
			builder.AddKaraokeStyle()
			for _, f := range frames {
				builder.AddKaraokeEvent(f)
			}
		}
	default:
		frames := caption.GroupWords(words, cfg)
		builder.AddStyle("Default")
		for _, f := range frames {
			builder.AddStaticEvent(f)
		}
	}

	return builder.Build()
}
