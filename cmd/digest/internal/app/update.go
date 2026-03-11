// SPDX-License-Identifier: AGPL-3.0-or-later
package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jredh-dev/nexus/internal/digest/tiles"
	tuitypes "github.com/jredh-dev/nexus/internal/tui"
	"github.com/segmentio/kafka-go"

	tea "charm.land/bubbletea/v2"
)

// Init satisfies tea.Model. Starts the background Kafka consumer loop.
func (m Model) Init() tea.Cmd {
	return nextMsg(m.kafkaAddr, m.digestTopic, m.groupID)
}

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// WindowSizeMsg handled via embedded WindowSize.
	if m.WindowSize.Handle(msg) {
		return m, nil
	}

	switch msg := msg.(type) {

	// ── kafka messages ───────────────────────────────────────────────────────
	case MsgTileSnapshot:
		m.tileMsgs = msg.Snap.Tiles
		m.statusMsg = ""
		return m, nextMsg(m.kafkaAddr, m.digestTopic, m.groupID)

	case MsgKafkaError:
		m.err = msg.Err
		// Keep retrying — transient network errors should not kill the TUI.
		return m, nextMsg(m.kafkaAddr, m.digestTopic, m.groupID)

	case MsgPublishDone:
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("publish error: %v", msg.Err)
		} else {
			m.statusMsg = "published"
		}
		return m, nil

	// ── keyboard ─────────────────────────────────────────────────────────────
	case tea.KeyPressMsg:
		k := msg.Key()
		switch m.mode {
		case modeNormal:
			return m.handleNormal(k)
		case modeEdit:
			return m.handleEdit(k, msg)
		case modeFuncPick:
			return m.handleFuncPick(k)
		}
	}

	return m, nil
}

// handleNormal processes key events in normal navigation mode.
// Tiles 0–3 are laid out in a 2-column grid; tile 4 occupies the full width.
func (m Model) handleNormal(k tea.Key) (tea.Model, tea.Cmd) {
	n := len(m.tileMsgs)

	if tuitypes.IsQuit(k) {
		return m, tea.Quit
	}

	if n == 0 {
		return m, nil
	}

	const cols = 2 // 2-column grid

	switch k.Code {
	case tea.KeyUp:
		if m.selectedIdx-cols >= 0 {
			m.selectedIdx -= cols
		}
	case tea.KeyDown:
		if m.selectedIdx+cols < n {
			m.selectedIdx += cols
		}
	case tea.KeyLeft:
		if m.selectedIdx > 0 {
			m.selectedIdx--
		}
	case tea.KeyRight:
		if m.selectedIdx < n-1 {
			m.selectedIdx++
		}

	case 'e':
		// Enter edit mode; pre-fill with current value.
		m.mode = modeEdit
		if m.selectedIdx < n {
			m.editInput = fmt.Sprintf("%v", m.tileMsgs[m.selectedIdx].Value)
		} else {
			m.editInput = ""
		}

	case 'r':
		// Publish reset record for the selected tile.
		if m.selectedIdx < n {
			name := m.tileMsgs[m.selectedIdx].Name
			rec := tiles.OverrideRecord{Type: "reset", Tile: name}
			return m, publishRecord(m.kafkaAddr, m.digestTopic, rec)
		}

	case 'f':
		// Open function picker popup.
		m.mode = modeFuncPick
		m.funcIdx = 0
	}

	return m, nil
}

// handleEdit processes key events while in inline-edit mode.
func (m Model) handleEdit(k tea.Key, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.Code {
	case tea.KeyEnter:
		if m.selectedIdx < len(m.tileMsgs) {
			name := m.tileMsgs[m.selectedIdx].Name
			rec := tiles.OverrideRecord{
				Type:  "override",
				Tile:  name,
				Value: m.editInput,
			}
			m.mode = modeNormal
			m.editInput = ""
			return m, publishRecord(m.kafkaAddr, m.digestTopic, rec)
		}
		m.mode = modeNormal
		m.editInput = ""

	case tea.KeyEscape:
		m.mode = modeNormal
		m.editInput = ""

	case tea.KeyBackspace:
		if len(m.editInput) > 0 {
			// Safe rune-aware trim.
			runes := []rune(m.editInput)
			m.editInput = string(runes[:len(runes)-1])
		}

	default:
		// Append printable typed text.
		if msg.Text != "" {
			m.editInput += msg.Text
		}
	}

	return m, nil
}

// handleFuncPick processes key events while the function picker popup is open.
func (m Model) handleFuncPick(k tea.Key) (tea.Model, tea.Cmd) {
	switch k.Code {
	case tea.KeyUp:
		if m.funcIdx > 0 {
			m.funcIdx--
		}
	case tea.KeyDown:
		if m.funcIdx < len(m.funcOptions)-1 {
			m.funcIdx++
		}

	case tea.KeyEnter:
		if m.selectedIdx < len(m.tileMsgs) && m.funcIdx < len(m.funcOptions) {
			name := m.tileMsgs[m.selectedIdx].Name
			fn := m.funcOptions[m.funcIdx]
			rec := tiles.OverrideRecord{
				Type: "func",
				Tile: name,
				Func: fn,
			}
			m.mode = modeNormal
			return m, publishRecord(m.kafkaAddr, m.digestTopic, rec)
		}
		m.mode = modeNormal

	case tea.KeyEscape:
		m.mode = modeNormal
	}

	return m, nil
}

// nextMsg returns a tea.Cmd that reads one message from the digest topic and
// returns a MsgTileSnapshot or MsgKafkaError. A fresh reader is created per
// call; the reader closes itself after reading one message.
func nextMsg(kafkaAddr, topic, groupID string) tea.Cmd {
	return func() tea.Msg {
		r := kafka.NewReader(kafka.ReaderConfig{
			Brokers:  []string{kafkaAddr},
			Topic:    topic,
			GroupID:  groupID,
			MaxBytes: 1 << 20, // 1 MiB
		})
		defer r.Close() //nolint:errcheck

		rawMsg, err := r.ReadMessage(context.Background())
		if err != nil {
			return tuitypes.MsgKafkaError{Err: err}
		}

		var snap tiles.TileSnapshot
		if err := json.Unmarshal(rawMsg.Value, &snap); err != nil {
			// Likely an OverrideRecord or unrecognised payload — skip and retry.
			return nextMsg(kafkaAddr, topic, groupID)()
		}

		// Only treat as a snapshot if it actually carries tile data.
		if len(snap.Tiles) == 0 {
			return nextMsg(kafkaAddr, topic, groupID)()
		}

		return MsgTileSnapshot{Snap: snap}
	}
}

// publishRecord returns a tea.Cmd that JSON-encodes rec and writes it to the
// digest Kafka topic as an OverrideRecord, then returns MsgPublishDone.
func publishRecord(kafkaAddr, topic string, rec tiles.OverrideRecord) tea.Cmd {
	return func() tea.Msg {
		b, err := json.Marshal(rec)
		if err != nil {
			return MsgPublishDone{Err: fmt.Errorf("marshal: %w", err)}
		}

		w := &kafka.Writer{
			Addr:     kafka.TCP(kafkaAddr),
			Topic:    topic,
			Balancer: &kafka.LeastBytes{},
		}
		defer w.Close() //nolint:errcheck

		err = w.WriteMessages(context.Background(), kafka.Message{Value: b})
		return MsgPublishDone{Err: err}
	}
}
