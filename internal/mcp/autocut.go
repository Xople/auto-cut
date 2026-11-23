package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"autocut/internal/captions"
	"autocut/internal/models"
	"autocut/internal/pipeline"
	"autocut/internal/render"
	"autocut/internal/timeline"
)

func (s *Server) registerAutoCutTools() {
	s.RegisterTool(ToolDefinition{
		Name:        "autocut_list_projects",
		Description: "List all projects in the AutoCut Workstation database.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, s.handleListProjects)

	s.RegisterTool(ToolDefinition{
		Name:        "autocut_get_project",
		Description: "Get full details of an AutoCut project including its timeline tracks, clips, imported assets, and whether a transcript exists.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":         map[string]any{"type": "string", "description": "Project ID"},
				"include_transcript": map[string]any{"type": "boolean", "description": "Include full word-level transcript if available"},
			},
			"required": []string{"project_id"},
		},
	}, s.handleGetProject)

	s.RegisterTool(ToolDefinition{
		Name:        "autocut_create_project",
		Description: "Create a new AutoCut project (defaults to 1080x1920 9:16 vertical Shorts at 60fps).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":   map[string]any{"type": "string", "description": "Project display name"},
				"width":  map[string]any{"type": "integer", "description": "Canvas width in pixels (default 1080 for Shorts)"},
				"height": map[string]any{"type": "integer", "description": "Canvas height in pixels (default 1920 for Shorts)"},
				"fps":    map[string]any{"type": "number", "description": "Frame rate (default 60)"},
			},
			"required": []string{"name"},
		},
	}, s.handleCreateProject)

	s.RegisterTool(ToolDefinition{
		Name:        "autocut_delete_project",
		Description: "Delete an AutoCut project and its media folder.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID to delete"},
			},
			"required": []string{"project_id"},
		},
	}, s.handleDeleteProject)

	s.RegisterTool(ToolDefinition{
		Name:        "autocut_import_asset",
		Description: "Import one or more local media files (.wav, .mp3, .mp4, .mkv, .png, .jpg) into an AutoCut project, automatically generating thumbnails and waveforms.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Target Project ID"},
				"path":       map[string]any{"type": "string", "description": "Single absolute file path to import"},
				"paths":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional list of absolute file paths to batch import"},
			},
			"required": []string{"project_id"},
		},
	}, s.handleImportAsset)

	s.RegisterTool(ToolDefinition{
		Name:        "autocut_analyze_vo",
		Description: "Analyze a voice-over asset in the project (FFmpeg silence detection + whisper.cpp word-level transcription + rule-based filler/retake detection + optional AI cut suggestions). Saves the transcript to the project.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"asset_id":   map[string]any{"type": "string", "description": "Voice-over audio/video Asset ID"},
				"use_ai":     map[string]any{"type": "boolean", "description": "Enable DeepSeek/TypeSafe AI cut suggestions if API keys are configured (default false for fast local analysis)"},
				"use_qa":     map[string]any{"type": "boolean", "description": "Enable Gemini QA review if API key is configured (default false)"},
			},
			"required": []string{"project_id", "asset_id"},
		},
	}, s.handleAnalyzeVO)

	s.RegisterTool(ToolDefinition{
		Name:        "autocut_set_transcript",
		Description: "Save or refine a word-level transcript for a project's voice-over asset (either from explicit word timings or from phrase segments automatically distributed across speech intervals). Useful when whisper.cpp is absent or when correcting proper nouns.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"asset_id":   map[string]any{"type": "string", "description": "Voice-over Asset ID"},
				"language":   map[string]any{"type": "string", "description": "Language code (default 'id')"},
				"words":      map[string]any{"type": "array", "description": "Optional explicit array of {word, start, end}"},
				"segments":   map[string]any{"type": "array", "description": "Optional array of {start, end, text} speech segments to automatically expand into word timings"},
			},
			"required": []string{"project_id", "asset_id"},
		},
	}, s.handleSetTranscript)

	s.RegisterTool(ToolDefinition{
		Name:        "autocut_apply_cuts",
		Description: "Apply cut segments (in asset seconds) to clips of a voice-over asset on the timeline, compacting kept intervals and optionally rippling subsequent clips.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"asset_id":   map[string]any{"type": "string", "description": "Asset ID whose clips should be cut"},
				"cuts":       map[string]any{"type": "array", "description": "Array of CutSegment objects ({start, end, is_cut: true, reason})"},
				"ripple":     map[string]any{"type": "boolean", "description": "Shift subsequent clips on the same track (default true)"},
			},
			"required": []string{"project_id", "asset_id", "cuts"},
		},
	}, s.handleApplyCuts)

	s.RegisterTool(ToolDefinition{
		Name:        "autocut_upsert_clip",
		Description: "Add or update a clip on a specific track in the project timeline (supports background_fill: 'blur', transitions, keyframes, audio volume/fades, and text/image overlays).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"track_id":   map[string]any{"type": "string", "description": "Target track ID (e.g. 'video-main', 'audio-main', 'overlay', 'caption')"},
				"clip":       map[string]any{"type": "object", "description": "Clip object to insert or update"},
			},
			"required": []string{"project_id", "track_id", "clip"},
		},
	}, s.handleUpsertClip)

	s.RegisterTool(ToolDefinition{
		Name:        "autocut_set_scene_transitions",
		Description: "Apply CapCut-style two-phase transitions across all video scene clips on the project timeline (supports 'variety' for automatic non-monotonous transition rotation, 'none' to clear, or any specific type: 'pull_in', 'pull_out', 'blink', 'flash', 'fade', 'slide_up', 'slide_down', 'slide_left', 'slide_right', 'spin_in') and automatically attach matching impact sound effects from D:\\Videos\\TxShd\\Sounds onto the Transition SFX audio track.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":    map[string]any{"type": "string", "description": "Project ID"},
				"mode":          map[string]any{"type": "string", "description": "'variety' (default), 'none', or a TransitionType ('pull_in', 'pull_out', 'blink', 'flash', 'fade', 'slide_up', 'slide_down', 'slide_left', 'slide_right', 'spin_in')"},
				"duration":      map[string]any{"type": "number", "description": "Transition duration in seconds (default 0.38)"},
				"include_first": map[string]any{"type": "boolean", "description": "Also apply transition to the very first scene at t=0 (default false)"},
				"with_sfx":      map[string]any{"type": "boolean", "description": "Automatically attach matching impact SFX from D:\\Videos\\TxShd\\Sounds on the 'audio-sfx' track (default true)"},
			},
			"required": []string{"project_id"},
		},
	}, s.handleSetSceneTransitions)

	s.RegisterTool(ToolDefinition{
		Name:        "autocut_update_timeline",
		Description: "Replace or normalize the full Timeline JSON of a project.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"timeline":   map[string]any{"type": "object", "description": "Full Timeline document"},
			},
			"required": []string{"project_id", "timeline"},
		},
	}, s.handleUpdateTimeline)

	s.RegisterTool(ToolDefinition{
		Name:        "autocut_generate_captions",
		Description: "Generate word-synced caption clips from the project transcript, with optional exclude_ranges to automatically hide captions while full-screen BRollEngine cards (Hooks / DeskriptifBroll) are on screen.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":     map[string]any{"type": "string", "description": "Project ID"},
				"exclude_ranges": map[string]any{"type": "array", "description": "Array of {start, end} timeline intervals in seconds where captions must be suppressed"},
				"caption_style":  map[string]any{"type": "object", "description": "Optional CaptionStyle overrides (font_family, font_size, position_y, words_per_chunk, highlight, background_color, background_alpha)"},
				"replace":        map[string]any{"type": "boolean", "description": "Replace existing caption track clips (default true)"},
			},
			"required": []string{"project_id"},
		},
	}, s.handleGenerateCaptions)

	s.RegisterTool(ToolDefinition{
		Name:        "autocut_assemble_short",
		Description: "High-level composite builder that assembles a complete multi-track Short/Essay timeline (Voice-Over + optional cuts + sequential/timed BRollEngine & gameplay clips with blur fill + smart-gated captions + optional BGM) in one atomic operation.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"voiceover": map[string]any{
					"type":        "object",
					"description": "Voice-over configuration: { asset_id, cuts?: [{start, end, is_cut}], volume?: 1.0 }",
				},
				"visual_segments": map[string]any{
					"type":        "array",
					"description": "Ordered visual clips: [{ asset_id, start?, duration?, in?, out?, background_fill?: 'blur'|'', keep_audio?: bool, volume?: number, suppress_captions?: bool, transition?: {type, duration} }]",
				},
				"bgm": map[string]any{
					"type":        "object",
					"description": "Optional background music bed: { asset_id, in?: 0, volume?: 0.12, fade_in?: 0.5, fade_out?: 1.0 }",
				},
				"caption_style": map[string]any{
					"type":        "object",
					"description": "Optional CaptionStyle overrides (e.g. background_color: '#000000', background_alpha: 0.68, highlight: false, words_per_chunk: 5)",
				},
				"auto_captions": map[string]any{
					"type":        "boolean",
					"description": "Automatically generate captions from transcript while excluding segments with suppress_captions: true (default true)",
				},
				"scene_transition": map[string]any{
					"type":        "string",
					"description": "Default transition mode for every scene cut ('variety', 'none', or a TransitionType such as 'pull_in', 'blink', 'flash', 'slide_up'). Defaults to 'variety'.",
				},
				"transition_duration": map[string]any{
					"type":        "number",
					"description": "Default scene transition duration in seconds (default 0.38)",
				},
			},
			"required": []string{"project_id", "voiceover", "visual_segments"},
		},
	}, s.handleAssembleShort)

	s.RegisterTool(ToolDefinition{
		Name:        "autocut_export_project",
		Description: "Render the project timeline into a final MP4 file (H.264 + AAC 192k 48kHz) with automatic hardware encoder fallback (h264_amf -> libx264) and optional copy to an external output path.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":  map[string]any{"type": "string", "description": "Project ID"},
				"filename":    map[string]any{"type": "string", "description": "Export filename inside project exports folder (e.g. '1105-autocut.mp4')"},
				"output_path": map[string]any{"type": "string", "description": "Optional external absolute path to also copy the rendered MP4 to"},
				"encoder":     map[string]any{"type": "string", "description": "Encoder ('auto', 'h264_amf', 'h264_nvenc', 'libx264')"},
				"quality":     map[string]any{"type": "integer", "description": "Quality CRF/QP (default 20)"},
			},
			"required": []string{"project_id"},
		},
	}, s.handleExportProject)
}

