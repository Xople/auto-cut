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
	"sync"
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
		Description: "Extract and trim a muted background video snippet (±4.5s) from a source footage file (.mkv/.mp4) into BRollEngine/public/videos/<topic>/<slug>.mp4 for DeskriptifBroll parallax backgrounds. Set pre_blur=true to bake blur & dim in FFmpeg (<0.5s) so Remotion skips expensive per-frame CSS blur.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source_path": map[string]any{"type": "string", "description": "Absolute path to source video (.mkv or .mp4)"},
				"start_sec":   map[string]any{"type": "number", "description": "Start timestamp in seconds in the source video"},
				"duration":    map[string]any{"type": "number", "description": "Snippet duration in seconds (default 4.5)"},
				"topic":       map[string]any{"type": "string", "description": "Topic folder slug (e.g. 'among-us')"},
				"slug":        map[string]any{"type": "string", "description": "Output filename slug without extension (e.g. 'sepi-2018-bg')"},
				"pre_blur":    map[string]any{"type": "boolean", "description": "Bake Gaussian blur + brightness dim directly in FFmpeg for 5x-10x faster Remotion renders (use with blur:0, dim:0 in spec)"},
				"blur":        map[string]any{"type": "number", "description": "Blur radius when pre_blur=true (default 3.0)"},
				"dim":         map[string]any{"type": "number", "description": "Darken factor 0..0.8 when pre_blur=true (default 0.45)"},
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
		Description: "Render a 1080x1920 MP4 motion graphic clip in BRollEngine (supports fast 30fps or 60fps and concurrency tuning) and optionally import it directly into an AutoCut project.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"composition":    map[string]any{"type": "string", "description": "Composition name (e.g. 'ImpactPoster', 'HookScene', 'DeskriptifBroll', 'DynamicBroll', 'GameDeal')"},
				"props_path":     map[string]any{"type": "string", "description": "Path to spec JSON (relative to BRollEngine or absolute)"},
				"output_path":    map[string]any{"type": "string", "description": "Output MP4 path (relative to BRollEngine or absolute)"},
				"fps":            map[string]any{"type": "integer", "description": "Optional FPS override: 30 (2x faster) or 60"},
				"concurrency":    map[string]any{"type": "integer", "description": "Optional Remotion browser tab concurrency (default 3 for stable speed on 6-core CPUs)"},
				"project_id":     map[string]any{"type": "string", "description": "Optional AutoCut project ID to automatically import the rendered clip into"},
				"skip_if_exists": map[string]any{"type": "boolean", "description": "If true and output_path already exists, skip re-rendering and reuse the existing file"},
			},
			"required": []string{"composition", "props_path", "output_path"},
		},
	}, s.handleBRollRenderClip)

	s.RegisterTool(ToolDefinition{
		Name:        "broll_render_batch",
		Description: "Render multiple BRollEngine clips in ONE single Webpack bundle (much faster than calling broll_render_clip per clip) and optionally auto-import all rendered clips into an AutoCut project.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":     map[string]any{"type": "string", "description": "Optional AutoCut project ID to import all rendered clips into"},
				"fps":            map[string]any{"type": "integer", "description": "Default FPS for all clips: 30 (2x faster) or 60"},
				"concurrency":    map[string]any{"type": "integer", "description": "Remotion concurrency per clip (default 3)"},
				"skip_if_exists": map[string]any{"type": "boolean", "description": "Skip rendering any output_path that already exists on disk"},
				"clips": map[string]any{
					"type":        "array",
					"description": "Array of clips to render: [{ composition, props_path, output_path, fps? }]",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"composition": map[string]any{"type": "string"},
							"props_path":  map[string]any{"type": "string"},
							"output_path": map[string]any{"type": "string"},
							"fps":         map[string]any{"type": "integer"},
						},
						"required": []string{"composition", "props_path", "output_path"},
					},
				},
			},
			"required": []string{"clips"},
		},
	}, s.handleBRollRenderBatch)
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
		PreBlur    bool    `json:"pre_blur"`
		Blur       float64 `json:"blur"`
		Dim        float64 `json:"dim"`
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

	vf := "scale=960:540:force_original_aspect_ratio=increase,crop=960:540"
	if args.PreBlur {
		blur := args.Blur
		if blur <= 0 {
			blur = 3.0
		}
		dim := args.Dim
		if dim <= 0 {
			dim = 0.45
		}
		if dim > 0.85 {
			dim = 0.85
		}
		vf = fmt.Sprintf("scale=480:270:force_original_aspect_ratio=increase,crop=480:270,gblur=sigma=%.2f,scale=960:540:flags=bilinear,eq=brightness=-%.2f", blur, dim)
	}

	ffArgs := []string{
		"-hide_banner", "-y", "-v", "error",
		"-ss", strconv.FormatFloat(args.StartSec, 'f', 3, 64),
		"-t", strconv.FormatFloat(args.Duration, 'f', 3, 64),
		"-i", args.SourcePath,
		"-an",
		"-vf", vf,
		"-r", "30",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23",
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
		"pre_blurred":          args.PreBlur,
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
		FPS          int    `json:"fps"`
		Concurrency  int    `json:"concurrency"`
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
		if args.FPS == 30 || args.FPS == 60 {
			cmdArgs = append(cmdArgs, "--fps", strconv.Itoa(args.FPS))
		}
		conc := args.Concurrency
		if conc <= 0 {
			conc = 4
		}
		cmdArgs = append(cmdArgs, "--concurrency", strconv.Itoa(conc))
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

func (s *Server) handleBRollRenderBatch(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		ProjectID    string `json:"project_id"`
		FPS          int    `json:"fps"`
		Concurrency  int    `json:"concurrency"`
		SkipIfExists bool   `json:"skip_if_exists"`
		Clips        []struct {
			Composition string `json:"composition"`
			PropsPath   string `json:"props_path"`
			OutputPath  string `json:"output_path"`
			FPS         int    `json:"fps"`
		} `json:"clips"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if len(args.Clips) == 0 {
		return nil, fmt.Errorf("clips must contain at least one item")
	}

	conc := args.Concurrency
	if conc <= 0 {
		conc = 4
	}

	type batchJob struct {
		Composition string `json:"comp"`
		PropsPath   string `json:"props"`
		OutputPath  string `json:"out"`
		FPS         int    `json:"fps,omitempty"`
	}

	toRender := make([]batchJob, 0, len(args.Clips))
	resolvedOutputs := make([]string, len(args.Clips))
	skippedFlags := make([]bool, len(args.Clips))

	for i, c := range args.Clips {
		outPath := c.OutputPath
		if !filepath.IsAbs(outPath) {
			outPath = filepath.Join(s.brollDir, outPath)
		}
		resolvedOutputs[i] = outPath
		if args.SkipIfExists {
			if info, err := os.Stat(outPath); err == nil && info.Size() > 0 {
				skippedFlags[i] = true
				continue
			}
		}
		clipFPS := c.FPS
		if clipFPS != 30 && clipFPS != 60 {
			clipFPS = args.FPS
		}
		toRender = append(toRender, batchJob{
			Composition: c.Composition,
			PropsPath:   c.PropsPath,
			OutputPath:  outPath,
			FPS:         clipFPS,
		})
	}

	var renderLog string
	if len(toRender) > 0 {
		manifestFile, err := os.CreateTemp("", "broll-batch-*.json")
		if err != nil {
			return nil, err
		}
		manifestPath := manifestFile.Name()
		defer os.Remove(manifestPath)

		manifestBytes, err := json.MarshalIndent(map[string]any{
			"concurrency": conc,
			"jobs":        toRender,
		}, "", "  ")
		if err != nil {
			manifestFile.Close()
			return nil, err
		}
		if _, err := manifestFile.Write(manifestBytes); err != nil {
			manifestFile.Close()
			return nil, err
		}
		manifestFile.Close()

		cmd := exec.CommandContext(ctx, "node", "scripts/broll.mjs", "batch", "--manifest", manifestPath)
		cmd.Dir = s.brollDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("broll batch render failed: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		renderLog = strings.TrimSpace(string(out))
	}

	items := make([]map[string]any, len(args.Clips))
	importErrs := make([]error, len(args.Clips))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, outPath := range resolvedOutputs {
		items[i] = map[string]any{
			"composition":    args.Clips[i].Composition,
			"output_path":    outPath,
			"skipped_render": skippedFlags[i],
		}
		if strings.TrimSpace(args.ProjectID) != "" {
			wg.Add(1)
			go func(idx int, p string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				asset, err := s.projects.ImportPath(args.ProjectID, p)
				if err != nil {
					importErrs[idx] = fmt.Errorf("rendered clip at %s, but import to project %s failed: %w", p, args.ProjectID, err)
					return
				}
				items[idx]["imported_asset"] = asset
			}(i, outPath)
		}
	}
	wg.Wait()
	for _, err := range importErrs {
		if err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(args.ProjectID) != "" {
		s.notifyLiveDesktop(args.ProjectID, "assets_imported")
	}

	return map[string]any{
		"rendered_count": len(toRender),
		"total_count":    len(args.Clips),
		"items":          items,
		"log":            renderLog,
	}, nil
}
