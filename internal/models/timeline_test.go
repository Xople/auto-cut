package models

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestNewTimelineIsCurrentVersion(t *testing.T) {
	tl := NewTimeline(1080, 1920, 24)
	if tl.Version != TimelineVersion {
		t.Fatalf("expected version %d, got %d", TimelineVersion, tl.Version)
	}
	if len(tl.Tracks) != 5 || tl.Tracks[3].ID != BackgroundMusicTrackID || tl.Tracks[3].Kind != TrackAudio {
		t.Fatalf("missing default music track: %+v", tl.Tracks)
	}
	for _, track := range tl.Tracks {
		if track.Height != DefaultTrackHeight {
			t.Errorf("track %s: expected height %d, got %d", track.ID, DefaultTrackHeight, track.Height)
		}
		if track.Hidden {
			t.Errorf("track %s should be visible by default", track.ID)
		}
	}
}

func TestNormalizeTimelineUpgradesV1(t *testing.T) {
	raw := `{
		"version": 1,
		"width": 1920,
		"height": 1080,
		"fps": 30,
		"tracks": [
			{
				"id": "video-main",
				"kind": "video",
				"name": "Video",
				"clips": [
					{"id": "clip_a", "start": 1, "duration": 2, "in": 4, "out": 0, "volume": 1, "opacity": 1, "scale": 1}
				]
			},
			{
				"id": "audio-main",
				"kind": "audio",
				"name": "Audio"
			}
		]
	}`
	var tl Timeline
	if err := json.Unmarshal([]byte(raw), &tl); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	NormalizeTimeline(&tl)

	if tl.Version != TimelineVersion {
		t.Fatalf("expected version %d, got %d", TimelineVersion, tl.Version)
	}
	video := tl.Tracks[0]
	if video.Height != DefaultTrackHeight {
		t.Errorf("expected default height, got %d", video.Height)
	}
	if video.Hidden {
		t.Error("hidden should default to false")
	}
	if video.Clips[0].Speed != 1 {
		t.Errorf("expected speed 1, got %f", video.Clips[0].Speed)
	}
	if video.Clips[0].Out != 6 {
		t.Errorf("expected out repaired to in+duration (6), got %f", video.Clips[0].Out)
	}
	if tl.Tracks[1].Clips == nil {
		t.Error("nil clip slices should normalize to empty slices")
	}
	if len(tl.Tracks) != 3 || tl.Tracks[2].ID != BackgroundMusicTrackID {
		t.Fatal("legacy timeline did not get a music track")
	}
	tl.Tracks = tl.Tracks[:2]
	NormalizeTimeline(&tl)
	if len(tl.Tracks) != 2 {
		t.Fatal("a removed music track was unexpectedly recreated")
	}
	if err := ValidateTimeline(tl); err != nil {
		t.Fatalf("normalized v1 timeline should be valid: %v", err)
	}
}

func TestValidateTimelineRejectsBrokenDocuments(t *testing.T) {
	base := func() Timeline {
		tl := NewTimeline(1920, 1080, 30)
		tl.Tracks[0].Clips = []Clip{{ID: "c1", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1, Volume: 1, Opacity: 1, Scale: 1}}
		return tl
	}

	t.Run("duplicate clip id", func(t *testing.T) {
		tl := base()
		tl.Tracks[0].Clips = append(tl.Tracks[0].Clips, tl.Tracks[0].Clips[0])
		if err := ValidateTimeline(tl); err == nil {
			t.Fatal("expected duplicate clip id to fail")
		}
	})

	t.Run("unknown track kind", func(t *testing.T) {
		tl := base()
		tl.Tracks[0].Kind = "sticker"
		if err := ValidateTimeline(tl); err == nil {
			t.Fatal("expected unknown kind to fail")
		}
	})

	t.Run("missing link target", func(t *testing.T) {
		tl := base()
		tl.Tracks[0].Clips[0].LinkedID = "ghost"
		if err := ValidateTimeline(tl); err == nil {
			t.Fatal("expected dangling linked_id to fail")
		}
	})

	t.Run("self link", func(t *testing.T) {
		tl := base()
		tl.Tracks[0].Clips[0].LinkedID = "c1"
		if err := ValidateTimeline(tl); err == nil {
			t.Fatal("expected self link to fail")
		}
	})

	t.Run("valid linked pair", func(t *testing.T) {
		tl := base()
		tl.Tracks[2].Clips = []Clip{{ID: "a1", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1, Volume: 1, Opacity: 1, Scale: 1, LinkedID: "c1"}}
		tl.Tracks[0].Clips[0].LinkedID = "a1"
		if err := ValidateTimeline(tl); err != nil {
			t.Fatalf("expected linked pair to pass: %v", err)
		}
	})

	t.Run("too many tracks", func(t *testing.T) {
		tl := base()
		for i := 0; i < MaxTracks; i++ {
			tl.Tracks = append(tl.Tracks, Track{ID: fmt.Sprintf("extra-%d", i), Kind: TrackVideo, Height: DefaultTrackHeight})
		}
		if err := ValidateTimeline(tl); err == nil {
			t.Fatal("expected track limit to fail")
		}
	})
}

