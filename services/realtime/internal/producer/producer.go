// Package producer wraps the kafka-go writer with AES-256-GCM encryption.
// Each published Event is encrypted into an Envelope before being serialised
// to JSON and written to the Kafka topic.
package producer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	kafkago "github.com/segmentio/kafka-go"

	kafkatypes "github.com/jredh-dev/nexus/internal/kafka"
)

// Producer encrypts and publishes events to a Kafka topic.
type Producer struct {
	writer *kafkago.Writer
	key    []byte
	source string
}

// New creates a Producer connected to the given broker address and topic.
// source is the label stamped on every Envelope's Source field.
// key must be 32 bytes (AES-256).
func New(addr, topic, source string, key []byte) *Producer {
	w := &kafkago.Writer{
		Addr:                   kafkago.TCP(addr),
		Topic:                  topic,
		Balancer:               &kafkago.LeastBytes{},
		AllowAutoTopicCreation: true, // convenient for dev; disable in prod
	}
	return &Producer{
		writer: w,
		key:    key,
		source: source,
	}
}

// Publish encrypts event e and writes it as an Envelope to Kafka.
// traceID is attached to the envelope for end-to-end correlation.
func (p *Producer) Publish(ctx context.Context, traceID string, e kafkatypes.Event) error {
	// Serialise the inner event to JSON before encrypting.
	plaintext, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("producer: marshal event: %w", err)
	}

	ciphertext, nonce, err := kafkatypes.Encrypt(p.key, plaintext)
	if err != nil {
		return fmt.Errorf("producer: encrypt: %w", err)
	}

	env := kafkatypes.Envelope{
		TraceID:   traceID,
		Timestamp: time.Now().UTC(),
		Source:    p.source,
		Payload:   ciphertext,
		Nonce:     nonce,
	}

	envBytes, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("producer: marshal envelope: %w", err)
	}

	msg := kafkago.Message{
		Key:   []byte(traceID), // use traceID as partition key for ordering
		Value: envBytes,
	}
	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		return fmt.Errorf("producer: write message: %w", err)
	}
	return nil
}

// Close closes the underlying Kafka writer, flushing any buffered messages.
func (p *Producer) Close() error {
	return p.writer.Close()
}
