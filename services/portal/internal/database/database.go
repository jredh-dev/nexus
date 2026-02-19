package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/jredh-dev/nexus/services/portal/pkg/models"

	_ "modernc.org/sqlite"
)

// DB wraps a SQLite connection.
type DB struct {
	conn *sql.DB
}

// New opens (or creates) the SQLite database and runs migrations.
func New(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Single writer, many readers.
	conn.SetMaxOpenConns(1)

	if err := migrate(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &DB{conn: conn}, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

// migrate creates tables if they do not exist and applies schema updates.
func migrate(conn *sql.DB) error {
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
		created_at    DATETIME NOT NULL,
		updated_at    DATETIME NOT NULL,
		last_login_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
	CREATE INDEX IF NOT EXISTS idx_users_email_hash ON users(email_hash);
	CREATE INDEX IF NOT EXISTS idx_users_phone_hash ON users(phone_hash);

	CREATE TABLE IF NOT EXISTS sessions (
		id         TEXT PRIMARY KEY,
		user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		expires_at DATETIME NOT NULL,
		created_at DATETIME NOT NULL,
		ip_address TEXT NOT NULL DEFAULT '',
		user_agent TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

	CREATE TABLE IF NOT EXISTS magic_tokens (
		id         TEXT PRIMARY KEY,
		user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		expires_at DATETIME NOT NULL,
		used_at    DATETIME,
		created_at DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_magic_tokens_user_id ON magic_tokens(user_id);
	`
	if _, err := conn.Exec(ddl); err != nil {
		return err
	}

	// Add role column to existing databases that lack it.
	if err := addColumnIfNotExists(conn, "users", "role", "TEXT NOT NULL DEFAULT 'user'"); err != nil {
		return err
	}

	return nil
}

// addColumnIfNotExists adds a column to a table if it does not already exist.
// SQLite doesn't support IF NOT EXISTS for ALTER TABLE ADD COLUMN, so we
// check the schema first.
func addColumnIfNotExists(conn *sql.DB, table, column, colDef string) error {
	rows, err := conn.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil // column already exists
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = conn.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colDef))
	return err
}

// userColumns is the SELECT column list for user queries.
const userColumns = `id, username, email, phone_number, name, role, password_hash, email_hash, phone_hash, created_at, updated_at, last_login_at`

// scanUser scans a row into a User model.
func scanUser(row interface{ Scan(...interface{}) error }) (*models.User, error) {
	u := &models.User{}
	err := row.Scan(
		&u.ID, &u.Username, &u.Email, &u.PhoneNumber, &u.Name, &u.Role,
		&u.PasswordHash, &u.EmailHash, &u.PhoneHash,
		&u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

// --- User operations ---

// CreateUser inserts a new user.
func (db *DB) CreateUser(u *models.User) error {
	const q = `INSERT INTO users (id, username, email, phone_number, name, role, password_hash, email_hash, phone_hash, created_at, updated_at, last_login_at)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := db.conn.Exec(q,
		u.ID, u.Username, u.Email, u.PhoneNumber, u.Name, u.Role,
		u.PasswordHash, u.EmailHash, u.PhoneHash,
		u.CreatedAt, u.UpdatedAt, u.LastLoginAt,
	)
	return err
}

// GetUserByEmail looks up a user by email.
func (db *DB) GetUserByEmail(email string) (*models.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE email = ?`
	return scanUser(db.conn.QueryRow(q, email))
}

// GetUserByID looks up a user by ID.
func (db *DB) GetUserByID(id string) (*models.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE id = ?`
	return scanUser(db.conn.QueryRow(q, id))
}

// GetUserByUsername looks up a user by username (case-insensitive).
func (db *DB) GetUserByUsername(username string) (*models.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE username = ? COLLATE NOCASE`
	return scanUser(db.conn.QueryRow(q, username))
}

// GetUserByEmailHash looks up a user by normalized email hash.
func (db *DB) GetUserByEmailHash(hash string) (*models.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE email_hash = ?`
	return scanUser(db.conn.QueryRow(q, hash))
}

// GetUserByPhoneHash looks up a user by normalized phone hash.
func (db *DB) GetUserByPhoneHash(hash string) (*models.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE phone_hash = ?`
	return scanUser(db.conn.QueryRow(q, hash))
}

// UpdateLastLogin sets the last_login_at timestamp.
func (db *DB) UpdateLastLogin(userID string, t time.Time) error {
	const q = `UPDATE users SET last_login_at = ?, updated_at = ? WHERE id = ?`
	_, err := db.conn.Exec(q, t, t, userID)
	return err
}

// --- Session operations ---

// CreateSession inserts a new session.
func (db *DB) CreateSession(s *models.Session) error {
	const q = `INSERT INTO sessions (id, user_id, expires_at, created_at, ip_address, user_agent)
	           VALUES (?, ?, ?, ?, ?, ?)`
	_, err := db.conn.Exec(q, s.ID, s.UserID, s.ExpiresAt, s.CreatedAt, s.IPAddress, s.UserAgent)
	return err
}

// GetSession looks up a session by ID and ensures it has not expired.
func (db *DB) GetSession(id string) (*models.Session, error) {
	const q = `SELECT id, user_id, expires_at, created_at, ip_address, user_agent
	           FROM sessions WHERE id = ? AND expires_at > ?`
	s := &models.Session{}
	err := db.conn.QueryRow(q, id, time.Now()).Scan(&s.ID, &s.UserID, &s.ExpiresAt, &s.CreatedAt, &s.IPAddress, &s.UserAgent)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

// DeleteSession removes a session by ID.
func (db *DB) DeleteSession(id string) error {
	_, err := db.conn.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// DeleteExpiredSessions cleans up sessions that have passed their expiry.
func (db *DB) DeleteExpiredSessions() error {
	_, err := db.conn.Exec(`DELETE FROM sessions WHERE expires_at <= ?`, time.Now())
	return err
}

// GetSessionsByUserID returns all active sessions for a user.
func (db *DB) GetSessionsByUserID(userID string) ([]models.Session, error) {
	const q = `SELECT id, user_id, expires_at, created_at, ip_address, user_agent
	           FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY created_at DESC`
	rows, err := db.conn.Query(q, userID, time.Now())
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

// --- Role operations ---

// UpdateUserRole sets the role for a user.
func (db *DB) UpdateUserRole(userID, role string) error {
	const q = `UPDATE users SET role = ?, updated_at = ? WHERE id = ?`
	_, err := db.conn.Exec(q, role, time.Now(), userID)
	return err
}

// --- Magic token operations ---

// CreateMagicToken inserts a new magic login token.
func (db *DB) CreateMagicToken(t *models.MagicToken) error {
	const q = `INSERT INTO magic_tokens (id, user_id, expires_at, created_at)
	           VALUES (?, ?, ?, ?)`
	_, err := db.conn.Exec(q, t.ID, t.UserID, t.ExpiresAt, t.CreatedAt)
	return err
}

// GetMagicToken retrieves a magic token by ID if it is unused and not expired.
func (db *DB) GetMagicToken(id string) (*models.MagicToken, error) {
	const q = `SELECT id, user_id, expires_at, created_at
	           FROM magic_tokens WHERE id = ? AND used_at IS NULL AND expires_at > ?`
	t := &models.MagicToken{}
	err := db.conn.QueryRow(q, id, time.Now()).Scan(&t.ID, &t.UserID, &t.ExpiresAt, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// ConsumeMagicToken marks a magic token as used.
func (db *DB) ConsumeMagicToken(id string) error {
	const q = `UPDATE magic_tokens SET used_at = ? WHERE id = ?`
	_, err := db.conn.Exec(q, time.Now(), id)
	return err
}

// DeleteExpiredMagicTokens cleans up tokens that have expired or been used.
func (db *DB) DeleteExpiredMagicTokens() error {
	const q = `DELETE FROM magic_tokens WHERE expires_at <= ? OR used_at IS NOT NULL`
	_, err := db.conn.Exec(q, time.Now())
	return err
}
