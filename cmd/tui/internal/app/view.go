// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	pb "github.com/jredh-dev/nexus/cmd/tui/proto"
	tuitypes "github.com/jredh-dev/nexus/internal/tui"
)

// --- App-specific styles (not shared) ---

var (
	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#00FF88"))

	promptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF88")).
			Bold(true)
)

// View renders the full-screen TUI.
func (m Model) View() tea.View {
	if m.Width == 0 {
		return tuitypes.AltView("loading...")
	}

	var s string
	switch m.state {
	case stateLogin:
		s = m.viewLogin()
	case stateConnecting:
		s = m.viewConnecting()
	case stateDashboard:
		s = m.viewDashboard()
	case stateBenchmark:
		s = m.viewBenchmark()
	case stateDB:
		s = m.viewDBConsole()
	case stateSecrets:
		s = m.viewSecrets()
	case stateError:
		s = m.viewError()
	}

	return tuitypes.AltView(s)
}

// --- Full-screen views ---

func (m Model) viewLogin() string {
	var b strings.Builder
	b.WriteString(tuitypes.TitleStyle.Render("  FOOL"))
	b.WriteString("\n\n")
	b.WriteString("  Connect to: ")
	b.WriteString(tuitypes.DimStyle.Render(m.addr))
	b.WriteString("\n\n")
	b.WriteString("  Username: ")
	b.WriteString(m.username)
	b.WriteString("█")
	b.WriteString("\n\n")
	b.WriteString(tuitypes.DimStyle.Render("  [enter] login  [esc] quit"))
	return b.String()
}

func (m Model) viewConnecting() string {
	return tuitypes.TitleStyle.Render("  FOOL") + "\n\n  Connecting to " + m.addr + "..."
}

func (m Model) viewError() string {
	var b strings.Builder
	b.WriteString(tuitypes.TitleStyle.Render("  FOOL"))
	b.WriteString("\n\n")
	if m.err != nil {
		b.WriteString(tuitypes.ErrStyle.Render("  ERROR: " + m.err.Error()))
	}
	b.WriteString("\n\n")
	b.WriteString(tuitypes.DimStyle.Render("  [q/esc] quit"))
	return b.String()
}

func (m Model) viewDashboard() string {
	return m.splitView(m.renderInfoPanel, m.renderControlPanel)
}

func (m Model) viewBenchmark() string {
	return m.splitView(m.renderBenchPanel, m.renderControlPanel)
}

func (m Model) viewDBConsole() string {
	return m.splitView(m.renderDBStatsPanel, m.renderDBInputPanel)
}

func (m Model) viewSecrets() string {
	return m.splitView(m.renderSecretsListPanel, m.renderSecretsInputPanel)
}

// splitView splits the terminal into two bordered panels stacked vertically.
// topFn and botFn receive the inner width available to their panel.
func (m Model) splitView(
	topFn func(innerW, maxLines int) string,
	botFn func(innerW, maxLines int) string,
) string {
	// Border overhead: 2 vertical borders (top+bottom) + 2 padding lines each side
	borderH := 2
	topHeight := m.Height/2 - borderH
	if topHeight < 4 {
		topHeight = 4
	}
	botHeight := m.Height - (topHeight + borderH*2) - borderH
	if botHeight < 3 {
		botHeight = 3
	}

	// Inner width: border (1 each side) + padding (1 each side) = 4 chars
	innerW := m.Width - 4
	if innerW < 20 {
		innerW = 20
	}

	topContent := topFn(innerW, topHeight)
	botContent := botFn(innerW, botHeight)

	topBox := tuitypes.BoxStyle.Width(innerW).Render(topContent)
	botBox := tuitypes.BoxStyle.Width(innerW).Render(botContent)

	return topBox + "\n" + botBox
}

// --- Panel renderers (signature: innerW, maxLines int) string ---

