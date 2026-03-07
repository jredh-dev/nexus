// Package main is the deadman switch service.
//
// Behavior:
//   - You must text the Twilio number from your registered phone every 72 hours.
//   - Any inbound SMS from your number resets the timer.
//   - At T+72h: send a warning SMS ("check in now — 24 hours left").
//   - At T+96h: send a final alert SMS ("deadman triggered").
//
// State lives in PostgreSQL. The ticker loop runs every minute.
//
// Environment variables (required):
//
//	TWILIO_ACCOUNT_SID   Twilio account SID
//	TWILIO_AUTH_TOKEN    Twilio auth token
//	TWILIO_FROM          Your Twilio phone number (E.164, e.g. +15005550006)
//	DEADMAN_PHONE        Your personal phone number (E.164) — must match inbound SMS From
//	PORT                 HTTP listen port (default: 8095)
//	DATABASE_URL         PostgreSQL DSN (default: host=/tmp/ctl-pg dbname=deadman user=jredh)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// -----------------------------------------------------------------------
// Config
// -----------------------------------------------------------------------

type config struct {
	TwilioSID   string
	TwilioToken string
	TwilioFrom  string // E.164 Twilio number
	MyPhone     string // E.164 your personal number
	Port        string
	DatabaseURL string
}

func loadConfig() (config, error) {
	c := config{
		TwilioSID:   os.Getenv("TWILIO_ACCOUNT_SID"),
		TwilioToken: os.Getenv("TWILIO_AUTH_TOKEN"),
		TwilioFrom:  os.Getenv("TWILIO_FROM"),
		MyPhone:     os.Getenv("DEADMAN_PHONE"),
		Port:        os.Getenv("PORT"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
	if c.Port == "" {
		c.Port = "8095"
	}
	if c.DatabaseURL == "" {
		c.DatabaseURL = "host=/tmp/ctl-pg dbname=deadman user=jredh"
	}
	missing := []string{}
	if c.TwilioSID == "" {
		missing = append(missing, "TWILIO_ACCOUNT_SID")
	}
	if c.TwilioToken == "" {
		missing = append(missing, "TWILIO_AUTH_TOKEN")
	}
	if c.TwilioFrom == "" {
		missing = append(missing, "TWILIO_FROM")
	}
	if c.MyPhone == "" {
		missing = append(missing, "DEADMAN_PHONE")
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	return c, nil
}

// -----------------------------------------------------------------------
// State row — one row per "switch", keyed by phone number.
// -----------------------------------------------------------------------

// State tracks timing and what notifications have been sent.
type State struct {
	Phone       string    // the personal phone this switch watches
	LastCheckin time.Time // last time an inbound SMS was received
	WarnSentAt  *time.Time
	FinalSentAt *time.Time
}

// -----------------------------------------------------------------------
// DB helpers
// -----------------------------------------------------------------------

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS deadman (
			phone        TEXT PRIMARY KEY,
			last_checkin TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			warn_sent_at TIMESTAMPTZ,
			final_sent_at TIMESTAMPTZ
		);
	`)
	return err
}

// upsertCheckin resets the timer and clears all sent flags.
func upsertCheckin(ctx context.Context, pool *pgxpool.Pool, phone string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO deadman (phone, last_checkin, warn_sent_at, final_sent_at)
		VALUES ($1, NOW(), NULL, NULL)
		ON CONFLICT (phone) DO UPDATE
		  SET last_checkin  = NOW(),
		      warn_sent_at  = NULL,
		      final_sent_at = NULL;
	`, phone)
	return err
}

// loadState fetches state, creating a fresh row if not present.
func loadState(ctx context.Context, pool *pgxpool.Pool, phone string) (State, error) {
	// Ensure a row exists on first run.
	_, err := pool.Exec(ctx, `
		INSERT INTO deadman (phone) VALUES ($1) ON CONFLICT DO NOTHING;
	`, phone)
	if err != nil {
		return State{}, err
	}

	var s State
	s.Phone = phone
	err = pool.QueryRow(ctx, `
		SELECT last_checkin, warn_sent_at, final_sent_at
		FROM   deadman
		WHERE  phone = $1
	`, phone).Scan(&s.LastCheckin, &s.WarnSentAt, &s.FinalSentAt)
	return s, err
}

func markWarnSent(ctx context.Context, pool *pgxpool.Pool, phone string) error {
	_, err := pool.Exec(ctx, `
		UPDATE deadman SET warn_sent_at = NOW() WHERE phone = $1
	`, phone)
	return err
}

func markFinalSent(ctx context.Context, pool *pgxpool.Pool, phone string) error {
	_, err := pool.Exec(ctx, `
		UPDATE deadman SET final_sent_at = NOW() WHERE phone = $1
	`, phone)
	return err
}

// -----------------------------------------------------------------------
// Twilio send
// -----------------------------------------------------------------------

func sendSMS(cfg config, to, body string) error {
	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", cfg.TwilioSID)
	data := url.Values{}
	data.Set("To", to)
	data.Set("From", cfg.TwilioFrom)
	data.Set("Body", body)

	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(cfg.TwilioSID, cfg.TwilioToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twilio %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// -----------------------------------------------------------------------
// Ticker loop
// -----------------------------------------------------------------------

const (
	warnAfter  = 72 * time.Hour // send warning after 72h of silence
	triggerAt  = 96 * time.Hour // send final alert after 96h (72h + 24h)
	tickPeriod = 1 * time.Minute
)

func runTicker(ctx context.Context, pool *pgxpool.Pool, cfg config) {
	tick := time.NewTicker(tickPeriod)
	defer tick.Stop()

	slog.Info("ticker started", "warn_after", warnAfter, "trigger_at", triggerAt)

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := check(ctx, pool, cfg); err != nil {
				slog.Error("ticker check failed", "err", err)
			}
		}
	}
}

func check(ctx context.Context, pool *pgxpool.Pool, cfg config) error {
	s, err := loadState(ctx, pool, cfg.MyPhone)
	if err != nil {
		return fmt.Errorf("loadState: %w", err)
	}

	elapsed := time.Since(s.LastCheckin)
	slog.Debug("tick", "elapsed", elapsed.Round(time.Minute), "warn_sent", s.WarnSentAt != nil, "final_sent", s.FinalSentAt != nil)

	// Final trigger: 96h elapsed, not yet sent.
	if elapsed >= triggerAt && s.FinalSentAt == nil {
		msg := fmt.Sprintf("DEADMAN TRIGGERED: No check-in for %s. This is your final alert.", elapsed.Round(time.Minute))
		if err := sendSMS(cfg, cfg.MyPhone, msg); err != nil {
			return fmt.Errorf("send final SMS: %w", err)
		}
		if err := markFinalSent(ctx, pool, cfg.MyPhone); err != nil {
			return err
		}
		slog.Warn("final alert sent", "elapsed", elapsed.Round(time.Minute))
		return nil
	}

	// Warning: 72h elapsed, warn not yet sent, final not yet sent.
	if elapsed >= warnAfter && s.WarnSentAt == nil && s.FinalSentAt == nil {
		remaining := (triggerAt - elapsed).Round(time.Minute)
		msg := fmt.Sprintf("DEADMAN WARNING: No check-in for %s. Reply to this number within %s or the switch triggers.", elapsed.Round(time.Minute), remaining)
		if err := sendSMS(cfg, cfg.MyPhone, msg); err != nil {
			return fmt.Errorf("send warn SMS: %w", err)
		}
		if err := markWarnSent(ctx, pool, cfg.MyPhone); err != nil {
			return err
		}
		slog.Warn("warning sent", "elapsed", elapsed.Round(time.Minute), "remaining", remaining)
	}

	return nil
}

// -----------------------------------------------------------------------
// Twilio inbound SMS webhook
// POST /sms  (configure as webhook URL in Twilio console)
// -----------------------------------------------------------------------

func makeSMSHandler(pool *pgxpool.Pool, cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		from := r.FormValue("From") // E.164 from Twilio
		body := strings.TrimSpace(r.FormValue("Body"))

		slog.Info("inbound SMS", "from", from, "body", body)

		// Only accept check-ins from your registered number.
		if from != cfg.MyPhone {
			slog.Warn("ignoring SMS from unknown number", "from", from)
			// Return empty TwiML — don't reply.
			w.Header().Set("Content-Type", "text/xml")
			fmt.Fprintln(w, `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`)
			return
		}

		ctx := r.Context()
		if err := upsertCheckin(ctx, pool, cfg.MyPhone); err != nil {
			slog.Error("upsertCheckin failed", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		slog.Info("timer reset", "phone", cfg.MyPhone)

		// Reply with a confirmation via TwiML.
		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Message>Check-in received. Timer reset. Next check-in due in 72 hours.</Message>
</Response>
`)
	}
}

// -----------------------------------------------------------------------
// Health + status endpoints
// -----------------------------------------------------------------------

func makeStatusHandler(pool *pgxpool.Pool, cfg config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		s, err := loadState(ctx, pool, cfg.MyPhone)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		elapsed := time.Since(s.LastCheckin)
		out := map[string]any{
			"last_checkin":   s.LastCheckin.Format(time.RFC3339),
			"elapsed":        elapsed.Round(time.Second).String(),
			"warn_due_in":    max(0, warnAfter-elapsed).Round(time.Second).String(),
			"trigger_due_in": max(0, triggerAt-elapsed).Round(time.Second).String(),
			"warn_sent":      s.WarnSentAt != nil,
			"final_sent":     s.FinalSentAt != nil,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

// -----------------------------------------------------------------------
// main
// -----------------------------------------------------------------------

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// --help / -h / -?
	if len(os.Args) > 1 {
		arg := os.Args[1]
		if arg == "--help" || arg == "-h" || arg == "-?" || arg == "help" {
			fmt.Println(`deadman - SMS deadman switch via Twilio

Usage:
  deadman

Environment variables (all required unless noted):
  TWILIO_ACCOUNT_SID   Twilio account SID
  TWILIO_AUTH_TOKEN    Twilio auth token
  TWILIO_FROM          Twilio phone number (E.164, e.g. +15005550006)
  DEADMAN_PHONE        Your personal number (E.164) — inbound SMS must come from here
  PORT                 HTTP listen port (default: 8095)
  DATABASE_URL         PostgreSQL DSN (default: host=/tmp/ctl-pg dbname=deadman user=jredh)

HTTP endpoints:
  POST /sms    Twilio inbound SMS webhook — resets the 72hr timer when you text in
  GET  /status JSON status (last check-in, elapsed, due times)
  GET  /health 200 OK

Behavior:
  Text the Twilio number from DEADMAN_PHONE every 72h to stay alive.
  At T+72h: warning SMS sent. 24h to reply.
  At T+96h: final alert SMS sent.`)
			os.Exit(0)
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := migrate(ctx, pool); err != nil {
		slog.Error("migration failed", "err", err)
		os.Exit(1)
	}
	slog.Info("db ready")

	// Boot the ticker in the background.
	go runTicker(ctx, pool, cfg)

	// HTTP server.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sms", makeSMSHandler(pool, cfg))
	mux.HandleFunc("GET /status", makeStatusHandler(pool, cfg))
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	addr := ":" + cfg.Port
	slog.Info("listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

// max returns the larger of two durations (stdlib min/max not available for time.Duration in older Go).
func max(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}
