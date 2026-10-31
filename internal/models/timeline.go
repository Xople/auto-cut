package models

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// Timeline schema versions. Version 2 adds per-track height/hidden and
// per-clip speed/linked_id on top of the original v1 document. Version 3 adds
// per-clip transitions. Version 4 adds per-clip transform keyframes.
const TimelineVersion = 4

// Timeline guard rails for the editor. They keep a runaway document from
// stalling saves, rendering, and the JSON transport.
const (
	DefaultTrackHeight      = 52
	MinTrackHeight          = 28
	MaxTrackHeight          = 160
	MaxTracks               = 64
	MaxClips                = 5000
	MaxKeyframes            = 128
	MinKeyframeScale        = 0.01
	MaxKeyframeScale        = 10
	DefaultTransitionSecond = 0.6
	MinTransitionSecond     = 0.1
	MaxTransitionSecond     = 3.0
)

// NormalizeTimeline upgrades a decoded document to the current schema version
// and fills in defaults that older projects never stored.
func NormalizeTimeline(t *Timeline) {
	if t.Version < TimelineVersion {
		t.Version = TimelineVersion
	}
	if t.Width <= 0 {
		t.Width = 1920
	}
	if t.Height <= 0 {
		t.Height = 1080
	}
	if t.FPS <= 0 {
		t.FPS = 30
	}
	for i := range t.Tracks {
		track := &t.Tracks[i]
		if track.Kind == "" {
			track.Kind = TrackVideo
		}
		if track.Name == "" {
			track.Name = defaultTrackName(track.Kind)
		}
		if track.Height == 0 {
			track.Height = DefaultTrackHeight
		}
		track.Height = clampInt(track.Height, MinTrackHeight, MaxTrackHeight)
		if track.Clips == nil {
			track.Clips = []Clip{}
		}
		for j := range track.Clips {
			clip := &track.Clips[j]
			if clip.Speed <= 0 || math.IsNaN(clip.Speed) || math.IsInf(clip.Speed, 0) {
				clip.Speed = 1
			}
			if clip.Start < 0 {
				clip.Start = 0
			}
			if clip.Duration < 0 {
				clip.Duration = 0
			}
			if clip.In < 0 {
				clip.In = 0
			}
			if clip.Out < clip.In {
				clip.Out = clip.In + clip.Duration
			}
			if clip.Duration == 0 && clip.Out > clip.In {
				clip.Duration = clip.Out - clip.In
			}
			normalizeTransition(clip)
			normalizeKeyframes(clip)
			if track.Kind == TrackVideo {
				// Video clips saved before the transform feature stored no
				// pose at all, so their zero values must mean "centered".
				if len(clip.Keyframes) == 0 && clip.X == 0 && clip.Y == 0 {
					clip.X = 0.5
					clip.Y = 0.5
				}
				if math.IsNaN(clip.Scale) || math.IsInf(clip.Scale, 0) || clip.Scale <= 0 {
					clip.Scale = 1
				}
				if math.IsNaN(clip.Rotation) || math.IsInf(clip.Rotation, 0) {
					clip.Rotation = 0
				}
			}
		}
	}
}

func normalizeKeyframes(clip *Clip) {
	if len(clip.Keyframes) == 0 {
		clip.Keyframes = nil
		return
	}
	frames := make([]TransformKeyframe, 0, len(clip.Keyframes))
	for _, frame := range clip.Keyframes {
		if math.IsNaN(frame.Time) || math.IsInf(frame.Time, 0) {
			continue
		}
		if math.IsNaN(frame.Scale) || math.IsInf(frame.Scale, 0) || frame.Scale <= 0 {
			frame.Scale = 1
		}
		if math.IsNaN(frame.X) || math.IsInf(frame.X, 0) {
			frame.X = 0.5
		}
		if math.IsNaN(frame.Y) || math.IsInf(frame.Y, 0) {
			frame.Y = 0.5
		}
		if math.IsNaN(frame.Rotation) || math.IsInf(frame.Rotation, 0) {
			frame.Rotation = 0
		}
		frame.Time = clampFloat(frame.Time, 0, math.Max(0, clip.Duration))
		frame.Scale = clampFloat(frame.Scale, MinKeyframeScale, MaxKeyframeScale)
		frames = append(frames, frame)
	}
	sort.SliceStable(frames, func(i, j int) bool { return frames[i].Time < frames[j].Time })
	deduped := frames[:0]
	for _, frame := range frames {
		if n := len(deduped); n > 0 && math.Abs(deduped[n-1].Time-frame.Time) < 0.001 {
			deduped[n-1] = frame
			continue
		}
		deduped = append(deduped, frame)
	}
	if len(deduped) > MaxKeyframes {
		deduped = deduped[:MaxKeyframes]
	}
	if len(deduped) == 0 {
		clip.Keyframes = nil
		return
	}
	clip.Keyframes = deduped
}

