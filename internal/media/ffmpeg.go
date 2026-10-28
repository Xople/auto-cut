package media

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"autocut/internal/models"
)

type FFmpeg struct {
	binPath  string
	useNVENC bool
}

func (f *FFmpeg) BinaryPath() string { return f.binPath }

func NewFFmpeg(binPath string, useNVENC bool) *FFmpeg {
	if binPath == "" {
		binPath = "ffmpeg"
	}
	return &FFmpeg{
		binPath:  binPath,
		useNVENC: useNVENC,
	}
}

// ExtractAudio extracts 16kHz mono PCM audio for whisper and silence detection.
func (f *FFmpeg) ExtractAudio(videoPath, audioPath string) error {
	if err := os.MkdirAll(filepath.Dir(audioPath), 0755); err != nil {
		return err
	}
	// -y overwrite, -vn no video, -ar 16000 16kHz, -ac 1 mono, -c:a pcm_s16le 16-bit uncompressed
	cmd := exec.Command(f.binPath, "-y", "-i", videoPath, "-vn", "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", audioPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg extract audio failed: %w, output: %s", err, string(output))
	}
	return nil
}

// HasAudio reports whether the file contains an audio stream.
func (f *FFmpeg) HasAudio(mediaPath string) bool {
	cmd := exec.Command(f.binPath, "-i", mediaPath)
	output, _ := cmd.CombinedOutput()
	return strings.Contains(string(output), "Audio:")
}

