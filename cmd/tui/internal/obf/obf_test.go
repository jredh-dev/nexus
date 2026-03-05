// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

package obf

import (
	"testing"
)

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		plaintext  string
		passphrase string
	}{
		{"simple", "hello", "secret"},
		{"url", "nexus-hermit-dev-2tvic4xjjq-uc.a.run.app:443", "my-passphrase"},
		{"base64 secret", "jIK4zs9kee6Ezpgy5Jo/c+CR/xtNMLskrvi6Bzu2VDM=", "build-passphrase"},
		{"https url", "https://nexus-secrets-dev-2tvic4xjjq-uc.a.run.app", "p@ss"},
		{"empty passphrase", "some-value", ""},
		{"unicode", "héllo wörld", "clé"},
		{"long value", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded := Encode(tt.plaintext, tt.passphrase)
			if encoded == "" {
				t.Fatal("Encode returned empty string")
			}
			if encoded == tt.plaintext {
				t.Fatal("Encode returned plaintext unchanged")
			}

			decoded, err := Decode(encoded, tt.passphrase)
			if err != nil {
				t.Fatalf("Decode error: %v", err)
			}
			if decoded != tt.plaintext {
				t.Fatalf("round-trip failed: got %q, want %q", decoded, tt.plaintext)
			}
		})
	}
}

func TestDecode_EmptyValue(t *testing.T) {
	_, err := Decode("", "passphrase")
	if err == nil {
		t.Fatal("expected error for empty encoded value")
	}
}

func TestDecode_InvalidHex(t *testing.T) {
	_, err := Decode("not-hex!", "passphrase")
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestDecode_WrongPassphrase(t *testing.T) {
	encoded := Encode("secret-value", "correct-passphrase")
	decoded, err := Decode(encoded, "wrong-passphrase")
	if err != nil {
		t.Fatalf("Decode should not error with wrong passphrase: %v", err)
	}
	if decoded == "secret-value" {
		t.Fatal("wrong passphrase should not decode to original plaintext")
	}
}

func TestKeyTamperDetection(t *testing.T) {
	// obfKey is Encode(passphrase, binaryName). If the binary is renamed,
	// Decode(obfKey, newName) returns a different passphrase, and subsequent
	// decodes produce garbage.
	passphrase := "build-time-secret"
	obfKey := Encode(passphrase, "tui")

	// Correct binary name recovers passphrase
	recovered, err := Decode(obfKey, "tui")
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if recovered != passphrase {
		t.Fatalf("got %q, want %q", recovered, passphrase)
	}

	// Renamed binary gets wrong passphrase
	wrong, err := Decode(obfKey, "renamed-binary")
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if wrong == passphrase {
		t.Fatal("renamed binary should not recover correct passphrase")
	}
}
