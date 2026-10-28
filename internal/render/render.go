// Package render turns an editor timeline document into a final video using
// a single ffmpeg filter graph.
package render

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"autocut/internal/fonts"
	"autocut/internal/media"
	"autocut/internal/models"
)

type Renderer struct {
	ffmpeg *media.FFmpeg
	fonts  *fonts.Index
}

func NewRenderer(ffmpeg *media.FFmpeg, fontIndex *fonts.Index) *Renderer {
	return &Renderer{ffmpeg: ffmpeg, fonts: fontIndex}
}

type RenderRequest struct {
	Timeline   models.Timeline
	Assets     map[string]*models.Asset
	Settings   models.ExportSettings
	OutputPath string
	CacheDir   string
	OnProgress func(progress float64, message string)
}

// Render builds and executes the ffmpeg graph for a timeline.
func (r *Renderer) Render(ctx context.Context, req RenderRequest) error {
	tl := req.Timeline
	width, height := tl.Width, tl.Height
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}
	fps := tl.FPS
	if fps <= 0 {
		fps = 30
	}
	total := models.TimelineDuration(tl)
	if total <= 0.05 {
		return fmt.Errorf("timeline masih kosong, tambahkan clip dulu")
	}

	outW, outH := req.Settings.Width, req.Settings.Height
	if outW <= 0 || outH <= 0 {
		outW, outH = width, height
	}
	outW -= outW % 2
	outH -= outH % 2
	if outW < 16 || outH < 16 {
		return fmt.Errorf("resolusi export tidak valid")
	}

	cacheDir := req.CacheDir
	if cacheDir == "" {
		cacheDir = filepath.Dir(req.OutputPath)
	}
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(req.OutputPath), 0755); err != nil {
		return err
	}

	report := func(progress float64, message string) {
		if req.OnProgress != nil {
			req.OnProgress(progress, message)
		}
	}

	cleanupOldOverlayCache(cacheDir)
	report(0.01, "Menyiapkan render...")

	overlayClips := collectClips(tl, models.TrackOverlay)
	overlayPaths := map[string]string{}
	for i, clip := range overlayClips {
		asset := req.Assets[clip.AssetID]
		path, err := r.renderOverlay(ctx, clip, asset, width, height, cacheDir, i)
		if err != nil {
			return fmt.Errorf("overlay %s gagal disiapkan: %w", clip.ID, err)
		}
		overlayPaths[clip.ID] = path
		report(0.02+0.10*float64(i+1)/float64(len(overlayClips)), "Menyiapkan overlay...")
	}

	b := newGraphBuilder()
	if _, err := buildVideo(b, tl, req.Assets, width, height, fps, total); err != nil {
		return err
	}
	audioOut, err := buildAudio(b, tl, req.Assets, total)
	if err != nil {
		return err
	}
	composited, err := buildOverlays(b, tl, overlayPaths, fps, total)
	if err != nil {
		return err
	}

	captionPath, hasCaptions, err := buildCaptions(tl, cacheDir, width, height)
	if err != nil {
		return err
	}

	finalVideo := composited
	if hasCaptions {
		b.add("%sass=filename='%s':fontsdir='%s'[vcap]", composited, escapeFilterValue(captionPath), escapeFilterValue(windowsFontsDir()))
		finalVideo = "[vcap]"
	}
	if outW != width || outH != height {
		b.add("%sscale=%d:%d:flags=lanczos[vfinal]", finalVideo, outW, outH)
		finalVideo = "[vfinal]"
	}
	if !hasCaptions && outW == width && outH == height {
		b.add("%snull[vfinal]", finalVideo)
		finalVideo = "[vfinal]"
	}

	scriptPath := filepath.Join(cacheDir, "filter_graph.txt")
	if err := os.WriteFile(scriptPath, []byte(strings.Join(b.lines, ";\n")), 0644); err != nil {
		return err
	}

	quality := req.Settings.Quality
	if quality <= 0 {
		quality = 20
	}
	requested := strings.TrimSpace(req.Settings.Encoder)
	attempts := []string{}
	switch requested {
	case "", "auto":
		attempts = []string{"h264_amf", "libx264"}
	case "libx264":
		attempts = []string{"libx264"}
	default:
		attempts = []string{requested, "libx264"}
	}

	baseArgs := []string{"-hide_banner", "-y", "-v", "error", "-nostats", "-progress", "pipe:1"}
	baseArgs = append(baseArgs, b.inputArgs()...)
	baseArgs = append(baseArgs,
		"-filter_complex_script", scriptPath,
		"-map", finalVideo,
		"-map", audioOut,
		"-t", formatSeconds(total),
		"-r", formatSeconds(fps),
		"-c:a", "aac", "-b:a", "192k", "-ar", "48000",
		"-movflags", "+faststart",
	)

	var lastErr error
	for i, encoder := range attempts {
		if i > 0 {
			report(0.13, fmt.Sprintf("Encoder %s tidak tersedia, mencoba %s...", attempts[i-1], encoder))
		}
		args := append([]string{}, baseArgs...)
		args = append(args, encoderArgs(encoder, quality)...)
		args = append(args, req.OutputPath)

		report(0.13, "Merender timeline...")
		stderr, err := runFFmpeg(ctx, r.ffmpeg.BinaryPath(), args, total, report)
		if err == nil {
			report(1, "Render selesai.")
			return nil
		}
		lastErr = fmt.Errorf("encoder %s: %w (%s)", encoder, err, strings.TrimSpace(stderr))
	}

	return fmt.Errorf("render gagal: %w", lastErr)
}

