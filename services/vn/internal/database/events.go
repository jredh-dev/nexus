package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SignificantEvent marks a notable moment (or moments) within a video.
type SignificantEvent struct {
	EventID     uuid.UUID `json:"event_id"`
	VideoID     uuid.UUID `json:"video_id"`
	Description string    `json:"description"`
	Timestamps  []float64 `json:"timestamps"`
	IsVisible   bool      `json:"is_visible"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateEventParams contains fields for creating a significant event.
type CreateEventParams struct {
	VideoID     uuid.UUID
	Description string
	Timestamps  []float64
	IsVisible   bool
}

// CreateEvent inserts a new significant event.
func (db *DB) CreateEvent(ctx context.Context, p CreateEventParams) (*SignificantEvent, error) {
	var e SignificantEvent
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO significant_events (video_id, description, timestamps, is_visible)
		VALUES ($1, $2, $3, $4)
		RETURNING event_id, video_id, description, timestamps, is_visible, created_at`,
		p.VideoID, p.Description, p.Timestamps, p.IsVisible,
	).Scan(&e.EventID, &e.VideoID, &e.Description, &e.Timestamps, &e.IsVisible, &e.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create event: %w", err)
	}
	return &e, nil
}

// GetEvent retrieves a single event by ID.
func (db *DB) GetEvent(ctx context.Context, id uuid.UUID) (*SignificantEvent, error) {
	var e SignificantEvent
	err := db.Pool.QueryRow(ctx, `
		SELECT event_id, video_id, description, timestamps, is_visible, created_at
		FROM significant_events WHERE event_id = $1`, id,
	).Scan(&e.EventID, &e.VideoID, &e.Description, &e.Timestamps, &e.IsVisible, &e.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get event %s: %w", id, err)
	}
	return &e, nil
}

// GetEventsByVideo returns all events for a video, ordered by first timestamp.
func (db *DB) GetEventsByVideo(ctx context.Context, videoID uuid.UUID) ([]SignificantEvent, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT event_id, video_id, description, timestamps, is_visible, created_at
		FROM significant_events
		WHERE video_id = $1
		ORDER BY timestamps[1] ASC NULLS LAST`, videoID)
	if err != nil {
		return nil, fmt.Errorf("list events for video %s: %w", videoID, err)
	}
	defer rows.Close()

	var events []SignificantEvent
	for rows.Next() {
		var e SignificantEvent
		if err := rows.Scan(&e.EventID, &e.VideoID, &e.Description, &e.Timestamps, &e.IsVisible, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// UpdateEvent updates an event's description, timestamps, and visibility.
func (db *DB) UpdateEvent(ctx context.Context, id uuid.UUID, description string, timestamps []float64, isVisible bool) error {
	tag, err := db.Pool.Exec(ctx, `
		UPDATE significant_events
		SET description = $2, timestamps = $3, is_visible = $4
		WHERE event_id = $1`, id, description, timestamps, isVisible)
	if err != nil {
		return fmt.Errorf("update event %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("event %s not found", id)
	}
	return nil
}

// DeleteEvent removes an event by ID.
func (db *DB) DeleteEvent(ctx context.Context, id uuid.UUID) error {
	tag, err := db.Pool.Exec(ctx, `DELETE FROM significant_events WHERE event_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete event %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("event %s not found", id)
	}
	return nil
}
