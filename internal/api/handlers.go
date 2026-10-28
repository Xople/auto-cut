package api

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"autocut/internal/config"
	"autocut/internal/db"
	"autocut/internal/media"
	"autocut/internal/models"
	"autocut/internal/pipeline"
	"autocut/internal/project"
	"autocut/internal/render"
	"autocut/internal/storage"
)

type Server struct {
	cfg          *config.Config
	db           *db.DB
	orchestrator *pipeline.Orchestrator
	storageMgr   *storage.StorageManager
	projects     *project.Service
	renderer     *render.Renderer
	tasks        *taskHub
	token        string
	mux          *http.ServeMux

	stateMu  sync.Mutex
	exports  map[string]*models.ExportJob
	latest   map[string]string
	analysis map[string]*models.AnalysisResult
}

func NewServer(cfg *config.Config, database *db.DB, orch *pipeline.Orchestrator) *Server {
	projectSvc := project.NewService(cfg, database)
	s := &Server{
		cfg:          cfg,
		db:           database,
		orchestrator: orch,
		storageMgr:   storage.NewStorageManager(cfg.StorageDir),
		projects:     projectSvc,
		renderer:     render.NewRenderer(projectSvc.FFmpeg(), projectSvc.Fonts()),
		tasks:        newTaskHub(),
		token:        os.Getenv("AUTOCUT_TOKEN"),
		mux:          http.NewServeMux(),
		exports:      map[string]*models.ExportJob{},
		latest:       map[string]string{},
		analysis:     map[string]*models.AnalysisResult{},
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for development
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Autocut-Token")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if s.token != "" && r.URL.Path != "/api/health" {
		token := r.Header.Get("X-Autocut-Token")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token != s.token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/fonts", s.handleFonts)

	s.mux.HandleFunc("GET /api/projects", s.handleListProjects)
	s.mux.HandleFunc("POST /api/projects", s.handleCreateProject)
	s.mux.HandleFunc("GET /api/projects/{id}", s.handleGetProject)
	s.mux.HandleFunc("PATCH /api/projects/{id}", s.handleRenameProject)
	s.mux.HandleFunc("DELETE /api/projects/{id}", s.handleDeleteProject)
	s.mux.HandleFunc("PUT /api/projects/{id}/timeline", s.handleSaveTimeline)

	s.mux.HandleFunc("GET /api/projects/{id}/assets", s.handleListAssets)
	s.mux.HandleFunc("POST /api/projects/{id}/import", s.handleImportAsset)
	s.mux.HandleFunc("POST /api/projects/{id}/import-path", s.handleImportAssetPath)
	s.mux.HandleFunc("DELETE /api/projects/{id}/assets/{assetID}", s.handleDeleteAsset)
	s.mux.HandleFunc("GET /api/projects/{id}/assets/{assetID}/file", s.handleAssetFile)
	s.mux.HandleFunc("GET /api/projects/{id}/assets/{assetID}/thumb", s.handleAssetThumb)
	s.mux.HandleFunc("GET /api/projects/{id}/assets/{assetID}/waveform", s.handleAssetWaveform)

	s.mux.HandleFunc("GET /api/projects/{id}/transcript", s.handleGetProjectTranscript)
	s.mux.HandleFunc("POST /api/projects/{id}/analyze", s.handleAnalyzeProject)
	s.mux.HandleFunc("GET /api/projects/{id}/analyze", s.handleLatestAnalysis)
	s.mux.HandleFunc("GET /api/projects/{id}/analyze/events", s.handleAnalyzeEvents)
	s.mux.HandleFunc("POST /api/projects/{id}/autocut/apply", s.handleApplyCuts)
	s.mux.HandleFunc("POST /api/projects/{id}/captions", s.handleGenerateCaptions)

	s.mux.HandleFunc("POST /api/projects/{id}/export", s.handleStartExport)
	s.mux.HandleFunc("GET /api/projects/{id}/export/events", s.handleExportEvents)
	s.mux.HandleFunc("GET /api/projects/{id}/export/latest", s.handleLatestExport)
	s.mux.HandleFunc("GET /api/projects/{id}/export/{exportID}/download", s.handleExportDownload)

	s.mux.HandleFunc("POST /api/jobs", s.handleCreateJob)
	s.mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	s.mux.HandleFunc("GET /api/jobs/{id}", s.handleGetJob)
	s.mux.HandleFunc("POST /api/jobs/{id}/vo", s.handleAttachVo)
	s.mux.HandleFunc("POST /api/jobs/{id}/video", s.handleAttachVideo)
	s.mux.HandleFunc("POST /api/jobs/{id}/autocut", s.handleAutoCut)
	s.mux.HandleFunc("POST /api/jobs/{id}/captions", s.handleCaptions)
	s.mux.HandleFunc("GET /api/jobs/{id}/events", s.handleSSE)
	s.mux.HandleFunc("GET /api/jobs/{id}/transcript", s.handleGetTranscript)
	s.mux.HandleFunc("GET /api/jobs/{id}/cutlist", s.handleGetCutlist)
	s.mux.HandleFunc("PUT /api/jobs/{id}/cutlist", s.handleUpdateCutlist)
	s.mux.HandleFunc("POST /api/jobs/{id}/render", s.handleRenderJob)
	s.mux.HandleFunc("GET /api/jobs/{id}/metadata", s.handleGetMetadata)
	s.mux.HandleFunc("GET /api/jobs/{id}/download/{type}", s.handleDownload)
	s.mux.HandleFunc("POST /api/jobs/{id}/cleanup", s.handleCleanup)
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("POST /api/config", s.handleSaveConfig)

	// Serve static frontend files if directory exists
	staticDir := filepath.Join(".", "web", "dist")
	if _, err := os.Stat(staticDir); err == nil {
		fs := http.FileServer(http.Dir(staticDir))
		s.mux.Handle("/", fs)
	} else {
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api") {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(`<!DOCTYPE html><html><head><title>TxShD AutoCut Workstation</title><style>body{background:#09090b;color:#f4f4f5;font-family:sans-serif;padding:40px;line-height:1.6;}code{background:#27272a;padding:2px 6px;border-radius:4px;}</style></head><body><h1>TxShD AutoCut Workstation Engine Ready</h1><p>API Server running on port 8080.</p><p>Frontend UI is served from <code>web/</code> directory.</p></body></html>`))
		})
	}
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	file, handler, err := readUpload(r)
	if err != nil {
		http.Error(w, "File upload missing (harus menyertakan file audio atau video): "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	kind, err := classifyUpload(handler.Filename)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	title := r.FormValue("title")
	if title == "" {
		title = strings.TrimSuffix(handler.Filename, filepath.Ext(handler.Filename))
	}

	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	stored, err := s.storeUpload(jobID, handler.Filename, file, kind)
	if err != nil {
		http.Error(w, "Failed to write media file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	job := &models.Job{
		ID:               jobID,
		Title:            title,
		OriginalFilename: filepath.Base(handler.Filename),
		MediaType:        kind,
		CurrentStep:      1,
		Progress:         0,
	}
	if kind == "audio" {
		job.VoPath = stored
		job.VoFilename = job.OriginalFilename
		job.HasVo = true
		job.Status = models.StatusPending
		job.StatusMessage = "Audio voice-over tersimpan. Auto cut siap dijalankan."
		job.DurationOriginal = s.probeDuration(stored)
	} else {
		job.RawVideoPath = stored
		job.HasVo = false
		job.Status = models.StatusAwaitingVo
		job.StatusMessage = "Video tersimpan. Upload VO atau file audio sebelum auto cut."
		job.VideoDuration = s.probeDuration(stored)
	}

	if err := s.db.CreateJob(job); err != nil {
		http.Error(w, "Failed to save job: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(job)
}

func (s *Server) handleAttachVo(w http.ResponseWriter, r *http.Request) {
	job, ok := s.requireJob(w, r)
	if !ok {
		return
	}
	if pipeline.IsBusy(job.Status) {
		http.Error(w, "Job sedang diproses", http.StatusConflict)
		return
	}

	file, handler, err := readUpload(r)
	if err != nil {
		http.Error(w, "File VO tidak ditemukan: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	kind, err := classifyUpload(handler.Filename)
	if err != nil || kind != "audio" {
		http.Error(w, "VO harus berupa file audio (wav, mp3, m4a, aac, flac, ogg)", http.StatusBadRequest)
		return
	}

	stored, err := s.storeUpload(job.ID, handler.Filename, file, "audio")
	if err != nil {
		http.Error(w, "Failed to write VO: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.db.UpdateJobVo(job.ID, stored, filepath.Base(handler.Filename), job.MediaType == "audio"); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	voDur := s.probeDuration(stored)
	if voDur > 0 {
		_ = s.db.UpdateJobDurations(job.ID, voDur, job.VideoDuration)
	}

	hadCut := job.CutVideoPath != "" || job.Status == models.StatusCutReady || job.Status == models.StatusCompleted || job.Status == models.StatusFailed
	if hadCut {
		if err := s.db.MarkCutStale(job.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else if job.Status == models.StatusAwaitingVo || job.Status == models.StatusPending {
		_ = s.db.UpdateJobProgress(job.ID, models.StatusPending, 1, 0, "VO terpasang. Auto cut siap dijalankan.")
	}

	updated, err := s.db.GetJob(job.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleAttachVideo(w http.ResponseWriter, r *http.Request) {
	job, ok := s.requireJob(w, r)
	if !ok {
		return
	}
	if pipeline.IsBusy(job.Status) {
		http.Error(w, "Job sedang diproses", http.StatusConflict)
		return
	}

	file, handler, err := readUpload(r)
	if err != nil {
		http.Error(w, "File video tidak ditemukan: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	kind, err := classifyUpload(handler.Filename)
	if err != nil || kind != "video" {
		http.Error(w, "Lampiran ini harus berupa file video (mp4, mkv, mov, avi, webm)", http.StatusBadRequest)
		return
	}

	stored, err := s.storeUpload(job.ID, handler.Filename, file, "video")
	if err != nil {
		http.Error(w, "Failed to write video: "+err.Error(), http.StatusInternalServerError)
		return
	}
	videoDur := s.probeDuration(stored)
	if err := s.db.UpdateJobVideo(job.ID, stored, filepath.Base(handler.Filename), videoDur); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := s.db.GetCutlist(job.ID); err == nil {
		if err := s.db.ClearRenderedMedia(job.ID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	updated, err := s.db.GetJob(job.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleAutoCut(w http.ResponseWriter, r *http.Request) {
	job, ok := s.requireJob(w, r)
	if !ok {
		return
	}
	if reason := pipeline.AutoCutBlockReason(job); reason != "" {
		http.Error(w, reason, http.StatusConflict)
		return
	}
	claimed, err := s.db.ClaimJob(job.ID, []models.JobStatus{job.Status}, models.StatusCutting, "Auto cut dimulai...")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !claimed {
		http.Error(w, "Job sedang diproses", http.StatusConflict)
		return
	}
	go func() {
		_ = s.orchestrator.RunAutoCut(job.ID)
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "auto cut started"})
}

func (s *Server) handleCaptions(w http.ResponseWriter, r *http.Request) {
	job, ok := s.requireJob(w, r)
	if !ok {
		return
	}
	_, transcriptErr := s.db.GetTranscript(job.ID)
	if reason := pipeline.CaptionsBlockReason(job, transcriptErr == nil); reason != "" {
		http.Error(w, reason, http.StatusConflict)
		return
	}
	claimed, err := s.db.ClaimJob(job.ID, []models.JobStatus{models.StatusCutReady, models.StatusCompleted}, models.StatusCaptioning, "Auto caption dimulai...")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !claimed {
		http.Error(w, "Job sedang diproses", http.StatusConflict)
		return
	}
	go func() {
		_ = s.orchestrator.RunAutoCaptions(job.ID)
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "auto captions started"})
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.db.ListJobs()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jobs)
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, err := s.db.GetJob(id)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(job)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.orchestrator.Subscribe(id)
	defer s.orchestrator.Unsubscribe(id, ch)

	// Send initial status immediately
	if job, err := s.db.GetJob(id); err == nil {
		initEvent := models.PipelineProgressEvent{
			JobID:         job.ID,
			Status:        job.Status,
			CurrentStep:   job.CurrentStep,
			TotalSteps:    9,
			Progress:      job.Progress,
			StatusMessage: job.StatusMessage,
			ErrorMessage:  job.ErrorMessage,
		}
		data, _ := json.Marshal(initEvent)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			if event.Status == models.StatusCompleted || event.Status == models.StatusFailed {
				return
			}
		}
	}
}

func (s *Server) handleGetTranscript(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := s.db.GetTranscript(id)
	if err != nil {
		http.Error(w, "Transcript not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(t)
}

func (s *Server) handleGetCutlist(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.db.GetCutlist(id)
	if err != nil {
		http.Error(w, "Cutlist not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(c)
}

func (s *Server) handleUpdateCutlist(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var payload struct {
		Segments []models.CutSegment `json:"segments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	job, err := s.db.GetJob(id)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Recalculate keep intervals on the video timeline when the job has a picture.
	total := pipeline.CutTimeline(job)
	if total <= 0 {
		total = job.DurationOriginal
	}
	newCutlist := pipeline.BuildCutlist(id, payload.Segments, total, s.cfg.SafetyPaddingMs)
	if err := s.db.SaveCutlist(newCutlist); err != nil {
		http.Error(w, "Failed to save cutlist: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(newCutlist)
}

func (s *Server) handleRenderJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.requireJob(w, r)
	if !ok {
		return
	}
	_, cutErr := s.db.GetCutlist(job.ID)
	if reason := pipeline.RenderBlockReason(job, cutErr == nil); reason != "" {
		http.Error(w, reason, http.StatusConflict)
		return
	}
	claimed, err := s.db.ClaimJob(job.ID, []models.JobStatus{models.StatusCutReady, models.StatusCompleted, models.StatusFailed}, models.StatusCutting, "Render ulang dimulai...")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !claimed {
		http.Error(w, "Job sedang diproses", http.StatusConflict)
		return
	}
	go func() {
		_ = s.orchestrator.RerenderCut(job.ID)
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "re-rendering started"})
}

func (s *Server) handleGetMetadata(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	meta, err := s.db.GetMetadata(id)
	if err != nil {
		http.Error(w, "Metadata not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(meta)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dlType := r.PathValue("type") // "final", "cut", "audio", "transcript", "cutlist"

	job, err := s.db.GetJob(id)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	var filePath string
	var downloadFilename string
	inline := r.URL.Query().Get("play") == "1"

	switch dlType {
	case "raw":
		if job.MediaType == "audio" {
			filePath = job.VoPath
			downloadFilename = fmt.Sprintf("TxShD_%s_vo%s", job.Title, filepath.Ext(job.VoPath))
		} else {
			filePath = job.RawVideoPath
			downloadFilename = fmt.Sprintf("TxShD_%s_raw%s", job.Title, filepath.Ext(job.RawVideoPath))
		}
		inline = true
	case "vo":
		filePath = job.VoPath
		downloadFilename = fmt.Sprintf("TxShD_%s_vo%s", job.Title, filepath.Ext(job.VoPath))
		inline = true
	case "final":
		filePath = job.FinalVideoPath
		if job.MediaType == "audio" {
			if filePath == "" {
				filePath = job.CutVideoPath
			}
			downloadFilename = fmt.Sprintf("TxShD_%s_cut.wav", job.Title)
		} else {
			downloadFilename = fmt.Sprintf("TxShD_%s_final.mp4", job.Title)
		}
	case "cut":
		filePath = job.CutVideoPath
		if job.MediaType == "audio" {
			downloadFilename = fmt.Sprintf("TxShD_%s_cut.wav", job.Title)
		} else {
			downloadFilename = fmt.Sprintf("TxShD_%s_cut.mp4", job.Title)
		}
	case "audio":
		filePath = cutAudioPath(s, id, job)
		downloadFilename = fmt.Sprintf("TxShD_%s_audio.wav", job.Title)
	case "ass", "subtitle":
		filePath = filepath.Join(s.storageMgr.GetTempDir(id), "subtitles.ass")
		downloadFilename = fmt.Sprintf("TxShD_%s_subtitles.ass", job.Title)
	case "transcript":
		filePath = filepath.Join(s.storageMgr.GetArchiveDir(id), "transcript.json")
		downloadFilename = fmt.Sprintf("TxShD_%s_transcript.json", job.Title)
	case "cutlist":
		filePath = filepath.Join(s.storageMgr.GetArchiveDir(id), "cutlist.json")
		downloadFilename = fmt.Sprintf("TxShD_%s_cutlist.json", job.Title)
	default:
		http.Error(w, "Invalid download type", http.StatusBadRequest)
		return
	}

	if filePath == "" {
		http.Error(w, "File not available yet", http.StatusNotFound)
		return
	}
	if _, err := os.Stat(filePath); err != nil {
		http.Error(w, "File not available yet", http.StatusNotFound)
		return
	}

	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=\"%s\"", disposition, safeDownloadName(downloadFilename)))
	http.ServeFile(w, r, filePath)
}

func (s *Server) handleCleanup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_ = s.storageMgr.CleanupEphemeralFiles(id)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": "Ephemeral video files cleaned up"})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.cfg)
}

func readUpload(r *http.Request) (multipart.File, *multipart.FileHeader, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, nil, err
	}
	var lastErr error
	for _, key := range []string{"file", "video", "audio", "media"} {
		f, h, err := r.FormFile(key)
		if err == nil {
			return f, h, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("missing file")
	}
	return nil, nil, lastErr
}

func classifyUpload(filename string) (string, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".wav", ".mp3", ".m4a", ".aac", ".flac", ".ogg", ".wma":
		return "audio", nil
	case ".mp4", ".mkv", ".mov", ".avi", ".webm", ".m4v":
		return "video", nil
	default:
		return "", fmt.Errorf("format tidak didukung: %s", filepath.Ext(filename))
	}
}

func (s *Server) storeUpload(jobID, filename string, src io.Reader, kind string) (string, error) {
	tempDir := s.storageMgr.GetTempDir(jobID)
	prefix := "raw_"
	if kind == "audio" {
		prefix = "vo_"
	}
	stored := filepath.Join(tempDir, prefix+filepath.Base(filename))
	dest, err := os.Create(stored)
	if err != nil {
		return "", err
	}
	defer dest.Close()
	if _, err := io.Copy(dest, src); err != nil {
		return "", err
	}
	return stored, nil
}

func (s *Server) probeDuration(path string) float64 {
	ff := media.NewFFmpeg(s.cfg.FFmpegPath, false)
	dur, err := ff.GetDuration(path)
	if err != nil || dur < 0 {
		return 0
	}
	return dur
}

func (s *Server) requireJob(w http.ResponseWriter, r *http.Request) (*models.Job, bool) {
	job, err := s.db.GetJob(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return nil, false
	}
	return job, true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func cutAudioPath(s *Server, jobID string, job *models.Job) string {
	candidates := []string{
		filepath.Join(s.storageMgr.GetTempDir(jobID), "audio_cut.wav"),
		filepath.Join(s.storageMgr.GetArchiveDir(jobID), "audio_cut.wav"),
	}
	if job.MediaType == "audio" {
		candidates = append([]string{job.CutVideoPath}, candidates...)
	}
	candidates = append(candidates, job.AudioPath, job.VoPath)
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func safeDownloadName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '"', '\\', '/', '\n', '\r':
			return '_'
		default:
			return r
		}
	}, name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "export"
	}
	return name
}

func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	var newCfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	*s.cfg = newCfg
	_ = s.cfg.SaveConfig()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.cfg)
}
