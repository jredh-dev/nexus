// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	pb "github.com/jredh-dev/nexus/cmd/fool/proto"
)

// --- Styles ---

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FF88"))

	selectedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#00FF88"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))

	errStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4444")).
			Bold(true)

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFAA00"))

	promptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF88")).
			Bold(true)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#444466")).
			Padding(0, 1)
)

// View renders the full-screen TUI.
func (m Model) View() tea.View {
	if m.width == 0 {
		v := tea.NewView("loading...")
		v.AltScreen = true
		return v
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

	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

// --- Full-screen views ---

func (m Model) viewLogin() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("  FOOL"))
	b.WriteString("\n\n")
	b.WriteString("  Connect to: ")
	b.WriteString(dimStyle.Render(m.addr))
	b.WriteString("\n\n")
	b.WriteString("  Username: ")
	b.WriteString(m.username)
	b.WriteString("█")
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  [enter] login  [esc] quit"))
	return b.String()
}

func (m Model) viewConnecting() string {
	return titleStyle.Render("  FOOL") + "\n\n  Connecting to " + m.addr + "..."
}

func (m Model) viewError() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("  FOOL"))
	b.WriteString("\n\n")
	if m.err != nil {
		b.WriteString(errStyle.Render("  ERROR: " + m.err.Error()))
	}
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("  [q/esc] quit"))
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
	topHeight := m.height/2 - borderH
	if topHeight < 4 {
		topHeight = 4
	}
	botHeight := m.height - (topHeight + borderH*2) - borderH
	if botHeight < 3 {
		botHeight = 3
	}

	// Inner width: border (1 each side) + padding (1 each side) = 4 chars
	innerW := m.width - 4
	if innerW < 20 {
		innerW = 20
	}

	topContent := topFn(innerW, topHeight)
	botContent := botFn(innerW, botHeight)

	topBox := panelStyle.Width(innerW).Render(topContent)
	botBox := panelStyle.Width(innerW).Render(botContent)

	return topBox + "\n" + botBox
}

// --- Panel renderers (signature: innerW, maxLines int) string ---

