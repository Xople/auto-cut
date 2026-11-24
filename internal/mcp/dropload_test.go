package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"autocut/internal/config"
	"autocut/internal/db"
	"autocut/internal/pipeline"
)

func newTestServerWithDropLoad(t *testing.T, droploadDir, droploadURL string) *Server {
	t.Helper()
	tmp := t.TempDir()
	cfg := config.LoadConfig()
	cfg.StorageDir = tmp

	database, err := db.InitDB(tmp)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	orch := pipeline.NewOrchestrator(cfg, database)
	srv := NewServer(cfg, database, orch)
	srv.droploadDir = droploadDir
	srv.droploadURL = droploadURL
	return srv
}

func TestDropLoadToolsRegistered(t *testing.T) {
	srv := newTestServerWithDropLoad(t, t.TempDir(), "")
	expected := []string{
		"dropload_status",
		"dropload_search_youtube",
		"dropload_extract_info",
		"dropload_download_material",
		"dropload_batch_download_materials",
		"dropload_list_materials",
	}
	toolNames := map[string]bool{}
	for _, def := range srv.ListTools() {
		toolNames[def.Name] = true
	}
	for _, name := range expected {
		if !toolNames[name] {
			t.Errorf("expected DropLoad MCP tool %q to be registered", name)
		}
	}
}

