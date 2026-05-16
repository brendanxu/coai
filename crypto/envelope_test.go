package crypto

import (
	"encoding/base64"
	"strings"
	"testing"
)

// testKEK is a fixed 32-zero-byte KEK for deterministic CI behavior.
var testKEK = func() []byte {
	kek := make([]byte, 32)
	return kek
}()

var testKEKB64 = base64.StdEncoding.EncodeToString(testKEK)

// TestRoundTrip verifies encrypt → decrypt returns the original plaintext.
func TestRoundTrip(t *testing.T) {
	tokens := []string{
		"sk-test-abc123",
		"sk-tnx-verylongtokenvalue0123456789",
		"", // empty string is valid
	}
	for _, tok := range tokens {
		envelope, err := EncryptToken(testKEK, tok)
		if err != nil {
			t.Fatalf("EncryptToken(%q): %v", tok, err)
		}
		got, err := DecryptToken(testKEK, envelope)
		if err != nil {
			t.Fatalf("DecryptToken(%q): %v", tok, err)
		}
		if got != tok {
			t.Errorf("round-trip mismatch: want %q, got %q", tok, got)
		}
	}
}

// TestWrongKEK verifies that using the wrong KEK returns an error.
func TestWrongKEK(t *testing.T) {
	envelope, err := EncryptToken(testKEK, "sk-secret")
	if err != nil {
		t.Fatalf("EncryptToken: %v", err)
	}

	wrongKEK := make([]byte, 32)
	wrongKEK[0] = 0xFF // differ from zero-byte testKEK
	_, err = DecryptToken(wrongKEK, envelope)
	if err == nil {
		t.Fatal("expected error with wrong KEK, got nil")
	}
}

// TestDecryptLegacyPlaintext verifies ErrNotEncrypted is returned for
// legacy plaintext token values (not base64 envelopes).
func TestDecryptLegacyPlaintext(t *testing.T) {
	legacyTokens := []string{
		"sk-test-default",
		"sk-abc123456789",
		"plain",
	}
	for _, tok := range legacyTokens {
		_, err := DecryptToken(testKEK, tok)
		if err != ErrNotEncrypted {
			t.Errorf("DecryptToken(%q): want ErrNotEncrypted, got %v", tok, err)
		}
	}
}

// TestIsEncryptedEnvelope correctly distinguishes plaintext from envelopes.
func TestIsEncryptedEnvelope(t *testing.T) {
	// Real envelope should be detected as encrypted.
	envelope, err := EncryptToken(testKEK, "sk-test")
	if err != nil {
		t.Fatalf("EncryptToken: %v", err)
	}
	if !IsEncryptedEnvelope(envelope) {
		t.Errorf("IsEncryptedEnvelope(%q) = false, want true", envelope)
	}

	// Plaintext tokens must NOT be detected as encrypted.
	plaintextCases := []string{
		"sk-test-default",
		"sk-tnx-abc123",
		"",
		"short",
	}
	for _, s := range plaintextCases {
		if IsEncryptedEnvelope(s) {
			t.Errorf("IsEncryptedEnvelope(%q) = true, want false", s)
		}
	}
}

// TestLoadKEKMissing verifies (nil, nil) when env var is absent.
func TestLoadKEKMissing(t *testing.T) {
	t.Setenv(envKeyName, "")
	kek, err := LoadKEK()
	if err != nil {
		t.Fatalf("LoadKEK with empty env: %v", err)
	}
	if kek != nil {
		t.Errorf("LoadKEK with empty env: want nil kek, got %v", kek)
	}
}

// TestLoadKEKValid verifies that a valid KEK_BASE64 is decoded correctly.
func TestLoadKEKValid(t *testing.T) {
	t.Setenv(envKeyName, testKEKB64)
	kek, err := LoadKEK()
	if err != nil {
		t.Fatalf("LoadKEK: %v", err)
	}
	if len(kek) != 32 {
		t.Errorf("LoadKEK: want 32 bytes, got %d", len(kek))
	}
}

// TestLoadKEKMalformed verifies error is returned for malformed KEK.
func TestLoadKEKMalformed(t *testing.T) {
	cases := []struct {
		name string
		val  string
	}{
		{"bad_base64", "not-valid-base64!!!"},
		{"wrong_length", base64.StdEncoding.EncodeToString(make([]byte, 16))}, // 16 bytes, not 32
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envKeyName, tc.val)
			_, err := LoadKEK()
			if err == nil {
				t.Fatalf("LoadKEK(%q): want error, got nil", tc.val)
			}
		})
	}
}

// TestEncryptedEnvelopeDifferentEachCall verifies each call produces a different
// ciphertext (random DEK + nonce), but both decrypt to the same plaintext.
func TestEncryptedEnvelopeDifferentEachCall(t *testing.T) {
	tok := "sk-same-token"
	e1, err1 := EncryptToken(testKEK, tok)
	e2, err2 := EncryptToken(testKEK, tok)
	if err1 != nil || err2 != nil {
		t.Fatalf("EncryptToken errors: %v, %v", err1, err2)
	}
	if e1 == e2 {
		t.Error("two EncryptToken calls returned identical envelopes (random nonce expected)")
	}
	// Both should decrypt to the same plaintext.
	p1, _ := DecryptToken(testKEK, e1)
	p2, _ := DecryptToken(testKEK, e2)
	if p1 != tok || p2 != tok {
		t.Errorf("decrypt mismatch: got %q, %q, want %q", p1, p2, tok)
	}
}

// TestEnvelopeMinLength ensures envelope length is always >= minB64Len.
func TestEnvelopeMinLength(t *testing.T) {
	// minRawEnvelope = 89, base64 → 120 chars
	minB64Len := (minRawEnvelope + 2) / 3 * 4
	envelope, err := EncryptToken(testKEK, "")
	if err != nil {
		t.Fatalf("EncryptToken: %v", err)
	}
	if len(envelope) < minB64Len {
		t.Errorf("envelope length %d < minB64Len %d", len(envelope), minB64Len)
	}
	// Must also be detectable.
	if !IsEncryptedEnvelope(envelope) {
		t.Error("IsEncryptedEnvelope(empty-token envelope) = false")
	}
}

// TestDecryptTamperedEnvelope verifies that bit-flipping the ciphertext
// returns an error (GCM authentication).
func TestDecryptTamperedEnvelope(t *testing.T) {
	envelope, _ := EncryptToken(testKEK, "sk-tamper-test")
	raw, _ := base64.StdEncoding.DecodeString(envelope)
	// Flip a bit in the token cipher portion (past the version + encDek + dekNonce).
	tamperOffset := 1 + gcmNonceSize + encDekCipherSize + gcmNonceSize
	if tamperOffset < len(raw) {
		raw[tamperOffset] ^= 0xFF
	}
	tampered := base64.StdEncoding.EncodeToString(raw)
	_, err := DecryptToken(testKEK, tampered)
	if err == nil {
		t.Fatal("expected error decrypting tampered envelope, got nil")
	}
	if strings.Contains(err.Error(), "ErrNotEncrypted") {
		t.Fatal("wrong error type: got ErrNotEncrypted, expected authentication failure")
	}
}
