// SPDX-License-Identifier: AGPL-3.0-or-later
package app

import (
	"time"

	tuitypes "github.com/jredh-dev/nexus/internal/tui"
)

// MsgNewLine is sent when a new decrypted log line arrives from Kafka.
type MsgNewLine struct {
	Timestamp time.Time
	Level     string
	Source    string
	Message   string
	TraceID   string
	Fields    map[string]string
}

// MsgKafkaError is sent when the Kafka consumer encounters an error.
// Uses the shared type from internal/tui.
type MsgKafkaError = tuitypes.MsgKafkaError
