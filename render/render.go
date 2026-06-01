package render

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"capper/animation"
	"capper/caption"
	"capper/config"
	"capper/whisper"

	ffmpeg "github.com/u2takey/ffmpeg-go"
)

type ASSBuilder struct {
	cfg    *config.Config
	styles []string
	events []string
}

func NewASSBuilder(cfg *config.Config) *ASSBuilder {
	return &ASSBuilder{cfg: cfg}
}

func (b *ASSBuilder) AddStyle(name string) {
	cfg := b.cfg
	primaryColor := config.HexToASSBGR(cfg.Font.Color)
	secondaryColor := primaryColor
	outlineColor := config.HexToASSBGR(cfg.Stroke.Color)
	shadowColor := config.HexToASSBGR(cfg.Shadow.Color)

	bold := 0
	if cfg.Font.Bold {
		bold = -1
	}
	italic := 0
	if cfg.Font.Italic {
		italic = -1
	}
	underline := 0
	if cfg.Font.Underline {
		underline = -1
	}

	style := fmt.Sprintf(
		"Style: %s,%s,%d,%s,%s,%s,%s,%d,%d,%d,%d,100,100,0,0,1,%.0f,%.0f,%d,%d,%d,%d,1",
		name,
		cfg.Font.Family,
		cfg.Font.Size,
		primaryColor,
		secondaryColor,
		outlineColor,
		shadowColor,
		bold,
		italic,
		underline,
		0,
		cfg.Stroke.Width,
		cfg.Shadow.Depth,
		cfg.Position.Alignment,
		cfg.Position.MarginLeft,
		cfg.Position.MarginRight,
		cfg.Position.MarginTop+cfg.Position.MarginBottom,
	)

	b.styles = append(b.styles, style)
}

func (b *ASSBuilder) AddKaraokeStyle() {
	cfg := b.cfg
	activeColor := config.HexToASSBGR(cfg.Karaoke.ActiveColor)
	inactiveColor := config.HexToASSBGR(cfg.Karaoke.InactiveColor)
	outlineColor := config.HexToASSBGR(cfg.Stroke.Color)
	shadowColor := config.HexToASSBGR(cfg.Shadow.Color)

	bold := 0
	if cfg.Font.Bold {
		bold = -1
	}
	italic := 0
	if cfg.Font.Italic {
		italic = -1
	}
	underline := 0
	if cfg.Font.Underline {
		underline = -1
	}

	style := fmt.Sprintf(
		"Style: Karaoke,%s,%d,%s,%s,%s,%s,%d,%d,%d,%d,100,100,0,0,1,%.0f,%.0f,%d,%d,%d,%d,1",
		cfg.Font.Family,
		cfg.Font.Size,
		activeColor,
		inactiveColor,
		outlineColor,
		shadowColor,
		bold,
		italic,
		underline,
		0,
		cfg.Stroke.Width,
		cfg.Shadow.Depth,
		cfg.Position.Alignment,
		cfg.Position.MarginLeft,
		cfg.Position.MarginRight,
		cfg.Position.MarginTop+cfg.Position.MarginBottom,
	)

	b.styles = append(b.styles, style)
}

func (b *ASSBuilder) AddStaticEvent(frame caption.Frame) {
	tags := animation.GenerateOverrideTags(frame, b.cfg)
	start := secondsToASS(frame.Start)
	end := secondsToASS(frame.End)
	event := fmt.Sprintf("Dialogue: 0,%s,%s,Default,,0,0,0,,%s%s", start, end, tags, frame.Text)
	b.events = append(b.events, event)
}

func (b *ASSBuilder) AddKaraokeEvent(frame caption.Frame) {
	text := animation.GenerateKaraokeTags(frame, b.cfg)
	start := secondsToASS(frame.Start)
	end := secondsToASS(frame.End)
	event := fmt.Sprintf("Dialogue: 0,%s,%s,Karaoke,,0,0,0,,%s", start, end, text)
	b.events = append(b.events, event)
}