func (m Model) renderInfoPanel(innerW, maxLines int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Server Information"))
	b.WriteString("\n")

	if m.serverInfo == nil {
		b.WriteString(dimStyle.Render("No server info yet. Select 'Hermit DB' or 'Benchmark' to connect."))
		if m.err != nil {
			b.WriteString("\n")
			b.WriteString(errStyle.Render(m.err.Error()))
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
		b.WriteString(dimStyle.Render("Log:"))
		b.WriteString("\n")
		start := 0
		if len(m.viewHistory) > 3 {
			start = len(m.viewHistory) - 3
		}
		for _, h := range m.viewHistory[start:] {
			b.WriteString(dimStyle.Render("  " + h))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func serverInfoLines(si *pb.ServerInfoResponse) []string {
	return []string{
		fmt.Sprintf("Version:   %s", valueStyle.Render(si.Version)),
		fmt.Sprintf("Region:    %s", valueStyle.Render(si.Region)),
		fmt.Sprintf("Uptime:    %s", valueStyle.Render(fmt.Sprintf("%ds", si.UptimeSeconds))),
		fmt.Sprintf("TLS:       %s", valueStyle.Render(fmt.Sprintf("%v", si.TlsEnabled))),
		fmt.Sprintf("gRPC Port: %s", valueStyle.Render(fmt.Sprintf("%d", si.GrpcPort))),
		fmt.Sprintf("TCP Port:  %s", valueStyle.Render(fmt.Sprintf("%d", si.TcpPort))),
	}
}

func (m Model) renderControlPanel(innerW, _ int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Menu"))
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
	b.WriteString(dimStyle.Render("[↑/↓ or k/j] navigate  [enter] select  [q] quit"))
	return b.String()
}

func (m Model) renderBenchPanel(innerW, _ int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Benchmark Results"))
	b.WriteString("\n")

	if m.benchRunning {
		b.WriteString("\n  Running benchmarks...")
		return b.String()
	}

	if m.grpcBench != nil {
		b.WriteString("\n")
		b.WriteString(titleStyle.Render("gRPC (TLS 1.3)"))
		b.WriteString("\n")
		gb := m.grpcBench
		b.WriteString(fmt.Sprintf("  min: %s  p50: %s  p99: %s  max: %s\n",
			valueStyle.Render(fmtNs(gb.MinNs)),
			valueStyle.Render(fmtNs(gb.P50Ns)),
			valueStyle.Render(fmtNs(gb.P99Ns)),
			valueStyle.Render(fmtNs(gb.MaxNs)),
		))
		b.WriteString(fmt.Sprintf("  mean: %s  overhead: %s  tls: %s\n",
			valueStyle.Render(fmtNs(gb.MeanNs)),
			valueStyle.Render(fmtNs(gb.ProcessingOverheadNs)),
			valueStyle.Render(gb.TlsVersion),
		))
	}

	for _, tcp := range []struct {
		label string
		r     *tcpBenchResultMsg
	}{
		{"TCP (Plaintext)", m.tcpPlain},
		{"TCP (TLS 1.3)", m.tcpTLS},
	} {
		if tcp.r == nil {
			continue
		}
		b.WriteString("\n")
		b.WriteString(titleStyle.Render(tcp.label))
		b.WriteString("\n")
		if tcp.r.err != nil {
			b.WriteString(errStyle.Render("  " + tcp.r.err.Error()))
		} else {
			b.WriteString(fmt.Sprintf("  RTT: %s\n", valueStyle.Render(fmtNs(tcp.r.rttNs))))
		}
	}

	return b.String()
}

func (m Model) renderDBStatsPanel(innerW, maxLines int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("In-Memory Database — Stats"))
	b.WriteString("\n")

	b.WriteString("\n")
	b.WriteString(titleStyle.Render("Document Store") + dimStyle.Render(" (zstd KVP, fast reads)"))
	b.WriteString("\n")
	if m.dbStats != nil {
		b.WriteString(fmt.Sprintf("  keys: %s   compressed: %s\n",
			valueStyle.Render(fmt.Sprintf("%d", m.dbStats.DocKeyCount)),
			valueStyle.Render(fmtBytes(m.dbStats.DocCompressedBytes)),
		))
	} else {
		b.WriteString(dimStyle.Render("  loading...\n"))
	}

	b.WriteString("\n")
	b.WriteString(titleStyle.Render("Relational Store") + dimStyle.Render(" (MPSC queue, eventual reads)"))
	b.WriteString("\n")
	if m.dbStats != nil {
		b.WriteString(fmt.Sprintf("  committed rows: %s   pending writes: %s\n",
			valueStyle.Render(fmt.Sprintf("%d", m.dbStats.RelRowCount)),
			valueStyle.Render(fmt.Sprintf("%d", m.dbStats.RelPendingWrites)),
		))
	} else {
		b.WriteString(dimStyle.Render("  loading...\n"))
	}

	if len(m.dbHistory) > 0 {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("Recent:"))
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
			style := dimStyle
			if h.isErr {
				style = errStyle
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
	b.WriteString(titleStyle.Render("DB Console"))
	b.WriteString("\n\n")

	b.WriteString(promptStyle.Render("> "))
	b.WriteString(m.dbInput)
	b.WriteString("█")
	b.WriteString("\n\n")

	b.WriteString(dimStyle.Render("kv:set <k> <v>  kv:get <k>  kv:list"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("sql:insert <k> <v>  sql:query [k]  stats  help"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("[enter] execute  [esc] back"))
	return b.String()
}

func (m Model) renderSecretsListPanel(innerW, maxLines int) string {
	var b strings.Builder
	stats := m.secretsStats
	b.WriteString(titleStyle.Render("Secrets") +
		dimStyle.Render(fmt.Sprintf("  total:%d  truths:%d  lies:%d  lenses:%d",
			stats.Total, stats.Truths, stats.Lies, stats.Lenses)))
	b.WriteString("\n\n")

	if len(m.secretsList) == 0 {
		b.WriteString(dimStyle.Render("No secrets yet."))
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
			if s.State == "lie" {
				stateColor = lipgloss.Color("#FF4444")
			}
			stateTag := lipgloss.NewStyle().Foreground(stateColor).Render(s.State)
			line := fmt.Sprintf("[%s] %s  %s",
				stateTag,
				valueStyle.Render(s.Value),
				dimStyle.Render("by "+s.SubmittedBy),
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
		b.WriteString(dimStyle.Render("Log:"))
		b.WriteString("\n")
		start := 0
		if len(m.secretsLog) > 3 {
			start = len(m.secretsLog) - 3
		}
		for _, e := range m.secretsLog[start:] {
			style := dimStyle
			if e.isErr {
				style = errStyle
			}
			b.WriteString(style.Render(fmt.Sprintf("  [%s] %s", e.ts, e.text)))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m Model) renderSecretsInputPanel(innerW, _ int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Submit a Secret"))
	b.WriteString("\n\n")

	b.WriteString(promptStyle.Render("> "))
	b.WriteString(m.secretsInput)
	b.WriteString("█")
	b.WriteString("\n\n")

	b.WriteString(dimStyle.Render("[enter] submit  [enter on empty] refresh  [esc] back"))
	return b.String()
}

// --- Formatting helpers ---

func fmtNs(ns int64) string {
	switch {
	case ns >= 1_000_000_000:
		return fmt.Sprintf("%.2fs", float64(ns)/1e9)
	case ns >= 1_000_000:
		return fmt.Sprintf("%.2fms", float64(ns)/1e6)
	case ns >= 1_000:
		return fmt.Sprintf("%.2fμs", float64(ns)/1e3)
	default:
		return fmt.Sprintf("%dns", ns)
	}
}

func fmtBytes(b uint64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}
