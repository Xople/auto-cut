package render

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"autocut/internal/fonts"
	"autocut/internal/media"
	"autocut/internal/models"
)

func makeColorVideo(t *testing.T, dir, name, color string, width, height int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	args := []string{
		"-hide_banner", "-y", "-v", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=%s:s=%dx%d:r=30:d=2", color, width, height),
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		path,
	}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("create test video: %v: %s", err, out)
	}
	return path
}

func framePixel(t *testing.T, video, dir string, at float64, x, y int) (uint8, uint8, uint8) {
	t.Helper()
	frame := filepath.Join(dir, "frame.png")
	args := []string{
		"-hide_banner", "-y", "-v", "error",
		"-ss", formatSeconds(at), "-i", video,
		"-frames:v", "1", frame,
	}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("extract frame: %v: %s", err, out)
	}
	file, err := os.Open(frame)
	if err != nil {
		t.Fatalf("open frame: %v", err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	r, g, b, _ := img.At(x, y).RGBA()
	return uint8(r >> 8), uint8(g >> 8), uint8(b >> 8)
}

func renderStackedTimeline(t *testing.T, topHidden bool, topW, topH int, topStart float64) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	output := filepath.Join(dir, "out.mp4")

	bottomPath := makeColorVideo(t, dir, "bottom.mp4", "red", 320, 180)
	topPath := makeColorVideo(t, dir, "top.mp4", "green", topW, topH)
	assets := map[string]*models.Asset{
		"bottom": {ID: "bottom", Kind: models.AssetVideo, Path: bottomPath, Duration: 2},
		"top":    {ID: "top", Kind: models.AssetVideo, Path: topPath, Duration: 2},
	}

	tl := models.NewTimeline(320, 180, 30)
	tl.Tracks = []models.Track{
		{
			ID: "video-top", Kind: models.TrackVideo, Name: "Video 2",
			Height: models.DefaultTrackHeight, Hidden: topHidden,
			Clips: []models.Clip{{ID: "top-clip", AssetID: "top", Start: topStart, Duration: 2 - topStart, In: 0, Out: 2 - topStart, Speed: 1, Opacity: 1, Scale: 1}},
		},
		{
			ID: "video-main", Kind: models.TrackVideo, Name: "Video",
			Height: models.DefaultTrackHeight,
			Clips:  []models.Clip{{ID: "bottom-clip", AssetID: "bottom", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1, Opacity: 1, Scale: 1}},
		},
		{ID: "audio-main", Kind: models.TrackAudio, Name: "Audio", Height: models.DefaultTrackHeight},
	}

	renderer := NewRenderer(media.NewFFmpeg("ffmpeg", false), fonts.NewIndex())
	err := renderer.Render(context.Background(), RenderRequest{
		Timeline:   tl,
		Assets:     assets,
		Settings:   models.ExportSettings{Width: 320, Height: 180, FPS: 30, Encoder: "libx264", Quality: 28, Filename: "out.mp4"},
		OutputPath: output,
		CacheDir:   cache,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("expected output file: %v", err)
	}
	return output, dir
}

func TestRenderUpperVideoTrackCoversBase(t *testing.T) {
	output, dir := renderStackedTimeline(t, false, 320, 180, 0)
	r, g, b := framePixel(t, output, dir, 1, 160, 90)
	if g < 100 || r > 80 || b > 80 {
		t.Fatalf("expected green top layer, got rgb(%d,%d,%d)", r, g, b)
	}
}

func TestRenderHiddenVideoTrackIsSkipped(t *testing.T) {
	output, dir := renderStackedTimeline(t, true, 320, 180, 0)
	r, g, b := framePixel(t, output, dir, 1, 160, 90)
	if r < 150 || g > 80 || b > 80 {
		t.Fatalf("expected red base when top is hidden, got rgb(%d,%d,%d)", r, g, b)
	}
}

func TestRenderUpperLayerKeepsBaseAroundPortrait(t *testing.T) {
	output, dir := renderStackedTimeline(t, false, 90, 180, 0)
	centerR, centerG, centerB := framePixel(t, output, dir, 1, 160, 90)
	if centerG < 100 || centerR > 80 {
		t.Fatalf("expected green portrait layer at center, got rgb(%d,%d,%d)", centerR, centerG, centerB)
	}
	sideR, sideG, sideB := framePixel(t, output, dir, 1, 10, 90)
	if sideR < 150 || sideG > 80 || sideB > 80 {
		t.Fatalf("expected base red outside the portrait layer, got rgb(%d,%d,%d)", sideR, sideG, sideB)
	}
}

func TestRenderUpperLayerRespectsStartTime(t *testing.T) {
	output, dir := renderStackedTimeline(t, false, 320, 180, 1)
	beforeR, beforeG, _ := framePixel(t, output, dir, 0.5, 160, 90)
	if beforeR < 150 || beforeG > 80 {
		t.Fatalf("expected base red before the upper clip starts, got rgb(%d,%d,*)", beforeR, beforeG)
	}
	afterR, afterG, _ := framePixel(t, output, dir, 1.5, 160, 90)
	if afterG < 100 || afterR > 80 {
		t.Fatalf("expected green after the upper clip starts, got rgb(%d,%d,*)", afterR, afterG)
	}
}

func TestRenderSlideTransitionStartsOffscreen(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	output := filepath.Join(dir, "out.mp4")

	bottomPath := makeColorVideo(t, dir, "bottom.mp4", "red", 320, 180)
	topPath := makeColorVideo(t, dir, "top.mp4", "green", 320, 180)
	assets := map[string]*models.Asset{
		"bottom": {ID: "bottom", Kind: models.AssetVideo, Path: bottomPath, Duration: 2},
		"top":    {ID: "top", Kind: models.AssetVideo, Path: topPath, Duration: 2},
	}
	tl := models.NewTimeline(320, 180, 30)
	tl.Tracks = []models.Track{
		{
			ID: "video-top", Kind: models.TrackVideo, Name: "Video 2", Height: models.DefaultTrackHeight,
			Clips: []models.Clip{{
				ID: "top-clip", AssetID: "top", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1, Opacity: 1, Scale: 1,
				Transition: &models.Transition{Type: models.TransitionSlideRight, Duration: 0.6},
			}},
		},
		{
			ID: "video-main", Kind: models.TrackVideo, Name: "Video", Height: models.DefaultTrackHeight,
			Clips: []models.Clip{{ID: "bottom-clip", AssetID: "bottom", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1, Opacity: 1, Scale: 1}},
		},
	}

	renderer := NewRenderer(media.NewFFmpeg("ffmpeg", false), fonts.NewIndex())
	if err := renderer.Render(context.Background(), RenderRequest{
		Timeline:   tl,
		Assets:     assets,
		Settings:   models.ExportSettings{Width: 320, Height: 180, FPS: 30, Encoder: "libx264", Quality: 28},
		OutputPath: output,
		CacheDir:   cache,
	}); err != nil {
		t.Fatalf("render: %v", err)
	}

	leftR, leftG, _ := framePixel(t, output, dir, 0.1, 8, 90)
	if leftR < 150 || leftG > 90 {
		t.Fatalf("expected base red on the left during slide, got rgb(%d,%d,*)", leftR, leftG)
	}
	centerR, centerG, _ := framePixel(t, output, dir, 1.2, 160, 90)
	if centerG < 100 || centerR > 90 {
		t.Fatalf("expected green after the slide finishes, got rgb(%d,%d,*)", centerR, centerG)
	}
}

func TestRenderVideoClipKeepsBuiltInAudio(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not available")
	}
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	output := filepath.Join(dir, "out.mp4")
	source := filepath.Join(dir, "talking.mp4")

	args := []string{
		"-hide_banner", "-y", "-v", "error",
		"-f", "lavfi", "-i", "color=c=red:s=320x180:r=30:d=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-shortest", source,
	}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("create test video with audio: %v: %s", err, out)
	}

	tl := models.NewTimeline(320, 180, 30)
	tl.Tracks = []models.Track{
		{
			ID: "video-main", Kind: models.TrackVideo, Name: "Video", Height: models.DefaultTrackHeight,
			Clips: []models.Clip{{ID: "clip-a", AssetID: "talking", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1, Volume: 1, Opacity: 1, Scale: 1}},
		},
		{ID: "audio-main", Kind: models.TrackAudio, Name: "Audio", Height: models.DefaultTrackHeight},
	}
	assets := map[string]*models.Asset{
		"talking": {ID: "talking", Kind: models.AssetVideo, Path: source, Duration: 2, HasAudio: true},
	}

	renderer := NewRenderer(media.NewFFmpeg("ffmpeg", false), fonts.NewIndex())
	if err := renderer.Render(context.Background(), RenderRequest{
		Timeline:   tl,
		Assets:     assets,
		Settings:   models.ExportSettings{Width: 320, Height: 180, FPS: 30, Encoder: "libx264", Quality: 28},
		OutputPath: output,
		CacheDir:   cache,
	}); err != nil {
		t.Fatalf("render: %v", err)
	}

	probe := exec.Command("ffprobe", "-v", "error", "-select_streams", "a",
		"-show_entries", "stream=codec_type", "-of", "csv=p=0", output)
	out, err := probe.Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	if !strings.Contains(string(out), "audio") {
		t.Fatalf("expected rendered file to carry the video's built-in audio, got %q", string(out))
	}
}

