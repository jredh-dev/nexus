// Package kafka defines shared event types for the realtime Kafka pipeline.
// All events are transmitted as encrypted Envelope messages; the inner Event
// is only visible after decryption with the shared AES-256-GCM key.
package kafka

import "time"

// Envelope is the on-wire format for all realtime events.
// Payload is AES-256-GCM encrypted; Nonce is the 12-byte GCM nonce.
// TraceID is propagated from the originating event for end-to-end tracing.
type Envelope struct {
	TraceID   string    `json:"trace_id"`
	Timestamp time.Time `json:"ts"`
	Source    string    `json:"src"`
	Payload   []byte    `json:"payload"` // AES-256-GCM ciphertext
	Nonce     []byte    `json:"nonce"`   // 12-byte GCM nonce
}

// Event is the decrypted inner payload carried by an Envelope.
type Event struct {
	Level   string            `json:"level"` // INFO, WARN, ERROR
	Message string            `json:"msg"`
	Fields  map[string]string `json:"fields,omitempty"` // arbitrary key/value metadata
}

// Level constants for Event.Level.
const (
	LevelInfo  = "INFO"
	LevelWarn  = "WARN"
	LevelError = "ERROR"
)
