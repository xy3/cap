package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Alignment int

const (
	AlignBottomLeft   Alignment = 1
	AlignBottomCenter Alignment = 2
	AlignBottomRight  Alignment = 3
	AlignMiddleLeft   Alignment = 4
	AlignMiddleCenter Alignment = 5
	AlignMiddleRight  Alignment = 6
	AlignTopLeft      Alignment = 7
	AlignTopCenter    Alignment = 8
	AlignTopRight     Alignment = 9
)

type AnimationType string

const (
	AnimFadeIn  AnimationType = "fade-in"
	AnimPopIn   AnimationType = "pop-in"
	AnimSlideIn AnimationType = "slide-in"
	AnimNone    AnimationType = "none"
)

type SlideDirection string

const (
	SlideFromLeft   SlideDirection = "left"
	SlideFromRight  SlideDirection = "right"
	SlideFromTop    SlideDirection = "top"
	SlideFromBottom SlideDirection = "bottom"
)

type DisplayMode string

const (
	DisplayStatic  DisplayMode = "static"
	DisplayKaraoke DisplayMode = "karaoke"
)

type FontConfig struct {
	Family     string `yaml:"family" json:"family"`
	Size       int    `yaml:"size" json:"size"`
	Color      string `yaml:"color" json:"color"`
	Bold       bool   `yaml:"bold" json:"bold"`
	Italic     bool   `yaml:"italic" json:"italic"`
	Underline  bool   `yaml:"underline" json:"underline"`
}

type StrokeConfig struct {
	Color string  `yaml:"color" json:"color"`
	Width float64 `yaml:"width" json:"width"`
	Blur  float64 `yaml:"blur" json:"blur"`
}

type ShadowConfig struct {
	Color string  `yaml:"color" json:"color"`
	Depth float64 `yaml:"depth" json:"depth"`
}

type GlowConfig struct {
	Color string  `yaml:"color" json:"color"`
	Width float64 `yaml:"width" json:"width"`
	Blur  float64 `yaml:"blur" json:"blur"`
}

type AnimationConfig struct {
	Type           AnimationType  `yaml:"type" json:"type"`
	DurationMs     int            `yaml:"duration_ms" json:"duration_ms"`
	SlideDirection SlideDirection `yaml:"slide_direction" json:"slide_direction"`
	SlideDistance  int            `yaml:"slide_distance" json:"slide_distance"`
}

type PositionConfig struct {
	Alignment    Alignment `yaml:"alignment" json:"alignment"`
	MarginLeft   int       `yaml:"margin_left" json:"margin_left"`
	MarginRight  int       `yaml:"margin_right" json:"margin_right"`
	MarginTop    int       `yaml:"margin_top" json:"margin_top"`
	MarginBottom int       `yaml:"margin_bottom" json:"margin_bottom"`
}

type WhisperMode string

const (
	WhisperModeAPI   WhisperMode = "api"
	WhisperModeLocal WhisperMode = "local"
)

type WhisperConfig struct {
	Mode        WhisperMode `yaml:"mode" json:"mode"`
	Model       string      `yaml:"model" json:"model"`
	Language    string      `yaml:"language" json:"language"`
	Prompt      string      `yaml:"prompt" json:"prompt"`
	Temperature float32     `yaml:"temperature" json:"temperature"`
	BinaryPath  string      `yaml:"binary_path" json:"binary_path"`
	ModelPath   string      `yaml:"model_path" json:"model_path"`
}

type HighlightMode string

const (
	HighlightColor     HighlightMode = "color"
	HighlightUnderline HighlightMode = "underline"
	HighlightBoth      HighlightMode = "both"
)

type KaraokeConfig struct {
	ActiveColor   string        `yaml:"active_color" json:"active_color"`
	InactiveColor string        `yaml:"inactive_color" json:"inactive_color"`
	Highlight     HighlightMode `yaml:"highlight" json:"highlight"`
	HighlightScale float64      `yaml:"highlight_scale" json:"highlight_scale"`
}

type Config struct {
	WordsPerFrame int              `yaml:"words_per_frame" json:"words_per_frame"`
	MaxGapMs      int              `yaml:"max_gap_ms" json:"max_gap_ms"`
	DisplayMode   DisplayMode      `yaml:"display_mode" json:"display_mode"`
	OutputPath    string           `yaml:"output_path" json:"output_path"`
	Resolution    string           `yaml:"resolution" json:"resolution"`
	Font          FontConfig       `yaml:"font" json:"font"`
	Stroke        StrokeConfig     `yaml:"stroke" json:"stroke"`
	Shadow        ShadowConfig     `yaml:"shadow" json:"shadow"`
	Glow          GlowConfig       `yaml:"glow" json:"glow"`
	Animation     AnimationConfig  `yaml:"animation" json:"animation"`
	Position      PositionConfig   `yaml:"position" json:"position"`
	Whisper       WhisperConfig    `yaml:"whisper" json:"whisper"`
	Karaoke       KaraokeConfig    `yaml:"karaoke" json:"karaoke"`
}

