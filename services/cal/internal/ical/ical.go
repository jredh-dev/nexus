// Package ical generates RFC 5545 iCalendar output from calendar events.
package ical

import (
	"fmt"
	"strings"
	"time"
)

// Event holds the data needed to render a VEVENT component.
type Event struct {
	UID         string
	Summary     string
	Description string
	Location    string
	URL         string
	Start       time.Time
	End         *time.Time
	AllDay      bool
	Deadline    *time.Time
	Status      string // TENTATIVE, CONFIRMED, CANCELLED
	Categories  string // comma-separated
	Created     time.Time
	Updated     time.Time
}

// Feed holds metadata for the VCALENDAR wrapper.
type Feed struct {
	Name        string
	Description string
	TTL         time.Duration // suggested refresh interval
}

// Generate produces a complete iCalendar document from a feed and its events.
func Generate(feed Feed, events []Event) string {
	var b strings.Builder

	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//jredh-dev//nexus-cal//EN\r\n")
	b.WriteString("METHOD:PUBLISH\r\n")
	b.WriteString("CALSCALE:GREGORIAN\r\n")

	writeProp(&b, "NAME", feed.Name)
	writeProp(&b, "X-WR-CALNAME", feed.Name)
	if feed.Description != "" {
		writeProp(&b, "DESCRIPTION", feed.Description)
		writeProp(&b, "X-WR-CALDESC", feed.Description)
	}

	if feed.TTL > 0 {
		dur := formatDuration(feed.TTL)
		writeProp(&b, "REFRESH-INTERVAL;VALUE=DURATION", dur)
		writeProp(&b, "X-PUBLISHED-TTL", dur)
	}

	for _, e := range events {
		writeEvent(&b, e)
	}

	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

func writeEvent(b *strings.Builder, e Event) {
	b.WriteString("BEGIN:VEVENT\r\n")
	writeProp(b, "UID", e.UID)
	writeProp(b, "DTSTAMP", formatDateTime(e.Updated))

	if e.AllDay {
		writeProp(b, "DTSTART;VALUE=DATE", formatDate(e.Start))
		if e.End != nil {
			writeProp(b, "DTEND;VALUE=DATE", formatDate(*e.End))
		}
	} else {
		writeProp(b, "DTSTART", formatDateTime(e.Start))
		if e.End != nil {
			writeProp(b, "DTEND", formatDateTime(*e.End))
		}
	}

	writeProp(b, "SUMMARY", escapeText(e.Summary))

	if e.Description != "" {
		writeProp(b, "DESCRIPTION", escapeText(e.Description))
	}
	if e.Location != "" {
		writeProp(b, "LOCATION", escapeText(e.Location))
	}
	if e.URL != "" {
		writeProp(b, "URL", e.URL)
	}
	if e.Status != "" {
		writeProp(b, "STATUS", e.Status)
	}
	if e.Categories != "" {
		writeProp(b, "CATEGORIES", e.Categories)
	}

	writeProp(b, "CREATED", formatDateTime(e.Created))
	writeProp(b, "LAST-MODIFIED", formatDateTime(e.Updated))

	// If there's a deadline, add an alarm 1 hour before.
	if e.Deadline != nil {
		b.WriteString("BEGIN:VALARM\r\n")
		writeProp(b, "TRIGGER", "-PT1H")
		writeProp(b, "ACTION", "DISPLAY")
		writeProp(b, "DESCRIPTION", "Deadline approaching: "+escapeText(e.Summary))
		b.WriteString("END:VALARM\r\n")
	}

	b.WriteString("END:VEVENT\r\n")
}

func writeProp(b *strings.Builder, name, value string) {
	line := name + ":" + value
	// RFC 5545: lines MUST be <= 75 octets. Fold long lines.
	for len(line) > 75 {
		b.WriteString(line[:75])
		b.WriteString("\r\n ")
		line = line[75:]
	}
	b.WriteString(line)
	b.WriteString("\r\n")
}

func formatDateTime(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

func formatDate(t time.Time) string {
	return t.Format("20060102")
}

// formatDuration converts a Go duration to an iCal DURATION value (e.g. PT1H, PT30M).
func formatDuration(d time.Duration) string {
	if d >= 24*time.Hour {
		days := int(d / (24 * time.Hour))
		return fmt.Sprintf("P%dD", days)
	}
	hours := int(d / time.Hour)
	minutes := int((d % time.Hour) / time.Minute)
	if hours > 0 && minutes > 0 {
		return fmt.Sprintf("PT%dH%dM", hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("PT%dH", hours)
	}
	return fmt.Sprintf("PT%dM", minutes)
}

// escapeText escapes special characters per RFC 5545 section 3.3.11.
func escapeText(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, ";", `\;`)
	s = strings.ReplaceAll(s, ",", `\,`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
