// Package db manages the ref service database: schema migrations and CRUD
// for prompts. Uses pgx/v5 with a connection pool.
//
// Schema overview:
//
//	prompts
//	  id             serial primary key
//	  title          text not null unique   -- short label for display
//	  prompt         text not null          -- full prompt text sent to OpenCode
//	  mode           text not null          -- batch | review | inactive
//	  response       text                   -- last response from OpenCode (nullable)
//	  run_count      int not null default 0 -- total successful runs
//	  no_change_count int not null default 0 -- consecutive runs with identical response
//	  created_at     timestamptz not null default now()
//	  updated_at     timestamptz not null default now()
//	  last_run_at    timestamptz            -- nullable; set after each completed run
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Prompt is a single entry in the queue.
type Prompt struct {
	ID            int        `json:"id"`
	Title         string     `json:"title"`
	Prompt        string     `json:"prompt"`
	Mode          string     `json:"mode"` // batch | review | inactive
	Response      *string    `json:"response"`
	RunCount      int        `json:"run_count"`
	NoChangeCount int        `json:"no_change_count"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	LastRunAt     *time.Time `json:"last_run_at"`
}

// CreatePromptInput holds fields for creating a new prompt.
type CreatePromptInput struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
	// Mode is optional; defaults to "batch".
	Mode string `json:"mode"`
}

// UpdatePromptInput holds fields for updating an existing prompt.
// Only non-empty fields are updated.
type UpdatePromptInput struct {
	Title  *string `json:"title"`
	Prompt *string `json:"prompt"`
	Mode   *string `json:"mode"`
}

// Migrate runs DDL migrations in a transaction. Idempotent — safe to call
// on every startup. New migrations are appended to the slice.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrations := []string{
		// v1: initial schema
		`CREATE TABLE IF NOT EXISTS prompts (
			id              SERIAL PRIMARY KEY,
			title           TEXT NOT NULL,
			prompt          TEXT NOT NULL,
			mode            TEXT NOT NULL DEFAULT 'batch',
			response        TEXT,
			run_count       INT  NOT NULL DEFAULT 0,
			no_change_count INT  NOT NULL DEFAULT 0,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			last_run_at     TIMESTAMPTZ
		)`,
		// Unique constraint on title so CLI lookups by name are unambiguous.
		`DO $$ BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM pg_constraint
				WHERE conname = 'prompts_title_key'
			) THEN
				ALTER TABLE prompts ADD CONSTRAINT prompts_title_key UNIQUE (title);
			END IF;
		END $$`,
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, m := range migrations {
		if _, err := tx.Exec(ctx, m); err != nil {
			return fmt.Errorf("migration: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// Create inserts a new prompt and returns it with the generated ID.
func Create(ctx context.Context, pool *pgxpool.Pool, in CreatePromptInput) (Prompt, error) {
	mode := in.Mode
	if mode == "" {
		mode = "batch"
	}
	if err := validateMode(mode); err != nil {
		return Prompt{}, err
	}

	var p Prompt
	err := pool.QueryRow(ctx,
		`INSERT INTO prompts (title, prompt, mode)
		 VALUES ($1, $2, $3)
		 RETURNING id, title, prompt, mode, response, run_count, no_change_count,
		           created_at, updated_at, last_run_at`,
		in.Title, in.Prompt, mode,
	).Scan(
		&p.ID, &p.Title, &p.Prompt, &p.Mode, &p.Response,
		&p.RunCount, &p.NoChangeCount,
		&p.CreatedAt, &p.UpdatedAt, &p.LastRunAt,
	)
	if err != nil {
		return Prompt{}, fmt.Errorf("create prompt: %w", err)
	}
	return p, nil
}

// Get returns a single prompt by ID.
func Get(ctx context.Context, pool *pgxpool.Pool, id int) (Prompt, error) {
	var p Prompt
	err := pool.QueryRow(ctx,
		`SELECT id, title, prompt, mode, response, run_count, no_change_count,
		        created_at, updated_at, last_run_at
		 FROM prompts WHERE id = $1`,
		id,
	).Scan(
		&p.ID, &p.Title, &p.Prompt, &p.Mode, &p.Response,
		&p.RunCount, &p.NoChangeCount,
		&p.CreatedAt, &p.UpdatedAt, &p.LastRunAt,
	)
	if err == pgx.ErrNoRows {
		return Prompt{}, fmt.Errorf("prompt %d not found", id)
	}
	if err != nil {
		return Prompt{}, fmt.Errorf("get prompt: %w", err)
	}
	return p, nil
}

// List returns all prompts ordered by ID.
func List(ctx context.Context, pool *pgxpool.Pool) ([]Prompt, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, title, prompt, mode, response, run_count, no_change_count,
		        created_at, updated_at, last_run_at
		 FROM prompts ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list prompts: %w", err)
	}
	defer rows.Close()

	var out []Prompt
	for rows.Next() {
		var p Prompt
		if err := rows.Scan(
			&p.ID, &p.Title, &p.Prompt, &p.Mode, &p.Response,
			&p.RunCount, &p.NoChangeCount,
			&p.CreatedAt, &p.UpdatedAt, &p.LastRunAt,
		); err != nil {
			return nil, fmt.Errorf("scan prompt: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListByMode returns prompts filtered by mode.
func ListByMode(ctx context.Context, pool *pgxpool.Pool, mode string) ([]Prompt, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, title, prompt, mode, response, run_count, no_change_count,
		        created_at, updated_at, last_run_at
		 FROM prompts WHERE mode = $1 ORDER BY id`,
		mode,
	)
	if err != nil {
		return nil, fmt.Errorf("list prompts by mode: %w", err)
	}
	defer rows.Close()

	var out []Prompt
	for rows.Next() {
		var p Prompt
		if err := rows.Scan(
			&p.ID, &p.Title, &p.Prompt, &p.Mode, &p.Response,
			&p.RunCount, &p.NoChangeCount,
			&p.CreatedAt, &p.UpdatedAt, &p.LastRunAt,
		); err != nil {
			return nil, fmt.Errorf("scan prompt: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Update applies non-nil fields from in to the prompt with the given ID.
// Returns the updated prompt.
func Update(ctx context.Context, pool *pgxpool.Pool, id int, in UpdatePromptInput) (Prompt, error) {
	// Validate mode if provided.
	if in.Mode != nil {
		if err := validateMode(*in.Mode); err != nil {
			return Prompt{}, err
		}
	}

	// Build SET clause dynamically from non-nil fields.
	sets := []string{"updated_at = NOW()"}
	args := []any{}
	argIdx := 1

	if in.Title != nil {
		sets = append(sets, fmt.Sprintf("title = $%d", argIdx))
		args = append(args, *in.Title)
		argIdx++
	}
	if in.Prompt != nil {
		sets = append(sets, fmt.Sprintf("prompt = $%d", argIdx))
		args = append(args, *in.Prompt)
		argIdx++
	}
	if in.Mode != nil {
		sets = append(sets, fmt.Sprintf("mode = $%d", argIdx))
		args = append(args, *in.Mode)
		argIdx++
	}

	// ID is always the last arg.
	args = append(args, id)

	query := fmt.Sprintf(
		`UPDATE prompts SET %s WHERE id = $%d
		 RETURNING id, title, prompt, mode, response, run_count, no_change_count,
		           created_at, updated_at, last_run_at`,
		joinSets(sets), argIdx,
	)

	var p Prompt
	err := pool.QueryRow(ctx, query, args...).Scan(
		&p.ID, &p.Title, &p.Prompt, &p.Mode, &p.Response,
		&p.RunCount, &p.NoChangeCount,
		&p.CreatedAt, &p.UpdatedAt, &p.LastRunAt,
	)
	if err == pgx.ErrNoRows {
		return Prompt{}, fmt.Errorf("prompt %d not found", id)
	}
	if err != nil {
		return Prompt{}, fmt.Errorf("update prompt: %w", err)
	}
	return p, nil
}

// Delete removes a prompt by ID. Returns an error if not found.
func Delete(ctx context.Context, pool *pgxpool.Pool, id int) error {
	tag, err := pool.Exec(ctx, `DELETE FROM prompts WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete prompt: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("prompt %d not found", id)
	}
	return nil
}

// RecordRun updates run_count, last_run_at, and response after a successful
// OpenCode run. Also manages no_change_count: increments if response is
// identical to previous, resets to 0 otherwise. Automatically sets mode to
// "inactive" when no_change_count reaches the threshold (3).
func RecordRun(ctx context.Context, pool *pgxpool.Pool, id int, newResponse string) (Prompt, error) {
	const inactiveThreshold = 3

	// Use a transaction: read current response, update counters, maybe flip mode.
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Prompt{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var cur Prompt
	err = tx.QueryRow(ctx,
		`SELECT id, title, prompt, mode, response, run_count, no_change_count,
		        created_at, updated_at, last_run_at
		 FROM prompts WHERE id = $1 FOR UPDATE`,
		id,
	).Scan(
		&cur.ID, &cur.Title, &cur.Prompt, &cur.Mode, &cur.Response,
		&cur.RunCount, &cur.NoChangeCount,
		&cur.CreatedAt, &cur.UpdatedAt, &cur.LastRunAt,
	)
	if err == pgx.ErrNoRows {
		return Prompt{}, fmt.Errorf("prompt %d not found", id)
	}
	if err != nil {
		return Prompt{}, fmt.Errorf("lock prompt: %w", err)
	}

	// Determine no_change_count delta.
	noChange := cur.NoChangeCount
	if cur.Response != nil && *cur.Response == newResponse {
		noChange++
	} else {
		noChange = 0
	}

	// Flip to inactive after 3 consecutive no-change runs.
	mode := cur.Mode
	if noChange >= inactiveThreshold && mode != "inactive" {
		mode = "inactive"
	}

	var updated Prompt
	err = tx.QueryRow(ctx,
		`UPDATE prompts
		 SET response        = $1,
		     run_count       = run_count + 1,
		     no_change_count = $2,
		     mode            = $3,
		     last_run_at     = NOW(),
		     updated_at      = NOW()
		 WHERE id = $4
		 RETURNING id, title, prompt, mode, response, run_count, no_change_count,
		           created_at, updated_at, last_run_at`,
		newResponse, noChange, mode, id,
	).Scan(
		&updated.ID, &updated.Title, &updated.Prompt, &updated.Mode, &updated.Response,
		&updated.RunCount, &updated.NoChangeCount,
		&updated.CreatedAt, &updated.UpdatedAt, &updated.LastRunAt,
	)
	if err != nil {
		return Prompt{}, fmt.Errorf("record run: %w", err)
	}

	return updated, tx.Commit(ctx)
}

// validateMode checks that the mode string is one of the allowed values.
func validateMode(mode string) error {
	switch mode {
	case "batch", "review", "inactive":
		return nil
	default:
		return fmt.Errorf("invalid mode %q: must be batch, review, or inactive", mode)
	}
}

// joinSets concatenates set clauses with ", ".
func joinSets(sets []string) string {
	out := ""
	for i, s := range sets {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
