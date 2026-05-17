// admin_tokens_test.go — TDD tests for PKG-A-3 Wave 1: personal access token
// management wrapper functions.
//
// Strategy:
//   - All functions under test use a fakeClient (satisfies tokenClientIface)
//     so no real HTTP roundtrips to NewAPI occur.
//   - SQLite in-memory DB is re-used from adminTestDB (admin_routing_test.go).
//   - Invariants tested explicitly:
//       - last-token guard: cannot revoke the last active token for a user
//       - max-10 limit: CreateUserToken returns ErrMaxTokensReached when
//         user already has 10 active tokens
//       - plaintext-leak prevention: ListUserTokens response must mask Key
//       - GetTokenUsage returns per-token aggregation from gtk_app_usage_log

package newapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// fakeClient — satisfies tokenClientIface without real HTTP
// ---------------------------------------------------------------------------

// fakeToken is a token record held by fakeClient.
type fakeToken struct {
	id          int64
	userID      int64
	name        string
	key         string
	status      int   // 1=active, 2=disabled
	remainQuota int64
	expiredTime int64
	createdAt   time.Time
}

// fakeNewAPIClient simulates NewAPI token operations.
type fakeNewAPIClient struct {
	tokens     []*fakeToken
	nextID     int64
	createErr  error
	listErr    error
	updateErr  error
	disableErr error
}

func newFakeClient() *fakeNewAPIClient {
	return &fakeNewAPIClient{nextID: 100}
}

func (f *fakeNewAPIClient) listTokensForUser(ctx context.Context, newapiUserID int64) ([]*Token, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*Token
	for _, t := range f.tokens {
		if t.userID == newapiUserID {
			out = append(out, &Token{
				ID:          t.id,
				UserID:      t.userID,
				Name:        t.name,
				Key:         t.key,
				Status:      t.status,
				RemainQuota: t.remainQuota,
				ExpiredTime: t.expiredTime,
			})
		}
	}
	return out, nil
}

func (f *fakeNewAPIClient) createToken(ctx context.Context, newapiUserID int64, req CreateTokenRequest) (*Token, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.nextID++
	key := "sk-test-" + req.Name + "-plaintext"
	tok := &fakeToken{
		id:          f.nextID,
		userID:      newapiUserID,
		name:        req.Name,
		key:         key,
		status:      1,
		remainQuota: req.RemainQuota,
		expiredTime: req.ExpiredTime,
		createdAt:   time.Now(),
	}
	f.tokens = append(f.tokens, tok)
	return &Token{
		ID:          tok.id,
		UserID:      tok.userID,
		Name:        tok.name,
		Key:         tok.key,
		Status:      tok.status,
		RemainQuota: tok.remainQuota,
		ExpiredTime: tok.expiredTime,
	}, nil
}

func (f *fakeNewAPIClient) updateToken(ctx context.Context, tokenID int64, req UpdateTokenRequest) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	for _, t := range f.tokens {
		if t.id == tokenID {
			if req.Name != "" {
				t.name = req.Name
			}
			if req.ExpiredTime != 0 {
				t.expiredTime = req.ExpiredTime
			}
			if req.RemainQuota != 0 {
				t.remainQuota = req.RemainQuota
			}
			return nil
		}
	}
	return ErrTokenNotFound
}

func (f *fakeNewAPIClient) disableToken(ctx context.Context, tokenID int64) error {
	if f.disableErr != nil {
		return f.disableErr
	}
	for _, t := range f.tokens {
		if t.id == tokenID {
			t.status = 2
			return nil
		}
	}
	return ErrTokenNotFound
}

// countActive returns how many tokens in fakeClient are active for a user.
func (f *fakeNewAPIClient) countActive(newapiUserID int64) int {
	n := 0
	for _, t := range f.tokens {
		if t.userID == newapiUserID && t.status == 1 {
			n++
		}
	}
	return n
}

// seedTokens adds n active tokens for newapiUserID into fakeClient.
func (f *fakeNewAPIClient) seedTokens(newapiUserID int64, n int) {
	for i := 0; i < n; i++ {
		f.nextID++
		f.tokens = append(f.tokens, &fakeToken{
			id:          f.nextID,
			userID:      newapiUserID,
			name:        "seed-token",
			key:         "sk-seed",
			status:      1,
			remainQuota: 1000,
			expiredTime: -1,
		})
	}
}

