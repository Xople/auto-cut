package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type dropLoadRuntime struct {
	Dir                  string
	YTDLPPath            string
	FFmpegPath           string
	FFprobePath          string
	CookiesPath          string
	CookiesFromBrowser   string
	YouTubePlayerClients string
	YouTubePOTBaseURL    string
	JSRuntimes           string
	StorageDir           string
	MaterialsDir         string
	APIBaseURL           string
}

type youTubeCandidate struct {
	ID                string `json:"id"`
	URL               string `json:"url"`
	Title             string `json:"title"`
	Channel           string `json:"channel"`
	Duration          int    `json:"duration"`
	DurationFormatted string `json:"duration_formatted"`
	ViewCount         int64  `json:"view_count,omitempty"`
	Thumbnail         string `json:"thumbnail,omitempty"`
	Description       string `json:"description,omitempty"`
	IsLive            bool   `json:"is_live"`
}

type dropLoadChapter struct {
	Title          string  `json:"title"`
	StartTime      float64 `json:"start_time"`
	EndTime        float64 `json:"end_time"`
	StartFormatted string  `json:"start_formatted"`
	EndFormatted   string  `json:"end_formatted"`
}

type dropLoadFormat struct {
	ID       string   `json:"id"`
	Type     string   `json:"type"`
	Label    string   `json:"label"`
	Height   int      `json:"height,omitempty"`
	FPS      float64  `json:"fps,omitempty"`
	Ext      string   `json:"ext"`
	VCodec   string   `json:"vcodec,omitempty"`
	ACodec   string   `json:"acodec,omitempty"`
	Filesize int64    `json:"filesize,omitempty"`
	ABR      *float64 `json:"abr,omitempty"`
}

type dropLoadMediaInfo struct {
	ID                string            `json:"id"`
	Title             string            `json:"title"`
	Channel           string            `json:"channel"`
	ChannelURL        string            `json:"channel_url,omitempty"`
	Duration          int               `json:"duration"`
	DurationFormatted string            `json:"duration_formatted"`
	IsLive            bool              `json:"is_live"`
	WebpageURL        string            `json:"webpage_url"`
	Thumbnail         string            `json:"thumbnail,omitempty"`
	Description       string            `json:"description,omitempty"`
	Chapters          []dropLoadChapter `json:"chapters,omitempty"`
	Subtitles         []string          `json:"subtitles,omitempty"`
	Formats           []dropLoadFormat  `json:"formats"`
	EngineUsed        string            `json:"engine_used"`
}

type dropLoadDownloadArgs struct {
	URL            string  `json:"url"`
	Topic          string  `json:"topic"`
	Slug           string  `json:"slug"`
	MediaType      string  `json:"media_type"`
	Quality        string  `json:"quality"`
	FormatID       string  `json:"format_id"`
	ClipStart      any     `json:"clip_start"`
	ClipEnd        any     `json:"clip_end"`
	OutputDir      string  `json:"output_dir"`
	ProjectID      string  `json:"project_id"`
	ExtractBRollBG bool    `json:"extract_broll_bg"`
	BRollBGStart   float64 `json:"broll_bg_start_sec"`
	BRollBGDur     float64 `json:"broll_bg_duration"`
	SkipIfExists   *bool   `json:"skip_if_exists"`
	UseAPI         bool    `json:"use_api"`
}

func (s *Server) registerDropLoadTools() {
	s.RegisterTool(ToolDefinition{
		Name:        "dropload_status",
		Description: "Check DropLoad (D:\\Dev\\.Projects\\DropLoad) integration status, resolved yt-dlp/ffmpeg/cookies paths, material storage directory, and whether the live DropLoad HTTP MCP API is reachable.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, s.handleDropLoadStatus)

	s.RegisterTool(ToolDefinition{
		Name:        "dropload_search_youtube",
		Description: "Search YouTube via DropLoad's yt-dlp engine to find candidate B-roll, gameplay, trailer, or news footage videos relevant to a script beat or topic before downloading.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":        map[string]any{"type": "string", "description": "YouTube search query derived from the script or topic (e.g. 'Among Us official gameplay trailer no commentary')"},
				"max_results":  map[string]any{"type": "integer", "description": "Maximum number of video candidates to return (1-20, default 6)"},
				"min_duration": map[string]any{"type": "integer", "description": "Optional minimum video duration in seconds"},
				"max_duration": map[string]any{"type": "integer", "description": "Optional maximum video duration in seconds (e.g. 900 to skip multi-hour streams)"},
				"exclude_live": map[string]any{"type": "boolean", "description": "Exclude active or upcoming livestreams (default true)"},
			},
			"required": []string{"query"},
		},
	}, s.handleDropLoadSearchYouTube)

	s.RegisterTool(ToolDefinition{
		Name:        "dropload_extract_info",
		Description: "Inspect metadata, chapters/timestamps, duration, channel, subtitles, and available formats of a YouTube or social media URL via DropLoad before downloading.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":     map[string]any{"type": "string", "description": "Full HTTP/HTTPS URL of the video or audio to inspect"},
				"use_api": map[string]any{"type": "boolean", "description": "If true, try DropLoad's running HTTP MCP server first before falling back to local DropLoad yt-dlp (default false)"},
			},
			"required": []string{"url"},
		},
	}, s.handleDropLoadExtractInfo)

	s.RegisterTool(ToolDefinition{
		Name:        "dropload_download_material",
		Description: "Download a YouTube/media video (full or specific clip_start..clip_end range) as production material using DropLoad (D:\\Dev\\.Projects\\DropLoad), optionally auto-importing it directly into an AutoCut project and/or extracting a muted 1280x720 BRollEngine parallax background.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"url":                map[string]any{"type": "string", "description": "Full HTTP/HTTPS URL of the YouTube/media video to download"},
				"topic":              map[string]any{"type": "string", "description": "Topic folder slug to organize materials (e.g. 'among-us', 'dota2-esic'; default 'general')"},
				"slug":               map[string]any{"type": "string", "description": "Clean output filename slug without extension (auto-generated from video title if omitted)"},
				"media_type":         map[string]any{"type": "string", "enum": []string{"video+audio", "video", "audio"}, "description": "Stream type: 'video+audio' (default), 'video' (silent B-roll), or 'audio' (m4a)"},
				"quality":            map[string]any{"type": "string", "description": "Preferred max resolution height in pixels: '2160', '1440', '1080' (default), '720', '480', or 'best'"},
				"format_id":          map[string]any{"type": "string", "description": "Optional explicit yt-dlp format selector (e.g. '137+140')"},
				"clip_start":         map[string]any{"type": "string", "description": "Optional clip start timestamp (e.g. '00:45', '01:15.5', or seconds '45')"},
				"clip_end":           map[string]any{"type": "string", "description": "Optional clip end timestamp (e.g. '01:10', '01:40.0', or seconds '70')"},
				"output_dir":         map[string]any{"type": "string", "description": "Optional custom absolute directory override (defaults to DropLoad/storage/downloads/materials/<topic>)"},
				"project_id":         map[string]any{"type": "string", "description": "Optional AutoCut project ID to automatically import the downloaded material into"},
				"extract_broll_bg":   map[string]any{"type": "boolean", "description": "If true, also extract a muted 1280x720 background video snippet into BRollEngine/public/videos/<topic>/<slug>-bg.mp4"},
				"broll_bg_start_sec": map[string]any{"type": "number", "description": "Start offset in seconds inside the downloaded video for BRollEngine BG extraction (default 0)"},
				"broll_bg_duration":  map[string]any{"type": "number", "description": "Duration in seconds for BRollEngine BG extraction (default 4.5)"},
				"skip_if_exists":     map[string]any{"type": "boolean", "description": "If true (default) and the target file already exists, reuse it without re-downloading"},
				"use_api":            map[string]any{"type": "boolean", "description": "If true, route through DropLoad's running HTTP MCP API when available, falling back to local DropLoad yt-dlp if offline"},
			},
			"required": []string{"url"},
		},
	}, s.handleDropLoadDownloadMaterial)

	s.RegisterTool(ToolDefinition{
		Name:        "dropload_batch_download_materials",
		Description: "Download multiple script-related YouTube video materials in one batch via DropLoad. Each item can specify a direct url OR a YouTube search query (auto-selecting the top matching non-live candidate), optional clip_start/clip_end, and auto-imports all downloaded assets into an AutoCut project.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"topic":          map[string]any{"type": "string", "description": "Topic folder slug (e.g. 'among-us')"},
				"project_id":     map[string]any{"type": "string", "description": "Optional AutoCut project ID to import all downloaded materials into"},
				"quality":        map[string]any{"type": "string", "description": "Default max resolution height for all items (default '1080')"},
				"media_type":     map[string]any{"type": "string", "enum": []string{"video+audio", "video", "audio"}, "description": "Default media stream type (default 'video+audio')"},
				"skip_if_exists": map[string]any{"type": "boolean", "description": "Reuse already downloaded material files (default true)"},
				"use_api":        map[string]any{"type": "boolean", "description": "Use live DropLoad HTTP MCP API when available (default false)"},
				"items": map[string]any{
					"type":        "array",
					"description": "Ordered list of script beat material requests: [{ beat?, url?, query?, slug?, clip_start?, clip_end?, quality?, media_type?, min_duration?, max_duration?, extract_broll_bg? }]",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"beat":             map[string]any{"type": "string", "description": "Script beat or narrative cue label for traceability"},
							"url":              map[string]any{"type": "string", "description": "Direct YouTube/media URL (if omitted, query is searched on YouTube)"},
							"query":            map[string]any{"type": "string", "description": "YouTube search query when url is omitted"},
							"slug":             map[string]any{"type": "string", "description": "Output filename slug"},
							"clip_start":       map[string]any{"type": "string", "description": "Optional clip start timestamp"},
							"clip_end":         map[string]any{"type": "string", "description": "Optional clip end timestamp"},
							"quality":          map[string]any{"type": "string", "description": "Optional per-item quality override"},
							"media_type":       map[string]any{"type": "string", "description": "Optional per-item media_type override"},
							"min_duration":     map[string]any{"type": "integer", "description": "Optional minimum video duration in seconds when searching by query"},
							"max_duration":     map[string]any{"type": "integer", "description": "Optional maximum video duration in seconds when searching by query"},
							"extract_broll_bg": map[string]any{"type": "boolean", "description": "Also extract a muted 1280x720 BRollEngine parallax background"},
						},
					},
				},
			},
			"required": []string{"items"},
		},
	}, s.handleDropLoadBatchDownload)

	s.RegisterTool(ToolDefinition{
		Name:        "dropload_list_materials",
		Description: "List downloaded video and audio materials stored in DropLoad's storage directory (optionally filtered by topic) so existing footage can be reused or imported into AutoCut without re-downloading.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"topic":        map[string]any{"type": "string", "description": "Optional topic subfolder filter under DropLoad/storage/downloads/materials/<topic>"},
				"include_root": map[string]any{"type": "boolean", "description": "Include top-level downloaded files in DropLoad/storage/downloads (default true when topic is empty)"},
				"probe":        map[string]any{"type": "boolean", "description": "Run ffprobe to include duration and resolution metadata (default false)"},
			},
		},
	}, s.handleDropLoadListMaterials)
}

