package captions

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"autocut/internal/models"
)

// SubtitleStyle defines typography and colors for ASS subtitles.
type SubtitleStyle struct {
	FontName       string // e.g. "Segoe UI Semibold" or "Arial"
	FontSize       int    // 64
	PrimaryColor   string // &H00FFFFFF (White BGR)
	HighlightColor string // &H0012E6F5 (Gold/Amber BGR: &H00 + BB + GG + RR)
	OutlineColor   string // &H00000000 (Black)
	Outline        float64
	Shadow         float64
	MarginV        int // Vertical margin. Unused when Alignment is centered.
	Alignment      int // ASS alignment numpad: 5 = center
}

// DefaultTxShDStyle is the CapCut-style auto caption: one short centered line,
// thick outline, and the spoken word popping yellow.
func DefaultTxShDStyle() SubtitleStyle {
	return SubtitleStyle{
		FontName:       "Arial",
		FontSize:       72,
		PrimaryColor:   "&H00FFFFFF",
		HighlightColor: "&H0000FFFF", // Yellow in ASS BGR
		OutlineColor:   "&H00000000",
		Outline:        4.0,
		Shadow:         1.5,
		MarginV:        0,
		Alignment:      5,
	}
}

// GenerateDynamicAssSubtitles generates an ASS subtitle file with dynamic active-word highlighting.
func GenerateDynamicAssSubtitles(words []models.WordTiming, keepIntervals []models.KeepInterval, outputPath string, style SubtitleStyle) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	// 1. Recalculate word timestamps to match the compacted cut timeline
	compactedWords := recalculateTimestamps(words, keepIntervals)
	if len(compactedWords) == 0 {
		return fmt.Errorf("no words fall within keep intervals")
	}

	// 2. Chunk words into short 2-3 word phrases, one centered line at a time.
	type WordGroup struct {
		Words []models.WordTiming
		Start float64
		End   float64
	}

	var groups []WordGroup
	chunkSize := 3
	for i := 0; i < len(compactedWords); i += chunkSize {
		end := i + chunkSize
		if end > len(compactedWords) {
			end = len(compactedWords)
		}
		chunk := compactedWords[i:end]
		groups = append(groups, WordGroup{
			Words: chunk,
			Start: chunk[0].Start,
			End:   chunk[len(chunk)-1].End,
		})
	}

	// 3. Build ASS header and events
	var sb strings.Builder
	sb.WriteString("[Script Info]\n")
	sb.WriteString("Title: TxShD AutoCut Dynamic Captions\n")
	sb.WriteString("ScriptType: v4.00+\n")
	sb.WriteString("WrapStyle: 0\n")
	sb.WriteString("ScaledBorderAndShadow: yes\n")
	sb.WriteString("PlayResX: 1920\n")
	sb.WriteString("PlayResY: 1080\n\n")

	sb.WriteString("[V4+ Styles]\n")
	sb.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	alignment := style.Alignment
	if alignment == 0 {
		alignment = 5
	}
	sb.WriteString(fmt.Sprintf("Style: Default,%s,%d,%s,%s,%s,&H80000000,1,0,0,0,100,100,0,0,1,%.1f,%.1f,%d,40,40,%d,1\n\n",
		style.FontName, style.FontSize, style.PrimaryColor, style.HighlightColor, style.OutlineColor, style.Outline, style.Shadow, alignment, style.MarginV))

	sb.WriteString("[Events]\n")
	sb.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")

	// 4. Generate dialogue events with word highlighting
	for _, group := range groups {
		for activeIdx, activeWord := range group.Words {
			startTime := formatASSTime(activeWord.Start)
			endTime := formatASSTime(activeWord.End)

			// Construct line: all words in white, but activeWord highlighted
			var lineParts []string
			for wIdx, w := range group.Words {
				if wIdx == activeIdx {
					// Highlighted active word with subtle scale up
					lineParts = append(lineParts, fmt.Sprintf("{\\c%s\\fscx110\\fscy110}%s{\\c%s\\fscx100\\fscy100}",
						style.HighlightColor, escapeASSText(w.Word), style.PrimaryColor))
				} else {
					lineParts = append(lineParts, escapeASSText(w.Word))
				}
			}
			dialogueLine := strings.Join(lineParts, " ")
			sb.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,Default,,0,0,0,,%s\n", startTime, endTime, dialogueLine))
		}
	}

	return os.WriteFile(outputPath, []byte(sb.String()), 0644)
}

// recalculateTimestamps shifts word timestamps to match the new cut video timeline.
func recalculateTimestamps(words []models.WordTiming, keepIntervals []models.KeepInterval) []models.WordTiming {
	var result []models.WordTiming

	for _, w := range words {
		// Find which keep interval this word belongs to
		var cumulativeOffset float64 = 0
		var found bool

		for _, interval := range keepIntervals {
			dur := interval.End - interval.Start
			if w.Start >= interval.Start && w.Start <= interval.End {
				// Word falls in this interval
				shiftedStart := cumulativeOffset + (w.Start - interval.Start)
				shiftedEnd := cumulativeOffset + (w.End - interval.Start)
				if shiftedEnd < shiftedStart+0.1 {
					shiftedEnd = shiftedStart + 0.1
				}
				result = append(result, models.WordTiming{
					Word:       w.Word,
					Start:      shiftedStart,
					End:        shiftedEnd,
					Confidence: w.Confidence,
				})
				found = true
				break
			}
			cumulativeOffset += dur
		}

		if !found {
			// Word fell inside a cut interval, skip
			continue
		}
	}

	return result
}

func formatASSTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	h := int(sec / 3600)
	m := int((sec - float64(h*3600)) / 60)
	s := sec - float64(h*3600) - float64(m*60)
	// ASS format: H:MM:SS.CC (centiseconds, 2 digits)
	return fmt.Sprintf("%d:%02d:%05.2f", h, m, s)
}

func escapeASSText(text string) string {
	text = strings.ReplaceAll(text, "{", "")
	text = strings.ReplaceAll(text, "}", "")
	text = strings.ReplaceAll(text, "\\", "")
	return strings.TrimSpace(text)
}