func TestRenderPullInTransitionFadesIn(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	output := filepath.Join(dir, "out.mp4")

	firstPath := makeColorVideo(t, dir, "first.mp4", "red", 320, 180)
	secondPath := makeColorVideo(t, dir, "second.mp4", "green", 320, 180)
	assets := map[string]*models.Asset{
		"first":  {ID: "first", Kind: models.AssetVideo, Path: firstPath, Duration: 2},
		"second": {ID: "second", Kind: models.AssetVideo, Path: secondPath, Duration: 2},
	}
	tl := models.NewTimeline(320, 180, 30)
	tl.Tracks = []models.Track{
		{
			ID: "video-main", Kind: models.TrackVideo, Name: "Video", Height: models.DefaultTrackHeight,
			Clips: []models.Clip{
				{ID: "clip-1", AssetID: "first", Start: 0, Duration: 1, In: 0, Out: 1, Speed: 1, Opacity: 1, Scale: 1},
				{
					ID: "clip-2", AssetID: "second", Start: 1, Duration: 1, In: 0, Out: 1, Speed: 1, Opacity: 1, Scale: 1,
					Transition: &models.Transition{Type: models.TransitionPullIn, Duration: 0.4},
				},
			},
		},
	}

	renderer := NewRenderer(media.NewFFmpeg("ffmpeg", false), fonts.NewIndex())
	if err := renderer.Render(context.Background(), RenderRequest{
		Timeline:   tl,
		Assets:     assets,
		Settings:   models.ExportSettings{Width: 320, Height: 180, FPS: 30, Encoder: "libx264", Quality: 28},
		OutputPath: output,
		CacheDir:   cache,
	}); err != nil {
		t.Fatalf("render: %v", err)
	}

	// Outgoing tail of clip-1 (t=0.92s) must still be solid opaque red (no black dip during pull_in)
	outR, outG, _ := framePixel(t, output, dir, 0.92, 160, 90)
	if outR < 150 || outG > 80 {
		t.Fatalf("expected outgoing pull-in tail to stay solid red, got rgb(%d,%d,*)", outR, outG)
	}
	// Incoming head of clip-2 (t=1.08s) must be solid opaque green
	inR, inG, _ := framePixel(t, output, dir, 1.08, 160, 90)
	if inG < 120 || inR > 90 {
		t.Fatalf("expected incoming pull-in head to be solid green, got rgb(%d,%d,*)", inR, inG)
	}
}

