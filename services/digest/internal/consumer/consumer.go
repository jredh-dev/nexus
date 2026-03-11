// Package consumer implements two Kafka consumer loops for the digest service:
//
//  1. realtime topic — decrypts Envelopes and feeds them into a rolling
//     Collector window.
//  2. digest topic — applies OverrideRecord messages (override / reset / func)
//     that arrive from the CLI or TUI.
//
// Every DIGEST_TICK_INTERVAL (default 15 min) the ticker fires and runs the
// configured reducer over the current Collector window, updating each tile's
// value.  Overridden tiles are skipped until they are explicitly reset.
//
// A TileSnapshot is published back to the digest topic on every tick so that
// TUI consumers can react in near-real-time.
package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	"github.com/jredh-dev/nexus/internal/collector"
	kafkatypes "github.com/jredh-dev/nexus/internal/kafka"
	"github.com/jredh-dev/nexus/services/digest/internal/tiles"
)

// Consumer coordinates the realtime consumer, digest consumer, and tick-based
// reducer loop.  Safe for concurrent use via the exported Tiles() method.
type Consumer struct {
	realtimeReader *kafkago.Reader
	digestReader   *kafkago.Reader
	col            *collector.Collector[kafkatypes.Envelope]
	tileMap        map[string]*tiles.TileValue // keyed by TileValue.Name
	mu             sync.RWMutex
	key            []byte // AES-256 key for decrypting realtime envelopes
	ticker         *time.Ticker
	tickInterval   time.Duration
	producer       *kafkago.Writer // publishes TileSnapshot back to digest topic
}

// New constructs a Consumer.  kafkaAddr is the broker address (e.g. "kafka:9092").
// key must be the 32-byte AES-256 key for the realtime topic.
func New(kafkaAddr, realtimeTopic, digestTopic, groupID string, tickInterval time.Duration, key []byte) *Consumer {
	realtimeReader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers: []string{kafkaAddr},
		Topic:   realtimeTopic,
		GroupID: groupID + "-realtime",
	})

	digestReader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers: []string{kafkaAddr},
		Topic:   digestTopic,
		GroupID: groupID,
	})

	producer := &kafkago.Writer{
		Addr:                   kafkago.TCP(kafkaAddr),
		Topic:                  digestTopic,
		Balancer:               &kafkago.LeastBytes{},
		AllowAutoTopicCreation: true,
	}

	// Initialise each tile with a zero value.
	tileMap := make(map[string]*tiles.TileValue, len(tiles.AllTiles))
	for _, name := range tiles.AllTiles {
		tileMap[name] = &tiles.TileValue{
			Name:      name,
			UpdatedAt: time.Now().UTC(),
		}
	}

	return &Consumer{
		realtimeReader: realtimeReader,
		digestReader:   digestReader,
		col:            collector.New[kafkatypes.Envelope](tickInterval),
		tileMap:        tileMap,
		key:            key,
		ticker:         time.NewTicker(tickInterval),
		tickInterval:   tickInterval,
		producer:       producer,
	}
}

// Run starts all consumer loops and the tick loop.  Blocks until ctx is
// cancelled.  Returns the first fatal error encountered.
func (c *Consumer) Run(ctx context.Context) error {
	errCh := make(chan error, 3)

	// Consume realtime envelopes — decrypt and add to rolling window.
	go func() {
		if err := c.consumeRealtime(ctx); err != nil {
			errCh <- fmt.Errorf("realtime consumer: %w", err)
		}
	}()

	// Consume digest topic — apply override/reset/func records.
	go func() {
		if err := c.consumeDigest(ctx); err != nil {
			errCh <- fmt.Errorf("digest consumer: %w", err)
		}
	}()

	// Tick loop — reduce window, publish snapshot.
	go func() {
		if err := c.runTicker(ctx); err != nil {
			errCh <- fmt.Errorf("ticker: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		c.ticker.Stop()
		_ = c.realtimeReader.Close()
		_ = c.digestReader.Close()
		_ = c.producer.Close()
		return ctx.Err()
	case err := <-errCh:
		c.ticker.Stop()
		_ = c.realtimeReader.Close()
		_ = c.digestReader.Close()
		_ = c.producer.Close()
		return err
	}
}

// Tiles returns a snapshot of all current tile values.  Thread-safe.
func (c *Consumer) Tiles() []tiles.TileValue {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]tiles.TileValue, 0, len(tiles.AllTiles))
	for _, name := range tiles.AllTiles {
		if tv, ok := c.tileMap[name]; ok {
			out = append(out, *tv) // copy so caller cannot mutate
		}
	}
	return out
}