func (s *Server) handleListProjects(_ context.Context, _ json.RawMessage) (any, error) {
	return s.projects.List()
}

func (s *Server) handleGetProject(_ context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID         string `json:"project_id"`
		IncludeTranscript bool   `json:"include_transcript"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	p, err := s.projects.Get(args.ProjectID)
	if err != nil {
		return nil, err
	}
	assets, _ := s.projects.Assets(args.ProjectID)
	res := map[string]any{
		"project": p,
		"assets":  assets,
	}
	if args.IncludeTranscript && p.HasTranscript {
		if tr, err := s.projects.Transcript(args.ProjectID); err == nil {
			res["transcript"] = tr
		}
	}
	return res, nil
}

func (s *Server) handleCreateProject(_ context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		Name   string  `json:"name"`
		Width  int     `json:"width"`
		Height int     `json:"height"`
		FPS    float64 `json:"fps"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if args.Width <= 0 {
		args.Width = 1080
	}
	if args.Height <= 0 {
		args.Height = 1920
	}
	if args.FPS <= 0 {
		args.FPS = 60
	}
	p, err := s.projects.Create(args.Name, args.Width, args.Height, args.FPS)
	if err != nil {
		return nil, err
	}
	s.notifyLiveDesktop(p.ID, "project_created")
	return p, nil
}

