// Package mailer provides a minimal SMTP client for sending transactional email.
// It uses only the standard library net/smtp — no external dependencies.
// In local development, point SMTP_HOST/SMTP_PORT at Mailpit (localhost:1025)
// so emails are captured in the Mailpit UI at http://localhost:8025.
package mailer

import (
	"fmt"
	"net/smtp"
	"strings"
)

// Mailer sends transactional email via SMTP.
type Mailer struct {
	host string // SMTP hostname (e.g. "mailpit" in Docker)
	port string // SMTP port (e.g. "1025" for Mailpit, "587" for prod)
	from string // From address for all outbound mail
}

// New creates a new Mailer. host and port are read from config (SMTP_HOST / SMTP_PORT).
func New(host, port, from string) *Mailer {
	return &Mailer{host: host, port: port, from: from}
}

// SendEmailChangeVerification sends a verification link to the new email address.
// The recipient must click the link to confirm the address change.
// link should be the full URL: https://host/auth/email-change?token=X
func (m *Mailer) SendEmailChangeVerification(to, link string) error {
	subject := "Confirm your new email address"
	body := strings.Join([]string{
		"Hello,",
		"",
		"A request was made to change the email address associated with your account.",
		"Click the link below to confirm your new email address:",
		"",
		link,
		"",
		"This link expires in 15 minutes and can only be used once.",
		"If you did not request this change, you can ignore this email.",
		"",
		"— nexus",
	}, "\r\n")

	return m.send(to, subject, body)
}

// send is the low-level SMTP delivery method. It builds a minimal RFC 5322
// message and sends it to host:port. Authentication is deliberately omitted —
// Mailpit and most internal relays don't require it. For prod with a real SMTP
// relay that needs auth, extend this with smtp.PlainAuth.
func (m *Mailer) send(to, subject, body string) error {
	addr := fmt.Sprintf("%s:%s", m.host, m.port)

	// Build raw message headers + body.
	// net/smtp.SendMail expects the full message including headers.
	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		m.from, to, subject, body,
	)

	// No auth — Mailpit and internal relays don't need it.
	// For authenticated SMTP, pass smtp.PlainAuth here.
	if err := smtp.SendMail(addr, nil, m.from, []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("smtp send to %s: %w", to, err)
	}
	return nil
}
