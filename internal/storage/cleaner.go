package storage

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	"autocut/internal/models"
)

type StorageManager struct {
	baseDir string
}

func NewStorageManager(baseDir string) *StorageManager {
	return &StorageManager{baseDir: baseDir}
}

func (s *StorageManager) GetTempDir(jobID string) string {
	dir := filepath.Join(s.baseDir, "temp", jobID)
	_ = os.MkdirAll(dir, 0755)
	return dir
}

func (s *StorageManager) GetArchiveDir(jobID string) string {
	dir := filepath.Join(s.baseDir, "archive", jobID)
	_ = os.MkdirAll(dir, 0755)
	return dir
}

// ArchiveEssentialAssets permanently stores lightweight assets: cut audio, transcript, cutlist, and metadata.
func (s *StorageManager) ArchiveEssentialAssets(jobID string, audioSourcePath string, transcript *models.Transcript, cutlist *models.Cutlist, meta *models.YouTubeMetadata) error {
	archiveDir := s.GetArchiveDir(jobID)

	// 1. Copy cut audio to permanent archive
	if audioSourcePath != "" {
		destAudio := filepath.Join(archiveDir, "audio_cut.wav")
		_ = copyFile(audioSourcePath, destAudio)
	}

	// 2. Save transcript JSON
	if transcript != nil {
		tData, _ := json.MarshalIndent(transcript, "", "  ")
		_ = os.WriteFile(filepath.Join(archiveDir, "transcript.json"), tData, 0644)
	}

	// 3. Save cutlist JSON
	if cutlist != nil {
		cData, _ := json.MarshalIndent(cutlist, "", "  ")
		_ = os.WriteFile(filepath.Join(archiveDir, "cutlist.json"), cData, 0644)
	}

	// 4. Save metadata JSON
	if meta != nil {
		mData, _ := json.MarshalIndent(meta, "", "  ")
		_ = os.WriteFile(filepath.Join(archiveDir, "metadata.json"), mData, 0644)
	}

	return nil
}

// CleanupEphemeralFiles deletes raw video and rendered video from temp storage after user download.
func (s *StorageManager) CleanupEphemeralFiles(jobID string) error {
	tempDir := filepath.Join(s.baseDir, "temp", jobID)
	if _, err := os.Stat(tempDir); err == nil {
		return os.RemoveAll(tempDir)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
