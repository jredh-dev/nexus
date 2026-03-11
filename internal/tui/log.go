// SPDX-License-Identifier: AGPL-3.0-or-later
package tui

import (
	"strings"
	"time"
)

// LogEntry is a timestamped log line with optional error flag.
type LogEntry struct {
	TS    string // formatted "15:04:05"
	Text  string
	IsErr bool
}

// NewEntry creates a LogEntry stamped at now.
func NewEntry(text string, isErr bool) LogEntry {
	return LogEntry{TS: time.Now().Format("15:04:05"), Text: text, IsErr: isErr}
}

// Trim returns the last maxLen entries of log.
func Trim(log []LogEntry, maxLen int) []LogEntry {
	if len(log) <= maxLen {
		return log
	}
	return log[len(log)-maxLen:]
}

// RenderLog renders the last n entries using ErrStyle for errors, DimStyle otherwise.
func RenderLog(log []LogEntry, n int) string {
	entries := Trim(log, n)
	lines := make([]string, len(entries))
	for i, e := range entries {
		ts := DimStyle.Render(e.TS + " ")
		var text string
		if e.IsErr {
			text = ErrStyle.Render(e.Text)
		} else {
			text = e.Text
		}
		lines[i] = ts + text
	}
	return strings.Join(lines, "\n")
}
