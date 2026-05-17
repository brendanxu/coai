// admin_tokens_integration_test.go — PKG-A-3 Wave 4 end-to-end lifecycle tests.
//
// Strategy:
//   - All tests use fakeNewAPIClient (from admin_tokens_test.go) + SQLite in-memory DB.
//   - Tests cover the full lifecycle: create → list → max-limit → last-token guard →
//     multi-token create/revoke → admin force-revoke bypasses guard.
//   - Deliberately kept at the tokenManager level (not HTTP handler level) because
//     AdminListTokensAPI / AdminRevokeTokenAPI call connection.DB which is a global;
//     mocking that at the HTTP layer would require gin router setup and is out of scope
//     for a 30-min integration test budget.
//
// Run: go test ./newapi/... -run TestIntegration -v

package newapi

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestIntegration_FullTokenLifecycle covers:
//   1. CreateUserToken (alice) → get plaintext sk-xxx
//   2. ListUserTokens(alice) → 1 token, masked
//   3. CreateUserToken at limit (10 active) → ErrMaxTokensReached
//   4. RevokeToken last active token → ErrCannotRevokeLastToken
//   5. CreateUserToken second → success (back to 2 active)
//   6. RevokeToken first → success (1 active remains)
//   7. Admin: ListUserTokens via tokenManager for alice's newapiUserID → sees remaining token
//   8. Admin force-revoke: RevokeToken(adminForce=true) on last token → success (no last guard)
//   9. ListUserTokens(alice) → 0 active (all revoked / disabled)
func TestIntegration_FullTokenLifecycle(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	// alice: coaiUserID=42 → newapiUserID=7
	seedBindingForUser(t, db, 42, 7)

	ctx := context.Background()

	// ── Step 1: Create first token ────────────────────────────────────────────
	tok1, err := mgr.CreateUserToken(ctx, 42, CreateTokenRequest{
		Name: "primary", RemainQuota: 1000, ExpiredTime: -1,
	})
	if err != nil {
		t.Fatalf("step1 create primary: %v", err)
	}
	if tok1.Key == "" {
		t.Fatal("step1: plaintext key must be non-empty on create")
	}
	if strings.Contains(tok1.Key, "...") {
		t.Errorf("step1: create response must return plaintext key, not masked; got %q", tok1.Key)
	}
	primaryID := tok1.ID

	// ── Step 2: List → 1 token, masked ───────────────────────────────────────
	listed, err := mgr.ListUserTokens(ctx, 42)
	if err != nil {
		t.Fatalf("step2 list: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("step2: want 1 token, got %d", len(listed))
	}
	if listed[0].Key == tok1.Key {
		t.Error("step2: list returned plaintext key — plaintext-leak violation")
	}
	if !strings.Contains(listed[0].Key, "...") {
		t.Errorf("step2: masked key %q should contain '...'", listed[0].Key)
	}

	// ── Step 3: Fill to max (10) then fail ───────────────────────────────────
	// Already have 1 active; seed 9 more to reach limit.
	fake.seedTokens(7, 9)
	if fake.countActive(7) != 10 {
		t.Fatalf("step3 precondition: want 10 active, got %d", fake.countActive(7))
	}
	_, err = mgr.CreateUserToken(ctx, 42, CreateTokenRequest{Name: "overflow"})
	if !errors.Is(err, ErrMaxTokensReached) {
		t.Fatalf("step3: want ErrMaxTokensReached, got %v", err)
	}

	// ── Step 4: Revoke seeded tokens to get back to 1, then try last-token guard ──
	// Revoke the 9 seeded tokens (they're at indices [1..9] in fake.tokens after primary).
	// fake.tokens[0] = primary (tok1); [1..9] = seeded.
	for i := 1; i <= 9; i++ {
		if err := mgr.RevokeToken(ctx, 42, fake.tokens[i].id, false); err != nil {
			t.Fatalf("step4 revoke seeded[%d]: %v", i, err)
		}
	}
	if fake.countActive(7) != 1 {
		t.Fatalf("step4 precondition: want 1 active after revoking 9 seeded, got %d", fake.countActive(7))
	}
	// Now try to revoke the last token — should be blocked.
	err = mgr.RevokeToken(ctx, 42, primaryID, false)
	if !errors.Is(err, ErrCannotRevokeLastToken) {
		t.Fatalf("step4: want ErrCannotRevokeLastToken, got %v", err)
	}

	// ── Step 5: Create a second token ────────────────────────────────────────
	tok2, err := mgr.CreateUserToken(ctx, 42, CreateTokenRequest{
		Name: "secondary", RemainQuota: 500, ExpiredTime: -1,
	})
	if err != nil {
		t.Fatalf("step5 create secondary: %v", err)
	}
	secondaryID := tok2.ID
	if fake.countActive(7) != 2 {
		t.Fatalf("step5: want 2 active after second create, got %d", fake.countActive(7))
	}

	// ── Step 6: Revoke primary (user-side, non-forced) ────────────────────────
	if err := mgr.RevokeToken(ctx, 42, primaryID, false); err != nil {
		t.Fatalf("step6 revoke primary: %v", err)
	}
	if fake.countActive(7) != 1 {
		t.Fatalf("step6: want 1 active after revoking primary, got %d", fake.countActive(7))
	}

	// ── Step 7: Admin view — list via tokenManager (same client, newapiUserID) ──
	// tokenManager.ListUserTokens uses the binding to resolve newapiUserID, then
	// calls fake.listTokensForUser — same data source as admin would use.
	adminView, err := mgr.ListUserTokens(ctx, 42)
	if err != nil {
		t.Fatalf("step7 admin view list: %v", err)
	}
	// Should see both tokens (primary disabled, secondary active) — NewAPI returns all.
	foundSecondary := false
	for _, tok := range adminView {
		if tok.ID == secondaryID {
			foundSecondary = true
		}
	}
	if !foundSecondary {
		t.Errorf("step7: secondary token (id=%d) not visible in admin list", secondaryID)
	}

	// ── Step 8: Admin force-revoke last active token (no last-token guard) ────
	if err := mgr.RevokeToken(ctx, 42, secondaryID, true /*adminForce*/); err != nil {
		t.Fatalf("step8 admin force-revoke secondary: %v", err)
	}
	if fake.countActive(7) != 0 {
		t.Fatalf("step8: want 0 active after admin force-revoke, got %d", fake.countActive(7))
	}

	// ── Step 9: ListUserTokens reflects all-revoked state ─────────────────────
	finalList, err := mgr.ListUserTokens(ctx, 42)
	if err != nil {
		t.Fatalf("step9 final list: %v", err)
	}
	for _, tok := range finalList {
		if tok.Status == 1 {
			t.Errorf("step9: token id=%d still shows status=active after all-revoke", tok.ID)
		}
	}
}

// TestIntegration_MaxTokenBoundary verifies the exact 10-token boundary.
func TestIntegration_MaxTokenBoundary(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 10, 20)
	ctx := context.Background()

	// Seed 9 — one slot remains.
	fake.seedTokens(20, 9)
	tok, err := mgr.CreateUserToken(ctx, 10, CreateTokenRequest{
		Name: "tenth", RemainQuota: 100, ExpiredTime: -1,
	})
	if err != nil {
		t.Fatalf("10th token should succeed: %v", err)
	}
	_ = tok

	// 11th should fail.
	_, err = mgr.CreateUserToken(ctx, 10, CreateTokenRequest{Name: "eleventh"})
	if !errors.Is(err, ErrMaxTokensReached) {
		t.Fatalf("want ErrMaxTokensReached on 11th create, got %v", err)
	}
}

// TestIntegration_LastTokenGuard_AdminBypass verifies that adminForce=true
// bypasses the last-token guard, while a normal user call is blocked.
func TestIntegration_LastTokenGuard_AdminBypass(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 55, 88)
	ctx := context.Background()

	fake.seedTokens(88, 1)
	lastID := fake.tokens[0].id

	// User attempt — blocked.
	err := mgr.RevokeToken(ctx, 55, lastID, false)
	if !errors.Is(err, ErrCannotRevokeLastToken) {
		t.Fatalf("user revoke of last token: want ErrCannotRevokeLastToken, got %v", err)
	}

	// Admin force — succeeds.
	if err := mgr.RevokeToken(ctx, 55, lastID, true); err != nil {
		t.Fatalf("admin force-revoke of last token: %v", err)
	}
	if fake.countActive(88) != 0 {
		t.Errorf("after admin force-revoke: want 0 active, got %d", fake.countActive(88))
	}
}
