// nexus - Personal AI assistant system
// Copyright (C) 2025  nexus contributors
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

package handlers

import (
	"fmt"
	"net/http"
)

// SMSHandler handles incoming SMS messages from Twilio
// For now, responds with "world" to any incoming message
func SMSHandler(w http.ResponseWriter, r *http.Request) {
	// Parse form data (Twilio sends webhook as POST form data)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	// Get the incoming message body (optional for now, but good for logging)
	from := r.FormValue("From")
	body := r.FormValue("Body")

	// Log incoming message (for debugging)
	fmt.Printf("SMS received from %s: %s\n", from, body)

	// Respond with TwiML (Twilio Markup Language)
	// This tells Twilio to send "world" back to the sender
	w.Header().Set("Content-Type", "text/xml")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<Response>
    <Message>world</Message>
</Response>`)
}
