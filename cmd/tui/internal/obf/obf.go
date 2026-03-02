// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

// Package obf provides XOR obfuscation for build-time embedded values.
//
// Design intent:
//   - The server address and shared secret are XOR-encoded at build time and
//     baked into the binary via -ldflags. They are never stored in plaintext
//     in the binary's data section, config files, or logs.
//   - Encoding uses a key derived from a build-time passphrase via SHA-256.
//     The passphrase itself is NOT in the binary — it is consumed only by the
//     CI/CD runner (or developer) at build time and discarded.
//   - Decoded values exist only in process memory, for the duration of the
//     connection setup, then are cleared.
//
// Threat model:
//   - Raises the bar against `strings`, `objdump`, and casual static analysis.
//   - Does NOT protect against a determined attacker with full binary + memory
//     access (runtime debugger, core dump). That requires a different threat
//     model (mTLS, short-lived tokens, etc. — deferred to TODO.md).
//
// Wire format (build time):
//   encoded = hex( xor(plaintext, stretch(passphrase)) )
//
// The Makefile encodes using `go run ./cmd/tui/internal/obf/encode`.

package obf

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Decode decodes a hex-encoded XOR-obfuscated value using the given passphrase.
// Returns the plaintext as a string. Returns an error if the encoded value is
// empty (treat as "not set") or malformed.
func Decode(encoded, passphrase string) (string, error) {
	if encoded == "" {
		return "", fmt.Errorf("obf: encoded value is empty")
	}
	ciphertext, err := hex.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("obf: invalid hex: %w", err)
	}
	key := stretchKey(passphrase, len(ciphertext))
	plain := xorBytes(ciphertext, key)
	return string(plain), nil
}

// Encode encodes a plaintext value using the given passphrase.
// Used by the build tool (cmd/tui/internal/obf/encode/main.go).
func Encode(plaintext, passphrase string) string {
	plain := []byte(plaintext)
	key := stretchKey(passphrase, len(plain))
	ciphertext := xorBytes(plain, key)
	return hex.EncodeToString(ciphertext)
}

// stretchKey derives a key of length n from a passphrase using SHA-256 in
// counter mode (poor man's KDF — sufficient for obfuscation, not for
// cryptographic security).
func stretchKey(passphrase string, n int) []byte {
	key := make([]byte, 0, n)
	counter := uint32(0)
	for len(key) < n {
		h := sha256.New()
		h.Write([]byte(passphrase))
		// Mix in counter so each 32-byte block is distinct
		h.Write([]byte{byte(counter >> 24), byte(counter >> 16), byte(counter >> 8), byte(counter)})
		key = append(key, h.Sum(nil)...)
		counter++
	}
	return key[:n]
}

func xorBytes(a, b []byte) []byte {
	out := make([]byte, len(a))
	for i := range a {
		out[i] = a[i] ^ b[i]
	}
	return out
}
