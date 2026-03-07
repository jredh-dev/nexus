// Package wallet provides a scaffold for crypto wallet-based deadman reset.
//
// Concept (from PLAN.md):
//   - Owner locks a crypto wallet address into the deadman system at registration.
//   - At trigger time, the deadman checks whether any outbound transfer occurred
//     from that wallet AFTER the last check-in.  If yes → reset the timer (owner
//     is demonstrably active); if no → fire the alert as normal.
//   - Phone number is NEVER associated with the wallet in any log or DB column;
//     the link exists only in the owner row (wallet_address field, optional).
//   - Uses a public blockchain API (e.g. Etherscan, Blockstream for Bitcoin)
//     with no authentication required for read-only queries.
//
// This file is a SCAFFOLD.  The interfaces and types are defined; the actual
// HTTP polling calls are stubbed with TODO markers.  Wire it up once the
// blockchain API choice is finalized.
package wallet

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Chain identifies a supported blockchain.
type Chain string

const (
	// ChainEthereum uses Etherscan-compatible APIs (Ethereum mainnet).
	ChainEthereum Chain = "ethereum"
	// ChainBitcoin uses Blockstream.info API (Bitcoin mainnet).
	ChainBitcoin Chain = "bitcoin"
)

// Activity represents the most recent outbound transaction from a wallet.
type Activity struct {
	// TxHash is the transaction identifier.
	TxHash string
	// Timestamp is when the transaction was confirmed.
	Timestamp time.Time
	// AmountSat is the amount in the smallest unit (satoshi / wei).
	AmountSat int64
}

// Watcher polls a blockchain for wallet activity.
// Implementations must be safe for concurrent use.
type Watcher interface {
	// LastActivity returns the most recent outbound transaction from address,
	// or (nil, nil) if the wallet has no outbound transactions.
	LastActivity(ctx context.Context, address string) (*Activity, error)
}

// NewWatcher returns a Watcher for the given chain.
// Returns an error for unsupported chains.
func NewWatcher(chain Chain) (Watcher, error) {
	switch chain {
	case ChainEthereum:
		return &ethWatcher{}, nil
	case ChainBitcoin:
		return &btcWatcher{}, nil
	default:
		return nil, fmt.Errorf("unsupported chain: %s", chain)
	}
}

// HasActivitySince returns true if the wallet had any outbound transaction
// strictly after the given time.  Used by the deadman ticker to decide
// whether to reset the timer instead of firing.
func HasActivitySince(ctx context.Context, w Watcher, address string, after time.Time) (bool, error) {
	act, err := w.LastActivity(ctx, address)
	if err != nil {
		return false, err
	}
	if act == nil {
		return false, nil
	}
	return act.Timestamp.After(after), nil
}

// -----------------------------------------------------------------------
// Ethereum watcher (Etherscan-compatible, no API key for basic queries)
// -----------------------------------------------------------------------

type ethWatcher struct{}

func (e *ethWatcher) LastActivity(ctx context.Context, address string) (*Activity, error) {
	// TODO: GET https://api.etherscan.io/api
	//         ?module=account&action=txlist
	//         &address=<address>
	//         &sort=desc&page=1&offset=1
	//         &startblock=0&endblock=99999999
	//
	// Filter result to txns where result[0].from == address (outbound).
	// Parse result[0].timeStamp (unix seconds) → Activity.Timestamp.
	//
	// Note: Etherscan free tier allows 5 req/s — more than sufficient for
	// a periodic deadman check (once per checkin_interval).
	slog.Warn("ethWatcher.LastActivity: not implemented", "address", address)
	return nil, nil
}

// -----------------------------------------------------------------------
// Bitcoin watcher (Blockstream.info, no auth required)
// -----------------------------------------------------------------------

type btcWatcher struct{}

func (b *btcWatcher) LastActivity(ctx context.Context, address string) (*Activity, error) {
	// TODO: GET https://blockstream.info/api/address/<address>/txs
	//
	// Parse the first tx where any vin[].prevout.scriptpubkey_address == address
	// (outbound spend).  Use tx.status.block_time for the timestamp.
	//
	// Blockstream has no rate limit for public anonymous queries.
	slog.Warn("btcWatcher.LastActivity: not implemented", "address", address)
	return nil, nil
}