func (s *Server) handleDeleteProject(_ context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if err := s.projects.Delete(args.ProjectID); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": args.ProjectID}, nil
}

func (s *Server) handleImportAsset(_ context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID string   `json:"project_id"`
		Path      string   `json:"path"`
		Paths     []string `json:"paths"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	var allPaths []string
	if strings.TrimSpace(args.Path) != "" {
		allPaths = append(allPaths, args.Path)
	}
	for _, p := range args.Paths {
		if strings.TrimSpace(p) != "" {
			allPaths = append(allPaths, p)
		}
	}
	if len(allPaths) == 0 {
		return nil, fmt.Errorf("at least one file path is required")
	}
	var imported []*models.Asset
	for _, p := range allPaths {
		a, err := s.projects.ImportPath(args.ProjectID, p)
		if err != nil {
			return nil, fmt.Errorf("failed importing %s: %w", p, err)
		}
		imported = append(imported, a)
	}
	s.notifyLiveDesktop(args.ProjectID, "assets_imported")
	if len(imported) == 1 {
		return imported[0], nil
	}
	return imported, nil
}

func (s *Server) handleAnalyzeVO(_ context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID string `json:"project_id"`
		AssetID   string `json:"asset_id"`
		UseAI     bool   `json:"use_ai"`
		UseQA     bool   `json:"use_qa"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	asset, err := s.projects.Asset(args.ProjectID, args.AssetID)
	if err != nil {
		return nil, fmt.Errorf("asset not found: %w", err)
	}
	result, err := s.orchestrator.AnalyzeVO(asset.Path, pipeline.AnalyzeOptions{
		TempDir: filepath.Join(s.projects.CacheDir(args.ProjectID), "analyze"),
		UseAI:   args.UseAI,
		UseQA:   args.UseQA,
	})
	if err != nil {
		return nil, err
	}
	result.Transcript.AssetID = asset.ID
	result.Transcript.JobID = args.ProjectID
	if err := s.projects.SaveTranscript(args.ProjectID, result.Transcript); err != nil {
		return nil, err
	}
	s.notifyLiveDesktop(args.ProjectID, "transcript_saved")
	return result, nil
}

