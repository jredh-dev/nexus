// Package pgbus implements a lightweight event bus backed by PostgreSQL LISTEN/NOTIFY.
//
// It provides two operations:
//   - Publish: sends a NOTIFY on a named channel with a JSON payload, using an
//     existing pgxpool connection acquired from the caller's pool.
//   - Subscribe: opens a dedicated persistent pgx.Conn (LISTEN requires a single
//     long-lived connection), listens on the named channel, and calls handler for
//     each incoming notification. Blocks until ctx is cancelled.
//
// Event payload shape (JSON):
//
//	{"event":"user.login","service":"portal","ts":"2006-01-02T15:04:05Z07:00","data":{...}}
package pgbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Event is the canonical payload shape published on every channel.
type Event struct {
	// Event is the dot-namespaced event name, e.g. "user.login".
	Event string `json:"event"`
	// Service is the originating service name, e.g. "portal".
	Service string `json:"service"`
	// Ts is the UTC timestamp of the event in RFC3339 format.
	Ts string `json:"ts"`
	// Data holds event-specific key/value metadata.
	Data map[string]any `json:"data,omitempty"`
}

// Publish sends a NOTIFY on channel with a JSON-encoded Event payload.
// It acquires a connection from pool for the duration of the Exec call.
//
// channel is the Postgres LISTEN/NOTIFY channel name (e.g. "portal.events").
// eventName is the dot-namespaced event type (e.g. "user.login").
// service is the originating service identifier.
// data is optional metadata to include in the payload; may be nil.
func Publish(ctx context.Context, pool *pgxpool.Pool, channel, eventName, service string, data map[string]any) error {
	evt := Event{
		Event:   eventName,
		Service: service,
		Ts:      time.Now().UTC().Format(time.RFC3339),
		Data:    data,
	}

	payload, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("pgbus: marshal event: %w", err)
	}

	// pg_notify payload is limited to 8000 bytes — log a warning if we're close.
	if len(payload) > 7500 {
		log.Printf("pgbus: WARNING payload size %d bytes (limit 8000) on channel %q", len(payload), channel)
	}

	_, err = pool.Exec(ctx, "SELECT pg_notify($1, $2)", channel, string(payload))
	if err != nil {
		return fmt.Errorf("pgbus: notify channel %q: %w", channel, err)
	}

	return nil
}

// Subscribe opens a dedicated pgx.Conn to connStr, issues LISTEN on channel,
// and calls handler for each incoming notification payload (raw JSON string).
//
// It blocks until ctx is cancelled. On context cancellation it closes the
// connection cleanly and returns nil. On unexpected errors it returns the error.
//
// The dedicated connection is required because LISTEN state is per-connection
// and is not compatible with a pgxpool connection that can be returned to the pool.
//
// handler is called synchronously in the Subscribe goroutine; it should not block.
// If handler may block, run it in its own goroutine.
func Subscribe(ctx context.Context, connStr, channel string, handler func(payload string)) error {
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return fmt.Errorf("pgbus: connect for subscribe: %w", err)
	}
	defer conn.Close(ctx) //nolint:errcheck

	if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
		return fmt.Errorf("pgbus: listen on %q: %w", channel, err)
	}

	log.Printf("pgbus: subscribed to channel %q", channel)

	for {
		// WaitForNotification blocks until a notification arrives or ctx is done.
		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			// Normal shutdown path.
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("pgbus: wait for notification on %q: %w", channel, err)
		}

		handler(notification.Payload)
	}
}
