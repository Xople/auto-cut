package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"autocut/internal/config"
	"autocut/internal/db"
	"autocut/internal/models"
	"autocut/internal/pipeline"
)

func newTimelineTestServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Port:       "8080",
		StorageDir: tmpDir,
		FFmpegPath: "ffmpeg-not-installed",
	}
	database, err := db.InitDB(tmpDir)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	orch := pipeline.NewOrchestrator(cfg, database)
	return NewServer(cfg, database, orch), database
}

func seedProject(t *testing.T, database *db.DB) *models.Project {
	t.Helper()
	p := &models.Project{
		ID:       "proj_timeline",
		Name:     "Timeline Test",
		Width:    1920,
		Height:   1080,
		FPS:      30,
		Timeline: models.NewTimeline(1920, 1080, 30),
	}
	if err := database.CreateProject(p); err != nil {
		t.Fatalf("failed to seed project: %v", err)
	}
	return p
}

const legacyTimelineV1 = `{
	"version": 1,
	"width": 1920,
	"height": 1080,
	"fps": 30,
	"caption_style": {"font_family": "Arial", "font_size": 72, "color": "#FFFFFF"},
	"tracks": [
		{
			"id": "video-main",
			"kind": "video",
			"name": "Video",
			"muted": false,
			"locked": false,
			"clips": [
				{"id": "clip_1", "start": 0, "duration": 2, "in": 0, "out": 2, "volume": 1, "fade_in": 0, "fade_out": 0}
			]
		}
	]
}`

func TestSaveTimelineUpgradesV1Document(t *testing.T) {
	server, database := newTimelineTestServer(t)
	seedProject(t, database)

	req := httptest.NewRequest(http.MethodPut, "/api/projects/proj_timeline/timeline", strings.NewReader(legacyTimelineV1))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var saved models.Project
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if saved.Timeline.Version != models.TimelineVersion {
		t.Errorf("response timeline version = %d, want %d", saved.Timeline.Version, models.TimelineVersion)
	}
	if got := saved.Timeline.Tracks[0].Height; got != models.DefaultTrackHeight {
		t.Errorf("response track height = %d, want %d", got, models.DefaultTrackHeight)
	}
	if got := saved.Timeline.Tracks[0].Clips[0].Speed; got != 1 {
		t.Errorf("response clip speed = %f, want 1", got)
	}

	stored, err := database.GetProject("proj_timeline")
	if err != nil {
		t.Fatalf("reload project: %v", err)
	}
	if stored.Timeline.Version != models.TimelineVersion {
		t.Errorf("persisted version = %d, want %d", stored.Timeline.Version, models.TimelineVersion)
	}
	if got := stored.Timeline.Tracks[0].Height; got != models.DefaultTrackHeight {
		t.Errorf("persisted track height = %d, want %d", got, models.DefaultTrackHeight)
	}
}

func TestSaveTimelineRejectsDuplicateClipIDs(t *testing.T) {
	server, database := newTimelineTestServer(t)
	seedProject(t, database)

	body := `{
		"version": 2,
		"width": 1920,
		"height": 1080,
		"fps": 30,
		"tracks": [
			{
				"id": "video-main",
				"kind": "video",
				"name": "Video",
				"height": 52,
				"clips": [
					{"id": "dup", "start": 0, "duration": 1, "in": 0, "out": 1, "speed": 1, "scale": 1, "opacity": 1, "volume": 1},
					{"id": "dup", "start": 1, "duration": 1, "in": 0, "out": 1, "speed": 1, "scale": 1, "opacity": 1, "volume": 1}
				]
			}
		]
	}`
	req := httptest.NewRequest(http.MethodPut, "/api/projects/proj_timeline/timeline", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for duplicate clip ids, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "valid") {
		t.Errorf("expected validation message, got %q", rec.Body.String())
	}
}
