package db

import (
	"database/sql"
	"encoding/json"
	"time"

	"autocut/internal/models"
)

func (d *DB) CreateProject(p *models.Project) error {
	timelineJSON, err := json.Marshal(p.Timeline)
	if err != nil {
		return err
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	_, err = d.conn.Exec(
		`INSERT INTO projects (id, name, width, height, fps, timeline_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Width, p.Height, p.FPS, string(timelineJSON), now, now,
	)
	return err
}

const projectSelect = `id, name, width, height, fps, timeline_json, COALESCE(transcript_json, ''),
	created_at, updated_at`

func scanProject(sc rowScanner) (*models.Project, error) {
	var p models.Project
	var timelineJSON, transcriptJSON string
	if err := sc.Scan(&p.ID, &p.Name, &p.Width, &p.Height, &p.FPS, &timelineJSON, &transcriptJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	if timelineJSON != "" && timelineJSON != "{}" {
		_ = json.Unmarshal([]byte(timelineJSON), &p.Timeline)
	}
	if p.Timeline.Version == 0 {
		p.Timeline = models.NewTimeline(p.Width, p.Height, p.FPS)
	}
	if p.Timeline.Width == 0 {
		p.Timeline.Width = p.Width
		p.Timeline.Height = p.Height
		p.Timeline.FPS = p.FPS
	}
	p.HasTranscript = transcriptJSON != ""
	return &p, nil
}

func (d *DB) GetProject(id string) (*models.Project, error) {
	return scanProject(d.conn.QueryRow(`SELECT `+projectSelect+` FROM projects WHERE id = ?`, id))
}

func (d *DB) ListProjects() ([]*models.Project, error) {
	rows, err := d.conn.Query(`SELECT ` + projectSelect + ` FROM projects ORDER BY updated_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	projects := []*models.Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (d *DB) UpdateProjectName(id, name string) error {
	_, err := d.conn.Exec(`UPDATE projects SET name = ?, updated_at = ? WHERE id = ?`, name, time.Now(), id)
	return err
}

func (d *DB) SaveTimeline(projectID string, t *models.Timeline) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = d.conn.Exec(`UPDATE projects SET timeline_json = ?, updated_at = ? WHERE id = ?`, string(data), time.Now(), projectID)
	return err
}

func (d *DB) DeleteProject(id string) error {
	_, err := d.conn.Exec(`DELETE FROM projects WHERE id = ?`, id)
	return err
}

func (d *DB) SaveProjectTranscript(projectID string, tr *models.Transcript) error {
	data, err := json.Marshal(tr)
	if err != nil {
		return err
	}
	_, err = d.conn.Exec(`UPDATE projects SET transcript_json = ?, updated_at = ? WHERE id = ?`, string(data), time.Now(), projectID)
	return err
}

func (d *DB) GetProjectTranscript(projectID string) (*models.Transcript, error) {
	var data string
	err := d.conn.QueryRow(`SELECT COALESCE(transcript_json, '') FROM projects WHERE id = ?`, projectID).Scan(&data)
	if err != nil {
		return nil, err
	}
	if data == "" {
		return nil, sql.ErrNoRows
	}
	var tr models.Transcript
	if err := json.Unmarshal([]byte(data), &tr); err != nil {
		return nil, err
	}
	return &tr, nil
}

func (d *DB) AddAsset(a *models.Asset) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	hasAudio := 0
	if a.HasAudio {
		hasAudio = 1
	}
	_, err := d.conn.Exec(
		`INSERT INTO assets (id, project_id, kind, filename, path, duration, width, height, fps, has_audio, thumb_path, waveform_path, size, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.ProjectID, a.Kind, a.Filename, a.Path, a.Duration, a.Width, a.Height, a.FPS, hasAudio,
		a.ThumbPath, a.WaveformPath, a.Size, a.CreatedAt,
	)
	return err
}

const assetSelect = `id, project_id, kind, filename, path, duration, width, height, fps, COALESCE(has_audio, 0),
	COALESCE(thumb_path, ''), COALESCE(waveform_path, ''), size, created_at`

func scanAsset(sc rowScanner) (*models.Asset, error) {
	var a models.Asset
	var hasAudio int
	if err := sc.Scan(&a.ID, &a.ProjectID, &a.Kind, &a.Filename, &a.Path, &a.Duration, &a.Width, &a.Height,
		&a.FPS, &hasAudio, &a.ThumbPath, &a.WaveformPath, &a.Size, &a.CreatedAt); err != nil {
		return nil, err
	}
	a.HasAudio = hasAudio == 1
	return &a, nil
}

func (d *DB) ListAssets(projectID string) ([]*models.Asset, error) {
	rows, err := d.conn.Query(`SELECT `+assetSelect+` FROM assets WHERE project_id = ? ORDER BY created_at ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	assets := []*models.Asset{}
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		assets = append(assets, a)
	}
	return assets, rows.Err()
}

func (d *DB) GetAsset(projectID, assetID string) (*models.Asset, error) {
	return scanAsset(d.conn.QueryRow(`SELECT `+assetSelect+` FROM assets WHERE project_id = ? AND id = ?`, projectID, assetID))
}

func (d *DB) DeleteAsset(projectID, assetID string) error {
	_, err := d.conn.Exec(`DELETE FROM assets WHERE project_id = ? AND id = ?`, projectID, assetID)
	return err
}
