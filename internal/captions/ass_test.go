package captions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autocut/internal/models"
)

func TestRecalculateTimestamps(t *testing.T) {
	words := []models.WordTiming{
		{Word: "Halo", Start: 1.0, End: 1.4},
		{Word: "teman-teman", Start: 1.5, End: 2.2},
		// Silence/cut from 2.2 to 5.0
		{Word: "hari", Start: 5.2, End: 5.5},
		{Word: "ini", Start: 5.6, End: 5.9},
	}

	keepIntervals := []models.KeepInterval{
		{Start: 0.9, End: 2.3}, // duration = 1.4s
		{Start: 5.1, End: 6.0}, // duration = 0.9s
	}

	compacted := recalculateTimestamps(words, keepIntervals)
	if len(compacted) != 4 {
		t.Fatalf("expected 4 words, got %d", len(compacted))
	}

	// Word "Halo" was at 1.0 within interval [0.9, 2.3] -> offset = 0.0 + (1.0 - 0.9) = 0.1
	if compacted[0].Word != "Halo" {
		t.Errorf("expected Halo, got %s", compacted[0].Word)
	}

	// Word "hari" was at 5.2 within interval [5.1, 6.0]
	// Cumulative offset of interval 1 is 1.4s
	// Shifted start should be 1.4 + (5.2 - 5.1) = 1.5s
	if compacted[2].Start < 1.49 || compacted[2].Start > 1.51 {
		t.Errorf("expected compacted start around 1.5, got %.3f", compacted[2].Start)
	}
}

func TestGenerateDynamicAssSubtitles(t *testing.T) {
	tmpDir := t.TempDir()
	assPath := filepath.Join(tmpDir, "test.ass")

	words := []models.WordTiming{
		{Word: "Halo", Start: 0.5, End: 0.8},
		{Word: "TxShD", Start: 0.9, End: 1.3},
		{Word: "di", Start: 1.4, End: 1.6},
		{Word: "sini", Start: 1.7, End: 2.0},
		{Word: "rahasia", Start: 9.0, End: 9.4},
	}

	keepIntervals := []models.KeepInterval{
		{Start: 0.0, End: 3.0},
	}

	err := GenerateDynamicAssSubtitles(words, keepIntervals, assPath, DefaultTxShDStyle())
	if err != nil {
		t.Fatalf("failed to generate ASS: %v", err)
	}

	data, err := os.ReadFile(assPath)
	if err != nil {
		t.Fatalf("failed to read generated ASS: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "[Script Info]") {
		t.Errorf("missing Script Info")
	}
	if !strings.Contains(content, "TxShD") {
		t.Errorf("missing word TxShD in events")
	}
	if !strings.Contains(content, "\\fscx110") {
		t.Errorf("missing CapCut active-word scale tag")
	}
	if !strings.Contains(content, "&H0000FFFF") {
		t.Errorf("missing yellow active-word color")
	}
	if !strings.Contains(content, ",5,") {
		t.Errorf("expected center alignment 5, got style line without it")
	}
	if strings.Contains(content, "rahasia") {
		t.Errorf("word outside keep intervals should be omitted")
	}

	var dialogues []string
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "Dialogue:") {
			dialogues = append(dialogues, line)
		}
	}
	if len(dialogues) != 4 {
		t.Fatalf("expected 4 dialogue events for 4 kept words, got %d", len(dialogues))
	}
	if strings.Contains(dialogues[0], "sini") {
		t.Errorf("first phrase should stay within 3 words, got %s", dialogues[0])
	}
}