func (r *Renderer) renderOverlay(ctx context.Context, clip models.Clip, asset *models.Asset, width, height int, cacheDir string, index int) (string, error) {
	out := filepath.Join(cacheDir, fmt.Sprintf("overlay_%03d_%s.png", index, safeFileID(clip.ID)))
	if strings.TrimSpace(clip.Text) != "" {
		return out, r.renderTextOverlay(ctx, clip, out, width, height, cacheDir, index)
	}
	if asset == nil {
		return "", fmt.Errorf("asset tidak ditemukan")
	}
	return out, r.renderImageOverlay(ctx, clip, asset, out, width, height)
}

func (r *Renderer) renderImageOverlay(ctx context.Context, clip models.Clip, asset *models.Asset, outPath string, width, height int) error {
	aw, ah := asset.Width, asset.Height
	if aw <= 0 || ah <= 0 {
		aw, ah = width, height
	}
	fit := math.Min(float64(width)/float64(aw), float64(height)/float64(ah))
	scale := clip.Scale
	if scale <= 0 {
		scale = 1
	}
	objW := math.Max(2, math.Round(float64(aw)*fit*scale))
	objH := math.Max(2, math.Round(float64(ah)*fit*scale))

	angle := clip.Rotation * math.Pi / 180
	rotW, rotH := rotatedSize(objW, objH, angle)
	if rotW > float64(width) || rotH > float64(height) {
		factor := math.Min(float64(width)/rotW, float64(height)/rotH)
		objW = math.Max(2, math.Round(objW*factor))
		objH = math.Max(2, math.Round(objH*factor))
		rotW, rotH = rotatedSize(objW, objH, angle)
	}
	opacity := clamp01(clip.Opacity)
	x := clip.X*float64(width) - rotW/2
	y := clip.Y*float64(height) - rotH/2

	chain := fmt.Sprintf("[0:v]scale=%d:%d,format=rgba,colorchannelmixer=aa=%.3f", int(objW), int(objH), opacity)
	if math.Abs(angle) > 0.0001 {
		chain += fmt.Sprintf(",rotate=%.6f:ow=rotw(%.6f):oh=roth(%.6f):c=black@0", angle, angle, angle)
	}
	chain += "[obj]"
	filter := chain + fmt.Sprintf(";[1:v][obj]overlay=x=%.2f:y=%.2f:format=auto,format=rgba[out]", x, y)

	args := []string{"-hide_banner", "-y", "-v", "error",
		"-i", asset.Path,
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=black@0:s=%dx%d:d=1,format=rgba", width, height),
		"-filter_complex", filter,
		"-map", "[out]",
		"-frames:v", "1",
		outPath,
	}
	return runSimple(ctx, r.ffmpeg.BinaryPath(), args)
}

