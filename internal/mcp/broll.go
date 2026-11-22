package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func (s *Server) registerBRollTools() {
	s.RegisterTool(ToolDefinition{
		Name:        "broll_list_compositions",
		Description: "List available Remotion motion graphic compositions in BRollEngine (Hooks, DeskriptifBroll, DynamicBroll, Data Cards, Overlays) and their default durations.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, s.handleBRollList)

	s.RegisterTool(ToolDefinition{
		Name:        "broll_extract_bg_video",
		Description: "Extract and trim a muted 1280x720 background video snippet (±4.5s) from a source footage file (.mkv/.mp4) into BRollEngine/public/videos/<topic>/<slug>.mp4 for DeskriptifBroll parallax backgrounds.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source_path": map[string]any{"type": "string", "description": "Absolute path to source video (.mkv or .mp4)"},
				"start_sec":   map[string]any{"type": "number", "description": "Start timestamp in seconds in the source video"},
				"duration":    map[string]any{"type": "number", "description": "Snippet duration in seconds (default 4.5)"},
				"topic":       map[string]any{"type": "string", "description": "Topic folder slug (e.g. 'among-us')"},
				"slug":        map[string]any{"type": "string", "description": "Output filename slug without extension (e.g. 'sepi-2018-bg')"},
			},
			"required": []string{"source_path", "start_sec", "topic", "slug"},
		},
	}, s.handleBRollExtractBgVideo)

	s.RegisterTool(ToolDefinition{
		Name:        "broll_write_spec",
		Description: "Write a motion graphic props specification JSON file to BRollEngine/specs/topics/<topic>/<name>.json.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"topic": map[string]any{"type": "string", "description": "Topic slug (e.g. 'among-us')"},
				"name":  map[string]any{"type": "string", "description": "Spec filename (e.g. 'among-us-01.json')"},
				"props": map[string]any{"type": "object", "description": "Props object matching the target composition's Zod schema"},
			},
			"required": []string{"topic", "name", "props"},
		},
	}, s.handleBRollWriteSpec)

	s.RegisterTool(ToolDefinition{
		Name:        "broll_render_still",
		Description: "Render a single PNG still frame from a BRollEngine composition for visual QA (safe area check) or cover thumbnail generation (e.g. 1105-Cover.jpg).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"composition": map[string]any{"type": "string", "description": "Composition name (e.g. 'ImpactPoster', 'HookScene', 'ArrowHook', 'DeskriptifBroll')"},
				"props_path":  map[string]any{"type": "string", "description": "Relative or absolute path to the spec JSON file"},
				"frame":       map[string]any{"type": "integer", "description": "Frame number to render (default: 60% of duration)"},
				"output_path": map[string]any{"type": "string", "description": "Output PNG/JPG path (relative to BRollEngine or absolute)"},
				"scale":       map[string]any{"type": "number", "description": "Render scale (0.5 for quick QA, 1 for cover thumbnail)"},
			},
			"required": []string{"composition", "props_path", "output_path"},
		},
	}, s.handleBRollRenderStill)

	s.RegisterTool(ToolDefinition{
		Name:        "broll_render_clip",
		Description: "Render a 1080x1920 60fps MP4 motion graphic clip in BRollEngine and optionally import it directly into an AutoCut project.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"composition":    map[string]any{"type": "string", "description": "Composition name (e.g. 'ImpactPoster', 'HookScene', 'DeskriptifBroll', 'DynamicBroll', 'GameDeal')"},
				"props_path":     map[string]any{"type": "string", "description": "Path to spec JSON (relative to BRollEngine or absolute)"},
				"output_path":    map[string]any{"type": "string", "description": "Output MP4 path (relative to BRollEngine or absolute)"},
				"project_id":     map[string]any{"type": "string", "description": "Optional AutoCut project ID to automatically import the rendered clip into"},
				"skip_if_exists": map[string]any{"type": "boolean", "description": "If true and output_path already exists, skip re-rendering and reuse the existing file"},
			},
			"required": []string{"composition", "props_path", "output_path"},
		},
	}, s.handleBRollRenderClip)
}

func (s *Server) handleBRollList(ctx context.Context, _ json.RawMessage) (any, error) {
	cmd := exec.CommandContext(ctx, "node", "scripts/broll.mjs", "list")
	cmd.Dir = s.brollDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("broll list failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return map[string]any{
		"broll_engine_dir": s.brollDir,
		"output":           strings.TrimSpace(string(out)),
	}, nil
}

