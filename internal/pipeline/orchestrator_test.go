package pipeline

import (
	"testing"

	"autocut/internal/models"
)

func TestBuildCutlist(t *testing.T) {
	// Total video 10 seconds
	// Cut 1: 2.0 to 4.0 (silence)
	// Cut 2: 3.5 to 5.0 (filler - overlapping with Cut 1!)
	// Cut 3: 7.0 to 8.0 (retake)
	cuts := []models.CutSegment{
		{
			ID:       "1",
			Start:    2.0,
			End:      4.0,
			Duration: 2.0,
			Reason:   models.ReasonSilence,
			IsCut:    true,
		},
		{
			ID:       "2",
			Start:    3.5,
			End:      5.0,
			Duration: 1.5,
			Reason:   models.ReasonFiller,
			IsCut:    true,
		},
		{
			ID:       "3",
			Start:    7.0,
			End:      8.0,
			Duration: 1.0,
			Reason:   models.ReasonRetake,
			IsCut:    true,
		},
		{
			ID:       "4",
			Start:    9.0,
			End:      9.5,
			Duration: 0.5,
			Reason:   models.ReasonKeep,
			IsCut:    false, // user overridden to KEEP
		},
	}

	totalDur := 10.0
	paddingMs := 50.0 // 0.05s

	cutlist := BuildCutlist("job_test", cuts, totalDur, paddingMs)

	// Cuts 1 and 2 should merge into [2.0, 5.0] (dur = 3.0)
	// Cut 3 is [7.0, 8.0] (dur = 1.0)
	// Total cut duration should be around 4.0s
	if cutlist.TotalCutDuration < 3.99 || cutlist.TotalCutDuration > 4.01 {
		t.Errorf("expected total cut ~4.0, got %.2f", cutlist.TotalCutDuration)
	}

	// Speech keep intervals should be:
	// Interval 0: [0, 2.0 - 0.05] = [0, 1.95]
	// Interval 1: [5.0 + 0.05, 7.0 - 0.05] = [5.05, 6.95]
	// Interval 2: [8.0 + 0.05, 10.0] = [8.05, 10.0]
	if len(cutlist.KeepIntervals) != 3 {
		t.Fatalf("expected 3 keep intervals, got %d", len(cutlist.KeepIntervals))
	}

	int0 := cutlist.KeepIntervals[0]
	if int0.Start != 0.0 || int0.End < 1.94 || int0.End > 1.96 {
		t.Errorf("unexpected keep interval 0: %+v", int0)
	}
}

func TestBuildCutlistKeepsPictureAfterVO(t *testing.T) {
	cuts := []models.CutSegment{{
		Start:    1,
		End:      2,
		Duration: 1,
		IsCut:    true,
	}}
	cutlist := BuildCutlist("job_video", cuts, 100, 0)
	if len(cutlist.KeepIntervals) != 2 {
		t.Fatalf("expected speech plus the video tail, got %d", len(cutlist.KeepIntervals))
	}
	tail := cutlist.KeepIntervals[1]
	if tail.Start != 2 || tail.End != 100 {
		t.Fatalf("video tail was collapsed: %+v", tail)
	}
}

func TestCutTimelineUsesVideo(t *testing.T) {
	job := &models.Job{MediaType: "video", DurationOriginal: 48, VideoDuration: 763}
	if got := CutTimeline(job); got != 763 {
		t.Fatalf("video timeline = %v", got)
	}
	audio := &models.Job{MediaType: "audio", DurationOriginal: 48, VideoDuration: 0}
	if got := CutTimeline(audio); got != 48 {
		t.Fatalf("audio timeline = %v", got)
	}
}

func TestAutoCutBlockReasonRequiresVOForVideo(t *testing.T) {
	video := &models.Job{MediaType: "video", Status: models.StatusAwaitingVo}
	if AutoCutBlockReason(video) == "" {
		t.Fatal("video without a VO must be rejected")
	}

	video.HasVo = true
	video.VoPath = "vo.wav"
	video.RawVideoPath = "raw.mp4"
	video.Status = models.StatusPending
	if reason := AutoCutBlockReason(video); reason != "" {
		t.Fatalf("video with a VO should be allowed, got %q", reason)
	}

	audio := &models.Job{MediaType: "audio", HasVo: true, VoPath: "take.wav", Status: models.StatusPending}
	if reason := AutoCutBlockReason(audio); reason != "" {
		t.Fatalf("audio-only job should be allowed, got %q", reason)
	}

	audio.Status = models.StatusCutting
	if AutoCutBlockReason(audio) == "" {
		t.Fatal("a running job must be rejected")
	}
}