// ---------------------------------------------------------------------------
// Helper: build a tokenMgr with an in-memory DB and a binding for coaiUserID
// ---------------------------------------------------------------------------

func newTokenTestEnv(t *testing.T) (mgr *tokenManager, db *sql.DB, fake *fakeNewAPIClient) {
	t.Helper()
	db = adminTestDB(t) // from admin_routing_test.go — creates auth + gtk_newapi_binding
	withConnDB(t, db)

	// Seed gtk_app_usage_log table for GetTokenUsage tests.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS gtk_app_usage_log (
		  id          INTEGER PRIMARY KEY AUTOINCREMENT,
		  user_id     INTEGER NOT NULL,
		  token_id    INTEGER NOT NULL DEFAULT 0,
		  model_id    TEXT    NOT NULL DEFAULT '',
		  tokens_used INTEGER NOT NULL DEFAULT 0,
		  input_tokens  INTEGER NOT NULL DEFAULT 0,
		  output_tokens INTEGER NOT NULL DEFAULT 0,
		  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("create gtk_app_usage_log: %v", err)
	}

	fake = newFakeClient()
	mgr = &tokenManager{db: db, cli: fake}
	return
}

// seedBindingForUser inserts an auth row + gtk_newapi_binding for the given
// coaiUserID mapped to newapiUserID. Mirrors seedBinding from admin_routing_test.go.
func seedBindingForUser(t *testing.T, db *sql.DB, coaiUserID, newapiUserID int64) {
	t.Helper()
	// Insert auth row (ignore conflict — adminTestDB already has id=1).
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO auth (id, username, is_admin) VALUES (?, ?, 0)`,
		coaiUserID, "testuser",
	); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO gtk_newapi_binding
		  (coai_user_id, newapi_user_id, newapi_token_id, newapi_token_key, newapi_group, last_known_quota)
		 VALUES (?, ?, 0, 'sk-placeholder', 'default', 1000)`,
		coaiUserID, newapiUserID,
	); err != nil {
		t.Fatalf("seed binding: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Task 1.1 — ListUserTokens
// ---------------------------------------------------------------------------

func TestListUserTokens_returnsTokensForUser(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 2)

	toks, err := mgr.ListUserTokens(context.Background(), 42)
	if err != nil {
		t.Fatalf("ListUserTokens: %v", err)
	}
	if len(toks) != 2 {
		t.Fatalf("want 2 tokens, got %d", len(toks))
	}
}

func TestListUserTokens_noBindingReturnsError(t *testing.T) {
	mgr, _, _ := newTokenTestEnv(t)
	_, err := mgr.ListUserTokens(context.Background(), 999)
	if err == nil {
		t.Fatal("want error for unprovisioned user, got nil")
	}
}

// Task 1.1 + plaintext-leak prevention
func TestListUserTokens_keysAreMasked(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	// Add a token whose key contains a recognisable plaintext segment.
	fake.tokens = append(fake.tokens, &fakeToken{
		id: 200, userID: 7, name: "prod", key: "sk-test-super-secret-plaintext", status: 1,
	})

	toks, err := mgr.ListUserTokens(context.Background(), 42)
	if err != nil {
		t.Fatalf("ListUserTokens: %v", err)
	}
	if len(toks) != 1 {
		t.Fatalf("want 1 token, got %d", len(toks))
	}
	masked := toks[0].Key
	// Masked key must not be the full plaintext key.
	if masked == "sk-test-super-secret-plaintext" {
		t.Error("ListUserTokens returned plaintext key — plaintext-leak violation")
	}
	// Masked key should contain '...' (masking marker).
	if !strings.Contains(masked, "...") {
		t.Errorf("masked key %q should contain '...'", masked)
	}
}

// ---------------------------------------------------------------------------
// Task 1.2 — UpdateToken
// ---------------------------------------------------------------------------

func TestUpdateToken_renamesToken(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 1)
	tokenID := fake.tokens[0].id

	if err := mgr.UpdateToken(context.Background(), 42, tokenID, UpdateTokenRequest{Name: "renamed"}); err != nil {
		t.Fatalf("UpdateToken: %v", err)
	}
	if fake.tokens[0].name != "renamed" {
		t.Errorf("want name=renamed, got %q", fake.tokens[0].name)
	}
}

func TestUpdateToken_tokenNotOwnedByUser(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	// Token belongs to newapiUserID=99, not 7.
	fake.tokens = append(fake.tokens, &fakeToken{id: 501, userID: 99, name: "other", key: "sk-x", status: 1})

	err := mgr.UpdateToken(context.Background(), 42, 501, UpdateTokenRequest{Name: "hack"})
	if err == nil {
		t.Fatal("want error when token not owned by user")
	}
}