func (r *Renderer) renderTextOverlay(ctx context.Context, clip models.Clip, outPath string, width, height int, cacheDir string, index int) error {
	style := clip.Style
	if style == nil {
		style = &models.TextStyle{FontFamily: "Arial", FontSize: 72, Color: "#FFFFFF", StrokeWidth: 4, StrokeColor: "#000000", Shadow: 2}
	}
	scale := clip.Scale
	if scale <= 0 {
		scale = 1
	}
	fontPx := float64(style.FontSize)
	if fontPx <= 0 {
		fontPx = 72
	}
	fontPx = fontPx * (float64(height) / 1080.0) * scale
	if fontPx < 8 {
		fontPx = 8
	}

	text := clip.Text
	if style.Uppercase {
		text = strings.ToUpper(text)
	}
	wrapped := wrapText(text, fontPx, float64(width)*0.86)
	textPath := filepath.Join(cacheDir, fmt.Sprintf("text_%03d_%s.txt", index, safeFileID(clip.ID)))
	if err := os.WriteFile(textPath, []byte(wrapped), 0644); err != nil {
		return err
	}

	maxLine := 0.0
	for _, line := range strings.Split(wrapped, "\n") {
		if w := estimateTextWidth(line, fontPx); w > maxLine {
			maxLine = w
		}
	}
	pad := math.Max(8, fontPx*0.4)
	objW := math.Min(float64(width)*1.2, maxLine*1.15+pad*2)
	objH := float64(len(strings.Split(wrapped, "\n")))*fontPx*1.25 + pad*2
	if objW < 4 {
		objW = 4
	}
	if objH < 4 {
		objH = 4
	}

	fontFile := style.FontFile
	if fontFile == "" {
		fontFile = r.fonts.ResolveFace(style.FontFamily, style.Bold, style.Italic)
	}
	if fontFile == "" {
		return fmt.Errorf("font untuk overlay teks tidak ditemukan")
	}

	color := fmt.Sprintf("0x%s@%.3f", hexDigits(style.Color, "FFFFFF"), clamp01(clip.Opacity))
	draw := fmt.Sprintf("drawtext=fontfile='%s':textfile='%s':expansion=none:fontsize=%d:fontcolor=%s",
		escapeFilterValue(fontFile), escapeFilterValue(textPath), int(math.Round(fontPx)), color)
	draw += fmt.Sprintf(":line_spacing=%d:x=(w-text_w)/2:y=(h-text_h)/2", int(math.Round(fontPx*0.18)))
	strokePx := style.StrokeWidth * (float64(height) / 1080.0) * scale
	if strokePx > 0.1 {
		draw += fmt.Sprintf(":borderw=%d:bordercolor=0x%s@1", int(math.Round(strokePx)), hexDigits(style.StrokeColor, "000000"))
	}
	shadowPx := style.Shadow * (float64(height) / 1080.0) * scale
	if shadowPx > 0.1 {
		draw += fmt.Sprintf(":shadowx=0:shadowy=%d:shadowcolor=0x%s@0.7", int(math.Round(shadowPx)), hexDigits(style.ShadowColor, "000000"))
	}
	if style.BackgroundAlpha > 0 {
		draw += fmt.Sprintf(":box=1:boxcolor=0x%s@%.3f:boxborderw=%d",
			hexDigits(style.BackgroundColor, "000000"), clamp01(style.BackgroundAlpha), int(math.Round(pad/2)))
	}

	angle := clip.Rotation * math.Pi / 180
	rotW, rotH := rotatedSize(objW, objH, angle)
	x := clip.X*float64(width) - rotW/2
	y := clip.Y*float64(height) - rotH/2

	objFilter := draw
	if math.Abs(angle) > 0.0001 {
		objFilter += fmt.Sprintf(",rotate=%.6f:ow=rotw(%.6f):oh=roth(%.6f):c=black@0", angle, angle, angle)
	}
	filter := fmt.Sprintf("[0:v]%s,format=rgba[obj];[1:v][obj]overlay=x=%.2f:y=%.2f:format=auto,format=rgba[out]", objFilter, x, y)

	args := []string{"-hide_banner", "-y", "-v", "error",
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=black@0:s=%dx%d:d=1,format=rgba", int(objW), int(objH)),
		"-f", "lavfi", "-i", fmt.Sprintf("color=c=black@0:s=%dx%d:d=1,format=rgba", width, height),
		"-filter_complex", filter,
		"-map", "[out]",
		"-frames:v", "1",
		outPath,
	}
	return runSimple(ctx, r.ffmpeg.BinaryPath(), args)
}

