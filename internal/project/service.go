// Package project owns the editor's project folders, media imports and
// derived preview files (thumbnails, waveforms).
package project

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"autocut/internal/config"
	"autocut/internal/db"
	"autocut/internal/fonts"
	"autocut/internal/logger"
	"autocut/internal/media"
	"autocut/internal/models"
)

type Service struct {
	cfg    *config.Config
	db     *db.DB
	ffmpeg *media.FFmpeg
	fonts  *fonts.Index
}

func NewService(cfg *config.Config, database *db.DB) *Service {
	ffmpeg := media.NewFFmpeg(cfg.FFmpegPath, cfg.UseNVENC)
	return &Service{
		cfg:    cfg,
		db:     database,
		ffmpeg: ffmpeg,
		fonts:  fonts.NewIndex(),
	}
}

func (s *Service) Fonts() *fonts.Index { return s.fonts }

func (s *Service) FFmpeg() *media.FFmpeg { return s.ffmpeg }

func (s *Service) ProjectDir(projectID string) string {
	return filepath.Join(s.cfg.StorageDir, "projects", projectID)
}

func (s *Service) assetsDir(projectID string) string {
	return filepath.Join(s.ProjectDir(projectID), "assets")
}

func (s *Service) thumbsDir(projectID string) string {
	return filepath.Join(s.ProjectDir(projectID), "thumbs")
}

func (s *Service) waveformsDir(projectID string) string {
	return filepath.Join(s.ProjectDir(projectID), "waveforms")
}

func (s *Service) CacheDir(projectID string) string {
	return filepath.Join(s.ProjectDir(projectID), "cache")
}

func (s *Service) ExportsDir(projectID string) string {
	return filepath.Join(s.ProjectDir(projectID), "exports")
}

func (s *Service) Create(name string, width, height int, fps float64) (*models.Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Untitled Project"
	}
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}
	if fps <= 0 {
		fps = 30
	}

	id := fmt.Sprintf("proj_%d", time.Now().UnixNano())
	p := &models.Project{
		ID:       id,
		Name:     name,
		Width:    width,
		Height:   height,
		FPS:      fps,
		Timeline: models.NewTimeline(width, height, fps),
	}

	for _, dir := range []string{s.assetsDir(id), s.thumbsDir(id), s.waveformsDir(id), s.CacheDir(id), s.ExportsDir(id)} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create project folder: %w", err)
		}
	}
	if err := s.db.CreateProject(p); err != nil {
		_ = os.RemoveAll(s.ProjectDir(id))
		return nil, err
	}
	logger.Info("[Project] created %s (%s)", p.ID, p.Name)
	return p, nil
}

func (s *Service) Get(projectID string) (*models.Project, error) {
	return s.db.GetProject(projectID)
}

func (s *Service) Rename(projectID, name string) error {
	return s.db.UpdateProjectName(projectID, name)
}

func (s *Service) List() ([]*models.Project, error) {
	return s.db.ListProjects()
}

func (s *Service) Delete(projectID string) error {
	if err := s.db.DeleteProject(projectID); err != nil {
		return err
	}
	return os.RemoveAll(s.ProjectDir(projectID))
}

func (s *Service) SaveTimeline(projectID string, timeline *models.Timeline) error {
	return s.db.SaveTimeline(projectID, timeline)
}

func (s *Service) SaveTranscript(projectID string, transcript *models.Transcript) error {
	return s.db.SaveProjectTranscript(projectID, transcript)
}

func (s *Service) Transcript(projectID string) (*models.Transcript, error) {
	return s.db.GetProjectTranscript(projectID)
}

func (s *Service) Assets(projectID string) ([]*models.Asset, error) {
	return s.db.ListAssets(projectID)
}

func (s *Service) Asset(projectID, assetID string) (*models.Asset, error) {
	return s.db.GetAsset(projectID, assetID)
}

// Import copies an uploaded file into the project folder and derives preview metadata.
func (s *Service) Import(projectID string, src io.Reader, filename string) (*models.Asset, error) {
	if _, err := s.db.GetProject(projectID); err != nil {
		return nil, fmt.Errorf("project not found")
	}

	kind, err := classifyMedia(filename)
	if err != nil {
		return nil, err
	}

	assetID := fmt.Sprintf("asset_%d", time.Now().UnixNano())
	safe := sanitizeFileName(filename)
	storedName := assetID + "_" + safe
	assetPath := filepath.Join(s.assetsDir(projectID), storedName)

	if err := os.MkdirAll(s.assetsDir(projectID), 0755); err != nil {
		return nil, err
	}
	dest, err := os.Create(assetPath)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(dest, src); err != nil {
		dest.Close()
		_ = os.Remove(assetPath)
		return nil, err
	}
	dest.Close()

	return s.finishImport(projectID, assetID, storedName, assetPath, kind, filename)
}

