// Package crypto provides envelope encryption primitives for greentokey's
// at-rest token protection. The only exported surface used by production
// code is LoadKEK / EncryptToken / DecryptToken / IsEncryptedEnvelope.
//
// Envelope layout (binary, before base64):
//
//	[0]        version byte  — 0x01
//	[1..12]    encDek nonce  — 12 bytes (AES-GCM standard)
//	[13..60]   encDek cipher — 32 bytes DEK + 16 bytes GCM tag = 48 bytes
//	[61..72]   dek nonce     — 12 bytes
//	[73..]     token cipher  — len(plaintext) + 16 bytes GCM tag
//
// Minimum envelope length (empty plaintext): 1+12+48+12+16 = 89 bytes raw
// → base64 encodes to ≥ 120 characters. Stored in VARCHAR(255).
//
// Graceful rollout: LoadKEK returns (nil, nil) when KEK_BASE64 env var is
// absent — callers treat nil KEK as "plaintext mode" (backward-compat for
// dev / pre-KEK prod). Encryption is only attempted when KEK is non-nil.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	crand "crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	envKeyName  = "KEK_BASE64"
	kekLength   = 32 // AES-256
	versionByte = byte(0x01)

	// encDek fixed sizes
	gcmNonceSize = 12
	aes256TagLen = 16
	dekSize      = kekLength // DEK is also 32 bytes

	// encDekCipherSize = ciphertext(DEK) + GCM tag = 32 + 16 = 48
	// (nonce is stored separately as encDekNonce)
	encDekCipherSize = dekSize + aes256TagLen

	// minRawEnvelope = version(1) + encDekNonce(12) + encDekCipher(48) + dekNonce(12) + minTokenCipher(16)
	minRawEnvelope = 1 + gcmNonceSize + encDekCipherSize + gcmNonceSize + aes256TagLen
)

// ErrNotEncrypted is returned by DecryptToken when the input does not look
// like a valid v1 envelope. Callers (LoadBinding) use this sentinel to
// detect legacy plaintext rows and fall through to the plaintext path.
var ErrNotEncrypted = errors.New("crypto: not an encrypted envelope")

// LoadKEK reads the master key-encryption key from the KEK_BASE64 environment
// variable. Returns (nil, nil) if the env var is absent — signals plaintext
// mode to callers. Returns a non-nil error only if the env var is present but
// malformed (wrong length after base64 decode, or bad base64).
func LoadKEK() ([]byte, error) {
	raw := os.Getenv(envKeyName)
	if raw == "" {
		return nil, nil // graceful: encryption disabled
	}
	kek, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("crypto: KEK_BASE64 base64 decode: %w", err)
	}
	if len(kek) != kekLength {
		return nil, fmt.Errorf("crypto: KEK_BASE64 must decode to %d bytes, got %d", kekLength, len(kek))
	}
	return kek, nil
}

// EncryptToken encrypts plaintext using a fresh random DEK (AES-256-GCM) and
// wraps the DEK with kek (also AES-256-GCM). Returns a base64-encoded
// envelope string safe for VARCHAR(255) storage.
//
// kek must be exactly 32 bytes. plaintext may be empty string (encrypts
// to a minimal ciphertext; DecryptToken will return "").
func EncryptToken(kek []byte, plaintext string) (string, error) {
	if len(kek) != kekLength {
		return "", fmt.Errorf("crypto: kek must be %d bytes, got %d", kekLength, len(kek))
	}

	// Generate a fresh 32-byte DEK.
	dek := make([]byte, dekSize)
	if _, err := io.ReadFull(crand.Reader, dek); err != nil {
		return "", fmt.Errorf("crypto: generate DEK: %w", err)
	}

	// Encrypt DEK with KEK.
	encDekNonce, encDekCipher, err := gcmEncrypt(kek, dek)
	if err != nil {
		return "", fmt.Errorf("crypto: encrypt DEK: %w", err)
	}

	// Encrypt plaintext with DEK.
	dekNonce, tokenCipher, err := gcmEncrypt(dek, []byte(plaintext))
	if err != nil {
		return "", fmt.Errorf("crypto: encrypt token: %w", err)
	}

	// Pack envelope: version || encDekNonce || encDekCipher || dekNonce || tokenCipher
	total := 1 + len(encDekNonce) + len(encDekCipher) + len(dekNonce) + len(tokenCipher)
	buf := make([]byte, 0, total)
	buf = append(buf, versionByte)
	buf = append(buf, encDekNonce...)
	buf = append(buf, encDekCipher...)
	buf = append(buf, dekNonce...)
	buf = append(buf, tokenCipher...)

	return base64.StdEncoding.EncodeToString(buf), nil
}

// DecryptToken decodes and decrypts a base64-encoded envelope produced by
// EncryptToken. Returns ErrNotEncrypted if envelopeB64 is not a valid v1
// envelope (e.g., it's a legacy plaintext token from before PKG-M6).
// Returns a different error if the envelope is valid but decryption fails
// (e.g., wrong KEK, tampered ciphertext).
func DecryptToken(kek []byte, envelopeB64 string) (string, error) {
	if len(kek) != kekLength {
		return "", fmt.Errorf("crypto: kek must be %d bytes, got %d", kekLength, len(kek))
	}

	raw, err := base64.StdEncoding.DecodeString(envelopeB64)
	if err != nil || len(raw) < minRawEnvelope {
		return "", ErrNotEncrypted
	}
	if raw[0] != versionByte {
		return "", ErrNotEncrypted
	}

	// Parse fields.
	offset := 1
	encDekNonce := raw[offset : offset+gcmNonceSize]
	offset += gcmNonceSize
	encDekCipher := raw[offset : offset+encDekCipherSize]
	offset += encDekCipherSize
	dekNonce := raw[offset : offset+gcmNonceSize]
	offset += gcmNonceSize
	tokenCipher := raw[offset:]

	// Unwrap DEK.
	dek, err := gcmDecrypt(kek, encDekNonce, encDekCipher)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt DEK: %w", err)
	}

	// Decrypt token.
	plainBytes, err := gcmDecrypt(dek, dekNonce, tokenCipher)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt token: %w", err)
	}

	return string(plainBytes), nil
}

// IsEncryptedEnvelope returns true when s is long enough and base64-valid to
// be a v1 envelope. Used by LoadBinding to differentiate legacy plaintext
// rows from new ciphertext rows without a full decode attempt.
//
// The heuristic: minimum raw length is minRawEnvelope (89) bytes; base64
// of that is ceil(89/3)*4 = 120 characters. Real sk-xxx tokens are ≤ 64
// chars, so there is no ambiguity.
func IsEncryptedEnvelope(s string) bool {
	minB64Len := (minRawEnvelope + 2) / 3 * 4 // = 120
	if len(s) < minB64Len {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(raw) < minRawEnvelope {
		return false
	}
	return raw[0] == versionByte
}

// gcmEncrypt encrypts plaintext with key using AES-256-GCM. Returns
// (nonce, ciphertext+tag).
func gcmEncrypt(key, plaintext []byte) (nonce, ciphertext []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(crand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return nonce, ciphertext, nil
}

// gcmDecrypt decrypts ciphertext (with GCM tag appended) using key + nonce.
func gcmDecrypt(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}
