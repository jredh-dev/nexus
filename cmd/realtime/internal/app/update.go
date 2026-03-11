// SPDX-License-Identifier: AGPL-3.0-or-later
package app

import (
	"context"
	"encoding/json"

	tea "charm.land/bubbletea/v2"
	kafkatypes "github.com/jredh-dev/nexus/internal/kafka"
	tuitypes "github.com/jredh-dev/nexus/internal/tui"
)

// Update implements tea.Model. Handles all incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// WindowSizeMsg handled via embedded WindowSize.
	if m.WindowSize.Handle(msg) {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		k := msg.Key()
		if tuitypes.IsQuit(k) {
			return m, tea.Quit
		}
		switch msg.String() {
		case "p", "P":
			m.paused = !m.paused
			return m, nil
		}

	case tea.InterruptMsg:
		// ctrl+c
		return m, tea.Quit

	case MsgNewLine:
		m.lines = append(m.lines, msg)
		// Trim buffer to maxLines.
		if m.maxLines > 0 && len(m.lines) > m.maxLines {
			m.lines = m.lines[len(m.lines)-m.maxLines:]
		}
		// Reschedule consumer — only if not paused. If paused we still
		// reschedule so messages keep buffering behind the scenes; the
		// view just doesn't scroll, and the buffer is trimmed above.
		return m, m.nextMsg()

	case MsgKafkaError:
		m.err = msg.Err
		// Keep retrying — transient errors are common.
		return m, m.nextMsg()
	}

	return m, nil
}

// nextMsg returns a tea.Cmd that reads exactly one message from Kafka,
// decrypts it, and returns a MsgNewLine (or MsgKafkaError on failure).
// The pattern is self-rescheduling: Update always re-issues nextMsg after
// receiving a MsgNewLine, so the consumer loop runs indefinitely.
func (m Model) nextMsg() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		rawMsg, err := m.reader.ReadMessage(ctx)
		if err != nil {
			return tuitypes.MsgKafkaError{Err: err}
		}

		// Unmarshal the on-wire Envelope.
		var env kafkatypes.Envelope
		if err := json.Unmarshal(rawMsg.Value, &env); err != nil {
			return tuitypes.MsgKafkaError{Err: err}
		}

		// Decrypt the inner Event payload.
		plaintext, err := kafkatypes.Decrypt(m.key, env.Payload, env.Nonce)
		if err != nil {
			return tuitypes.MsgKafkaError{Err: err}
		}

		var event kafkatypes.Event
		if err := json.Unmarshal(plaintext, &event); err != nil {
			return tuitypes.MsgKafkaError{Err: err}
		}

		return MsgNewLine{
			Timestamp: env.Timestamp,
			Level:     event.Level,
			Source:    env.Source,
			Message:   event.Message,
			TraceID:   env.TraceID,
			Fields:    event.Fields,
		}
	}
}