// ---------------------------------------------------------------------------
// Task 1.3 — RevokeToken (last-token guard)
// ---------------------------------------------------------------------------

func TestRevokeToken_successWhenMultipleActive(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 2)
	tokenID := fake.tokens[0].id

	if err := mgr.RevokeToken(context.Background(), 42, tokenID, false); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if fake.tokens[0].status != 2 {
		t.Error("token status should be 2 (disabled) after revoke")
	}
}

func TestRevokeToken_lastTokenGuard(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 1)
	tokenID := fake.tokens[0].id

	err := mgr.RevokeToken(context.Background(), 42, tokenID, false)
	if !errors.Is(err, ErrCannotRevokeLastToken) {
		t.Fatalf("want ErrCannotRevokeLastToken, got %v", err)
	}
}

func TestRevokeToken_adminForceRevokeBypasses(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 1)
	tokenID := fake.tokens[0].id

	// adminForce=true should bypass last-token guard.
	if err := mgr.RevokeToken(context.Background(), 42, tokenID, true); err != nil {
		t.Fatalf("admin force revoke should succeed: %v", err)
	}
}

func TestRevokeToken_twoTokensRevokeOneThenBlock(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 2)

	// Revoke first: should succeed.
	if err := mgr.RevokeToken(context.Background(), 42, fake.tokens[0].id, false); err != nil {
		t.Fatalf("revoke first token: %v", err)
	}
	// Revoke second: last-token guard should fire.
	err := mgr.RevokeToken(context.Background(), 42, fake.tokens[1].id, false)
	if !errors.Is(err, ErrCannotRevokeLastToken) {
		t.Fatalf("want ErrCannotRevokeLastToken after revoking to 1, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Task 1.4 — CreateUserToken (max-10 limit + one-time plaintext)
// ---------------------------------------------------------------------------

func TestCreateUserToken_succeeds(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 2)

	tok, err := mgr.CreateUserToken(context.Background(), 42, CreateTokenRequest{
		Name: "my-key", RemainQuota: 500, ExpiredTime: -1,
	})
	if err != nil {
		t.Fatalf("CreateUserToken: %v", err)
	}
	if tok.Key == "" {
		t.Error("plaintext key should be returned on create")
	}
	// Key must not be masked.
	if strings.Contains(tok.Key, "...") {
		t.Error("create response must return full plaintext key, not masked")
	}
}

func TestCreateUserToken_maxLimitReached(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	_ = fake // only used for seedTokens side-effect
	fake.seedTokens(7, 10) // exactly at limit

	_, err := mgr.CreateUserToken(context.Background(), 42, CreateTokenRequest{Name: "overflow"})
	if !errors.Is(err, ErrMaxTokensReached) {
		t.Fatalf("want ErrMaxTokensReached, got %v", err)
	}
}

func TestCreateUserToken_ninthDoesNotBlock(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 9) // 9 existing — room for one more

	_, err := mgr.CreateUserToken(context.Background(), 42, CreateTokenRequest{
		Name: "ninth", RemainQuota: 100, ExpiredTime: -1,
	})
	if err != nil {
		t.Fatalf("CreateUserToken with 9 existing should succeed: %v", err)
	}
}

// Task 1.4 — plaintext-leak prevention (list does NOT expose plaintext)
func TestListUserTokens_doesNotLeakPlaintextAfterCreate(t *testing.T) {
	mgr, db, _ := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)

	// Create a token — response should have full key.
	created, err := mgr.CreateUserToken(context.Background(), 42, CreateTokenRequest{
		Name: "leak-test", RemainQuota: 100, ExpiredTime: -1,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	plaintextKey := created.Key

	// Subsequent list must NOT return the plaintext key.
	listed, err := mgr.ListUserTokens(context.Background(), 42)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, tok := range listed {
		if tok.Key == plaintextKey {
			t.Errorf("list response exposed plaintext key — leak violation: %q", tok.Key)
		}
	}
}

// ---------------------------------------------------------------------------
// Task 1.5 — GetTokenUsage
// ---------------------------------------------------------------------------

func TestGetTokenUsage_aggregatesFromUsageLog(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 1)
	tokenID := fake.tokens[0].id

	// Seed two usage rows for this token.
	if _, err := db.Exec(`
		INSERT INTO gtk_app_usage_log (user_id, token_id, model_id, tokens_used, input_tokens, output_tokens)
		VALUES (42, ?, 'gpt-4', 100, 60, 40),
		       (42, ?, 'gpt-4', 200, 120, 80)
	`, tokenID, tokenID); err != nil {
		t.Fatalf("seed usage log: %v", err)
	}

	usage, err := mgr.GetTokenUsage(context.Background(), 42, tokenID)
	if err != nil {
		t.Fatalf("GetTokenUsage: %v", err)
	}
	if usage.TotalTokensUsed != 300 {
		t.Errorf("want TotalTokensUsed=300, got %d", usage.TotalTokensUsed)
	}
	if usage.TotalCalls != 2 {
		t.Errorf("want TotalCalls=2, got %d", usage.TotalCalls)
	}
}