func (s *Server) handleBRollExtractBgVideo(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		SourcePath string  `json:"source_path"`
		StartSec   float64 `json:"start_sec"`
		Duration   float64 `json:"duration"`
		Topic      string  `json:"topic"`
		Slug       string  `json:"slug"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if args.Duration <= 0 {
		args.Duration = 4.5
	}
	relPath := filepath.ToSlash(filepath.Join("videos", args.Topic, args.Slug+".mp4"))
	destPath := filepath.Join(s.brollDir, "public", filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return nil, err
	}

	ffArgs := []string{
		"-hide_banner", "-y", "-v", "error",
		"-ss", strconv.FormatFloat(args.StartSec, 'f', 3, 64),
		"-t", strconv.FormatFloat(args.Duration, 'f', 3, 64),
		"-i", args.SourcePath,
		"-an",
		"-vf", "scale=1280:720:force_original_aspect_ratio=increase,crop=1280:720",
		"-c:v", "libx264", "-preset", "medium", "-crf", "22",
		"-pix_fmt", "yuv420p", "-movflags", "+faststart",
		destPath,
	}
	cmd := exec.CommandContext(ctx, s.cfg.FFmpegPath, ffArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg extract failed: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	return map[string]any{
		"public_relative_path": relPath,
		"absolute_path":        destPath,
		"duration":             args.Duration,
	}, nil
}

func (s *Server) handleBRollWriteSpec(_ context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		Topic string         `json:"topic"`
		Name  string         `json:"name"`
		Props map[string]any `json:"props"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if !strings.HasSuffix(strings.ToLower(args.Name), ".json") {
		args.Name += ".json"
	}
	relPath := filepath.ToSlash(filepath.Join("specs", "topics", args.Topic, args.Name))
	fullPath := filepath.Join(s.brollDir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(args.Props, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(fullPath, append(data, '\n'), 0644); err != nil {
		return nil, err
	}
	return map[string]any{
		"spec_relative_path": relPath,
		"absolute_path":      fullPath,
	}, nil
}

func (s *Server) handleBRollRenderStill(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		Composition string  `json:"composition"`
		PropsPath   string  `json:"props_path"`
		Frame       int     `json:"frame"`
		OutputPath  string  `json:"output_path"`
		Scale       float64 `json:"scale"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	outPath := args.OutputPath
	if !filepath.IsAbs(outPath) {
		outPath = filepath.Join(s.brollDir, outPath)
	}
	cmdArgs := []string{"scripts/broll.mjs", "still", "--comp", args.Composition, "--props", args.PropsPath, "--out", outPath}
	if args.Frame > 0 {
		cmdArgs = append(cmdArgs, "--frame", strconv.Itoa(args.Frame))
	}
	if args.Scale > 0 {
		cmdArgs = append(cmdArgs, "--scale", strconv.FormatFloat(args.Scale, 'f', 2, 64))
	}
	cmd := exec.CommandContext(ctx, "node", cmdArgs...)
	cmd.Dir = s.brollDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("broll still failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return map[string]any{
		"output_path": outPath,
		"log":         strings.TrimSpace(string(out)),
	}, nil
}

func (s *Server) handleBRollRenderClip(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		Composition  string `json:"composition"`
		PropsPath    string `json:"props_path"`
		OutputPath   string `json:"output_path"`
		ProjectID    string `json:"project_id"`
		SkipIfExists bool   `json:"skip_if_exists"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	outPath := args.OutputPath
	if !filepath.IsAbs(outPath) {
		outPath = filepath.Join(s.brollDir, outPath)
	}
	skipped := false
	if args.SkipIfExists {
		if info, err := os.Stat(outPath); err == nil && info.Size() > 0 {
			skipped = true
		}
	}
	var renderLog string
	if !skipped {
		cmdArgs := []string{"scripts/broll.mjs", "render", "--comp", args.Composition, "--props", args.PropsPath, "--out", outPath}
		cmd := exec.CommandContext(ctx, "node", cmdArgs...)
		cmd.Dir = s.brollDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("broll render failed: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		renderLog = strings.TrimSpace(string(out))
	}

	result := map[string]any{
		"output_path":    outPath,
		"skipped_render": skipped,
		"log":            renderLog,
	}

	if strings.TrimSpace(args.ProjectID) != "" {
		asset, err := s.projects.ImportPath(args.ProjectID, outPath)
		if err != nil {
			return nil, fmt.Errorf("rendered clip at %s, but import to project %s failed: %w", outPath, args.ProjectID, err)
		}
		s.notifyLiveDesktop(args.ProjectID, "asset_imported")
		result["imported_asset"] = asset
	}
	return result, nil
}
