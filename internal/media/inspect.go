package media

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"autocut/internal/models"
)

// ProbeInfo is the metadata the editor needs about an imported asset.
type ProbeInfo struct {
	Duration float64
	Width    int
	Height   int
	FPS      float64
	HasAudio bool
	HasVideo bool
	Size     int64
}

type probeOutput struct {
	Format struct {
		Duration string `json:"duration"`
		Size     string `json:"size"`
	} `json:"format"`
	Streams []struct {
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		AvgFrameRate string `json:"avg_frame_rate"`
		RFrameRate   string `json:"r_frame_rate"`
		Duration     string `json:"duration"`
	} `json:"streams"`
}

func (f *FFmpeg) ffprobeBin() string {
	base := filepath.Base(f.binPath)
	if strings.Contains(base, "ffmpeg") {
		return filepath.Join(filepath.Dir(f.binPath), strings.Replace(base, "ffmpeg", "ffprobe", 1))
	}
	return "ffprobe"
}

// Probe reads duration, dimensions, frame rate and stream presence with ffprobe.
func (f *FFmpeg) Probe(path string) (*ProbeInfo, error) {
	cmd := exec.Command(f.ffprobeBin(), "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", path)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed for %s: %w", filepath.Base(path), err)
	}

	var parsed probeOutput
	if err := json.Unmarshal(output, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	info := &ProbeInfo{}
	if d, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
		info.Duration = d
	}
	if s, err := strconv.ParseInt(parsed.Format.Size, 10, 64); err == nil {
		info.Size = s
	}

	for _, stream := range parsed.Streams {
		switch stream.CodecType {
		case "video":
			info.HasVideo = true
			if stream.Width > 0 && (info.Width == 0 || stream.Width < info.Width) {
				info.Width = stream.Width
				info.Height = stream.Height
			}
			if info.FPS == 0 {
				if fps := parseFrameRate(stream.AvgFrameRate); fps > 0 {
					info.FPS = fps
				} else if fps := parseFrameRate(stream.RFrameRate); fps > 0 {
					info.FPS = fps
				}
			}
			if info.Duration == 0 {
				if d, err := strconv.ParseFloat(stream.Duration, 64); err == nil {
					info.Duration = d
				}
			}
		case "audio":
			info.HasAudio = true
			if info.Duration == 0 {
				if d, err := strconv.ParseFloat(stream.Duration, 64); err == nil {
					info.Duration = d
				}
			}
		}
	}

	if info.Size == 0 {
		if st, err := os.Stat(path); err == nil {
			info.Size = st.Size()
		}
	}
	return info, nil
}

func parseFrameRate(value string) float64 {
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		num, err1 := strconv.ParseFloat(parts[0], 64)
		den, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 == nil && err2 == nil && den > 0 {
			return num / den
		}
	}
	if fps, err := strconv.ParseFloat(value, 64); err == nil {
		return fps
	}
	return 0
}

// CreateThumbnail writes a small JPEG preview for a video or image asset.
func (f *FFmpeg) CreateThumbnail(mediaPath, outputPath string, isVideo bool) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	args := []string{"-y", "-v", "error"}
	if isVideo {
		args = append(args, "-ss", "0.2")
	}
	args = append(args,
		"-i", mediaPath,
		"-frames:v", "1",
		"-vf", "scale=480:-2",
		"-q:v", "4",
		outputPath,
	)
	cmd := exec.Command(f.binPath, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("thumbnail failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// Waveform is the compact peak data the timeline draws for audio clips.
type Waveform struct {
	Peaks    []float64 `json:"peaks"`
	Duration float64   `json:"duration"`
}

// ComputeWaveform decodes audio to 8kHz mono and stores normalized peaks.
func (f *FFmpeg) ComputeWaveform(mediaPath, outputPath string, duration float64, buckets int) error {
	if buckets <= 0 {
		buckets = 1200
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	const sampleRate = 8000
	totalSamples := int(duration * sampleRate)
	if totalSamples <= 0 {
		totalSamples = sampleRate * 60
	}
	bucketSize := int(math.Ceil(float64(totalSamples) / float64(buckets)))
	if bucketSize < 1 {
		bucketSize = 1
	}

	cmd := exec.Command(f.binPath, "-v", "error", "-i", mediaPath,
		"-vn", "-ac", "1", "-ar", strconv.Itoa(sampleRate), "-f", "s16le", "-")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	peaks := make([]float64, 0, buckets)
	var bucketMax int32
	var bucketCount int
	reader := bufio.NewReaderSize(stdout, 64*1024)
	buf := make([]byte, 32*1024)
	for {
		n, readErr := reader.Read(buf)
		for i := 0; i+1 < n; i += 2 {
			sample := int32(int16(binary.LittleEndian.Uint16(buf[i : i+2])))
			if sample < 0 {
				sample = -sample
			}
			if sample > bucketMax {
				bucketMax = sample
			}
			bucketCount++
			if bucketCount >= bucketSize {
				peaks = append(peaks, roundPeak(bucketMax))
				bucketMax = 0
				bucketCount = 0
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			break
		}
	}
	if bucketCount > 0 {
		peaks = append(peaks, roundPeak(bucketMax))
	}
	_ = cmd.Wait()

	data, err := json.Marshal(Waveform{Peaks: peaks, Duration: duration})
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, data, 0644)
}

func roundPeak(sample int32) float64 {
	value := float64(sample) / 32768.0
	if value > 1 {
		value = 1
	}
	return math.Round(value*1000) / 1000
}

// DetectVideoAudio is a cheap helper for the renderer.
func (f *FFmpeg) DetectVideoAudio(path string) bool { return f.HasAudio(path) }

// EnsureAssetKind maps a file category to the editor's asset kinds.
func EnsureAssetKind(kind string) string {
	switch kind {
	case models.AssetVideo, models.AssetAudio, models.AssetImage:
		return kind
	default:
		return models.AssetVideo
	}
}
