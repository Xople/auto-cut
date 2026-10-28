package pipeline

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"autocut/internal/ai/deepseek"
	"autocut/internal/ai/gemini"
	"autocut/internal/ai/typesafe"
	"autocut/internal/captions"
	"autocut/internal/config"
	"autocut/internal/db"
	"autocut/internal/logger"
	"autocut/internal/media"
	"autocut/internal/models"
	"autocut/internal/storage"
	"autocut/internal/stt"
)

type Orchestrator struct {
	cfg        *config.Config
	db         *db.DB
	ffmpeg     *media.FFmpeg
	whisper    *stt.WhisperRunner
	deepseek   *deepseek.Client
	typesafe   *typesafe.Client
	gemini     *gemini.Client
	storageMgr *storage.StorageManager

	// SSE subscriber channels: map[jobID][]chan models.PipelineProgressEvent
	subscribersMu sync.RWMutex
	subscribers   map[string][]chan models.PipelineProgressEvent
}

func NewOrchestrator(cfg *config.Config, database *db.DB) *Orchestrator {
	return &Orchestrator{
		cfg:         cfg,
		db:          database,
		ffmpeg:      media.NewFFmpeg(cfg.FFmpegPath, cfg.UseNVENC),
		whisper:     stt.NewWhisperRunner(cfg.WhisperCLIPath, cfg.WhisperModelPath),
		deepseek:    deepseek.NewClient(cfg.DeepSeekAPIKey),
		typesafe:    typesafe.NewClient(cfg.TypeSafeAPIKey),
		gemini:      gemini.NewClient(cfg.GeminiAPIKey),
		storageMgr:  storage.NewStorageManager(cfg.StorageDir),
		subscribers: make(map[string][]chan models.PipelineProgressEvent),
	}
}

// Subscribe returns a channel that receives real-time progress events for a job.
func (o *Orchestrator) Subscribe(jobID string) chan models.PipelineProgressEvent {
	o.subscribersMu.Lock()
	defer o.subscribersMu.Unlock()

	ch := make(chan models.PipelineProgressEvent, 20)
	o.subscribers[jobID] = append(o.subscribers[jobID], ch)
	return ch
}

// Unsubscribe removes a listener channel.
func (o *Orchestrator) Unsubscribe(jobID string, ch chan models.PipelineProgressEvent) {
	o.subscribersMu.Lock()
	defer o.subscribersMu.Unlock()

	channels := o.subscribers[jobID]
	for i, c := range channels {
		if c == ch {
			o.subscribers[jobID] = append(channels[:i], channels[i+1:]...)
			close(c)
			break
		}
	}
}

func (o *Orchestrator) emitProgress(jobID string, status models.JobStatus, step, total int, progress float64, msg string, errMsg string) {
	_ = o.db.UpdateJobProgress(jobID, status, step, progress, msg)
	if errMsg != "" {
		_ = o.db.UpdateJobError(jobID, errMsg)
		logger.Error("[Pipeline Job %s] [Step %d/%d] %s: %s", jobID, step, total, msg, errMsg)
	} else {
		logger.Info("[Pipeline Job %s] [Step %d/%d] (%.1f%%) %s", jobID, step, total, progress, msg)
	}

	event := models.PipelineProgressEvent{
		JobID:         jobID,
		Status:        status,
		CurrentStep:   step,
		TotalSteps:    total,
		Progress:      progress,
		StatusMessage: msg,
		ErrorMessage:  errMsg,
	}

	o.subscribersMu.RLock()
	defer o.subscribersMu.RUnlock()

	if channels, ok := o.subscribers[jobID]; ok {
		for _, ch := range channels {
			select {
			case ch <- event:
			default:
				// Avoid blocking if subscriber is slow
			}
		}
	}
}

// IsBusy reports whether a job is already inside auto cut, captions, or a legacy in-progress step.
func IsBusy(status models.JobStatus) bool {
	switch status {
	case models.StatusCutting, models.StatusCaptioning,
		models.StatusExtractingAudio, models.StatusDetectingSilence, models.StatusTranscribing,
		models.StatusReasoning, models.StatusReviewing, models.StatusRendering,
		models.StatusBurningCaptions, models.StatusGeneratingMeta:
		return true
	default:
		return false
	}
}

