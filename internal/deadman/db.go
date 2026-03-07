// Package deadman implements the core deadman switch logic:
// owners, subscribers, subscriptions, polls, admins, and SMS routing.
package deadman

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// -----------------------------------------------------------------------
// Domain types
// -----------------------------------------------------------------------

// SubscriberStatus tracks consent state for a subscriber.
type SubscriberStatus string

const (
	StatusPending    SubscriberStatus = "pending"
	StatusSubscribed SubscriberStatus = "subscribed"
	StatusDeclined   SubscriberStatus = "declined"
	StatusBlocked    SubscriberStatus = "blocked" // replied Q — never contact again
)

// PollType is the kind of poll a subscriber sent (W or H).
type PollType string

const (
	PollWhy PollType = "W"
	PollHow PollType = "H"
)

// Owner is a person whose check-in timer runs.
type Owner struct {
	ID              int64
	Phone           string
	Name            string
	CheckInInterval time.Duration // default 72h
	WarnInterval    time.Duration // warn fires this long after last check-in (default = CheckInInterval)
	TriggerInterval time.Duration // final fires this long after last check-in (default = CheckInInterval + 24h)
	LastCheckin     time.Time
	WarnSentAt      *time.Time
	FinalSentAt     *time.Time
	CreatedAt       time.Time
}

// Subscriber is a person who can be notified when an owner's deadman fires.
type Subscriber struct {
	ID        int64
	Phone     string
	Name      string
	CreatedAt time.Time
}

// Subscription links an owner to a subscriber with a consent status.
type Subscription struct {
	ID           int64
	OwnerID      int64
	SubscriberID int64
	Status       SubscriberStatus
	CreatedAt    time.Time
	// Joined fields (populated by list queries)
	OwnerPhone      string
	OwnerName       string
	SubscriberPhone string
	SubscriberName  string
}

// Poll records a W or H request from a subscriber after a trigger.
type Poll struct {
	ID             int64
	SubscriptionID int64
	Type           PollType
	CreatedAt      time.Time
	ResolvedAt     *time.Time
}

// Admin receives W/H poll notifications and handles them manually.
type Admin struct {
	ID        int64
	Phone     string
	Name      string
	CreatedAt time.Time
}

// -----------------------------------------------------------------------
// Migrations
// -----------------------------------------------------------------------

