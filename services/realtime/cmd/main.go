// nexus-realtime — Kafka producer service with AES-256-GCM event envelopes.
// Copyright (C) 2026  nexus contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"

	kafkatypes "github.com/jredh-dev/nexus/internal/kafka"
	"github.com/jredh-dev/nexus/services/realtime/internal/producer"
	"github.com/jredh-dev/nexus/services/realtime/internal/server"
)

// config holds all runtime configuration parsed from environment variables.
type config struct {
	kafkaAddr      string
	kafkaTopic     string
	realtimeKey    string // 64-char hex AES-256-GCM key (required)
	source         string
	tickerInterval time.Duration
	port           string
}

// loadConfig reads env vars with sensible defaults. It does NOT validate
// REALTIME_KEY — that happens in main so we can produce a fatal log message.
func loadConfig() config {
	return config{
		kafkaAddr:      envOr("KAFKA_ADDR", "kafka:9092"),
		kafkaTopic:     envOr("KAFKA_TOPIC_REALTIME", "realtime"),
		realtimeKey:    os.Getenv("REALTIME_KEY"),
		source:         envOr("REALTIME_SOURCE", "realtime"),
		tickerInterval: parseDuration(envOr("REALTIME_TICKER_INTERVAL", "2s")),
		port:           envOr("PORT", "8097"),
	}
}

func main() {
	cfg := loadConfig()

	// Parse and validate the encryption key — fatal if missing or malformed.
	if cfg.realtimeKey == "" {
		log.Fatal("REALTIME_KEY is required: set a 64-char hex AES-256-GCM key")
	}
	key, err := kafkatypes.ParseKey(cfg.realtimeKey)
	if err != nil {
		log.Fatalf("REALTIME_KEY invalid: %v", err)
	}

	// Create the Kafka producer.
	p := producer.New(cfg.kafkaAddr, cfg.kafkaTopic, cfg.source, key)

	// Build and configure the HTTP server.
	srv := server.New(p, true)
	srv.OnStop(func() {
		if err := p.Close(); err != nil {
			log.Printf("producer close: %v", err)
		}
	})

	// Start synthetic event ticker in the background.
	go runTicker(p, cfg.tickerInterval)

	addr := ":" + cfg.port
	log.Printf("nexus-realtime starting on %s (kafka=%s topic=%s interval=%s)",
		addr, cfg.kafkaAddr, cfg.kafkaTopic, cfg.tickerInterval)

	if err := srv.ListenAndServe(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// runTicker publishes rotating synthetic events at the configured interval.
// Useful for local dev and demo environments — a Kafka consumer can verify
// the pipeline end-to-end without needing a real event source.
func runTicker(p *producer.Producer, interval time.Duration) {
	// Cycle through event levels to exercise the full spectrum.
	levels := []string{kafkatypes.LevelInfo, kafkatypes.LevelWarn, kafkatypes.LevelError}
	idx := 0

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for t := range ticker.C {
		level := levels[idx%len(levels)]
		idx++

		// Generate a plausible latency value that slowly drifts for realism.
		latencyMs := 20 + (idx*7)%200

		event := kafkatypes.Event{
			Level:   level,
			Message: fmt.Sprintf("synthetic tick at %s", t.UTC().Format(time.RFC3339)),
			Fields: map[string]string{
				"latency_ms": strconv.Itoa(latencyMs),
				"tick":       strconv.Itoa(idx),
				"source":     "ticker",
			},
		}

		traceID := uuid.New().String()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := p.Publish(ctx, traceID, event); err != nil {
			log.Printf("[ticker] publish error (level=%s tick=%d): %v", level, idx, err)
		} else {
			log.Printf("[ticker] published trace=%s level=%s latency_ms=%d", traceID, level, latencyMs)
		}
		cancel()
	}
}

// envOr returns the value of env var key or fallback if unset/empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseDuration parses s as a time.Duration, falling back to 2s on error.
func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Printf("invalid REALTIME_TICKER_INTERVAL %q, using 2s: %v", s, err)
		return 2 * time.Second
	}
	return d
}
