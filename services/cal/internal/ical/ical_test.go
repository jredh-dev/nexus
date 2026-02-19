package ical

import (
	"strings"
	"testing"
	"time"
)

func TestGenerate_BasicFeed(t *testing.T) {
	feed := Feed{
		Name: "Test Calendar",
		TTL:  1 * time.Hour,
	}

	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	created := time.Date(2026, 2, 18, 12, 0, 0, 0, time.UTC)

	events := []Event{
		{
			UID:         "event-1@nexus-cal",
			Summary:     "Team Meeting",
			Description: "Weekly sync",
			Start:       start,
			End:         &end,
			Status:      "CONFIRMED",
			Created:     created,
			Updated:     created,
		},
	}

	result := Generate(feed, events)

	required := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//jredh-dev//nexus-cal//EN",
		"METHOD:PUBLISH",
		"X-WR-CALNAME:Test Calendar",
		"NAME:Test Calendar",
		"REFRESH-INTERVAL;VALUE=DURATION:PT1H",
		"X-PUBLISHED-TTL:PT1H",
		"BEGIN:VEVENT",
		"UID:event-1@nexus-cal",
		"SUMMARY:Team Meeting",
		"DESCRIPTION:Weekly sync",
		"DTSTART:20260301T090000Z",
		"DTEND:20260301T100000Z",
		"STATUS:CONFIRMED",
		"END:VEVENT",
		"END:VCALENDAR",
	}

	for _, s := range required {
		if !strings.Contains(result, s) {
			t.Errorf("output missing %q", s)
		}
	}
}

func TestGenerate_AllDayEvent(t *testing.T) {
	feed := Feed{Name: "Test"}
	start := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)

	events := []Event{
		{
			UID:     "allday-1@nexus-cal",
			Summary: "Holiday",
			Start:   start,
			AllDay:  true,
			Created: start,
			Updated: start,
		},
	}

	result := Generate(feed, events)

	if !strings.Contains(result, "DTSTART;VALUE=DATE:20260615") {
		t.Error("all-day event should use VALUE=DATE format")
	}
	if strings.Contains(result, "DTSTART:2026") {
		t.Error("all-day event should not use datetime format for DTSTART")
	}
}

func TestGenerate_DeadlineAlarm(t *testing.T) {
	feed := Feed{Name: "Test"}
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	deadline := time.Date(2026, 3, 5, 17, 0, 0, 0, time.UTC)

	events := []Event{
		{
			UID:      "deadline-1@nexus-cal",
			Summary:  "Ship feature",
			Start:    start,
			Deadline: &deadline,
			Created:  start,
			Updated:  start,
		},
	}

	result := Generate(feed, events)

	required := []string{
		"BEGIN:VALARM",
		"TRIGGER:-PT1H",
		"ACTION:DISPLAY",
		"END:VALARM",
	}
	for _, s := range required {
		if !strings.Contains(result, s) {
			t.Errorf("deadline event missing alarm component %q", s)
		}
	}
}

func TestGenerate_EmptyFeed(t *testing.T) {
	feed := Feed{Name: "Empty"}
	result := Generate(feed, nil)

	if !strings.Contains(result, "BEGIN:VCALENDAR") {
		t.Error("empty feed should still produce valid VCALENDAR")
	}
	if strings.Contains(result, "BEGIN:VEVENT") {
		t.Error("empty feed should have no events")
	}
	if !strings.Contains(result, "END:VCALENDAR") {
		t.Error("empty feed should close VCALENDAR")
	}
}

func TestGenerate_CRLFLineEndings(t *testing.T) {
	feed := Feed{Name: "Test"}
	result := Generate(feed, nil)

	lines := strings.Split(result, "\r\n")
	// Last element after split on trailing \r\n will be empty
	if len(lines) < 2 {
		t.Fatal("expected multiple CRLF-terminated lines")
	}
	// Ensure no bare \n without \r
	for _, line := range lines {
		if strings.Contains(line, "\n") {
			t.Errorf("found bare LF in line: %q", line)
		}
	}
}

func TestEscapeText(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "simple"},
		{"semi;colon", `semi\;colon`},
		{"com,ma", `com\,ma`},
		{"new\nline", `new\nline`},
		{`back\slash`, `back\\slash`},
	}
	for _, tt := range tests {
		got := escapeText(tt.input)
		if got != tt.want {
			t.Errorf("escapeText(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{1 * time.Hour, "PT1H"},
		{30 * time.Minute, "PT30M"},
		{90 * time.Minute, "PT1H30M"},
		{24 * time.Hour, "P1D"},
		{48 * time.Hour, "P2D"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
