package project

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"autocut/internal/models"
)

const DefaultSoundsDir = `D:\Videos\TxShd\Sounds`

type TransitionSFXSpec struct {
	Filename string
	In       float64
	Duration float64
	Lead     float64
	Volume   float64
	FadeIn   float64
	FadeOut  float64
}

var VarietyTransitionOrder = []string{
	models.TransitionPullIn,
	models.TransitionBlink,
	models.TransitionSlideUp,
	models.TransitionFlash,
	models.TransitionPullOut,
	models.TransitionSpinIn,
	models.TransitionSlideLeft,
	models.TransitionSlideRight,
}

// TransitionSFXFor returns the primary impact SFX spec from D:\Videos\TxShd\Sounds
// that matches a CapCut transition type.
func TransitionSFXFor(transitionType string) (TransitionSFXSpec, bool) {
	switch transitionType {
	case models.TransitionPullIn:
		return TransitionSFXSpec{
			Filename: "punch.mp3",
			In:       0.0,
			Duration: 0.35,
			Lead:     0.05,
			Volume:   0.54,
			FadeIn:   0.01,
			FadeOut:  0.08,
		}, true
	case models.TransitionPullOut:
		return TransitionSFXSpec{
			Filename: "woosh.mp3",
			In:       0.24,
			Duration: 0.40,
			Lead:     0.15,
			Volume:   0.54,
			FadeIn:   0.02,
			FadeOut:  0.08,
		}, true
	case models.TransitionBlink:
		return TransitionSFXSpec{
			Filename: "camera_zLdd1zp.mp3",
			In:       0.04,
			Duration: 0.45,
			Lead:     0.10,
			Volume:   0.55,
			FadeIn:   0.02,
			FadeOut:  0.08,
		}, true
	case models.TransitionFlash:
		return TransitionSFXSpec{
			Filename: "camera_zLdd1zp.mp3",
			In:       0.0,
			Duration: 0.58,
			Lead:     0.08,
			Volume:   0.58,
			FadeIn:   0.01,
			FadeOut:  0.12,
		}, true
	case models.TransitionSlideUp, models.TransitionSlideDown, models.TransitionSlideLeft, models.TransitionSlideRight:
		return TransitionSFXSpec{
			Filename: "woosh.mp3",
			In:       0.26,
			Duration: 0.36,
			Lead:     0.14,
			Volume:   0.55,
			FadeIn:   0.02,
			FadeOut:  0.08,
		}, true
	case models.TransitionSpinIn:
		return TransitionSFXSpec{
			Filename: "dbz-teleport.mp3",
			In:       0.02,
			Duration: 0.48,
			Lead:     0.10,
			Volume:   0.50,
			FadeIn:   0.02,
			FadeOut:  0.10,
		}, true
	case models.TransitionFade:
		return TransitionSFXSpec{
			Filename: "pop_7e9Is8L.mp3",
			In:       0.0,
			Duration: 0.26,
			Lead:     0.04,
			Volume:   0.52,
			FadeIn:   0.01,
			FadeOut:  0.05,
		}, true
	default:
		return TransitionSFXSpec{}, false
	}
}

func ResolveSceneTransition(mode string, sceneIdx int, dur float64) *models.Transition {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		mode = "variety"
	}
	if mode == "none" {
		return nil
	}
	if dur <= 0 {
		dur = 0.38
	}
	tType := mode
	if mode == "variety" || mode == "auto" || mode == "auto_variety" {
		tType = VarietyTransitionOrder[sceneIdx%len(VarietyTransitionOrder)]
	}
	return &models.Transition{
		Type:     tType,
		Duration: dur,
	}
}

// ApplySceneTransitions assigns transitions to video clips in tl and optionally
// populates the dedicated "audio-sfx" track with matching impact sounds from D:\Videos\TxShd\Sounds.
func (s *Service) ApplySceneTransitions(projectID string, tl *models.Timeline, mode string, dur float64, includeFirst bool, withSFX bool) (int, int, []map[string]any, error) {
	if dur <= 0 {
		dur = 0.38
	}
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "variety"
	}

	updatedClips := 0
	var applied []map[string]any
	for i := range tl.Tracks {
		track := &tl.Tracks[i]
		if track.Kind != models.TrackVideo || track.Locked {
			continue
		}
		sort.SliceStable(track.Clips, func(a, b int) bool { return track.Clips[a].Start < track.Clips[b].Start })
		sceneIdx := 0
		for j := range track.Clips {
			clip := &track.Clips[j]
			if clip.AssetID == "" {
				continue
			}
			if !includeFirst && clip.Start < 0.05 {
				clip.Transition = nil
				continue
			}
			clip.Transition = ResolveSceneTransition(mode, sceneIdx, math.Min(dur, clip.Duration*0.5))
			sceneIdx++
			updatedClips++
			tName := "none"
			sfxFile := ""
			if clip.Transition != nil {
				tName = clip.Transition.Type
				if spec, ok := TransitionSFXFor(tName); ok {
					sfxFile = spec.Filename
				}
			}
			applied = append(applied, map[string]any{
				"clip_id":    clip.ID,
				"start":      clip.Start,
				"transition": tName,
				"sfx_file":   sfxFile,
			})
		}
	}

	sfxCount := 0
	if withSFX {
		n, sfxDetails, err := s.SyncTransitionSFXTrack(projectID, tl, "")
		if err != nil {
			return updatedClips, 0, applied, err
		}
		sfxCount = n
		if len(sfxDetails) > 0 {
			applied = sfxDetails
		}
	}
	return updatedClips, sfxCount, applied, nil
}