func TestParseFlexibleTimestampMs(t *testing.T) {
	cases := []struct {
		in   any
		want int64
		ok   bool
	}{
		{"00:45", 45000, true},
		{"01:15", 75000, true},
		{"00:01:15.500", 75500, true},
		{"1:02:03", 3723000, true},
		{"90.25", 90250, true},
		{float64(30), 30000, true},
		{int(12), 12000, true},
		{"", 0, false},
		{"-5", 0, false},
		{"abc", 0, false},
		{nil, 0, false},
	}
	for _, tc := range cases {
		got, ok := parseFlexibleTimestampMs(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseFlexibleTimestampMs(%v) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSanitizeMaterialSlug(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Among Us: Official Trailer! (2026)", "among-us-official-trailer-2026"},
		{"../../etc/passwd", "etc-passwd"},
		{"  Dota2_ESIC  Match #1  ", "dota2_esic-match-1"},
		{"", ""},
	}
	for _, tc := range cases {
		got := sanitizeMaterialSlug(tc.in)
		if got != tc.want {
			t.Errorf("sanitizeMaterialSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildDropLoadFormatSelector(t *testing.T) {
	if got := buildDropLoadFormatSelector("video+audio", "1080", "137+140"); got != "137+140" {
		t.Errorf("explicit format override failed: got %q", got)
	}
	if got := buildDropLoadFormatSelector("audio", "1080", ""); !strings.Contains(got, "bestaudio") {
		t.Errorf("expected bestaudio selector, got %q", got)
	}
	if got := buildDropLoadFormatSelector("video", "720p", ""); !strings.Contains(got, "height<=720") || strings.Contains(got, "+bestaudio") {
		t.Errorf("expected video-only 720p selector, got %q", got)
	}
	if got := buildDropLoadFormatSelector("video+audio", "1080", ""); !strings.Contains(got, "height<=1080") || !strings.Contains(got, "+bestaudio") {
		t.Errorf("expected video+audio 1080p selector, got %q", got)
	}
}

func TestResolveDropLoadRuntimeAndListMaterials(t *testing.T) {
	dlDir := t.TempDir()
	mustMkdir := func(rel string) string {
		p := filepath.Join(dlDir, rel)
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatalf("MkdirAll %s: %v", p, err)
		}
		return p
	}
	mustWrite := func(rel, content string) string {
		p := filepath.Join(dlDir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("MkdirAll %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile %s: %v", p, err)
		}
		return p
	}

	mustMkdir("bin")
	cookiesFile := mustWrite("bin/cookies.txt", "# Netscape HTTP Cookie File\n")
	mustWrite("server/.env", "YOUTUBE_PLAYER_CLIENTS=default,tv\nJS_RUNTIMES=node\nSTORAGE_DIR=./storage/downloads\nAPI_PORT=3999\n")
	mustWrite("storage/downloads/materials/among-us/trailer.mp4", "fake-mp4-data")
	mustWrite("storage/downloads/materials/dota2/highlight.mkv", "fake-mkv-data")
	mustWrite("storage/downloads/root-sample.mp4", "fake-root-mp4")

	srv := newTestServerWithDropLoad(t, dlDir, "")
	rt := srv.resolveDropLoadRuntime()
	if rt.CookiesPath != cookiesFile {
		t.Errorf("expected cookies path %q, got %q", cookiesFile, rt.CookiesPath)
	}
	if rt.YouTubePlayerClients != "default,tv" {
		t.Errorf("expected player clients from server/.env, got %q", rt.YouTubePlayerClients)
	}
	if rt.APIBaseURL != "http://localhost:3999" {
		t.Errorf("expected API_BASE_URL from API_PORT, got %q", rt.APIBaseURL)
	}

	// List filtered by topic
	res, err := srv.CallTool(context.Background(), "dropload_list_materials", json.RawMessage(`{"topic":"among-us"}`))
	if err != nil {
		t.Fatalf("dropload_list_materials(topic) failed: %v", err)
	}
	resMap := res.(map[string]any)
	if count := resMap["count"].(int); count != 1 {
		t.Fatalf("expected 1 material for topic among-us, got %d (%+v)", count, resMap)
	}

	// List all (materials + root)
	allRes, err := srv.CallTool(context.Background(), "dropload_list_materials", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("dropload_list_materials(all) failed: %v", err)
	}
	allMap := allRes.(map[string]any)
	if count := allMap["count"].(int); count != 3 {
		t.Fatalf("expected 3 materials total, got %d (%+v)", count, allMap)
	}
}

func TestDropLoadDownloadValidationAndCacheHit(t *testing.T) {
	dlDir := t.TempDir()
	srv := newTestServerWithDropLoad(t, dlDir, "")

	// Invalid URL
	if _, err := srv.CallTool(context.Background(), "dropload_download_material", json.RawMessage(`{"url":"ftp://example.com/a.mp4"}`)); err == nil {
		t.Fatalf("expected error for non-http URL")
	}
	// Missing clip_end
	if _, err := srv.CallTool(context.Background(), "dropload_download_material", json.RawMessage(`{"url":"https://www.youtube.com/watch?v=abc","clip_start":"00:10"}`)); err == nil {
		t.Fatalf("expected error when clip_end is omitted")
	}
	// Inverted clip range
	if _, err := srv.CallTool(context.Background(), "dropload_download_material", json.RawMessage(`{"url":"https://www.youtube.com/watch?v=abc","clip_start":"00:30","clip_end":"00:10"}`)); err == nil {
		t.Fatalf("expected error when clip_end <= clip_start")
	}

	// Cache hit when target file already exists and skip_if_exists defaults to true
	cachedPath := filepath.Join(dlDir, "storage", "downloads", "materials", "among-us", "existing-clip.mp4")
	if err := os.MkdirAll(filepath.Dir(cachedPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(cachedPath, []byte("cached-video-bytes"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	res, err := srv.CallTool(context.Background(), "dropload_download_material", json.RawMessage(`{
		"url": "https://www.youtube.com/watch?v=XBW8vsscafM",
		"topic": "among-us",
		"slug": "existing-clip"
	}`))
	if err != nil {
		t.Fatalf("expected cache hit success, got error: %v", err)
	}
	m := res.(map[string]any)
	if skipped, _ := m["skipped"].(bool); !skipped {
		t.Errorf("expected skipped=true on cache hit, got %+v", m)
	}
	if gotPath, _ := m["output_path"].(string); gotPath != cachedPath {
		t.Errorf("expected output_path=%q, got %q", cachedPath, gotPath)
	}
}

func TestDropLoadHTTPMCPBridgeMock(t *testing.T) {
	var mockServer *httptest.Server
	mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","service":"dropload-go-api"}`))
		case "/mcp":
			var req struct {
				Params struct {
					Name string `json:"name"`
				} `json:"params"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			switch req.Params.Name {
			case "extract_media_info":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"result": map[string]any{
						"isError": false,
						"structuredContent": map[string]any{
							"id":    "vid123",
							"title": "Mock Trailer",
						},
					},
				})
			case "start_download":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"result": map[string]any{
						"isError": false,
						"structuredContent": map[string]any{
							"jobId":       "job_1",
							"status":      "completed",
							"downloadUrl": mockServer.URL + "/api/downloads/job_1/file",
						},
					},
				})
			}
		case "/api/downloads/job_1/file":
			_, _ = w.Write([]byte("mock-mp4-stream-payload"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mockServer.Close()

	dlDir := t.TempDir()
	srv := newTestServerWithDropLoad(t, dlDir, mockServer.URL)

	// 1. Check status detects live DropLoad API
	statusRes, err := srv.CallTool(context.Background(), "dropload_status", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("dropload_status failed: %v", err)
	}
	if reachable, _ := statusRes.(map[string]any)["api_reachable"].(bool); !reachable {
		t.Fatalf("expected api_reachable=true, got %+v", statusRes)
	}

	// 2. Extract info via live DropLoad HTTP MCP
	infoRes, err := srv.CallTool(context.Background(), "dropload_extract_info", json.RawMessage(`{
		"url": "https://www.youtube.com/watch?v=vid123",
		"use_api": true
	}`))
	if err != nil {
		t.Fatalf("dropload_extract_info(use_api) failed: %v", err)
	}
	if engine, _ := infoRes.(map[string]any)["engine_used"].(string); engine != "dropload_http_mcp" {
		t.Fatalf("expected engine_used=dropload_http_mcp, got %+v", infoRes)
	}

	// 3. Download via live DropLoad HTTP MCP into local material storage
	dlRes, err := srv.CallTool(context.Background(), "dropload_download_material", json.RawMessage(`{
		"url": "https://www.youtube.com/watch?v=vid123",
		"topic": "mock-topic",
		"slug": "mock-clip",
		"use_api": true
	}`))
	if err != nil {
		t.Fatalf("dropload_download_material(use_api) failed: %v", err)
	}
	dlMap := dlRes.(map[string]any)
	if engine, _ := dlMap["engine_used"].(string); engine != "dropload_http_mcp" {
		t.Fatalf("expected engine_used=dropload_http_mcp, got %+v", dlMap)
	}
	outPath := dlMap["output_path"].(string)
	data, err := os.ReadFile(outPath)
	if err != nil || string(data) != "mock-mp4-stream-payload" {
		t.Fatalf("unexpected downloaded content at %s: %q (err=%v)", outPath, string(data), err)
	}
}