func TestValidateTimelineTransitions(t *testing.T) {
	base := func() Timeline {
		tl := NewTimeline(1920, 1080, 30)
		tl.Tracks[0].Clips = []Clip{{ID: "c1", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1, Volume: 1, Opacity: 1, Scale: 1}}
		return tl
	}

	t.Run("known type", func(t *testing.T) {
		tl := base()
		tl.Tracks[0].Clips[0].Transition = &Transition{Type: TransitionSlideLeft, Duration: 0.6}
		if err := ValidateTimeline(tl); err != nil {
			t.Fatalf("expected transition to validate: %v", err)
		}
	})

	t.Run("unknown type", func(t *testing.T) {
		tl := base()
		tl.Tracks[0].Clips[0].Transition = &Transition{Type: "spin", Duration: 0.6}
		if err := ValidateTimeline(tl); err == nil {
			t.Fatal("expected unknown transition type to fail")
		}
	})

	t.Run("longer than clip", func(t *testing.T) {
		tl := base()
		tl.Tracks[0].Clips[0].Transition = &Transition{Type: TransitionPullIn, Duration: 3}
		if err := ValidateTimeline(tl); err == nil {
			t.Fatal("expected transition longer than clip to fail")
		}
	})
}

func TestNormalizeTransitionRepairsValues(t *testing.T) {
	tl := NewTimeline(1920, 1080, 30)
	tl.Tracks[0].Clips = []Clip{
		{ID: "c1", Start: 0, Duration: 1, In: 0, Out: 1, Speed: 1, Transition: &Transition{Type: TransitionSlideRight, Duration: 0}},
		{ID: "c2", Start: 1, Duration: 1, In: 0, Out: 1, Speed: 1, Transition: &Transition{Type: "spin", Duration: 0.5}},
	}
	NormalizeTimeline(&tl)
	if got := tl.Tracks[0].Clips[0].Transition.Duration; got != DefaultTransitionSecond {
		t.Fatalf("expected missing duration to default, got %f", got)
	}
	if tl.Tracks[0].Clips[1].Transition != nil {
		t.Fatal("expected unknown transition to be dropped")
	}
}

func TestTimelineOverlapsFlagsOneTrackOnly(t *testing.T) {
	tl := NewTimeline(1920, 1080, 30)
	tl.Tracks[0].Clips = []Clip{
		{ID: "c1", Start: 0, Duration: 2},
		{ID: "c2", Start: 1, Duration: 2},
	}
	tl.Tracks[1].Clips = []Clip{
		{ID: "o1", Start: 0, Duration: 5},
	}
	if overlaps := TimelineOverlaps(tl); len(overlaps) != 1 {
		t.Fatalf("expected one same-track overlap, got %d", len(overlaps))
	}
	tl.Tracks[0].Clips[1].Start = 2
	if overlaps := TimelineOverlaps(tl); len(overlaps) != 0 {
		t.Fatalf("expected no overlaps once clips are adjacent, got %d", len(overlaps))
	}
}

func TestNormalizeTimelineFillsLegacyVideoTransform(t *testing.T) {
	tl := NewTimeline(1920, 1080, 30)
	tl.Tracks = []Track{{
		ID: "video-main", Kind: TrackVideo, Name: "Video",
		Clips: []Clip{{ID: "clip_a", Start: 0, Duration: 2, Scale: 0}},
	}}
	NormalizeTimeline(&tl)
	clip := tl.Tracks[0].Clips[0]
	if clip.X != 0.5 || clip.Y != 0.5 {
		t.Fatalf("expected legacy clip centered, got x=%v y=%v", clip.X, clip.Y)
	}
	if clip.Scale != 1 {
		t.Fatalf("expected legacy clip scale 1, got %v", clip.Scale)
	}
}

func TestNormalizeTimelinePreservesVideoAtOrigin(t *testing.T) {
	tl := NewTimeline(1080, 1920, 30)
	tl.Tracks[0].Clips = []Clip{{ID: "video", Scale: 1, X: 0, Y: 0, PositionSet: true}}
	NormalizeTimeline(&tl)
	clip := tl.Tracks[0].Clips[0]
	if clip.X != 0 || clip.Y != 0 {
		t.Fatalf("video moved after saving at the top-left corner: (%v, %v)", clip.X, clip.Y)
	}
}
