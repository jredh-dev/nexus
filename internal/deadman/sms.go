// Package deadman - inbound SMS router.
//
// All inbound SMS from Twilio POST to /sms.
// This file routes based on the sender's role and current state.
//
// Owner texts:  any message → check-in (reset timer)
// Unknown/new texts: ignored (silent TwiML response)
// Subscriber texts (after trigger):
//
//	R → resubscribe (resend current status for active deadmans)
//	W → log WHY poll, ack, alert admins
//	H → log HOW poll, ack, alert admins
//	U → begin unsubscribe flow (one confirm per deadman)
//	Y → confirm pending unsubscribe
//	N → cancel pending unsubscribe
//
// Subscriber texts (consent phase):
//
//	Y → set subscribed
//	N → set declined
//	Q → set blocked (never contact again)
package deadman

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// unsubPending tracks subscribers currently in the unsubscribe confirmation
// flow: maps subscriberPhone → list of ownerPhones awaiting Y/N confirmation.
// Stored in-memory; lost on restart (acceptable — subscriber just re-sends U).
var unsubPending = map[string][]string{}

// MakeSMSHandler returns the Twilio inbound SMS HTTP handler.
func MakeSMSHandler(pool *pgxpool.Pool, twilio TwilioConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		from := strings.TrimSpace(r.FormValue("From"))
		body := strings.TrimSpace(r.FormValue("Body"))
		first := strings.ToUpper(string([]rune(body)[:min(1, len([]rune(body)))]))

		slog.Info("inbound SMS", "from", from, "body", body)

		ctx := r.Context()
		reply := route(ctx, pool, twilio, from, first, body)

		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprintln(w, TwiMLResponse(reply))
	}
}

// route returns the TwiML reply body (empty string = no reply).
func route(ctx context.Context, pool *pgxpool.Pool, twilio TwilioConfig, from, first, body string) string {
	// --- Is this an owner? ---
	owner, err := GetOwnerByPhone(ctx, pool, from)
	if err == nil {
		// Owner check-in: any message resets the timer.
		if err := CheckIn(ctx, pool, from); err != nil {
			slog.Error("checkin failed", "owner", from, "err", err)
			return "Error resetting timer. Please try again."
		}
		elapsed := time.Since(owner.LastCheckin).Round(time.Minute)
		slog.Info("owner checked in", "owner", from, "prev_elapsed", elapsed)
		return fmt.Sprintf("Check-in received. Timer reset. Next check-in due in %s.", owner.CheckInInterval.Round(time.Minute))
	}

	// --- Is this a subscriber? ---
	sub, err := GetSubscriberByPhone(ctx, pool, from)
	if err != nil {
		if err == pgx.ErrNoRows {
			// Unknown sender — silent.
			slog.Warn("SMS from unknown number", "from", from)
			return ""
		}
		slog.Error("lookup subscriber", "from", from, "err", err)
		return ""
	}

	// --- Subscriber in unsubscribe confirmation flow? ---
	if pending, ok := unsubPending[from]; ok && len(pending) > 0 {
		return handleUnsubConfirm(ctx, pool, twilio, sub, from, first, pending)
	}

	// --- Subscriber consent phase (has a pending subscription)? ---
	subs, err := ListSubscriptionsBySubscriber(ctx, pool, from)
	if err != nil {
		slog.Error("list subscriptions", "from", from, "err", err)
		return ""
	}
	hasPending := false
	for _, s := range subs {
		if s.Status == StatusPending {
			hasPending = true
			break
		}
	}
	if hasPending {
		return handleConsent(ctx, pool, from, first, subs)
	}

	// --- Subscriber post-trigger commands ---
	switch first {
	case "R":
		return handleRefresh(ctx, pool, from, subs)
	case "W":
		return handlePoll(ctx, pool, twilio, sub, from, subs, PollWhy)
	case "H":
		return handlePoll(ctx, pool, twilio, sub, from, subs, PollHow)
	case "U":
		return handleUnsubStart(ctx, pool, twilio, from, subs)
	default:
		slog.Info("unrecognized subscriber command", "from", from, "first", first)
		return ""
	}
}

// handleConsent processes Y / N / Q from a subscriber with pending subscriptions.
func handleConsent(ctx context.Context, pool *pgxpool.Pool, from, first string, subs []Subscription) string {
	switch first {
	case "Y":
		for _, s := range subs {
			if s.Status == StatusPending {
				if err := SetSubscriptionStatus(ctx, pool, s.ID, StatusSubscribed); err != nil {
					slog.Error("set subscribed", "sub_id", s.ID, "err", err)
				}
			}
		}
		slog.Info("subscriber consented", "phone", from)
		return "You are now subscribed. You will be notified if the deadman triggers."
	case "N":
		for _, s := range subs {
			if s.Status == StatusPending {
				if err := SetSubscriptionStatus(ctx, pool, s.ID, StatusDeclined); err != nil {
					slog.Error("set declined", "sub_id", s.ID, "err", err)
				}
			}
		}
		slog.Info("subscriber declined", "phone", from)
		return "Declined. You will not be notified."
	case "Q":
		for _, s := range subs {
			if s.Status == StatusPending {
				if err := SetSubscriptionStatus(ctx, pool, s.ID, StatusBlocked); err != nil {
					slog.Error("set blocked", "sub_id", s.ID, "err", err)
				}
			}
		}
		slog.Info("subscriber blocked", "phone", from)
		return "You have been removed and will not receive further messages from this service."
	default:
		return "Reply Y to subscribe, N to decline, Q to stop all messages."
	}
}

