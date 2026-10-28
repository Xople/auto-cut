package stt

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"autocut/internal/models"
)

type WhisperRunner struct {
	cliPath   string
	modelPath string
}

func NewWhisperRunner(cliPath, modelPath string) *WhisperRunner {
	return &WhisperRunner{
		cliPath:   cliPath,
		modelPath: modelPath,
	}
}

// WhisperOutputJSON represents the JSON output produced by whisper.cpp with word timestamps.
type WhisperOutputJSON struct {
	Transcription []struct {
		Text       string `json:"text"`
		Timestamps struct {
			From string `json:"from"`
			To   string `json:"to"`
		} `json:"timestamps"`
		Offsets struct {
			From int64 `json:"from"` // in ms
			To   int64 `json:"to"`   // in ms
		} `json:"offsets"`
		Tokens []struct {
			Text       string  `json:"text"`
			P          float64 `json:"p"` // probability / confidence
			Timestamps struct {
				From string `json:"from"`
				To   string `json:"to"`
			} `json:"timestamps"`
			Offsets struct {
				From int64 `json:"from"`
				To   int64 `json:"to"`
			} `json:"offsets"`
		} `json:"tokens"`
	} `json:"transcription"`
}

// Transcribe executes whisper.cpp and returns structured word timings.
func (w *WhisperRunner) Transcribe(audioPath string, totalDuration float64) (*models.Transcript, error) {
	// Check if whisper CLI binary exists on disk
	if _, err := os.Stat(w.cliPath); os.IsNotExist(err) {
		// Also check system PATH for whisper-cli or whisper
		if path, err := exec.LookPath("whisper-cli"); err == nil {
			w.cliPath = path
		} else if path, err := exec.LookPath("whisper"); err == nil {
			w.cliPath = path
		} else {
			// If binary not found, return an informative fallback for local dev/testing
			return generateDevFallbackTranscript(audioPath, totalDuration)
		}
	}

	// Prepare output base path (whisper.cpp appends .json)
	tempDir := filepath.Dir(audioPath)
	outputBase := filepath.Join(tempDir, "whisper_out")
	outputJSON := outputBase + ".json"
	_ = os.Remove(outputJSON) // Clean previous

	args := []string{
		"-m", w.modelPath,
		"-f", audioPath,
		"--language", "id", // Indonesian default, handles EN jargon
		"--dtw",            // Dynamic Time Warping for word-level precision
		"-oj",              // Output JSON
		"-of", outputBase,  // Output file prefix
	}

	cmd := exec.Command(w.cliPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("whisper execution failed: %w, log: %s", err, string(output))
	}

	// Read generated JSON file
	jsonData, err := os.ReadFile(outputJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to read whisper json output: %w", err)
	}

	var wOut WhisperOutputJSON
	if err := json.Unmarshal(jsonData, &wOut); err != nil {
		return nil, fmt.Errorf("failed to parse whisper json output: %w", err)
	}

	var words []models.WordTiming
	var fullTextBuilder strings.Builder

	for _, seg := range wOut.Transcription {
		for _, tok := range seg.Tokens {
			cleanedWord := strings.TrimSpace(tok.Text)
			if cleanedWord == "" || strings.HasPrefix(cleanedWord, "[_") {
				continue
			}
			startSec := float64(tok.Offsets.From) / 1000.0
			endSec := float64(tok.Offsets.To) / 1000.0

			words = append(words, models.WordTiming{
				Word:       cleanedWord,
				Start:      startSec,
				End:        endSec,
				Confidence: tok.P,
			})
			fullTextBuilder.WriteString(cleanedWord + " ")
		}
	}

	return &models.Transcript{
		FullText: strings.TrimSpace(fullTextBuilder.String()),
		Words:    words,
		Duration: totalDuration,
		Language: "id",
	}, nil
}

// generateDevFallbackTranscript creates a mock transcript when whisper-cli is not yet downloaded
func generateDevFallbackTranscript(audioPath string, duration float64) (*models.Transcript, error) {
	sampleSentences := []string{
		"Halo semuanya selamat datang kembali di channel TxShD.",
		"Hari ini kita akan membahas salah satu misteri terbesar dalam game.",
		"Um, eh salah, maksud saya mari kita lihat mekanik intinya.",
		"Banyak player tidak menyadari ada rahasia tersembunyi di balik level desain ini.",
		"Kenapa developer memutuskan membuat gameplay seperti ini?",
	}

	fullText := strings.Join(sampleSentences, " ")
	tokens := strings.Fields(fullText)

	var words []models.WordTiming
	step := duration / float64(len(tokens)+2)
	if step <= 0 {
		step = 0.35
	}

	for i, tok := range tokens {
		start := float64(i)*step + 0.2
		end := start + (step * 0.8)
		words = append(words, models.WordTiming{
			Word:       tok,
			Start:      start,
			End:        end,
			Confidence: 0.95,
		})
	}

	return &models.Transcript{
		FullText: fullText,
		Words:    words,
		Duration: duration,
		Language: "id",
		Mock:     true,
	}, nil
}
