// Package deadman - per-owner ticker loop.
package deadman

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const tickPeriod = 1 * time.Minute

// RunTicker starts the ticker loop that checks all owners every minute.
// Blocks until ctx is cancelled.
func RunTicker(ctx context.Context, pool *pgxpool.Pool, twilio TwilioConfig) {
	tick := time.NewTicker(tickPeriod)
	defer tick.Stop()
	slog.Info("ticker started", "period", tickPeriod)

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := checkAllOwners(ctx, pool, twilio); err != nil {
				slog.Error("ticker error", "err", err)
			}
		}
	}
}

func checkAllOwners(ctx context.Context, pool *pgxpool.Pool, twilio TwilioConfig) error {
	owners, err := ListOwners(ctx, pool)
	if err != nil {
		return fmt.Errorf("list owners: %w", err)
	}
	for _, o := range owners {
		if err := checkOwner(ctx, pool, twilio, o); err != nil {
			slog.Error("check owner failed", "owner", o.Phone, "err", err)
		}
	}
	return nil
}

func checkOwner(ctx context.Context, pool *pgxpool.Pool, twilio TwilioConfig, o Owner) error {
	elapsed := time.Since(o.LastCheckin)

	slog.Debug("tick owner",
		"owner", o.Phone,
		"elapsed", elapsed.Round(time.Minute),
		"warn_sent", o.WarnSentAt != nil,
		"final_sent", o.FinalSentAt != nil,
	)

	// --- Final trigger ---
	if elapsed >= o.TriggerInterval && o.FinalSentAt == nil {
		// Alert all active subscribers.
		subs, err := ActiveSubscribersForOwner(ctx, pool, o.ID)
		if err != nil {
			return fmt.Errorf("active subscribers: %w", err)
		}
		msg := TriggerSMS(o.Phone)
		for _, sub := range subs {
			if err := SendSMS(twilio, sub.SubscriberPhone, msg); err != nil {
				slog.Error("send trigger SMS", "to", sub.SubscriberPhone, "err", err)
			}
		}
		// Also warn the owner themselves (last chance).
		ownerMsg := fmt.Sprintf("DEADMAN TRIGGERED: You have been dark for %s. Your subscribers have been notified.", elapsed.Round(time.Minute))
		if err := SendSMS(twilio, o.Phone, ownerMsg); err != nil {
			slog.Error("send trigger to owner", "owner", o.Phone, "err", err)
		}
		if err := MarkFinalSent(ctx, pool, o.ID); err != nil {
			return err
		}
		slog.Warn("deadman triggered", "owner", o.Phone, "elapsed", elapsed.Round(time.Minute), "subscribers_alerted", len(subs))
		return nil
	}

	// --- Warning ---
	if elapsed >= o.WarnInterval && o.WarnSentAt == nil && o.FinalSentAt == nil {
		remaining := (o.TriggerInterval - elapsed).Round(time.Minute)
		msg := fmt.Sprintf(
			"DEADMAN WARNING: No check-in for %s. Reply to reset. %s until your subscribers are alerted.",
			elapsed.Round(time.Minute), remaining,
		)
		if err := SendSMS(twilio, o.Phone, msg); err != nil {
			return fmt.Errorf("send warn SMS: %w", err)
		}
		if err := MarkWarnSent(ctx, pool, o.ID); err != nil {
			return err
		}
		slog.Warn("warning sent", "owner", o.Phone, "elapsed", elapsed.Round(time.Minute), "trigger_in", remaining)
	}

	return nil
}
