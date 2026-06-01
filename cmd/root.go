package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	return rootCmd.Execute()
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

	resW, resH, err := render.DetectResolution(inputPath)
	if err == nil && resW > 0 && resH > 0 {
		cfg.Resolution = fmt.Sprintf("%dx%d", resW, resH)
		fmt.Printf("Detected video resolution: %dx%d\n", resW, resH)
	}

	var result *whisper.TranscriptionResult

	cachePath := whisper.CachePath(inputPath)

	if !force {
		if cached, err := whisper.LoadCache(inputPath); err == nil {
			fmt.Printf("Using cached transcription (%s)\n", cachePath)
			result = cached
		}
	}

	if result == nil {
		var transcriber whisper.Transcriber

		switch cfg.Whisper.Mode {
		case config.WhisperModeLocal:
			fmt.Printf("Using local whisper (%s) with model %s\n",
				cfg.Whisper.BinaryPath, cfg.Whisper.ModelPath)
			transcriber = whisper.NewLocalClient(
				cfg.Whisper.BinaryPath,
				cfg.Whisper.ModelPath,
				cfg.Whisper.Language,
			)

		default:
			if apiKey == "" {
				apiKey = os.Getenv("OPENAI_API_KEY")
			}
			if apiKey == "" {
				return fmt.Errorf("OpenAI API key required: set OPENAI_API_KEY env var or use --api-key")
			}
			transcriber = whisper.NewOpenAIClient(apiKey)
		}

		fmt.Println("Extracting audio and transcribing with Whisper...")
		startTime := time.Now()

		ctx := context.Background()

		result, err = transcriber.Transcribe(ctx, inputPath, whisper.Params{
			Model:       cfg.Whisper.Model,
			Language:    cfg.Whisper.Language,
			Prompt:      cfg.Whisper.Prompt,
			Temperature: cfg.Whisper.Temperature,
			BinaryPath:  cfg.Whisper.BinaryPath,
			ModelPath:   cfg.Whisper.ModelPath,
		})
		if err != nil {
			return fmt.Errorf("transcription failed: %w", err)
		}

		fmt.Printf("Transcription complete (%d words, %.1fs audio) in %s\n",
			len(result.Words), result.Duration, time.Since(startTime).Round(time.Millisecond))

		if err := whisper.SaveCache(inputPath, result); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write cache: %v\n", err)
		}
	}

	if len(result.Words) == 0 {
		return fmt.Errorf("no words found in transcription")
	}

	builder := render.NewASSBuilder(cfg)

	var frames []caption.Frame

	switch cfg.DisplayMode {
	case config.DisplayKaraoke:
		if cfg.Karaoke.Highlight == config.HighlightUnderline || cfg.Karaoke.Highlight == config.HighlightBoth {
			frames = caption.GroupWords(result.Words, cfg)
			builder.AddStyle("Default")
			fmt.Printf("Generated %d frames (%d words per frame) with word highlight\n", len(frames), cfg.WordsPerFrame)
			for _, f := range frames {
				builder.AddPosWordEvents(f)
			}
		} else {
			frames = caption.GroupWordsKaraoke(result.Words)
			builder.AddKaraokeStyle()
			fmt.Printf("Generated %d karaoke frames (%d words per group)\n", len(frames), cfg.WordsPerFrame)
			for _, f := range frames {
				builder.AddKaraokeEvent(f)
			}
		}
	default:
		frames = caption.GroupWords(result.Words, cfg)
		builder.AddStyle("Default")
		fmt.Printf("Generated %d frames (%d words per frame)\n", len(frames), cfg.WordsPerFrame)
		for _, f := range frames {
			builder.AddStaticEvent(f)
		}
	}

	assContent := builder.Build()

	tmpDir := filepath.Dir(cfg.OutputPath)
	assPath := filepath.Join(tmpDir, "capper_subtitles.ass")
	if err := render.WriteASSFile(assContent, assPath); err != nil {
		return fmt.Errorf("writing ASS file: %w", err)
	}
	defer func() {
		if cleanupErr := os.Remove(assPath); cleanupErr != nil {
			_ = cleanupErr
		}
	}()

	fmt.Println("Rendering final video with FFmpeg...")
	renderStart := time.Now()

	if err := render.RenderVideo(inputPath, assPath, cfg.OutputPath); err != nil {
		return fmt.Errorf("rendering video: %w", err)
	}

	fmt.Printf("Video rendered in %s\n", time.Since(renderStart).Round(time.Millisecond))
	fmt.Printf("Output: %s\n", cfg.OutputPath)

	return nil
}
