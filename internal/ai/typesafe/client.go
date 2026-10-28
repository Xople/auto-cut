package typesafe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"autocut/internal/models"
)

const Endpoint = "https://api.typesafe.ai/v1/systemone"

type Client struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		model:  "jev-latest",
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// SystemOneRequest matches the TypeSafe API contract.
type SystemOneRequest struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

type Question struct {
	Type         string `json:"type"`                   // "noul", "choice", "score"
	Instructions any    `json:"instructions"`           // string or object
	Criteria     any    `json:"criteria,omitempty"`     // object or map or array
}

type SystemOneResponse struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type Answer struct {
	Type          string             `json:"type"`
	Noul          float64            `json:"noul,omitempty"`          // 0.0 - 1.0
	Choice        string             `json:"choice,omitempty"`        // Selected option
	Score         float64            `json:"score,omitempty"`         // Weighted score
	Confidence    float64            `json:"confidence,omitempty"`    // 0.0 - 1.0
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
}

// EvaluateBoundaryContinuity tests if cutting a segment leaves an unnatural sentence break.
func (c *Client) EvaluateBoundaryContinuity(preceding, cutCandidate, following string) (isFluent bool, prob float64, err error) {
	if c.apiKey == "" {
		// Mock behavior when key is not configured
		return true, 0.95, nil
	}

	reqBody := SystemOneRequest{
		State: map[string]string{
			"preceding_words": preceding,
			"cut_segment":     cutCandidate,
			"following_words": following,
		},
		Model: c.model,
		Questions: map[string]Question{
			"is_fluent": {
				Type: "noul",
				Instructions: "If the `cut_segment` is removed, does the junction between `preceding_words` and `following_words` remain syntactically natural and coherent in Indonesian/English voiceover narration?",
				Criteria: map[string]string{
					"true":  "The transition flows naturally without orphaned prepositions or broken thoughts.",
					"false": "The cut leaves an incomplete word or unnatural grammatical rupture.",
				},
			},
		},
	}

	resp, err := c.sendRequest(reqBody)
	if err != nil {
		return true, 0.90, err
	}

	ans, ok := resp.Answers["is_fluent"]
	if !ok {
		return true, 0.90, nil
	}

	// Noul near 1.0 = fluent; near 0.0 = broken cut point
	return ans.Noul >= 0.65, ans.Noul, nil
}

// ClassifySegment classifies a candidate cut and assigns calibrated confidence.
func (c *Client) ClassifySegment(text, context string) (reason models.CutReason, confidence float64, needsReview bool, err error) {
	if c.apiKey == "" {
		return models.ReasonKeep, 0.90, false, nil
	}

	reqBody := SystemOneRequest{
		State: map[string]string{
			"candidate_segment": text,
			"context":           context,
		},
		Model: c.model,
		Questions: map[string]Question{
			"classification": {
				Type: "choice",
				Instructions: "Classify the `candidate_segment` in the given `context` of a YouTube video essay narration.",
				Criteria: map[string]string{
					"keep":     "Valid narrative information or intentional dramatic pause",
					"filler":   "Verbal tic, filler sound, or meaningless vocal hesitation (um, eh, anu, jadi ya)",
					"retake":   "False start, stumble, or repeated sentence take where the speaker restarts the sentence",
					"offtopic": "Off-topic mic check, cough, clearing throat, or speaking to camera operator",
				},
			},
		},
	}

	resp, err := c.sendRequest(reqBody)
	if err != nil {
		return models.ReasonKeep, 0.8, true, err
	}

	ans, ok := resp.Answers["classification"]
	if !ok {
		return models.ReasonKeep, 0.8, true, nil
	}

	conf := ans.Confidence
	// Confidence gating: if confidence is below 0.80, flag for creator review
	needsReview = conf < 0.80

	switch ans.Choice {
	case "filler":
		return models.ReasonFiller, conf, needsReview, nil
	case "retake":
		return models.ReasonRetake, conf, needsReview, nil
	case "offtopic":
		return models.ReasonOffTopic, conf, needsReview, nil
	default:
		return models.ReasonKeep, conf, needsReview, nil
	}
}

// ScoreTitleHooks uses TypeSafe Score primitive to rate YouTube title options.
func (c *Client) ScoreTitleHooks(titles []string, transcriptSummary string) ([]models.TitleOption, error) {
	var result []models.TitleOption

	if c.apiKey == "" {
		for i, t := range titles {
			result = append(result, models.TitleOption{
				Title:     t,
				HookScore: 4.0 + float64(i)*0.2,
				Angle:     "Curiosity Hook",
			})
		}
		return result, nil
	}

	questions := make(map[string]Question)
	for i, t := range titles {
		qID := fmt.Sprintf("title_%d", i)
		questions[qID] = Question{
			Type: "score",
			Instructions: map[string]string{
				"title":   t,
				"summary": transcriptSummary,
				"prompt":  "Rate the YouTube video essay click-through hook and curiosity strength of `title` based on the content `summary`.",
			},
			Criteria: []string{
				"Boring or generic statement",
				"Informational but lacks curiosity",
				"Decent intrigue for interested viewers",
				"Strong psychological curiosity gap",
				"Exceptional irresistible video essay title",
			},
		}
	}

	reqBody := SystemOneRequest{
		State:     transcriptSummary,
		Model:     c.model,
		Questions: questions,
	}

	resp, err := c.sendRequest(reqBody)
	if err != nil {
		// Fallback without failing the whole pipeline
		for _, t := range titles {
			result = append(result, models.TitleOption{Title: t, HookScore: 4.0, Angle: "Direct Hook"})
		}
		return result, nil
	}

	for i, t := range titles {
		qID := fmt.Sprintf("title_%d", i)
		score := 3.5
		if ans, ok := resp.Answers[qID]; ok {
			score = ans.Score + 1.0 // Map 0-4 to 1-5 scale
		}
		result = append(result, models.TitleOption{
			Title:     t,
			HookScore: score,
			Angle:     "Ranked by TypeSafe Jev",
		})
	}

	return result, nil
}

func (c *Client) sendRequest(req SystemOneRequest) (*SystemOneResponse, error) {
	data, err := json.Marshal(req)
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
		return nil, fmt.Errorf("typesafe api call failed: %w", err)
	}
	defer httpResp.Body.Close()

	bodyBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("typesafe api error HTTP %d: %s", httpResp.StatusCode, string(bodyBytes))
	}

	var parsed SystemOneResponse
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse typesafe response: %w", err)
	}

	return &parsed, nil
}