// AutoCutBlockReason is empty when auto cut may start. Video jobs need an attached VO.
func AutoCutBlockReason(job *models.Job) string {
	if job == nil {
		return "Job tidak ditemukan"
	}
	if IsBusy(job.Status) {
		return "Job sedang diproses"
	}
	if !job.HasVo || strings.TrimSpace(job.VoPath) == "" {
		return "Upload VO atau file audio dulu sebelum auto cut"
	}
	if job.MediaType == "video" && strings.TrimSpace(job.RawVideoPath) == "" {
		return "File video belum ada"
	}
	return ""
}

// CaptionsBlockReason is empty when auto captions may start. A transcript from auto cut is required.
func CaptionsBlockReason(job *models.Job, hasTranscript bool) string {
	if job == nil {
		return "Job tidak ditemukan"
	}
	if IsBusy(job.Status) {
		return "Job sedang diproses"
	}
	if !hasTranscript {
		return "Auto caption membutuhkan hasil auto cut terlebih dahulu"
	}
	if job.Status != models.StatusCutReady && job.Status != models.StatusCompleted {
		return "Auto caption tersedia setelah auto cut selesai"
	}
	if job.MediaType == "video" && strings.TrimSpace(job.CutVideoPath) == "" {
		return "Render ulang dulu agar caption dibakar ke video baru"
	}
	return ""
}

// RenderBlockReason is empty when the current cutlist can be applied again without re-transcribing.
func RenderBlockReason(job *models.Job, hasCutlist bool) string {
	if job == nil {
		return "Job tidak ditemukan"
	}
	if IsBusy(job.Status) {
		return "Job sedang diproses"
	}
	if !hasCutlist {
		return "Belum ada cutlist. Jalankan auto cut terlebih dahulu"
	}
	return ""
}

const autoCutSteps = 9
const captionSteps = 2

