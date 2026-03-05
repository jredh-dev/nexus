package database

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Video represents a video stored as a PostgreSQL large object.
type Video struct {
	VideoID             uuid.UUID `json:"video_id"`
	Name                string    `json:"name"`
	Codec               string    `json:"codec"`
	MimeType            string    `json:"mime_type"`
	DurationMS          int       `json:"duration_ms"`
	Width               *int      `json:"width,omitempty"`
	Height              *int      `json:"height,omitempty"`
	LoopType            string    `json:"loop_type"`
	HasSubtitlesAlready bool      `json:"has_subtitles_already"`
	DataOID             uint32    `json:"-"`
	SizeBytes           int64     `json:"size_bytes"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// ImportVideoParams contains the metadata for importing a new video.
type ImportVideoParams struct {
	Name                string
	Codec               string
	MimeType            string
	DurationMS          int
	Width               *int
	Height              *int
	LoopType            string
	HasSubtitlesAlready bool
}

// ImportVideo writes video data into a PostgreSQL large object and records
// metadata in the videos table. The entire operation runs in a single
// transaction so the large object and row are created atomically.
func (db *DB) ImportVideo(ctx context.Context, params ImportVideoParams, data io.Reader) (*Video, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	lobs := tx.LargeObjects()

	oid, err := lobs.Create(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("create large object: %w", err)
	}

	lo, err := lobs.Open(ctx, oid, pgx.LargeObjectModeWrite)
	if err != nil {
		return nil, fmt.Errorf("open large object for write: %w", err)
	}

	n, err := io.Copy(lo, data)
	if err != nil {
		return nil, fmt.Errorf("write large object data: %w", err)
	}

	if err := lo.Close(); err != nil {
		return nil, fmt.Errorf("close large object: %w", err)
	}

	loopType := params.LoopType
	if loopType == "" {
		loopType = "none"
	}

	var v Video
	err = tx.QueryRow(ctx, `
		INSERT INTO videos (name, codec, mime_type, duration_ms, width, height,
			loop_type, has_subtitles_already, data_oid, size_bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING video_id, name, codec, mime_type, duration_ms, width, height,
			loop_type, has_subtitles_already, data_oid, size_bytes, created_at, updated_at`,
		params.Name, params.Codec, params.MimeType, params.DurationMS,
		toNullInt(params.Width), toNullInt(params.Height),
		loopType, params.HasSubtitlesAlready, oid, n,
	).Scan(
		&v.VideoID, &v.Name, &v.Codec, &v.MimeType, &v.DurationMS,
		&v.Width, &v.Height, &v.LoopType, &v.HasSubtitlesAlready,
		&v.DataOID, &v.SizeBytes, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert video row: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return &v, nil
}

// GetVideo retrieves video metadata by ID (does not read large object data).
func (db *DB) GetVideo(ctx context.Context, id uuid.UUID) (*Video, error) {
	var v Video
	err := db.Pool.QueryRow(ctx, `
		SELECT video_id, name, codec, mime_type, duration_ms, width, height,
			loop_type, has_subtitles_already, data_oid, size_bytes, created_at, updated_at
		FROM videos WHERE video_id = $1`, id,
	).Scan(
		&v.VideoID, &v.Name, &v.Codec, &v.MimeType, &v.DurationMS,
		&v.Width, &v.Height, &v.LoopType, &v.HasSubtitlesAlready,
		&v.DataOID, &v.SizeBytes, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get video %s: %w", id, err)
	}
	return &v, nil
}

// ListVideos returns all videos ordered by creation time (newest first).
func (db *DB) ListVideos(ctx context.Context) ([]Video, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT video_id, name, codec, mime_type, duration_ms, width, height,
			loop_type, has_subtitles_already, data_oid, size_bytes, created_at, updated_at
		FROM videos ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list videos: %w", err)
	}
	defer rows.Close()

	var videos []Video
	for rows.Next() {
		var v Video
		if err := rows.Scan(
			&v.VideoID, &v.Name, &v.Codec, &v.MimeType, &v.DurationMS,
			&v.Width, &v.Height, &v.LoopType, &v.HasSubtitlesAlready,
			&v.DataOID, &v.SizeBytes, &v.CreatedAt, &v.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan video row: %w", err)
		}
		videos = append(videos, v)
	}
	return videos, rows.Err()
}

// ReadVideoData opens the large object for a video and calls fn with the
// reader. The caller should NOT close the reader; it is closed automatically
// when fn returns. The read happens inside a transaction.
func (db *DB) ReadVideoData(ctx context.Context, id uuid.UUID, fn func(r io.Reader, sizeBytes int64) error) error {
	v, err := db.GetVideo(ctx, id)
	if err != nil {
		return err
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	lobs := tx.LargeObjects()
	lo, err := lobs.Open(ctx, v.DataOID, pgx.LargeObjectModeRead)
	if err != nil {
		return fmt.Errorf("open large object: %w", err)
	}

	if err := fn(lo, v.SizeBytes); err != nil {
		lo.Close() //nolint:errcheck
		return err
	}

	if err := lo.Close(); err != nil {
		return fmt.Errorf("close large object: %w", err)
	}
	return tx.Commit(ctx)
}

// DeleteVideo removes a video and its large object data.
func (db *DB) DeleteVideo(ctx context.Context, id uuid.UUID) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Get the OID before deleting the row.
	var oid uint32
	err = tx.QueryRow(ctx, `DELETE FROM videos WHERE video_id = $1 RETURNING data_oid`, id).Scan(&oid)
	if err != nil {
		return fmt.Errorf("delete video row: %w", err)
	}

	// Remove the large object.
	lobs := tx.LargeObjects()
	if err := lobs.Unlink(ctx, oid); err != nil {
		return fmt.Errorf("unlink large object: %w", err)
	}

	return tx.Commit(ctx)
}

// toNullInt converts *int to pgtype.Int4 for nullable integer columns.
func toNullInt(p *int) pgtype.Int4 {
	if p == nil {
		return pgtype.Int4{Valid: false}
	}
	return pgtype.Int4{Int32: int32(*p), Valid: true}
}
