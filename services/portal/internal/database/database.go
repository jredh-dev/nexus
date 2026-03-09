// Package database provides PostgreSQL access for the portal service.
//
// It manages connection pooling via pgxpool, schema migrations, and CRUD
// operations for users, sessions, magic tokens, and email change tokens.
//
// All timestamps are stored as TIMESTAMPTZ and handled as time.Time natively
// by pgx/v5 — no manual parsing required.
package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jredh-dev/nexus/services/portal/pkg/models"
)

// DB wraps a pgxpool.Pool and provides portal domain operations.
type DB struct {
	pool *pgxpool.Pool
}

// New connects to PostgreSQL using connStr and runs schema migrations.
// connStr is a libpq-style DSN, e.g.:
//
//	"host=localhost port=5432 dbname=portal user=portal password=portal-dev-password"
func New(ctx context.Context, connStr string) (*DB, error) {
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("open portal database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping portal database: %w", err)
	}
	db := &DB{pool: pool}
	if err := db.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate portal database: %w", err)
	}
	return db, nil
}

// Close shuts down the connection pool.
func (db *DB) Close() {
	db.pool.Close()
}

// Pool returns the underlying pgxpool for callers that need direct pool access
// (e.g. pgbus.Publish).
func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

// migrate creates all tables and indexes if they do not exist.
// PostgreSQL supports IF NOT EXISTS for CREATE TABLE and CREATE INDEX, so
// this is idempotent and safe to run on every startup.
func (db *DB) migrate(ctx context.Context) error {
	const ddl = `
	CREATE TABLE IF NOT EXISTS users (
		id            TEXT PRIMARY KEY,
		username      TEXT UNIQUE NOT NULL DEFAULT '',
		email         TEXT UNIQUE NOT NULL,
		phone_number  TEXT NOT NULL DEFAULT '',
		name          TEXT NOT NULL DEFAULT '',
		role          TEXT NOT NULL DEFAULT 'user',
		password_hash TEXT NOT NULL,
		email_hash    TEXT NOT NULL DEFAULT '',
		phone_hash    TEXT NOT NULL DEFAULT '',
		created_at    TIMESTAMPTZ NOT NULL,
		updated_at    TIMESTAMPTZ NOT NULL,
		last_login_at TIMESTAMPTZ NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_users_username    ON users(username);
	CREATE INDEX IF NOT EXISTS idx_users_email_hash  ON users(email_hash);
	CREATE INDEX IF NOT EXISTS idx_users_phone_hash  ON users(phone_hash);

	CREATE TABLE IF NOT EXISTS sessions (
		id         TEXT PRIMARY KEY,
		user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		expires_at TIMESTAMPTZ NOT NULL,
		created_at TIMESTAMPTZ NOT NULL,
		ip_address TEXT NOT NULL DEFAULT '',
		user_agent TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_sessions_user_id    ON sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

	CREATE TABLE IF NOT EXISTS magic_tokens (
		id         TEXT PRIMARY KEY,
		user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		expires_at TIMESTAMPTZ NOT NULL,
		used_at    TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_magic_tokens_user_id ON magic_tokens(user_id);

	CREATE TABLE IF NOT EXISTS email_change_tokens (
		id         TEXT PRIMARY KEY,
		user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		new_email  TEXT NOT NULL,
		expires_at TIMESTAMPTZ NOT NULL,
		used_at    TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_email_change_tokens_user_id ON email_change_tokens(user_id);
	`
	_, err := db.pool.Exec(ctx, ddl)
	return err
}

// --- helpers ---

// userColumns is the SELECT column list for user queries.
const userColumns = `id, username, email, phone_number, name, role, password_hash, email_hash, phone_hash, created_at, updated_at, last_login_at`

