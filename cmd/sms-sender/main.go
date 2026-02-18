// nascent-nexus - Personal AI assistant system
// Copyright (C) 2025  nascent-nexus contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.

// sms-sender is a long-running Kafka consumer that reads outbound SMS messages
// from the "sms-outbox" topic and delivers them via the configured SMS backend.
//
// Configuration is done entirely via environment variables so the binary runs
// identically in Docker, on bare metal, or in any CI environment:
//
//	KAFKA_BROKERS       comma-separated broker list, e.g. "kafka:9092"
//	TELNYX_API_KEY      Telnyx API v2 key (starts with "KEY...")
//	TELNYX_FROM_NUMBER  E.164 number provisioned in Telnyx, e.g. "+15550001234"
//
// The service joins the "agentic-network" Docker network so it can reach the
// existing Kafka cluster by its service-discovery hostname ("kafka").
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jredh-dev/nascent-nexus/internal/sms"
)

func main() {
	brokers := requireEnv("KAFKA_BROKERS")
	apiKey := requireEnv("TELNYX_API_KEY")
	fromNumber := requireEnv("TELNYX_FROM_NUMBER")

	sender := sms.NewTelnyxSender(apiKey, fromNumber)
	consumer := sms.NewConsumer(strings.Split(brokers, ","), sender)
	defer func() {
		if err := consumer.Close(); err != nil {
			log.Printf("sms-sender: error closing consumer: %v", err)
		}
	}()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log.Printf("sms-sender: starting (brokers=%s from=%s)", brokers, fromNumber)
	if err := consumer.Run(ctx); err != nil {
		log.Fatalf("sms-sender: fatal error: %v", err)
	}
	log.Println("sms-sender: shutdown complete")
}

// requireEnv returns the value of the named environment variable or calls
// log.Fatal if it is empty.  This keeps startup-time misconfiguration loud and
// obvious rather than surfacing as a runtime nil-pointer or auth failure later.
func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("sms-sender: required environment variable %q is not set", key)
	}
	return v
}
