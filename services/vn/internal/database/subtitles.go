package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Subtitle represents a text overlay with toggle-point visibility.
//
// The visibility model works like a flip-flop:
//   - initialize_visible determines the state at t=0
//   - Each timestamp in timestamps_visible toggles the state
//   - end_visible must match the final state after all toggles
//
// This is enforced by a CHECK constraint in PostgreSQL.
type Subtitle struct {
	SubtitleID        uuid.UUID  `json:"subtitle_id"`
	VideoID           uuid.UUID  `json:"video_id"`
	EventID           *uuid.UUID `json:"event_id,omitempty"`
	Text              string     `json:"text"`
	TimestampsVisible []float64  `json:"timestamps_visible"`
	InitializeVisible bool       `json:"initialize_visible"`
	EndVisible        bool       `json:"end_visible"`
	SortOrder         int        `json:"sort_order"`
	CreatedAt         time.Time  `json:"created_at"`
}

// CreateSubtitleParams contains fields for creating a subtitle.
type CreateSubtitleParams struct {
	VideoID           uuid.UUID
	EventID           *uuid.UUID
	Text              string
	TimestampsVisible []float64
	InitializeVisible bool
	EndVisible        bool
	SortOrder         int
}

// ValidateVisibility checks that the timestamp count is consistent with
// the initialize_visible/end_visible flags. Returns an error describing
// the violation, or nil if valid.
//
// Rules:
//
//	same init/end   → even count (includes 0)
//	different       → odd count
func ValidateVisibility(initVisible, endVisible bool, timestamps []float64) error {
	n := len(timestamps)
	if initVisible == endVisible {
		if n%2 != 0 {
			return fmt.Errorf(
				"init_visible=%v end_visible=%v requires even timestamp count, got %d",
				initVisible, endVisible, n)
		}
	} else {
		if n%2 != 1 {
			return fmt.Errorf(
				"init_visible=%v end_visible=%v requires odd timestamp count, got %d",
				initVisible, endVisible, n)
		}
	}
	return nil
}

// CreateSubtitle inserts a new subtitle. It validates visibility constraints
// before sending to the database (fail-fast rather than relying solely on
// the CHECK constraint).
func (db *DB) CreateSubtitle(ctx context.Context, p CreateSubtitleParams) (*Subtitle, error) {
	if err := ValidateVisibility(p.InitializeVisible, p.EndVisible, p.TimestampsVisible); err != nil {
		return nil, fmt.Errorf("invalid subtitle visibility: %w", err)
	}

	var s Subtitle
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO subtitles (video_id, event_id, text, timestamps_visible,
			initialize_visible, end_visible, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING subtitle_id, video_id, event_id, text, timestamps_visible,
			initialize_visible, end_visible, sort_order, created_at`,
		p.VideoID, p.EventID, p.Text, p.TimestampsVisible,
		p.InitializeVisible, p.EndVisible, p.SortOrder,
	).Scan(
		&s.SubtitleID, &s.VideoID, &s.EventID, &s.Text,
		&s.TimestampsVisible, &s.InitializeVisible, &s.EndVisible,
		&s.SortOrder, &s.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create subtitle: %w", err)
	}
	return &s, nil
}

// GetSubtitle retrieves a single subtitle by ID.
func (db *DB) GetSubtitle(ctx context.Context, id uuid.UUID) (*Subtitle, error) {
	var s Subtitle
	err := db.Pool.QueryRow(ctx, `
		SELECT subtitle_id, video_id, event_id, text, timestamps_visible,
			initialize_visible, end_visible, sort_order, created_at
		FROM subtitles WHERE subtitle_id = $1`, id,
	).Scan(
		&s.SubtitleID, &s.VideoID, &s.EventID, &s.Text,
		&s.TimestampsVisible, &s.InitializeVisible, &s.EndVisible,
		&s.SortOrder, &s.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get subtitle %s: %w", id, err)
	}
	return &s, nil
}

// GetSubtitlesByVideo returns all subtitles for a video, ordered by sort_order.
func (db *DB) GetSubtitlesByVideo(ctx context.Context, videoID uuid.UUID) ([]Subtitle, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT subtitle_id, video_id, event_id, text, timestamps_visible,
			initialize_visible, end_visible, sort_order, created_at
		FROM subtitles
		WHERE video_id = $1
		ORDER BY sort_order ASC`, videoID)
	if err != nil {
		return nil, fmt.Errorf("list subtitles for video %s: %w", videoID, err)
	}
	defer rows.Close()

	var subs []Subtitle
	for rows.Next() {
		var s Subtitle
		if err := rows.Scan(
			&s.SubtitleID, &s.VideoID, &s.EventID, &s.Text,
			&s.TimestampsVisible, &s.InitializeVisible, &s.EndVisible,
			&s.SortOrder, &s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan subtitle row: %w", err)
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

// UpdateSubtitle updates a subtitle's text, visibility, and sort order.
func (db *DB) UpdateSubtitle(ctx context.Context, id uuid.UUID, p CreateSubtitleParams) error {
	if err := ValidateVisibility(p.InitializeVisible, p.EndVisible, p.TimestampsVisible); err != nil {
		return fmt.Errorf("invalid subtitle visibility: %w", err)
	}

	tag, err := db.Pool.Exec(ctx, `
		UPDATE subtitles
		SET text = $2, event_id = $3, timestamps_visible = $4,
			initialize_visible = $5, end_visible = $6, sort_order = $7
		WHERE subtitle_id = $1`,
		id, p.Text, p.EventID, p.TimestampsVisible,
		p.InitializeVisible, p.EndVisible, p.SortOrder)
	if err != nil {
		return fmt.Errorf("update subtitle %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("subtitle %s not found", id)
	}
	return nil
}

// DeleteSubtitle removes a subtitle by ID.
func (db *DB) DeleteSubtitle(ctx context.Context, id uuid.UUID) error {
	tag, err := db.Pool.Exec(ctx, `DELETE FROM subtitles WHERE subtitle_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete subtitle %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("subtitle %s not found", id)
	}
	return nil
}