// --- Internal loops ---

// consumeRealtime reads from the realtime topic, decrypts each Envelope, and
// adds it to the rolling Collector window.
func (c *Consumer) consumeRealtime(ctx context.Context) error {
	for {
		m, err := c.realtimeReader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil // context cancelled — clean exit
			}
			log.Printf("[consumer/realtime] read error: %v", err)
			continue
		}

		var env kafkatypes.Envelope
		if err := json.Unmarshal(m.Value, &env); err != nil {
			log.Printf("[consumer/realtime] unmarshal envelope: %v", err)
			continue
		}

		// Add the raw envelope (with encrypted payload) to the window.
		// Decryption happens lazily during the reducer tick so we pay the
		// cost once per window cycle rather than per message.
		c.col.Add(env)
	}
}

// consumeDigest reads from the digest topic and processes OverrideRecord
// messages.  TileSnapshot messages (published by our own ticker) are silently
// ignored — they carry a "tiles" key, not a "type" key.
func (c *Consumer) consumeDigest(ctx context.Context) error {
	for {
		m, err := c.digestReader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("[consumer/digest] read error: %v", err)
			continue
		}

		// Try to decode as an OverrideRecord.
		var rec tiles.OverrideRecord
		if err := json.Unmarshal(m.Value, &rec); err != nil {
			log.Printf("[consumer/digest] unmarshal record: %v", err)
			continue
		}

		// Ignore messages that don't have a recognised "type" field —
		// these are likely TileSnapshot messages published by the ticker.
		switch rec.Type {
		case "override", "reset", "func":
			c.applyRecord(rec)
		default:
			// Not an override record (e.g. snapshot) — ignore.
		}
	}
}

// applyRecord applies an OverrideRecord to the tile map.
// Safe to call from the digest consumer goroutine only; the mu write lock
// ensures visibility to Tiles() callers.
func (c *Consumer) applyRecord(rec tiles.OverrideRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()

	tv, ok := c.tileMap[rec.Tile]
	if !ok {
		log.Printf("[consumer/digest] unknown tile %q in %s record — ignoring", rec.Tile, rec.Type)
		return
	}

	switch rec.Type {
	case "override":
		// Manually set tile value and mark as overridden.
		tv.Value = rec.Value
		tv.Overridden = true
		tv.UpdatedAt = time.Now().UTC()
		log.Printf("[consumer/digest] override tile=%s value=%v", rec.Tile, rec.Value)

	case "reset":
		// Clear override; next tick will recompute.
		tv.Overridden = false
		tv.UpdatedAt = time.Now().UTC()
		log.Printf("[consumer/digest] reset tile=%s", rec.Tile)

	case "func":
		// Store preferred reducer name; does not override the value.
		tv.Func = rec.Func
		tv.UpdatedAt = time.Now().UTC()
		log.Printf("[consumer/digest] func tile=%s func=%s", rec.Tile, rec.Func)
	}
}

// runTicker fires on every tick interval.  It snapshots the Collector window,
// runs reducers, updates non-overridden tiles, and publishes a TileSnapshot
// to the digest topic.
func (c *Consumer) runTicker(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case t := <-c.ticker.C:
			log.Printf("[consumer/ticker] tick at %s — reducing window", t.UTC().Format(time.RFC3339))
			c.reduce()
			if err := c.publishSnapshot(ctx); err != nil {
				log.Printf("[consumer/ticker] publish snapshot: %v", err)
				// Non-fatal — keep ticking.
			}
		}
	}
}

