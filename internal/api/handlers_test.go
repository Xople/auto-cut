package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"autocut/internal/config"
	"autocut/internal/db"
	"autocut/internal/models"
	"autocut/internal/pipeline"
)

func TestUploadAudioAndVideoJobs(t *testing.T) {
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
	defer database.Close()

	orch := pipeline.NewOrchestrator(cfg, database)
	server := NewServer(cfg, database, orch)

	// Test 1: Upload Audio File (.wav)
	{
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("file", "voiceover.wav")
		if err != nil {
			t.Fatalf("failed to create form file: %v", err)
		}
		part.Write([]byte("RIFFdummywavdata"))
		_ = writer.WriteField("title", "Voice Over Part 1")
		writer.Close()

		req := httptest.NewRequest("POST", "/api/jobs", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()

		server.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for audio upload, got %d: %s", rec.Code, rec.Body.String())
		}

		var job models.Job
		if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
			t.Fatalf("failed to parse response json: %v", err)
		}

		if job.MediaType != "audio" {
			t.Errorf("expected MediaType 'audio', got '%s'", job.MediaType)
		}
		if !job.HasVo || job.VoPath == "" {
			t.Errorf("audio upload should be stored as the VO")
		}
		if job.Status != models.StatusPending {
			t.Errorf("audio upload must not start the pipeline, got status %s", job.Status)
		}
		if _, err := os.Stat(filepath.Join(tmpDir, "temp", job.ID)); os.IsNotExist(err) {
			t.Errorf("expected temp dir to exist for job")
		}
	}

	// Test 2: Upload Video File (.mp4)
	{
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("video", "raw_gameplay.mp4")
		if err != nil {
			t.Fatalf("failed to create form file: %v", err)
		}
		part.Write([]byte("ftypisommp4dummydata"))
		_ = writer.WriteField("title", "Gameplay Take 1")
		writer.Close()

		req := httptest.NewRequest("POST", "/api/jobs", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()

		server.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 Created for video upload, got %d: %s", rec.Code, rec.Body.String())
		}

		var job models.Job
		if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
			t.Fatalf("failed to parse response json: %v", err)
		}

		if job.MediaType != "video" {
			t.Errorf("expected MediaType 'video', got '%s'", job.MediaType)
		}
		if job.HasVo {
			t.Errorf("video upload should not count as a VO")
		}
		if job.Status != models.StatusAwaitingVo {
			t.Errorf("expected awaiting_vo, got %s", job.Status)
		}

		cutReq := httptest.NewRequest("POST", "/api/jobs/"+job.ID+"/autocut", nil)
		cutRec := httptest.NewRecorder()
		server.ServeHTTP(cutRec, cutReq)
		if cutRec.Code != http.StatusConflict {
			t.Fatalf("expected 409 without a VO, got %d: %s", cutRec.Code, cutRec.Body.String())
		}

		voBody := &bytes.Buffer{}
		voWriter := multipart.NewWriter(voBody)
		voPart, err := voWriter.CreateFormFile("file", "narration.wav")
		if err != nil {
			t.Fatalf("failed to create vo form: %v", err)
		}
		voPart.Write([]byte("RIFFdummywavdata"))
		voWriter.Close()

		voReq := httptest.NewRequest("POST", "/api/jobs/"+job.ID+"/vo", voBody)
		voReq.Header.Set("Content-Type", voWriter.FormDataContentType())
		voRec := httptest.NewRecorder()
		server.ServeHTTP(voRec, voReq)
		if voRec.Code != http.StatusOK {
			t.Fatalf("expected 200 attaching VO, got %d: %s", voRec.Code, voRec.Body.String())
		}
		var withVo models.Job
		if err := json.Unmarshal(voRec.Body.Bytes(), &withVo); err != nil {
			t.Fatalf("failed to parse vo response: %v", err)
		}
		if !withVo.HasVo || withVo.Status != models.StatusPending {
			t.Fatalf("expected pending job with VO, got status=%s has_vo=%v", withVo.Status, withVo.HasVo)
		}

		allowReq := httptest.NewRequest("POST", "/api/jobs/"+job.ID+"/autocut", nil)
		allowRec := httptest.NewRecorder()
		server.ServeHTTP(allowRec, allowReq)
		if allowRec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 once a VO is attached, got %d: %s", allowRec.Code, allowRec.Body.String())
		}
	}
}
