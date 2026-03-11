// SPDX-License-Identifier: AGPL-3.0-or-later
package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	tuitypes "github.com/jredh-dev/nexus/internal/tui"
)

// App-specific styles not in the shared palette.
var (
	liveStyle  = tuitypes.InfoStyle
	pauseStyle = tuitypes.ErrStyle
)

// View implements tea.Model. Renders the full TUI.
func (m Model) View() tea.View {
	var sb strings.Builder

	// Title bar.
	sb.WriteString(tuitypes.TitleStyle.Render(" realtime "))
	sb.WriteString("\n")

	// How many log lines to show — leave 4 rows for title + status bar (+
	// optional error line + blank separator).
	visibleLines := m.Height - 4
	if visibleLines < 0 {
		visibleLines = 0
	}

	// Take the tail of the buffer.
	lines := m.lines
	if len(lines) > visibleLines {
		lines = lines[len(lines)-visibleLines:]
	}

	// Render each log line.
	for _, l := range lines {
		ts := tuitypes.DimStyle.Render(l.Timestamp.Format("15:04:05"))

		// Level with color.
		var levelStr string
		switch l.Level {
		case "WARN":
			levelStr = tuitypes.WarnStyle.Render(fmt.Sprintf("%-5s", l.Level))
		case "ERROR":
			levelStr = tuitypes.ErrStyle.Render(fmt.Sprintf("%-5s", l.Level))
		default: // INFO and anything else
			levelStr = tuitypes.InfoStyle.Render(fmt.Sprintf("%-5s", l.Level))
		}

		src := tuitypes.SourceStyle.Render(fmt.Sprintf("%-12s", l.Source))
		// Message in plain white (terminal default).
		sb.WriteString(fmt.Sprintf("%s %s %s %s\n", ts, levelStr, src, l.Message))
	}

	// Pad empty rows if buffer is smaller than the viewport.
	for i := len(lines); i < visibleLines; i++ {
		sb.WriteString("\n")
	}

	// Status bar.
	count := fmt.Sprintf("%d lines", len(m.lines))
	var stateStr string
	if m.paused {
		stateStr = pauseStyle.Render("PAUSED")
	} else {
		stateStr = liveStyle.Render("LIVE")
	}
	help := tuitypes.DimStyle.Render("  p:pause  q:quit")
	sb.WriteString(fmt.Sprintf("%s │ %s%s\n", count, stateStr, help))

	// Optional error line.
	if m.err != nil {
		sb.WriteString(tuitypes.RenderError(m.err))
		sb.WriteString("\n")
	}

	return tuitypes.AltView(sb.String())
}