func (b *ASSBuilder) AddUnderlineEvent(frame caption.Frame) {
	text := animation.UnderlineTags(frame, b.cfg)
	start := secondsToASS(frame.Start)
	end := secondsToASS(frame.End)
	style := "Default"
	if b.cfg.Karaoke.Highlight == config.HighlightBoth {
		style = "Karaoke"
	}
	event := fmt.Sprintf("Dialogue: 0,%s,%s,%s,,0,0,0,,%s", start, end, style, text)
	b.events = append(b.events, event)
}

func (b *ASSBuilder) AddWordHighlightEvent(frame caption.Frame, wordIndex int, w whisper.Word) {
	text := animation.WordHighlightText(frame, wordIndex, b.cfg)
	start := secondsToASS(w.Start)
	end := secondsToASS(w.End)
	event := fmt.Sprintf("Dialogue: 0,%s,%s,Default,,0,0,0,,%s", start, end, text)
	b.events = append(b.events, event)
}

func (b *ASSBuilder) AddPosWordEvents(frame caption.Frame) {
	glowOverride := ""
	if b.cfg.Glow.Width > 0 {
		glowColor := config.HexToASSBGR(b.cfg.Glow.Color)
		glowOverride = fmt.Sprintf(`{\1a&HFF&\3a&H00&\3c%s\bord%.1f\blur%.1f\shad0}`,
			glowColor, b.cfg.Glow.Width, b.cfg.Glow.Blur)
	}

	for _, e := range animation.WholeLineHighlightEvents(frame, b.cfg) {
		start := secondsToASS(e.Start)
		end := secondsToASS(e.End)
		if glowOverride != "" {
			glowEv := fmt.Sprintf("Dialogue: 0,%s,%s,Default,,0,0,0,,%s%s", start, end, glowOverride, e.Text)
			b.events = append(b.events, glowEv)
		}
		ev := fmt.Sprintf("Dialogue: 1,%s,%s,Default,,0,0,0,,%s", start, end, e.Text)
		b.events = append(b.events, ev)
	}
}

func (b *ASSBuilder) Build() string {
	var sb strings.Builder

	sb.WriteString("[Script Info]\n")
	sb.WriteString("Title: Capper Subtitles\n")
	sb.WriteString("ScriptType: v4.00+\n")
	sb.WriteString("WrapStyle: 2\n")
	sb.WriteString("ScaledBorderAndShadow: yes\n")
	sb.WriteString(fmt.Sprintf("PlayResX: %d\n", b.cfg.ResX()))
	sb.WriteString(fmt.Sprintf("PlayResY: %d\n", b.cfg.ResY()))
	sb.WriteString("\n")

	sb.WriteString("[V4+ Styles]\n")
	sb.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	for _, s := range b.styles {
		sb.WriteString(s + "\n")
	}
	sb.WriteString("\n")

	sb.WriteString("[Events]\n")
	sb.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
	for _, e := range b.events {
		sb.WriteString(e + "\n")
	}

	return sb.String()
}

func secondsToASS(seconds float64) string {
	h := int(seconds) / 3600
	m := (int(seconds) % 3600) / 60
	s := int(seconds) % 60
	cs := int((seconds - float64(int(seconds))) * 100)
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, cs)
}

func WriteASSFile(content, path string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

func DetectResolution(videoPath string) (int, int, error) {
	probeData, err := ffmpeg.Probe(videoPath, ffmpeg.KwArgs{
		"v":              "error",
		"select_streams": "v:0",
		"show_entries":   "stream=width,height",
		"of":             "csv=p=0",
	})
	if err != nil {
		return 0, 0, fmt.Errorf("probing video: %w", err)
	}

	var w, h int
	_, err = fmt.Sscanf(strings.TrimSpace(probeData), "%d,%d", &w, &h)
	if err != nil {
		return 1920, 1080, nil
	}
	if w == 0 || h == 0 {
		return 1920, 1080, nil
	}
	return w, h, nil
}

func RenderVideo(inputVideo, assPath, outputPath string) error {
	absAss, err := filepath.Abs(assPath)
	if err != nil {
		return fmt.Errorf("resolving ASS path: %w", err)
	}

	return ffmpeg.Input(inputVideo).
		Output(outputPath, ffmpeg.KwArgs{
			"vf":  fmt.Sprintf("ass=%s", absAss),
			"c:a": "copy",
		}).
		OverWriteOutput().
		Run()
}