func DefaultConfig() *Config {
	return &Config{
		WordsPerFrame: 4,
		MaxGapMs:      0,
		DisplayMode:   DisplayStatic,
		OutputPath:    "output.mp4",
		Resolution:    "1920x1080",
		Font: FontConfig{
			Family: "Arial",
			Size:   48,
			Color:  "#FFFFFF",
			Bold:   false,
			Italic: false,
		},
		Stroke: StrokeConfig{
			Color: "#000000",
			Width: 2.0,
		},
		Shadow: ShadowConfig{
			Color: "#000000",
			Depth: 2.0,
		},
		Animation: AnimationConfig{
			Type:           AnimFadeIn,
			DurationMs:     300,
			SlideDirection: SlideFromBottom,
			SlideDistance:  50,
		},
		Position: PositionConfig{
			Alignment:    AlignBottomCenter,
			MarginLeft:   60,
			MarginRight:  60,
			MarginTop:    20,
			MarginBottom: 100,
		},
		Whisper: WhisperConfig{
			Mode:        WhisperModeAPI,
			Model:       "whisper-1",
			Language:    "en",
			Temperature: 0.0,
			BinaryPath:  "whisper",
			ModelPath:   "base",
		},
		Karaoke: KaraokeConfig{
			ActiveColor:    "#FFFF00",
			InactiveColor:  "#FFFFFF",
			Highlight:      HighlightColor,
			HighlightScale: 1.3,
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := DefaultConfig()

	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing YAML config: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing JSON config: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported config format: %s (use .yaml, .yml, or .json)", ext)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.WordsPerFrame < 1 {
		return fmt.Errorf("words_per_frame must be at least 1")
	}

	validAnimations := map[AnimationType]bool{
		AnimFadeIn: true, AnimPopIn: true, AnimSlideIn: true, AnimNone: true,
	}
	if !validAnimations[c.Animation.Type] {
		return fmt.Errorf("invalid animation type: %s", c.Animation.Type)
	}

	validModes := map[DisplayMode]bool{DisplayStatic: true, DisplayKaraoke: true}
	if !validModes[c.DisplayMode] {
		return fmt.Errorf("invalid display mode: %s", c.DisplayMode)
	}

	validDirs := map[SlideDirection]bool{
		SlideFromLeft: true, SlideFromRight: true,
		SlideFromTop: true, SlideFromBottom: true,
	}
	if !validDirs[c.Animation.SlideDirection] {
		return fmt.Errorf("invalid slide direction: %s", c.Animation.SlideDirection)
	}

	validModes2 := map[WhisperMode]bool{WhisperModeAPI: true, WhisperModeLocal: true}
	if !validModes2[c.Whisper.Mode] {
		return fmt.Errorf("invalid whisper mode: %s (use 'api' or 'local')", c.Whisper.Mode)
	}

	if c.Whisper.Mode == WhisperModeLocal && c.Whisper.ModelPath == "" {
		return fmt.Errorf("model_path is required for local whisper mode")
	}

	validHighlights := map[HighlightMode]bool{HighlightColor: true, HighlightUnderline: true, HighlightBoth: true}
	if !validHighlights[c.Karaoke.Highlight] {
		return fmt.Errorf("invalid karaoke highlight mode: %s (use 'color', 'underline', or 'both')", c.Karaoke.Highlight)
	}

	if c.Animation.DurationMs < 0 {
		return fmt.Errorf("animation duration must be non-negative")
	}

	if c.Font.Size < 1 {
		return fmt.Errorf("font size must be at least 1")
	}

	return nil
}

func (c *Config) ResX() int {
	var x, _ int
	fmt.Sscanf(c.Resolution, "%dx%d", &x, new(int))
	if x == 0 {
		x = 1920
	}
	return x
}

func (c *Config) ResY() int {
	var _, y int
	fmt.Sscanf(c.Resolution, "%dx%d", &y, new(int))
	if y == 0 {
		y = 1080
	}
	return y
}

func HexToASSBGR(hex string) string {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		hex = "FFFFFF"
	}
	b := hex[4:6]
	g := hex[2:4]
	r := hex[0:2]
	return "&H00" + b + g + r + "&"
}
