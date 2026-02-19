package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite connection.
type DB struct {
	conn *sql.DB
}

// Feed represents a calendar feed with a unique subscription token.
type Feed struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Token     string    `json:"token"` // unguessable token for subscription URL
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Event represents a single calendar event within a feed.
type Event struct {
	ID          string     `json:"id"`
	FeedID      string     `json:"feed_id"`
	Summary     string     `json:"summary"`
	Description string     `json:"description"`
	Location    string     `json:"location"`
	URL         string     `json:"url"`
	Start       time.Time  `json:"start"`
	End         *time.Time `json:"end,omitempty"` // nil = no end time (all-day or point-in-time)
	AllDay      bool       `json:"all_day"`
	Deadline    *time.Time `json:"deadline,omitempty"` // optional deadline (used as DTSTART if set, with VALARM)
	Status      string     `json:"status"`             // TENTATIVE, CONFIRMED, CANCELLED
	Categories  string     `json:"categories"`         // comma-separated
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

const schema = `
CREATE TABLE IF NOT EXISTS feeds (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL,
	token      TEXT NOT NULL UNIQUE,
	created_at DATETIME NOT NULL DEFAULT (datetime('now')),
	updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS events (
	id          TEXT PRIMARY KEY,
	feed_id     TEXT NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
	summary     TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	location    TEXT NOT NULL DEFAULT '',
	url         TEXT NOT NULL DEFAULT '',
	start_time  DATETIME NOT NULL,
	end_time    DATETIME,
	all_day     BOOLEAN NOT NULL DEFAULT 0,
	deadline    DATETIME,
	status      TEXT NOT NULL DEFAULT 'CONFIRMED',
	categories  TEXT NOT NULL DEFAULT '',
	created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
	updated_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_events_feed_id ON events(feed_id);
CREATE INDEX IF NOT EXISTS idx_events_start   ON events(start_time);
CREATE INDEX IF NOT EXISTS idx_feeds_token    ON feeds(token);
`

// Open creates or opens the SQLite database at path and applies the schema.
func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &DB{conn: conn}, nil
}

// Close shuts down the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// --- Feed operations ---

// CreateFeed inserts a new feed.
func (db *DB) CreateFeed(f *Feed) error {
	_, err := db.conn.Exec(
		`INSERT INTO feeds (id, name, token, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		f.ID, f.Name, f.Token, f.CreatedAt, f.UpdatedAt,
	)
	return err
}

// FeedByToken looks up a feed by its subscription token.
func (db *DB) FeedByToken(token string) (*Feed, error) {
	f := &Feed{}
	err := db.conn.QueryRow(
		`SELECT id, name, token, created_at, updated_at FROM feeds WHERE token = ?`,
		token,
	).Scan(&f.ID, &f.Name, &f.Token, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// FeedByID looks up a feed by ID.
func (db *DB) FeedByID(id string) (*Feed, error) {
	f := &Feed{}
	err := db.conn.QueryRow(
		`SELECT id, name, token, created_at, updated_at FROM feeds WHERE id = ?`,
		id,
	).Scan(&f.ID, &f.Name, &f.Token, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// ListFeeds returns all feeds.
func (db *DB) ListFeeds() ([]*Feed, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, token, created_at, updated_at FROM feeds ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var feeds []*Feed
	for rows.Next() {
		f := &Feed{}
		if err := rows.Scan(&f.ID, &f.Name, &f.Token, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}

// DeleteFeed removes a feed and its events (CASCADE).
func (db *DB) DeleteFeed(id string) error {
	_, err := db.conn.Exec(`DELETE FROM feeds WHERE id = ?`, id)
	return err
}

// --- Event operations ---

// CreateEvent inserts a new event.
func (db *DB) CreateEvent(e *Event) error {
	_, err := db.conn.Exec(
		`INSERT INTO events (id, feed_id, summary, description, location, url, start_time, end_time, all_day, deadline, status, categories, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.FeedID, e.Summary, e.Description, e.Location, e.URL,
		e.Start, e.End, e.AllDay, e.Deadline, e.Status, e.Categories,
		e.CreatedAt, e.UpdatedAt,
	)
	return err
}

// UpdateEvent updates an existing event.
func (db *DB) UpdateEvent(e *Event) error {
	_, err := db.conn.Exec(
		`UPDATE events SET summary=?, description=?, location=?, url=?, start_time=?, end_time=?, all_day=?, deadline=?, status=?, categories=?, updated_at=?
		 WHERE id = ?`,
		e.Summary, e.Description, e.Location, e.URL,
		e.Start, e.End, e.AllDay, e.Deadline, e.Status, e.Categories,
		e.UpdatedAt, e.ID,
	)
	return err
}

// EventsByFeed returns all events for a feed, ordered by start time.
func (db *DB) EventsByFeed(feedID string) ([]*Event, error) {
	rows, err := db.conn.Query(
		`SELECT id, feed_id, summary, description, location, url, start_time, end_time, all_day, deadline, status, categories, created_at, updated_at
		 FROM events WHERE feed_id = ? ORDER BY start_time ASC`,
		feedID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		e := &Event{}
		if err := rows.Scan(
			&e.ID, &e.FeedID, &e.Summary, &e.Description, &e.Location, &e.URL,
			&e.Start, &e.End, &e.AllDay, &e.Deadline, &e.Status, &e.Categories,
			&e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// EventByID returns a single event.
func (db *DB) EventByID(id string) (*Event, error) {
	e := &Event{}
	err := db.conn.QueryRow(
		`SELECT id, feed_id, summary, description, location, url, start_time, end_time, all_day, deadline, status, categories, created_at, updated_at
		 FROM events WHERE id = ?`,
		id,
	).Scan(
		&e.ID, &e.FeedID, &e.Summary, &e.Description, &e.Location, &e.URL,
		&e.Start, &e.End, &e.AllDay, &e.Deadline, &e.Status, &e.Categories,
		&e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// DeleteEvent removes a single event.
func (db *DB) DeleteEvent(id string) error {
	_, err := db.conn.Exec(`DELETE FROM events WHERE id = ?`, id)
	return err
}
