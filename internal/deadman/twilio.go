// Package deadman - Twilio SMS send helper and webhook auto-config.
package deadman

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// TwilioConfig holds Twilio credentials and phone numbers.
type TwilioConfig struct {
	AccountSID string
	AuthToken  string
	From       string // E.164 Twilio number
}

// SendSMS sends a single SMS via Twilio REST API.
func SendSMS(cfg TwilioConfig, to, body string) error {
	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", cfg.AccountSID)
	data := url.Values{}
	data.Set("To", to)
	data.Set("From", cfg.From)
	data.Set("Body", body)

	req, err := http.NewRequest(http.MethodPost, apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(cfg.AccountSID, cfg.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twilio %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// TwiMLResponse returns a TwiML XML response with an optional reply message.
// Pass empty string for body to send no reply (empty <Response/>).
func TwiMLResponse(body string) string {
	if body == "" {
		return `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response><Message>%s</Message></Response>`, body)
}

// ConsentSMS returns the consent invitation message sent to a new subscriber.
func ConsentSMS(ownerPhone string) string {
	return fmt.Sprintf(
		"You've been added to %s's deadman switch. Reply Y to subscribe, N to decline, Q to stop receiving these messages.",
		ownerPhone,
	)
}

// TriggerSMS returns the alert message sent to a subscriber when a deadman fires.
func TriggerSMS(ownerPhone string) string {
	return fmt.Sprintf(
		"%s's deadman has triggered. Reply: R=status update, W=ask why, H=ask how, U=unsubscribe all",
		ownerPhone,
	)
}

// StatusSMS returns a current-status message for a subscriber's R reply.
func StatusSMS(ownerPhone string, o Owner) string {
	return fmt.Sprintf("%s — %s", ownerPhone, OwnerStatus(o))
}

// PollAckSMS returns the ack sent to a subscriber after a W or H poll.
func PollAckSMS(pollType PollType) string {
	switch pollType {
	case PollWhy:
		return "Your 'why' request has been logged. An administrator will follow up."
	case PollHow:
		return "Your 'how' request has been logged. An administrator will follow up."
	}
	return "Your request has been logged."
}

// AdminPollSMS returns the message sent to all admins when a subscriber polls.
func AdminPollSMS(pollType PollType, subscriberPhone, ownerPhone string) string {
	switch pollType {
	case PollWhy:
		return fmt.Sprintf("POLL WHY: %s is asking why %s's deadman triggered. Handle via contact info on file.", subscriberPhone, ownerPhone)
	case PollHow:
		return fmt.Sprintf("POLL HOW: %s is asking how %s is doing. Handle via contact info on file.", subscriberPhone, ownerPhone)
	}
	return fmt.Sprintf("POLL: %s asked about %s's deadman.", subscriberPhone, ownerPhone)
}

// UnsubscribeConfirmSMS returns the per-deadman confirmation request.
func UnsubscribeConfirmSMS(ownerPhone string) string {
	return fmt.Sprintf("Confirm unsubscribe from %s's deadman? Reply Y to confirm, N to keep.", ownerPhone)
}

// UnsubscribeDoneSMS confirms a single unsubscribe.
func UnsubscribeDoneSMS(ownerPhone string) string {
	return fmt.Sprintf("Unsubscribed from %s's deadman.", ownerPhone)
}

// -----------------------------------------------------------------------
// Webhook auto-config
// -----------------------------------------------------------------------

// twilioPhoneList is the subset of the Twilio IncomingPhoneNumbers list response
// we care about.
type twilioPhoneList struct {
	IncomingPhoneNumbers []struct {
		Sid string `json:"sid"`
		// PhoneNumber is the E.164 number, e.g. "+15706006135"
		PhoneNumber string `json:"phone_number"`
	} `json:"incoming_phone_numbers"`
}

// ConfigureWebhook sets the SMS webhook URL on the Twilio number cfg.From to
// smsURL (e.g. "https://nexus-deadman-dev-xxx.run.app/sms").
//
// Steps:
//  1. List all IncomingPhoneNumbers to find the SID matching cfg.From.
//  2. PATCH that number's SmsUrl.
//
// This is called once at serve startup when DEADMAN_PUBLIC_URL is set.
// It is idempotent — safe to call on every restart.
func ConfigureWebhook(cfg TwilioConfig, smsURL string) error {
	sid, err := twilioFindPhoneSID(cfg)
	if err != nil {
		return fmt.Errorf("find phone SID: %w", err)
	}

	patchURL := fmt.Sprintf(
		"https://api.twilio.com/2010-04-01/Accounts/%s/IncomingPhoneNumbers/%s.json",
		cfg.AccountSID, sid,
	)
	data := url.Values{}
	data.Set("SmsUrl", smsURL)
	data.Set("SmsMethod", "POST")

	req, err := http.NewRequest(http.MethodPost, patchURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(cfg.AccountSID, cfg.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twilio patch %d: %s", resp.StatusCode, string(b))
	}

	slog.Info("twilio webhook configured", "number", cfg.From, "url", smsURL)
	return nil
}

// twilioFindPhoneSID returns the Twilio IncomingPhoneNumber SID for cfg.From.
// Returns an error if the number is not found in the account.
func twilioFindPhoneSID(cfg TwilioConfig) (string, error) {
	listURL := fmt.Sprintf(
		"https://api.twilio.com/2010-04-01/Accounts/%s/IncomingPhoneNumbers.json?PhoneNumber=%s",
		cfg.AccountSID, url.QueryEscape(cfg.From),
	)

	req, err := http.NewRequest(http.MethodGet, listURL, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(cfg.AccountSID, cfg.AuthToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("twilio list %d: %s", resp.StatusCode, string(b))
	}

	var result twilioPhoneList
	if err := json.Unmarshal(b, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(result.IncomingPhoneNumbers) == 0 {
		return "", fmt.Errorf("phone number %s not found in Twilio account", cfg.From)
	}

	return result.IncomingPhoneNumbers[0].Sid, nil
}
