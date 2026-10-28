package deepseek

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

const Endpoint = "https://api.deepseek.com/chat/completions"

type Client struct {
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type MacroCutProposal struct {
	StartIndex  int              `json:"start_index"`  // Word index
	EndIndex    int              `json:"end_index"`    // Word index
	ExcerptText string           `json:"excerpt_text"` // Text to cut
	Reason      models.CutReason `json:"reason"`       // "retake", "filler", "off_topic"
	Explanation string           `json:"explanation"`  // Why it should be cut
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model          string        `json:"model"`
	Messages       []ChatMessage `json:"messages"`
	ResponseFormat struct {
		Type string `json:"type"`
	} `json:"response_format"`
	Temperature float64 `json:"temperature"`
}

type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// AnalyzeTranscriptForCuts passes the entire word transcript and extracts retakes & filler passages.
func (c *Client) AnalyzeTranscriptForCuts(words []models.WordTiming) ([]MacroCutProposal, error) {
	if c.apiKey == "" {
		// Mock analyzer when API key is not configured
		return mockAnalysis(words), nil
	}

	// Prepare indexed word list for precise mapping
	var wordList strings.Builder
	for i, w := range words {
		wordList.WriteString(fmt.Sprintf("[%d]%s ", i, w.Word))
	}

	systemPrompt := `Kamu adalah asisten editor video profesional untuk channel YouTube video essay gaming (TxShD).
Tugasmu adalah menganalisis transkrip voice-over narasi mentah (Bahasa Indonesia campur istilah gaming Inggris) dan mengidentifikasi bagian yang HARUS DIPOTONG:
1. Retake / False Start: Ketika narator salah bicara, terbata-bata, lalu mengulang kembali kalimat yang sama (misal: "eh salah, ulang...", atau pengulangan take 2-3 kali). Simpan take terakhir yang paling bagus, potong take sebelumnya yang gagal!
2. Filler phrases yang merusak pacing (misal: "jadi apa ya namanya...", "eh wait...", "hmmm gimana ya").
3. Kalimat menggantung yang tidak selesai karena pembicara berganti pikiran.

Berikan output HANYA dalam format JSON valid berupa array objek:
[
  {
    "start_index": <int_indeks_kata_mulai>,
    "end_index": <int_indeks_kata_selesai>,
    "excerpt_text": "<potongan_kalimat>",
    "reason": "retake" | "filler" | "off_topic",
    "explanation": "<alasan_singkat>"
  }
]`

	userPrompt := fmt.Sprintf("Berikut transkrip dengan indeks kata:\n\n%s", wordList.String())

	reqBody := ChatRequest{
		Model: "deepseek-chat",
		Messages: []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0.1,
	}
	reqBody.ResponseFormat.Type = "json_object"

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest("POST", Endpoint, bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("deepseek api call failed: %w", err)
	}
	defer httpResp.Body.Close()

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("deepseek api error HTTP %d: %s", httpResp.StatusCode, string(bodyBytes))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
		return nil, err
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("deepseek returned no choices")
	}

	rawContent := chatResp.Choices[0].Message.Content

	// DeepSeek might wrap array in {"cuts": [...]} or raw array
	var proposals []MacroCutProposal
	if err := json.Unmarshal([]byte(rawContent), &proposals); err != nil {
		var wrapper struct {
			Cuts []MacroCutProposal `json:"cuts"`
		}
		if err2 := json.Unmarshal([]byte(rawContent), &wrapper); err2 == nil {
			proposals = wrapper.Cuts
		} else {
			return nil, fmt.Errorf("failed to parse deepseek cuts json: %w (content: %s)", err, rawContent)
		}
	}

	return proposals, nil
}

func mockAnalysis(words []models.WordTiming) []MacroCutProposal {
	var proposals []MacroCutProposal
	// Detect basic filler words in Indonesian/English
	fillers := map[string]bool{
		"uh": true, "um": true, "eh": true, "anu": true,
	}

	for i, w := range words {
		lower := strings.ToLower(strings.Trim(w.Word, ".,!?-"))
		if fillers[lower] {
			proposals = append(proposals, MacroCutProposal{
				StartIndex:  i,
				EndIndex:    i,
				ExcerptText: w.Word,
				Reason:      models.ReasonFiller,
				Explanation: "Vocal filler sound",
			})
		}
	}
	return proposals
}