func renderClipTimeline(t *testing.T, clip models.Clip) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	output := filepath.Join(dir, "out.mp4")
	source := makeColorVideo(t, dir, "layer.mp4", "green", 320, 180)
	tl := models.NewTimeline(320, 180, 30)
	tl.Tracks = []models.Track{
		{
			ID: "video-main", Kind: models.TrackVideo, Name: "Video", Height: models.DefaultTrackHeight,
			Clips: []models.Clip{clip},
		},
		{ID: "audio-main", Kind: models.TrackAudio, Name: "Audio", Height: models.DefaultTrackHeight},
	}
	assets := map[string]*models.Asset{
		"layer": {ID: "layer", Kind: models.AssetVideo, Path: source, Duration: 2},
	}
	renderer := NewRenderer(media.NewFFmpeg("ffmpeg", false), fonts.NewIndex())
	if err := renderer.Render(context.Background(), RenderRequest{
		Timeline:   tl,
		Assets:     assets,
		Settings:   models.ExportSettings{Width: 320, Height: 180, FPS: 30, Encoder: "libx264", Quality: 28},
		OutputPath: output,
		CacheDir:   cache,
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	return output, dir
}

func TestRenderStaticTransformMovesAndScalesLayer(t *testing.T) {
	clip := models.Clip{
		ID: "layer", AssetID: "layer", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1,
		Opacity: 1, X: 0.75, Y: 0.5, Scale: 0.5,
	}
	output, dir := renderClipTimeline(t, clip)
	if r, g, _ := framePixel(t, output, dir, 1, 240, 90); g < 100 || r > 90 {
		t.Fatalf("expected green content around the moved center, got rgb(%d,%d,*)", r, g)
	}
	if r, g, _ := framePixel(t, output, dir, 1, 60, 90); r > 90 && g > 90 {
		t.Fatalf("expected black canvas left of the layer, got rgb(%d,%d,*)", r, g)
	}
}

func TestRenderKeyframedPositionInterpolates(t *testing.T) {
	clip := models.Clip{
		ID: "layer", AssetID: "layer", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1,
		Opacity: 1, X: 0.5, Y: 0.5, Scale: 0.5,
		Keyframes: []models.TransformKeyframe{
			{Time: 0, X: 0.25, Y: 0.5, Scale: 0.5},
			{Time: 1.5, X: 0.75, Y: 0.5, Scale: 0.5},
		},
	}
	output, dir := renderClipTimeline(t, clip)
	if r, g, _ := framePixel(t, output, dir, 0.2, 100, 90); g < 100 || r > 90 {
		t.Fatalf("expected green near the left keyframe, got rgb(%d,%d,*)", r, g)
	}
	if r, g, _ := framePixel(t, output, dir, 1.4, 230, 90); g < 100 || r > 90 {
		t.Fatalf("expected green near the right keyframe, got rgb(%d,%d,*)", r, g)
	}
}

func brightPixelsIn(t *testing.T, video, dir, name string, at float64, minX, minY, maxX, maxY int) int {
	t.Helper()
	frame := filepath.Join(dir, name)
	args := []string{
		"-hide_banner", "-y", "-v", "error",
		"-ss", formatSeconds(at), "-i", video,
		"-frames:v", "1", frame,
	}
	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("extract frame: %v: %s", err, out)
	}
	file, err := os.Open(frame)
	if err != nil {
		t.Fatalf("open frame: %v", err)
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	count := 0
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 > 200 && g>>8 > 200 && b>>8 > 200 {
				count++
			}
		}
	}
	return count
}

