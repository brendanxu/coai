package newapi

// PKG-M6 integration tests: envelope encryption at the SaveBinding/LoadBinding
// boundary. These tests exercise all four paths:
//   - KEK enabled: save encrypts, load decrypts (round-trip)
//   - KEK enabled: load on a legacy plaintext row returns plaintext (fallback)
//   - KEK disabled (env unset): save stores plaintext, load returns plaintext
//   - Wrong KEK on load: returns error (no silent garbage)
//
// Uses t.Setenv("KEK_BASE64", ...) for isolated env management per test.
// testKEKB64 = base64(32 zero bytes) matches crypto/envelope_test.go convention.

import (
	"encoding/base64"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// testKEKB64M6 is a 32-zero-byte KEK encoded as base64, for deterministic CI.
var testKEKB64M6 = base64.StdEncoding.EncodeToString(make([]byte, 32))

// wrongKEKB64M6 differs from testKEKB64M6 by one byte to simulate a KEK rotation
// scenario where the wrong key is used.
var wrongKEKB64M6 = func() string {
	k := make([]byte, 32)
	k[0] = 0xFF
	return base64.StdEncoding.EncodeToString(k)
}()

// TestM6_KEKEnabled_RoundTrip: KEK present → SaveBinding stores envelope;
// LoadBinding decrypts back to original plaintext.
func TestM6_KEKEnabled_RoundTrip(t *testing.T) {
	t.Setenv("KEK_BASE64", testKEKB64M6)

	db := newSqliteWithFKDeps(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (100)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}

	plain := "sk-tnx-plaintoken123"
	if err := SaveBinding(db, &Binding{
		CoaiUserID:     100,
		NewapiUserID:   200,
		NewapiTokenID:  300,
		NewapiTokenKey: plain,
	}); err != nil {
		t.Fatalf("SaveBinding: %v", err)
	}

	// Verify the raw DB value is NOT the plaintext (it's an envelope).
	var rawVal string
	if err := db.QueryRow(`SELECT newapi_token_key FROM gtk_newapi_binding WHERE coai_user_id = 100`).Scan(&rawVal); err != nil {
		t.Fatalf("raw DB scan: %v", err)
	}
	if rawVal == plain {
		t.Error("token stored as plaintext in DB — encryption not applied")
	}
	if !strings.HasPrefix(rawVal, "") || len(rawVal) < 120 {
		t.Errorf("raw DB value looks too short to be an envelope: %q", rawVal)
	}

	// LoadBinding should decrypt back to the original plaintext.
	got, err := LoadBinding(db, 100)
	if err != nil {
		t.Fatalf("LoadBinding: %v", err)
	}
	if got.NewapiTokenKey != plain {
		t.Errorf("LoadBinding.NewapiTokenKey = %q, want %q", got.NewapiTokenKey, plain)
	}
}

// TestM6_KEKEnabled_LegacyPlaintextFallback: KEK present + legacy plaintext
// row in DB → LoadBinding returns the plaintext unchanged (no error).
func TestM6_KEKEnabled_LegacyPlaintextFallback(t *testing.T) {
	t.Setenv("KEK_BASE64", testKEKB64M6)

	db := newSqliteWithFKDeps(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (101)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}

	// Insert a legacy plaintext row directly (bypassing SaveBinding encryption).
	legacyToken := "sk-test-legacy-plain"
	if _, err := db.Exec(`
		INSERT INTO gtk_newapi_binding
		    (coai_user_id, newapi_user_id, newapi_token_id, newapi_token_key, newapi_group, last_known_quota)
		VALUES (101, 201, 301, ?, 'default', 0)
	`, legacyToken); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	// LoadBinding with KEK set should recognize plaintext (not an envelope)
	// and return it as-is.
	got, err := LoadBinding(db, 101)
	if err != nil {
		t.Fatalf("LoadBinding on legacy plaintext: %v", err)
	}
	if got.NewapiTokenKey != legacyToken {
		t.Errorf("LoadBinding = %q, want %q", got.NewapiTokenKey, legacyToken)
	}
}

// TestM6_KEKDisabled_PlaintextPassthrough: KEK env unset → SaveBinding stores
// plaintext; LoadBinding returns plaintext (existing behavior unchanged).
func TestM6_KEKDisabled_PlaintextPassthrough(t *testing.T) {
	t.Setenv("KEK_BASE64", "") // explicitly unset

	db := newSqliteWithFKDeps(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (102)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}

	plain := "sk-test-no-kek"
	if err := SaveBinding(db, &Binding{
		CoaiUserID:     102,
		NewapiUserID:   202,
		NewapiTokenID:  302,
		NewapiTokenKey: plain,
	}); err != nil {
		t.Fatalf("SaveBinding: %v", err)
	}

	// Raw DB value must be the plaintext (no encryption when KEK is absent).
	var rawVal string
	if err := db.QueryRow(`SELECT newapi_token_key FROM gtk_newapi_binding WHERE coai_user_id = 102`).Scan(&rawVal); err != nil {
		t.Fatalf("raw DB scan: %v", err)
	}
	if rawVal != plain {
		t.Errorf("raw DB value = %q, want plaintext %q (KEK disabled)", rawVal, plain)
	}

	got, err := LoadBinding(db, 102)
	if err != nil {
		t.Fatalf("LoadBinding: %v", err)
	}
	if got.NewapiTokenKey != plain {
		t.Errorf("LoadBinding = %q, want %q", got.NewapiTokenKey, plain)
	}
}

// TestM6_WrongKEK_ReturnsError: envelope saved with testKEK; subsequent load
// with a different KEK must return an error (not silently return garbage).
func TestM6_WrongKEK_ReturnsError(t *testing.T) {
	// Save with the correct KEK.
	t.Setenv("KEK_BASE64", testKEKB64M6)

	db := newSqliteWithFKDeps(t)
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (103)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}

	if err := SaveBinding(db, &Binding{
		CoaiUserID:     103,
		NewapiUserID:   203,
		NewapiTokenID:  303,
		NewapiTokenKey: "sk-secret-token",
	}); err != nil {
		t.Fatalf("SaveBinding: %v", err)
	}

	// Switch to the wrong KEK for the load.
	t.Setenv("KEK_BASE64", wrongKEKB64M6)

	_, err := LoadBinding(db, 103)
	if err == nil {
		t.Fatal("LoadBinding with wrong KEK: expected error, got nil")
	}
}
