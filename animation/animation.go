package animation

import (
	"fmt"
	"strings"

	"capper/caption"
	"capper/config"
)

func GenerateOverrideTags(frame caption.Frame, cfg *config.Config) string {
	var tags []string

	dur := cfg.Animation.DurationMs
	if dur <= 0 {
		dur = 1
	}

	switch cfg.Animation.Type {
	case config.AnimFadeIn:
		tags = append(tags, fadeInTags(dur)...)
	case config.AnimPopIn:
		tags = append(tags, popInTags(dur)...)
	case config.AnimSlideIn:
		tags = append(tags, slideInTags(cfg, dur)...)
	case config.AnimNone:
		tags = append(tags, "")
	}

	if len(tags) == 0 {
		return ""
	}

	return "{" + strings.Join(tags, "") + "}"
}

func GenerateKaraokeTags(frame caption.Frame, cfg *config.Config) string {
	var parts []string

	for i, w := range frame.Words {
		durationCs := int((w.End - w.Start) * 100)
		if durationCs < 1 {
			durationCs = 1
		}

		if i == len(frame.Words)-1 {
			parts = append(parts, fmt.Sprintf(`{\kf%d}%s`, durationCs, w.Text))
		} else {
			parts = append(parts, fmt.Sprintf(`{\k%d}%s`, durationCs, w.Text))
		}
	}

	animTags := GenerateOverrideTags(frame, cfg)

	if animTags != "" && len(parts) > 0 {
		parts[0] = animTags + parts[0]
	}

	return strings.Join(parts, "")
}

func UnderlineTags(frame caption.Frame, cfg *config.Config) string {
	return `{\u1}` + frame.Text
}

func WordHighlightText(frame caption.Frame, activeIndex int, cfg *config.Config) string {
	highlightSize := int(float64(cfg.Font.Size) * cfg.Karaoke.HighlightScale)
	var parts []string
	for i, w := range frame.Words {
		if i == activeIndex {
			parts = append(parts, fmt.Sprintf(`{\alpha&H00&\u1\fs%d}%s`, highlightSize, w.Text))
		} else {
			parts = append(parts, fmt.Sprintf(`{\alpha&HFF&}%s`, w.Text))
		}
	}
	return strings.Join(parts, " ")
}

func WholeLineHighlightEvents(frame caption.Frame, cfg *config.Config) []WordEvent {
	if len(frame.Words) == 0 {
		return nil
	}

	scale := cfg.Karaoke.HighlightScale
	if scale <= 0 {
		scale = 1.0
	}
	scalePct := int(scale * 100)

	blurPrefix := ""
	if cfg.Stroke.Blur > 0 {
		blurPrefix = fmt.Sprintf(`{\blur%.1f}`, cfg.Stroke.Blur)
	}

	peakPct := scalePct + 8
	bounceUp := 90
	bounceDown := 180

	buildLine := func(activeIdx int) string {
		var parts []string
		for i, w := range frame.Words {
			t := strings.TrimSpace(w.Text)
			if i == activeIdx {
				active := fmt.Sprintf(
					`{\u1\fscx100\fscy100\t(0,%d,\fscx%d\fscy%d)\t(%d,%d,\fscx%d\fscy%d)}%s{\u0\fscx100\fscy100}`,
					bounceUp, peakPct, peakPct,
					bounceUp, bounceDown, scalePct, scalePct,
					t,
				)
				parts = append(parts, active)
			} else {
				parts = append(parts, t)
			}
		}
		return blurPrefix + strings.Join(parts, " ")
	}

	var events []WordEvent
	n := len(frame.Words)
	for i, w := range frame.Words {
		start := w.Start
		if i == 0 {
			start = frame.Start
		}
		var end float64
		if i == n-1 {
			end = frame.End
		} else {
			end = frame.Words[i+1].Start
		}
		events = append(events, WordEvent{
			Start: start,
			End:   end,
			Text:  buildLine(i),
		})
	}


	if len(events) > 0 {
		if anim := GenerateOverrideTags(frame, cfg); anim != "" {
			events[0].Text = anim + events[0].Text
		}
	}

	return events
}

type WordEvent struct {
	Start float64
	End   float64
	Text  string
}

func fadeInTags(durationMs int) []string {
	return []string{fmt.Sprintf(`\fad(%d,0)`, durationMs)}
}

func popInTags(durationMs int) []string {
	half := durationMs / 2
	var tags []string

	tags = append(tags, fmt.Sprintf(`\fad(%d,0)`, durationMs/4+1))

	tags = append(tags, `\fscx0\fscy0`)
	tags = append(tags, fmt.Sprintf(`\t(0,%d,1,\fscx115\fscy115)`, half))
	tags = append(tags, fmt.Sprintf(`\t(%d,%d,1,\fscx100\fscy100)`, half, durationMs))

	return tags
}

func slideInTags(cfg *config.Config, durationMs int) []string {
	resX := cfg.ResX()
	resY := cfg.ResY()

	var x1, y1, x2, y2 int

	x2, y2 = positionToPixel(cfg)

	switch cfg.Animation.SlideDirection {
	case config.SlideFromLeft:
		x1 = -cfg.Animation.SlideDistance
		y1 = y2
	case config.SlideFromRight:
		x1 = resX + cfg.Animation.SlideDistance
		y1 = y2
	case config.SlideFromTop:
		x1 = x2
		y1 = -cfg.Animation.SlideDistance
	case config.SlideFromBottom:
		x1 = x2
		y1 = resY + cfg.Animation.SlideDistance
	default:
		x1 = x2
		y1 = y2
	}

	return []string{
		fmt.Sprintf(`\move(%d,%d,%d,%d,0,%d)`, x1, y1, x2, y2, durationMs),
	}
}

func positionToPixel(cfg *config.Config) (x, y int) {
	resX := cfg.ResX()
	resY := cfg.ResY()

	align := cfg.Position.Alignment

	switch align {
	case config.AlignBottomLeft, config.AlignMiddleLeft, config.AlignTopLeft:
		x = cfg.Position.MarginLeft
	case config.AlignBottomRight, config.AlignMiddleRight, config.AlignTopRight:
		x = resX - cfg.Position.MarginRight
	default:
		x = resX / 2
	}

	switch align {
	case config.AlignTopLeft, config.AlignTopCenter, config.AlignTopRight:
		y = cfg.Position.MarginTop
	case config.AlignBottomLeft, config.AlignBottomCenter, config.AlignBottomRight:
		y = resY - cfg.Position.MarginBottom
	default:
		y = resY / 2
	}

	return x, y
}
