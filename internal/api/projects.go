package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"autocut/internal/captions"
	"autocut/internal/models"
	"autocut/internal/pipeline"
	"autocut/internal/render"
	"autocut/internal/timeline"
)

type analyzeRequest struct {
	AssetID string `json:"asset_id"`
	UseAI   *bool  `json:"use_ai"`
	UseQA   *bool  `json:"use_qa"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ffmpegReady := false
	if _, err := exec.LookPath(s.cfg.FFmpegPath); err == nil {
		ffmpegReady = true
	} else if _, err := os.Stat(s.cfg.FFmpegPath); err == nil {
		ffmpegReady = true
	}
	whisperReady := false
	if _, err := os.Stat(s.cfg.WhisperCLIPath); err == nil {
		whisperReady = true
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":             ffmpegReady,
		"storage_dir":    s.cfg.StorageDir,
		"ffmpeg":         s.cfg.FFmpegPath,
		"ffmpeg_ready":   ffmpegReady,
		"whisper_ready":  whisperReady,
		"token_required": s.token != "",
	})
}

func (s *Server) handleFonts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.projects.Fonts().List())
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.projects.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, p := range projects {
		s.attachLastExport(p)
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name   string  `json:"name"`
		Width  int     `json:"width"`
		Height int     `json:"height"`
		FPS    float64 `json:"fps"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	p, err := s.projects.Create(body.Name, body.Width, body.Height, body.FPS)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	p, err := s.projects.Get(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, s.attachLastExport(p))
}

func (s *Server) handleRenameProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		http.Error(w, "Nama project tidak valid", http.StatusBadRequest)
		return
	}
	id := r.PathValue("id")
	if err := s.projects.Rename(id, strings.TrimSpace(body.Name)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p, err := s.projects.Get(id)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, s.attachLastExport(p))
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.projects.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.stateMu.Lock()
	delete(s.latest, id)
	delete(s.analysis, id)
	s.stateMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSaveTimeline(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.projects.Get(id)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}
	var tl models.Timeline
	if err := json.NewDecoder(r.Body).Decode(&tl); err != nil {
		http.Error(w, "Timeline tidak valid: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(tl.Tracks) == 0 {
		http.Error(w, "Timeline harus memiliki minimal satu track", http.StatusBadRequest)
		return
	}
	if tl.Width <= 0 {
		tl.Width = p.Width
	}
	if tl.Height <= 0 {
		tl.Height = p.Height
	}
	if tl.FPS <= 0 {
		tl.FPS = p.FPS
	}
	if tl.Version == 0 {
		tl.Version = 1
	}
	if err := s.projects.SaveTimeline(id, &tl); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p.Timeline = tl
	writeJSON(w, http.StatusOK, s.attachLastExport(p))
}

func (s *Server) handleListAssets(w http.ResponseWriter, r *http.Request) {
	assets, err := s.projects.Assets(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, assets)
}