func normalizeTransition(clip *Clip) {
	if clip.Transition == nil {
		return
	}
	transition := clip.Transition
	if !validTransitionType(transition.Type) {
		clip.Transition = nil
		return
	}
	if math.IsNaN(transition.Duration) || math.IsInf(transition.Duration, 0) || transition.Duration <= 0 {
		transition.Duration = DefaultTransitionSecond
	}
	transition.Duration = clampFloat(transition.Duration, MinTransitionSecond, MaxTransitionSecond)
	if clip.Duration > 0 && transition.Duration > clip.Duration {
		transition.Duration = clip.Duration
	}
}

// ValidateTimeline rejects documents the editor cannot represent. It runs after
// NormalizeTimeline, so only structural problems reach the caller.
func ValidateTimeline(t Timeline) error {
	if len(t.Tracks) == 0 {
		return errors.New("timeline harus memiliki minimal satu track")
	}
	if len(t.Tracks) > MaxTracks {
		return fmt.Errorf("jumlah track melebihi batas %d", MaxTracks)
	}

	trackIDs := make(map[string]bool, len(t.Tracks))
	clipIDs := make(map[string]bool)
	clipCount := 0

	for i, track := range t.Tracks {
		if track.ID == "" {
			return fmt.Errorf("track #%d tidak memiliki id", i+1)
		}
		if trackIDs[track.ID] {
			return fmt.Errorf("id track ganda: %s", track.ID)
		}
		trackIDs[track.ID] = true
		if !validTrackKind(track.Kind) {
			return fmt.Errorf("track %s memiliki jenis tidak dikenal: %q", track.ID, track.Kind)
		}
		if track.Height < MinTrackHeight || track.Height > MaxTrackHeight {
			return fmt.Errorf("tinggi track %s di luar rentang %d-%d", track.ID, MinTrackHeight, MaxTrackHeight)
		}
		clipCount += len(track.Clips)
		if clipCount > MaxClips {
			return fmt.Errorf("jumlah clip melebihi batas %d", MaxClips)
		}
		for j, clip := range track.Clips {
			if clip.ID == "" {
				return fmt.Errorf("clip #%d pada track %s tidak memiliki id", j+1, track.ID)
			}
			if clipIDs[clip.ID] {
				return fmt.Errorf("id clip ganda: %s", clip.ID)
			}
			clipIDs[clip.ID] = true
			if err := validateClipNumbers(clip); err != nil {
				return fmt.Errorf("clip %s: %w", clip.ID, err)
			}
			if err := validateTransition(clip); err != nil {
				return fmt.Errorf("clip %s: %w", clip.ID, err)
			}
		}
	}

	for _, track := range t.Tracks {
		for _, clip := range track.Clips {
			if clip.LinkedID == "" {
				continue
			}
			if clip.LinkedID == clip.ID {
				return fmt.Errorf("clip %s tidak boleh terhubung ke dirinya sendiri", clip.ID)
			}
			if !clipIDs[clip.LinkedID] {
				return fmt.Errorf("clip %s terhubung ke clip yang tidak ada: %s", clip.ID, clip.LinkedID)
			}
		}
	}
	return nil
}