func (s *Server) handleSetTranscript(_ context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID string              `json:"project_id"`
		AssetID   string              `json:"asset_id"`
		Language  string              `json:"language"`
		Words     []models.WordTiming `json:"words"`
		Segments  []struct {
			Start float64 `json:"start"`
			End   float64 `json:"end"`
			Text  string  `json:"text"`
		} `json:"segments"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	asset, err := s.projects.Asset(args.ProjectID, args.AssetID)
	if err != nil {
		return nil, fmt.Errorf("asset not found: %w", err)
	}
	words := args.Words
	if len(words) == 0 && len(args.Segments) > 0 {
		for _, seg := range args.Segments {
			tokens := strings.Fields(seg.Text)
			if len(tokens) == 0 || seg.End <= seg.Start {
				continue
			}
			step := (seg.End - seg.Start) / float64(len(tokens))
			for i, tok := range tokens {
				wStart := math.Round((seg.Start+float64(i)*step)*1000) / 1000
				wEnd := math.Round((seg.Start+float64(i+1)*step)*1000) / 1000
				words = append(words, models.WordTiming{
					Word:       tok,
					Start:      wStart,
					End:        wEnd,
					Confidence: 0.99,
				})
			}
		}
	}
	if len(words) == 0 {
		return nil, fmt.Errorf("either words or segments must be non-empty")
	}
	var fullParts []string
	for _, w := range words {
		fullParts = append(fullParts, w.Word)
	}
	lang := strings.TrimSpace(args.Language)
	if lang == "" {
		lang = "id"
	}
	tr := &models.Transcript{
		JobID:     args.ProjectID,
		AssetID:   asset.ID,
		FullText:  strings.Join(fullParts, " "),
		Words:     words,
		Duration:  asset.Duration,
		Language:  lang,
		Mock:      false,
		CreatedAt: time.Now(),
	}
	if err := s.projects.SaveTranscript(args.ProjectID, tr); err != nil {
		return nil, err
	}
	s.notifyLiveDesktop(args.ProjectID, "transcript_saved")
	return tr, nil
}

func (s *Server) handleApplyCuts(_ context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID string              `json:"project_id"`
		AssetID   string              `json:"asset_id"`
		Cuts      []models.CutSegment `json:"cuts"`
		Ripple    *bool               `json:"ripple"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	p, err := s.projects.Get(args.ProjectID)
	if err != nil {
		return nil, err
	}
	var clipIDs []string
	for _, track := range p.Timeline.Tracks {
		if track.Kind == models.TrackCaption {
			continue
		}
		for _, clip := range track.Clips {
			if clip.AssetID == args.AssetID {
				clipIDs = append(clipIDs, clip.ID)
			}
		}
	}
	if len(clipIDs) == 0 {
		return nil, fmt.Errorf("no clips on timeline use asset %s", args.AssetID)
	}
	ripple := true
	if args.Ripple != nil {
		ripple = *args.Ripple
	}
	removed := timeline.ApplyCuts(&p.Timeline, clipIDs, args.Cuts, ripple)
	if err := s.projects.SaveTimeline(args.ProjectID, &p.Timeline); err != nil {
		return nil, err
	}
	s.notifyLiveDesktop(args.ProjectID, "cuts_applied")
	return map[string]any{
		"removed_seconds":   removed,
		"timeline_duration": models.TimelineDuration(p.Timeline),
		"timeline":          p.Timeline,
	}, nil
}

