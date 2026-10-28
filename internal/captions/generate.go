// Package captions turns word timings into caption clips laid out on the
// editor timeline, preserving each clip's placement and trims.
package captions

import (
	"fmt"
	"sort"
	"strings"

	"autocut/internal/models"
)

// FromTranscript maps transcript words onto the timeline clips that use the
// transcript's source asset, so captions follow trims and ripple edits.
func FromTranscript(clips []models.Clip, transcript *models.Transcript, style models.CaptionStyle) []models.Clip {
	if transcript == nil || len(transcript.Words) == 0 {
		return nil
	}
	wordsPerChunk := style.WordsPerChunk
	if wordsPerChunk <= 0 {
		wordsPerChunk = 3
	}

	sorted := make([]models.Clip, 0, len(clips))
	for _, clip := range clips {
		if clip.AssetID == "" || clip.AssetID != transcript.AssetID {
			continue
		}
		if clip.Duration <= 0 {
			continue
		}
		sorted = append(sorted, clip)
	}
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].Start < sorted[b].Start })

	var out []models.Clip
	for _, clip := range sorted {
		var mapped []models.WordTiming
		for _, w := range transcript.Words {
			if w.End <= clip.In+0.01 || w.Start >= clip.Out-0.01 {
				continue
			}
			start := w.Start
			if start < clip.In {
				start = clip.In
			}
			end := w.End
			if end > clip.Out {
				end = clip.Out
			}
			if end <= start {
				continue
			}
			mapped = append(mapped, models.WordTiming{
				Word:  w.Word,
				Start: clip.Start + (start - clip.In),
				End:   clip.Start + (end - clip.In),
			})
		}
		out = append(out, chunkWords(mapped, wordsPerChunk)...)
	}

	for i := range out {
		out[i].ID = fmt.Sprintf("cap_%d_%d", i, len(out[i].Words))
	}
	return out
}

func chunkWords(words []models.WordTiming, wordsPerChunk int) []models.Clip {
	var clips []models.Clip
	var current []models.WordTiming

	flush := func() {
		if len(current) == 0 {
			return
		}
		start := current[0].Start
		end := current[len(current)-1].End
		if end-start < 0.2 {
			end = start + 0.2
		}
		text := make([]string, 0, len(current))
		for _, w := range current {
			text = append(text, w.Word)
		}
		clips = append(clips, models.Clip{
			Start:    start,
			Duration: end - start,
			In:       0,
			Out:      end - start,
			Text:     strings.Join(text, " "),
			Style:    nil,
			Volume:   1,
			Opacity:  1,
			Words:    current,
		})
		current = nil
	}

	for _, w := range words {
		if len(current) > 0 {
			prev := current[len(current)-1]
			gap := w.Start - prev.End
			if gap > 0.6 || w.Start-current[0].Start > 5.0 {
				flush()
			}
		}
		current = append(current, w)

		trimmed := strings.TrimRight(w.Word, `"'”’)`)
		endsSentence := strings.HasSuffix(trimmed, ".") || strings.HasSuffix(trimmed, "!") || strings.HasSuffix(trimmed, "?")
		if len(current) >= wordsPerChunk || endsSentence {
			flush()
		}
	}
	flush()
	return clips
}