// GetDuration parses duration of media file in seconds.
func (f *FFmpeg) GetDuration(mediaPath string) (float64, error) {
	cmd := exec.Command(f.binPath, "-i", mediaPath)
	output, _ := cmd.CombinedOutput() // ffmpeg exits with 1 when no output file specified
	outStr := string(output)

	// Look for "Duration: 00:01:23.45"
	re := regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+\.\d+)`)
	matches := re.FindStringSubmatch(outStr)
	if len(matches) < 4 {
		return 0, fmt.Errorf("could not parse duration from ffmpeg output")
	}

	h, _ := strconv.ParseFloat(matches[1], 64)
	m, _ := strconv.ParseFloat(matches[2], 64)
	s, _ := strconv.ParseFloat(matches[3], 64)

	return h*3600 + m*60 + s, nil
}

// DetectSilence runs ffmpeg silencedetect filter and parses silence start & end timestamps.
func (f *FFmpeg) DetectSilence(audioPath string, noiseThreshold string, minDuration float64) ([]models.CutSegment, error) {
	if noiseThreshold == "" {
		noiseThreshold = "-30dB"
	}
	if minDuration <= 0 {
		minDuration = 0.4
	}

	filterArg := fmt.Sprintf("silencedetect=noise=%s:d=%0.2f", noiseThreshold, minDuration)
	cmd := exec.Command(f.binPath, "-i", audioPath, "-af", filterArg, "-f", "null", "-")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("ffmpeg silencedetect failed: %w, output: %s", err, string(output))
	}

	outStr := string(output)
	var segments []models.CutSegment

	// Regex for silence_start: 12.34 and silence_end: 15.67 | silence_duration: 3.33
	reStart := regexp.MustCompile(`silence_start:\s*([0-9.]+)`)
	reEnd := regexp.MustCompile(`silence_end:\s*([0-9.]+)\s*\|\s*silence_duration:\s*([0-9.]+)`)

	lines := strings.Split(outStr, "\n")
	var currentStart float64 = -1

	for _, line := range lines {
		if startMatch := reStart.FindStringSubmatch(line); len(startMatch) > 1 {
			s, err := strconv.ParseFloat(startMatch[1], 64)
			if err == nil {
				currentStart = s
			}
		}
		if endMatch := reEnd.FindStringSubmatch(line); len(endMatch) > 2 && currentStart >= 0 {
			e, err := strconv.ParseFloat(endMatch[1], 64)
			dur, _ := strconv.ParseFloat(endMatch[2], 64)
			if err == nil && e > currentStart {
				segments = append(segments, models.CutSegment{
					ID:             fmt.Sprintf("silence_%.2f", currentStart),
					Start:          currentStart,
					End:            e,
					Duration:       dur,
					Reason:         models.ReasonSilence,
					Source:         "ffmpeg",
					Confidence:     1.0,
					NeedsReview:    false,
					UserOverridden: false,
					IsCut:          true,
				})
			}
			currentStart = -1
		}
	}

	return segments, nil
}

// CutAndReencode cuts the video to keep only keepIntervals, applying frame-accurate re-encoding.
func (f *FFmpeg) CutAndReencode(videoPath string, keepIntervals []models.KeepInterval, outputPath string) error {
	if len(keepIntervals) == 0 {
		return fmt.Errorf("cannot cut video: keepIntervals is empty")
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	filter := buildCutFilter(keepIntervals)

	args := []string{
		"-y",
		"-i", videoPath,
		"-filter_complex", filter,
		"-map", "[outv]",
		"-map", "[outa]",
	}

	// Apply NVENC if requested, with CPU fallback
	if f.useNVENC {
		args = append(args, "-c:v", "h264_nvenc", "-preset", "p5", "-cq", "21")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "fast", "-crf", "20")
	}
	args = append(args, "-c:a", "aac", "-b:a", "192k", outputPath)

	cmd := exec.Command(f.binPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// If NVENC fails (e.g. driver issue), retry with libx264
		if f.useNVENC && strings.Contains(string(output), "nvenc") {
			return f.retryWithLibx264(videoPath, filter, outputPath)
		}
		return fmt.Errorf("ffmpeg cut and reencode failed: %w, output: %s", err, string(output))
	}

	return nil
}

// buildCutFilter trims a video that already has an audio stream. Each input pad is
// split once so several keep intervals can share it.
func buildCutFilter(keepIntervals []models.KeepInterval) string {
	n := len(keepIntervals)
	var filterBuf bytes.Buffer
	if n == 1 {
		interval := keepIntervals[0]
		filterBuf.WriteString(fmt.Sprintf("[0:v]trim=start=%.3f:end=%.3f,setpts=PTS-STARTPTS[v0];", interval.Start, interval.End))
		filterBuf.WriteString(fmt.Sprintf("[0:a]atrim=start=%.3f:end=%.3f,asetpts=PTS-STARTPTS[a0];", interval.Start, interval.End))
	} else {
		var vLabels, aLabels []string
		for i := 0; i < n; i++ {
			vLabels = append(vLabels, fmt.Sprintf("[vsrc%d]", i))
			aLabels = append(aLabels, fmt.Sprintf("[asrc%d]", i))
		}
		filterBuf.WriteString(fmt.Sprintf("[0:v]split=%d%s;", n, strings.Join(vLabels, "")))
		filterBuf.WriteString(fmt.Sprintf("[0:a]asplit=%d%s;", n, strings.Join(aLabels, "")))
		for i, interval := range keepIntervals {
			filterBuf.WriteString(fmt.Sprintf("[vsrc%d]trim=start=%.3f:end=%.3f,setpts=PTS-STARTPTS[v%d];", i, interval.Start, interval.End, i))
			filterBuf.WriteString(fmt.Sprintf("[asrc%d]atrim=start=%.3f:end=%.3f,asetpts=PTS-STARTPTS[a%d];", i, interval.Start, interval.End, i))
		}
	}
	var concatInputs []string
	for i := 0; i < n; i++ {
		concatInputs = append(concatInputs, fmt.Sprintf("[v%d][a%d]", i, i))
	}
	filterBuf.WriteString(fmt.Sprintf("%sconcat=n=%d:v=1:a=1[outv][outa]", strings.Join(concatInputs, ""), n))
	return filterBuf.String()
}

func (f *FFmpeg) retryWithLibx264(videoPath, filterComplex, outputPath string) error {
	args := []string{
		"-y",
		"-i", videoPath,
		"-filter_complex", filterComplex,
		"-map", "[outv]",
		"-map", "[outa]",
		"-c:v", "libx264", "-preset", "fast", "-crf", "20",
		"-c:a", "aac", "-b:a", "192k",
		outputPath,
	}
	cmd := exec.Command(f.binPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg cut fallback to libx264 failed: %w, output: %s", err, string(output))
	}
	return nil
}

// CutVideoWithVO trims the picture and lays the VO under it. The video timeline is the
// timeline being cut: picture after the VO ends stays, with silence under that tail.
func (f *FFmpeg) CutVideoWithVO(videoPath, voPath string, keepIntervals []models.KeepInterval, outputPath string) error {
	if len(keepIntervals) == 0 {
		return fmt.Errorf("cannot cut video: keepIntervals is empty")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	voDur, err := f.GetDuration(voPath)
	if err != nil {
		return fmt.Errorf("cannot read VO duration: %w", err)
	}

	filter := buildVideoVOFilter(keepIntervals, voDur)
	args := []string{"-y", "-i", videoPath, "-i", voPath, "-filter_complex", filter, "-map", "[outv]", "-map", "[outa]"}
	if f.useNVENC {
		args = append(args, "-c:v", "h264_nvenc", "-preset", "p5", "-cq", "21")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "fast", "-crf", "20")
	}
	args = append(args, "-c:a", "aac", "-b:a", "192k", outputPath)

	cmd := exec.Command(f.binPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if f.useNVENC && strings.Contains(string(output), "nvenc") {
			return f.cutVideoWithVOLibx264(videoPath, voPath, filter, outputPath)
		}
		return fmt.Errorf("ffmpeg cut video with VO failed: %w, output: %s", err, string(output))
	}
	return nil
}

func (f *FFmpeg) cutVideoWithVOLibx264(videoPath, voPath, filter, outputPath string) error {
	args := []string{
		"-y", "-i", videoPath, "-i", voPath,
		"-filter_complex", filter,
		"-map", "[outv]", "-map", "[outa]",
		"-c:v", "libx264", "-preset", "fast", "-crf", "20",
		"-c:a", "aac", "-b:a", "192k",
		outputPath,
	}
	cmd := exec.Command(f.binPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg cut video with VO fallback failed: %w, output: %s", err, string(output))
	}
	return nil
}

// buildVideoVOFilter trims the picture to each keep interval. Audio comes from the VO where
// that interval overlaps it, and from silence where the video continues past the VO.
func buildVideoVOFilter(keepIntervals []models.KeepInterval, voDuration float64) string {
	var filterBuf bytes.Buffer
	n := len(keepIntervals)

	if n == 1 {
		interval := keepIntervals[0]
		filterBuf.WriteString(fmt.Sprintf("[0:v]trim=start=%.3f:end=%.3f,setpts=PTS-STARTPTS[v0];", interval.Start, interval.End))
	} else {
		var vLabels []string
		for i := 0; i < n; i++ {
			vLabels = append(vLabels, fmt.Sprintf("[vsrc%d]", i))
		}
		filterBuf.WriteString(fmt.Sprintf("[0:v]split=%d%s;", n, strings.Join(vLabels, "")))
		for i, interval := range keepIntervals {
			filterBuf.WriteString(fmt.Sprintf("[vsrc%d]trim=start=%.3f:end=%.3f,setpts=PTS-STARTPTS[v%d];", i, interval.Start, interval.End, i))
		}
	}

	var voIndexes []int
	for i, interval := range keepIntervals {
		if interval.Start < voDuration-0.02 {
			voIndexes = append(voIndexes, i)
		}
	}
	voSrc := map[int]string{}
	if len(voIndexes) == 1 {
		voSrc[voIndexes[0]] = "[1:a]"
	} else if len(voIndexes) > 1 {
		var aLabels []string
		for _, i := range voIndexes {
			aLabels = append(aLabels, fmt.Sprintf("[asrc%d]", i))
		}
		filterBuf.WriteString(fmt.Sprintf("[1:a]asplit=%d%s;", len(voIndexes), strings.Join(aLabels, "")))
		for _, i := range voIndexes {
			voSrc[i] = fmt.Sprintf("[asrc%d]", i)
		}
	}

	var concatInputs []string
	for i, interval := range keepIntervals {
		dur := interval.End - interval.Start
		if label, ok := voSrc[i]; ok && interval.End <= voDuration+0.02 {
			filterBuf.WriteString(fmt.Sprintf("%satrim=start=%.3f:end=%.3f,asetpts=PTS-STARTPTS,%s[a%d];", label, interval.Start, interval.End, voAudioFormat, i))
		} else if label, ok := voSrc[i]; ok {
			filterBuf.WriteString(fmt.Sprintf("%satrim=start=%.3f:end=%.3f,asetpts=PTS-STARTPTS,%s,apad=whole_dur=%.3f[a%d];", label, interval.Start, voDuration, voAudioFormat, dur, i))
		} else {
			filterBuf.WriteString(fmt.Sprintf("anullsrc=channel_layout=stereo:sample_rate=48000,atrim=end=%.3f,asetpts=PTS-STARTPTS,%s[a%d];", dur, voAudioFormat, i))
		}
		concatInputs = append(concatInputs, fmt.Sprintf("[v%d][a%d]", i, i))
	}
	filterBuf.WriteString(fmt.Sprintf("%sconcat=n=%d:v=1:a=1[outv][outa]", strings.Join(concatInputs, ""), n))
	return filterBuf.String()
}

// voAudioFormat forces every VO and silence pad onto the same layout before concat.
const voAudioFormat = "aformat=sample_fmts=fltp:sample_rates=48000:channel_layouts=stereo"

// CutAudioOnly cuts an audio file to keep only keepIntervals without requiring a video stream.
func (f *FFmpeg) CutAudioOnly(audioPath string, keepIntervals []models.KeepInterval, outputPath string) error {
	if len(keepIntervals) == 0 {
		return fmt.Errorf("cannot cut audio: keepIntervals is empty")
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	args := []string{
		"-y",
		"-i", audioPath,
		"-filter_complex", buildAudioCutFilter(keepIntervals),
		"-map", "[outa]",
	}

	if strings.HasSuffix(strings.ToLower(outputPath), ".wav") {
		args = append(args, "-c:a", "pcm_s16le", outputPath)
	} else {
		args = append(args, "-c:a", "aac", "-b:a", "256k", outputPath)
	}

	cmd := exec.Command(f.binPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg cut audio only failed: %w, output: %s", err, string(output))
	}
	return nil
}

// buildAudioCutFilter trims one audio stream. Several intervals share it through asplit.
func buildAudioCutFilter(keepIntervals []models.KeepInterval) string {
	n := len(keepIntervals)
	var filterBuf bytes.Buffer
	if n == 1 {
		interval := keepIntervals[0]
		filterBuf.WriteString(fmt.Sprintf("[0:a]atrim=start=%.3f:end=%.3f,asetpts=PTS-STARTPTS[outa]", interval.Start, interval.End))
		return filterBuf.String()
	}
	var labels []string
	for i := 0; i < n; i++ {
		labels = append(labels, fmt.Sprintf("[asrc%d]", i))
	}
	filterBuf.WriteString(fmt.Sprintf("[0:a]asplit=%d%s;", n, strings.Join(labels, "")))
	var concatInputs []string
	for i, interval := range keepIntervals {
		filterBuf.WriteString(fmt.Sprintf("[asrc%d]atrim=start=%.3f:end=%.3f,asetpts=PTS-STARTPTS[a%d];", i, interval.Start, interval.End, i))
		concatInputs = append(concatInputs, fmt.Sprintf("[a%d]", i))
	}
	filterBuf.WriteString(fmt.Sprintf("%sconcat=n=%d:v=0:a=1[outa]", strings.Join(concatInputs, ""), n))
	return filterBuf.String()
}

// BurnAssCaptions burns ASS dynamic subtitles onto the cut video.
func (f *FFmpeg) BurnAssCaptions(videoPath, assPath, outputPath string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	// Escape path for ffmpeg filter syntax (Windows backslashes must be escaped or forward slashed)
	escapedAss := strings.ReplaceAll(assPath, "\\", "/")
	escapedAss = strings.ReplaceAll(escapedAss, ":", "\\:")

	vfArg := fmt.Sprintf("ass='%s'", escapedAss)
	args := []string{
		"-y",
		"-i", videoPath,
		"-vf", vfArg,
	}

	if f.useNVENC {
		args = append(args, "-c:v", "h264_nvenc", "-preset", "p5", "-cq", "21")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "fast", "-crf", "20")
	}
	args = append(args, "-c:a", "copy", outputPath)

	cmd := exec.Command(f.binPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback without NVENC if needed
		if f.useNVENC && strings.Contains(string(output), "nvenc") {
			fallbackArgs := []string{
				"-y", "-i", videoPath, "-vf", vfArg,
				"-c:v", "libx264", "-preset", "fast", "-crf", "20",
				"-c:a", "copy", outputPath,
			}
			cmdFallback := exec.Command(f.binPath, fallbackArgs...)
			outFb, errFb := cmdFallback.CombinedOutput()
			if errFb != nil {
				return fmt.Errorf("ffmpeg burn captions fallback failed: %w, output: %s", errFb, string(outFb))
			}
			return nil
		}
		return fmt.Errorf("ffmpeg burn ass captions failed: %w, output: %s", err, string(output))
	}

	return nil
}
