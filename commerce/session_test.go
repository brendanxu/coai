// session_test.go — dual-engine tests for the payment session lifecycle
// (PKG-2 Wave 2 B1). Mirrors payment/migration_test.go + plans/
// migration_test.go pattern: in-memory sqlite via globals.SqliteEngine
// flag, stub auth(id) FK target, run commerce.Migrate, exercise the API.
//
// Test matrix (per plan v2 §B1 acceptance):
//
//   - HappyPath        — open returns non-empty SessionID + correct fields
//   - TTL              — provider-specific TTL (LS/hupijiao 24h, manual 72h)
//   - Idempotent close — open → close → close again returns nil
//   - Close on expired — terminal state preserved, no error
//   - Close not found  — webhook race tolerated, returns nil
//   - Expire sweep     — past-TTL pending → 'expired' + count
//   - Duplicate UUID   — UNIQUE session_id constraint catches collision

package commerce

import (
	"chat/globals"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// newSqliteWithFKDeps spins up a fresh in-memory sqlite, flips
// globals.SqliteEngine on (with cleanup restoring the prior value),
// stubs the auth(id) FK target, and runs commerce.Migrate to create
// gtk_payment_session.
//
// Returns a ready-to-use *sql.DB. Cleanup is registered on t so callers
// don't have to defer anything.
func newSqliteWithFKDeps(t *testing.T) *sql.DB {
	t.Helper()

	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// gtk_payment_session.coai_user_id FKs auth(id); stub the target.
	if _, err := db.Exec(`CREATE TABLE auth (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	// Pre-seed an auth row so FK inserts succeed (FKs are checked even
	// without PRAGMA foreign_keys=ON for column existence; rows still
	// need to exist if we ever flip the pragma in test). Use id=1 for
	// every test for simplicity.
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (1)`); err != nil {
		t.Fatalf("seed auth row: %v", err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// TestOpenPaymentSession_HappyPath: open returns a populated session and
// the row exists in DB with the same shape.
func TestOpenPaymentSession_HappyPath(t *testing.T) {
	db := newSqliteWithFKDeps(t)

	got, err := OpenPaymentSession(
		db, "ord_test_happy", ProductToken, "lemonsqueezy",
		1500 /* $15.00 */, 1 /* user id */)
	if err != nil {
		t.Fatalf("OpenPaymentSession: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil session")
	}
	if got.SessionID == "" {
		t.Error("SessionID should be non-empty UUID")
	}
	if got.OrderNo != "ord_test_happy" {
		t.Errorf("OrderNo = %q, want ord_test_happy", got.OrderNo)
	}
	if got.ProductType != ProductToken {
		t.Errorf("ProductType = %q, want %q", got.ProductType, ProductToken)
	}
	if got.Provider != "lemonsqueezy" {
		t.Errorf("Provider = %q, want lemonsqueezy", got.Provider)
	}
	if got.AmountCents != 1500 {
		t.Errorf("AmountCents = %d, want 1500", got.AmountCents)
	}
	if got.Status != "pending" {
		t.Errorf("Status = %q, want pending", got.Status)
	}
	if got.CoaiUserID != 1 {
		t.Errorf("CoaiUserID = %d, want 1", got.CoaiUserID)
	}
	if got.ClosedAt.Valid {
		t.Error("ClosedAt should be NULL on a fresh pending session")
	}

	// Row exists in DB?
	var (
		dbStatus string
		dbAmount int64
	)
	err = globals.QueryRowDb(db,
		`SELECT status, amount_cents FROM gtk_payment_session WHERE session_id = ?`,
		got.SessionID,
	).Scan(&dbStatus, &dbAmount)
	if err != nil {
		t.Fatalf("read back row: %v", err)
	}
	if dbStatus != "pending" || dbAmount != 1500 {
		t.Errorf("DB row mismatch: status=%q amount=%d", dbStatus, dbAmount)
	}
}

// TestOpenPaymentSession_TTL: provider TTL mapping.
//
//	lemonsqueezy → 24h, hupijiao → 24h, manual → 72h
//
// Use a generous fudge factor (±5s) because we read time.Now() twice
// (once in test setup, once inside OpenPaymentSession).
func TestOpenPaymentSession_TTL(t *testing.T) {
	db := newSqliteWithFKDeps(t)

	cases := []struct {
		provider string
		wantTTL  time.Duration
	}{
		{"lemonsqueezy", 24 * time.Hour},
		{"hupijiao", 24 * time.Hour},
		{"manual", 72 * time.Hour},
	}

	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			before := time.Now().UTC()
			s, err := OpenPaymentSession(
				db, "ord_ttl_"+tc.provider, ProductService,
				tc.provider, 100, 1)
			if err != nil {
				t.Fatalf("OpenPaymentSession: %v", err)
			}
			after := time.Now().UTC()

			gotTTL := s.ExpiresAt.Sub(s.CreatedAt)
			if gotTTL != tc.wantTTL {
				t.Errorf("ExpiresAt - CreatedAt = %v, want %v",
					gotTTL, tc.wantTTL)
			}
			// Sanity: ExpiresAt is between (before+ttl) and (after+ttl).
			lo := before.Add(tc.wantTTL).Add(-5 * time.Second)
			hi := after.Add(tc.wantTTL).Add(5 * time.Second)
			if s.ExpiresAt.Before(lo) || s.ExpiresAt.After(hi) {
				t.Errorf("ExpiresAt %v outside expected window [%v, %v]",
					s.ExpiresAt, lo, hi)
			}
		})
	}
}

// TestClosePaymentSession_Idempotent: open → close → close again returns
// nil with no error. Documents H4 from autoplan review.
func TestClosePaymentSession_Idempotent(t *testing.T) {
	db := newSqliteWithFKDeps(t)

	s, err := OpenPaymentSession(db, "ord_idem", ProductToken,
		"lemonsqueezy", 500, 1)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// First close: pending → paid.
	if err := ClosePaymentSession(db, s.SessionID); err != nil {
		t.Fatalf("first close: %v", err)
	}

	// Second close: should be no-op success.
	if err := ClosePaymentSession(db, s.SessionID); err != nil {
		t.Fatalf("second close (must be idempotent): %v", err)
	}

	// State is 'paid', not crashed back to 'pending'.
	var status string
	if err := globals.QueryRowDb(db,
		`SELECT status FROM gtk_payment_session WHERE session_id = ?`,
		s.SessionID).Scan(&status); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "paid" {
		t.Errorf("status = %q after double-close, want paid", status)
	}
}

// TestClosePaymentSession_Expired: open, manually flip the row to
// 'expired' (simulating the cron beat), then close. Should return nil
// (no error) and leave the row in 'expired' state — terminal state wins.
func TestClosePaymentSession_Expired(t *testing.T) {
	db := newSqliteWithFKDeps(t)

	s, err := OpenPaymentSession(db, "ord_expired", ProductToken,
		"manual", 500, 1)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Manually expire the row (simulates the cron beating the webhook).
	if _, err := globals.ExecDb(db,
		`UPDATE gtk_payment_session SET status='expired', closed_at=? WHERE session_id=?`,
		time.Now().UTC(), s.SessionID); err != nil {
		t.Fatalf("manual expire: %v", err)
	}

	// Close should not error — terminal state mismatch is logged, not raised.
	if err := ClosePaymentSession(db, s.SessionID); err != nil {
		t.Fatalf("close on expired (must tolerate): %v", err)
	}

	// State must remain 'expired' (don't promote a stale webhook).
	var status string
	if err := globals.QueryRowDb(db,
		`SELECT status FROM gtk_payment_session WHERE session_id = ?`,
		s.SessionID).Scan(&status); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "expired" {
		t.Errorf("status = %q after close-on-expired, want expired", status)
	}
}

// TestClosePaymentSession_NotFound: a webhook can theoretically arrive
// before the corresponding session row is committed. Per H4, we tolerate
// this — return nil so the dispatcher can continue to the entitlement
// layer (which has its own idempotency).
//
// Documents the design: the contract is "best-effort match by
// session_id; failure to match is not a hard error".
func TestClosePaymentSession_NotFound(t *testing.T) {
	db := newSqliteWithFKDeps(t)

	// No session opened — close a random UUID-ish string.
	err := ClosePaymentSession(db, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Errorf("close on missing session_id should be nil-tolerant, got %v", err)
	}
}

// TestExpirePaymentSessions: open with expires_at in the past (manually
// backdated), run ExpirePaymentSessions, expect count=1 and status flipped.
//
// Also verifies a not-yet-expired session is left alone (compare-and-swap
// on status='pending' AND expires_at<now).
func TestExpirePaymentSessions(t *testing.T) {
	db := newSqliteWithFKDeps(t)

	// Session A: expired (manually backdate expires_at).
	a, err := OpenPaymentSession(db, "ord_expire_a", ProductToken,
		"lemonsqueezy", 100, 1)
	if err != nil {
		t.Fatalf("open A: %v", err)
	}
	if _, err := globals.ExecDb(db,
		`UPDATE gtk_payment_session SET expires_at=? WHERE session_id=?`,
		time.Now().UTC().Add(-1*time.Hour), a.SessionID); err != nil {
		t.Fatalf("backdate A: %v", err)
	}

	// Session B: not expired (still pending, future expires_at).
	b, err := OpenPaymentSession(db, "ord_expire_b", ProductToken,
		"manual", 100, 1)
	if err != nil {
		t.Fatalf("open B: %v", err)
	}

	count, err := ExpirePaymentSessions(db)
	if err != nil {
		t.Fatalf("ExpirePaymentSessions: %v", err)
	}
	if count != 1 {
		t.Errorf("expired count = %d, want 1 (only A should be reaped)", count)
	}

	// A is now 'expired'.
	var statusA string
	if err := globals.QueryRowDb(db,
		`SELECT status FROM gtk_payment_session WHERE session_id = ?`,
		a.SessionID).Scan(&statusA); err != nil {
		t.Fatalf("read A: %v", err)
	}
	if statusA != "expired" {
		t.Errorf("A status = %q, want expired", statusA)
	}

	// B is still 'pending'.
	var statusB string
	if err := globals.QueryRowDb(db,
		`SELECT status FROM gtk_payment_session WHERE session_id = ?`,
		b.SessionID).Scan(&statusB); err != nil {
		t.Fatalf("read B: %v", err)
	}
	if statusB != "pending" {
		t.Errorf("B status = %q, want pending", statusB)
	}
}

// TestOpenPaymentSession_DuplicateSessionID: the UNIQUE constraint on
// session_id catches a duplicate cleanly. Real UUID v4 collisions are
// astronomically improbable; we simulate by hand-crafting a duplicate
// INSERT to verify the schema's defensive layer works.
//
// Why this matters: if a future change to the UUID generator regressed
// to a non-collision-resistant scheme (e.g. timestamp-only), the schema
// is the last line of defense; this test ensures it remains effective.
func TestOpenPaymentSession_DuplicateSessionID(t *testing.T) {
	db := newSqliteWithFKDeps(t)

	// Open one session legitimately.
	s, err := OpenPaymentSession(db, "ord_dup_a", ProductToken,
		"lemonsqueezy", 100, 1)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Manually attempt to insert a second row with the same session_id —
	// the UNIQUE constraint must reject it.
	_, err = globals.ExecDb(db, `
		INSERT INTO gtk_payment_session
		  (session_id, order_no, product_type, provider,
		   amount_cents, status, coai_user_id, created_at, expires_at)
		VALUES (?, ?, 'token', 'lemonsqueezy', 100, 'pending', 1, ?, ?)
	`, s.SessionID, "ord_dup_b",
		time.Now().UTC(), time.Now().UTC().Add(24*time.Hour))
	if err == nil {
		t.Fatal("expected UNIQUE constraint violation, got nil")
	}
}
