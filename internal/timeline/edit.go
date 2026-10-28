// Package timeline holds pure timeline edits shared by the API handlers.
package timeline

import (
	"fmt"
	"math"
	"sort"

	"autocut/internal/models"
)

const epsilon = 0.001

// ApplyCuts removes cut ranges (expressed in asset time) from the given clips,
// compacts the remaining source segments back to back, and optionally ripples
// later clips on the same track. Returns the removed duration per track.
func ApplyCuts(tl *models.Timeline, clipIDs []string, cuts []models.CutSegment, ripple bool) float64 {
	targets := make(map[string]bool, len(clipIDs))
	for _, id := range clipIDs {
		targets[id] = true
	}
	if len(targets) == 0 {
		return 0
	}

	var totalRemoved float64
	for i := range tl.Tracks {
		track := &tl.Tracks[i]
		if track.Kind == models.TrackCaption {
			continue
		}
		var rebuilt []models.Clip
		var removed float64
		for _, clip := range track.Clips {
			if !targets[clip.ID] || clip.Duration <= 0 {
				rebuilt = append(rebuilt, clip)
				continue
			}
			keeps := keepIntervals(clip.In, clip.Out, cuts)
			if len(keeps) == 0 {
				removed += clip.Duration
				continue
			}
			dropped := clip.Duration
			start := clip.Start
			for k, interval := range keeps {
				duration := interval[1] - interval[0]
				dropped -= duration
				piece := clip
				piece.ID = fmt.Sprintf("%s_k%d", clip.ID, k)
				piece.Start = start
				piece.In = interval[0]
				piece.Out = interval[1]
				piece.Duration = duration
				if k == 0 {
					piece.ID = clip.ID
				}
				rebuilt = append(rebuilt, piece)
				start += duration
			}
			removed += math.Max(0, dropped)
		}

		if ripple && removed > 0 {
			cutEnd := math.Inf(-1)
			for _, clip := range track.Clips {
				if targets[clip.ID] {
					cutEnd = math.Max(cutEnd, clip.End())
				}
			}
			for j := range rebuilt {
				if rebuilt[j].Start >= cutEnd-epsilon && !targets[rebuilt[j].ID] {
					rebuilt[j].Start = math.Max(0, rebuilt[j].Start-removed)
				}
			}
		}

		sortClips(rebuilt)
		track.Clips = rebuilt
		totalRemoved += removed
	}
	return totalRemoved
}

// SplitClipAt divides a clip at a timeline position. Returns the new clip IDs.
func SplitClipAt(tl *models.Timeline, clipID string, at float64, newID string) (string, string, bool) {
	for i := range tl.Tracks {
		track := &tl.Tracks[i]
		for j := range track.Clips {
			clip := track.Clips[j]
			if clip.ID != clipID {
				continue
			}
			if at <= clip.Start+epsilon || at >= clip.End()-epsilon {
				return "", "", false
			}
			offset := at - clip.Start
			left := clip
			left.Duration = offset
			left.Out = clip.In + offset

			right := clip
			right.ID = newID
			right.Start = at
			right.In = clip.In + offset
			right.Duration = clip.Duration - offset
			right.Words = nil

			track.Clips[j] = left
			track.Clips = append(track.Clips, right)
			sortClips(track.Clips)
			return left.ID, right.ID, true
		}
	}
	return "", "", false
}

func keepIntervals(in, out float64, cuts []models.CutSegment) [][2]float64 {
	if out <= in {
		return nil
	}
	merged := mergeCuts(cuts, in, out)
	if len(merged) == 0 {
		return [][2]float64{{in, out}}
	}
	var keeps [][2]float64
	cursor := in
	for _, cut := range merged {
		if cut[0] > cursor+epsilon {
			keeps = append(keeps, [2]float64{cursor, cut[0]})
		}
		if cut[1] > cursor {
			cursor = cut[1]
		}
	}
	if cursor < out-epsilon {
		keeps = append(keeps, [2]float64{cursor, out})
	}
	return keeps
}

func mergeCuts(cuts []models.CutSegment, in, out float64) [][2]float64 {
	var ranges [][2]float64
	for _, cut := range cuts {
		start := math.Max(cut.Start, in)
		end := math.Min(cut.End, out)
		if end-start <= epsilon {
			continue
		}
		ranges = append(ranges, [2]float64{start, end})
	}
	if len(ranges) == 0 {
		return nil
	}
	sort.Slice(ranges, func(a, b int) bool { return ranges[a][0] < ranges[b][0] })
	merged := [][2]float64{ranges[0]}
	for _, r := range ranges[1:] {
		last := &merged[len(merged)-1]
		if r[0] <= last[1]+epsilon {
			if r[1] > last[1] {
				last[1] = r[1]
			}
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

func sortClips(clips []models.Clip) {
	sort.SliceStable(clips, func(a, b int) bool { return clips[a].Start < clips[b].Start })
}