// ImportPath imports a media file that already exists on disk (Tauri file dialog).
func (s *Service) ImportPath(projectID, sourcePath string) (*models.Asset, error) {
	file, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", sourcePath, err)
	}
	defer file.Close()
	return s.Import(projectID, file, filepath.Base(sourcePath))
}

func (s *Service) finishImport(projectID, assetID, storedName, assetPath, kind, originalName string) (*models.Asset, error) {
	info, err := s.ffmpeg.Probe(assetPath)
	if err != nil {
		_ = os.Remove(assetPath)
		return nil, err
	}

	asset := &models.Asset{
		ID:        assetID,
		ProjectID: projectID,
		Kind:      kind,
		Filename:  filepath.Base(originalName),
		Path:      assetPath,
		Duration:  info.Duration,
		Width:     info.Width,
		Height:    info.Height,
		FPS:       info.FPS,
		HasAudio:  info.HasAudio,
		Size:      info.Size,
	}

	if kind == models.AssetVideo || kind == models.AssetImage {
		thumbPath := filepath.Join(s.thumbsDir(projectID), assetID+".jpg")
		if err := s.ffmpeg.CreateThumbnail(assetPath, thumbPath, kind == models.AssetVideo); err != nil {
			logger.Error("[Project] thumbnail failed for %s: %v", storedName, err)
		} else {
			asset.ThumbPath = thumbPath
		}
	}

	if kind == models.AssetAudio || (kind == models.AssetVideo && info.HasAudio) {
		wavePath := filepath.Join(s.waveformsDir(projectID), assetID+".json")
		if info.Duration > 0 {
			if err := s.ffmpeg.ComputeWaveform(assetPath, wavePath, info.Duration, 1200); err != nil {
				logger.Error("[Project] waveform failed for %s: %v", storedName, err)
			} else {
				asset.WaveformPath = wavePath
			}
		}
	}

	if err := s.db.AddAsset(asset); err != nil {
		_ = os.Remove(assetPath)
		return nil, err
	}
	logger.Info("[Project] imported asset %s (%s, %.1fs)", assetID, asset.Filename, asset.Duration)
	return asset, nil
}

func (s *Service) DeleteAsset(projectID, assetID string) error {
	asset, err := s.db.GetAsset(projectID, assetID)
	if err != nil {
		return err
	}
	_ = os.Remove(asset.Path)
	_ = os.Remove(asset.ThumbPath)
	_ = os.Remove(asset.WaveformPath)
	return s.db.DeleteAsset(projectID, assetID)
}

// AssetThumbnail resolves the JPEG thumbnail for an asset, if any.
func (s *Service) AssetThumbnail(projectID, assetID string) (string, error) {
	asset, err := s.db.GetAsset(projectID, assetID)
	if err != nil {
		return "", err
	}
	if asset.ThumbPath == "" {
		return "", sql.ErrNoRows
	}
	if _, err := os.Stat(asset.ThumbPath); err != nil {
		return "", err
	}
	return asset.ThumbPath, nil
}

func (s *Service) AssetWaveform(projectID, assetID string) (string, error) {
	asset, err := s.db.GetAsset(projectID, assetID)
	if err != nil {
		return "", err
	}
	if asset.WaveformPath == "" {
		return "", sql.ErrNoRows
	}
	if _, err := os.Stat(asset.WaveformPath); err != nil {
		return "", err
	}
	return asset.WaveformPath, nil
}

func (s *Service) ResolveFont(family, file string) string {
	return s.fonts.Resolve(family, file)
}

func classifyMedia(filename string) (string, error) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".mp4", ".mkv", ".mov", ".avi", ".webm", ".m4v", ".wmv", ".flv":
		return models.AssetVideo, nil
	case ".wav", ".mp3", ".m4a", ".aac", ".flac", ".ogg", ".wma", ".aiff":
		return models.AssetAudio, nil
	case ".png", ".jpg", ".jpeg", ".webp", ".bmp", ".gif", ".tif", ".tiff":
		return models.AssetImage, nil
	default:
		return "", fmt.Errorf("format tidak didukung: %s", filepath.Ext(filename))
	}
}

func sanitizeFileName(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == ".." {
		return "media"
	}
	invalid := `<>:"/\|?*`
	var b strings.Builder
	for _, r := range base {
		if r < 32 || strings.ContainsRune(invalid, r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	clean := strings.Trim(b.String(), " .")
	if clean == "" {
		clean = "media"
	}
	// Keep the extension but cap long names for Windows path limits.
	ext := filepath.Ext(clean)
	stem := strings.TrimSuffix(clean, ext)
	if len([]rune(stem)) > 80 {
		stem = string([]rune(stem)[:80])
	}
	return stem + ext
}