// ---- graph construction ----

type inputSpec struct {
	args []string
}

type graphBuilder struct {
	lines    []string
	inputs   []inputSpec
	indices  map[string]int
	splits   map[string][]string
	labelSeq int
}

func newGraphBuilder() *graphBuilder {
	return &graphBuilder{
		indices: map[string]int{},
		splits:  map[string][]string{},
	}
}

func (b *graphBuilder) add(format string, args ...any) {
	b.lines = append(b.lines, fmt.Sprintf(format, args...))
}

func (b *graphBuilder) input(key string, args ...string) int {
	if idx, ok := b.indices[key]; ok {
		return idx
	}
	idx := len(b.inputs)
	b.inputs = append(b.inputs, inputSpec{args: args})
	b.indices[key] = idx
	return idx
}

func (b *graphBuilder) nextLabel() int {
	b.labelSeq++
	return b.labelSeq
}

func (b *graphBuilder) labels(key, stream string, index, count int) []string {
	if count <= 1 {
		return []string{fmt.Sprintf("[%d:%s]", index, stream)}
	}
	splitKey := key + "|" + stream
	if labels, ok := b.splits[splitKey]; ok {
		return labels
	}
	labels := make([]string, count)
	for i := range labels {
		labels[i] = fmt.Sprintf("[%s%d]", stream, b.nextLabel())
	}
	filterName := "split"
	if stream == "a" {
		filterName = "asplit"
	}
	b.add("[%d:%s]%s=%d%s", index, stream, filterName, count, strings.Join(labels, ""))
	b.splits[splitKey] = labels
	return labels
}

func (b *graphBuilder) inputArgs() []string {
	var args []string
	for _, input := range b.inputs {
		args = append(args, input.args...)
	}
	return args
}

type videoPlan struct {
	clip  models.Clip
	asset *models.Asset
	in    float64
	out   float64
	eff   float64
}

func buildVideo(b *graphBuilder, tl models.Timeline, assets map[string]*models.Asset, width, height int, fps, total float64) (string, error) {
	clips := firstTrackClips(tl, models.TrackVideo)
	sortClips(clips)

	plans := make([]videoPlan, 0, len(clips))
	counts := map[string]int{}
	for _, clip := range clips {
		asset := assets[clip.AssetID]
		plan := videoPlan{clip: clip, asset: asset}
		if asset != nil && asset.Kind == models.AssetVideo {
			in := clip.In
			out := clip.In + clip.Duration
			if clip.Out > clip.In {
				out = clip.Out
			}
			if asset.Duration > 0 && out > asset.Duration {
				out = asset.Duration
			}
			if out-in > 0.02 {
				plan.in = in
				plan.out = out
				plan.eff = math.Min(clip.Duration, out-in)
				counts[clip.AssetID]++
			}
		}
		plans = append(plans, plan)
	}

	indices := map[string]int{}
	for assetID := range counts {
		asset := assets[assetID]
		indices[assetID] = b.input("asset:"+assetID, "-i", asset.Path)
	}
	used := map[string]int{}
	segments := []string{}
	cursor := 0.0
	gap := func(duration float64) {
		if duration <= 0.005 {
			return
		}
		label := fmt.Sprintf("[gap%d]", b.nextLabel())
		b.add("color=c=black:s=%dx%d:r=%s:d=%s,setsar=1,format=yuv420p%s", width, height, formatSeconds(fps), formatSeconds(duration), label)
		segments = append(segments, label)
	}

	for _, plan := range plans {
		if plan.clip.Start > cursor+0.005 {
			gap(plan.clip.Start - cursor)
			cursor = plan.clip.Start
		}
		if plan.eff <= 0 {
			gap(plan.clip.Duration)
			cursor = plan.clip.Start + plan.clip.Duration
			continue
		}
		labels := b.labels("asset:"+plan.clip.AssetID, "v", indices[plan.clip.AssetID], counts[plan.clip.AssetID])
		src := labels[used[plan.clip.AssetID]]
		used[plan.clip.AssetID]++
		label := fmt.Sprintf("[vc%d]", b.nextLabel())
		b.add("%strim=start=%s:end=%s,setpts=PTS-STARTPTS,fps=%s,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=black,setsar=1,format=yuv420p%s",
			src, formatSeconds(plan.in), formatSeconds(plan.out), formatSeconds(fps), width, height, width, height, label)
		segments = append(segments, label)
		cursor = plan.clip.Start + plan.eff
		if plan.eff < plan.clip.Duration-0.005 {
			gap(plan.clip.Duration - plan.eff)
			cursor = plan.clip.Start + plan.clip.Duration
		}
	}
	if cursor < total-0.005 {
		gap(total - cursor)
	}

	if len(segments) == 0 {
		b.add("color=c=black:s=%dx%d:r=%s:d=%s,setsar=1,format=yuv420p[base0]", width, height, formatSeconds(fps), formatSeconds(total))
		return "[base0]", nil
	}
	if len(segments) == 1 {
		b.add("%snull[base0]", segments[0])
		return "[base0]", nil
	}
	b.add("%sconcat=n=%d:v=1:a=0[base0]", strings.Join(segments, ""), len(segments))
	return "[base0]", nil
}