// SyncTransitionSFXTrack rebuilds the "audio-sfx" track so every video clip with a
// transition has its matching impact SFX from D:\Videos\TxShd\Sounds placed at clip.Start - Lead.
func (s *Service) SyncTransitionSFXTrack(projectID string, tl *models.Timeline, soundsDir string) (int, []map[string]any, error) {
	if strings.TrimSpace(soundsDir) == "" {
		soundsDir = os.Getenv("TXSHD_SOUNDS_DIR")
	}
	if strings.TrimSpace(soundsDir) == "" {
		soundsDir = DefaultSoundsDir
	}

	type pendingCue struct {
		clipID  string
		cutTime float64
		tType   string
		spec    TransitionSFXSpec
	}
	var cues []pendingCue
	for _, track := range tl.Tracks {
		if track.Kind != models.TrackVideo || track.Hidden {
			continue
		}
		for _, clip := range track.Clips {
			if clip.Transition == nil || clip.Transition.Type == "" {
				continue
			}
			spec, ok := TransitionSFXFor(clip.Transition.Type)
			if !ok {
				continue
			}
			cues = append(cues, pendingCue{
				clipID:  clip.ID,
				cutTime: clip.Start,
				tType:   clip.Transition.Type,
				spec:    spec,
			})
		}
	}
	sort.SliceStable(cues, func(a, b int) bool { return cues[a].cutTime < cues[b].cutTime })

	// Remove existing audio-sfx track if no cues remain
	if len(cues) == 0 {
		kept := tl.Tracks[:0]
		for _, track := range tl.Tracks {
			if track.ID == "audio-sfx" {
				continue
			}
			kept = append(kept, track)
		}
		tl.Tracks = kept
		return 0, nil, nil
	}

	existingAssets, err := s.Assets(projectID)
	if err != nil {
		return 0, nil, err
	}
	assetByName := map[string]*models.Asset{}
	for _, a := range existingAssets {
		assetByName[strings.ToLower(a.Filename)] = a
	}

	ensureSoundAsset := func(filename string) (*models.Asset, error) {
		key := strings.ToLower(filename)
		if a, ok := assetByName[key]; ok {
			return a, nil
		}
		fullPath := filepath.Join(soundsDir, filename)
		if _, err := os.Stat(fullPath); err != nil {
			return nil, err
		}
		a, err := s.ImportPath(projectID, fullPath)
		if err != nil {
			return nil, err
		}
		assetByName[key] = a
		return a, nil
	}

	var sfxClips []models.Clip
	var applied []map[string]any
	lastEnd := -1.0
	for idx, cue := range cues {
		asset, err := ensureSoundAsset(cue.spec.Filename)
		if err != nil {
			// Skip gracefully if sound file does not exist on disk
			applied = append(applied, map[string]any{
				"clip_id":    cue.clipID,
				"start":      cue.cutTime,
				"transition": cue.tType,
				"sfx_file":   cue.spec.Filename,
				"sfx_placed": false,
			})
			continue
		}
		start := math.Max(0, cue.cutTime-cue.spec.Lead)
		if start < lastEnd+0.02 {
			start = lastEnd + 0.02
		}
		dur := cue.spec.Duration
		in := cue.spec.In
		out := in + dur
		if asset.Duration > 0 && out > asset.Duration {
			out = asset.Duration
			dur = out - in
		}
		if dur <= 0.04 {
			continue
		}
		sfxClip := models.Clip{
			ID:       fmt.Sprintf("clip_sfx_%02d_%s", idx+1, cue.tType),
			AssetID:  asset.ID,
			Start:    math.Round(start*1000) / 1000,
			Duration: math.Round(dur*1000) / 1000,
			In:       math.Round(in*1000) / 1000,
			Out:      math.Round(out*1000) / 1000,
			Speed:    1,
			Scale:    1,
			Opacity:  1,
			Volume:   cue.spec.Volume,
			FadeIn:   cue.spec.FadeIn,
			FadeOut:  cue.spec.FadeOut,
		}
		sfxClips = append(sfxClips, sfxClip)
		applied = append(applied, map[string]any{
			"clip_id":      cue.clipID,
			"start":        cue.cutTime,
			"transition":   cue.tType,
			"sfx_clip_id":  sfxClip.ID,
			"sfx_asset_id": asset.ID,
			"sfx_file":     cue.spec.Filename,
			"sfx_start":    sfxClip.Start,
			"sfx_duration": sfxClip.Duration,
			"sfx_placed":   true,
		})
		lastEnd = start + dur
	}

	foundTrack := false
	for i := range tl.Tracks {
		if tl.Tracks[i].ID == "audio-sfx" {
			tl.Tracks[i].Clips = sfxClips
			foundTrack = true
			break
		}
	}
	if !foundTrack && len(sfxClips) > 0 {
		tl.Tracks = append(tl.Tracks, models.Track{
			ID:     "audio-sfx",
			Kind:   models.TrackAudio,
			Name:   "Transition SFX",
			Height: models.DefaultTrackHeight,
			Clips:  sfxClips,
		})
	}
	return len(sfxClips), applied, nil
}
