package media

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"autocut/internal/models"
)

func TestBuildVideoVOFilterKeepsPicturePastVO(t *testing.T) {
	keeps := []models.KeepInterval{
		{Start: 0, End: 10},
		{Start: 12, End: 100},
	}
	filter := buildVideoVOFilter(keeps, 48)

	if !strings.Contains(filter, "trim=start=12.000:end=100.000") {
		t.Fatalf("video tail was not kept: %s", filter)
	}
	if !strings.Contains(filter, "apad=whole_dur=") {
		t.Fatalf("audio past the VO should be padded, got %s", filter)
	}
	if strings.Contains(filter, "tpad=") {
		t.Fatalf("picture should not be extended to the VO length: %s", filter)
	}
	if strings.Count(filter, "[0:v]") != 1 {
		t.Fatalf("video pad used more than once: %s", filter)
	}
}

func TestBuildVideoVOFilterOneIntervalKeepsFullPicture(t *testing.T) {
	filter := buildVideoVOFilter([]models.KeepInterval{{Start: 0, End: 120}}, 48)
	if !strings.Contains(filter, "trim=start=0.000:end=120.000") {
		t.Fatalf("full picture was not kept: %s", filter)
	}
	if !strings.Contains(filter, "apad=whole_dur=120.000") {
		t.Fatalf("silence was not added under the video tail: %s", filter)
	}
	if strings.Contains(filter, "tpad=") {
		t.Fatalf("picture should not be cloned to the VO: %s", filter)
	}
}

func TestBuildCutFilterSplitsSharedPads(t *testing.T) {
	filter := buildCutFilter([]models.KeepInterval{{Start: 0, End: 10}, {Start: 12, End: 100}})
	if strings.Count(filter, "[0:v]") != 1 || strings.Count(filter, "[0:a]") != 1 {
		t.Fatalf("input pads must be split once: %s", filter)
	}
	if !strings.Contains(filter, "split=2") || !strings.Contains(filter, "asplit=2") {
		t.Fatalf("missing split: %s", filter)
	}
}

func TestCutVideoWithVOKeepsPicturePastVO(t *testing.T) {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	video := filepath.Join(dir, "picture.mp4")
	vo := filepath.Join(dir, "voice.wav")
	out := filepath.Join(dir, "cut.mp4")

	makeClip(t, bin, "-y", "-f", "lavfi", "-i", "color=c=black:s=320x180:r=25:d=4", "-an", video)
	makeClip(t, bin, "-y", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=2", "-ac", "2", vo)

	ff := NewFFmpeg(bin, false)
	if ff.HasAudio(video) {
		t.Fatal("synthetic picture should have no audio stream")
	}
	keeps := []models.KeepInterval{{Start: 0, End: 1}, {Start: 1.5, End: 4}}
	if err := ff.CutVideoWithVO(video, vo, keeps, out); err != nil {
		t.Fatal(err)
	}
	got, err := ff.GetDuration(out)
	if err != nil {
		t.Fatal(err)
	}
	// 1s + 2.5s of picture. The 2s VO must not collapse the result.
	if got < 3.2 || got > 3.8 {
		t.Fatalf("cut duration = %.3f, want about 3.5s of video", got)
	}
	if !ff.HasAudio(out) {
		t.Fatal("cut video should carry the VO where it overlaps")
	}
}

func makeClip(t *testing.T, bin string, args ...string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg %v: %v\n%s", args, err, output)
	}
	if _, err := os.Stat(args[len(args)-1]); err != nil {
		t.Fatal(err)
	}
}

func TestBuildAudioCutFilterSplitsSharedPad(t *testing.T) {
	filter := buildAudioCutFilter([]models.KeepInterval{{Start: 0, End: 10}, {Start: 12, End: 40}})
	if strings.Count(filter, "[0:a]") != 1 {
		t.Fatalf("audio pad used more than once: %s", filter)
	}
	if !strings.Contains(filter, "asplit=2") {
		t.Fatalf("missing asplit: %s", filter)
	}
}