func (s *Server) handleUpsertClip(_ context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID string      `json:"project_id"`
		TrackID   string      `json:"track_id"`
		Clip      models.Clip `json:"clip"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	p, err := s.projects.Get(args.ProjectID)
	if err != nil {
		return nil, err
	}
	clip := args.Clip
	if clip.ID == "" {
		clip.ID = fmt.Sprintf("clip_%d", time.Now().UnixNano())
	}
	if clip.Opacity <= 0 {
		clip.Opacity = 1
	}
	if clip.Scale <= 0 {
		clip.Scale = 1
	}
	if clip.Speed <= 0 {
		clip.Speed = 1
	}

	foundTrack := false
	for i := range p.Timeline.Tracks {
		track := &p.Timeline.Tracks[i]
		if track.ID != args.TrackID {
			continue
		}
		foundTrack = true
		updated := false
		for j := range track.Clips {
			if track.Clips[j].ID == clip.ID {
				track.Clips[j] = clip
				updated = true
				break
			}
		}
		if !updated {
			track.Clips = append(track.Clips, clip)
		}
		break
	}
	if !foundTrack {
		return nil, fmt.Errorf("track %s not found", args.TrackID)
	}
	models.NormalizeTimeline(&p.Timeline)
	if err := models.ValidateTimeline(p.Timeline); err != nil {
		return nil, err
	}
	if err := s.projects.SaveTimeline(args.ProjectID, &p.Timeline); err != nil {
		return nil, err
	}
	s.notifyLiveDesktop(args.ProjectID, "clip_upserted")
	return clip, nil
}

var varietyTransitionCycle = []string{
	models.TransitionPullIn,
	models.TransitionBlink,
	models.TransitionSlideUp,
	models.TransitionFlash,
	models.TransitionPullOut,
	models.TransitionSpinIn,
	models.TransitionSlideLeft,
	models.TransitionSlideRight,
}

func resolveSceneTransition(mode string, sceneIdx int, dur float64) *models.Transition {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "variety"
	}
	if mode == "none" {
		return nil
	}
	if dur <= 0 {
		dur = 0.38
	}
	tType := mode
	if mode == "variety" || mode == "auto" || mode == "auto_variety" {
		tType = varietyTransitionCycle[sceneIdx%len(varietyTransitionCycle)]
	}
	return &models.Transition{
		Type:     tType,
		Duration: dur,
	}
}

func (s *Server) handleSetSceneTransitions(_ context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID    string  `json:"project_id"`
		Mode         string  `json:"mode"`
		Duration     float64 `json:"duration"`
		IncludeFirst bool    `json:"include_first"`
		WithSFX      *bool   `json:"with_sfx"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	p, err := s.projects.Get(args.ProjectID)
	if err != nil {
		return nil, err
	}
	dur := args.Duration
	if dur <= 0 {
		dur = 0.38
	}
	mode := strings.TrimSpace(args.Mode)
	if mode == "" {
		mode = "variety"
	}
	withSFX := true
	if args.WithSFX != nil {
		withSFX = *args.WithSFX
	}

	updatedCount, sfxCount, applied, err := s.projects.ApplySceneTransitions(
		args.ProjectID,
		&p.Timeline,
		mode,
		dur,
		args.IncludeFirst,
		withSFX,
	)
	if err != nil {
		return nil, err
	}

	models.NormalizeTimeline(&p.Timeline)
	if err := models.ValidateTimeline(p.Timeline); err != nil {
		return nil, err
	}
	if err := s.projects.SaveTimeline(args.ProjectID, &p.Timeline); err != nil {
		return nil, err
	}
	s.notifyLiveDesktop(args.ProjectID, "transitions_updated")
	return map[string]any{
		"project_id":           args.ProjectID,
		"mode":                 mode,
		"duration":             dur,
		"clips_updated":        updatedCount,
		"transition_sfx_count": sfxCount,
		"scenes":               applied,
	}, nil
}

func (s *Server) handleUpdateTimeline(_ context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID string          `json:"project_id"`
		Timeline  models.Timeline `json:"timeline"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	p, err := s.projects.Get(args.ProjectID)
	if err != nil {
		return nil, err
	}
	tl := args.Timeline
	if tl.Width <= 0 {
		tl.Width = p.Width
	}
	if tl.Height <= 0 {
		tl.Height = p.Height
	}
	if tl.FPS <= 0 {
		tl.FPS = p.FPS
	}
	models.NormalizeTimeline(&tl)
	if err := models.ValidateTimeline(tl); err != nil {
		return nil, err
	}
	if err := s.projects.SaveTimeline(args.ProjectID, &tl); err != nil {
		return nil, err
	}
	s.notifyLiveDesktop(args.ProjectID, "timeline_updated")
	return tl, nil
}

func (s *Server) handleGenerateCaptions(_ context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID     string               `json:"project_id"`
		ExcludeRanges []models.TimeRange   `json:"exclude_ranges"`
		CaptionStyle  *models.CaptionStyle `json:"caption_style"`
		Replace       *bool                `json:"replace"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	p, err := s.projects.Get(args.ProjectID)
	if err != nil {
		return nil, err
	}
	tr, err := s.projects.Transcript(args.ProjectID)
	if err != nil || len(tr.Words) == 0 {
		return nil, fmt.Errorf("transcript not found; run autocut_analyze_vo first")
	}
	if args.CaptionStyle != nil {
		mergeCaptionStyle(&p.Timeline.CaptionStyle, *args.CaptionStyle)
	}
	var sources []models.Clip
	for _, track := range p.Timeline.Tracks {
		if track.ID == "audio-bgm" || track.ID == "audio-sfx" {
			continue
		}
		if track.Kind == models.TrackAudio || track.Kind == models.TrackVideo {
			sources = append(sources, track.Clips...)
		}
	}
	generated := captions.FromTranscriptWithExclusions(sources, tr, p.Timeline.CaptionStyle, args.ExcludeRanges)
	replace := true
	if args.Replace != nil {
		replace = *args.Replace
	}
	target := -1
	for i := range p.Timeline.Tracks {
		if p.Timeline.Tracks[i].Kind == models.TrackCaption {
			target = i
			if replace {
				p.Timeline.Tracks[i].Clips = []models.Clip{}
			}
			break
		}
	}
	if target < 0 {
		p.Timeline.Tracks = append(p.Timeline.Tracks, models.Track{
			ID:     "caption",
			Kind:   models.TrackCaption,
			Name:   "Caption",
			Height: models.DefaultTrackHeight,
			Clips:  []models.Clip{},
		})
		target = len(p.Timeline.Tracks) - 1
	}
	p.Timeline.Tracks[target].Clips = append(p.Timeline.Tracks[target].Clips, generated...)
	models.NormalizeTimeline(&p.Timeline)
	if err := s.projects.SaveTimeline(args.ProjectID, &p.Timeline); err != nil {
		return nil, err
	}
	s.notifyLiveDesktop(args.ProjectID, "captions_generated")
	return map[string]any{
		"caption_count":   len(generated),
		"excluded_ranges": len(args.ExcludeRanges),
		"caption_style":   p.Timeline.CaptionStyle,
	}, nil
}

