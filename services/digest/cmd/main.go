// nexus-digest — Kafka consumer service that computes tile metrics from the
// realtime topic and publishes TileSnapshot records to the digest topic.
// Copyright (C) 2026  nexus contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package main

import (
	"context"
	"log"
	"os"
	"time"

	kafkatypes "github.com/jredh-dev/nexus/internal/kafka"
	"github.com/jredh-dev/nexus/services/digest/internal/consumer"
	"github.com/jredh-dev/nexus/services/digest/internal/server"
)

// config holds all runtime configuration for the digest service, parsed from
// environment variables.
type config struct {
	kafkaAddr     string
	realtimeTopic string
	digestTopic   string
	groupID       string
	realtimeKey   string // 64-char hex AES-256-GCM key (required)
	tickInterval  time.Duration
	port          string
}

// loadConfig reads env vars with sensible defaults.
func loadConfig() config {
	return config{
		kafkaAddr:     envOr("KAFKA_ADDR", "kafka:9092"),
		realtimeTopic: envOr("KAFKA_TOPIC_REALTIME", "realtime"),
		digestTopic:   envOr("KAFKA_TOPIC_DIGEST", "digest"),
		groupID:       envOr("KAFKA_GROUP_DIGEST", "digest-service"),
		realtimeKey:   os.Getenv("REALTIME_KEY"),
		tickInterval:  parseDuration(envOr("DIGEST_TICK_INTERVAL", "15m")),
		port:          envOr("PORT", "8096"),
	}
}

func main() {
	cfg := loadConfig()

	// Parse and validate the AES-256 key — fatal if missing or malformed.
	if cfg.realtimeKey == "" {
		log.Fatal("REALTIME_KEY is required: set a 64-char hex AES-256-GCM key")
	}
	key, err := kafkatypes.ParseKey(cfg.realtimeKey)
	if err != nil {
		log.Fatalf("REALTIME_KEY invalid: %v", err)
	}

	log.Printf("nexus-digest starting: kafka=%s realtime=%s digest=%s group=%s tick=%s port=%s",
		cfg.kafkaAddr, cfg.realtimeTopic, cfg.digestTopic, cfg.groupID, cfg.tickInterval, cfg.port)

	// Build the Kafka consumer (also runs the reducer ticker internally).
	cons := consumer.New(
		cfg.kafkaAddr,
		cfg.realtimeTopic,
		cfg.digestTopic,
		cfg.groupID,
		cfg.tickInterval,
		key,
	)

	// Build the HTTP server backed by the consumer tile state.
	srv := server.New(cons)
	srv.OnStop(func() {
		log.Println("[main] HTTP server stopping")
	})

	// Run the consumer in the background.
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := cons.Run(ctx); err != nil {
			log.Printf("[main] consumer stopped: %v", err)
			cancel()
		}
	}()

	// OnStop will fire during graceful shutdown triggered inside ListenAndServe.
	srv.OnStop(cancel)

	addr := ":" + cfg.port
	if err := srv.ListenAndServe(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// envOr returns the value of env var key or fallback if unset/empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseDuration parses s as a time.Duration.  Falls back to 15m on error.
func parseDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		log.Printf("invalid duration %q, using 15m: %v", s, err)
		return 15 * time.Minute
	}
	return d
}