func (s *Server) handleImportAsset(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	file, handler, err := readUpload(r)
	if err != nil {
		http.Error(w, "File media tidak ditemukan: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	asset, err := s.projects.Import(projectID, file, handler.Filename)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, asset)
}

func (s *Server) handleImportAssetPath(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Path) == "" {
		http.Error(w, "Path media tidak valid", http.StatusBadRequest)
		return
	}
	asset, err := s.projects.ImportPath(r.PathValue("id"), body.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, asset)
}

func (s *Server) handleDeleteAsset(w http.ResponseWriter, r *http.Request) {
	if err := s.projects.DeleteAsset(r.PathValue("id"), r.PathValue("assetID")); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAssetFile(w http.ResponseWriter, r *http.Request) {
	asset, err := s.projects.Asset(r.PathValue("id"), r.PathValue("assetID"))
	if err != nil {
		http.Error(w, "Asset not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", safeDownloadName(asset.Filename)))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, asset.Path)
}

func (s *Server) handleAssetThumb(w http.ResponseWriter, r *http.Request) {
	path, err := s.projects.AssetThumbnail(r.PathValue("id"), r.PathValue("assetID"))
	if err != nil {
		http.Error(w, "Thumbnail not available", http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}

func (s *Server) handleAssetWaveform(w http.ResponseWriter, r *http.Request) {
	path, err := s.projects.AssetWaveform(r.PathValue("id"), r.PathValue("assetID"))
	if err != nil {
		http.Error(w, "Waveform not available", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}

func (s *Server) handleGetProjectTranscript(w http.ResponseWriter, r *http.Request) {
	transcript, err := s.projects.Transcript(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Transcript not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, transcript)
}

func (s *Server) handleAnalyzeProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	var body analyzeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Payload tidak valid: "+err.Error(), http.StatusBadRequest)
		return
	}
	asset, err := s.projects.Asset(projectID, body.AssetID)
	if err != nil {
		http.Error(w, "Asset tidak ditemukan", http.StatusNotFound)
		return
	}
	if asset.Kind == models.AssetImage || (asset.Kind == models.AssetVideo && !asset.HasAudio) {
		http.Error(w, "Asset ini tidak punya audio untuk dianalisis", http.StatusBadRequest)
		return
	}
	if s.tasks.running("analyze", projectID) {
		http.Error(w, "Analisis masih berjalan", http.StatusConflict)
		return
	}

	useAI, useQA := true, true
	if body.UseAI != nil {
		useAI = *body.UseAI
	}
	if body.UseQA != nil {
		useQA = *body.UseQA
	}

	task := s.tasks.begin("analyze", projectID, fmt.Sprintf("ana_%d", time.Now().UnixNano()))
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				task.Fail(fmt.Errorf("analisis berhenti tak terduga: %v", rec))
			}
		}()
		result, err := s.orchestrator.AnalyzeVO(asset.Path, pipeline.AnalyzeOptions{
			TempDir: filepath.Join(s.projects.CacheDir(projectID), "analyze"),
			UseAI:   useAI,
			UseQA:   useQA,
			OnProgress: func(pct float64, message string) {
				task.Update(0.03+0.92*pct, message)
			},
		})
		if err != nil {
			task.Fail(err)
			return
		}
		result.Transcript.AssetID = asset.ID
		result.Transcript.JobID = projectID
		if err := s.projects.SaveTranscript(projectID, result.Transcript); err != nil {
			task.Fail(err)
			return
		}
		s.stateMu.Lock()
		s.analysis[projectID] = result
		s.stateMu.Unlock()
		task.Complete(result)
	}()

	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "started",
		"task_id": task.id,
		"kind":    "analyze",
	})
}

