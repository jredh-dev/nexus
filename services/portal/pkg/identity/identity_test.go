package identity

import (
	"testing"
)

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercase", "User@Example.COM", "user@example.com"},
		{"trim whitespace", "  user@example.com  ", "user@example.com"},
		{"gmail plus alias", "user+shopping@gmail.com", "user@gmail.com"},
		{"gmail dots", "u.s.e.r@gmail.com", "user@gmail.com"},
		{"gmail dots and plus", "u.s.e.r+tag@gmail.com", "user@gmail.com"},
		{"googlemail to gmail", "user@googlemail.com", "user@gmail.com"},
		{"googlemail dots and plus", "u.s.e.r+tag@googlemail.com", "user@gmail.com"},
		{"non-gmail plus preserved", "user+tag@outlook.com", "user+tag@outlook.com"},
		{"non-gmail dots preserved", "first.last@outlook.com", "first.last@outlook.com"},
		{"no at sign", "noemail", "noemail"},
		{"empty string", "", ""},
		{"multiple plus signs gmail", "user+a+b@gmail.com", "user@gmail.com"},
		{"domain only dots in gmail", "....@gmail.com", "@gmail.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeEmail(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeEmail(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizePhone(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"digits only", "5551234567", "15551234567"},
		{"with country code", "15551234567", "15551234567"},
		{"with plus and country code", "+15551234567", "15551234567"},
		{"formatted US", "(555) 123-4567", "15551234567"},
		{"formatted with country", "+1 (555) 123-4567", "15551234567"},
		{"dashes", "555-123-4567", "15551234567"},
		{"dots", "555.123.4567", "15551234567"},
		{"spaces", "555 123 4567", "15551234567"},
		{"international", "+442071234567", "442071234567"},
		{"empty string", "", ""},
		{"whitespace only", "   ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizePhone(tt.input)
			if got != tt.want {
				t.Errorf("NormalizePhone(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHashIdentifier(t *testing.T) {
	// SHA-256 of "test" is known.
	got := HashIdentifier("test")
	want := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	if got != want {
		t.Errorf("HashIdentifier(%q) = %q, want %q", "test", got, want)
	}

	// Same input -> same hash.
	hash1 := HashIdentifier("hello")
	hash2 := HashIdentifier("hello")
	if hash1 != hash2 {
		t.Error("same input should produce same hash")
	}

	// Different input -> different hash.
	if HashIdentifier("hello") == HashIdentifier("world") {
		t.Error("different inputs should produce different hashes")
	}
}

func TestEmailHash_GmailEquivalence(t *testing.T) {
	// All of these Gmail variants should produce the same hash.
	variants := []string{
		"user@gmail.com",
		"u.s.e.r@gmail.com",
		"user+shopping@gmail.com",
		"u.s.e.r+tag@gmail.com",
		"USER@gmail.com",
		"User@GMAIL.COM",
		"user@googlemail.com",
	}

	expected := EmailHash(variants[0])
	for _, v := range variants[1:] {
		got := EmailHash(v)
		if got != expected {
			t.Errorf("EmailHash(%q) = %q, want %q (same as %q)", v, got, expected, variants[0])
		}
	}
}

func TestPhoneHash_Equivalence(t *testing.T) {
	// All of these phone formats should produce the same hash.
	variants := []string{
		"5551234567",
		"15551234567",
		"+15551234567",
		"(555) 123-4567",
		"+1 (555) 123-4567",
		"555-123-4567",
		"555.123.4567",
	}

	expected := PhoneHash(variants[0])
	for _, v := range variants[1:] {
		got := PhoneHash(v)
		if got != expected {
			t.Errorf("PhoneHash(%q) = %q, want %q (same as %q)", v, got, expected, variants[0])
		}
	}
}
