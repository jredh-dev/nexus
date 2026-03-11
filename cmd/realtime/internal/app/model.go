// SPDX-License-Identifier: AGPL-3.0-or-later
package app

import (
	tea "charm.land/bubbletea/v2"
	tuitypes "github.com/jredh-dev/nexus/internal/tui"
	"github.com/segmentio/kafka-go"
)

// Model is the bubbletea v2 model for the realtime TUI.
type Model struct {
	tuitypes.WindowSize // embeds Width, Height

	lines    []MsgNewLine // scrolling log buffer
	maxLines int
	paused   bool
	reader   *kafka.Reader
	key      []byte
	err      error
}

// New creates a Model ready to consume from the given Kafka reader.
func New(reader *kafka.Reader, key []byte, maxLines int) Model {
	return Model{
		reader:   reader,
		key:      key,
		maxLines: maxLines,
	}
}

// Init implements tea.Model. Starts the Kafka consumer loop.
func (m Model) Init() tea.Cmd {
	return m.nextMsg()
}
