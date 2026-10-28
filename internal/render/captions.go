package render

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"autocut/internal/models"
)

// writeASS renders the caption track into a single ASS file.
func writeASS(path string, clips []models.Clip, style models.CaptionStyle, width, height int) error {
	if len(clips) == 0 {
		return nil
	}
	scale := float64(height) / 1080.0
	if scale <= 0 {
		scale = 1
	}
	fontSize := style.FontSize
	if fontSize <= 0 {
		fontSize = 72
	}
	fontSize = int(float64(fontSize) * scale)
	outline := style.StrokeWidth * scale
	shadow := style.Shadow * scale
	opacity := clamp01(style.Opacity)
	alpha := 255 - int(opacity*255)
	primary := assColor(style.Color, alpha)
	highlight := assColor(style.HighlightColor, alpha)
	outlineColor := assColor(style.StrokeColor, alpha)
	shadowColor := assColor(style.ShadowColor, alpha)
	fontName := style.FontFamily
	if strings.TrimSpace(fontName) == "" {
		fontName = "Arial"
	}
	bold := 0
	if style.Bold {
		bold = -1
	}
	italic := 0
	if style.Italic {
		italic = -1
	}
	positionY := clamp01(style.PositionY)
	if positionY <= 0 {
		positionY = 0.82
	}
	if positionY > 0.98 {
		positionY = 0.98
	}
	posX := width / 2
	posY := int(float64(height) * positionY)

	sorted := make([]models.Clip, 0, len(clips))
	for _, clip := range clips {
		if strings.TrimSpace(clip.Text) == "" && len(clip.Words) == 0 {
			continue
		}
		if clip.Duration <= 0 {
			continue
		}
		sorted = append(sorted, clip)
	}
	if len(sorted) == 0 {
		return nil
	}
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].Start < sorted[b].Start })

	var sb strings.Builder
	sb.WriteString("[Script Info]\n")
	sb.WriteString("Title: AutoCut Captions\n")
	sb.WriteString("ScriptType: v4.00+\n")
	sb.WriteString("WrapStyle: 0\n")
	sb.WriteString("ScaledBorderAndShadow: yes\n")
	sb.WriteString(fmt.Sprintf("PlayResX: %d\nPlayResY: %d\n\n", width, height))

	sb.WriteString("[V4+ Styles]\n")
	sb.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	sb.WriteString(fmt.Sprintf("Style: Cap,%s,%d,%s,%s,%s,%s,%d,%d,0,0,100,100,0,0,1,%.1f,%.1f,5,60,60,0,1\n\n",
		fontName, fontSize, primary, highlight, outlineColor, shadowColor, bold, italic, outline, shadow))

	sb.WriteString("[Events]\n")
	sb.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")

	for _, clip := range sorted {
		words := clip.Words
		if len(words) > 0 && style.Highlight {
			for active, word := range words {
				start := word.Start
				end := word.End
				if end <= start {
					end = start + 0.15
				}
				if start < clip.Start {
					start = clip.Start
				}
				if end > clip.End() {
					end = clip.End()
				}
				if end <= start {
					continue
				}
				var parts []string
				for idx, w := range words {
					token := escapeASSText(w.Word)
					if style.Uppercase {
						token = strings.ToUpper(token)
					}
					if idx == active {
						parts = append(parts, fmt.Sprintf("{\\c%s&\\fscx108\\fscy108}%s{\\c%s&\\fscx100\\fscy100}",
							strings.TrimSuffix(highlight, "&")+"&", token, strings.TrimSuffix(primary, "&")+"&"))
					} else {
						parts = append(parts, token)
					}
				}
				text := strings.Join(parts, " ")
				sb.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,Cap,,0,0,0,,{\\pos(%d,%d)}%s\n",
					formatASSTime(start), formatASSTime(end), posX, posY, text))
			}
			continue
		}

		text := clip.Text
		if text == "" {
			var tokens []string
			for _, w := range words {
				tokens = append(tokens, w.Word)
			}
			text = strings.Join(tokens, " ")
		}
		if style.Uppercase {
			text = strings.ToUpper(text)
		}
		sb.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,Cap,,0,0,0,,{\\pos(%d,%d)}%s\n",
			formatASSTime(clip.Start), formatASSTime(clip.End()), posX, posY, escapeASSText(text)))
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func assColor(hex string, alpha int) string {
	r, g, b, ok := parseHexColor(hex)
	if !ok {
		r, g, b = 255, 255, 255
	}
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 255 {
		alpha = 255
	}
	return fmt.Sprintf("&H%02X%02X%02X%02X", alpha, b, g, r)
}

func parseHexColor(value string) (int, int, int, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "#")
	value = strings.TrimPrefix(value, "0x")
	value = strings.TrimPrefix(value, "0X")
	switch len(value) {
	case 3:
		value = string([]byte{value[0], value[0], value[1], value[1], value[2], value[2]})
	case 6:
	case 8:
		value = value[0:6]
	default:
		return 0, 0, 0, false
	}
	n, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(n>>16) & 0xFF, int(n>>8) & 0xFF, int(n) & 0xFF, true
}

func formatASSTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	h := int(sec / 3600)
	m := int((sec - float64(h*3600)) / 60)
	s := sec - float64(h*3600) - float64(m*60)
	return fmt.Sprintf("%d:%02d:%05.2f", h, m, s)
}

func escapeASSText(text string) string {
	text = strings.ReplaceAll(text, "{", "")
	text = strings.ReplaceAll(text, "}", "")
	text = strings.ReplaceAll(text, "\\", "")
	text = strings.ReplaceAll(text, "\r", "")
	text = strings.ReplaceAll(text, "\n", "\\N")
	return strings.TrimSpace(text)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