// RunAutoCut detects silence and filler on the VO, then cuts the video on the video's timeline.
// A shorter VO only removes the overlapping picture. Audio-only jobs cut the VO itself.
func (o *Orchestrator) RunAutoCut(jobID string) error {
	job, err := o.db.GetJob(jobID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(job.VoPath) == "" {
		o.emitProgress(jobID, models.StatusFailed, 1, autoCutSteps, 0, "VO tidak ditemukan", "VO tidak ditemukan")
		return fmt.Errorf("VO tidak ditemukan")
	}

	tempDir := o.storageMgr.GetTempDir(jobID)
	analysisWav := filepath.Join(tempDir, "vo_analysis.wav")

	o.emitProgress(jobID, models.StatusCutting, 2, autoCutSteps, 10, "Menyiapkan audio voice-over...", "")
	if err := o.ffmpeg.ExtractAudio(job.VoPath, analysisWav); err != nil {
		o.emitProgress(jobID, models.StatusFailed, 2, autoCutSteps, 10, "Gagal menyiapkan voice-over", err.Error())
		return err
	}

	duration, err := o.ffmpeg.GetDuration(job.VoPath)
	if err != nil || duration <= 0 {
		duration, err = o.ffmpeg.GetDuration(analysisWav)
	}
	if err != nil || duration <= 0 {
		duration = 120
	}

	videoDur := job.VideoDuration
	if job.MediaType == "video" && job.RawVideoPath != "" {
		if probed, probeErr := o.ffmpeg.GetDuration(job.RawVideoPath); probeErr == nil && probed > 0 {
			videoDur = probed
		}
	}

	o.emitProgress(jobID, models.StatusCutting, 3, autoCutSteps, 20, "Mendeteksi jeda hening pada voice-over...", "")
	silenceCuts, err := o.ffmpeg.DetectSilence(analysisWav, o.cfg.SilenceNoiseThreshold, o.cfg.MinSilenceDuration)
	if err != nil {
		o.emitProgress(jobID, models.StatusFailed, 3, autoCutSteps, 20, "Gagal deteksi silence", err.Error())
		return err
	}

	o.emitProgress(jobID, models.StatusCutting, 4, autoCutSteps, 35, "Menjalankan Speech-to-Text lokal (whisper.cpp)...", "")
	transcript, err := o.whisper.Transcribe(analysisWav, duration)
	if err != nil {
		o.emitProgress(jobID, models.StatusFailed, 4, autoCutSteps, 35, "Gagal transkripsi voice-over", err.Error())
		return err
	}
	transcript.JobID = jobID
	_ = o.db.SaveTranscript(transcript)

	o.emitProgress(jobID, models.StatusCutting, 5, autoCutSteps, 50, "Menganalisis retake dan filler dengan DeepSeek...", "")
	macroProposals, err := o.deepseek.AnalyzeTranscriptForCuts(transcript.Words)
	if err != nil {
		macroProposals = nil
	}

	o.emitProgress(jobID, models.StatusCutting, 6, autoCutSteps, 60, "Memvalidasi batas kalimat dengan TypeSafe...", "")
	var aiCuts []models.CutSegment
	for _, prop := range macroProposals {
		if prop.StartIndex >= len(transcript.Words) || prop.EndIndex >= len(transcript.Words) {
			continue
		}
		startSec := transcript.Words[prop.StartIndex].Start
		endSec := transcript.Words[prop.EndIndex].End

		preceding := ""
		if prop.StartIndex > 0 {
			preceding = transcript.Words[prop.StartIndex-1].Word
		}
		following := ""
		if prop.EndIndex < len(transcript.Words)-1 {
			following = transcript.Words[prop.EndIndex+1].Word
		}

		isFluent, prob, _ := o.typesafe.EvaluateBoundaryContinuity(preceding, prop.ExcerptText, following)
		classifiedReason, confidence, needsReview, _ := o.typesafe.ClassifySegment(prop.ExcerptText, transcript.FullText)
		if !isFluent {
			needsReview = true
		}

		aiCuts = append(aiCuts, models.CutSegment{
			ID:             fmt.Sprintf("ai_cut_%d_%d", prop.StartIndex, prop.EndIndex),
			Start:          startSec,
			End:            endSec,
			Duration:       endSec - startSec,
			Reason:         classifiedReason,
			Source:         "typesafe+deepseek",
			Text:           prop.ExcerptText,
			Confidence:     confidence * prob,
			NeedsReview:    needsReview,
			UserOverridden: false,
			IsCut:          true,
		})
	}

	// The video is the timeline being cut. A shorter VO only marks silence inside itself;
	// the rest of the picture stays.
	timeline := duration
	if job.MediaType == "video" && videoDur > 0 {
		timeline = videoDur
	}

	o.emitProgress(jobID, models.StatusCutting, 7, autoCutSteps, 70, "Menggabungkan cutlist video...", "")
	cutlist := BuildCutlist(jobID, append(silenceCuts, aiCuts...), timeline, o.cfg.SafetyPaddingMs)

	o.emitProgress(jobID, models.StatusCutting, 8, autoCutSteps, 75, "QA pass cutlist dengan Gemini...", "")
	passed, qaReport, err := o.gemini.SanityCheckCutlist(transcript.FullText, duration, cutlist.TotalCutDuration, cutlist.Segments)
	if err != nil {
		qaReport = "QA sanity check selesai dengan catatan."
	}
	cutlist.QASanityPassed = passed
	cutlist.QAReport = qaReport
	_ = o.db.SaveCutlist(cutlist)

	keeps := clampKeeps(cutlist.KeepIntervals, timeline)
	if len(keeps) == 0 {
		err := fmt.Errorf("tidak ada interval yang dipertahankan")
		o.emitProgress(jobID, models.StatusFailed, 9, autoCutSteps, 82, "Cutlist kosong", err.Error())
		return err
	}

	var cutPath, finalPath string
	if job.MediaType == "audio" {
		o.emitProgress(jobID, models.StatusCutting, 9, autoCutSteps, 88, "Memotong voice-over...", "")
		cutPath = filepath.Join(tempDir, "audio_cut.wav")
		if err := o.ffmpeg.CutAudioOnly(analysisWav, keeps, cutPath); err != nil {
			o.emitProgress(jobID, models.StatusFailed, 9, autoCutSteps, 88, "Gagal memotong audio", err.Error())
			return err
		}
		finalPath = cutPath
	} else {
		o.emitProgress(jobID, models.StatusCutting, 9, autoCutSteps, 88, "Memotong video...", "")
		cutPath = filepath.Join(tempDir, "video_cut.mp4")
		var cutErr error
		if o.ffmpeg.HasAudio(job.RawVideoPath) {
			cutErr = o.ffmpeg.CutAndReencode(job.RawVideoPath, keeps, cutPath)
		} else {
			cutErr = o.ffmpeg.CutVideoWithVO(job.RawVideoPath, job.VoPath, keeps, cutPath)
		}
		if cutErr != nil {
			o.emitProgress(jobID, models.StatusFailed, 9, autoCutSteps, 88, "Gagal memotong video", cutErr.Error())
			return cutErr
		}
		voCut := filepath.Join(tempDir, "audio_cut.wav")
		voKeeps := clampKeeps(keeps, duration)
		if len(voKeeps) == 0 {
			voCut = ""
		} else if err := o.ffmpeg.CutAudioOnly(analysisWav, voKeeps, voCut); err != nil {
			o.emitProgress(jobID, models.StatusFailed, 9, autoCutSteps, 88, "Gagal memotong audio voice-over", err.Error())
			return err
		}
		finalPath = ""
		_ = o.storageMgr.ArchiveEssentialAssets(jobID, voCut, transcript, cutlist, nil)
	}

	if job.MediaType == "audio" {
		_ = o.storageMgr.ArchiveEssentialAssets(jobID, cutPath, transcript, cutlist, nil)
	}

	timeSaved := 0.0
	if timeline > 0 {
		timeSaved = (cutlist.TotalCutDuration / timeline) * 100.0
	}
	// duration_original stays the VO length. video_duration is the picture being cut.
	if err := o.db.UpdateJobCutReady(jobID, cutPath, finalPath, analysisWav, duration, cutlist.TotalKeepDuration, timeSaved, videoDur); err != nil {
		return err
	}
	o.emitProgress(jobID, models.StatusCutReady, 9, autoCutSteps, 100, "Auto cut selesai. Periksa cutlist, lalu jalankan auto caption.", "")
	return nil
}

// RunAutoCaptions writes CapCut-style ASS from the cut transcript.
// Video jobs are burned from the uncaptioned cut so the step can be repeated.
func (o *Orchestrator) RunAutoCaptions(jobID string) error {
	job, err := o.db.GetJob(jobID)
	if err != nil {
		return err
	}
	transcript, err := o.db.GetTranscript(jobID)
	if err != nil {
		o.emitProgress(jobID, models.StatusFailed, 1, captionSteps, 10, "Transkrip tidak ditemukan", err.Error())
		return err
	}
	cutlist, err := o.db.GetCutlist(jobID)
	if err != nil {
		o.emitProgress(jobID, models.StatusFailed, 1, captionSteps, 10, "Cutlist tidak ditemukan", err.Error())
		return err
	}

	tempDir := o.storageMgr.GetTempDir(jobID)
	assPath := filepath.Join(tempDir, "subtitles.ass")
	o.emitProgress(jobID, models.StatusCaptioning, 1, captionSteps, 40, "Menyusun caption gaya CapCut...", "")
	if err := captions.GenerateDynamicAssSubtitles(transcript.Words, cutlist.KeepIntervals, assPath, captions.DefaultTxShDStyle()); err != nil {
		o.emitProgress(jobID, models.StatusFailed, 1, captionSteps, 40, "Gagal membuat subtitle", err.Error())
		return err
	}

	finalPath := job.FinalVideoPath
	if job.MediaType == "audio" {
		if finalPath == "" {
			finalPath = job.CutVideoPath
		}
	} else {
		if job.CutVideoPath == "" {
			err := fmt.Errorf("video hasil cut belum ada")
			o.emitProgress(jobID, models.StatusFailed, 2, captionSteps, 70, "Video cut tidak ditemukan", err.Error())
			return err
		}
		o.emitProgress(jobID, models.StatusCaptioning, 2, captionSteps, 70, "Membakar caption ke video...", "")
		finalPath = filepath.Join(tempDir, "video_final.mp4")
		if err := o.ffmpeg.BurnAssCaptions(job.CutVideoPath, assPath, finalPath); err != nil {
			o.emitProgress(jobID, models.StatusFailed, 2, captionSteps, 70, "Gagal membakar caption", err.Error())
			return err
		}
	}

	if err := o.db.UpdateJobCaptioned(jobID, finalPath); err != nil {
		return err
	}
	o.emitProgress(jobID, models.StatusCompleted, 2, captionSteps, 100, "Auto caption selesai.", "")
	return nil
}

// RerenderCut applies the current cutlist again. It does not transcribe or burn captions.
func (o *Orchestrator) RerenderCut(jobID string) error {
	job, err := o.db.GetJob(jobID)
	if err != nil {
		return err
	}
	cutlist, err := o.db.GetCutlist(jobID)
	if err != nil {
		o.emitProgress(jobID, models.StatusFailed, 1, 1, 10, "Cutlist tidak ditemukan", err.Error())
		return err
	}
	timeline := CutTimeline(job)
	if timeline <= 0 {
		timeline = cutlist.TotalKeepDuration + cutlist.TotalCutDuration
	}
	keeps := clampKeeps(cutlist.KeepIntervals, timeline)
	if len(keeps) == 0 {
		err := fmt.Errorf("tidak ada interval yang dipertahankan")
		o.emitProgress(jobID, models.StatusFailed, 1, 1, 10, "Cutlist kosong", err.Error())
		return err
	}

	tempDir := o.storageMgr.GetTempDir(jobID)
	sourceAudio := job.AudioPath
	if sourceAudio == "" {
		sourceAudio = job.VoPath
	}

	var cutPath, finalPath string
	if job.MediaType == "audio" {
		o.emitProgress(jobID, models.StatusCutting, 1, 1, 40, "Merender ulang potongan audio...", "")
		cutPath = filepath.Join(tempDir, "audio_cut.wav")
		if err := o.ffmpeg.CutAudioOnly(sourceAudio, keeps, cutPath); err != nil {
			o.emitProgress(jobID, models.StatusFailed, 1, 1, 40, "Gagal merender ulang audio", err.Error())
			return err
		}
		finalPath = cutPath
	} else {
		o.emitProgress(jobID, models.StatusCutting, 1, 1, 40, "Merender ulang potongan video...", "")
		cutPath = filepath.Join(tempDir, "video_cut.mp4")
		var cutErr error
		if o.ffmpeg.HasAudio(job.RawVideoPath) {
			cutErr = o.ffmpeg.CutAndReencode(job.RawVideoPath, keeps, cutPath)
		} else {
			cutErr = o.ffmpeg.CutVideoWithVO(job.RawVideoPath, job.VoPath, keeps, cutPath)
		}
		if cutErr != nil {
			o.emitProgress(jobID, models.StatusFailed, 1, 1, 40, "Gagal merender ulang video", cutErr.Error())
			return cutErr
		}
		voCut := filepath.Join(tempDir, "audio_cut.wav")
		voKeeps := clampKeeps(keeps, job.DurationOriginal)
		if len(voKeeps) == 0 {
			voCut = ""
		} else if err := o.ffmpeg.CutAudioOnly(sourceAudio, voKeeps, voCut); err != nil {
			o.emitProgress(jobID, models.StatusFailed, 1, 1, 40, "Gagal merender ulang audio voice-over", err.Error())
			return err
		}
		_ = o.storageMgr.ArchiveEssentialAssets(jobID, voCut, nil, cutlist, nil)
		finalPath = ""
	}

	timeSaved := job.TimeSavedPercent
	if timeline > 0 {
		timeSaved = (cutlist.TotalCutDuration / timeline) * 100.0
	}
	if err := o.db.UpdateJobCutReady(jobID, cutPath, finalPath, sourceAudio, job.DurationOriginal, cutlist.TotalKeepDuration, timeSaved, job.VideoDuration); err != nil {
		return err
	}
	o.emitProgress(jobID, models.StatusCutReady, 1, 1, 100, "Render ulang selesai. Caption lama dibuang; jalankan auto caption lagi bila perlu.", "")
	return nil
}

// CutTimeline is the media Auto Cut trims: the picture when the job has video, otherwise the VO.
func CutTimeline(job *models.Job) float64 {
	if job == nil {
		return 0
	}
	if job.MediaType == "video" && job.VideoDuration > 0 {
		return job.VideoDuration
	}
	return job.DurationOriginal
}

func clampKeeps(keeps []models.KeepInterval, limit float64) []models.KeepInterval {
	var out []models.KeepInterval
	for _, k := range keeps {
		start, end := k.Start, k.End
		if start < 0 {
			start = 0
		}
		if limit > 0 && end > limit {
			end = limit
		}
		if end-start >= 0.05 {
			out = append(out, models.KeepInterval{Start: start, End: end})
		}
	}
	return out
}

// BuildCutlist merges overlapping cut segments and inverts them into keep intervals with safety padding.
func BuildCutlist(jobID string, segments []models.CutSegment, totalDuration float64, paddingMs float64) *models.Cutlist {
	if len(segments) == 0 {
		return &models.Cutlist{
			JobID: jobID,
			KeepIntervals: []models.KeepInterval{
				{Start: 0, End: totalDuration},
			},
			TotalCutDuration:  0,
			TotalKeepDuration: totalDuration,
			UpdatedAt:         time.Now(),
		}
	}

	// Filter only segments marked as cut
	var activeCuts []models.CutSegment
	for _, s := range segments {
		if s.IsCut {
			activeCuts = append(activeCuts, s)
		}
	}

	// Sort cuts by start time
	sort.Slice(activeCuts, func(i, j int) bool {
		return activeCuts[i].Start < activeCuts[j].Start
	})

	paddingSec := paddingMs / 1000.0 // e.g. 0.06s padding

	// Merge overlapping cuts
	var mergedCuts []models.CutSegment
	for _, cut := range activeCuts {
		if len(mergedCuts) == 0 {
			mergedCuts = append(mergedCuts, cut)
			continue
		}
		lastIdx := len(mergedCuts) - 1
		if cut.Start <= mergedCuts[lastIdx].End {
			// Overlapping, extend end if longer
			if cut.End > mergedCuts[lastIdx].End {
				mergedCuts[lastIdx].End = cut.End
				mergedCuts[lastIdx].Duration = mergedCuts[lastIdx].End - mergedCuts[lastIdx].Start
			}
		} else {
			mergedCuts = append(mergedCuts, cut)
		}
	}

	// Invert cuts into keep intervals
	var keepIntervals []models.KeepInterval
	var currentPos float64 = 0.0
	var totalCutSec float64 = 0.0

	for _, cut := range mergedCuts {
		totalCutSec += cut.Duration

		// Speech section before this cut: pad slightly to avoid cutting off word edges
		keepStart := currentPos
		keepEnd := cut.Start - paddingSec

		if keepEnd > keepStart+0.1 {
			keepIntervals = append(keepIntervals, models.KeepInterval{
				Start: keepStart,
				End:   keepEnd,
			})
		}
		currentPos = cut.End + paddingSec
	}

	// Final speech segment after the last cut
	if currentPos < totalDuration-0.1 {
		keepIntervals = append(keepIntervals, models.KeepInterval{
			Start: currentPos,
			End:   totalDuration,
		})
	}

	totalKeepSec := totalDuration - totalCutSec
	if totalKeepSec < 0 {
		totalKeepSec = 0
	}

	return &models.Cutlist{
		JobID:             jobID,
		Segments:          segments,
		KeepIntervals:     keepIntervals,
		TotalCutDuration:  totalCutSec,
		TotalKeepDuration: totalKeepSec,
		UpdatedAt:         time.Now(),
	}
}
