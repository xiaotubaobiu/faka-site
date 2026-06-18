package payment

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// encPrefix marks encrypted values stored in the config table so they can be
// distinguished from legacy plaintext on read. Versioned for forward compat.
const encPrefix = "enc:v1:"

// secretKeyLen is the AES-256 key length in bytes.
const secretKeyLen = 32

var (
	secretOnce sync.Once
	secretKey  []byte
	secretErr  error

	ErrSecretNotConfigured = errors.New("PAY_SECRET not configured (must be 32 bytes / 64 hex chars)")
)

// InitSecret loads the AES-256 master key from the PAY_SECRET environment
// variable. It accepts either 32 raw bytes or 64 hex characters. It is safe
// to call repeatedly; the first call wins.
//
// We deliberately initialize lazily (via mustSecret) instead of panicking at
// import time, so that tests and commands that never touch encrypted config
// don't require the env var to be set.
func InitSecret() error {
	secretOnce.Do(func() {
		raw := os.Getenv("PAY_SECRET")
		if raw == "" {
			// Lazy: leave unset; Seal/Open will surface a clear error when used.
			secretErr = ErrSecretNotConfigured
			return
		}
		key, err := parseSecret(raw)
		if err != nil {
			secretErr = err
			return
		}
		secretKey = key
	})
	return secretErr
}

// parseSecret accepts either 32 raw bytes or a 64-char hex string.
func parseSecret(raw string) ([]byte, error) {
	if len(raw) == secretKeyLen*2 {
		if b, err := hex.DecodeString(raw); err == nil && len(b) == secretKeyLen {
			return b, nil
		}
	}
	if len(raw) == secretKeyLen {
		return []byte(raw), nil
	}
	return nil, fmt.Errorf("PAY_SECRET must be %d bytes or %d hex chars, got %d", secretKeyLen, secretKeyLen*2, len(raw))
}

// SecretConfigured reports whether a usable master key is loaded.
func SecretConfigured() bool {
	_ = InitSecret()
	return len(secretKey) == secretKeyLen
}

// Seal encrypts plaintext with AES-256-GCM and returns a prefixed, base64
// string suitable for storage in the config KV table. Empty input is returned
// unchanged so empty fields don't get encrypted noise written to them.
func Seal(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	if err := InitSecret(); err != nil {
		return "", err
	}
	block, err := aes.NewCipher(secretKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	// Seal appends ciphertext+tag to nonce.
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ct), nil
}

// Open reverses Seal. A value without the encPrefix is treated as legacy
// plaintext and returned as-is (backward compat for old config rows). Empty
// input yields empty output.
func Open(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	if len(stored) <= len(encPrefix) || stored[:len(encPrefix)] != encPrefix {
		// Legacy plaintext value — pass through.
		return stored, nil
	}
	if err := InitSecret(); err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(stored[len(encPrefix):])
	if err != nil {
		return "", fmt.Errorf("decode encrypted value: %w", err)
	}
	block, err := aes.NewCipher(secretKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns+gcm.Overhead() {
		return "", errors.New("encrypted value too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt value: %w", err)
	}
	return string(pt), nil
}