func TestRenderTextOverlayPositionScaleAndBundledFont(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	output := filepath.Join(dir, "out.mp4")
	base := makeColorVideo(t, dir, "base.mp4", "black", 640, 360)

	tl := models.NewTimeline(640, 360, 30)
	tl.Tracks = []models.Track{
		{
			ID: "overlay-main", Kind: models.TrackOverlay, Name: "Overlay", Height: models.DefaultTrackHeight,
			Clips: []models.Clip{{
				ID: "headline", Text: "AUTOCUT", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1,
				X: 0.25, Y: 0.25, Scale: 1.5, Opacity: 1,
				Style: &models.TextStyle{FontFamily: "Inter", FontSize: 64, Bold: true, Color: "#FFFFFF"},
			}},
		},
		{
			ID: "video-main", Kind: models.TrackVideo, Name: "Video", Height: models.DefaultTrackHeight,
			Clips: []models.Clip{{ID: "base", AssetID: "base", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1, Opacity: 1, Scale: 1}},
		},
		{ID: "audio-main", Kind: models.TrackAudio, Name: "Audio", Height: models.DefaultTrackHeight},
	}
	assets := map[string]*models.Asset{
		"base": {ID: "base", Kind: models.AssetVideo, Path: base, Duration: 2},
	}
	renderer := NewRenderer(media.NewFFmpeg("ffmpeg", false), fonts.NewIndex())
	if err := renderer.Render(context.Background(), RenderRequest{
		Timeline:   tl,
		Assets:     assets,
		Settings:   models.ExportSettings{Width: 640, Height: 360, FPS: 30, Encoder: "libx264", Quality: 28},
		OutputPath: output,
		CacheDir:   cache,
	}); err != nil {
		t.Fatalf("render: %v", err)
	}

	top := brightPixelsIn(t, output, dir, "text-top.png", 1, 0, 0, 320, 180)
	if top < 40 {
		t.Fatalf("expected the scaled text in the top-left quadrant, found %d bright pixels", top)
	}
	bottom := brightPixelsIn(t, output, dir, "text-bottom.png", 1, 0, 200, 640, 360)
	if bottom > 0 {
		t.Fatalf("expected no text at the bottom, found %d bright pixels", bottom)
	}
}

