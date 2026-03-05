package gohttp

import (
	"encoding/json"
	"log"
	"net/http"
)

// WriteJSON encodes v as JSON and writes it to w with the given status code.
// Sets Content-Type to application/json. Logs encoding errors but does not
// return them — headers are already sent by the time encoding can fail.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[gohttp] encode JSON response: %v", err)
	}
}

// WriteError writes a JSON error response: {"error": msg}.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// DecodeJSON reads the request body as JSON into v. Callers should check
// the returned error and respond with 400 if non-nil.
func DecodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