func (s *Server) resolveDropLoadRuntime() dropLoadRuntime {
	dir := strings.TrimSpace(s.droploadDir)
	if dir == "" {
		dir = `D:\Dev\.Projects\DropLoad`
	}

	envMap := map[string]string{}
	// Load root .env first, then server/.env overrides (closer to Go backend config).
	loadSimpleEnvFile(filepath.Join(dir, ".env"), envMap)
	loadSimpleEnvFile(filepath.Join(dir, "server", ".env"), envMap)

	getVal := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(os.Getenv(k)); v != "" {
				return v
			}
			if v := strings.TrimSpace(envMap[k]); v != "" && !strings.Contains(v, "${{") {
				return v
			}
		}
		return ""
	}

	ytdlp := resolveDropLoadBinary(dir, getVal("YTDLP_PATH"), "yt-dlp")
	ffmpeg := resolveDropLoadBinary(dir, getVal("FFMPEG_PATH"), "ffmpeg")
	if !fileExistsOnDisk(ffmpeg) {
		if cfgFF := strings.TrimSpace(s.cfg.FFmpegPath); cfgFF != "" {
			if resolved := resolveExecutable(cfgFF); resolved != "" {
				ffmpeg = resolved
			}
		}
	}
	ffprobe := resolveCompanionProbe(dir, ffmpeg)

	cookies := resolveDropLoadCookies(dir, getVal("YTDLP_COOKIES_PATH", "COOKIES_PATH"))

	ytClients := getVal("YOUTUBE_PLAYER_CLIENTS")
	if ytClients == "" {
		ytClients = "default,tv,mweb"
	}
	jsRuntimes := getVal("JS_RUNTIMES")
	if jsRuntimes == "" {
		jsRuntimes = "node,deno"
	}

	storageDir := filepath.Join(dir, "storage", "downloads")
	if envStorage := getVal("STORAGE_DIR"); envStorage != "" {
		if filepath.IsAbs(envStorage) {
			storageDir = envStorage
		} else {
			storageDir = filepath.Clean(filepath.Join(dir, envStorage))
		}
	}

	apiBase := strings.TrimSpace(s.droploadURL)
	if apiBase == "" {
		apiBase = getVal("API_BASE_URL")
	}
	if apiBase == "" {
		port := getVal("API_PORT")
		if port == "" {
			port = "3001"
		}
		apiBase = "http://localhost:" + port
	}
	apiBase = strings.TrimRight(apiBase, "/")

	return dropLoadRuntime{
		Dir:                  dir,
		YTDLPPath:            ytdlp,
		FFmpegPath:           ffmpeg,
		FFprobePath:          ffprobe,
		CookiesPath:          cookies,
		CookiesFromBrowser:   getVal("COOKIES_FROM_BROWSER", "YTDLP_COOKIES_FROM_BROWSER"),
		YouTubePlayerClients: ytClients,
		YouTubePOTBaseURL:    getVal("YOUTUBE_POT_BASE_URL", "BGUTIL_POT_URL"),
		JSRuntimes:           jsRuntimes,
		StorageDir:           storageDir,
		MaterialsDir:         filepath.Join(storageDir, "materials"),
		APIBaseURL:           apiBase,
	}
}

func loadSimpleEnvFile(path string, dst map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || strings.ContainsAny(k, " \t") {
			continue
		}
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		if v != "" {
			dst[k] = v
		}
	}
}

func resolveDropLoadBinary(droploadDir, configured, name string) string {
	candidates := []string{}
	if configured != "" {
		if filepath.IsAbs(configured) {
			candidates = append(candidates, configured, configured+".exe")
		} else {
			candidates = append(candidates,
				filepath.Join(droploadDir, "server", configured),
				filepath.Join(droploadDir, "server", configured+".exe"),
				filepath.Join(droploadDir, configured),
				filepath.Join(droploadDir, configured+".exe"),
			)
		}
	}
	candidates = append(candidates,
		filepath.Join(droploadDir, "bin", name+".exe"),
		filepath.Join(droploadDir, "bin", name),
		filepath.Join(droploadDir, "server", "bin", name+".exe"),
		filepath.Join(droploadDir, "server", "bin", name),
	)
	for _, cand := range candidates {
		if fileExistsOnDisk(cand) {
			if abs, err := filepath.Abs(cand); err == nil {
				return abs
			}
			return cand
		}
	}
	if found := resolveExecutable(name); found != "" {
		return found
	}
	if configured != "" {
		return configured
	}
	return name
}

func resolveCompanionProbe(droploadDir, ffmpegPath string) string {
	candidates := []string{}
	if ffmpegPath != "" && filepath.IsAbs(ffmpegPath) {
		dir := filepath.Dir(ffmpegPath)
		candidates = append(candidates, filepath.Join(dir, "ffprobe.exe"), filepath.Join(dir, "ffprobe"))
	}
	candidates = append(candidates,
		filepath.Join(droploadDir, "bin", "ffprobe.exe"),
		filepath.Join(droploadDir, "bin", "ffprobe"),
	)
	for _, cand := range candidates {
		if fileExistsOnDisk(cand) {
			if abs, err := filepath.Abs(cand); err == nil {
				return abs
			}
			return cand
		}
	}
	if found := resolveExecutable("ffprobe"); found != "" {
		return found
	}
	return "ffprobe"
}