func TestRenderOverlayTrackSitsAboveDefaultVideoTrack(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	output := filepath.Join(dir, "out.mp4")
	base := makeColorVideo(t, dir, "base.mp4", "red", 640, 360)

	// Default track order puts the video track at index 0 and the overlay track
	// at index 1; the overlay must still paint on top of the video.
	tl := models.NewTimeline(640, 360, 30)
	for i := range tl.Tracks {
		switch tl.Tracks[i].ID {
		case "video-main":
			tl.Tracks[i].Clips = []models.Clip{{ID: "base", AssetID: "base", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1, Opacity: 1, Scale: 1}}
		case "overlay":
			tl.Tracks[i].Clips = []models.Clip{{
				ID: "headline", Text: "AUTOCUT", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1,
				X: 0.5, Y: 0.5, Scale: 1, Opacity: 1,
				Style: &models.TextStyle{FontFamily: "Inter", FontSize: 56, Bold: true, Color: "#FFFFFF"},
			}}
		}
	}
	assets := map[string]*models.Asset{
		"base": {ID: "base", Kind: models.AssetVideo, Path: base, Duration: 2},
	}

	renderer := NewRenderer(media.NewFFmpeg("ffmpeg", false), fonts.NewIndex())
	if err := renderer.Render(context.Background(), RenderRequest{
		Timeline:   tl,
		Assets:     assets,
		Settings:   models.ExportSettings{Width: 640, Height: 360, FPS: 30, Encoder: "libx264", Quality: 28},
		OutputPath: output,
		CacheDir:   cache,
	}); err != nil {
		t.Fatalf("render: %v", err)
	}

	center := brightPixelsIn(t, output, dir, "overlay-center.png", 1, 200, 120, 440, 240)
	if center < 40 {
		t.Fatalf("expected the overlay text above the video, found %d bright pixels at the center", center)
	}
	r, g, b := framePixel(t, output, dir, 1, 10, 10)
	if r < 150 || g > 80 || b > 80 {
		t.Fatalf("expected the red base video around the overlay, got rgb(%d,%d,%d)", r, g, b)
	}
}

