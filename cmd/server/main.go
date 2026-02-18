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

package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/jredh-dev/nascent-nexus/internal/handlers"
)

func main() {
	// Health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
	})

	// SMS webhook endpoint (Twilio will POST here)
	http.HandleFunc("/sms", handlers.SMSHandler)

	// Start server
	port := "8080"
	fmt.Printf("🚀 nascent-nexus server starting on port %s\n", port)
	fmt.Printf("📱 SMS webhook available at: http://localhost:%s/sms\n", port)
	fmt.Printf("💚 Health check at: http://localhost:%s/health\n", port)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
