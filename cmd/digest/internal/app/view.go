// SPDX-License-Identifier: AGPL-3.0-or-later
package app

import (
	"fmt"
	"strings"

	"github.com/jredh-dev/nexus/internal/digest/tiles"
	tuitypes "github.com/jredh-dev/nexus/internal/tui"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ── App-specific styles (not in the shared palette) ───────────────────────────

var (
	// tileStyle / selectedTile / wideTile / selectedWide use BoxStyle/SelectedBoxStyle
	// from the shared package but with fixed widths baked in.
	tileStyle    = tuitypes.BoxStyle.Width(30)
	selectedTile = tuitypes.SelectedBoxStyle.Width(30)
	wideTile     = tuitypes.BoxStyle.Width(64)
	selectedWide = tuitypes.SelectedBoxStyle.Width(64)
	ovrBadge     = tuitypes.WarnStyle.Bold(true)
	valueStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
	nameStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	helpStyle    = tuitypes.DimStyle
	inputStyle   = tuitypes.InfoStyle
	popupStyle   = tuitypes.SelectedBoxStyle.Width(24)
	hlStyle      = tuitypes.AccentStyle
)

// View satisfies tea.Model. Renders the full TUI and returns a tea.View.
// AltScreen is enabled on every frame so the program occupies the full window.
func (m Model) View() tea.View {
	return tuitypes.AltView(m.renderContent())
}

// renderContent builds the string content for the view.
func (m Model) renderContent() string {
	var b strings.Builder

	// Title bar.
	b.WriteString(tuitypes.TitleStyle.Render("digest") + "\n\n")

	// Tiles or waiting message.
	if len(m.tileMsgs) == 0 {
		b.WriteString("  waiting for Kafka data…\n")
	} else {
		b.WriteString(m.renderGrid())
	}

	b.WriteString("\n")

	// Status / error line.
	if m.err != nil {
		b.WriteString(helpStyle.Render(fmt.Sprintf("  kafka error: %v", m.err)) + "\n")
	} else if m.statusMsg != "" {
		b.WriteString(helpStyle.Render("  "+m.statusMsg) + "\n")
	}

	// Mode-specific overlays.
	switch m.mode {
	case modeEdit:
		b.WriteString(m.renderEditPrompt())
	case modeFuncPick:
		b.WriteString(m.renderFuncPicker())
	default:
		b.WriteString(helpStyle.Render("  e:edit  r:reset  f:func  q:quit") + "\n")
	}

	return b.String()
}

// renderGrid renders tiles 0–3 in a 2-column grid, tile 4 full-width below.
func (m Model) renderGrid() string {
	var b strings.Builder

	n := len(m.tileMsgs)
	gridEnd := minInt(4, n)

	// 2-column grid for tiles 0..3.
	for i := 0; i < gridEnd; i += 2 {
		left := m.renderTileAt(i)
		if i+1 < gridEnd {
			right := m.renderTileAt(i + 1)
			b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right))
		} else {
			b.WriteString(left)
		}
		b.WriteString("\n")
	}

	// Tile 4 (last_error) full-width.
	if n > 4 {
		b.WriteString(m.renderTileWide(4))
		b.WriteString("\n")
	}

	return b.String()
}

// renderTileAt renders a narrow (width-30) tile at index idx.
func (m Model) renderTileAt(idx int) string {
	tv := m.tileMsgs[idx]
	content := tileContent(tv)
	if idx == m.selectedIdx {
		return selectedTile.Render(content)
	}
	return tileStyle.Render(content)
}

// renderTileWide renders the full-width (width-64) tile at index idx.
func (m Model) renderTileWide(idx int) string {
	tv := m.tileMsgs[idx]
	content := tileContent(tv)
	if idx == m.selectedIdx {
		return selectedWide.Render(content)
	}
	return wideTile.Render(content)
}

// tileContent builds the two-line content string for a tile cell.
func tileContent(tv tiles.TileValue) string {
	nameLine := nameStyle.Render(tv.Name)
	if tv.Overridden {
		nameLine += " " + ovrBadge.Render("[OVR]")
	}
	valLine := valueStyle.Render(fmt.Sprintf("%v", tv.Value))
	return nameLine + "\n" + valLine
}

// renderEditPrompt renders the inline edit bar at the bottom.
func (m Model) renderEditPrompt() string {
	var tileName string
	if m.selectedIdx < len(m.tileMsgs) {
		tileName = m.tileMsgs[m.selectedIdx].Name
	}
	return inputStyle.Render(fmt.Sprintf("  > edit %s: %s_", tileName, m.editInput)) + "\n"
}

// renderFuncPicker renders the function-picker popup.
func (m Model) renderFuncPicker() string {
	var items strings.Builder
	for i, fn := range m.funcOptions {
		if i == m.funcIdx {
			items.WriteString(hlStyle.Render("> "+fn) + "\n")
		} else {
			items.WriteString("  " + fn + "\n")
		}
	}
	items.WriteString("\n" + helpStyle.Render("↑↓:move  enter:apply  esc:cancel"))

	var sb strings.Builder
	sb.WriteString("  select function:\n")
	sb.WriteString(popupStyle.Render(items.String()))
	sb.WriteString("\n")
	return sb.String()
}

// minInt returns the smaller of two ints.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