func resolveDropLoadCookies(droploadDir, configured string) string {
	candidates := []string{}
	if configured != "" {
		if filepath.IsAbs(configured) {
			candidates = append(candidates, configured)
		} else {
			candidates = append(candidates,
				filepath.Join(droploadDir, "server", configured),
				filepath.Join(droploadDir, configured),
				filepath.Join(droploadDir, "bin", filepath.Base(configured)),
			)
		}
	}
	candidates = append(candidates,
		filepath.Join(droploadDir, "bin", "cookies.txt"),
		filepath.Join(droploadDir, "cookies.txt"),
		filepath.Join(droploadDir, "server", "bin", "cookies.txt"),
	)
	for _, cand := range candidates {
		if fileExistsOnDisk(cand) {
			if abs, err := filepath.Abs(cand); err == nil {
				return abs
			}
			return cand
		}
	}
	return ""
}

func resolveExecutable(name string) string {
	if fileExistsOnDisk(name) {
		if abs, err := filepath.Abs(name); err == nil {
			return abs
		}
		return name
	}
	if found, err := exec.LookPath(name); err == nil {
		if abs, err := filepath.Abs(found); err == nil {
			return abs
		}
		return found
	}
	if !strings.HasSuffix(strings.ToLower(name), ".exe") {
		if found, err := exec.LookPath(name + ".exe"); err == nil {
			if abs, err := filepath.Abs(found); err == nil {
				return abs
			}
			return found
		}
	}
	return ""
}

