package database

import (
	"context"
	"fmt"
	"time"
)

// HeatmapBucket represents aggregated activity data for a single
// day-of-week + hour-of-day cell in a 7x24 heatmap grid.
// Used to visualize when a channel or guild is most active.
type HeatmapBucket struct {
	DayOfWeek    int     `json:"day_of_week"` // 0=Sunday, 6=Saturday
	HourOfDay    int     `json:"hour_of_day"` // 0-23
	AvgMessages  float64 `json:"avg_messages"`
	PeakMessages int     `json:"peak_messages"`
}

// RecordActivity upserts an hourly activity bucket for a channel.
// If a record already exists for the channel+hour combination, the
// message count and unique authors are added to the existing values.
// The hour should be truncated to the start of the hour (e.g. 14:00:00).
func (db *DB) RecordActivity(ctx context.Context, channelID string, hour time.Time, msgCount, uniqueAuthors int) error {
	// Truncate to the exact hour to ensure consistent bucket keys.
	hourBucket := hour.Truncate(time.Hour)

	_, err := db.Pool.Exec(ctx, `
		INSERT INTO activity_hourly (channel_id, hour_bucket, message_count, unique_authors)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (channel_id, hour_bucket) DO UPDATE SET
			message_count  = activity_hourly.message_count + EXCLUDED.message_count,
			unique_authors = GREATEST(activity_hourly.unique_authors, EXCLUDED.unique_authors)`,
		channelID, hourBucket, msgCount, uniqueAuthors,
	)
	if err != nil {
		return fmt.Errorf("record activity for channel %s at %s: %w", channelID, hourBucket, err)
	}
	return nil
}

// GetHeatmap returns the 7x24 activity grid for a channel over the last
// N days. Each cell contains the average and peak message counts for that
// day-of-week + hour-of-day combination.
//
// PostgreSQL's EXTRACT(DOW ...) returns 0=Sunday through 6=Saturday.
// EXTRACT(HOUR ...) returns 0-23.
func (db *DB) GetHeatmap(ctx context.Context, channelID string, days int) ([]HeatmapBucket, error) {
	cutoff := time.Now().AddDate(0, 0, -days)

	rows, err := db.Pool.Query(ctx, `
		SELECT
			EXTRACT(DOW FROM hour_bucket)::int  AS day_of_week,
			EXTRACT(HOUR FROM hour_bucket)::int AS hour_of_day,
			AVG(message_count)::float8           AS avg_messages,
			MAX(message_count)::int              AS peak_messages
		FROM activity_hourly
		WHERE channel_id = $1 AND hour_bucket >= $2
		GROUP BY day_of_week, hour_of_day
		ORDER BY day_of_week, hour_of_day`,
		channelID, cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("get heatmap for channel %s: %w", channelID, err)
	}
	defer rows.Close()

	var buckets []HeatmapBucket
	for rows.Next() {
		var b HeatmapBucket
		if err := rows.Scan(
			&b.DayOfWeek, &b.HourOfDay,
			&b.AvgMessages, &b.PeakMessages,
		); err != nil {
			return nil, fmt.Errorf("scan heatmap row: %w", err)
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}

// GetGuildHeatmap aggregates activity across all channels in a guild
// over the last N days. Same 7x24 grid format as GetHeatmap but summed
// across channels.
func (db *DB) GetGuildHeatmap(ctx context.Context, guildID string, days int) ([]HeatmapBucket, error) {
	cutoff := time.Now().AddDate(0, 0, -days)

	rows, err := db.Pool.Query(ctx, `
		SELECT
			EXTRACT(DOW FROM a.hour_bucket)::int  AS day_of_week,
			EXTRACT(HOUR FROM a.hour_bucket)::int AS hour_of_day,
			AVG(a.message_count)::float8           AS avg_messages,
			MAX(a.message_count)::int              AS peak_messages
		FROM activity_hourly a
		JOIN channels c ON c.channel_id = a.channel_id
		WHERE c.guild_id = $1 AND a.hour_bucket >= $2
		GROUP BY day_of_week, hour_of_day
		ORDER BY day_of_week, hour_of_day`,
		guildID, cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("get guild heatmap for %s: %w", guildID, err)
	}
	defer rows.Close()

	var buckets []HeatmapBucket
	for rows.Next() {
		var b HeatmapBucket
		if err := rows.Scan(
			&b.DayOfWeek, &b.HourOfDay,
			&b.AvgMessages, &b.PeakMessages,
		); err != nil {
			return nil, fmt.Errorf("scan guild heatmap row: %w", err)
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}
