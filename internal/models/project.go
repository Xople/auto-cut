package models

import "time"

// Track kinds supported by the editor timeline.
const (
	TrackVideo   = "video"
	TrackAudio   = "audio"
	TrackOverlay = "overlay"
	TrackCaption = "caption"
)

// Asset kinds supported by the media library.
const (
	AssetVideo = "video"
	AssetAudio = "audio"
	AssetImage = "image"
)

// Project is a single editing session: media library plus timeline document.
type Project struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Width         int        `json:"width"`
	Height        int        `json:"height"`
	FPS           float64    `json:"fps"`
	Timeline      Timeline   `json:"timeline"`
	HasTranscript bool       `json:"has_transcript"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	LastExport    *ExportJob `json:"last_export,omitempty"`
}

// Asset is an imported media file stored inside the project folder.
type Asset struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	Kind         string    `json:"kind"`
	Filename     string    `json:"filename"`
	Path         string    `json:"-"`
	Duration     float64   `json:"duration"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	FPS          float64   `json:"fps"`
	HasAudio     bool      `json:"has_audio"`
	ThumbPath    string    `json:"-"`
	WaveformPath string    `json:"-"`
	Size         int64     `json:"size"`
	CreatedAt    time.Time `json:"created_at"`
}

// Timeline is the editable document persisted as JSON.
type Timeline struct {
	Version      int          `json:"version"`
	Width        int          `json:"width"`
	Height       int          `json:"height"`
	FPS          float64      `json:"fps"`
	CaptionStyle CaptionStyle `json:"caption_style"`
	Tracks       []Track      `json:"tracks"`
}

// Track holds clips of one kind. Order in the slice is the vertical order in the UI:
// index 0 is the topmost row, later rows are drawn underneath.
type Track struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Muted  bool   `json:"muted"`
	Locked bool   `json:"locked"`
	Hidden bool   `json:"hidden"`
	Height int    `json:"height"`
	Clips  []Clip `json:"clips"`
}

// Clip is a placed media, overlay, or caption item.
type Clip struct {
	ID       string  `json:"id"`
	AssetID  string  `json:"asset_id,omitempty"`
	Start    float64 `json:"start"`
	Duration float64 `json:"duration"`
	In       float64 `json:"in"`
	Out      float64 `json:"out"`
	// Speed is a playback rate multiplier; 1 is normal. LinkedID ties a clip to
	// its counterpart on another track so edits move both together.
	Speed    float64 `json:"speed"`
	LinkedID string  `json:"linked_id,omitempty"`

	// Overlay transform. X/Y are the normalized center of the overlay (0..1).
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Scale     float64 `json:"scale"`
	Rotation  float64 `json:"rotation"`
	Opacity   float64 `json:"opacity"`
	BlendMode string  `json:"blend_mode"`

	// Text overlay content and style.
	Text  string     `json:"text,omitempty"`
	Style *TextStyle `json:"style,omitempty"`

	// Audio properties.
	Volume  float64 `json:"volume"`
	FadeIn  float64 `json:"fade_in"`
	FadeOut float64 `json:"fade_out"`

	// Caption word timings in timeline seconds, used for karaoke highlight.
	Words []WordTiming `json:"words,omitempty"`

	// Transition plays at the start of the clip, over the layer below it.
	Transition *Transition `json:"transition,omitempty"`

	// Keyframes animate the transform above. Time is relative to the clip start.
	Keyframes []TransformKeyframe `json:"keyframes,omitempty"`
}

