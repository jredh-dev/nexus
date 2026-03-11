// nexus-dashboard — live ops dashboard backed by Kafka realtime + digest.
// Copyright (C) 2026  nexus contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package main

import (
	"log"
	"net/http"
	"os"
	"time"

	kafkatypes "github.com/jredh-dev/nexus/internal/kafka"
	"github.com/jredh-dev/nexus/services/dashboard/internal/server"
)

func main() {
	kafkaAddr := envOr("KAFKA_ADDR", "kafka:9092")
	kafkaTopic := envOr("KAFKA_TOPIC_REALTIME", "realtime")
	digestAddr := envOr("DIGEST_ADDR", "http://nexus-digest:8096")
	port := envOr("PORT", "8098")
	rawKey := os.Getenv("REALTIME_KEY")

	// Parse AES-256-GCM key — required.
	if rawKey == "" {
		log.Fatal("REALTIME_KEY is required")
	}
	key, err := kafkatypes.ParseKey(rawKey)
	if err != nil {
		log.Fatalf("REALTIME_KEY invalid: %v", err)
	}

	cfg := server.Config{
		KafkaAddr:  kafkaAddr,
		KafkaTopic: kafkaTopic,
		DigestAddr: digestAddr,
		Key:        key,
		// Keep the last 200 events in memory for new SSE subscribers.
		EventBufSize: 200,
	}

	srv := server.New(cfg)

	log.Printf("dashboard listening on :%s", port)
	httpSrv := &http.Server{
		Addr:         ":" + port,
		Handler:      srv,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // SSE streams are long-lived
		IdleTimeout:  120 * time.Second,
	}
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
