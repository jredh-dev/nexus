//go:build giveaway

// Package database provides PostgreSQL access for the portal service.
// This file contains the giveaway sub-feature (items and claims), guarded by
// the "giveaway" build tag so it is not compiled in normal builds.
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

// GiveawayDB wraps a pgxpool.Pool for the giveaway service.
type GiveawayDB struct {
	pool *pgxpool.Pool
}

// NewGiveaway connects to PostgreSQL and runs giveaway schema migrations.
func NewGiveaway(ctx context.Context, connStr string) (*GiveawayDB, error) {
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("open giveaway database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping giveaway database: %w", err)
	}
	db := &GiveawayDB{pool: pool}
	if err := db.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate giveaway database: %w", err)
	}
	return db, nil
}

// Close shuts down the connection pool.
func (db *GiveawayDB) Close() {
	db.pool.Close()
}

func (db *GiveawayDB) migrate(ctx context.Context) error {
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
		created_at    TIMESTAMPTZ NOT NULL,
		updated_at    TIMESTAMPTZ NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_items_status     ON items(status);
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
		created_at    TIMESTAMPTZ NOT NULL,
		updated_at    TIMESTAMPTZ NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_claims_item_id ON claims(item_id);
	CREATE INDEX IF NOT EXISTS idx_claims_status  ON claims(status);
	`
	_, err := db.pool.Exec(ctx, ddl)
	return err
}

// --- Item operations ---

const itemColumns = `id, title, description, image_url, condition, status, dist_miles, drive_minutes, created_at, updated_at`

func scanItem(row pgx.Row) (*models.Item, error) {
	item := &models.Item{}
	err := row.Scan(
		&item.ID, &item.Title, &item.Description, &item.ImageURL,
		&item.Condition, &item.Status, &item.DistMiles, &item.DriveMinutes,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// CreateItem inserts a new giveaway item.
func (db *GiveawayDB) CreateItem(item *models.Item) error {
	const q = `INSERT INTO items (id, title, description, image_url, condition, status, dist_miles, drive_minutes, created_at, updated_at)
	           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := db.pool.Exec(context.Background(), q,
		item.ID, item.Title, item.Description, item.ImageURL,
		item.Condition, item.Status, item.DistMiles, item.DriveMinutes,
		item.CreatedAt, item.UpdatedAt,
	)
	return err
}

// GetItem returns an item by ID.
func (db *GiveawayDB) GetItem(id string) (*models.Item, error) {
	q := `SELECT ` + itemColumns + ` FROM items WHERE id = $1`
	return scanItem(db.pool.QueryRow(context.Background(), q, id))
}

// ListItems returns items filtered by status. If status is empty, all items are returned.
func (db *GiveawayDB) ListItems(status models.ItemStatus) ([]models.Item, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if status != "" {
		q := `SELECT ` + itemColumns + ` FROM items WHERE status = $1 ORDER BY created_at DESC`
		rows, err = db.pool.Query(context.Background(), q, string(status))
	} else {
		q := `SELECT ` + itemColumns + ` FROM items ORDER BY created_at DESC`
		rows, err = db.pool.Query(context.Background(), q)
	}
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
	const q = `UPDATE items SET title = $1, description = $2, image_url = $3, condition = $4,
	           status = $5, dist_miles = $6, drive_minutes = $7, updated_at = $8 WHERE id = $9`
	_, err := db.pool.Exec(context.Background(), q,
		item.Title, item.Description, item.ImageURL, item.Condition,
		item.Status, item.DistMiles, item.DriveMinutes, time.Now(), item.ID,
	)
	return err
}

// DeleteItem removes an item by ID.
func (db *GiveawayDB) DeleteItem(id string) error {
	_, err := db.pool.Exec(context.Background(), `DELETE FROM items WHERE id = $1`, id)
	return err
}

// --- Claim operations ---

const claimColumns = `id, item_id, claimer_name, claimer_email, claimer_phone, delivery_fee, status, notes, created_at, updated_at`

func scanClaim(row pgx.Row) (*models.Claim, error) {
	c := &models.Claim{}
	err := row.Scan(
		&c.ID, &c.ItemID, &c.ClaimerName, &c.ClaimerEmail, &c.ClaimerPhone,
		&c.DeliveryFee, &c.Status, &c.Notes, &c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// CreateClaim inserts a new claim.
func (db *GiveawayDB) CreateClaim(claim *models.Claim) error {
	const q = `INSERT INTO claims (id, item_id, claimer_name, claimer_email, claimer_phone, delivery_fee, status, notes, created_at, updated_at)
	           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := db.pool.Exec(context.Background(), q,
		claim.ID, claim.ItemID, claim.ClaimerName, claim.ClaimerEmail,
		claim.ClaimerPhone, claim.DeliveryFee, claim.Status, claim.Notes,
		claim.CreatedAt, claim.UpdatedAt,
	)
	return err
}

// GetClaim returns a claim by ID.
func (db *GiveawayDB) GetClaim(id string) (*models.Claim, error) {
	q := `SELECT ` + claimColumns + ` FROM claims WHERE id = $1`
	return scanClaim(db.pool.QueryRow(context.Background(), q, id))
}

// ListClaimsByItem returns all claims for an item, newest first.
func (db *GiveawayDB) ListClaimsByItem(itemID string) ([]models.Claim, error) {
	q := `SELECT ` + claimColumns + ` FROM claims WHERE item_id = $1 ORDER BY created_at DESC`
	return db.queryClaims(context.Background(), q, itemID)
}

// ListClaims returns all claims, optionally filtered by status.
func (db *GiveawayDB) ListClaims(status models.ClaimStatus) ([]models.Claim, error) {
	if status != "" {
		q := `SELECT ` + claimColumns + ` FROM claims WHERE status = $1 ORDER BY created_at DESC`
		return db.queryClaims(context.Background(), q, string(status))
	}
	q := `SELECT ` + claimColumns + ` FROM claims ORDER BY created_at DESC`
	return db.queryClaims(context.Background(), q)
}

// UpdateClaimStatus updates a claim's status.
func (db *GiveawayDB) UpdateClaimStatus(id string, status models.ClaimStatus) error {
	const q = `UPDATE claims SET status = $1, updated_at = $2 WHERE id = $3`
	_, err := db.pool.Exec(context.Background(), q, string(status), time.Now(), id)
	return err
}

func (db *GiveawayDB) queryClaims(ctx context.Context, query string, args ...any) ([]models.Claim, error) {
	rows, err := db.pool.Query(ctx, query, args...)
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
