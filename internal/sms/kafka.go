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

// Package sms provides types and services for outbound SMS delivery via
// Kafka-driven pub/sub and configurable SMS backends.
package sms

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

const (
	// OutboxTopic is where producers publish messages they want sent as SMS.
	OutboxTopic = "sms-outbox"

	// DLQTopic is where messages that exhaust all retries are written so they
	// can be inspected and replayed manually without blocking the main consumer.
	DLQTopic = "sms-dlq"

	// maxRetries is the number of delivery attempts before a message is routed
	// to the DLQ.  Each attempt adds a short exponential backoff.
	maxRetries = 3
)

// Consumer reads OutboundMessages from the sms-outbox Kafka topic and
// dispatches them via a Sender.  It commits Kafka offsets only after a
// successful send, providing at-least-once delivery semantics.
//
// On repeated failure a message is forwarded to sms-dlq so the consumer can
// continue making progress without losing the problematic record.
//
// Design rationale:
//   - segmentio/kafka-go is chosen over confluent-kafka-go because it is pure
//     Go (no CGO, no librdkafka dependency), making it straightforward to build
//     a small static Docker image.
//   - At-least-once (not exactly-once) is acceptable here because SMS delivery
//     itself is not idempotent at the carrier level regardless; the recipient
//     sees a duplicate text rather than a silent miss.  Producers should use
//     stable IDs so duplicates can be detected if needed.
type Consumer struct {
	reader *kafka.Reader
	dlq    *kafka.Writer
	sender Sender
}

// NewConsumer creates a Consumer connected to the given Kafka brokers.
// brokers should be a comma-separated list like "kafka:9092".
func NewConsumer(brokers []string, sender Sender) *Consumer {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        brokers,
		Topic:          OutboxTopic,
		GroupID:        "nascent-nexus-sms-sender",
		MinBytes:       1,
		MaxBytes:       1 << 20, // 1 MiB
		CommitInterval: 0,       // explicit commits only
		StartOffset:    kafka.LastOffset,
	})

	dlq := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        DLQTopic,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireOne,
	}

	return &Consumer{
		reader: reader,
		dlq:    dlq,
		sender: sender,
	}
}

// Run blocks, consuming messages until ctx is cancelled.
// It logs each attempt and handles retries + DLQ routing internally.
func (c *Consumer) Run(ctx context.Context) error {
	log.Printf("sms-sender: consuming from topic %q", OutboxTopic)

	for {
		m, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				// Clean shutdown.
				return nil
			}
			return fmt.Errorf("fetch: %w", err)
		}

		if err := c.dispatch(ctx, m); err != nil {
			// dispatch already logged the error and sent to DLQ.
			// We still commit so the consumer does not get stuck.
			log.Printf("sms-sender: routed message key=%s to DLQ: %v", string(m.Key), err)
		}

		if err := c.reader.CommitMessages(ctx, m); err != nil {
			log.Printf("sms-sender: commit failed (message may be redelivered): %v", err)
		}
	}
}

// Close releases all Kafka resources.
func (c *Consumer) Close() error {
	rerr := c.reader.Close()
	werr := c.dlq.Close()
	if rerr != nil {
		return rerr
	}
	return werr
}

// dispatch attempts to send msg up to maxRetries times with exponential
// backoff.  If all attempts fail it writes the raw Kafka message to the DLQ.
func (c *Consumer) dispatch(ctx context.Context, m kafka.Message) error {
	var msg OutboundMessage
	if err := json.Unmarshal(m.Value, &msg); err != nil {
		return c.sendToDLQ(ctx, m, fmt.Errorf("unmarshal: %w", err))
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		lastErr = c.sender.Send(ctx, msg)
		if lastErr == nil {
			log.Printf("sms-sender: sent id=%s to=%s (attempt %d)", msg.ID, msg.To, attempt)
			return nil
		}

		log.Printf("sms-sender: attempt %d/%d failed for id=%s: %v", attempt, maxRetries, msg.ID, lastErr)

		if attempt < maxRetries {
			backoff := time.Duration(attempt) * 2 * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return c.sendToDLQ(ctx, m, lastErr)
}

// sendToDLQ writes the original raw Kafka message to the dead-letter topic so
// it can be inspected and replayed without blocking the main consumer.
func (c *Consumer) sendToDLQ(ctx context.Context, original kafka.Message, reason error) error {
	err := c.dlq.WriteMessages(ctx, kafka.Message{
		Key:   original.Key,
		Value: original.Value,
	})
	if err != nil {
		log.Printf("sms-sender: CRITICAL — could not write to DLQ: %v", err)
	}
	return reason
}
