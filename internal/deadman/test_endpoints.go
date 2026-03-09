// Package deadman - test-only HTTP endpoints for manual SMS verification.
//
// These endpoints are disabled by default. Set TEST_ENDPOINTS_ENABLED=true to
// mount them. They must never be reachable in production.
//
// Endpoints:
//
//	POST /test/trigger?to=<phone>   Send the trigger SMS to <phone> (or DEADMAN_PHONE)
//	POST /test/warn?to=<phone>      Send the warn SMS to <phone> (or DEADMAN_PHONE)
//	POST /test/checkin?phone=<phone> Simulate an owner check-in for <phone>
//
// The ?to= / ?phone= query parameters default to DEADMAN_PHONE if not provided,
// which is populated from Vault secrets — no hardcoding needed.
package deadman

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MakeTestHandler returns an http.Handler that mounts all test endpoints under
// the given prefix (e.g. "/test"). Returns nil if TEST_ENDPOINTS_ENABLED is not
// "true" — caller should skip registering the route in that case.
//
// defaultPhone is the fallback recipient when the ?to= query param is absent.
// Pass os.Getenv("DEADMAN_PHONE") at call site so the Vault-rendered value is used.
func MakeTestHandler(pool *pgxpool.Pool, twilio TwilioConfig, defaultPhone string) http.Handler {
	mux := http.NewServeMux()

	// POST /test/trigger?to=<phone>
	// Sends the full trigger SMS (same message the ticker sends when a deadman fires)
	// directly to <phone>. Does NOT alter any owner state — pure outbound send.
	mux.HandleFunc("POST /test/trigger", func(w http.ResponseWriter, r *http.Request) {
		to := phoneParam(r, defaultPhone)
		if to == "" {
			http.Error(w, "no phone: provide ?to=+1xxx or set DEADMAN_PHONE", http.StatusBadRequest)
			return
		}

		// Build a synthetic owner so TriggerSMS has an owner phone to display.
		ownerPhone := r.URL.Query().Get("owner")
		if ownerPhone == "" {
			ownerPhone = to // self-test: owner == recipient
		}

		msg := TriggerSMS(ownerPhone)
		slog.Info("test/trigger: sending trigger SMS", "to", to, "owner", ownerPhone)
		if err := SendSMS(twilio, to, msg); err != nil {
			slog.Error("test/trigger: send failed", "err", err)
			http.Error(w, fmt.Sprintf("send failed: %v", err), http.StatusInternalServerError)
			return
		}
		slog.Info("test/trigger: sent", "to", to)
		writeJSON(w, map[string]string{"status": "sent", "to": to, "message": msg})
	})

	// POST /test/warn?to=<phone>
	// Sends the warning SMS (same as ticker sends at WarnInterval) directly to
	// <phone>. Does NOT alter owner state.
	mux.HandleFunc("POST /test/warn", func(w http.ResponseWriter, r *http.Request) {
		to := phoneParam(r, defaultPhone)
		if to == "" {
			http.Error(w, "no phone: provide ?to=+1xxx or set DEADMAN_PHONE", http.StatusBadRequest)
			return
		}

		// Synthetic elapsed / remaining for the warning message.
		elapsed := 72 * time.Hour
		remaining := 24 * time.Hour
		msg := fmt.Sprintf(
			"DEADMAN WARNING: No check-in for %s. Reply to reset. %s until your subscribers are alerted.",
			elapsed.Round(time.Minute), remaining.Round(time.Minute),
		)
		slog.Info("test/warn: sending warn SMS", "to", to)
		if err := SendSMS(twilio, to, msg); err != nil {
			slog.Error("test/warn: send failed", "err", err)
			http.Error(w, fmt.Sprintf("send failed: %v", err), http.StatusInternalServerError)
			return
		}
		slog.Info("test/warn: sent", "to", to)
		writeJSON(w, map[string]string{"status": "sent", "to": to, "message": msg})
	})

	// POST /test/checkin?phone=<owner-phone>
	// Resets the check-in timer for the given owner phone. Useful for resetting
	// state after a test trigger so the ticker does not fire again immediately.
	mux.HandleFunc("POST /test/checkin", func(w http.ResponseWriter, r *http.Request) {
		phone := r.URL.Query().Get("phone")
		if phone == "" {
			phone = defaultPhone
		}
		if phone == "" {
			http.Error(w, "no phone: provide ?phone=+1xxx or set DEADMAN_PHONE", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		if err := testCheckin(ctx, pool, phone); err != nil {
			slog.Error("test/checkin: failed", "phone", phone, "err", err)
			http.Error(w, fmt.Sprintf("checkin failed: %v", err), http.StatusInternalServerError)
			return
		}
		slog.Info("test/checkin: timer reset", "phone", phone)
		writeJSON(w, map[string]string{"status": "ok", "phone": phone, "reset_at": time.Now().UTC().Format(time.RFC3339)})
	})

	return mux
}

// phoneParam returns the ?to= query param, falling back to defaultPhone.
func phoneParam(r *http.Request, defaultPhone string) string {
	if v := r.URL.Query().Get("to"); v != "" {
		return v
	}
	return defaultPhone
}

// testCheckin resets the owner's last_checkin and clears warn/final sent flags.
// It uses the same CheckIn path but also clears the sent flags so the ticker
// won't skip the next warn/trigger cycle. Exported only for internal use.
func testCheckin(ctx context.Context, pool *pgxpool.Pool, phone string) error {
	// Verify the owner exists first so we get a clear error if the phone isn't registered.
	_, err := GetOwnerByPhone(ctx, pool, phone)
	if err != nil {
		return fmt.Errorf("owner not found for %s: %w", phone, err)
	}
	return CheckIn(ctx, pool, phone)
}

// writeJSON writes v as JSON with a 200 status. Logs on encode error.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("writeJSON", "err", err)
	}
}

// TestEndpointsEnabled reports whether TEST_ENDPOINTS_ENABLED=true in env.
func TestEndpointsEnabled() bool {
	return os.Getenv("TEST_ENDPOINTS_ENABLED") == "true"
}
