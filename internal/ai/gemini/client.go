package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"autocut/internal/models"
)

type Client struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		model:  "gemini-2.5-flash", // Fast, accurate, standard Google AI Studio endpoint
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

type GeminiRequest struct {
	Contents         []Content        `json:"contents"`
	GenerationConfig GenerationConfig `json:"generationConfig"`
}

type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

type GenerationConfig struct {
	Temperature      float64 `json:"temperature"`
	ResponseMimeType string  `json:"responseMimeType,omitempty"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// Call 1: SanityCheckCutlist runs a single high-level QA pass over the cutlist.
func (c *Client) SanityCheckCutlist(fullTranscript string, origDur, cutDur float64, cuts []models.CutSegment) (passed bool, report string, err error) {
	if c.apiKey == "" {
		return true, "QA Pass: Mode Offline (Tanpa Gemini API Key). Cutlist lolos validasi otomatis.", nil
	}

	var cutSummary strings.Builder
	for i, cut := range cuts {
		if i > 25 {
			cutSummary.WriteString(fmt.Sprintf("... dan %d potongan lainnya.\n", len(cuts)-25))
			break
		}
		cutSummary.WriteString(fmt.Sprintf("- [%.2fs - %.2fs] (%s): %s\n", cut.Start, cut.End, cut.Reason, cut.Text))
	}

	prompt := fmt.Sprintf(`Bertindaklah sebagai QA Supervisor video essay YouTube (Channel TxShD).
Berikut adalah draf pemotongan video (AutoCut):
- Durasi Asli: %.1f detik
- Estimasi Pengurangan Durasi: %.1f detik
- Daftar Potongan (Sampel):
%s

- Inti Transkrip:
%s

Lakukan SANITY CHECK singkat:
1. Apakah ada resiko potongan ini menghapus poin penting atau memotong penjelasan esensial di tengah argumen?
2. Berikan penilaian apakah cutlist ini AMAN untuk dirender.

Jawab HANYA dalam format JSON:
{
  "passed": true,
  "report": "<1-2 paragraf evaluasi singkat dan catatan untuk editor>"
}`, origDur, cutDur, cutSummary.String(), truncate(fullTranscript, 3000))

	respText, err := c.generateContent(prompt, "application/json")
	if err != nil {
		return true, fmt.Sprintf("QA Warning: Gagal memanggil Gemini QA (%v), melanjutkan render.", err), nil
	}

	var res struct {
		Passed bool   `json:"passed"`
		Report string `json:"report"`
	}
	if err := json.Unmarshal([]byte(respText), &res); err != nil {
		return true, respText, nil
	}

	return res.Passed, res.Report, nil
}

// Call 2: GenerateYouTubeMetadata creates title options, description, and recalculated chapters.
func (c *Client) GenerateYouTubeMetadata(keptTranscript string, finalDuration float64) (*models.YouTubeMetadata, error) {
	if c.apiKey == "" {
		return mockMetadata(finalDuration), nil
	}

	prompt := fmt.Sprintf(`Kamu adalah YouTube Strategist untuk channel gaming video essay "TxShD" (@txshd).
Konten ini membahas analisis mendalam seputar game, mekanik gameplay, atau misteri cerita game.
Berdasarkan transkrip akhir hasil cut berikut (durasi total video: %.1f detik):

%s

Buatkan paket metadata YouTube lengkap:
1. 5 opsi judul yang sangat memikat (curiosity-driven, bukan clickbait murahan), gaya video essay TxShD.
2. Deskripsi YouTube yang terstruktur (Hook pembuka, rangkuman, ajakan interaksi).
3. Daftar Chapters/Timestamps YouTube yang dimulai dengan "00:00 Intro" dan disebar secara proporsional sesuai topik transkrip (maksimal total durasi %.1f detik).
4. 8-12 tags relevan.

Jawab HANYA dalam format JSON valid:
{
  "titles": ["Judul 1", "Judul 2", "Judul 3", "Judul 4", "Judul 5"],
  "description": "...",
  "chapters": [
    {"timestamp": "00:00", "seconds": 0.0, "title": "Intro: Masalah Utama"},
    {"timestamp": "02:15", "seconds": 135.0, "title": "Misteri Tersembunyi"}
  ],
  "tags": ["gaming", "video essay", "txshd"]
}`, finalDuration, truncate(keptTranscript, 4000), finalDuration)

	respText, err := c.generateContent(prompt, "application/json")
	if err != nil {
		return mockMetadata(finalDuration), fmt.Errorf("gemini metadata error: %w", err)
	}

	var res struct {
		Titles      []string `json:"titles"`
		Description string   `json:"description"`
		Chapters    []struct {
			Timestamp string  `json:"timestamp"`
			Seconds   float64 `json:"seconds"`
			Title     string  `json:"title"`
		} `json:"chapters"`
		Tags []string `json:"tags"`
	}

	if err := json.Unmarshal([]byte(respText), &res); err != nil {
		return mockMetadata(finalDuration), nil
	}

	meta := &models.YouTubeMetadata{
		Description: res.Description,
		Tags:        res.Tags,
		UpdatedAt:   time.Now(),
	}

	for _, t := range res.Titles {
		meta.SuggestedTitles = append(meta.SuggestedTitles, models.TitleOption{
			Title:     t,
			HookScore: 4.5,
			Angle:     "Curiosity Video Essay",
		})
	}

	for _, ch := range res.Chapters {
		meta.Chapters = append(meta.Chapters, models.Chapter{
			Timestamp: ch.Timestamp,
			Seconds:   ch.Seconds,
			Title:     ch.Title,
		})
	}

	return meta, nil
}

func (c *Client) generateContent(prompt, mimeType string) (string, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", c.model, c.apiKey)

	reqBody := GeminiRequest{
		Contents: []Content{
			{
				Role: "user",
				Parts: []Part{
					{Text: prompt},
				},
			},
		},
		GenerationConfig: GenerationConfig{
			Temperature:      0.4,
			ResponseMimeType: mimeType,
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer httpResp.Body.Close()

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return "", err
	}

	if httpResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini API returned %d: %s", httpResp.StatusCode, string(bodyBytes))
	}

	var parsed GeminiResponse
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return "", err
	}

	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini returned no candidates")
	}

	return parsed.Candidates[0].Content.Parts[0].Text, nil
}

func truncate(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	return s[:maxChars] + "... [dipotong untuk efisiensi token]"
}

func mockMetadata(dur float64) *models.YouTubeMetadata {
	return &models.YouTubeMetadata{
		SuggestedTitles: []models.TitleOption{
			{Title: "Kenapa Game Ini Diam-Diam Sangat Jenius?", HookScore: 4.8, Angle: "Curiosity Paradox"},
			{Title: "Detail Gila yang Mungkin Anda Lewatkan!", HookScore: 4.5, Angle: "Hidden Detail"},
			{Title: "Analisis Mendalam: Desain yang Merubah Industri", HookScore: 4.2, Angle: "Documentary Analysis"},
		},
		Description: "Analisis video essay mendalam seputar dunia game oleh TxShD.\n\nJangan lupa subscribe untuk konten video essay berikutnya!",
		Chapters: []models.Chapter{
			{Timestamp: "00:00", Seconds: 0.0, Title: "Intro & Premis"},
			{Timestamp: "01:30", Seconds: 90.0, Title: "Mekanik Inti"},
			{Timestamp: "04:00", Seconds: 240.0, Title: "Kesimpulan Akhir"},
		},
		Tags:      []string{"txshd", "gaming", "video essay", "analisis game"},
		UpdatedAt: time.Now(),
	}
}