func TestCaptionFontsDirIncludesBundledFace(t *testing.T) {
	renderer := NewRenderer(media.NewFFmpeg("ffmpeg", false), fonts.NewIndex())
	dir := renderer.captionFontsDir(t.TempDir(), models.CaptionStyle{FontFamily: "Inter", Bold: true})
	if _, err := os.Stat(filepath.Join(dir, "inter-700.ttf")); err != nil {
		t.Fatalf("caption fonts dir is missing the bundled bold face: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "inter-400.ttf")); err != nil {
		t.Fatalf("caption fonts dir is missing the bundled regular face: %v", err)
	}
}

func TestRenderCaptionsUseBundledFont(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	output := filepath.Join(dir, "out.mp4")
	base := makeColorVideo(t, dir, "base.mp4", "black", 640, 360)

	tl := models.NewTimeline(640, 360, 30)
	tl.CaptionStyle = models.CaptionStyle{
		FontFamily: "Inter", FontSize: 72, Bold: true, Color: "#FFFFFF",
		HighlightColor: "#FFD400", Opacity: 1, PositionY: 0.82, WordsPerChunk: 3,
	}
	tl.Tracks = []models.Track{
		{
			ID: "caption-main", Kind: models.TrackCaption, Name: "Caption", Height: models.DefaultTrackHeight,
			Clips: []models.Clip{{ID: "cap", Text: "HALO DUNIA", Start: 0, Duration: 2}},
		},
		{
			ID: "video-main", Kind: models.TrackVideo, Name: "Video", Height: models.DefaultTrackHeight,
			Clips: []models.Clip{{ID: "base", AssetID: "base", Start: 0, Duration: 2, In: 0, Out: 2, Speed: 1, Opacity: 1, Scale: 1}},
		},
		{ID: "audio-main", Kind: models.TrackAudio, Name: "Audio", Height: models.DefaultTrackHeight},
	}
	assets := map[string]*models.Asset{
		"base": {ID: "base", Kind: models.AssetVideo, Path: base, Duration: 2},
	}
	renderer := NewRenderer(media.NewFFmpeg("ffmpeg", false), fonts.NewIndex())
	if err := renderer.Render(context.Background(), RenderRequest{
		Timeline:   tl,
		Assets:     assets,
		Settings:   models.ExportSettings{Width: 640, Height: 360, FPS: 30, Encoder: "libx264", Quality: 28},
		OutputPath: output,
		CacheDir:   cache,
	}); err != nil {
		t.Fatalf("render: %v", err)
	}

	lower := brightPixelsIn(t, output, dir, "caption.png", 1, 0, 260, 640, 360)
	if lower < 40 {
		t.Fatalf("expected caption glyphs near the bottom, found %d bright pixels", lower)
	}
}

func TestRenderVideoBlurBackgroundFillAndCaptionBox(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	dir := t.TempDir()
	cache := filepath.Join(dir, "cache")
	output := filepath.Join(dir, "blur-out.mp4")
	// 16:9 red video placed inside a 9:16 (360x640) project with blur fill
	wide := makeColorVideo(t, dir, "wide.mp4", "red", 640, 360)

	tl := models.NewTimeline(360, 640, 30)
	tl.CaptionStyle = models.CaptionStyle{
		FontFamily:      "Inter",
		FontSize:        56,
		Bold:            true,
		Color:           "#FFFFFF",
		HighlightColor:  "#FFD400",
		Opacity:         1,
		PositionY:       0.82,
		WordsPerChunk:   3,
		BackgroundColor: "#000000",
		BackgroundAlpha: 0.7,
	}
	tl.Tracks = []models.Track{
		{
			ID: "video-main", Kind: models.TrackVideo, Name: "Video", Height: models.DefaultTrackHeight,
			Clips: []models.Clip{
				{
					ID:             "wide",
					AssetID:        "wide",
					Start:          0,
					Duration:       1.5,
					In:             0,
					Out:            1.5,
					Speed:          1,
					Opacity:        1,
					Scale:          1,
					BackgroundFill: "blur",
					BackgroundBlur: 18,
					BackgroundDim:  0.12,
				},
			},
		},
		{
			ID: "caption", Kind: models.TrackCaption, Name: "Caption", Height: models.DefaultTrackHeight,
			Clips: []models.Clip{{ID: "cap1", Text: "AMONG US", Start: 0, Duration: 1.5}},
		},
		{ID: "audio-main", Kind: models.TrackAudio, Name: "Audio", Height: models.DefaultTrackHeight},
	}
	assets := map[string]*models.Asset{
		"wide": {ID: "wide", Kind: models.AssetVideo, Path: wide, Duration: 1.5, Width: 640, Height: 360},
	}
	renderer := NewRenderer(media.NewFFmpeg("ffmpeg", false), fonts.NewIndex())
	if err := renderer.Render(context.Background(), RenderRequest{
		Timeline:   tl,
		Assets:     assets,
		Settings:   models.ExportSettings{Width: 360, Height: 640, FPS: 30, Encoder: "libx264", Quality: 28},
		OutputPath: output,
		CacheDir:   cache,
	}); err != nil {
		t.Fatalf("render blur fill: %v", err)
	}

	// Top letterbox area (y=20) should NOT be black because blur fill fills the 9:16 canvas with dimmed red
	r, _, _ := framePixel(t, output, dir, 0.5, 180, 20)
	if r < 80 {
		t.Fatalf("expected top 9:16 bar to be filled with blurred red background, got r=%d", r)
	}
}
