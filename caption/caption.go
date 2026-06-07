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

	limit := cfg.CharsPerFrame
	if limit < 1 {
		limit = 1
	}

	maxGap := float64(cfg.MaxGapMs) / 1000.0

	var frames []Frame
	var group []whisper.Word
	groupLen := 0 // rendered character length of the current group

	for i := range words {
		wordLen := wordRuneLen(words[i])

		// split on timing gap if enabled and group is non-empty
		if maxGap > 0 && len(group) > 0 {
			gap := words[i].Start - group[len(group)-1].End
			if gap > maxGap {
				frames = append(frames, buildFrame(group, len(frames)))
				group = nil
				groupLen = 0
			}
		}

		// split on character count: if adding this word would overflow the
		// limit and we already have something, flush first. A single word
		// longer than the limit still goes on its own line.
		if len(group) > 0 && groupLen+1+wordLen > limit {
			frames = append(frames, buildFrame(group, len(frames)))
			group = nil
			groupLen = 0
		}

		if len(group) > 0 {
			groupLen += 1 + wordLen // +1 for the joining space
		} else {
			groupLen = wordLen
		}
		group = append(group, words[i])
	}

	if len(group) > 0 {
		frames = append(frames, buildFrame(group, len(frames)))
	}

	return frames
}

// wordRuneLen is the number of characters a word contributes to a line, after
// the same trimming buildText applies.
func wordRuneLen(w whisper.Word) int {
	return len([]rune(strings.TrimSpace(w.Text)))
}

func GroupWordsKaraoke(words []whisper.Word, cfg *config.Config) []Frame {
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

	limit := cfg.CharsPerFrame
	if limit < 1 {
		limit = 1
	}
	merged := mergeKaraokeFrames(frames, limit)
	return merged
}

func mergeKaraokeFrames(frames []Frame, limit int) []Frame {
	if limit < 1 {
		limit = 1
	}

	var result []Frame
	var group []Frame
	groupLen := 0 // rendered character length of the current group

	for i := range frames {
		wordLen := len([]rune(frames[i].Text))

		// flush before overflowing the character limit (single over-long
		// words still go on their own line)
		if len(group) > 0 && groupLen+1+wordLen > limit {
			result = append(result, mergeFrameGroup(group, len(result)))
			group = nil
			groupLen = 0
		}

		if len(group) > 0 {
			groupLen += 1 + wordLen
		} else {
			groupLen = wordLen
		}
		group = append(group, frames[i])
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
