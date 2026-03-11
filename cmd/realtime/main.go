// SPDX-License-Identifier: AGPL-3.0-or-later
// Binary realtime streams the realtime Kafka topic as a color-coded TUI.
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/segmentio/kafka-go"

	"github.com/jredh-dev/nexus/cmd/realtime/internal/app"
	kafkatypes "github.com/jredh-dev/nexus/internal/kafka"
)

func main() {
	kafkaAddr := envOr("KAFKA_ADDR", "kafka:9092")
	topic := envOr("KAFKA_TOPIC_REALTIME", "realtime")
	group := envOr("KAFKA_GROUP_REALTIME", "realtime-tui")
	keyHex := os.Getenv("REALTIME_KEY")
	maxLinesStr := envOr("MAX_LINES", "200")

	if keyHex == "" {
		log.Fatal("REALTIME_KEY is required")
	}
	key, err := kafkatypes.ParseKey(keyHex)
	if err != nil {
		log.Fatalf("REALTIME_KEY: %v", err)
	}
	maxLines, _ := strconv.Atoi(maxLinesStr)
	if maxLines <= 0 {
		maxLines = 200
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{kafkaAddr},
		Topic:          topic,
		GroupID:        group,
		MinBytes:       1,
		MaxBytes:       1e6,
		MaxWait:        500 * time.Millisecond,
		CommitInterval: time.Second,
	})
	defer reader.Close()

	m := app.New(reader, key, maxLines)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
