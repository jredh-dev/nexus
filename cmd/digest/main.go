// SPDX-License-Identifier: AGPL-3.0-or-later
// digest — bubbletea v2 TUI + CLI for the digest Kafka topic.
//
// Usage:
//
//	digest [stream]           — launch TUI (default)
//	digest set <tile> <val>   — publish override
//	digest reset <tile>       — publish reset
//	digest apply <tile> <fn>  — publish func override
//	digest tiles              — print tile JSON from HTTP
//	digest --help | -h | -?   — show this help
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"context"
	"github.com/jredh-dev/nexus/cmd/digest/internal/app"
	"github.com/jredh-dev/nexus/internal/digest/tiles"
	"github.com/segmentio/kafka-go"

	tea "charm.land/bubbletea/v2"
)

func main() {
	args := os.Args[1:]

	if len(args) == 0 || args[0] == "stream" {
		runTUI()
		return
	}

	switch args[0] {
	case "set":
		runSet(args[1:])
	case "reset":
		runReset(args[1:])
	case "apply":
		runApply(args[1:])
	case "tiles":
		runTiles()
	case "--help", "-h", "-?", "help":
		printUsage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

// printUsage writes the help text to stdout.
func printUsage() {
	fmt.Print(`digest — Kafka digest topic TUI and CLI

Usage:
  digest [stream]           launch TUI (streams tile snapshots)
  digest set <tile> <val>   publish value override
  digest reset <tile>       publish reset (clears override)
  digest apply <tile> <fn>  publish function change (avg/median/count/rate/last)
  digest tiles              fetch and print tile JSON from HTTP endpoint
  digest --help | -h | -?   show this help

Environment variables:
  KAFKA_ADDR            Kafka broker address  (default: kafka:9092)
  KAFKA_TOPIC_DIGEST    Kafka topic           (default: digest)
  KAFKA_GROUP_DIGEST    Consumer group ID     (default: digest-tui)
  DIGEST_URL            Digest HTTP base URL  (default: http://localhost:8096)
`)
}

// ── Environment helpers ────────────────────────────────────────────────────────

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func kafkaAddr() string  { return envOr("KAFKA_ADDR", "kafka:9092") }
func kafkaTopic() string { return envOr("KAFKA_TOPIC_DIGEST", "digest") }
func kafkaGroup() string { return envOr("KAFKA_GROUP_DIGEST", "digest-tui") }
func digestURL() string  { return envOr("DIGEST_URL", "http://localhost:8096") }

// ── TUI ───────────────────────────────────────────────────────────────────────

func runTUI() {
	m := app.New(kafkaAddr(), kafkaTopic(), kafkaGroup())
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
}

// ── CLI subcommands ────────────────────────────────────────────────────────────

func runSet(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: digest set <tile> <value>")
		os.Exit(1)
	}
	publish(tiles.OverrideRecord{
		Type:  "override",
		Tile:  args[0],
		Value: args[1],
	})
}

func runReset(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: digest reset <tile>")
		os.Exit(1)
	}
	publish(tiles.OverrideRecord{
		Type: "reset",
		Tile: args[0],
	})
}

func runApply(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: digest apply <tile> <func>")
		os.Exit(1)
	}
	publish(tiles.OverrideRecord{
		Type: "func",
		Tile: args[0],
		Func: args[1],
	})
}

// publish JSON-encodes rec and writes it to the Kafka topic, then exits.
func publish(rec tiles.OverrideRecord) {
	b, err := json.Marshal(rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal error: %v\n", err)
		os.Exit(1)
	}

	w := &kafka.Writer{
		Addr:     kafka.TCP(kafkaAddr()),
		Topic:    kafkaTopic(),
		Balancer: &kafka.LeastBytes{},
	}
	defer w.Close() //nolint:errcheck

	if err := w.WriteMessages(context.Background(), kafka.Message{Value: b}); err != nil {
		fmt.Fprintf(os.Stderr, "publish error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("published: %s\n", b)
}

// runTiles fetches the /tiles endpoint and prints the JSON response.
func runTiles() {
	url := digestURL() + "/tiles"
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		fmt.Fprintf(os.Stderr, "http error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read error: %v\n", err)
		os.Exit(1)
	}

	// Pretty-print if valid JSON.
	var pretty any
	if json.Unmarshal(body, &pretty) == nil {
		out, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Println(string(out))
		return
	}
	fmt.Println(string(body))
}
