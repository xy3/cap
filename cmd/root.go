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

	output, err := RunPipeline(cfg, inputPath, apiKey, force, true)
	if err != nil {
		return err
	}

	fmt.Printf("Output: %s\n", output)
	return nil
}

func RunPipeline(cfg *config.Config, input, apiKeyArg string, forceRecache, verbose bool) (string, error) {
	resW, resH, err := render.DetectResolution(input)
	if err == nil && resW > 0 && resH > 0 {
		cfg.Resolution = fmt.Sprintf("%dx%d", resW, resH)
		if verbose {
			fmt.Printf("Detected video resolution: %dx%d\n", resW, resH)
		}
	}

	result, err := TranscribeOrCache(cfg, input, apiKeyArg, forceRecache, verbose)
	if err != nil {
		return "", err
	}

	if len(result.Words) == 0 {
		return "", fmt.Errorf("no words found in transcription")
	}

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

	if err := render.RenderVideo(input, assPath, cfg.OutputPath); err != nil {
		return "", fmt.Errorf("rendering video: %w", err)
	}

	if verbose {
		fmt.Printf("Video rendered in %s\n", time.Since(renderStart).Round(time.Millisecond))
	}

	return cfg.OutputPath, nil
}

func TranscribeOrCache(cfg *config.Config, input, apiKeyArg string, forceRecache, verbose bool) (*whisper.TranscriptionResult, error) {
	if !forceRecache {
		if cached, err := whisper.LoadCache(input); err == nil {
			if verbose {
				fmt.Printf("Using cached transcription (%s)\n", whisper.CachePath(input))
			}
			return cached, nil
		}
	}

	var transcriber whisper.Transcriber
	switch cfg.Whisper.Mode {
	case config.WhisperModeLocal:
		if verbose {
			fmt.Printf("Using local whisper (%s) with model %s\n", cfg.Whisper.BinaryPath, cfg.Whisper.ModelPath)
		}
		transcriber = whisper.NewLocalClient(cfg.Whisper.BinaryPath, cfg.Whisper.ModelPath, cfg.Whisper.Language)
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

	result, err := transcriber.Transcribe(context.Background(), input, whisper.Params{
		Model:       cfg.Whisper.Model,
		Language:    cfg.Whisper.Language,
		Prompt:      cfg.Whisper.Prompt,
		Temperature: cfg.Whisper.Temperature,
		BinaryPath:  cfg.Whisper.BinaryPath,
		ModelPath:   cfg.Whisper.ModelPath,
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
