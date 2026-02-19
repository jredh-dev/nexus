// Package identity provides email and phone normalization with SHA-256
// hashing for identity deduplication. Normalized identifiers are hashed
// so that semantically equivalent emails (e.g. Gmail +aliases and dots)
// or phone numbers map to the same hash, preventing duplicate accounts.
package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"
)

// NormalizeEmail returns a canonical form of an email address.
//
// For Gmail addresses (@gmail.com and @googlemail.com):
//   - Strips the "+suffix" from the local part (user+tag -> user)
//   - Removes all dots from the local part (u.s.e.r -> user)
//   - Normalizes @googlemail.com to @gmail.com
//
// For all addresses:
//   - Lowercases the entire address
//   - Trims whitespace
func NormalizeEmail(email string) string {
	email = strings.TrimSpace(strings.ToLower(email))

	at := strings.LastIndex(email, "@")
	if at < 0 {
		return email // malformed, return as-is
	}

	local := email[:at]
	domain := email[at+1:]

	// Normalize googlemail.com -> gmail.com
	if domain == "googlemail.com" {
		domain = "gmail.com"
	}

	isGmail := domain == "gmail.com"

	if isGmail {
		// Strip +suffix
		if plus := strings.Index(local, "+"); plus >= 0 {
			local = local[:plus]
		}
		// Remove dots
		local = strings.ReplaceAll(local, ".", "")
	}

	return local + "@" + domain
}

// NormalizePhone strips a phone number down to digits only.
// If the result is 10 digits (US number without country code), it prepends "1".
// This gives a consistent canonical form for US numbers regardless of
// whether the caller included +1, 1, or just the 10-digit number.
func NormalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)

	// Strip to digits only.
	var digits strings.Builder
	for _, r := range phone {
		if unicode.IsDigit(r) {
			digits.WriteRune(r)
		}
	}

	result := digits.String()

	// US normalization: 10 digits -> prepend country code 1
	if len(result) == 10 {
		result = "1" + result
	}

	return result
}

// HashIdentifier returns the hex-encoded SHA-256 hash of the given string.
// Use this on already-normalized values from NormalizeEmail or NormalizePhone.
func HashIdentifier(normalized string) string {
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:])
}

// EmailHash normalizes the email and returns its SHA-256 hash.
func EmailHash(email string) string {
	return HashIdentifier(NormalizeEmail(email))
}

// PhoneHash normalizes the phone number and returns its SHA-256 hash.
func PhoneHash(phone string) string {
	return HashIdentifier(NormalizePhone(phone))
}
