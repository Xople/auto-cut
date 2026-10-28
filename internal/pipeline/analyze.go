package pipeline

import (
	"fmt"
	"os"
	"path/filepath"

	"autocut/internal/ai/deepseek"
	"autocut/internal/models"
)

// AnalyzeOptions tunes the voice-over analysis used by the editor's auto cut.
type AnalyzeOptions struct {
	TempDir    string
	UseAI      bool
	UseQA      bool
	OnProgress func(progress float64, message string)
}

// AnalyzeVO runs silence detection, local speech-to-text and (optionally) the
// AI filler/retake pass over a voice-over file. It returns cut segments in the
// voice-over's own timeline; the editor maps them onto the video track.
func (o *Orchestrator) AnalyzeVO(voPath string, opts AnalyzeOptions) (*models.AnalysisResult, error) {
	if voPath == "" {
		return nil, fmt.Errorf("voice-over tidak ditemukan")
	}
	tempDir := opts.TempDir
	if tempDir == "" {
		tempDir = filepath.Dir(voPath)
	}
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, err
	}
	progress := func(pct float64, msg string) {
		if opts.OnProgress != nil {
			opts.OnProgress(pct, msg)
		}
	}

	analysisWav := filepath.Join(tempDir, "vo_analysis.wav")
	progress(0.05, "Menyiapkan audio voice-over...")
	if err := o.ffmpeg.ExtractAudio(voPath, analysisWav); err != nil {
		return nil, fmt.Errorf("gagal mengekstrak audio: %w", err)
	}

	duration, err := o.ffmpeg.GetDuration(voPath)
	if err != nil || duration <= 0 {
		duration, err = o.ffmpeg.GetDuration(analysisWav)
	}
	if err != nil || duration <= 0 {
		return nil, fmt.Errorf("durasi voice-over tidak terbaca")
	}

	progress(0.15, "Mendeteksi jeda hening...")
	silenceCuts, err := o.ffmpeg.DetectSilence(analysisWav, o.cfg.SilenceNoiseThreshold, o.cfg.MinSilenceDuration)
	if err != nil {
		return nil, fmt.Errorf("gagal mendeteksi silence: %w", err)
	}

	progress(0.3, "Transkripsi lokal (whisper.cpp)...")
	transcript, err := o.whisper.Transcribe(analysisWav, duration)
	if err != nil {
		return nil, fmt.Errorf("gagal transkripsi: %w", err)
	}
	transcript.Duration = duration

	var aiCuts []models.CutSegment
	if opts.UseAI && !transcript.Mock && len(transcript.Words) > 0 {
		progress(0.6, "Menganalisis filler dan retake...")
		proposals, err := o.deepseek.AnalyzeTranscriptForCuts(transcript.Words)
		if err == nil {
			aiCuts = o.proposalsToCuts(proposals, transcript)
		}
	}

	cuts := append(silenceCuts, aiCuts...)
	progress(0.85, "Menyusun cutlist...")
	cutlist := BuildCutlist("", cuts, duration, o.cfg.SafetyPaddingMs)

	if opts.UseQA {
		progress(0.92, "QA pass cutlist...")
		passed, report, qaErr := o.gemini.SanityCheckCutlist(transcript.FullText, duration, cutlist.TotalCutDuration, cuts)
		if qaErr == nil {
			cutlist.QASanityPassed = passed
			cutlist.QAReport = report
		}
	}

	progress(1, "Analisis selesai.")
	return &models.AnalysisResult{
		Transcript: transcript,
		Cuts:       cuts,
		TotalCuts:  cutlist.TotalCutDuration,
	}, nil
}

func (o *Orchestrator) proposalsToCuts(proposals []deepseek.MacroCutProposal, transcript *models.Transcript) []models.CutSegment {
	var cuts []models.CutSegment
	for _, prop := range proposals {
		if prop.StartIndex < 0 || prop.EndIndex >= len(transcript.Words) || prop.StartIndex > prop.EndIndex {
			continue
		}
		startSec := transcript.Words[prop.StartIndex].Start
		endSec := transcript.Words[prop.EndIndex].End
		if endSec <= startSec {
			continue
		}

		preceding := ""
		if prop.StartIndex > 0 {
			preceding = transcript.Words[prop.StartIndex-1].Word
		}
		following := ""
		if prop.EndIndex < len(transcript.Words)-1 {
			following = transcript.Words[prop.EndIndex+1].Word
		}

		reason := prop.Reason
		confidence := 0.85
		needsReview := false
		if o.typesafe != nil && o.cfg.TypeSafeAPIKey != "" {
			isFluent, prob, _ := o.typesafe.EvaluateBoundaryContinuity(preceding, prop.ExcerptText, following)
			classified, conf, review, _ := o.typesafe.ClassifySegment(prop.ExcerptText, transcript.FullText)
			if classified == models.ReasonKeep {
				continue
			}
			reason = classified
			confidence = conf * prob
			needsReview = review || !isFluent
		}

		cuts = append(cuts, models.CutSegment{
			ID:             fmt.Sprintf("ai_cut_%d_%d", prop.StartIndex, prop.EndIndex),
			Start:          startSec,
			End:            endSec,
			Duration:       endSec - startSec,
			Reason:         reason,
			Source:         "deepseek+typesafe",
			Text:           prop.ExcerptText,
			Confidence:     confidence,
			NeedsReview:    needsReview,
			UserOverridden: false,
			IsCut:          true,
		})
	}
	return cuts
}