type audioPlan struct {
	clip  models.Clip
	asset *models.Asset
	in    float64
	out   float64
}

func buildAudio(b *graphBuilder, tl models.Timeline, assets map[string]*models.Asset, total float64) (string, error) {
	var clips []models.Clip
	for _, track := range tl.Tracks {
		if track.Kind != models.TrackAudio || track.Muted {
			continue
		}
		clips = append(clips, track.Clips...)
	}
	sortClips(clips)

	plans := make([]audioPlan, 0, len(clips))
	counts := map[string]int{}
	for _, clip := range clips {
		asset := assets[clip.AssetID]
		if asset == nil || !asset.HasAudio {
			continue
		}
		in := clip.In
		out := clip.In + clip.Duration
		if clip.Out > clip.In {
			out = clip.Out
		}
		if asset.Duration > 0 && out > asset.Duration {
			out = asset.Duration
		}
		if out-in <= 0.02 {
			continue
		}
		plans = append(plans, audioPlan{clip: clip, asset: asset, in: in, out: out})
		counts[clip.AssetID]++
	}

	if len(plans) == 0 {
		b.add("anullsrc=channel_layout=stereo:sample_rate=48000,atrim=end=%s[aout]", formatSeconds(total))
		return "[aout]", nil
	}

	indices := map[string]int{}
	for assetID := range counts {
		asset := assets[assetID]
		indices[assetID] = b.input("asset:"+assetID, "-i", asset.Path)
	}
	used := map[string]int{}
	var mixed []string
	for _, plan := range plans {
		labels := b.labels("asset:"+plan.clip.AssetID, "a", indices[plan.clip.AssetID], counts[plan.clip.AssetID])
		src := labels[used[plan.clip.AssetID]]
		used[plan.clip.AssetID]++

		duration := plan.out - plan.in
		volume := plan.clip.Volume
		chain := fmt.Sprintf("%satrim=start=%s:end=%s,asetpts=PTS-STARTPTS,aresample=48000,aformat=sample_fmts=fltp:channel_layouts=stereo,volume=%.3f",
			src, formatSeconds(plan.in), formatSeconds(plan.out), volume)
		if plan.clip.FadeIn > 0 {
			chain += fmt.Sprintf(",afade=t=in:st=0:d=%s", formatSeconds(math.Min(plan.clip.FadeIn, duration)))
		}
		if plan.clip.FadeOut > 0 {
			fadeOut := math.Min(plan.clip.FadeOut, duration)
			chain += fmt.Sprintf(",afade=t=out:st=%s:d=%s", formatSeconds(duration-fadeOut), formatSeconds(fadeOut))
		}
		if delay := int(math.Round(plan.clip.Start * 1000)); delay > 0 {
			chain += fmt.Sprintf(",adelay=delays=%d:all=1", delay)
		}
		label := fmt.Sprintf("[amc%d]", b.nextLabel())
		b.add("%s%s", chain, label)
		mixed = append(mixed, label)
	}

	mixLabel := "[amixed0]"
	if len(mixed) == 1 {
		b.add("%snull%s", mixed[0], mixLabel)
	} else {
		b.add("%samix=inputs=%d:duration=longest:normalize=0%s", strings.Join(mixed, ""), len(mixed), mixLabel)
	}
	b.add("%sapad=whole_dur=%s,atrim=end=%s[aout]", mixLabel, formatSeconds(total), formatSeconds(total))
	return "[aout]", nil
}

