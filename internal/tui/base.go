// SPDX-License-Identifier: AGPL-3.0-or-later
package tui

import tea "charm.land/bubbletea/v2"

// WindowSize tracks terminal dimensions. Embed in app Model structs.
type WindowSize struct {
	Width  int
	Height int
}

// Handle updates dimensions if msg is a WindowSizeMsg. Returns true if it was.
func (w *WindowSize) Handle(msg tea.Msg) bool {
	if m, ok := msg.(tea.WindowSizeMsg); ok {
		w.Width = m.Width
		w.Height = m.Height
		return true
	}
	return false
}

// AltView wraps content string in a tea.View with AltScreen enabled.
func AltView(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	return v
}