type assembleShortArgs struct {
	ProjectID string `json:"project_id"`
	Voiceover struct {
		AssetID string              `json:"asset_id"`
		Cuts    []models.CutSegment `json:"cuts"`
		Volume  *float64            `json:"volume"`
	} `json:"voiceover"`
	VisualSegments []struct {
		AssetID          string             `json:"asset_id"`
		Start            *float64           `json:"start"`
		Duration         *float64           `json:"duration"`
		In               float64            `json:"in"`
		Out              float64            `json:"out"`
		BackgroundFill   string             `json:"background_fill"`
		BackgroundBlur   float64            `json:"background_blur"`
		BackgroundDim    float64            `json:"background_dim"`
		KeepAudio        *bool              `json:"keep_audio"`
		Volume           *float64           `json:"volume"`
		SuppressCaptions bool               `json:"suppress_captions"`
		Transition       *models.Transition `json:"transition"`
	} `json:"visual_segments"`
	BGM *struct {
		AssetID string   `json:"asset_id"`
		In      float64  `json:"in"`
		Volume  *float64 `json:"volume"`
		FadeIn  float64  `json:"fade_in"`
		FadeOut float64  `json:"fade_out"`
	} `json:"bgm"`
	CaptionStyle       *models.CaptionStyle `json:"caption_style"`
	AutoCaptions       *bool                `json:"auto_captions"`
	SceneTransition    string               `json:"scene_transition"`
	TransitionDuration float64              `json:"transition_duration"`
}