func buildOverlays(b *graphBuilder, tl models.Timeline, paths map[string]string, fps, total float64) (string, error) {
	clips := collectClips(tl, models.TrackOverlay)
	if len(clips) == 0 {
		return "[base0]", nil
	}
	sortClips(clips)
	b.add("[base0]format=rgba[bov0]")
	base := "[bov0]"

	for _, clip := range clips {
		if clip.Start >= total {
			continue
		}
		path := paths[clip.ID]
		if path == "" {
			continue
		}
		end := math.Min(clip.End(), total)
		if end <= clip.Start+0.001 {
			continue
		}
		idx := b.input("overlay:"+clip.ID,
			"-loop", "1", "-t", formatSeconds(total), "-framerate", formatSeconds(fps), "-i", path)
		ov := fmt.Sprintf("[ov%d]", b.nextLabel())
		b.add("[%d:v]format=rgba%s", idx, ov)
		next := fmt.Sprintf("[bov%d]", b.nextLabel())

		mode := normalizeBlendMode(clip.BlendMode)
		if mode == "normal" {
			b.add("%s%soverlay=0:0:format=auto:enable='between(t,%s,%s)'%s",
				base, ov, formatSeconds(clip.Start), formatSeconds(end), next)
			base = next
			continue
		}

		ovA := fmt.Sprintf("[ova%d]", b.nextLabel())
		ovB := fmt.Sprintf("[ovb%d]", b.nextLabel())
		blended := fmt.Sprintf("[bl%d]", b.nextLabel())
		mask := fmt.Sprintf("[mask%d]", b.nextLabel())
		enabled := fmt.Sprintf("enable='between(t,%s,%s)'", formatSeconds(clip.Start), formatSeconds(end))
		b.add("%ssplit%s%s", ov, ovA, ovB)
		b.add("%s%sblend=all_mode=%s:%s%s", base, ovA, mode, enabled, blended)
		b.add("%salphaextract%s", ovB, mask)
		b.add("%s%s%smaskedmerge=%s%s", base, blended, mask, enabled, next)
		base = next
	}

	b.add("%sformat=yuv420p[basev]", base)
	return "[basev]", nil
}

func buildCaptions(tl models.Timeline, cacheDir string, width, height int) (string, bool, error) {
	clips := collectClips(tl, models.TrackCaption)
	if len(clips) == 0 {
		return "", false, nil
	}
	sortClips(clips)
	path := filepath.Join(cacheDir, "captions.ass")
	if err := writeASS(path, clips, tl.CaptionStyle, width, height); err != nil {
		return "", false, err
	}
	return path, true, nil
}

// ---- helpers ----

func collectClips(tl models.Timeline, kind string) []models.Clip {
	var clips []models.Clip
	for _, track := range tl.Tracks {
		if track.Kind != kind || track.Muted {
			continue
		}
		clips = append(clips, track.Clips...)
	}
	return clips
}

func firstTrackClips(tl models.Timeline, kind string) []models.Clip {
	for _, track := range tl.Tracks {
		if track.Kind == kind && !track.Muted {
			return append([]models.Clip{}, track.Clips...)
		}
	}
	return nil
}

func sortClips(clips []models.Clip) {
	sort.Slice(clips, func(a, b int) bool {
		if clips[a].Start == clips[b].Start {
			return clips[a].ID < clips[b].ID
		}
		return clips[a].Start < clips[b].Start
	})
}

func normalizeBlendMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "normal":
		return "normal"
	case "screen":
		return "screen"
	case "multiply":
		return "multiply"
	case "overlay":
		return "overlay"
	case "softlight", "soft_light", "soft-light":
		return "softlight"
	case "hardlight", "hard_light", "hard-light":
		return "hardlight"
	case "add", "addition":
		return "addition"
	case "difference":
		return "difference"
	case "darken":
		return "darken"
	case "lighten":
		return "lighten"
	case "dodge", "color_dodge":
		return "dodge"
	case "burn", "color_burn":
		return "burn"
	default:
		return "normal"
	}
}