// handleRefresh re-sends current status for all triggered owners this subscriber watches.
func handleRefresh(ctx context.Context, pool *pgxpool.Pool, from string, subs []Subscription) string {
	var lines []string
	for _, s := range subs {
		if s.Status != StatusSubscribed {
			continue
		}
		owner, err := GetOwnerByPhone(ctx, pool, s.OwnerPhone)
		if err != nil {
			continue
		}
		lines = append(lines, StatusSMS(s.OwnerPhone, owner))
	}
	if len(lines) == 0 {
		return "No active deadman subscriptions."
	}
	return strings.Join(lines, "\n")
}

// handlePoll logs a W or H poll and notifies admins.
func handlePoll(ctx context.Context, pool *pgxpool.Pool, twilio TwilioConfig, sub Subscriber, from string, subs []Subscription, pt PollType) string {
	admins, err := ListAdmins(ctx, pool)
	if err != nil {
		slog.Error("list admins", "err", err)
	}

	for _, s := range subs {
		if s.Status != StatusSubscribed {
			continue
		}
		poll, err := CreatePoll(ctx, pool, s.ID, pt)
		if err != nil {
			slog.Error("create poll", "err", err)
			continue
		}
		slog.Info("poll created", "poll_id", poll.ID, "type", pt, "subscriber", from, "owner", s.OwnerPhone)

		// Alert all admins.
		msg := AdminPollSMS(pt, from, s.OwnerPhone)
		for _, admin := range admins {
			if err := SendSMS(twilio, admin.Phone, msg); err != nil {
				slog.Error("send admin poll SMS", "admin", admin.Phone, "err", err)
			}
		}
	}
	return PollAckSMS(pt)
}

// handleUnsubStart initiates the unsubscribe confirmation flow.
// Sends one confirmation SMS per active subscription and queues them in memory.
func handleUnsubStart(ctx context.Context, pool *pgxpool.Pool, twilio TwilioConfig, from string, subs []Subscription) string {
	var owners []string
	for _, s := range subs {
		if s.Status == StatusSubscribed || s.Status == StatusPending {
			owners = append(owners, s.OwnerPhone)
		}
	}
	if len(owners) == 0 {
		return "You have no active subscriptions."
	}
	unsubPending[from] = owners
	// Send one confirmation per owner.
	for _, ownerPhone := range owners {
		msg := UnsubscribeConfirmSMS(ownerPhone)
		if err := SendSMS(twilio, from, msg); err != nil {
			slog.Error("send unsub confirm", "to", from, "owner", ownerPhone, "err", err)
		}
	}
	return "" // confirmations sent as separate messages
}

// handleUnsubConfirm processes Y/N replies during the unsubscribe flow.
func handleUnsubConfirm(ctx context.Context, pool *pgxpool.Pool, twilio TwilioConfig, sub Subscriber, from, first string, pending []string) string {
	if len(pending) == 0 {
		delete(unsubPending, from)
		return "All done."
	}

	ownerPhone := pending[0]
	rest := pending[1:]

	switch first {
	case "Y":
		// Confirm unsubscribe for this owner.
		s, err := GetSubscriptionByOwnerAndSubscriber(ctx, pool, ownerPhone, from)
		if err == nil {
			if err := SetSubscriptionStatus(ctx, pool, s.ID, StatusDeclined); err != nil {
				slog.Error("unsub set declined", "err", err)
			}
		}
		slog.Info("unsubscribed", "subscriber", from, "owner", ownerPhone)
		_ = SendSMS(twilio, from, UnsubscribeDoneSMS(ownerPhone))
	case "N":
		slog.Info("unsub cancelled", "subscriber", from, "owner", ownerPhone)
	}

	if len(rest) == 0 {
		delete(unsubPending, from)
		return "All subscriptions processed."
	}
	unsubPending[from] = rest
	return "" // next confirmation already sent by handleUnsubStart
}

// MakeStatusHandler returns a JSON status endpoint for ops/debugging.
func MakeStatusHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		owners, err := ListOwners(ctx, pool)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type ownerOut struct {
			Phone       string `json:"phone"`
			Name        string `json:"name"`
			Status      string `json:"status"`
			LastCheckin string `json:"last_checkin"`
			Elapsed     string `json:"elapsed"`
		}
		var out []ownerOut
		for _, o := range owners {
			out = append(out, ownerOut{
				Phone:       o.Phone,
				Name:        o.Name,
				Status:      OwnerStatus(o),
				LastCheckin: o.LastCheckin.Format(time.RFC3339),
				Elapsed:     time.Since(o.LastCheckin).Round(time.Second).String(),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(out); err != nil {
			slog.Error("status encode", "err", err)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