// scanUser scans a pgx.Row or pgx.Rows into a User model.
// Returns (nil, nil) if no row was found.
func scanUser(row pgx.Row) (*models.User, error) {
	u := &models.User{}
	err := row.Scan(
		&u.ID, &u.Username, &u.Email, &u.PhoneNumber, &u.Name, &u.Role,
		&u.PasswordHash, &u.EmailHash, &u.PhoneHash,
		&u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

// --- User operations ---

// CreateUser inserts a new user.
func (db *DB) CreateUser(u *models.User) error {
	const q = `INSERT INTO users (id, username, email, phone_number, name, role, password_hash, email_hash, phone_hash, created_at, updated_at, last_login_at)
	           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	_, err := db.pool.Exec(context.Background(), q,
		u.ID, u.Username, u.Email, u.PhoneNumber, u.Name, u.Role,
		u.PasswordHash, u.EmailHash, u.PhoneHash,
		u.CreatedAt, u.UpdatedAt, u.LastLoginAt,
	)
	return err
}

// GetUserByEmail looks up a user by email.
func (db *DB) GetUserByEmail(email string) (*models.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE email = $1`
	return scanUser(db.pool.QueryRow(context.Background(), q, email))
}

// GetUserByID looks up a user by ID.
func (db *DB) GetUserByID(id string) (*models.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE id = $1`
	return scanUser(db.pool.QueryRow(context.Background(), q, id))
}

// GetUserByUsername looks up a user by username (case-insensitive).
func (db *DB) GetUserByUsername(username string) (*models.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE lower(username) = lower($1)`
	return scanUser(db.pool.QueryRow(context.Background(), q, username))
}

// GetUserByEmailHash looks up a user by normalized email hash.
func (db *DB) GetUserByEmailHash(hash string) (*models.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE email_hash = $1`
	return scanUser(db.pool.QueryRow(context.Background(), q, hash))
}

// GetUserByPhoneHash looks up a user by normalized phone hash.
func (db *DB) GetUserByPhoneHash(hash string) (*models.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE phone_hash = $1`
	return scanUser(db.pool.QueryRow(context.Background(), q, hash))
}

// UpdateLastLogin sets the last_login_at timestamp.
func (db *DB) UpdateLastLogin(userID string, t time.Time) error {
	const q = `UPDATE users SET last_login_at = $1, updated_at = $2 WHERE id = $3`
	_, err := db.pool.Exec(context.Background(), q, t, t, userID)
	return err
}

// UpdateUserRole sets the role for a user.
func (db *DB) UpdateUserRole(userID, role string) error {
	const q = `UPDATE users SET role = $1, updated_at = $2 WHERE id = $3`
	_, err := db.pool.Exec(context.Background(), q, role, time.Now(), userID)
	return err
}

// UpdateUserEmail updates a user's email and email hash.
func (db *DB) UpdateUserEmail(userID, newEmail, newEmailHash string) error {
	const q = `UPDATE users SET email = $1, email_hash = $2, updated_at = $3 WHERE id = $4`
	_, err := db.pool.Exec(context.Background(), q, newEmail, newEmailHash, time.Now(), userID)
	return err
}

// DeleteUser deletes a user by ID (cascades to sessions and tokens).
func (db *DB) DeleteUser(userID string) error {
	_, err := db.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	return err
}

// --- Session operations ---

// CreateSession inserts a new session.
func (db *DB) CreateSession(s *models.Session) error {
	const q = `INSERT INTO sessions (id, user_id, expires_at, created_at, ip_address, user_agent)
	           VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := db.pool.Exec(context.Background(), q, s.ID, s.UserID, s.ExpiresAt, s.CreatedAt, s.IPAddress, s.UserAgent)
	return err
}

// GetSession looks up a session by ID and ensures it has not expired.
func (db *DB) GetSession(id string) (*models.Session, error) {
	const q = `SELECT id, user_id, expires_at, created_at, ip_address, user_agent
	           FROM sessions WHERE id = $1 AND expires_at > $2`
	s := &models.Session{}
	err := db.pool.QueryRow(context.Background(), q, id, time.Now()).Scan(
		&s.ID, &s.UserID, &s.ExpiresAt, &s.CreatedAt, &s.IPAddress, &s.UserAgent,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// DeleteSession removes a session by ID.
func (db *DB) DeleteSession(id string) error {
	_, err := db.pool.Exec(context.Background(), `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

// DeleteExpiredSessions cleans up sessions that have passed their expiry.
func (db *DB) DeleteExpiredSessions() error {
	_, err := db.pool.Exec(context.Background(), `DELETE FROM sessions WHERE expires_at <= $1`, time.Now())
	return err
}

// GetSessionsByUserID returns all active sessions for a user, newest first.
func (db *DB) GetSessionsByUserID(userID string) ([]models.Session, error) {
	const q = `SELECT id, user_id, expires_at, created_at, ip_address, user_agent
	           FROM sessions WHERE user_id = $1 AND expires_at > $2 ORDER BY created_at DESC`
	rows, err := db.pool.Query(context.Background(), q, userID, time.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []models.Session
	for rows.Next() {
		var s models.Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.ExpiresAt, &s.CreatedAt, &s.IPAddress, &s.UserAgent); err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// --- Magic token operations ---

// CreateMagicToken inserts a new magic login token.
func (db *DB) CreateMagicToken(t *models.MagicToken) error {
	const q = `INSERT INTO magic_tokens (id, user_id, expires_at, created_at)
	           VALUES ($1, $2, $3, $4)`
	_, err := db.pool.Exec(context.Background(), q, t.ID, t.UserID, t.ExpiresAt, t.CreatedAt)
	return err
}

// GetMagicToken retrieves a magic token by ID if it is unused and not expired.
func (db *DB) GetMagicToken(id string) (*models.MagicToken, error) {
	const q = `SELECT id, user_id, expires_at, created_at
	           FROM magic_tokens WHERE id = $1 AND used_at IS NULL AND expires_at > $2`
	t := &models.MagicToken{}
	err := db.pool.QueryRow(context.Background(), q, id, time.Now()).Scan(
		&t.ID, &t.UserID, &t.ExpiresAt, &t.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// ConsumeMagicToken marks a magic token as used.
func (db *DB) ConsumeMagicToken(id string) error {
	const q = `UPDATE magic_tokens SET used_at = $1 WHERE id = $2`
	_, err := db.pool.Exec(context.Background(), q, time.Now(), id)
	return err
}

// DeleteExpiredMagicTokens cleans up tokens that have expired or been used.
func (db *DB) DeleteExpiredMagicTokens() error {
	const q = `DELETE FROM magic_tokens WHERE expires_at <= $1 OR used_at IS NOT NULL`
	_, err := db.pool.Exec(context.Background(), q, time.Now())
	return err
}

// --- Email change token operations ---

// CreateEmailChangeToken inserts a new email-change verification token.
func (db *DB) CreateEmailChangeToken(t *models.EmailChangeToken) error {
	const q = `INSERT INTO email_change_tokens (id, user_id, new_email, expires_at, created_at)
	           VALUES ($1, $2, $3, $4, $5)`
	_, err := db.pool.Exec(context.Background(), q, t.ID, t.UserID, t.NewEmail, t.ExpiresAt, t.CreatedAt)
	return err
}

// GetEmailChangeToken retrieves an email-change token by ID if it is unused and not expired.
func (db *DB) GetEmailChangeToken(id string) (*models.EmailChangeToken, error) {
	const q = `SELECT id, user_id, new_email, expires_at, created_at
	           FROM email_change_tokens WHERE id = $1 AND used_at IS NULL AND expires_at > $2`
	t := &models.EmailChangeToken{}
	err := db.pool.QueryRow(context.Background(), q, id, time.Now()).Scan(
		&t.ID, &t.UserID, &t.NewEmail, &t.ExpiresAt, &t.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return t, err
}

// ConsumeEmailChangeToken marks an email-change token as used.
func (db *DB) ConsumeEmailChangeToken(id string) error {
	const q = `UPDATE email_change_tokens SET used_at = $1 WHERE id = $2`
	_, err := db.pool.Exec(context.Background(), q, time.Now(), id)
	return err
}

// DeleteExpiredEmailChangeTokens cleans up tokens that have expired or been used.
func (db *DB) DeleteExpiredEmailChangeTokens() error {
	const q = `DELETE FROM email_change_tokens WHERE expires_at <= $1 OR used_at IS NOT NULL`
	_, err := db.pool.Exec(context.Background(), q, time.Now())
	return err
}
