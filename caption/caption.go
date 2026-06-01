package caption

import (
	"strings"

	"capper/config"
	"capper/whisper"
)

type Frame struct {
	Words []whisper.Word
	Text  string
	Start float64
	End   float64
	Index int
}

func GroupWords(words []whisper.Word, cfg *config.Config) []Frame {
	if len(words) == 0 {
		return nil
	}

	limit := cfg.WordsPerFrame
	if limit < 1 {
		limit = 1
	}

	maxGap := float64(cfg.MaxGapMs) / 1000.0

	var frames []Frame
	var group []whisper.Word

	for i := range words {
		// split on timing gap if enabled and group is non-empty
		if maxGap > 0 && len(group) > 0 {
			gap := words[i].Start - group[len(group)-1].End
			if gap > maxGap {
				frames = append(frames, buildFrame(group, len(frames)))
				group = nil
			}
		}

		group = append(group, words[i])

		// split on word count
		if len(group) >= limit {
			frames = append(frames, buildFrame(group, len(frames)))
			group = nil
		}
	}

	if len(group) > 0 {
		frames = append(frames, buildFrame(group, len(frames)))
	}

	return frames
}

func GroupWordsKaraoke(words []whisper.Word) []Frame {
	if len(words) == 0 {
		return nil
	}

	var frames []Frame
	for i, w := range words {
		// Clean up the word text — Whisper sometimes returns leading spaces
		text := strings.TrimSpace(w.Text)
		if text == "" {
			continue
		}
		frames = append(frames, Frame{
			Words: []whisper.Word{w},
			Text:  text,
			Start: w.Start,
			End:   w.End,
			Index: i,
		})
	}

	if len(frames) == 0 {
		return nil
	}

	merged := mergeKaraokeFrames(frames, 3)
	return merged
}

func mergeKaraokeFrames(frames []Frame, limit int) []Frame {
	if limit < 1 {
		limit = 3
	}

	var result []Frame
	var group []Frame

	for i := range frames {
		group = append(group, frames[i])

		if len(group) >= limit {
			result = append(result, mergeFrameGroup(group, len(result)))
			group = nil
		}
	}

	if len(group) > 0 {
		result = append(result, mergeFrameGroup(group, len(result)))
	}

	return result
}

func mergeFrameGroup(group []Frame, index int) Frame {
	if len(group) == 0 {
		return Frame{}
	}

	var allWords []whisper.Word
	for _, f := range group {
		allWords = append(allWords, f.Words...)
	}

	return Frame{
		Words: allWords,
		Text:  buildText(allWords),
		Start: group[0].Start,
		End:   group[len(group)-1].End,
		Index: index,
	}
}

func buildFrame(group []whisper.Word, index int) Frame {
	return Frame{
		Words: group,
		Text:  buildText(group),
		Start: group[0].Start,
		End:   group[len(group)-1].End,
		Index: index,
	}
}

func buildText(words []whisper.Word) string {
	var parts []string
	for _, w := range words {
		parts = append(parts, strings.TrimSpace(w.Text))
	}
	return strings.Join(parts, " ")
}