func validateClipNumbers(clip Clip) error {
	values := []struct {
		name  string
		value float64
	}{
		{"start", clip.Start},
		{"duration", clip.Duration},
		{"in", clip.In},
		{"out", clip.Out},
		{"speed", clip.Speed},
		{"volume", clip.Volume},
		{"opacity", clip.Opacity},
		{"scale", clip.Scale},
		{"x", clip.X},
		{"y", clip.Y},
	}
	for _, v := range values {
		if math.IsNaN(v.value) || math.IsInf(v.value, 0) {
			return fmt.Errorf("nilai %s tidak valid", v.name)
		}
	}
	if clip.Start < 0 {
		return errors.New("waktu mulai negatif")
	}
	if clip.Duration < 0 {
		return errors.New("durasi negatif")
	}
	if clip.In < 0 {
		return errors.New("titik masuk negatif")
	}
	if clip.Out < clip.In {
		return errors.New("titik keluar lebih kecil dari titik masuk")
	}
	if clip.Speed <= 0 {
		return errors.New("kecepatan harus lebih dari nol")
	}
	for i, frame := range clip.Keyframes {
		values := []float64{frame.Time, frame.X, frame.Y, frame.Scale, frame.Rotation}
		for _, value := range values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("keyframe #%d tidak valid", i+1)
			}
		}
		if frame.Scale <= 0 {
			return fmt.Errorf("scale keyframe #%d harus lebih dari nol", i+1)
		}
	}
	return nil
}

func validTrackKind(kind string) bool {
	switch kind {
	case TrackVideo, TrackAudio, TrackOverlay, TrackCaption:
		return true
	}
	return false
}

// ClipOverlap marks two clips that occupy the same time range on one track.
type ClipOverlap struct {
	TrackID  string
	FirstID  string
	SecondID string
}

// TimelineOverlaps reports clips that share space on the same track. Layers on
// different tracks are allowed; clips on one track cannot.
func TimelineOverlaps(t Timeline) []ClipOverlap {
	var overlaps []ClipOverlap
	for _, track := range t.Tracks {
		clips := append([]Clip{}, track.Clips...)
		sort.Slice(clips, func(a, b int) bool {
			if clips[a].Start == clips[b].Start {
				return clips[a].ID < clips[b].ID
			}
			return clips[a].Start < clips[b].Start
		})
		for i := 1; i < len(clips); i++ {
			previousEnd := clips[i-1].Start + clips[i-1].Duration
			if clips[i].Start < previousEnd-1e-4 && clips[i].Duration > 0 && clips[i-1].Duration > 0 {
				overlaps = append(overlaps, ClipOverlap{
					TrackID:  track.ID,
					FirstID:  clips[i-1].ID,
					SecondID: clips[i].ID,
				})
			}
		}
	}
	return overlaps
}

func validateTransition(clip Clip) error {
	if clip.Transition == nil {
		return nil
	}
	transition := clip.Transition
	if !validTransitionType(transition.Type) {
		return fmt.Errorf("jenis transisi tidak dikenal: %q", transition.Type)
	}
	if math.IsNaN(transition.Duration) || math.IsInf(transition.Duration, 0) {
		return errors.New("durasi transisi tidak valid")
	}
	if transition.Duration < MinTransitionSecond || transition.Duration > MaxTransitionSecond {
		return fmt.Errorf("durasi transisi di luar rentang %.1f-%.1f detik", MinTransitionSecond, MaxTransitionSecond)
	}
	if transition.Duration > clip.Duration+1e-4 {
		return errors.New("durasi transisi melebihi durasi clip")
	}
	return nil
}

func validTransitionType(kind string) bool {
	switch kind {
	case TransitionPullIn, TransitionSlideRight, TransitionSlideLeft:
		return true
	}
	return false
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func defaultTrackName(kind string) string {
	switch kind {
	case TrackVideo:
		return "Video"
	case TrackAudio:
		return "Audio"
	case TrackOverlay:
		return "Overlay"
	case TrackCaption:
		return "Caption"
	}
	return "Track"
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
