package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Reader is an anonymous viewer identified by a device fingerprint.
type Reader struct {
	ReaderID   uuid.UUID `json:"reader_id"`
	DeviceHash string    `json:"device_hash"`
	Tokens     int       `json:"tokens"`
	CreatedAt  time.Time `json:"created_at"`
}

// Vote records a reader spending tokens on a story choice.
type Vote struct {
	VoteID      uuid.UUID `json:"vote_id"`
	ReaderID    uuid.UUID `json:"reader_id"`
	ChapterID   string    `json:"chapter_id"`
	Choice      string    `json:"choice"`
	TokensSpent int       `json:"tokens_spent"`
	CreatedAt   time.Time `json:"created_at"`
}

// VoteTally summarizes votes for a single choice within a chapter.
type VoteTally struct {
	Choice      string `json:"choice"`
	TotalTokens int    `json:"total_tokens"`
	VoteCount   int    `json:"vote_count"`
}

// GetOrCreateReader returns the reader for a device hash, creating one if
// it doesn't exist (upsert on device_hash).
func (db *DB) GetOrCreateReader(ctx context.Context, deviceHash string) (*Reader, error) {
	var r Reader
	err := db.Pool.QueryRow(ctx, `
		INSERT INTO readers (device_hash)
		VALUES ($1)
		ON CONFLICT (device_hash) DO UPDATE SET device_hash = EXCLUDED.device_hash
		RETURNING reader_id, device_hash, tokens, created_at`,
		deviceHash,
	).Scan(&r.ReaderID, &r.DeviceHash, &r.Tokens, &r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get or create reader: %w", err)
	}
	return &r, nil
}

// GetReader retrieves a reader by ID.
func (db *DB) GetReader(ctx context.Context, id uuid.UUID) (*Reader, error) {
	var r Reader
	err := db.Pool.QueryRow(ctx, `
		SELECT reader_id, device_hash, tokens, created_at
		FROM readers WHERE reader_id = $1`, id,
	).Scan(&r.ReaderID, &r.DeviceHash, &r.Tokens, &r.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get reader %s: %w", id, err)
	}
	return &r, nil
}

// GrantTokens adds n tokens to a reader's balance. Returns the new balance.
func (db *DB) GrantTokens(ctx context.Context, readerID uuid.UUID, n int) (int, error) {
	var newBalance int
	err := db.Pool.QueryRow(ctx, `
		UPDATE readers SET tokens = tokens + $2
		WHERE reader_id = $1
		RETURNING tokens`, readerID, n,
	).Scan(&newBalance)
	if err != nil {
		return 0, fmt.Errorf("grant tokens to %s: %w", readerID, err)
	}
	return newBalance, nil
}

// CastVote spends tokens on a choice. Fails if the reader doesn't have
// enough tokens.
func (db *DB) CastVote(ctx context.Context, readerID uuid.UUID, chapterID, choice string, tokensSpent int) (*Vote, error) {
	if tokensSpent < 1 {
		return nil, fmt.Errorf("tokens_spent must be >= 1, got %d", tokensSpent)
	}

	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Deduct tokens (will fail if balance goes negative due to CHECK or
	// we check manually).
	var remaining int
	err = tx.QueryRow(ctx, `
		UPDATE readers SET tokens = tokens - $2
		WHERE reader_id = $1
		RETURNING tokens`, readerID, tokensSpent,
	).Scan(&remaining)
	if err != nil {
		return nil, fmt.Errorf("deduct tokens: %w", err)
	}
	if remaining < 0 {
		return nil, fmt.Errorf("insufficient tokens: would leave balance at %d", remaining)
	}

	var v Vote
	err = tx.QueryRow(ctx, `
		INSERT INTO votes (reader_id, chapter_id, choice, tokens_spent)
		VALUES ($1, $2, $3, $4)
		RETURNING vote_id, reader_id, chapter_id, choice, tokens_spent, created_at`,
		readerID, chapterID, choice, tokensSpent,
	).Scan(&v.VoteID, &v.ReaderID, &v.ChapterID, &v.Choice, &v.TokensSpent, &v.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert vote: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit vote tx: %w", err)
	}
	return &v, nil
}

// TallyVotes returns vote totals grouped by choice for a chapter.
func (db *DB) TallyVotes(ctx context.Context, chapterID string) ([]VoteTally, error) {
	rows, err := db.Pool.Query(ctx, `
		SELECT choice, SUM(tokens_spent) AS total_tokens, COUNT(*) AS vote_count
		FROM votes
		WHERE chapter_id = $1
		GROUP BY choice
		ORDER BY total_tokens DESC`, chapterID)
	if err != nil {
		return nil, fmt.Errorf("tally votes for %s: %w", chapterID, err)
	}
	defer rows.Close()

	var tallies []VoteTally
	for rows.Next() {
		var t VoteTally
		if err := rows.Scan(&t.Choice, &t.TotalTokens, &t.VoteCount); err != nil {
			return nil, fmt.Errorf("scan vote tally: %w", err)
		}
		tallies = append(tallies, t)
	}
	return tallies, rows.Err()
}