func (m Model) renderInfoPanel(innerW, maxLines int) string {
	var b strings.Builder
	b.WriteString(tuitypes.TitleStyle.Render("Server Information"))
	b.WriteString("\n")

	if m.serverInfo == nil {
		b.WriteString(tuitypes.DimStyle.Render("No server info yet. Select 'Hermit DB' or 'Benchmark' to connect."))
		if m.err != nil {
			b.WriteString("\n")
			b.WriteString(tuitypes.ErrStyle.Render(m.err.Error()))
		}
		return b.String()
	}

	si := m.serverInfo
	lines := serverInfoLines(si)
	for i, line := range lines {
		if i >= maxLines-2 {
			break
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	if len(m.viewHistory) > 0 {
		b.WriteString("\n")
		b.WriteString(tuitypes.DimStyle.Render("Log:"))
		b.WriteString("\n")
		start := 0
		if len(m.viewHistory) > 3 {
			start = len(m.viewHistory) - 3
		}
		for _, h := range m.viewHistory[start:] {
			b.WriteString(tuitypes.DimStyle.Render("  " + h))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func serverInfoLines(si *pb.ServerInfoResponse) []string {
	return []string{
		fmt.Sprintf("Version:   %s", tuitypes.ValueStyle.Render(si.Version)),
		fmt.Sprintf("Region:    %s", tuitypes.ValueStyle.Render(si.Region)),
		fmt.Sprintf("Uptime:    %s", tuitypes.ValueStyle.Render(fmt.Sprintf("%ds", si.UptimeSeconds))),
		fmt.Sprintf("TLS:       %s", tuitypes.ValueStyle.Render(fmt.Sprintf("%v", si.TlsEnabled))),
		fmt.Sprintf("gRPC Port: %s", tuitypes.ValueStyle.Render(fmt.Sprintf("%d", si.GrpcPort))),
	}
}

func (m Model) renderControlPanel(innerW, _ int) string {
	var b strings.Builder
	b.WriteString(tuitypes.TitleStyle.Render("Menu"))
	b.WriteString("\n\n")

	for i, item := range m.menuItems {
		if i == m.menuIdx {
			b.WriteString(selectedStyle.Render(" ▸ " + item + " "))
		} else {
			b.WriteString("   " + item)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(tuitypes.DimStyle.Render("[↑/↓ or k/j] navigate  [enter] select  [q] quit"))
	return b.String()
}

func (m Model) renderBenchPanel(innerW, _ int) string {
	var b strings.Builder
	b.WriteString(tuitypes.TitleStyle.Render("Benchmark Results"))
	b.WriteString("\n")

	if m.benchRunning {
		b.WriteString("\n  Running benchmarks...")
		return b.String()
	}

	if m.grpcBench != nil {
		b.WriteString("\n")
		b.WriteString(tuitypes.TitleStyle.Render("gRPC (TLS 1.3)"))
		b.WriteString("\n")
		gb := m.grpcBench
		b.WriteString(fmt.Sprintf("  min: %s  p50: %s  p99: %s  max: %s\n",
			tuitypes.ValueStyle.Render(tuitypes.FmtNs(gb.MinNs)),
			tuitypes.ValueStyle.Render(tuitypes.FmtNs(gb.P50Ns)),
			tuitypes.ValueStyle.Render(tuitypes.FmtNs(gb.P99Ns)),
			tuitypes.ValueStyle.Render(tuitypes.FmtNs(gb.MaxNs)),
		))
		b.WriteString(fmt.Sprintf("  mean: %s  overhead: %s  tls: %s\n",
			tuitypes.ValueStyle.Render(tuitypes.FmtNs(gb.MeanNs)),
			tuitypes.ValueStyle.Render(tuitypes.FmtNs(gb.ProcessingOverheadNs)),
			tuitypes.ValueStyle.Render(gb.TlsVersion),
		))
	}

	return b.String()
}

func (m Model) renderDBStatsPanel(innerW, maxLines int) string {
	var b strings.Builder
	b.WriteString(tuitypes.TitleStyle.Render("In-Memory Database — Stats"))
	b.WriteString("\n")

	b.WriteString("\n")
	b.WriteString(tuitypes.TitleStyle.Render("Document Store") + tuitypes.DimStyle.Render(" (zstd KVP, fast reads)"))
	b.WriteString("\n")
	if m.dbStats != nil {
		b.WriteString(fmt.Sprintf("  keys: %s   compressed: %s\n",
			tuitypes.ValueStyle.Render(fmt.Sprintf("%d", m.dbStats.DocKeyCount)),
			tuitypes.ValueStyle.Render(tuitypes.FmtBytes(m.dbStats.DocCompressedBytes)),
		))
	} else {
		b.WriteString(tuitypes.DimStyle.Render("  loading...\n"))
	}

	b.WriteString("\n")
	b.WriteString(tuitypes.TitleStyle.Render("Relational Store") + tuitypes.DimStyle.Render(" (MPSC queue, eventual reads)"))
	b.WriteString("\n")
	if m.dbStats != nil {
		b.WriteString(fmt.Sprintf("  committed rows: %s   pending writes: %s\n",
			tuitypes.ValueStyle.Render(fmt.Sprintf("%d", m.dbStats.RelRowCount)),
			tuitypes.ValueStyle.Render(fmt.Sprintf("%d", m.dbStats.RelPendingWrites)),
		))
	} else {
		b.WriteString(tuitypes.DimStyle.Render("  loading...\n"))
	}

	if len(m.dbHistory) > 0 {
		b.WriteString("\n")
		b.WriteString(tuitypes.DimStyle.Render("Recent:"))
		b.WriteString("\n")
		linesUsed := 7
		avail := maxLines - linesUsed - 2
		if avail < 1 {
			avail = 1
		}
		start := 0
		if len(m.dbHistory) > avail {
			start = len(m.dbHistory) - avail
		}
		for _, h := range m.dbHistory[start:] {
			style := tuitypes.DimStyle
			if h.isErr {
				style = tuitypes.ErrStyle
			}
			line := fmt.Sprintf("[%s] %s → %s", h.ts, h.cmd, h.output)
			if len(line) > innerW-2 {
				line = line[:innerW-5] + "..."
			}
			b.WriteString(style.Render("  " + line))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m Model) renderDBInputPanel(innerW, _ int) string {
	var b strings.Builder
	b.WriteString(tuitypes.TitleStyle.Render("DB Console"))
	b.WriteString("\n\n")

	b.WriteString(promptStyle.Render("> "))
	b.WriteString(m.dbInput)
	b.WriteString("█")
	b.WriteString("\n\n")

	b.WriteString(tuitypes.DimStyle.Render("kv:set <k> <v>  kv:get <k>  kv:list"))
	b.WriteString("\n")
	b.WriteString(tuitypes.DimStyle.Render("sql:insert <k> <v>  sql:query [k]  stats  help"))
	b.WriteString("\n")
	b.WriteString(tuitypes.DimStyle.Render("[enter] execute  [esc] back"))
	return b.String()
}

func (m Model) renderSecretsListPanel(innerW, maxLines int) string {
	var b strings.Builder
	stats := m.secretsStats
	b.WriteString(tuitypes.TitleStyle.Render("Secrets") +
		tuitypes.DimStyle.Render(fmt.Sprintf("  total:%d  secrets:%d  exposed:%d  lenses:%d",
			stats.Total, stats.Secrets, stats.NotSecrets, stats.Lenses)))
	b.WriteString("\n\n")

	if len(m.secretsList) == 0 {
		b.WriteString(tuitypes.DimStyle.Render("No secrets yet."))
	} else {
		// Show most recent first, capped to fit maxLines
		avail := maxLines - 4
		if avail < 1 {
			avail = 1
		}
		list := m.secretsList
		if len(list) > avail {
			list = list[len(list)-avail:]
		}
		for _, s := range list {
			stateColor := lipgloss.Color("#00FF88")
			stateLabel := "secret"
			if !s.IsSecret() {
				stateColor = lipgloss.Color("#FF4444")
				stateLabel = "exposed"
			}
			stateTag := lipgloss.NewStyle().Foreground(stateColor).Render(stateLabel)
			countInfo := tuitypes.DimStyle.Render(fmt.Sprintf("x%d", s.Count))
			line := fmt.Sprintf("[%s] %s  %s  %s",
				stateTag,
				tuitypes.ValueStyle.Render(s.Value),
				countInfo,
				tuitypes.DimStyle.Render("by "+s.SubmittedBy),
			)
			if len(line) > innerW {
				line = line[:innerW-3] + "..."
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	if len(m.secretsLog) > 0 {
		b.WriteString("\n")
		b.WriteString(tuitypes.DimStyle.Render("Log:"))
		b.WriteString("\n")
		start := 0
		if len(m.secretsLog) > 3 {
			start = len(m.secretsLog) - 3
		}
		for _, e := range m.secretsLog[start:] {
			style := tuitypes.DimStyle
			if e.isErr {
				style = tuitypes.ErrStyle
			}
			b.WriteString(style.Render(fmt.Sprintf("  [%s] %s", e.ts, e.text)))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m Model) renderSecretsInputPanel(innerW, _ int) string {
	var b strings.Builder
	b.WriteString(tuitypes.TitleStyle.Render("Submit a Secret"))
	b.WriteString("\n\n")

	b.WriteString(promptStyle.Render("> "))
	b.WriteString(m.secretsInput)
	b.WriteString("█")
	b.WriteString("\n\n")

	b.WriteString(tuitypes.DimStyle.Render("[enter] submit  [enter on empty] refresh  [esc] back"))
	return b.String()
}