func fileExistsOnDisk(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func dirExistsOnDisk(p string) bool {
	if strings.TrimSpace(p) == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func (s *Server) handleDropLoadStatus(ctx context.Context, _ json.RawMessage) (any, error) {
	rt := s.resolveDropLoadRuntime()
	ytdlpOK := fileExistsOnDisk(rt.YTDLPPath) || resolveExecutable(rt.YTDLPPath) != ""
	ytdlpVersion := ""
	if ytdlpOK {
		vCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(vCtx, rt.YTDLPPath, "--version").Output(); err == nil {
			ytdlpVersion = strings.TrimSpace(string(out))
		}
	}
	ffmpegOK := fileExistsOnDisk(rt.FFmpegPath) || resolveExecutable(rt.FFmpegPath) != ""
	apiReachable := isDropLoadAPIReachable(ctx, rt.APIBaseURL)

	return map[string]any{
		"dropload_dir":           rt.Dir,
		"dropload_dir_exists":    dirExistsOnDisk(rt.Dir),
		"ytdlp_path":             rt.YTDLPPath,
		"ytdlp_available":        ytdlpOK,
		"ytdlp_version":          ytdlpVersion,
		"ffmpeg_path":            rt.FFmpegPath,
		"ffmpeg_available":       ffmpegOK,
		"ffprobe_path":           rt.FFprobePath,
		"cookies_path":           rt.CookiesPath,
		"cookies_available":      rt.CookiesPath != "",
		"youtube_player_clients": rt.YouTubePlayerClients,
		"js_runtimes":            rt.JSRuntimes,
		"storage_dir":            rt.StorageDir,
		"materials_dir":          rt.MaterialsDir,
		"api_base_url":           rt.APIBaseURL,
		"api_reachable":          apiReachable,
		"mcp_endpoint":           rt.APIBaseURL + "/mcp",
	}, nil
}

func (s *Server) handleDropLoadSearchYouTube(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		Query       string `json:"query"`
		MaxResults  int    `json:"max_results"`
		MinDuration int    `json:"min_duration"`
		MaxDuration int    `json:"max_duration"`
		ExcludeLive *bool  `json:"exclude_live"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(args.Query)
	if query == "" {
		return nil, fmt.Errorf("parameter 'query' is required")
	}
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = 6
	}
	if maxResults > 20 {
		maxResults = 20
	}
	excludeLive := true
	if args.ExcludeLive != nil {
		excludeLive = *args.ExcludeLive
	}

	rt := s.resolveDropLoadRuntime()
	candidates, err := searchYouTubeWithDropLoad(ctx, rt, query, maxResults, args.MinDuration, args.MaxDuration, excludeLive)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"query":        query,
		"count":        len(candidates),
		"candidates":   candidates,
		"dropload_dir": rt.Dir,
		"ytdlp_path":   rt.YTDLPPath,
	}, nil
}

func searchYouTubeWithDropLoad(ctx context.Context, rt dropLoadRuntime, query string, maxResults, minDur, maxDur int, excludeLive bool) ([]youTubeCandidate, error) {
	fetchCount := maxResults
	if minDur > 0 || maxDur > 0 || excludeLive {
		fetchCount = maxResults * 2
		if fetchCount < 10 {
			fetchCount = 10
		}
		if fetchCount > 30 {
			fetchCount = 30
		}
	}

	searchCtx, cancel := withDefaultTimeout(ctx, 45*time.Second)
	defer cancel()

	cmdArgs := []string{
		"--flat-playlist",
		"--dump-single-json",
		"--no-warnings",
		"--no-colors",
		"--skip-download",
		fmt.Sprintf("ytsearch%d:%s", fetchCount, query),
	}
	stdout, stderr, err := runDropLoadCommand(searchCtx, rt.YTDLPPath, cmdArgs...)
	if err != nil {
		return nil, fmt.Errorf("dropload youtube search failed: %w (%s)", err, summarizeStderr(stderr))
	}

	var parsed struct {
		Entries []struct {
			ID          string  `json:"id"`
			URL         string  `json:"url"`
			WebpageURL  string  `json:"webpage_url"`
			Title       string  `json:"title"`
			Channel     string  `json:"channel"`
			Uploader    string  `json:"uploader"`
			Duration    float64 `json:"duration"`
			ViewCount   int64   `json:"view_count"`
			Description string  `json:"description"`
			LiveStatus  string  `json:"live_status"`
			IsLive      bool    `json:"is_live"`
			Thumbnails  []struct {
				URL    string `json:"url"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"thumbnails"`
		} `json:"entries"`
	}
	jsonLine := extractFirstJSONLine(stdout)
	if jsonLine == "" {
		return nil, fmt.Errorf("dropload youtube search returned no JSON payload")
	}
	if err := json.Unmarshal([]byte(jsonLine), &parsed); err != nil {
		return nil, fmt.Errorf("failed parsing youtube search output: %w", err)
	}

	results := make([]youTubeCandidate, 0, maxResults)
	for _, e := range parsed.Entries {
		isLive := e.IsLive || e.LiveStatus == "is_live" || e.LiveStatus == "is_upcoming"
		if excludeLive && isLive {
			continue
		}
		dur := int(math.Round(e.Duration))
		if minDur > 0 && dur < minDur {
			continue
		}
		if maxDur > 0 && dur > maxDur {
			continue
		}
		videoURL := strings.TrimSpace(e.WebpageURL)
		if videoURL == "" {
			videoURL = strings.TrimSpace(e.URL)
		}
		if videoURL != "" && !strings.HasPrefix(videoURL, "http://") && !strings.HasPrefix(videoURL, "https://") {
			videoURL = "https://www.youtube.com/watch?v=" + videoURL
		}
		if videoURL == "" && e.ID != "" {
			videoURL = "https://www.youtube.com/watch?v=" + e.ID
		}
		if videoURL == "" {
			continue
		}

		channel := strings.TrimSpace(e.Channel)
		if channel == "" {
			channel = strings.TrimSpace(e.Uploader)
		}
		thumb := ""
		if len(e.Thumbnails) > 0 {
			thumb = e.Thumbnails[len(e.Thumbnails)-1].URL
		}

		results = append(results, youTubeCandidate{
			ID:                e.ID,
			URL:               videoURL,
			Title:             strings.TrimSpace(e.Title),
			Channel:           channel,
			Duration:          dur,
			DurationFormatted: formatSecondsHMS(float64(dur)),
			ViewCount:         e.ViewCount,
			Thumbnail:         thumb,
			Description:       truncateText(strings.TrimSpace(e.Description), 240),
			IsLive:            isLive,
		})
		if len(results) >= maxResults {
			break
		}
	}
	return results, nil
}

func (s *Server) handleDropLoadExtractInfo(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		URL    string `json:"url"`
		UseAPI bool   `json:"use_api"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	safeURL, err := validateDropLoadURL(args.URL)
	if err != nil {
		return nil, err
	}

	rt := s.resolveDropLoadRuntime()
	if args.UseAPI && isDropLoadAPIReachable(ctx, rt.APIBaseURL) {
		if apiRes, apiErr := callDropLoadHTTPMCP(ctx, rt.APIBaseURL, "extract_media_info", map[string]any{"url": safeURL}); apiErr == nil {
			return map[string]any{
				"engine_used": "dropload_http_mcp",
				"api_url":     rt.APIBaseURL + "/mcp",
				"metadata":    apiRes,
			}, nil
		}
	}

	return extractMediaInfoLocal(ctx, rt, safeURL)
}

func extractMediaInfoLocal(ctx context.Context, rt dropLoadRuntime, safeURL string) (*dropLoadMediaInfo, error) {
	metaCtx, cancel := withDefaultTimeout(ctx, 45*time.Second)
	defer cancel()

	buildArgs := func(useCookies bool) []string {
		a := []string{
			"--dump-single-json",
			"--no-download",
			"--no-playlist",
			"--age-limit", "99",
			"--no-colors",
			"--no-warnings",
		}
		a = append(a, buildDropLoadCommonExtractorArgs(rt, useCookies)...)
		return append(a, safeURL)
	}

	useCookies := rt.CookiesPath != "" || rt.CookiesFromBrowser != ""
	stdout, stderr, err := runDropLoadCommand(metaCtx, rt.YTDLPPath, buildArgs(useCookies)...)
	if err != nil && useCookies {
		// Retry once without cookies in case exported cookies expired.
		if retryOut, _, retryErr := runDropLoadCommand(metaCtx, rt.YTDLPPath, buildArgs(false)...); retryErr == nil {
			stdout = retryOut
			err = nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("dropload extract info failed: %w (%s)", err, summarizeStderr(stderr))
	}

	jsonLine := extractFirstJSONLine(stdout)
	if jsonLine == "" {
		return nil, fmt.Errorf("yt-dlp did not return metadata JSON")
	}

	var rawMeta struct {
		ID                string  `json:"id"`
		Title             string  `json:"title"`
		Channel           string  `json:"channel"`
		Uploader          string  `json:"uploader"`
		ChannelURL        string  `json:"channel_url"`
		Duration          float64 `json:"duration"`
		IsLive            bool    `json:"is_live"`
		WebpageURL        string  `json:"webpage_url"`
		Thumbnail         string  `json:"thumbnail"`
		Description       string  `json:"description"`
		Subtitles         map[string]any `json:"subtitles"`
		AutomaticCaptions map[string]any `json:"automatic_captions"`
		Chapters          []struct {
			Title     string  `json:"title"`
			StartTime float64 `json:"start_time"`
			EndTime   float64 `json:"end_time"`
		} `json:"chapters"`
		Formats []struct {
			FormatID       string   `json:"format_id"`
			Ext            string   `json:"ext"`
			Height         *int     `json:"height"`
			FPS            *float64 `json:"fps"`
			VCodec         string   `json:"vcodec"`
			ACodec         string   `json:"acodec"`
			Filesize       int64    `json:"filesize"`
			FilesizeApprox int64    `json:"filesize_approx"`
			ABR            *float64 `json:"abr"`
		} `json:"formats"`
	}
	if err := json.Unmarshal([]byte(jsonLine), &rawMeta); err != nil {
		return nil, fmt.Errorf("failed parsing metadata JSON: %w", err)
	}

	channel := strings.TrimSpace(rawMeta.Channel)
	if channel == "" {
		channel = strings.TrimSpace(rawMeta.Uploader)
	}
	webpage := strings.TrimSpace(rawMeta.WebpageURL)
	if webpage == "" {
		webpage = safeURL
	}

	chapters := make([]dropLoadChapter, 0, len(rawMeta.Chapters))
	for _, ch := range rawMeta.Chapters {
		chapters = append(chapters, dropLoadChapter{
			Title:          strings.TrimSpace(ch.Title),
			StartTime:      ch.StartTime,
			EndTime:        ch.EndTime,
			StartFormatted: formatSecondsHMS(ch.StartTime),
			EndFormatted:   formatSecondsHMS(ch.EndTime),
		})
	}

	subSet := map[string]struct{}{}
	for k := range rawMeta.Subtitles {
		subSet[k] = struct{}{}
	}
	for k := range rawMeta.AutomaticCaptions {
		if k == "id" || k == "en" || strings.HasPrefix(k, "en-") || strings.HasPrefix(k, "id-") {
			subSet[k+" (auto)"] = struct{}{}
		}
	}
	subs := make([]string, 0, len(subSet))
	for k := range subSet {
		subs = append(subs, k)
	}
	sort.Strings(subs)

	// Build concise normalized format list (best video per height + top audio).
	bestByHeight := map[int]dropLoadFormat{}
	var bestAudio *dropLoadFormat
	for _, f := range rawMeta.Formats {
		if f.Ext == "" || f.Ext == "mhtml" || f.Ext == "m3u8" || f.Ext == "m3u8_native" {
			continue
		}
		hasV := f.VCodec != "" && f.VCodec != "none"
		hasA := f.ACodec != "" && f.ACodec != "none"
		size := f.Filesize
		if size <= 0 {
			size = f.FilesizeApprox
		}
		if hasV && f.Height != nil && *f.Height >= 240 {
			h := *f.Height
			fps := 0.0
			if f.FPS != nil {
				fps = *f.FPS
			}
			fType := "video"
			if hasA {
				fType = "video+audio"
			}
			cand := dropLoadFormat{
				ID:       f.FormatID,
				Type:     fType,
				Label:    fmt.Sprintf("%dp %s (%s)", h, strings.ToUpper(f.Ext), shortCodecName(f.VCodec)),
				Height:   h,
				FPS:      fps,
				Ext:      f.Ext,
				VCodec:   shortCodecName(f.VCodec),
				ACodec:   shortCodecName(f.ACodec),
				Filesize: size,
			}
			cur, exists := bestByHeight[h]
			if !exists || scoreDropLoadVideoFormat(cand) > scoreDropLoadVideoFormat(cur) {
				bestByHeight[h] = cand
			}
		} else if hasA && !hasV {
			abr := 0.0
			if f.ABR != nil {
				abr = *f.ABR
			}
			cand := dropLoadFormat{
				ID:       f.FormatID,
				Type:     "audio",
				Label:    fmt.Sprintf("Audio %.0fkbps (%s)", abr, strings.ToUpper(f.Ext)),
				Ext:      f.Ext,
				ACodec:   shortCodecName(f.ACodec),
				Filesize: size,
				ABR:      f.ABR,
			}
			if bestAudio == nil || scoreDropLoadAudioFormat(cand) > scoreDropLoadAudioFormat(*bestAudio) {
				c := cand
				bestAudio = &c
			}
		}
	}

	formats := make([]dropLoadFormat, 0, len(bestByHeight)+1)
	for _, f := range bestByHeight {
		formats = append(formats, f)
	}
	sort.Slice(formats, func(i, j int) bool {
		return formats[i].Height > formats[j].Height
	})
	if bestAudio != nil {
		formats = append(formats, *bestAudio)
	}

	dur := int(math.Round(rawMeta.Duration))
	return &dropLoadMediaInfo{
		ID:                rawMeta.ID,
		Title:             strings.TrimSpace(rawMeta.Title),
		Channel:           channel,
		ChannelURL:        strings.TrimSpace(rawMeta.ChannelURL),
		Duration:          dur,
		DurationFormatted: formatSecondsHMS(float64(dur)),
		IsLive:            rawMeta.IsLive,
		WebpageURL:        webpage,
		Thumbnail:         strings.TrimSpace(rawMeta.Thumbnail),
		Description:       truncateText(strings.TrimSpace(rawMeta.Description), 1200),
		Chapters:          chapters,
		Subtitles:         subs,
		Formats:           formats,
		EngineUsed:        "dropload_local_ytdlp",
	}, nil
}

func (s *Server) handleDropLoadDownloadMaterial(ctx context.Context, raw json.RawMessage) (any, error) {
	var args dropLoadDownloadArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	return s.downloadMaterial(ctx, args)
}

func (s *Server) downloadMaterial(ctx context.Context, args dropLoadDownloadArgs) (map[string]any, error) {
	safeURL, err := validateDropLoadURL(args.URL)
	if err != nil {
		return nil, err
	}

	rt := s.resolveDropLoadRuntime()
	mediaType := strings.ToLower(strings.TrimSpace(args.MediaType))
	if mediaType == "" {
		mediaType = "video+audio"
	}
	if mediaType != "video+audio" && mediaType != "video" && mediaType != "audio" {
		return nil, fmt.Errorf("invalid media_type %q (expected 'video+audio', 'video', or 'audio')", mediaType)
	}
	quality := strings.TrimSpace(args.Quality)
	if quality == "" {
		quality = "1080"
	}

	startMs, hasStart := parseFlexibleTimestampMs(args.ClipStart)
	endMs, hasEnd := parseFlexibleTimestampMs(args.ClipEnd)
	hasClip := hasStart || hasEnd
	if hasClip {
		if !hasStart || !hasEnd {
			return nil, fmt.Errorf("both clip_start and clip_end are required when clipping")
		}
		if endMs <= startMs || endMs-startMs < 500 {
			return nil, fmt.Errorf("clip_end must be at least 0.5s greater than clip_start")
		}
	}

	topic := sanitizeMaterialSlug(args.Topic)
	if topic == "" {
		topic = "general"
	}

	// Fetch lightweight metadata if slug is omitted OR when clipping (to decide hybrid section vs full+cut strategy).
	var meta *dropLoadMediaInfo
	if strings.TrimSpace(args.Slug) == "" || hasClip {
		if m, mErr := extractMediaInfoLocal(ctx, rt, safeURL); mErr == nil {
			meta = m
		}
	}

	slug := sanitizeMaterialSlug(args.Slug)
	if slug == "" && meta != nil && meta.Title != "" {
		slug = sanitizeMaterialSlug(meta.Title)
	}
	if slug == "" {
		slug = fmt.Sprintf("material-%d", time.Now().Unix())
	}

	ext := ".mp4"
	if mediaType == "audio" {
		ext = ".m4a"
	}
	fileStem := slug
	if hasClip {
		fileStem = fmt.Sprintf("%s-clip-%s-%s", slug, formatClipTag(startMs), formatClipTag(endMs))
	}

	destDir := strings.TrimSpace(args.OutputDir)
	if destDir == "" {
		destDir = filepath.Join(rt.MaterialsDir, topic)
	}
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed creating material output dir %s: %w", destDir, err)
	}
	destPath := filepath.Join(destDir, fileStem+ext)

	skipIfExists := true
	if args.SkipIfExists != nil {
		skipIfExists = *args.SkipIfExists
	}

	skipped := false
	engineUsed := "cache"
	if skipIfExists {
		if info, statErr := os.Stat(destPath); statErr == nil && !info.IsDir() && info.Size() > 0 {
			skipped = true
		}
	}

	if !skipped {
		dlCtx, cancel := withDefaultTimeout(ctx, 10*time.Minute)
		defer cancel()

		var downloadedViaAPI bool
		if args.UseAPI && isDropLoadAPIReachable(dlCtx, rt.APIBaseURL) {
			if apiErr := downloadViaDropLoadHTTPMCP(dlCtx, rt.APIBaseURL, safeURL, mediaType, quality, args.FormatID, args.ClipStart, args.ClipEnd, destPath); apiErr == nil {
				downloadedViaAPI = true
				engineUsed = "dropload_http_mcp"
			}
		}

		if !downloadedViaAPI {
			var totalDurSec float64
			if meta != nil && meta.Duration > 0 {
				totalDurSec = float64(meta.Duration)
			}
			if err := downloadViaDropLoadLocal(dlCtx, rt, safeURL, mediaType, quality, args.FormatID, hasClip, startMs, endMs, totalDurSec, destPath); err != nil {
				return nil, err
			}
			engineUsed = "dropload_local_ytdlp"
		}
	}

	statInfo, err := os.Stat(destPath)
	if err != nil || statInfo.Size() == 0 {
		return nil, fmt.Errorf("downloaded material missing or empty at %s", destPath)
	}

	result := map[string]any{
		"output_path":  destPath,
		"filename":     filepath.Base(destPath),
		"topic":        topic,
		"slug":         slug,
		"source_url":   safeURL,
		"media_type":   mediaType,
		"quality":      quality,
		"size_bytes":   statInfo.Size(),
		"skipped":      skipped,
		"engine_used":  engineUsed,
		"dropload_dir": rt.Dir,
	}
	if meta != nil {
		result["title"] = meta.Title
		result["channel"] = meta.Channel
		result["source_duration"] = meta.Duration
	}
	if hasClip {
		result["clip_start_sec"] = float64(startMs) / 1000.0
		result["clip_end_sec"] = float64(endMs) / 1000.0
	}
	if probed, pErr := s.projects.FFmpeg().Probe(destPath); pErr == nil {
		result["duration"] = probed.Duration
		result["width"] = probed.Width
		result["height"] = probed.Height
		result["fps"] = probed.FPS
		result["has_audio"] = probed.HasAudio
	}

	if strings.TrimSpace(args.ProjectID) != "" {
		asset, err := s.projects.ImportPath(strings.TrimSpace(args.ProjectID), destPath)
		if err != nil {
			return nil, fmt.Errorf("downloaded material at %s, but import to project %s failed: %w", destPath, args.ProjectID, err)
		}
		s.notifyLiveDesktop(strings.TrimSpace(args.ProjectID), "asset_imported")
		result["imported_asset"] = asset
	}

	if args.ExtractBRollBG && mediaType != "audio" {
		bgDur := args.BRollBGDur
		if bgDur <= 0 {
			bgDur = 4.5
		}
		bgPayload, _ := json.Marshal(map[string]any{
			"source_path": destPath,
			"start_sec":   args.BRollBGStart,
			"duration":    bgDur,
			"topic":       topic,
			"slug":        slug + "-bg",
			"pre_blur":    true,
		})
		bgRes, bgErr := s.handleBRollExtractBgVideo(ctx, bgPayload)
		if bgErr != nil {
			result["broll_bg_error"] = bgErr.Error()
		} else {
			result["broll_bg"] = bgRes
		}
	}

	return result, nil
}

func (s *Server) handleDropLoadBatchDownload(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		Topic        string `json:"topic"`
		ProjectID    string `json:"project_id"`
		Quality      string `json:"quality"`
		MediaType    string `json:"media_type"`
		SkipIfExists *bool  `json:"skip_if_exists"`
		UseAPI       bool   `json:"use_api"`
		Items        []struct {
			Beat           string  `json:"beat"`
			URL            string  `json:"url"`
			Query          string  `json:"query"`
			Slug           string  `json:"slug"`
			ClipStart      any     `json:"clip_start"`
			ClipEnd        any     `json:"clip_end"`
			Quality        string  `json:"quality"`
			MediaType      string  `json:"media_type"`
			MinDuration    int     `json:"min_duration"`
			MaxDuration    int     `json:"max_duration"`
			ExtractBRollBG bool    `json:"extract_broll_bg"`
			BRollBGStart   float64 `json:"broll_bg_start_sec"`
			BRollBGDur     float64 `json:"broll_bg_duration"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	if len(args.Items) == 0 {
		return nil, fmt.Errorf("parameter 'items' must contain at least one material entry")
	}

	rt := s.resolveDropLoadRuntime()
	topic := sanitizeMaterialSlug(args.Topic)
	if topic == "" {
		topic = "general"
	}
	defaultQuality := strings.TrimSpace(args.Quality)
	if defaultQuality == "" {
		defaultQuality = "1080"
	}
	defaultMediaType := strings.TrimSpace(args.MediaType)
	if defaultMediaType == "" {
		defaultMediaType = "video+audio"
	}

	results := make([]map[string]any, len(args.Items))
	var mu sync.Mutex
	usedURLs := map[string]bool{}
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup

	for idx := range args.Items {
		wg.Add(1)
		go func(idx int, itemIdx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			it := args.Items[itemIdx]
			entry := map[string]any{
				"index": idx,
				"beat":  it.Beat,
			}

			targetURL := strings.TrimSpace(it.URL)
			var matchedCandidate *youTubeCandidate
			if targetURL == "" {
				q := strings.TrimSpace(it.Query)
				if q == "" {
					entry["status"] = "error"
					entry["error"] = "either 'url' or 'query' is required"
					results[idx] = entry
					return
				}
				maxDur := it.MaxDuration
				if maxDur <= 0 {
					maxDur = 1200 // Default 20 min cap to avoid huge multi-hour streams in auto batch mode
				}
				cands, err := searchYouTubeWithDropLoad(ctx, rt, q, 5, it.MinDuration, maxDur, true)
				if err != nil || len(cands) == 0 {
					entry["status"] = "error"
					if err != nil {
						entry["error"] = err.Error()
					} else {
						entry["error"] = fmt.Sprintf("no YouTube video found for query %q", q)
					}
					results[idx] = entry
					return
				}
				mu.Lock()
				chosen := cands[0]
				for _, c := range cands {
					if !usedURLs[c.URL] {
						chosen = c
						break
					}
				}
				usedURLs[chosen.URL] = true
				mu.Unlock()
				targetURL = chosen.URL
				cCopy := chosen
				matchedCandidate = &cCopy
			}

			itemQuality := strings.TrimSpace(it.Quality)
			if itemQuality == "" {
				itemQuality = defaultQuality
			}
			itemMediaType := strings.TrimSpace(it.MediaType)
			if itemMediaType == "" {
				itemMediaType = defaultMediaType
			}
			itemSlug := strings.TrimSpace(it.Slug)
			if itemSlug == "" && strings.TrimSpace(it.Beat) != "" {
				itemSlug = fmt.Sprintf("%02d-%s", idx+1, sanitizeMaterialSlug(it.Beat))
			}

			dlRes, err := s.downloadMaterial(ctx, dropLoadDownloadArgs{
				URL:            targetURL,
				Topic:          topic,
				Slug:           itemSlug,
				MediaType:      itemMediaType,
				Quality:        itemQuality,
				ClipStart:      it.ClipStart,
				ClipEnd:        it.ClipEnd,
				ProjectID:      args.ProjectID,
				ExtractBRollBG: it.ExtractBRollBG,
				BRollBGStart:   it.BRollBGStart,
				BRollBGDur:     it.BRollBGDur,
				SkipIfExists:   args.SkipIfExists,
				UseAPI:         args.UseAPI,
			})
			if err != nil {
				entry["status"] = "error"
				entry["url"] = targetURL
				entry["error"] = err.Error()
				results[idx] = entry
				return
			}

			entry["status"] = "ok"
			if matchedCandidate != nil {
				entry["matched_candidate"] = matchedCandidate
			}
			for k, v := range dlRes {
				entry[k] = v
			}
			results[idx] = entry
		}(idx, idx)
	}
	wg.Wait()

	importedAssetIDs := []string{}
	succeeded := 0
	failed := 0
	for _, entry := range results {
		if entry["status"] == "ok" {
			succeeded++
			if importedAsset, ok := entry["imported_asset"]; ok {
				if rawBytes, mErr := json.Marshal(importedAsset); mErr == nil {
					var idWrap struct {
						ID string `json:"id"`
					}
					if json.Unmarshal(rawBytes, &idWrap) == nil && idWrap.ID != "" {
						importedAssetIDs = append(importedAssetIDs, idWrap.ID)
					}
				}
			}
		} else {
			failed++
		}
	}

	return map[string]any{
		"topic":              topic,
		"project_id":         args.ProjectID,
		"total":              len(args.Items),
		"succeeded":          succeeded,
		"failed":             failed,
		"imported_asset_ids": importedAssetIDs,
		"items":              results,
	}, nil
}

func (s *Server) handleDropLoadListMaterials(_ context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		Topic       string `json:"topic"`
		IncludeRoot *bool  `json:"include_root"`
		Probe       bool   `json:"probe"`
	}
	_ = json.Unmarshal(raw, &args)

	rt := s.resolveDropLoadRuntime()
	topic := sanitizeMaterialSlug(args.Topic)
	includeRoot := topic == ""
	if args.IncludeRoot != nil {
		includeRoot = *args.IncludeRoot
	}

	type materialFile struct {
		Path       string  `json:"path"`
		Filename   string  `json:"filename"`
		Topic      string  `json:"topic"`
		SizeBytes  int64   `json:"size_bytes"`
		ModifiedAt string  `json:"modified_at"`
		modTime    time.Time
		Duration   float64 `json:"duration,omitempty"`
		Width      int     `json:"width,omitempty"`
		Height     int     `json:"height,omitempty"`
	}

	files := []materialFile{}
	seen := map[string]bool{}

	scanDir := func(rootDir, topicLabel string, recursive bool) {
		if !dirExistsOnDisk(rootDir) {
			return
		}
		_ = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != rootDir && !recursive {
					return filepath.SkipDir
				}
				return nil
			}
			if !isMediaFileExt(filepath.Ext(d.Name())) {
				return nil
			}
			clean := filepath.Clean(path)
			if seen[clean] {
				return nil
			}
			seen[clean] = true
			info, statErr := d.Info()
			if statErr != nil || info.Size() == 0 {
				return nil
			}
			itemTopic := topicLabel
			if itemTopic == "" {
				if rel, relErr := filepath.Rel(rt.MaterialsDir, filepath.Dir(clean)); relErr == nil && !strings.HasPrefix(rel, "..") && rel != "." {
					itemTopic = filepath.ToSlash(rel)
				} else {
					itemTopic = "root"
				}
			}
			mf := materialFile{
				Path:       clean,
				Filename:   d.Name(),
				Topic:      itemTopic,
				SizeBytes:  info.Size(),
				ModifiedAt: info.ModTime().Format(time.RFC3339),
				modTime:    info.ModTime(),
			}
			if args.Probe {
				if probed, pErr := s.projects.FFmpeg().Probe(clean); pErr == nil {
					mf.Duration = probed.Duration
					mf.Width = probed.Width
					mf.Height = probed.Height
				}
			}
			files = append(files, mf)
			return nil
		})
	}

	if topic != "" {
		scanDir(filepath.Join(rt.MaterialsDir, topic), topic, true)
	} else {
		scanDir(rt.MaterialsDir, "", true)
		if includeRoot {
			scanDir(rt.StorageDir, "root", false)
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	return map[string]any{
		"storage_dir":   rt.StorageDir,
		"materials_dir": rt.MaterialsDir,
		"topic_filter":  topic,
		"count":         len(files),
		"materials":     files,
	}, nil
}

func downloadViaDropLoadLocal(
	ctx context.Context,
	rt dropLoadRuntime,
	sourceURL, mediaType, quality, formatID string,
	hasClip bool,
	startMs, endMs int64,
	totalDurSec float64,
	destPath string,
) error {
	formatSelector := buildDropLoadFormatSelector(mediaType, quality, formatID)
	tempDir := filepath.Dir(destPath)
	tempStamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	tempBase := filepath.Join(tempDir, ".dropload-tmp-"+tempStamp)
	defer cleanupTempPrefix(tempBase)

	useSection := false
	if hasClip {
		clipDurSec := float64(endMs-startMs) / 1000.0
		// Mirror DropLoad's hybrid clip strategy: use --download-sections on long videos
		// (>= 5 min where clip is <= 25%) so we don't download a 2-hour stream for a 20s clip.
		if totalDurSec >= 300 && clipDurSec > 0 && (clipDurSec/totalDurSec) <= 0.25 {
			useSection = true
		}
	}

	runYTDLP := func(sectionMode bool, useCookies bool) (string, string, error) {
		cleanupTempPrefix(tempBase)
		outTpl := tempBase + ".%(ext)s"
		args := []string{}
		if rt.FFmpegPath != "" {
			args = append(args, "--ffmpeg-location", rt.FFmpegPath)
		}
		if formatSelector != "" {
			args = append(args, "-f", formatSelector)
		}
		if mediaType == "audio" {
			args = append(args, "-x", "--audio-format", "m4a")
		} else {
			args = append(args, "--merge-output-format", "mp4")
		}
		args = append(args, buildDropLoadCommonExtractorArgs(rt, useCookies)...)
		args = append(args,
			"--no-playlist",
			"--no-colors",
			"--no-warnings",
			"--retries", "5",
			"--fragment-retries", "5",
			"--concurrent-fragments", "5",
			"--extractor-retries", "3",
			"--retry-sleep", "extractor:1",
			"--socket-timeout", "30",
			"-o", outTpl,
			"--print", "after_move:filepath",
		)
		if sectionMode {
			args = append(args,
				"--download-sections", fmt.Sprintf("*%.3f-%.3f", float64(startMs)/1000.0, float64(endMs)/1000.0),
			)
		}
		args = append(args, sourceURL)
		stdout, stderr, err := runDropLoadCommand(ctx, rt.YTDLPPath, args...)
		if err != nil {
			return "", stderr, err
		}
		actual := locateDownloadedFile(stdout, tempBase)
		if actual == "" {
			return "", stderr, fmt.Errorf("yt-dlp completed but output file was not found")
		}
		return actual, stderr, nil
	}

	useCookies := rt.CookiesPath != "" || rt.CookiesFromBrowser != ""
	actualPath, stderr, err := runYTDLP(useSection, useCookies)
	if err != nil && useCookies {
		// Retry without cookies in case cookies expired.
		if retryPath, retryErrText, retryErr := runYTDLP(useSection, false); retryErr == nil {
			actualPath = retryPath
			stderr = retryErrText
			err = nil
		}
	}
	if err != nil && useSection {
		// Fallback to DropLoad's concurrent full download + local FFmpeg cut if section streaming failed.
		useSection = false
		actualPath, stderr, err = runYTDLP(false, useCookies)
		if err != nil && useCookies {
			if retryPath, retryErrText, retryErr := runYTDLP(false, false); retryErr == nil {
				actualPath = retryPath
				stderr = retryErrText
				err = nil
			}
		}
	}
	if err != nil {
		return fmt.Errorf("dropload download failed: %w (%s)", err, summarizeStderr(stderr))
	}

	_ = os.Remove(destPath)
	if hasClip {
		startSec := float64(startMs) / 1000.0
		durSec := float64(endMs-startMs) / 1000.0
		if err := ffmpegCutOrNormalize(ctx, rt.FFmpegPath, rt.FFprobePath, actualPath, destPath, !useSection, startSec, durSec, mediaType); err != nil {
			return fmt.Errorf("dropload ffmpeg clip processing failed: %w", err)
		}
		return nil
	}

	// If file already has the desired extension, move it directly; otherwise remux cleanly.
	if strings.EqualFold(filepath.Ext(actualPath), filepath.Ext(destPath)) {
		if err := os.Rename(actualPath, destPath); err == nil {
			return nil
		}
		if err := copyFilePath(actualPath, destPath); err != nil {
			return err
		}
		return nil
	}

	if err := ffmpegCutOrNormalize(ctx, rt.FFmpegPath, rt.FFprobePath, actualPath, destPath, false, 0, 0, mediaType); err != nil {
		// Fallback: move file with its native extension if container remux failed.
		if copyErr := copyFilePath(actualPath, destPath); copyErr != nil {
			return fmt.Errorf("remux failed (%w) and copy failed (%v)", err, copyErr)
		}
	}
	return nil
}

func ffmpegCutOrNormalize(ctx context.Context, ffmpegPath, ffprobePath, srcPath, dstPath string, doCut bool, startSec, durSec float64, mediaType string) error {
	vcodec := probeStreamCodec(ctx, ffprobePath, srcPath, "v:0")
	acodec := probeStreamCodec(ctx, ffprobePath, srcPath, "a:0")
	ext := strings.ToLower(filepath.Ext(dstPath))

	args := []string{"-hide_banner", "-y", "-v", "error"}
	if doCut {
		args = append(args, "-ss", fmt.Sprintf("%.3f", startSec))
	}
	args = append(args, "-i", srcPath)
	if doCut {
		args = append(args, "-t", fmt.Sprintf("%.3f", durSec))
	}

	if mediaType == "audio" || ext == ".m4a" || ext == ".mp3" {
		args = append(args, "-vn")
		if ext == ".m4a" && acodec == "aac" {
			args = append(args, "-c:a", "copy")
		} else {
			args = append(args, "-c:a", "aac", "-b:a", "192k")
		}
	} else {
		// Ensure MP4 compatibility for smooth AutoCut Workstation WebView2 preview & FFmpeg rendering.
		if !doCut && (vcodec == "h264" || vcodec == "avc1" || strings.HasPrefix(vcodec, "avc")) {
			args = append(args, "-c:v", "copy")
		} else {
			// Re-encoding on cuts ensures exact frame-accurate boundaries without keyframe drift.
			args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-pix_fmt", "yuv420p")
		}
		if mediaType == "video" {
			args = append(args, "-an")
		} else if acodec == "aac" && !doCut {
			args = append(args, "-c:a", "copy")
		} else if acodec != "" {
			args = append(args, "-c:a", "aac", "-b:a", "192k")
		}
		args = append(args, "-movflags", "+faststart")
	}
	args = append(args, dstPath)

	_, stderr, err := runDropLoadCommand(ctx, ffmpegPath, args...)
	if err != nil {
		return fmt.Errorf("%w (%s)", err, summarizeStderr(stderr))
	}
	return nil
}

func probeStreamCodec(ctx context.Context, ffprobePath, mediaPath, streamSelector string) string {
	pCtx, cancel := withDefaultTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(pCtx, ffprobePath,
		"-v", "error",
		"-select_streams", streamSelector,
		"-show_entries", "stream=codec_name",
		"-of", "default=noprint_wrappers=1:nokey=1",
		mediaPath,
	).Output()
	if err != nil {
		return ""
	}
	codec := strings.ToLower(strings.TrimSpace(string(out)))
	if codec == "n/a" {
		return ""
	}
	return codec
}

func buildDropLoadCommonExtractorArgs(rt dropLoadRuntime, useCookies bool) []string {
	ytClient := rt.YouTubePlayerClients
	if ytClient == "" {
		ytClient = "default,tv,mweb"
	}
	args := []string{
		"--extractor-args", "youtube:player_client=" + ytClient,
		"--extractor-args", "tiktok:api_hostname=api16-normal-c-useast1a.tiktokv.com;app_info=7355_1",
	}
	if rt.YouTubePOTBaseURL != "" {
		args = append(args, "--extractor-args", "youtubepot-bgutilhttp:base_url="+rt.YouTubePOTBaseURL)
	}
	if rt.JSRuntimes != "" {
		for _, item := range strings.Split(rt.JSRuntimes, ",") {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				args = append(args, "--js-runtimes", trimmed)
			}
		}
	}
	if useCookies {
		if rt.CookiesFromBrowser != "" {
			args = append(args, "--cookies-from-browser", rt.CookiesFromBrowser)
		} else if rt.CookiesPath != "" {
			args = append(args, "--cookies", rt.CookiesPath)
		}
	}
	return args
}

func buildDropLoadFormatSelector(mediaType, quality, explicitFormatID string) string {
	if trimmed := strings.TrimSpace(explicitFormatID); trimmed != "" {
		return trimmed
	}
	q := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(quality), "p"))
	height, err := strconv.Atoi(q)
	hasCap := err == nil && height > 0

	switch mediaType {
	case "audio":
		return "bestaudio[ext=m4a]/bestaudio/best"
	case "video":
		if hasCap {
			return fmt.Sprintf("bestvideo[height<=%d][vcodec^=avc1]/bestvideo[height<=%d][ext=mp4]/bestvideo[height<=%d]/best[height<=%d]/best", height, height, height, height)
		}
		return "bestvideo[vcodec^=avc1]/bestvideo[ext=mp4]/bestvideo/best"
	default: // video+audio
		if hasCap {
			return fmt.Sprintf("bestvideo[height<=%d][vcodec^=avc1]+bestaudio[acodec^=mp4a]/bestvideo[height<=%d][ext=mp4]+bestaudio[ext=m4a]/bestvideo[height<=%d]+bestaudio/best[height<=%d]/best", height, height, height, height)
		}
		return "bestvideo[vcodec^=avc1]+bestaudio[acodec^=mp4a]/bestvideo+bestaudio/best"
	}
}

func isDropLoadAPIReachable(ctx context.Context, apiBaseURL string) bool {
	if strings.TrimSpace(apiBaseURL) == "" {
		return false
	}
	hCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(hCtx, http.MethodGet, strings.TrimRight(apiBaseURL, "/")+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 800 * time.Millisecond}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func callDropLoadHTTPMCP(ctx context.Context, apiBaseURL, toolName string, arguments map[string]any) (map[string]any, error) {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      toolName,
			"arguments": arguments,
		},
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(apiBaseURL, "/")+"/mcp", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 75 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var rpcResp struct {
		Result struct {
			IsError           bool `json:"isError"`
			StructuredContent any  `json:"structuredContent"`
			Content           []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, err
	}
	if rpcResp.Error != nil {
		return nil, errors.New(rpcResp.Error.Message)
	}
	if rpcResp.Result.IsError {
		msg := "DropLoad HTTP MCP tool failed"
		if len(rpcResp.Result.Content) > 0 {
			msg = rpcResp.Result.Content[0].Text
		}
		return nil, errors.New(msg)
	}
	if m, ok := rpcResp.Result.StructuredContent.(map[string]any); ok {
		return m, nil
	}
	if len(rpcResp.Result.Content) > 0 {
		var parsed map[string]any
		if json.Unmarshal([]byte(rpcResp.Result.Content[0].Text), &parsed) == nil {
			return parsed, nil
		}
	}
	return map[string]any{}, nil
}

func downloadViaDropLoadHTTPMCP(ctx context.Context, apiBaseURL, sourceURL, mediaType, quality, formatID string, clipStart, clipEnd any, destPath string) error {
	params := map[string]any{
		"url":                 sourceURL,
		"type":                mediaType,
		"quality":             quality,
		"wait_for_completion": true,
	}
	if strings.TrimSpace(formatID) != "" {
		params["format_id"] = formatID
	}
	if clipStart != nil && fmt.Sprint(clipStart) != "" {
		params["clip_start"] = clipStart
	}
	if clipEnd != nil && fmt.Sprint(clipEnd) != "" {
		params["clip_end"] = clipEnd
	}

	res, err := callDropLoadHTTPMCP(ctx, apiBaseURL, "start_download", params)
	if err != nil {
		return err
	}
	jobID, _ := res["jobId"].(string)
	status, _ := res["status"].(string)
	downloadURL, _ := res["downloadUrl"].(string)

	// Poll up to 4 additional rounds if the download takes longer than DropLoad's 55s wait.
	for attempts := 0; attempts < 4 && status != "completed" && status != "failed" && jobID != ""; attempts++ {
		stRes, stErr := callDropLoadHTTPMCP(ctx, apiBaseURL, "get_download_status", map[string]any{
			"job_id":       jobID,
			"wait_seconds": 30,
		})
		if stErr != nil {
			return stErr
		}
		status, _ = stRes["status"].(string)
		if u, ok := stRes["downloadUrl"].(string); ok && u != "" {
			downloadURL = u
		}
	}

	if status != "completed" || downloadURL == "" {
		return fmt.Errorf("DropLoad HTTP job %s did not complete (status=%s)", jobID, status)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("DropLoad download URL returned HTTP %d", resp.StatusCode)
	}
	return writeToFile(destPath, resp.Body)
}

func runDropLoadCommand(ctx context.Context, bin string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func validateDropLoadURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("parameter 'url' is required")
	}
	parsed, err := url.ParseRequestURI(trimmed)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("invalid URL: %q", trimmed)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("only http and https URLs are supported")
	}
	return parsed.String(), nil
}

func parseFlexibleTimestampMs(raw any) (int64, bool) {
	if raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		if v < 0 {
			return 0, false
		}
		return int64(math.Round(v * 1000)), true
	case int:
		if v < 0 {
			return 0, false
		}
		return int64(v) * 1000, true
	case int64:
		if v < 0 {
			return 0, false
		}
		return v * 1000, true
	case string:
		str := strings.TrimSpace(v)
		if str == "" {
			return 0, false
		}
		if strings.Contains(str, ":") {
			parts := strings.Split(str, ":")
			var totalSec float64
			for _, p := range parts {
				val, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
				if err != nil || val < 0 {
					return 0, false
				}
				totalSec = totalSec*60 + val
			}
			return int64(math.Round(totalSec * 1000)), true
		}
		val, err := strconv.ParseFloat(str, 64)
		if err != nil || val < 0 {
			return 0, false
		}
		return int64(math.Round(val * 1000)), true
	default:
		return 0, false
	}
}

var nonSlugPattern = regexp.MustCompile(`[^a-z0-9\-_]+`)
var multiDashPattern = regexp.MustCompile(`-+`)

func sanitizeMaterialSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, " ", "-")
	s = nonSlugPattern.ReplaceAllString(s, "-")
	s = multiDashPattern.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-_")
	if len(s) > 64 {
		s = strings.Trim(s[:64], "-_")
	}
	return s
}

func formatSecondsHMS(sec float64) string {
	total := int(math.Round(sec))
	if total < 0 {
		total = 0
	}
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func formatClipTag(ms int64) string {
	total := int(ms / 1000)
	if total < 0 {
		total = 0
	}
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%02dh%02dm%02ds", h, m, s)
	}
	return fmt.Sprintf("%02dm%02ds", m, s)
}

func extractFirstJSONLine(stdout string) string {
	for _, line := range strings.Split(stdout, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
			return trimmed
		}
	}
	trimmed := strings.TrimSpace(stdout)
	if strings.HasPrefix(trimmed, "{") {
		return trimmed
	}
	return ""
}

func locateDownloadedFile(stdout, tempBase string) string {
	lines := strings.Split(stdout, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		cand := strings.TrimSpace(lines[i])
		if cand != "" && fileExistsOnDisk(cand) {
			return cand
		}
	}
	matches, _ := filepath.Glob(tempBase + ".*")
	for _, m := range matches {
		if !strings.HasSuffix(m, ".part") && !strings.HasSuffix(m, ".ytdl") && fileExistsOnDisk(m) {
			return m
		}
	}
	return ""
}

func cleanupTempPrefix(tempBase string) {
	matches, _ := filepath.Glob(tempBase + "*")
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

func copyFilePath(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func withDefaultTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}

func truncateText(s string, maxChars int) string {
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	return string(runes[:maxChars]) + "..."
}

func summarizeStderr(stderr string) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	meaningful := make([]string, 0, 3)
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" {
			meaningful = append(meaningful, t)
		}
	}
	if len(meaningful) > 3 {
		meaningful = meaningful[len(meaningful)-3:]
	}
	return strings.Join(meaningful, " | ")
}

func shortCodecName(codec string) string {
	c := strings.ToLower(strings.TrimSpace(codec))
	switch {
	case strings.HasPrefix(c, "avc1"), strings.HasPrefix(c, "h264"):
		return "h264"
	case strings.HasPrefix(c, "av01"):
		return "av1"
	case strings.HasPrefix(c, "vp09"), strings.HasPrefix(c, "vp9"):
		return "vp9"
	case strings.HasPrefix(c, "mp4a"):
		return "aac"
	case strings.HasPrefix(c, "opus"):
		return "opus"
	case c == "none":
		return ""
	default:
		return c
	}
}

func scoreDropLoadVideoFormat(f dropLoadFormat) int {
	score := 0
	if f.VCodec == "h264" {
		score += 400
	} else if f.VCodec == "vp9" {
		score += 200
	} else if f.VCodec == "av1" {
		score += 100
	}
	if f.Ext == "mp4" {
		score += 150
	}
	if f.Type == "video+audio" {
		score += 50
	}
	if f.FPS >= 50 {
		score += 80
	}
	return score
}

func scoreDropLoadAudioFormat(f dropLoadFormat) int {
	score := 0
	if f.Ext == "m4a" || f.ACodec == "aac" {
		score += 200
	} else if f.ACodec == "opus" {
		score += 150
	}
	if f.ABR != nil {
		score += int(*f.ABR)
	}
	return score
}

func isMediaFileExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp4", ".mkv", ".mov", ".webm", ".m4v", ".avi", ".m4a", ".mp3", ".wav", ".aac", ".ogg", ".opus", ".flac":
		return true
	default:
		return false
	}
}