// Migrate creates all tables if they don't exist.
// Safe to call on every startup — idempotent.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dm_owners (
			id                 BIGSERIAL PRIMARY KEY,
			phone              TEXT NOT NULL UNIQUE,
			name               TEXT NOT NULL DEFAULT '',
			-- intervals stored as seconds
			checkin_interval_s BIGINT NOT NULL DEFAULT 259200,  -- 72h
			warn_interval_s    BIGINT NOT NULL DEFAULT 259200,  -- 72h
			trigger_interval_s BIGINT NOT NULL DEFAULT 345600,  -- 96h
			last_checkin       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			warn_sent_at       TIMESTAMPTZ,
			final_sent_at      TIMESTAMPTZ,
			created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS dm_subscribers (
			id         BIGSERIAL PRIMARY KEY,
			phone      TEXT NOT NULL UNIQUE,
			name       TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		CREATE TABLE IF NOT EXISTS dm_subscriptions (
			id            BIGSERIAL PRIMARY KEY,
			owner_id      BIGINT NOT NULL REFERENCES dm_owners(id) ON DELETE CASCADE,
			subscriber_id BIGINT NOT NULL REFERENCES dm_subscribers(id) ON DELETE CASCADE,
			status        TEXT NOT NULL DEFAULT 'pending',
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(owner_id, subscriber_id)
		);

		CREATE TABLE IF NOT EXISTS dm_polls (
			id              BIGSERIAL PRIMARY KEY,
			subscription_id BIGINT NOT NULL REFERENCES dm_subscriptions(id) ON DELETE CASCADE,
			type            TEXT NOT NULL,  -- 'W' or 'H'
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			resolved_at     TIMESTAMPTZ
		);

		CREATE TABLE IF NOT EXISTS dm_admins (
			id         BIGSERIAL PRIMARY KEY,
			phone      TEXT NOT NULL UNIQUE,
			name       TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`)
	return err
}

// -----------------------------------------------------------------------
// Owner CRUD
// -----------------------------------------------------------------------

func AddOwner(ctx context.Context, pool *pgxpool.Pool, phone, name string, checkin, warn, trigger time.Duration) (Owner, error) {
	var o Owner
	err := pool.QueryRow(ctx, `
		INSERT INTO dm_owners (phone, name, checkin_interval_s, warn_interval_s, trigger_interval_s)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (phone) DO UPDATE
		  SET name               = EXCLUDED.name,
		      checkin_interval_s = EXCLUDED.checkin_interval_s,
		      warn_interval_s    = EXCLUDED.warn_interval_s,
		      trigger_interval_s = EXCLUDED.trigger_interval_s
		RETURNING id, phone, name, checkin_interval_s, warn_interval_s, trigger_interval_s,
		          last_checkin, warn_sent_at, final_sent_at, created_at
	`, phone, name, int64(checkin.Seconds()), int64(warn.Seconds()), int64(trigger.Seconds())).
		Scan(&o.ID, &o.Phone, &o.Name,
			new(int64), new(int64), new(int64), // scanned below via scanOwner
			&o.LastCheckin, &o.WarnSentAt, &o.FinalSentAt, &o.CreatedAt)
	if err != nil {
		return o, err
	}
	o.CheckInInterval = checkin
	o.WarnInterval = warn
	o.TriggerInterval = trigger
	return o, nil
}

func scanOwner(row interface {
	Scan(...any) error
}) (Owner, error) {
	var o Owner
	var checkinS, warnS, triggerS int64
	err := row.Scan(
		&o.ID, &o.Phone, &o.Name,
		&checkinS, &warnS, &triggerS,
		&o.LastCheckin, &o.WarnSentAt, &o.FinalSentAt, &o.CreatedAt,
	)
	if err != nil {
		return o, err
	}
	o.CheckInInterval = time.Duration(checkinS) * time.Second
	o.WarnInterval = time.Duration(warnS) * time.Second
	o.TriggerInterval = time.Duration(triggerS) * time.Second
	return o, nil
}

func GetOwnerByPhone(ctx context.Context, pool *pgxpool.Pool, phone string) (Owner, error) {
	row := pool.QueryRow(ctx, `
		SELECT id, phone, name, checkin_interval_s, warn_interval_s, trigger_interval_s,
		       last_checkin, warn_sent_at, final_sent_at, created_at
		FROM dm_owners WHERE phone = $1
	`, phone)
	return scanOwner(row)
}

func ListOwners(ctx context.Context, pool *pgxpool.Pool) ([]Owner, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, phone, name, checkin_interval_s, warn_interval_s, trigger_interval_s,
		       last_checkin, warn_sent_at, final_sent_at, created_at
		FROM dm_owners ORDER BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var owners []Owner
	for rows.Next() {
		o, err := scanOwner(rows)
		if err != nil {
			return nil, err
		}
		owners = append(owners, o)
	}
	return owners, rows.Err()
}

func RemoveOwner(ctx context.Context, pool *pgxpool.Pool, phone string) error {
	_, err := pool.Exec(ctx, `DELETE FROM dm_owners WHERE phone = $1`, phone)
	return err
}

// CheckIn resets the owner's timer and clears warn/final flags.
func CheckIn(ctx context.Context, pool *pgxpool.Pool, phone string) error {
	_, err := pool.Exec(ctx, `
		UPDATE dm_owners
		SET last_checkin  = NOW(),
		    warn_sent_at  = NULL,
		    final_sent_at = NULL
		WHERE phone = $1
	`, phone)
	return err
}

func MarkWarnSent(ctx context.Context, pool *pgxpool.Pool, ownerID int64) error {
	_, err := pool.Exec(ctx, `UPDATE dm_owners SET warn_sent_at = NOW() WHERE id = $1`, ownerID)
	return err
}

func MarkFinalSent(ctx context.Context, pool *pgxpool.Pool, ownerID int64) error {
	_, err := pool.Exec(ctx, `UPDATE dm_owners SET final_sent_at = NOW() WHERE id = $1`, ownerID)
	return err
}

// -----------------------------------------------------------------------
// Subscriber CRUD
// -----------------------------------------------------------------------

func AddSubscriber(ctx context.Context, pool *pgxpool.Pool, phone, name string) (Subscriber, error) {
	var s Subscriber
	err := pool.QueryRow(ctx, `
		INSERT INTO dm_subscribers (phone, name)
		VALUES ($1, $2)
		ON CONFLICT (phone) DO UPDATE SET name = EXCLUDED.name
		RETURNING id, phone, name, created_at
	`, phone, name).Scan(&s.ID, &s.Phone, &s.Name, &s.CreatedAt)
	return s, err
}

func GetSubscriberByPhone(ctx context.Context, pool *pgxpool.Pool, phone string) (Subscriber, error) {
	var s Subscriber
	err := pool.QueryRow(ctx, `
		SELECT id, phone, name, created_at FROM dm_subscribers WHERE phone = $1
	`, phone).Scan(&s.ID, &s.Phone, &s.Name, &s.CreatedAt)
	return s, err
}

func ListSubscribers(ctx context.Context, pool *pgxpool.Pool) ([]Subscriber, error) {
	rows, err := pool.Query(ctx, `SELECT id, phone, name, created_at FROM dm_subscribers ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var subs []Subscriber
	for rows.Next() {
		var s Subscriber
		if err := rows.Scan(&s.ID, &s.Phone, &s.Name, &s.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}

func RemoveSubscriber(ctx context.Context, pool *pgxpool.Pool, phone string) error {
	_, err := pool.Exec(ctx, `DELETE FROM dm_subscribers WHERE phone = $1`, phone)
	return err
}

// -----------------------------------------------------------------------
// Subscription CRUD
// -----------------------------------------------------------------------

// Subscribe links owner → subscriber and sets status to pending.
// Returns the subscription row (new or existing).
func Subscribe(ctx context.Context, pool *pgxpool.Pool, ownerPhone, subscriberPhone string) (Subscription, error) {
	var sub Subscription
	err := pool.QueryRow(ctx, `
		INSERT INTO dm_subscriptions (owner_id, subscriber_id, status)
		SELECT o.id, s.id, 'pending'
		FROM   dm_owners o, dm_subscribers s
		WHERE  o.phone = $1 AND s.phone = $2
		ON CONFLICT (owner_id, subscriber_id) DO UPDATE SET status = 'pending'
		RETURNING id, owner_id, subscriber_id, status, created_at
	`, ownerPhone, subscriberPhone).
		Scan(&sub.ID, &sub.OwnerID, &sub.SubscriberID, &sub.Status, &sub.CreatedAt)
	return sub, err
}

func SetSubscriptionStatus(ctx context.Context, pool *pgxpool.Pool, subscriptionID int64, status SubscriberStatus) error {
	_, err := pool.Exec(ctx, `UPDATE dm_subscriptions SET status = $1 WHERE id = $2`, status, subscriptionID)
	return err
}

// SetAllSubscriptionsBySubscriberPhone sets status for all subscriptions for a given subscriber phone.
func SetAllSubscriptionsBySubscriberPhone(ctx context.Context, pool *pgxpool.Pool, subscriberPhone string, status SubscriberStatus) ([]Subscription, error) {
	rows, err := pool.Query(ctx, `
		UPDATE dm_subscriptions sub
		SET    status = $1
		FROM   dm_subscribers s
		WHERE  sub.subscriber_id = s.id AND s.phone = $2
		RETURNING sub.id, sub.owner_id, sub.subscriber_id, sub.status, sub.created_at
	`, status, subscriberPhone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var subs []Subscription
	for rows.Next() {
		var sub Subscription
		if err := rows.Scan(&sub.ID, &sub.OwnerID, &sub.SubscriberID, &sub.Status, &sub.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

func ListSubscriptionsByOwner(ctx context.Context, pool *pgxpool.Pool, ownerPhone string) ([]Subscription, error) {
	rows, err := pool.Query(ctx, `
		SELECT sub.id, sub.owner_id, sub.subscriber_id, sub.status, sub.created_at,
		       o.phone, o.name, s.phone, s.name
		FROM   dm_subscriptions sub
		JOIN   dm_owners      o ON o.id = sub.owner_id
		JOIN   dm_subscribers s ON s.id = sub.subscriber_id
		WHERE  o.phone = $1
		ORDER  BY sub.created_at
	`, ownerPhone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSubscriptions(rows)
}

func ListSubscriptionsBySubscriber(ctx context.Context, pool *pgxpool.Pool, subscriberPhone string) ([]Subscription, error) {
	rows, err := pool.Query(ctx, `
		SELECT sub.id, sub.owner_id, sub.subscriber_id, sub.status, sub.created_at,
		       o.phone, o.name, s.phone, s.name
		FROM   dm_subscriptions sub
		JOIN   dm_owners      o ON o.id = sub.owner_id
		JOIN   dm_subscribers s ON s.id = sub.subscriber_id
		WHERE  s.phone = $1
		ORDER  BY sub.created_at
	`, subscriberPhone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSubscriptions(rows)
}

// ActiveSubscribersForOwner returns all subscribed (consented) subscribers for an owner.
func ActiveSubscribersForOwner(ctx context.Context, pool *pgxpool.Pool, ownerID int64) ([]Subscription, error) {
	rows, err := pool.Query(ctx, `
		SELECT sub.id, sub.owner_id, sub.subscriber_id, sub.status, sub.created_at,
		       o.phone, o.name, s.phone, s.name
		FROM   dm_subscriptions sub
		JOIN   dm_owners      o ON o.id = sub.owner_id
		JOIN   dm_subscribers s ON s.id = sub.subscriber_id
		WHERE  sub.owner_id = $1 AND sub.status = 'subscribed'
		ORDER  BY sub.created_at
	`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSubscriptions(rows)
}

func scanSubscriptions(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]Subscription, error) {
	var subs []Subscription
	for rows.Next() {
		var sub Subscription
		if err := rows.Scan(
			&sub.ID, &sub.OwnerID, &sub.SubscriberID, &sub.Status, &sub.CreatedAt,
			&sub.OwnerPhone, &sub.OwnerName, &sub.SubscriberPhone, &sub.SubscriberName,
		); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

// GetSubscriptionByID fetches a single subscription with joined owner/subscriber fields.
func GetSubscriptionByID(ctx context.Context, pool *pgxpool.Pool, id int64) (Subscription, error) {
	var sub Subscription
	err := pool.QueryRow(ctx, `
		SELECT sub.id, sub.owner_id, sub.subscriber_id, sub.status, sub.created_at,
		       o.phone, o.name, s.phone, s.name
		FROM   dm_subscriptions sub
		JOIN   dm_owners      o ON o.id = sub.owner_id
		JOIN   dm_subscribers s ON s.id = sub.subscriber_id
		WHERE  sub.id = $1
	`, id).Scan(
		&sub.ID, &sub.OwnerID, &sub.SubscriberID, &sub.Status, &sub.CreatedAt,
		&sub.OwnerPhone, &sub.OwnerName, &sub.SubscriberPhone, &sub.SubscriberName,
	)
	return sub, err
}

// GetSubscriptionByOwnerAndSubscriber looks up a subscription by both phone numbers.
func GetSubscriptionByOwnerAndSubscriber(ctx context.Context, pool *pgxpool.Pool, ownerPhone, subscriberPhone string) (Subscription, error) {
	var sub Subscription
	err := pool.QueryRow(ctx, `
		SELECT sub.id, sub.owner_id, sub.subscriber_id, sub.status, sub.created_at,
		       o.phone, o.name, s.phone, s.name
		FROM   dm_subscriptions sub
		JOIN   dm_owners      o ON o.id = sub.owner_id
		JOIN   dm_subscribers s ON s.id = sub.subscriber_id
		WHERE  o.phone = $1 AND s.phone = $2
	`, ownerPhone, subscriberPhone).Scan(
		&sub.ID, &sub.OwnerID, &sub.SubscriberID, &sub.Status, &sub.CreatedAt,
		&sub.OwnerPhone, &sub.OwnerName, &sub.SubscriberPhone, &sub.SubscriberName,
	)
	return sub, err
}

// -----------------------------------------------------------------------
// Poll CRUD
// -----------------------------------------------------------------------

func CreatePoll(ctx context.Context, pool *pgxpool.Pool, subscriptionID int64, pollType PollType) (Poll, error) {
	var p Poll
	err := pool.QueryRow(ctx, `
		INSERT INTO dm_polls (subscription_id, type)
		VALUES ($1, $2)
		RETURNING id, subscription_id, type, created_at, resolved_at
	`, subscriptionID, string(pollType)).
		Scan(&p.ID, &p.SubscriptionID, &p.Type, &p.CreatedAt, &p.ResolvedAt)
	return p, err
}

func ListUnresolvedPolls(ctx context.Context, pool *pgxpool.Pool) ([]Poll, error) {
	rows, err := pool.Query(ctx, `
		SELECT id, subscription_id, type, created_at, resolved_at
		FROM   dm_polls
		WHERE  resolved_at IS NULL
		ORDER  BY created_at
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var polls []Poll
	for rows.Next() {
		var p Poll
		if err := rows.Scan(&p.ID, &p.SubscriptionID, &p.Type, &p.CreatedAt, &p.ResolvedAt); err != nil {
			return nil, err
		}
		polls = append(polls, p)
	}
	return polls, rows.Err()
}

func ResolvePoll(ctx context.Context, pool *pgxpool.Pool, pollID int64) error {
	_, err := pool.Exec(ctx, `UPDATE dm_polls SET resolved_at = NOW() WHERE id = $1`, pollID)
	return err
}

// -----------------------------------------------------------------------
// Admin CRUD
// -----------------------------------------------------------------------

func AddAdmin(ctx context.Context, pool *pgxpool.Pool, phone, name string) (Admin, error) {
	var a Admin
	err := pool.QueryRow(ctx, `
		INSERT INTO dm_admins (phone, name)
		VALUES ($1, $2)
		ON CONFLICT (phone) DO UPDATE SET name = EXCLUDED.name
		RETURNING id, phone, name, created_at
	`, phone, name).Scan(&a.ID, &a.Phone, &a.Name, &a.CreatedAt)
	return a, err
}

func ListAdmins(ctx context.Context, pool *pgxpool.Pool) ([]Admin, error) {
	rows, err := pool.Query(ctx, `SELECT id, phone, name, created_at FROM dm_admins ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var admins []Admin
	for rows.Next() {
		var a Admin
		if err := rows.Scan(&a.ID, &a.Phone, &a.Name, &a.CreatedAt); err != nil {
			return nil, err
		}
		admins = append(admins, a)
	}
	return admins, rows.Err()
}

func RemoveAdmin(ctx context.Context, pool *pgxpool.Pool, phone string) error {
	_, err := pool.Exec(ctx, `DELETE FROM dm_admins WHERE phone = $1`, phone)
	return err
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

// OwnerStatus returns a human-readable status string for an owner.
func OwnerStatus(o Owner) string {
	elapsed := time.Since(o.LastCheckin).Round(time.Minute)
	remaining := o.WarnInterval - time.Since(o.LastCheckin)
	if remaining < 0 {
		remaining = 0
	}
	switch {
	case o.FinalSentAt != nil:
		return fmt.Sprintf("TRIGGERED — dark for %s (final alert sent %s ago)",
			elapsed, time.Since(*o.FinalSentAt).Round(time.Minute))
	case o.WarnSentAt != nil:
		triggerIn := o.TriggerInterval - time.Since(o.LastCheckin)
		if triggerIn < 0 {
			triggerIn = 0
		}
		return fmt.Sprintf("WARNING — dark for %s, final trigger in %s",
			elapsed, triggerIn.Round(time.Minute))
	default:
		return fmt.Sprintf("OK — last check-in %s ago, warn in %s",
			elapsed, remaining.Round(time.Minute))
	}
}
