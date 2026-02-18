// nascent-nexus - Personal AI assistant system
// Copyright (C) 2025  nascent-nexus contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published
// by the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.

// Package sms provides types and services for outbound SMS delivery via
// Kafka-driven pub/sub and configurable SMS backends.
package sms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const telnyxMessagesURL = "https://api.telnyx.com/v2/messages"

// TelnyxSender sends outbound SMS messages via the Telnyx REST API using
// stdlib net/http only — no SDK dependency.
//
// Telnyx was chosen as the initial backend because:
//   - Transparent privacy policy: no third-party data sales, no ad targeting
//   - Easy REST API, well-documented
//   - ~$1/month for a number + $0.004/SMS outbound (very cheap for dev)
//   - Trivially swappable: implement Sender and point at a different backend
//     (e.g. an Android phone running android-sms-gateway-server)
//
// The long-term goal is to replace this with a hardware-only path
// (Android phone + SIM, or USB GSM modem on Linux) to eliminate all
// third-party visibility into message content.
type TelnyxSender struct {
	apiKey     string
	fromNumber string
	httpClient *http.Client
}

// Sender is the interface any SMS backend must implement.  Keeping it minimal
// means backends are trivially swappable without changing the Kafka consumer.
type Sender interface {
	Send(ctx context.Context, msg OutboundMessage) error
}

// NewTelnyxSender creates a TelnyxSender ready to use.
//
// apiKey is the Telnyx API v2 key (starts with "KEY...").
// fromNumber is your Telnyx-provisioned number in E.164 format (e.g. "+15550001234").
func NewTelnyxSender(apiKey, fromNumber string) *TelnyxSender {
	return &TelnyxSender{
		apiKey:     apiKey,
		fromNumber: fromNumber,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// telnyxRequest is the JSON body sent to POST /v2/messages.
type telnyxRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
	Text string `json:"text"`
}

// telnyxResponse captures just the fields we care about for logging.
type telnyxResponse struct {
	Data struct {
		ID string `json:"id"`
		To []struct {
			Status string `json:"status"`
		} `json:"to"`
	} `json:"data"`
	Errors []struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	} `json:"errors"`
}

// Send dispatches msg to the Telnyx API.  It returns a non-nil error if the
// HTTP request fails or Telnyx returns a non-2xx status.  The caller (Kafka
// consumer) decides whether to retry or route to the DLQ.
func (s *TelnyxSender) Send(ctx context.Context, msg OutboundMessage) error {
	body, err := json.Marshal(telnyxRequest{
		From: s.fromNumber,
		To:   msg.To,
		Text: msg.Body,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, telnyxMessagesURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telnyx returned %d: %s", resp.StatusCode, string(respBody))
	}

	var telResp telnyxResponse
	if err := json.Unmarshal(respBody, &telResp); err == nil && len(telResp.Errors) > 0 {
		return fmt.Errorf("telnyx error %s: %s", telResp.Errors[0].Code, telResp.Errors[0].Detail)
	}

	return nil
}