func TestGetTokenUsage_zeroWhenNoRows(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 1)
	tokenID := fake.tokens[0].id

	usage, err := mgr.GetTokenUsage(context.Background(), 42, tokenID)
	if err != nil {
		t.Fatalf("GetTokenUsage on empty log: %v", err)
	}
	if usage.TotalTokensUsed != 0 || usage.TotalCalls != 0 {
		t.Errorf("want zeros, got %+v", usage)
	}
}

func TestGetTokenUsage_tokenNotOwnedReturnsError(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	// tokenID 999 belongs to newapiUserID=99, not to user 42.
	fake.tokens = append(fake.tokens, &fakeToken{id: 999, userID: 99, status: 1, key: "sk-x", name: "x"})

	_, err := mgr.GetTokenUsage(context.Background(), 42, 999)
	if err == nil {
		t.Fatal("want error when token not owned by user")
	}
}

// ---------------------------------------------------------------------------
// Task 1.7 — Full lifecycle integration test
// ---------------------------------------------------------------------------

func TestTokenLifecycle_createListUseRevoke(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)

	// 1. Create two tokens.
	tok1, err := mgr.CreateUserToken(context.Background(), 42, CreateTokenRequest{
		Name: "prod", RemainQuota: 1000, ExpiredTime: -1,
	})
	if err != nil {
		t.Fatalf("create tok1: %v", err)
	}
	tok2, err := mgr.CreateUserToken(context.Background(), 42, CreateTokenRequest{
		Name: "dev", RemainQuota: 500, ExpiredTime: -1,
	})
	if err != nil {
		t.Fatalf("create tok2: %v", err)
	}

	// 2. List — should see 2 tokens, both masked.
	listed, err := mgr.ListUserTokens(context.Background(), 42)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("want 2 tokens, got %d", len(listed))
	}
	for _, tok := range listed {
		if tok.Key == tok1.Key || tok.Key == tok2.Key {
			t.Errorf("list exposed plaintext key — leak violation: %q", tok.Key)
		}
	}

	// 3. Simulate usage for tok1.
	if _, err := db.Exec(`
		INSERT INTO gtk_app_usage_log (user_id, token_id, model_id, tokens_used, input_tokens, output_tokens)
		VALUES (42, ?, 'claude-3-5-sonnet', 150, 100, 50)
	`, tok1.ID); err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	// 4. GetTokenUsage for tok1.
	usage, err := mgr.GetTokenUsage(context.Background(), 42, tok1.ID)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if usage.TotalTokensUsed != 150 {
		t.Errorf("want 150 tokens used, got %d", usage.TotalTokensUsed)
	}

	// 5. Revoke tok1 — should succeed (tok2 remains active).
	if err := mgr.RevokeToken(context.Background(), 42, tok1.ID, false); err != nil {
		t.Fatalf("revoke tok1: %v", err)
	}

	// 6. Try to revoke tok2 — last-token guard.
	err = mgr.RevokeToken(context.Background(), 42, tok2.ID, false)
	if !errors.Is(err, ErrCannotRevokeLastToken) {
		t.Fatalf("want ErrCannotRevokeLastToken, got %v", err)
	}

	// 7. List again — still 2 tokens (one disabled).
	listed2, err := mgr.ListUserTokens(context.Background(), 42)
	if err != nil {
		t.Fatalf("list2: %v", err)
	}
	// tok1 is disabled (status=2) but still visible.
	_ = listed2
	if fake.countActive(7) != 1 {
		t.Errorf("want 1 active token after revoke, got %d", fake.countActive(7))
	}
}
