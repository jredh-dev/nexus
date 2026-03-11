package kafka

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

// Encrypt encrypts plaintext with AES-256-GCM using key (must be 32 bytes).
// Returns ciphertext and a freshly-generated 12-byte GCM nonce separately.
// The nonce must be stored alongside the ciphertext so it can be passed to
// Decrypt later.
func Encrypt(key, plaintext []byte) (ciphertext, nonce []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("kafka/crypto: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("kafka/crypto: new GCM: %w", err)
	}

	// Generate a random 12-byte nonce (GCM standard size).
	nonce = make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("kafka/crypto: generate nonce: %w", err)
	}

	// Seal appends the authenticated ciphertext to dst (nil here).
	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// Decrypt decrypts AES-256-GCM ciphertext using key and nonce.
// Returns the original plaintext or an error if authentication fails.
func Decrypt(key, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("kafka/crypto: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("kafka/crypto: new GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("kafka/crypto: decrypt: %w", err)
	}
	return plaintext, nil
}

// ParseKey parses a 64-char hex string into a 32-byte AES-256 key.
// Returns an error if the string is not exactly 64 hex characters.
func ParseKey(hexKey string) ([]byte, error) {
	if len(hexKey) != 64 {
		return nil, fmt.Errorf("kafka/crypto: key must be 64 hex chars (got %d)", len(hexKey))
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("kafka/crypto: decode hex key: %w", err)
	}
	return key, nil
}