func (s *Server) handleAssembleShort(_ context.Context, raw json.RawMessage) (any, error) {
	var args assembleShortArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	p, err := s.projects.Get(args.ProjectID)
	if err != nil {
		return nil, err
	}
	assets, err := s.projects.Assets(args.ProjectID)
	if err != nil {
		return nil, err
	}
	assetMap := make(map[string]*models.Asset, len(assets))
	for _, a := range assets {
		assetMap[a.ID] = a
	}

	voAsset := assetMap[args.Voiceover.AssetID]
	if voAsset == nil {
		return nil, fmt.Errorf("voiceover asset %s not found in project", args.Voiceover.AssetID)
	}

	tl := models.NewTimeline(p.Width, p.Height, p.FPS)
	if args.CaptionStyle != nil {
		mergeCaptionStyle(&tl.CaptionStyle, *args.CaptionStyle)
	}

	voVol := 1.0
	if args.Voiceover.Volume != nil {
		voVol = *args.Voiceover.Volume
	}
	voClip := models.Clip{
		ID:       "clip_vo_main",
		AssetID:  voAsset.ID,
		Start:    0,
		Duration: voAsset.Duration,
		In:       0,
		Out:      voAsset.Duration,
		Speed:    1,
		Scale:    1,
		Opacity:  1,
		Volume:   voVol,
	}

	// Place VO clip on audio-main track and apply any cuts
	for i := range tl.Tracks {
		if tl.Tracks[i].Kind == models.TrackAudio {
			tl.Tracks[i].Clips = []models.Clip{voClip}
			break
		}
	}
	if len(args.Voiceover.Cuts) > 0 {
		timeline.ApplyCuts(&tl, []string{voClip.ID}, args.Voiceover.Cuts, true)
	}

	// Compute total voiceover timeline duration after cuts
	voTimelineDur := models.TimelineDuration(tl)
	if voTimelineDur <= 0 {
		voTimelineDur = voAsset.Duration
	}

	// Build visual clips and caption exclusion intervals
	var videoClips []models.Clip
	var excludeRanges []models.TimeRange
	cursor := 0.0

	for idx, seg := range args.VisualSegments {
		a := assetMap[seg.AssetID]
		if a == nil {
			return nil, fmt.Errorf("visual_segments[%d]: asset %s not found", idx, seg.AssetID)
		}
		start := cursor
		if seg.Start != nil {
			start = *seg.Start
		}
		in := seg.In
		if in < 0 {
			in = 0
		}
		var dur float64
		if seg.Duration != nil && *seg.Duration > 0 {
			dur = *seg.Duration
		} else if seg.Out > in {
			dur = seg.Out - in
		} else if a.Duration > in {
			dur = a.Duration - in
		} else {
			dur = 3.0
		}
		out := in + dur
		if a.Duration > 0 && out > a.Duration {
			out = a.Duration
			dur = out - in
		}

		keepAudio := seg.SuppressCaptions
		if seg.KeepAudio != nil {
			keepAudio = *seg.KeepAudio
		}
		vol := 0.0
		if keepAudio {
			vol = 1.0
		}
		if seg.Volume != nil {
			vol = *seg.Volume
		}

		bgFill := seg.BackgroundFill
		if bgFill == "" && a.Width > a.Height && p.Height > p.Width {
			// Default 16:9 clip inside 9:16 canvas to blurred background fill
			bgFill = "blur"
		}

		trans := seg.Transition
		if trans == nil && idx > 0 {
			sceneMode := args.SceneTransition
			if strings.TrimSpace(sceneMode) == "" {
				sceneMode = "variety"
			}
			trans = resolveSceneTransition(sceneMode, idx-1, args.TransitionDuration)
		}

		clip := models.Clip{
			ID:             fmt.Sprintf("clip_v_%02d", idx+1),
			AssetID:        a.ID,
			Start:          math.Round(start*1000) / 1000,
			Duration:       math.Round(dur*1000) / 1000,
			In:             math.Round(in*1000) / 1000,
			Out:            math.Round(out*1000) / 1000,
			X:              0.5,
			Y:              0.5,
			Scale:          1,
			Opacity:        1,
			BlendMode:      "normal",
			Volume:         vol,
			Speed:          1,
			BackgroundFill: bgFill,
			BackgroundBlur: seg.BackgroundBlur,
			BackgroundDim:  seg.BackgroundDim,
			Transition:     trans,
		}
		videoClips = append(videoClips, clip)
		if seg.SuppressCaptions {
			excludeRanges = append(excludeRanges, models.TimeRange{
				Start: clip.Start,
				End:   clip.Start + clip.Duration,
			})
		}
		cursor = clip.Start + clip.Duration
	}

	for i := range tl.Tracks {
		if tl.Tracks[i].Kind == models.TrackVideo {
			tl.Tracks[i].Clips = videoClips
			break
		}
	}

	totalDur := models.TimelineDuration(tl)

	// Optional BGM track
	if args.BGM != nil && strings.TrimSpace(args.BGM.AssetID) != "" {
		bgmAsset := assetMap[args.BGM.AssetID]
		if bgmAsset == nil {
			return nil, fmt.Errorf("bgm asset %s not found", args.BGM.AssetID)
		}
		bgmVol := 0.12
		if args.BGM.Volume != nil {
			bgmVol = *args.BGM.Volume
		}
		bgmIn := args.BGM.In
		bgmDur := totalDur
		if bgmAsset.Duration > 0 && bgmIn+bgmDur > bgmAsset.Duration {
			bgmDur = bgmAsset.Duration - bgmIn
		}
		tl.Tracks = append(tl.Tracks, models.Track{
			ID:     "audio-bgm",
			Kind:   models.TrackAudio,
			Name:   "BGM",
			Height: models.DefaultTrackHeight,
			Clips: []models.Clip{
				{
					ID:       "clip_bgm_main",
					AssetID:  bgmAsset.ID,
					Start:    0,
					Duration: bgmDur,
					In:       bgmIn,
					Out:      bgmIn + bgmDur,
					Speed:    1,
					Scale:    1,
					Opacity:  1,
					Volume:   bgmVol,
					FadeIn:   args.BGM.FadeIn,
					FadeOut:  args.BGM.FadeOut,
				},
			},
		})
	}

	// Auto-generate gated captions
	autoCap := true
	if args.AutoCaptions != nil {
		autoCap = *args.AutoCaptions
	}
	captionCount := 0
	if autoCap {
		if tr, err := s.projects.Transcript(args.ProjectID); err == nil && len(tr.Words) > 0 {
			var voClips []models.Clip
			for _, track := range tl.Tracks {
				if track.ID == "audio-bgm" || track.ID == "audio-sfx" {
					continue
				}
				if track.Kind == models.TrackAudio {
					voClips = append(voClips, track.Clips...)
				}
			}
			generated := captions.FromTranscriptWithExclusions(voClips, tr, tl.CaptionStyle, excludeRanges)
			captionCount = len(generated)
			for i := range tl.Tracks {
				if tl.Tracks[i].Kind == models.TrackCaption {
					tl.Tracks[i].Clips = generated
					break
				}
			}
		}
	}

	sfxCount, sfxApplied, _ := s.projects.SyncTransitionSFXTrack(args.ProjectID, &tl, "")

	models.NormalizeTimeline(&tl)
	if err := models.ValidateTimeline(tl); err != nil {
		return nil, fmt.Errorf("assembled timeline validation failed: %w", err)
	}
	if err := s.projects.SaveTimeline(args.ProjectID, &tl); err != nil {
		return nil, err
	}
	s.notifyLiveDesktop(args.ProjectID, "short_assembled")

	return map[string]any{
		"project_id":           args.ProjectID,
		"timeline_duration":    models.TimelineDuration(tl),
		"video_clips":          len(videoClips),
		"caption_clips":        captionCount,
		"transition_sfx_count": sfxCount,
		"transition_sfx":       sfxApplied,
		"excluded_ranges":      excludeRanges,
	}, nil
}

