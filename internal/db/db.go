package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"autocut/internal/models"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

func InitDB(storageDir string) (*DB, error) {
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage dir: %w", err)
	}

	dbPath := filepath.Join(storageDir, "autocut.db")
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	// Optimize SQLite for concurrent web usage
	conn.SetMaxOpenConns(1) // modernc sqlite single-writer recommended
	if _, err := conn.Exec(`PRAGMA journal_mode = WAL; PRAGMA foreign_keys = ON;`); err != nil {
		return nil, fmt.Errorf("failed to set pragma: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("failed to migrate db: %w", err)
	}

	return db, nil
}

func (d *DB) Close() error {
	if d.conn != nil {
		return d.conn.Close()
	}
	return nil
}

func (d *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS jobs (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		original_filename TEXT NOT NULL,
		media_type TEXT DEFAULT 'video',
		raw_video_path TEXT,
		vo_path TEXT,
		vo_filename TEXT,
		has_vo INTEGER DEFAULT 0,
		audio_path TEXT,
		cut_video_path TEXT,
		final_video_path TEXT,
		status TEXT NOT NULL,
		current_step INTEGER DEFAULT 1,
		progress REAL DEFAULT 0.0,
		status_message TEXT,
		error_message TEXT,
		duration_original REAL DEFAULT 0.0,
		video_duration REAL DEFAULT 0.0,
		duration_final REAL DEFAULT 0.0,
		time_saved_percent REAL DEFAULT 0.0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS transcripts (
		job_id TEXT PRIMARY KEY,
		full_text TEXT NOT NULL,
		words_json TEXT NOT NULL,
		duration REAL DEFAULT 0.0,
		language TEXT DEFAULT 'id',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS cutlists (
		job_id TEXT PRIMARY KEY,
		segments_json TEXT NOT NULL,
		keep_intervals_json TEXT NOT NULL,
		total_cut_duration REAL DEFAULT 0.0,
		total_keep_duration REAL DEFAULT 0.0,
		qa_sanity_passed INTEGER DEFAULT 0,
		qa_report TEXT,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS metadata (
		job_id TEXT PRIMARY KEY,
		suggested_titles TEXT NOT NULL,
		description TEXT NOT NULL,
		chapters_json TEXT NOT NULL,
		tags_json TEXT NOT NULL,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS projects (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		width INTEGER NOT NULL DEFAULT 1920,
		height INTEGER NOT NULL DEFAULT 1080,
		fps REAL NOT NULL DEFAULT 30,
		timeline_json TEXT NOT NULL DEFAULT '{}',
		transcript_json TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS assets (
		id TEXT PRIMARY KEY,
		project_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		filename TEXT NOT NULL,
		path TEXT NOT NULL,
		duration REAL DEFAULT 0,
		width INTEGER DEFAULT 0,
		height INTEGER DEFAULT 0,
		fps REAL DEFAULT 0,
		has_audio INTEGER DEFAULT 0,
		thumb_path TEXT DEFAULT '',
		waveform_path TEXT DEFAULT '',
		size INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_assets_project ON assets(project_id);
	`
	if _, err := d.conn.Exec(schema); err != nil {
		return err
	}
	// Migration for existing databases. Ignore errors when the column already exists.
	_, _ = d.conn.Exec(`ALTER TABLE jobs ADD COLUMN media_type TEXT DEFAULT 'video';`)
	_, _ = d.conn.Exec(`ALTER TABLE jobs ADD COLUMN vo_path TEXT;`)
	_, _ = d.conn.Exec(`ALTER TABLE jobs ADD COLUMN vo_filename TEXT;`)
	_, _ = d.conn.Exec(`ALTER TABLE jobs ADD COLUMN has_vo INTEGER DEFAULT 0;`)
	_, _ = d.conn.Exec(`ALTER TABLE jobs ADD COLUMN video_duration REAL DEFAULT 0.0;`)
	_, _ = d.conn.Exec(`UPDATE jobs SET has_vo = 1, vo_path = raw_video_path, vo_filename = original_filename
		WHERE media_type = 'audio' AND (vo_path IS NULL OR vo_path = '');`)
	return nil
}

const jobSelectColumns = `id, title, original_filename, COALESCE(media_type, 'video'),
	COALESCE(raw_video_path, ''), COALESCE(audio_path, ''), COALESCE(cut_video_path, ''), COALESCE(final_video_path, ''),
	status, current_step, progress, COALESCE(status_message, ''), COALESCE(error_message, ''),
	duration_original, duration_final, time_saved_percent,
	COALESCE(vo_path, ''), COALESCE(vo_filename, ''), COALESCE(has_vo, 0), COALESCE(video_duration, 0),
	created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(sc rowScanner) (*models.Job, error) {
	var j models.Job
	var errStr string
	var hasVo int
	err := sc.Scan(
		&j.ID, &j.Title, &j.OriginalFilename, &j.MediaType,
		&j.RawVideoPath, &j.AudioPath, &j.CutVideoPath, &j.FinalVideoPath,
		&j.Status, &j.CurrentStep, &j.Progress, &j.StatusMessage, &errStr,
		&j.DurationOriginal, &j.DurationFinal, &j.TimeSavedPercent,
		&j.VoPath, &j.VoFilename, &hasVo, &j.VideoDuration,
		&j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	j.ErrorMessage = errStr
	j.HasVo = hasVo == 1
	return &j, nil
}

func (d *DB) CreateJob(job *models.Job) error {
	query := `
	INSERT INTO jobs (id, title, original_filename, media_type, raw_video_path, vo_path, vo_filename, has_vo, audio_path, status, current_step, progress, status_message, duration_original, video_duration, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	now := time.Now()
	job.CreatedAt = now
	job.UpdatedAt = now
	if job.MediaType == "" {
		job.MediaType = "video"
	}
	hasVo := 0
	if job.HasVo {
		hasVo = 1
	}
	_, err := d.conn.Exec(query,
		job.ID, job.Title, job.OriginalFilename, job.MediaType, job.RawVideoPath, job.VoPath, job.VoFilename, hasVo, job.AudioPath,
		job.Status, job.CurrentStep, job.Progress, job.StatusMessage, job.DurationOriginal, job.VideoDuration, job.CreatedAt, job.UpdatedAt,
	)
	return err
}

func (d *DB) GetJob(id string) (*models.Job, error) {
	query := `SELECT ` + jobSelectColumns + ` FROM jobs WHERE id = ?`
	return scanJob(d.conn.QueryRow(query, id))
}

func (d *DB) ListJobs() ([]models.Job, error) {
	query := `SELECT ` + jobSelectColumns + ` FROM jobs ORDER BY created_at DESC LIMIT 50`
	rows, err := d.conn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []models.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *j)
	}
	return jobs, nil
}

func (d *DB) UpdateJobProgress(id string, status models.JobStatus, step int, progress float64, message string) error {
	query := `UPDATE jobs SET status = ?, current_step = ?, progress = ?, status_message = ?, updated_at = ? WHERE id = ?`
	_, err := d.conn.Exec(query, status, step, progress, message, time.Now(), id)
	return err
}

// ClaimJob moves a job from one of the expected statuses to next. False means another request won the race.
func (d *DB) ClaimJob(id string, expected []models.JobStatus, next models.JobStatus, message string) (bool, error) {
	if len(expected) == 0 {
		return false, fmt.Errorf("no expected status")
	}
	placeholders := make([]string, len(expected))
	args := []any{next, message, time.Now(), id}
	for i, status := range expected {
		placeholders[i] = "?"
		args = append(args, status)
	}
	query := `UPDATE jobs SET status = ?, status_message = ?, progress = 1, error_message = '', updated_at = ?
		WHERE id = ? AND status IN (` + strings.Join(placeholders, ",") + `)`
	res, err := d.conn.Exec(query, args...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (d *DB) UpdateJobVo(id, voPath, voFilename string, audioOnly bool) error {
	if audioOnly {
		query := `UPDATE jobs SET vo_path = ?, vo_filename = ?, has_vo = 1, original_filename = ?, updated_at = ? WHERE id = ?`
		_, err := d.conn.Exec(query, voPath, voFilename, voFilename, time.Now(), id)
		return err
	}
	query := `UPDATE jobs SET vo_path = ?, vo_filename = ?, has_vo = 1, updated_at = ? WHERE id = ?`
	_, err := d.conn.Exec(query, voPath, voFilename, time.Now(), id)
	return err
}

func (d *DB) UpdateJobVideo(id, rawPath, filename string, videoDuration float64) error {
	query := `UPDATE jobs SET media_type = 'video', raw_video_path = ?, original_filename = ?, video_duration = ?, updated_at = ? WHERE id = ?`
	_, err := d.conn.Exec(query, rawPath, filename, videoDuration, time.Now(), id)
	return err
}

func (d *DB) UpdateJobDurations(id string, original, video float64) error {
	query := `UPDATE jobs SET duration_original = ?, video_duration = ?, updated_at = ? WHERE id = ?`
	_, err := d.conn.Exec(query, original, video, time.Now(), id)
	return err
}

// MarkCutStale drops a previous cut because the narration changed.
func (d *DB) MarkCutStale(id string) error {
	query := `UPDATE jobs SET cut_video_path = '', final_video_path = '', audio_path = '',
		duration_final = 0, time_saved_percent = 0, progress = 0, current_step = 1,
		status = CASE WHEN COALESCE(has_vo, 0) = 1 THEN ? ELSE ? END,
		status_message = 'Voice-over diganti. Jalankan auto cut lagi.', error_message = '', updated_at = ?
		WHERE id = ?`
	if _, err := d.conn.Exec(query, models.StatusPending, models.StatusAwaitingVo, time.Now(), id); err != nil {
		return err
	}
	if _, err := d.conn.Exec(`DELETE FROM transcripts WHERE job_id = ?`, id); err != nil {
		return err
	}
	_, err := d.conn.Exec(`DELETE FROM cutlists WHERE job_id = ?`, id)
	return err
}

// ClearRenderedMedia drops burned and cut files but keeps the cutlist, so render can run again.
func (d *DB) ClearRenderedMedia(id string) error {
	query := `UPDATE jobs SET cut_video_path = '', final_video_path = '', duration_final = 0, time_saved_percent = 0,
		status = ?, status_message = 'Video diganti. Render ulang untuk menerapkan cutlist ke gambar baru.',
		progress = 0, error_message = '', updated_at = ? WHERE id = ?`
	_, err := d.conn.Exec(query, models.StatusCutReady, time.Now(), id)
	return err
}

func (d *DB) UpdateJobCutReady(id, cutPath, finalPath, audioPath string, durOriginal, durFinal, savedPercent, videoDur float64) error {
	query := `UPDATE jobs SET status = ?, cut_video_path = ?, final_video_path = ?, audio_path = ?,
		duration_original = ?, duration_final = ?, time_saved_percent = ?, video_duration = ?,
		progress = 100.0, current_step = 9, error_message = '',
		status_message = 'Auto cut selesai. Periksa cutlist, lalu jalankan auto caption.', updated_at = ? WHERE id = ?`
	_, err := d.conn.Exec(query, models.StatusCutReady, cutPath, finalPath, audioPath, durOriginal, durFinal, savedPercent, videoDur, time.Now(), id)
	return err
}

func (d *DB) UpdateJobCaptioned(id, finalPath string) error {
	query := `UPDATE jobs SET status = ?, final_video_path = ?, progress = 100.0, current_step = 2,
		error_message = '', status_message = 'Auto caption selesai.', updated_at = ? WHERE id = ?`
	_, err := d.conn.Exec(query, models.StatusCompleted, finalPath, time.Now(), id)
	return err
}

func (d *DB) UpdateJobError(id string, errMsg string) error {
	query := `UPDATE jobs SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`
	_, err := d.conn.Exec(query, models.StatusFailed, errMsg, time.Now(), id)
	return err
}

func (d *DB) SaveTranscript(t *models.Transcript) error {
	wordsJSON, err := json.Marshal(t.Words)
	if err != nil {
		return err
	}
	query := `INSERT OR REPLACE INTO transcripts (job_id, full_text, words_json, duration, language, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`
	_, err = d.conn.Exec(query, t.JobID, t.FullText, string(wordsJSON), t.Duration, t.Language, time.Now())
	return err
}

func (d *DB) GetTranscript(jobID string) (*models.Transcript, error) {
	query := `SELECT job_id, full_text, words_json, duration, language, created_at FROM transcripts WHERE job_id = ?`
	row := d.conn.QueryRow(query, jobID)

	var t models.Transcript
	var wordsJSON string
	err := row.Scan(&t.JobID, &t.FullText, &wordsJSON, &t.Duration, &t.Language, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(wordsJSON), &t.Words); err != nil {
		return nil, err
	}
	return &t, nil
}

func (d *DB) SaveCutlist(c *models.Cutlist) error {
	segJSON, err := json.Marshal(c.Segments)
	if err != nil {
		return err
	}
	keepJSON, err := json.Marshal(c.KeepIntervals)
	if err != nil {
		return err
	}
	qaPassed := 0
	if c.QASanityPassed {
		qaPassed = 1
	}

	query := `INSERT OR REPLACE INTO cutlists (job_id, segments_json, keep_intervals_json, total_cut_duration, total_keep_duration, qa_sanity_passed, qa_report, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err = d.conn.Exec(query, c.JobID, string(segJSON), string(keepJSON), c.TotalCutDuration, c.TotalKeepDuration, qaPassed, c.QAReport, time.Now())
	return err
}

func (d *DB) GetCutlist(jobID string) (*models.Cutlist, error) {
	query := `SELECT job_id, segments_json, keep_intervals_json, total_cut_duration, total_keep_duration, qa_sanity_passed, COALESCE(qa_report, ''), updated_at
		FROM cutlists WHERE job_id = ?`
	row := d.conn.QueryRow(query, jobID)

	var c models.Cutlist
	var segJSON, keepJSON, qaReport string
	var qaPassed int
	err := row.Scan(&c.JobID, &segJSON, &keepJSON, &c.TotalCutDuration, &c.TotalKeepDuration, &qaPassed, &qaReport, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(segJSON), &c.Segments)
	_ = json.Unmarshal([]byte(keepJSON), &c.KeepIntervals)
	c.QASanityPassed = qaPassed == 1
	c.QAReport = qaReport
	return &c, nil
}

func (d *DB) SaveMetadata(m *models.YouTubeMetadata) error {
	titlesJSON, err := json.Marshal(m.SuggestedTitles)
	if err != nil {
		return err
	}
	chaptersJSON, err := json.Marshal(m.Chapters)
	if err != nil {
		return err
	}
	tagsJSON, err := json.Marshal(m.Tags)
	if err != nil {
		return err
	}
	query := `INSERT OR REPLACE INTO metadata (job_id, suggested_titles, description, chapters_json, tags_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`
	_, err = d.conn.Exec(query, m.JobID, string(titlesJSON), m.Description, string(chaptersJSON), string(tagsJSON), time.Now())
	return err
}

func (d *DB) GetMetadata(jobID string) (*models.YouTubeMetadata, error) {
	query := `SELECT job_id, suggested_titles, description, chapters_json, tags_json, updated_at FROM metadata WHERE job_id = ?`
	row := d.conn.QueryRow(query, jobID)

	var m models.YouTubeMetadata
	var titlesJSON, chaptersJSON, tagsJSON string
	err := row.Scan(&m.JobID, &titlesJSON, &m.Description, &chaptersJSON, &tagsJSON, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(titlesJSON), &m.SuggestedTitles)
	_ = json.Unmarshal([]byte(chaptersJSON), &m.Chapters)
	_ = json.Unmarshal([]byte(tagsJSON), &m.Tags)
	return &m, nil
}
