// SPDX-License-Identifier: AGPL-3.0-or-later
// Package app implements the Bubbletea model, update, and view for the digest
// TUI.  The TUI streams TileSnapshot messages from the digest Kafka topic and
// renders them as a 2-column tile grid with inline edit support.
package app

import (
	"github.com/jredh-dev/nexus/internal/digest/tiles"
	tuitypes "github.com/jredh-dev/nexus/internal/tui"
)

// uiMode describes which interactive overlay (if any) is currently active.
type uiMode int

const (
	modeNormal   uiMode = iota // standard navigation
	modeEdit                   // inline text input for override
	modeFuncPick               // popup: choose reducer function
)

// Model is the root Bubbletea model for the digest TUI.
type Model struct {
	tuitypes.WindowSize // embeds Width, Height

	// Tile state — updated on each MsgTileSnapshot.
	tileMsgs []tiles.TileValue

	// Navigation
	selectedIdx int // index into tileMsgs
	mode        uiMode

	// Edit mode
	editInput string // text being typed

	// Func-pick popup
	funcOptions []string // available reducer names
	funcIdx     int      // selected index in funcOptions

	// Kafka config (injected via New)
	kafkaAddr   string
	digestTopic string
	groupID     string

	// Status line for transient messages (errors, confirmations)
	statusMsg string

	// Error state
	err error
}

// New creates a fresh Model ready for use as a Bubbletea model.
func New(kafkaAddr, digestTopic, groupID string) Model {
	return Model{
		kafkaAddr:   kafkaAddr,
		digestTopic: digestTopic,
		groupID:     groupID,
		funcOptions: []string{"avg", "median", "count", "rate", "last"},
	}
}