func (s *Server) handleLatestAnalysis(w http.ResponseWriter, r *http.Request) {
	s.stateMu.Lock()
	result, ok := s.analysis[r.PathValue("id")]
	s.stateMu.Unlock()
	if !ok {
		http.Error(w, "Belum ada hasil analisis", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleApplyCuts(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	var body struct {
		AssetID string              `json:"asset_id"`
		ClipIDs []string            `json:"clip_ids"`
		Cuts    []models.CutSegment `json:"cuts"`
		Ripple  *bool               `json:"ripple"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Payload tidak valid: "+err.Error(), http.StatusBadRequest)
		return
	}
	p, err := s.projects.Get(projectID)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	clipIDs := body.ClipIDs
	if len(clipIDs) == 0 {
		for _, track := range p.Timeline.Tracks {
			if track.Kind == models.TrackCaption {
				continue
			}
			for _, clip := range track.Clips {
				if clip.AssetID == body.AssetID {
					clipIDs = append(clipIDs, clip.ID)
				}
			}
		}
	}
	if len(clipIDs) == 0 {
		http.Error(w, "Tidak ada clip yang memakai asset ini di timeline", http.StatusBadRequest)
		return
	}
	ripple := true
	if body.Ripple != nil {
		ripple = *body.Ripple
	}
	removed := timeline.ApplyCuts(&p.Timeline, clipIDs, body.Cuts, ripple)
	if err := s.projects.SaveTimeline(projectID, &p.Timeline); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project":         s.attachLastExport(p),
		"removed_seconds": removed,
		"clips_affected":  len(clipIDs),
	})
}

func (s *Server) handleGenerateCaptions(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	var body struct {
		Replace *bool `json:"replace"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	transcript, err := s.projects.Transcript(projectID)
	if err != nil || len(transcript.Words) == 0 {
		http.Error(w, "Belum ada transkrip. Jalankan analisis voice-over dulu.", http.StatusConflict)
		return
	}
	p, err := s.projects.Get(projectID)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	var sources []models.Clip
	for _, track := range p.Timeline.Tracks {
		if track.Kind == models.TrackAudio || track.Kind == models.TrackVideo {
			sources = append(sources, track.Clips...)
		}
	}
	generated := captions.FromTranscript(sources, transcript, p.Timeline.CaptionStyle)
	if len(generated) == 0 {
		http.Error(w, "Tidak ada kata transkrip yang cocok dengan clip di timeline.", http.StatusUnprocessableEntity)
		return
	}

	replace := true
	if body.Replace != nil {
		replace = *body.Replace
	}
	target := -1
	for i := range p.Timeline.Tracks {
		if p.Timeline.Tracks[i].Kind != models.TrackCaption {
			continue
		}
		if target < 0 {
			target = i
		}
		if replace {
			p.Timeline.Tracks[i].Clips = []models.Clip{}
		}
	}
	if target < 0 {
		p.Timeline.Tracks = append(p.Timeline.Tracks, models.Track{
			ID:    "caption",
			Kind:  models.TrackCaption,
			Name:  "Caption",
			Clips: []models.Clip{},
		})
		target = len(p.Timeline.Tracks) - 1
	}
	p.Timeline.Tracks[target].Clips = append(p.Timeline.Tracks[target].Clips, generated...)

	if err := s.projects.SaveTimeline(projectID, &p.Timeline); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project":           s.attachLastExport(p),
		"caption_count":     len(generated),
		"replaced_existing": replace,
	})
}

func (s *Server) handleStartExport(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	if s.tasks.running("export", projectID) {
		http.Error(w, "Export masih berjalan", http.StatusConflict)
		return
	}
	p, err := s.projects.Get(projectID)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}
	if models.TimelineDuration(p.Timeline) <= 0.05 {
		http.Error(w, "Timeline masih kosong", http.StatusBadRequest)
		return
	}

	var body struct {
		Settings models.ExportSettings `json:"settings"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	settings := body.Settings
	if settings.Width <= 0 {
		settings.Width = p.Timeline.Width
	}
	if settings.Height <= 0 {
		settings.Height = p.Timeline.Height
	}
	if settings.FPS <= 0 {
		settings.FPS = p.Timeline.FPS
	}
	if settings.Quality <= 0 {
		settings.Quality = 20
	}
	if strings.TrimSpace(settings.Encoder) == "" {
		settings.Encoder = "auto"
	}
	outputName := exportFileName(settings.Filename, p.Name)
	settings.Filename = outputName
	outputPath := filepath.Join(s.projects.ExportsDir(projectID), outputName)

	assets, err := s.projects.Assets(projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	assetsByID := make(map[string]*models.Asset, len(assets))
	for _, asset := range assets {
		assetsByID[asset.ID] = asset
	}

	exportID := fmt.Sprintf("exp_%d", time.Now().UnixNano())
	job := &models.ExportJob{
		ID:         exportID,
		ProjectID:  projectID,
		Settings:   settings,
		Status:     TaskRunning,
		Message:    "Menyiapkan render...",
		OutputName: outputName,
		StartedAt:  time.Now(),
	}
	s.stateMu.Lock()
	s.exports[exportID] = job
	s.latest[projectID] = exportID
	s.stateMu.Unlock()

	task := s.tasks.begin("export", projectID, exportID)
	go func() {
		update := func(progress float64, message string) {
			task.Update(progress, message)
			s.stateMu.Lock()
			job.Progress = progress * 100
			if message != "" {
				job.Message = message
			}
			s.stateMu.Unlock()
		}
		err := s.renderer.Render(context.Background(), render.RenderRequest{
			Timeline:   p.Timeline,
			Assets:     assetsByID,
			Settings:   settings,
			OutputPath: outputPath,
			CacheDir:   s.projects.CacheDir(projectID),
			OnProgress: update,
		})
		now := time.Now()
		s.stateMu.Lock()
		job.FinishedAt = &now
		if err != nil {
			job.Status = TaskFailed
			job.Error = err.Error()
			job.Message = "Render gagal."
		} else {
			job.Status = TaskCompleted
			job.Progress = 100
			job.Message = "Export selesai."
			job.OutputPath = outputPath
		}
		snapshot := *job
		s.stateMu.Unlock()
		if err != nil {
			task.Fail(err)
			return
		}
		task.Complete(&snapshot)
	}()

	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) handleExportEvents(w http.ResponseWriter, r *http.Request) {
	s.streamTask(w, r, "export")
}

func (s *Server) handleAnalyzeEvents(w http.ResponseWriter, r *http.Request) {
	s.streamTask(w, r, "analyze")
}

func (s *Server) streamTask(w http.ResponseWriter, r *http.Request, kind string) {
	projectID := r.PathValue("id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch, cancel, ok := s.tasks.subscribe(kind, projectID)
	if !ok {
		http.Error(w, "Tidak ada task", http.StatusNotFound)
		return
	}
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event, open := <-ch:
			if !open {
				return
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if event.Status != TaskRunning {
				return
			}
		}
	}
}

func (s *Server) handleLatestExport(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("id")
	s.stateMu.Lock()
	id, ok := s.latest[projectID]
	var job *models.ExportJob
	if ok {
		job = s.exports[id]
	}
	var snapshot models.ExportJob
	if job != nil {
		snapshot = *job
	}
	s.stateMu.Unlock()
	if job == nil {
		http.Error(w, "Belum ada export", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) handleExportDownload(w http.ResponseWriter, r *http.Request) {
	s.stateMu.Lock()
	job := s.exports[r.PathValue("exportID")]
	s.stateMu.Unlock()
	if job == nil || job.ProjectID != r.PathValue("id") {
		http.Error(w, "Export not found", http.StatusNotFound)
		return
	}
	if job.Status != TaskCompleted || job.OutputPath == "" {
		http.Error(w, "Export belum selesai", http.StatusConflict)
		return
	}
	if _, err := os.Stat(job.OutputPath); err != nil {
		http.Error(w, "File export tidak ditemukan", http.StatusNotFound)
		return
	}
	disposition := "attachment"
	if r.URL.Query().Get("play") == "1" {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=\"%s\"", disposition, safeDownloadName(job.OutputName)))
	http.ServeFile(w, r, job.OutputPath)
}

func (s *Server) attachLastExport(p *models.Project) *models.Project {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if id, ok := s.latest[p.ID]; ok {
		if job, ok := s.exports[id]; ok {
			snapshot := *job
			p.LastExport = &snapshot
		}
	}
	return p
}

func exportFileName(requested, projectName string) string {
	name := strings.TrimSpace(requested)
	if name == "" {
		name = strings.TrimSpace(projectName)
	}
	if name == "" {
		name = "Export"
	}
	name = filepath.Base(name)
	invalid := `<>:"/\|?*`
	name = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(invalid, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if strings.TrimSpace(name) == "" {
		name = "Export"
	}
	return name + ".mp4"
}
