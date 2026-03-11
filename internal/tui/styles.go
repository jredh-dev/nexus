// SPDX-License-Identifier: AGPL-3.0-or-later
// Package tui provides shared styles, helpers, and types for nexus TUI commands.
package tui

import "charm.land/lipgloss/v2"

// Color palette — shared across all TUI commands.
var (
	TitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00FF88"))
	DimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))
	ErrStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444")).Bold(true)
	WarnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00"))
	ValueStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00"))
	AccentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF88")).Bold(true)
	InfoStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF88"))
	SourceStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#8888FF"))

	// BoxStyle: rounded border in dim purple. Chain .Width(n) before rendering.
	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#444466")).
			Padding(0, 1)

	// SelectedBoxStyle: same but green border for focused tile.
	SelectedBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#00FF88")).
				Padding(0, 1)
)