func encoderArgs(encoder string, quality int) []string {
	q := strconv.Itoa(quality)
	switch encoder {
	case "h264_nvenc":
		return []string{"-c:v", "h264_nvenc", "-preset", "p5", "-rc", "vbr", "-cq", q, "-b:v", "0"}
	case "h264_amf":
		return []string{"-c:v", "h264_amf", "-quality", "quality", "-rc", "cqp", "-qp_i", q, "-qp_p", q, "-qp_b", q}
	default:
		return []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", q}
	}
}

func runFFmpeg(ctx context.Context, bin string, args []string, total float64, report func(float64, string)) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", err
	}

	scanner := bufio.NewScanner(stdout)
	lastReport := -1.0
	for scanner.Scan() {
		line := scanner.Text()
		if value, ok := strings.CutPrefix(line, "out_time="); ok {
			if seconds, err := parseFFTime(value); err == nil && total > 0 {
				ratio := math.Min(seconds/total, 1)
				progress := 0.14 + 0.85*ratio
				if progress-lastReport >= 0.01 {
					lastReport = progress
					report(progress, fmt.Sprintf("Merender... %.0f%%", ratio*100))
				}
			}
		}
	}
	waitErr := cmd.Wait()
	if waitErr != nil {
		if ctx.Err() != nil {
			return stderr.String(), ctx.Err()
		}
		return stderr.String(), waitErr
	}
	return stderr.String(), nil
}

func runSimple(ctx context.Context, bin string, args []string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func parseFFTime(value string) (float64, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid time")
	}
	h, err1 := strconv.ParseFloat(parts[0], 64)
	m, err2 := strconv.ParseFloat(parts[1], 64)
	s, err3 := strconv.ParseFloat(parts[2], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, fmt.Errorf("invalid time")
	}
	return h*3600 + m*60 + s, nil
}

func rotatedSize(w, h, angle float64) (float64, float64) {
	cos := math.Abs(math.Cos(angle))
	sin := math.Abs(math.Sin(angle))
	return w*cos + h*sin, w*sin + h*cos
}

func formatSeconds(value float64) string {
	return strconv.FormatFloat(value, 'f', 3, 64)
}

func escapeFilterValue(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	value = strings.ReplaceAll(value, ":", "\\:")
	value = strings.ReplaceAll(value, "'", "\\'")
	return value
}

func windowsFontsDir() string {
	if win := os.Getenv("WINDIR"); win != "" {
		return filepath.Join(win, "Fonts")
	}
	return `C:\Windows\Fonts`
}

func safeFileID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "clip"
	}
	return b.String()
}

func cleanupOldOverlayCache(cacheDir string) {
	for _, pattern := range []string{"overlay_*.png", "text_*.txt"} {
		matches, _ := filepath.Glob(filepath.Join(cacheDir, pattern))
		for _, match := range matches {
			_ = os.Remove(match)
		}
	}
}

func hexDigits(value, fallback string) string {
	r, g, b, ok := parseHexColor(value)
	if !ok {
		r, g, b, _ = parseHexColor(fallback)
	}
	return fmt.Sprintf("%02X%02X%02X", r, g, b)
}

func wrapText(text string, fontPx, maxWidth float64) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := words[0]
		lineWidth := estimateTextWidth(line, fontPx)
		for _, word := range words[1:] {
			wordWidth := estimateTextWidth(" "+word, fontPx)
			if lineWidth+wordWidth > maxWidth {
				lines = append(lines, line)
				line = word
				lineWidth = estimateTextWidth(word, fontPx)
				continue
			}
			line += " " + word
			lineWidth += wordWidth
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func estimateTextWidth(text string, fontPx float64) float64 {
	total := 0.0
	for _, r := range text {
		switch {
		case r == ' ':
			total += 0.32
		case r > 0x2E80:
			total += 1.0
		case r == 'i' || r == 'l' || r == 'I' || r == '.' || r == ',' || r == '\'':
			total += 0.28
		default:
			total += 0.58
		}
	}
	return total * fontPx
}