// reduce runs the configured reducers over the current Collector window and
// updates any tile that is not currently overridden.
func (c *Consumer) reduce() {
	snap := c.col.Snapshot()

	// Decrypt all envelopes once.  Events that fail decryption are skipped.
	type pair struct {
		env   kafkatypes.Envelope
		event kafkatypes.Event
		ok    bool
	}
	pairs := make([]pair, len(snap))
	for i, env := range snap {
		plaintext, err := kafkatypes.Decrypt(c.key, env.Payload, env.Nonce)
		if err != nil {
			// Envelope may come from a different source or key mismatch; skip.
			log.Printf("[consumer/reducer] decrypt trace=%s: %v", env.TraceID, err)
			continue
		}
		var evt kafkatypes.Event
		if err := json.Unmarshal(plaintext, &evt); err != nil {
			log.Printf("[consumer/reducer] unmarshal event trace=%s: %v", env.TraceID, err)
			continue
		}
		pairs[i] = pair{env: env, event: evt, ok: true}
	}

	// --- avg_latency_ms ---
	// Average latency_ms field across all envelopes that carry it.
	avgLatency := func() float64 {
		var sum float64
		var n int
		for _, p := range pairs {
			if !p.ok {
				continue
			}
			if v, ok := p.event.Fields["latency_ms"]; ok {
				f, err := strconv.ParseFloat(v, 64)
				if err == nil {
					sum += f
					n++
				}
			}
		}
		if n == 0 {
			return 0
		}
		return sum / float64(n)
	}()

	// --- publish_rate --- (events/min over the full tick window)
	publishRate := collector.Rate[kafkatypes.Envelope](c.tickInterval)(snap)

	// --- error_rate --- (ERROR events/min over the full tick window)
	errorEnvs := collector.Filter[kafkatypes.Envelope](func(e kafkatypes.Envelope) bool {
		// We rely on the pairs map to check level without re-decrypting.
		for _, p := range pairs {
			if p.ok && p.env.TraceID == e.TraceID {
				return p.event.Level == kafkatypes.LevelError
			}
		}
		return false
	})(snap)
	errorRate := collector.Rate[kafkatypes.Envelope](c.tickInterval)(errorEnvs)

	// --- last_error --- (last error message in the window)
	lastError := ""
	for i := len(pairs) - 1; i >= 0; i-- {
		p := pairs[i]
		if p.ok && p.event.Level == kafkatypes.LevelError {
			lastError = p.event.Message
			if p.env.TraceID != "" {
				lastError += " trace=" + p.env.TraceID
			}
			break
		}
	}

	// --- event_count ---
	eventCount := collector.Count[kafkatypes.Envelope]()(snap)

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now().UTC()
	setIfNotOverridden := func(name string, value any) {
		tv, ok := c.tileMap[name]
		if !ok || tv.Overridden {
			return
		}
		tv.Value = value
		tv.UpdatedAt = now
	}

	setIfNotOverridden(tiles.TileAvgLatency, avgLatency)
	setIfNotOverridden(tiles.TilePublishRate, publishRate)
	setIfNotOverridden(tiles.TileErrorRate, errorRate)
	setIfNotOverridden(tiles.TileLastError, lastError)
	setIfNotOverridden(tiles.TileEventCount, eventCount)
}

// publishSnapshot serialises the current tile state as a TileSnapshot and
// writes it to the digest topic so TUI consumers can update.
func (c *Consumer) publishSnapshot(ctx context.Context) error {
	snap := tiles.TileSnapshot{
		Tiles:      c.Tiles(),
		ComputedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	msg := kafkago.Message{Value: data}
	if err := c.producer.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}
