package models

import "time"

// JobStatus represents the state of a video processing job.
type JobStatus string

const (
	StatusPending          JobStatus = "pending"
	StatusExtractingAudio  JobStatus = "extracting_audio"
	StatusDetectingSilence JobStatus = "detecting_silence"
	StatusTranscribing     JobStatus = "transcribing"
	StatusReasoning        JobStatus = "reasoning"
	StatusReviewing        JobStatus = "reviewing"
	StatusRendering        JobStatus = "rendering"
	StatusBurningCaptions  JobStatus = "burning_captions"
	StatusGeneratingMeta   JobStatus = "generating_meta"
	StatusAwaitingVo       JobStatus = "awaiting_vo"
	StatusCutting          JobStatus = "cutting"
	StatusCutReady         JobStatus = "cut_ready"
	StatusCaptioning       JobStatus = "captioning"
	StatusCompleted        JobStatus = "completed"
	StatusFailed           JobStatus = "failed"
)

// Job represents a single video auto-cut job.
type Job struct {
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	OriginalFilename string    `json:"original_filename"`
	MediaType        string    `json:"media_type"` // "video" (has a picture) or "audio" (narration only)
	RawVideoPath     string    `json:"raw_video_path"`
	VoPath           string    `json:"vo_path"`
	VoFilename       string    `json:"vo_filename"`
	HasVo            bool      `json:"has_vo"`
	AudioPath        string    `json:"audio_path"` // normalized VO wav used for analysis
	CutVideoPath     string    `json:"cut_video_path"`
	FinalVideoPath   string    `json:"final_video_path"`
	Status           JobStatus `json:"status"`
	CurrentStep      int       `json:"current_step"`
	Progress         float64   `json:"progress"` // 0.0 - 100.0
	StatusMessage    string    `json:"status_message"`
	ErrorMessage     string    `json:"error_message,omitempty"`
	DurationOriginal float64   `json:"duration_original"` // VO duration, seconds
	VideoDuration    float64   `json:"video_duration"`    // picture duration, seconds; 0 for audio-only
	DurationFinal    float64   `json:"duration_final"`
	TimeSavedPercent float64   `json:"time_saved_percent"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// WordTiming represents a single transcribed word with millisecond precision.
type WordTiming struct {
	Word       string  `json:"word"`
	Start      float64 `json:"start"`      // In seconds
	End        float64 `json:"end"`        // In seconds
	Confidence float64 `json:"confidence"` // Whisper confidence
}

// Transcript represents the full transcription with word-level timings.
type Transcript struct {
	JobID     string       `json:"job_id"`
	AssetID   string       `json:"asset_id,omitempty"`
	FullText  string       `json:"full_text"`
	Words     []WordTiming `json:"words"`
	Duration  float64      `json:"duration"`
	Language  string       `json:"language"`
	Mock      bool         `json:"mock,omitempty"` // true when whisper-cli is missing
	CreatedAt time.Time    `json:"created_at"`
}

// CutReason describes why a segment was flagged for cutting.
type CutReason string

const (
	ReasonSilence  CutReason = "silence"
	ReasonFiller   CutReason = "filler"
	ReasonRetake   CutReason = "retake"
	ReasonOffTopic CutReason = "off_topic"
	ReasonKeep     CutReason = "keep"
)

// CutSegment represents an interval flagged to be removed or kept.
type CutSegment struct {
	ID             string    `json:"id"`
	Start          float64   `json:"start"`           // In seconds
	End            float64   `json:"end"`             // In seconds
	Duration       float64   `json:"duration"`        // End - Start
	Reason         CutReason `json:"reason"`          // silence, filler, retake
	Source         string    `json:"source"`          // "ffmpeg", "deepseek", "typesafe", "user"
	Text           string    `json:"text,omitempty"`  // Associated spoken text (if any)
	Confidence     float64   `json:"confidence"`      // TypeSafe or detector confidence
	NeedsReview    bool      `json:"needs_review"`    // True if confidence is borderline (0.4 - 0.85)
	UserOverridden bool      `json:"user_overridden"` // True if user manually toggled KEEP/CUT
	IsCut          bool      `json:"is_cut"`          // true = remove, false = keep
}

// KeepInterval is an inverted contiguous interval of speech to keep in the final cut.
type KeepInterval struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// Cutlist holds all segments and keep intervals for a job.
type Cutlist struct {
	JobID             string         `json:"job_id"`
	Segments          []CutSegment   `json:"segments"`
	KeepIntervals     []KeepInterval `json:"keep_intervals"`
	TotalCutDuration  float64        `json:"total_cut_duration"`
	TotalKeepDuration float64        `json:"total_keep_duration"`
	QASanityPassed    bool           `json:"qa_sanity_passed"`
	QAReport          string         `json:"qa_report,omitempty"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

// TitleOption represents a YouTube title proposal with TypeSafe hook score.
type TitleOption struct {
	Title     string  `json:"title"`
	HookScore float64 `json:"hook_score"` // Rated 1.0 - 5.0 by TypeSafe System One
	Angle     string  `json:"angle"`      // e.g. "Curiosity", "Controversial", "Direct Problem"
}

// Chapter represents a YouTube video chapter marker.
type Chapter struct {
	Timestamp string  `json:"timestamp"` // "00:00", "02:14"
	Seconds   float64 `json:"seconds"`
	Title     string  `json:"title"`
}

// YouTubeMetadata contains final YouTube publishing package.
type YouTubeMetadata struct {
	JobID           string        `json:"job_id"`
	SuggestedTitles []TitleOption `json:"suggested_titles"`
	Description     string        `json:"description"`
	Chapters        []Chapter     `json:"chapters"`
	Tags            []string      `json:"tags"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

// PipelineProgressEvent is streamed via Server-Sent Events (SSE) to the frontend.
type PipelineProgressEvent struct {
	JobID         string    `json:"job_id"`
	Status        JobStatus `json:"status"`
	CurrentStep   int       `json:"current_step"`
	TotalSteps    int       `json:"total_steps"`
	Progress      float64   `json:"progress"`
	StatusMessage string    `json:"status_message"`
	ErrorMessage  string    `json:"error_message,omitempty"`
}