func (s *Server) handleExportProject(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID  string `json:"project_id"`
		Filename   string `json:"filename"`
		OutputPath string `json:"output_path"`
		Encoder    string `json:"encoder"`
		Quality    int    `json:"quality"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	p, err := s.projects.Get(args.ProjectID)
	if err != nil {
		return nil, err
	}
	assets, err := s.projects.Assets(args.ProjectID)
	if err != nil {
		return nil, err
	}
	assetsByID := make(map[string]*models.Asset, len(assets))
	for _, a := range assets {
		assetsByID[a.ID] = a
	}

	filename := strings.TrimSpace(args.Filename)
	if filename == "" {
		filename = p.Name + ".mp4"
	}
	if !strings.HasSuffix(strings.ToLower(filename), ".mp4") {
		filename += ".mp4"
	}
	exportPath := filepath.Join(s.projects.ExportsDir(args.ProjectID), filepath.Base(filename))
	encoder := strings.TrimSpace(args.Encoder)
	if encoder == "" {
		encoder = "auto"
	}
	quality := args.Quality
	if quality <= 0 {
		quality = 20
	}

	settings := models.ExportSettings{
		Width:    p.Timeline.Width,
		Height:   p.Timeline.Height,
		FPS:      p.Timeline.FPS,
		Encoder:  encoder,
		Quality:  quality,
		Filename: filepath.Base(filename),
	}

	started := time.Now()
	if err := s.renderer.Render(ctx, render.RenderRequest{
		Timeline:   p.Timeline,
		Assets:     assetsByID,
		Settings:   settings,
		OutputPath: exportPath,
		CacheDir:   s.projects.CacheDir(args.ProjectID),
	}); err != nil {
		return nil, err
	}

	info, err := s.projects.FFmpeg().Probe(exportPath)
	if err != nil {
		return nil, fmt.Errorf("rendered %s but probe failed: %w", exportPath, err)
	}

	res := map[string]any{
		"project_id":   args.ProjectID,
		"export_path":  exportPath,
		"duration_sec": info.Duration,
		"width":        info.Width,
		"height":       info.Height,
		"fps":          info.FPS,
		"has_audio":    info.HasAudio,
		"size_bytes":   info.Size,
		"elapsed_sec":  math.Round(time.Since(started).Seconds()*100) / 100,
	}

	if strings.TrimSpace(args.OutputPath) != "" {
		if err := copyFile(exportPath, args.OutputPath); err != nil {
			return nil, fmt.Errorf("exported to %s, but copying to %s failed: %w", exportPath, args.OutputPath, err)
		}
		res["copied_to"] = args.OutputPath
	}
	s.notifyLiveDesktop(args.ProjectID, "export_completed")
	return res, nil
}

func mergeCaptionStyle(dst *models.CaptionStyle, src models.CaptionStyle) {
	if src.FontFamily != "" {
		dst.FontFamily = src.FontFamily
	}
	if src.FontFile != "" {
		dst.FontFile = src.FontFile
	}
	if src.FontSize > 0 {
		dst.FontSize = src.FontSize
	}
	dst.Bold = src.Bold
	dst.Italic = src.Italic
	if src.Color != "" {
		dst.Color = src.Color
	}
	if src.HighlightColor != "" {
		dst.HighlightColor = src.HighlightColor
	}
	if src.StrokeWidth >= 0 {
		dst.StrokeWidth = src.StrokeWidth
	}
	if src.StrokeColor != "" {
		dst.StrokeColor = src.StrokeColor
	}
	if src.Shadow >= 0 {
		dst.Shadow = src.Shadow
	}
	if src.ShadowColor != "" {
		dst.ShadowColor = src.ShadowColor
	}
	if src.Opacity > 0 {
		dst.Opacity = src.Opacity
	}
	if src.PositionY > 0 {
		dst.PositionY = src.PositionY
	}
	dst.Uppercase = src.Uppercase
	dst.Highlight = src.Highlight
	if src.WordsPerChunk > 0 {
		dst.WordsPerChunk = src.WordsPerChunk
	}
	if src.BackgroundColor != "" {
		dst.BackgroundColor = src.BackgroundColor
	}
	if src.BackgroundAlpha >= 0 {
		dst.BackgroundAlpha = src.BackgroundAlpha
	}
}

func copyFile(src, dst string) error {
	data, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	in, err := sFileOpen(src)
	if err != nil {
		return err
	}
	defer in.Close()
	return writeToFile(data, in)
}
