//go:build giveaway

package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/jredh-dev/nexus/services/portal/pkg/models"

	_ "modernc.org/sqlite"
)

// GiveawayDB wraps a SQLite connection for the giveaway service.
type GiveawayDB struct {
	conn *sql.DB
}

// NewGiveaway opens (or creates) the giveaway SQLite database and runs migrations.
func NewGiveaway(path string) (*GiveawayDB, error) {
	conn, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open giveaway database: %w", err)
	}

	conn.SetMaxOpenConns(1)

	if err := migrateGiveaway(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate giveaway: %w", err)
	}

	return &GiveawayDB{conn: conn}, nil
}

// Close closes the database connection.
func (db *GiveawayDB) Close() error {
	return db.conn.Close()
}

func migrateGiveaway(conn *sql.DB) error {
	const ddl = `
	CREATE TABLE IF NOT EXISTS items (
		id            TEXT PRIMARY KEY,
		title         TEXT NOT NULL,
		description   TEXT NOT NULL DEFAULT '',
		image_url     TEXT NOT NULL DEFAULT '',
		condition     TEXT NOT NULL DEFAULT 'good',
		status        TEXT NOT NULL DEFAULT 'available',
		dist_miles    REAL NOT NULL DEFAULT 0,
		drive_minutes INTEGER NOT NULL DEFAULT 0,
		created_at    DATETIME NOT NULL,
		updated_at    DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_items_status ON items(status);
	CREATE INDEX IF NOT EXISTS idx_items_created_at ON items(created_at);

	CREATE TABLE IF NOT EXISTS claims (
		id            TEXT PRIMARY KEY,
		item_id       TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
		claimer_name  TEXT NOT NULL,
		claimer_email TEXT NOT NULL,
		claimer_phone TEXT NOT NULL DEFAULT '',
		delivery_fee  REAL NOT NULL DEFAULT 0,
		status        TEXT NOT NULL DEFAULT 'pending',
		notes         TEXT NOT NULL DEFAULT '',
		created_at    DATETIME NOT NULL,
		updated_at    DATETIME NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_claims_item_id ON claims(item_id);
	CREATE INDEX IF NOT EXISTS idx_claims_status ON claims(status);
	`
	_, err := conn.Exec(ddl)
	return err
}

// --- Item operations ---

const itemColumns = `id, title, description, image_url, condition, status, dist_miles, drive_minutes, created_at, updated_at`