// TransformKeyframe stores one transform pose on a clip. Values interpolate
// linearly between consecutive keyframes.
type TransformKeyframe struct {
	Time     float64 `json:"time"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Scale    float64 `json:"scale"`
	Rotation float64 `json:"rotation"`
}

// Transition kinds supported by the timeline.
const (
	TransitionPullIn     = "pull_in"
	TransitionSlideRight = "slide_right"
	TransitionSlideLeft  = "slide_left"
)

// Transition is a visual entrance effect at a clip's start.
type Transition struct {
	Type     string  `json:"type"`
	Duration float64 `json:"duration"`
}

// End returns the clip end time on the timeline.
func (c Clip) End() float64 { return c.Start + c.Duration }

// TextStyle is the per-clip styling for overlay text.
type TextStyle struct {
	FontFamily      string  `json:"font_family"`
	FontFile        string  `json:"font_file"`
	FontSize        int     `json:"font_size"`
	Bold            bool    `json:"bold"`
	Italic          bool    `json:"italic"`
	Color           string  `json:"color"`
	StrokeWidth     float64 `json:"stroke_width"`
	StrokeColor     string  `json:"stroke_color"`
	Shadow          float64 `json:"shadow"`
	ShadowColor     string  `json:"shadow_color"`
	Align           string  `json:"align"`
	Uppercase       bool    `json:"uppercase"`
	BackgroundColor string  `json:"background_color"`
	BackgroundAlpha float64 `json:"background_alpha"`
}

// CaptionStyle is the global caption look for the project.
type CaptionStyle struct {
	FontFamily     string  `json:"font_family"`
	FontFile       string  `json:"font_file"`
	FontSize       int     `json:"font_size"`
	Bold           bool    `json:"bold"`
	Italic         bool    `json:"italic"`
	Color          string  `json:"color"`
	HighlightColor string  `json:"highlight_color"`
	StrokeWidth    float64 `json:"stroke_width"`
	StrokeColor    string  `json:"stroke_color"`
	Shadow         float64 `json:"shadow"`
	ShadowColor    string  `json:"shadow_color"`
	Opacity        float64 `json:"opacity"`
	BlendMode      string  `json:"blend_mode"`
	PositionY      float64 `json:"position_y"`
	Uppercase      bool    `json:"uppercase"`
	Highlight      bool    `json:"highlight"`
	WordsPerChunk  int     `json:"words_per_chunk"`
}

// ExportSettings controls the final ffmpeg render.
type ExportSettings struct {
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	FPS      float64 `json:"fps"`
	Encoder  string  `json:"encoder"` // auto, libx264, h264_amf, h264_nvenc
	Quality  int     `json:"quality"` // CRF/CQ, lower is better
	Filename string  `json:"filename"`
}

// ExportJob is a finished or running timeline render.
type ExportJob struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	Settings   ExportSettings `json:"settings"`
	Status     string         `json:"status"` // running, completed, failed
	Progress   float64        `json:"progress"`
	Message    string         `json:"message"`
	OutputPath string         `json:"-"`
	OutputName string         `json:"output_name"`
	Error      string         `json:"error,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
}

// AnalysisResult is returned by the auto-cut analyzer.
type AnalysisResult struct {
	Transcript *Transcript  `json:"transcript"`
	Cuts       []CutSegment `json:"cuts"`
	TotalCuts  float64      `json:"total_cut_duration"`
}

// DefaultCaptionStyle is the CapCut-style caption preset used for new projects.
func DefaultCaptionStyle() CaptionStyle {
	return CaptionStyle{
		FontFamily:     "Arial",
		FontSize:       72,
		Bold:           true,
		Color:          "#FFFFFF",
		HighlightColor: "#FFD400",
		StrokeWidth:    5,
		StrokeColor:    "#000000",
		Shadow:         2,
		ShadowColor:    "#000000",
		Opacity:        1,
		BlendMode:      "normal",
		PositionY:      0.82,
		Highlight:      true,
		WordsPerChunk:  3,
	}
}

// NewTimeline builds the default four-track timeline for a new project.
func NewTimeline(width, height int, fps float64) Timeline {
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}
	if fps <= 0 {
		fps = 30
	}
	return Timeline{
		Version:      TimelineVersion,
		Width:        width,
		Height:       height,
		FPS:          fps,
		CaptionStyle: DefaultCaptionStyle(),
		Tracks: []Track{
			{ID: "video-main", Kind: TrackVideo, Name: "Video", Height: DefaultTrackHeight, Clips: []Clip{}},
			{ID: "overlay", Kind: TrackOverlay, Name: "Overlay", Height: DefaultTrackHeight, Clips: []Clip{}},
			{ID: "audio-main", Kind: TrackAudio, Name: "Audio", Height: DefaultTrackHeight, Clips: []Clip{}},
			{ID: "caption", Kind: TrackCaption, Name: "Caption", Height: DefaultTrackHeight, Clips: []Clip{}},
		},
	}
}

// TimelineDuration returns the furthest clip end on the timeline.
func TimelineDuration(t Timeline) float64 {
	var maxEnd float64
	for _, track := range t.Tracks {
		for _, clip := range track.Clips {
			if end := clip.End(); end > maxEnd {
				maxEnd = end
			}
		}
	}
	return maxEnd
}
