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

// OutboundMessage is the canonical schema for messages on the sms-outbox Kafka
// topic.  Producers publish JSON-encoded OutboundMessages; the sms-sender
// consumer reads them and dispatches them to the configured SMS backend.
//
// JSON schema:
//
//	{
//	  "id":   "550e8400-e29b-41d4-a716-446655440000",
//	  "to":   "+15551234567",
//	  "body": "hello world"
//	}
type OutboundMessage struct {
	// ID is a client-generated UUID used for idempotency and correlation.
	// The sms-sender logs this value alongside the delivery outcome so
	// duplicate sends can be detected when replaying a partition.
	ID string `json:"id"`

	// To is the E.164-formatted destination phone number (e.g. "+15551234567").
	To string `json:"to"`

	// Body is the UTF-8 SMS message body.  Carriers typically truncate at
	// 160 GSM-7 characters or 70 UCS-2 characters for single-segment messages;
	// multi-segment concatenation is handled transparently by the carrier.
	Body string `json:"body"`
}