func scanItem(row interface{ Scan(...interface{}) error }) (*models.Item, error) {
	item := &models.Item{}
	err := row.Scan(
		&item.ID, &item.Title, &item.Description, &item.ImageURL,
		&item.Condition, &item.Status, &item.DistMiles, &item.DriveMinutes,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return item, err
}

// CreateItem inserts a new giveaway item.
func (db *GiveawayDB) CreateItem(item *models.Item) error {
	const q = `INSERT INTO items (id, title, description, image_url, condition, status, dist_miles, drive_minutes, created_at, updated_at)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := db.conn.Exec(q,
		item.ID, item.Title, item.Description, item.ImageURL,
		item.Condition, item.Status, item.DistMiles, item.DriveMinutes,
		item.CreatedAt, item.UpdatedAt,
	)
	return err
}

// GetItem returns an item by ID.
func (db *GiveawayDB) GetItem(id string) (*models.Item, error) {
	q := `SELECT ` + itemColumns + ` FROM items WHERE id = ?`
	return scanItem(db.conn.QueryRow(q, id))
}

// ListItems returns items filtered by status. If status is empty, all items are returned.
func (db *GiveawayDB) ListItems(status models.ItemStatus) ([]models.Item, error) {
	var q string
	var args []interface{}

	if status != "" {
		q = `SELECT ` + itemColumns + ` FROM items WHERE status = ? ORDER BY created_at DESC`
		args = append(args, string(status))
	} else {
		q = `SELECT ` + itemColumns + ` FROM items ORDER BY created_at DESC`
	}

	rows, err := db.conn.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []models.Item
	for rows.Next() {
		var item models.Item
		if err := rows.Scan(
			&item.ID, &item.Title, &item.Description, &item.ImageURL,
			&item.Condition, &item.Status, &item.DistMiles, &item.DriveMinutes,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpdateItem updates an existing item.
func (db *GiveawayDB) UpdateItem(item *models.Item) error {
	const q = `UPDATE items SET title = ?, description = ?, image_url = ?, condition = ?,
	           status = ?, dist_miles = ?, drive_minutes = ?, updated_at = ? WHERE id = ?`
	_, err := db.conn.Exec(q,
		item.Title, item.Description, item.ImageURL, item.Condition,
		item.Status, item.DistMiles, item.DriveMinutes, time.Now(), item.ID,
	)
	return err
}

// DeleteItem removes an item by ID.
func (db *GiveawayDB) DeleteItem(id string) error {
	_, err := db.conn.Exec(`DELETE FROM items WHERE id = ?`, id)
	return err
}

// --- Claim operations ---

const claimColumns = `id, item_id, claimer_name, claimer_email, claimer_phone, delivery_fee, status, notes, created_at, updated_at`

func scanClaim(row interface{ Scan(...interface{}) error }) (*models.Claim, error) {
	c := &models.Claim{}
	err := row.Scan(
		&c.ID, &c.ItemID, &c.ClaimerName, &c.ClaimerEmail, &c.ClaimerPhone,
		&c.DeliveryFee, &c.Status, &c.Notes, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return c, err
}

// CreateClaim inserts a new claim.
func (db *GiveawayDB) CreateClaim(claim *models.Claim) error {
	const q = `INSERT INTO claims (id, item_id, claimer_name, claimer_email, claimer_phone, delivery_fee, status, notes, created_at, updated_at)
	           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := db.conn.Exec(q,
		claim.ID, claim.ItemID, claim.ClaimerName, claim.ClaimerEmail,
		claim.ClaimerPhone, claim.DeliveryFee, claim.Status, claim.Notes,
		claim.CreatedAt, claim.UpdatedAt,
	)
	return err
}

// GetClaim returns a claim by ID.
func (db *GiveawayDB) GetClaim(id string) (*models.Claim, error) {
	q := `SELECT ` + claimColumns + ` FROM claims WHERE id = ?`
	return scanClaim(db.conn.QueryRow(q, id))
}

// ListClaimsByItem returns all claims for an item.
func (db *GiveawayDB) ListClaimsByItem(itemID string) ([]models.Claim, error) {
	q := `SELECT ` + claimColumns + ` FROM claims WHERE item_id = ? ORDER BY created_at DESC`
	return db.queryClaims(q, itemID)
}

// ListClaims returns all claims, optionally filtered by status.
func (db *GiveawayDB) ListClaims(status models.ClaimStatus) ([]models.Claim, error) {
	if status != "" {
		q := `SELECT ` + claimColumns + ` FROM claims WHERE status = ? ORDER BY created_at DESC`
		return db.queryClaims(q, string(status))
	}
	q := `SELECT ` + claimColumns + ` FROM claims ORDER BY created_at DESC`
	return db.queryClaims(q)
}

// UpdateClaimStatus updates a claim's status.
func (db *GiveawayDB) UpdateClaimStatus(id string, status models.ClaimStatus) error {
	const q = `UPDATE claims SET status = ?, updated_at = ? WHERE id = ?`
	_, err := db.conn.Exec(q, string(status), time.Now(), id)
	return err
}

func (db *GiveawayDB) queryClaims(query string, args ...interface{}) ([]models.Claim, error) {
	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var claims []models.Claim
	for rows.Next() {
		var c models.Claim
		if err := rows.Scan(
			&c.ID, &c.ItemID, &c.ClaimerName, &c.ClaimerEmail, &c.ClaimerPhone,
			&c.DeliveryFee, &c.Status, &c.Notes, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		claims = append(claims, c)
	}
	return claims, rows.Err()
}
