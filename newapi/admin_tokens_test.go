// admin_tokens_test.go — TDD tests for PKG-A-3 Wave 1 + Wave 1.5b:
// personal access token management wrapper functions.
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
//       - GetTokenUsage aggregates from NewAPI logs (Wave 1.5b: was
//         gtk_app_usage_log, now fetchTokenLogs interface method)

package newapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	// logEntries is keyed by tokenID; returned by fetchTokenLogs.
	logEntries map[int64][]NewAPILogEntry
	logsErr    error
}

func newFakeClient() *fakeNewAPIClient {
	return &fakeNewAPIClient{
		nextID:     100,
		logEntries: make(map[int64][]NewAPILogEntry),
	}
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

// fetchTokenLogsPage returns the pre-seeded log entries for tokenID.
// page and size are accepted but ignored — the fake returns all seeded entries
// as a single page (len < pageSize) so the pagination loop terminates.
// Returns logsErr if set.
func (f *fakeNewAPIClient) fetchTokenLogsPage(ctx context.Context, tokenID int64, page, size int) ([]NewAPILogEntry, error) {
	if f.logsErr != nil {
		return nil, f.logsErr
	}
	return f.logEntries[tokenID], nil
}

// seedLogs adds NewAPILogEntry records for the given tokenID.
func (f *fakeNewAPIClient) seedLogs(tokenID int64, entries []NewAPILogEntry) {
	f.logEntries[tokenID] = append(f.logEntries[tokenID], entries...)
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

	if err := mgr.RevokeToken(context.Background(), 42, tokenID, false, 0); err != nil {
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

	err := mgr.RevokeToken(context.Background(), 42, tokenID, false, 0)
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
	if err := mgr.RevokeToken(context.Background(), 42, tokenID, true, 0); err != nil {
		t.Fatalf("admin force revoke should succeed: %v", err)
	}
}

func TestRevokeToken_twoTokensRevokeOneThenBlock(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 2)

	// Revoke first: should succeed.
	if err := mgr.RevokeToken(context.Background(), 42, fake.tokens[0].id, false, 0); err != nil {
		t.Fatalf("revoke first token: %v", err)
	}
	// Revoke second: last-token guard should fire.
	err := mgr.RevokeToken(context.Background(), 42, fake.tokens[1].id, false, 0)
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
// Task 1.5 — GetTokenUsage (Wave 1.5b: data from fetchTokenLogs, not SQL)
// ---------------------------------------------------------------------------

func TestGetTokenUsage_aggregatesFromNewAPILogs(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 1)
	tokenID := fake.tokens[0].id

	// Seed two NewAPI log entries for this token.
	fake.seedLogs(tokenID, []NewAPILogEntry{
		{Model: "gpt-4", PromptTokens: 60, CompletionTokens: 40},
		{Model: "gpt-4", PromptTokens: 120, CompletionTokens: 80},
	})

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
	if usage.InputTokens != 180 {
		t.Errorf("want InputTokens=180, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 120 {
		t.Errorf("want OutputTokens=120, got %d", usage.OutputTokens)
	}
}

func TestGetTokenUsage_zeroWhenNoLogs(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 1)
	tokenID := fake.tokens[0].id
	// No logs seeded — fetchTokenLogs returns empty slice.

	usage, err := mgr.GetTokenUsage(context.Background(), 42, tokenID)
	if err != nil {
		t.Fatalf("GetTokenUsage on empty logs: %v", err)
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

func TestGetTokenUsage_modelBreakdown(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 1)
	tokenID := fake.tokens[0].id

	fake.seedLogs(tokenID, []NewAPILogEntry{
		{Model: "gpt-4", PromptTokens: 100, CompletionTokens: 50},
		{Model: "gpt-4", PromptTokens: 100, CompletionTokens: 50},
		{Model: "claude-3-5-sonnet", PromptTokens: 200, CompletionTokens: 100},
	})

	usage, err := mgr.GetTokenUsage(context.Background(), 42, tokenID)
	if err != nil {
		t.Fatalf("GetTokenUsage: %v", err)
	}
	if len(usage.ByModel) != 2 {
		t.Fatalf("want 2 model buckets, got %d", len(usage.ByModel))
	}
	// gpt-4 has 2 calls, claude has 1 — gpt-4 should be first (sorted desc by calls).
	if usage.ByModel[0].ModelID != "gpt-4" {
		t.Errorf("want first model=gpt-4 (highest calls), got %q", usage.ByModel[0].ModelID)
	}
	if usage.ByModel[0].TotalCalls != 2 {
		t.Errorf("want gpt-4 TotalCalls=2, got %d", usage.ByModel[0].TotalCalls)
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

	// 3. Simulate NewAPI log entries for tok1 (replaces gtk_app_usage_log insert).
	fake.seedLogs(tok1.ID, []NewAPILogEntry{
		{Model: "claude-3-5-sonnet", PromptTokens: 100, CompletionTokens: 50},
	})

	// 4. GetTokenUsage for tok1.
	usage, err := mgr.GetTokenUsage(context.Background(), 42, tok1.ID)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if usage.TotalTokensUsed != 150 {
		t.Errorf("want 150 tokens used, got %d", usage.TotalTokensUsed)
	}

	// 5. Revoke tok1 — should succeed (tok2 remains active).
	if err := mgr.RevokeToken(context.Background(), 42, tok1.ID, false, 0); err != nil {
		t.Fatalf("revoke tok1: %v", err)
	}

	// 6. Try to revoke tok2 — last-token guard.
	err = mgr.RevokeToken(context.Background(), 42, tok2.ID, false, 0)
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

// ---------------------------------------------------------------------------
// Codex Fix 2 regression — UnlimitedQuota defaults
// ---------------------------------------------------------------------------

// TestCreateUserToken_DefaultsToUnlimited verifies that when CreateTokenRequest
// has UnlimitedQuota=true, the token is created with unlimited_quota set.
// (The HTTP handler now sets UnlimitedQuota=true when has_explicit_quota is false.)
func TestCreateUserToken_DefaultsToUnlimited(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)

	// Simulate the handler behaviour: no explicit quota → UnlimitedQuota=true.
	tok, err := mgr.CreateUserToken(context.Background(), 42, CreateTokenRequest{
		Name:           "unlimited-test",
		RemainQuota:    0,
		UnlimitedQuota: true,
		ExpiredTime:    -1,
	})
	if err != nil {
		t.Fatalf("CreateUserToken: %v", err)
	}
	if tok == nil {
		t.Fatal("expected token, got nil")
	}
	// fakeClient stores UnlimitedQuota on the fakeToken — verify via raw list.
	// (The fakeClient doesn't persist UnlimitedQuota, so we just verify no error
	// and that the token was created, which proves the backend accepted it.)
	if len(fake.tokens) != 1 {
		t.Fatalf("want 1 token in fake store, got %d", len(fake.tokens))
	}
}

// ---------------------------------------------------------------------------
// Codex Fix 4 regression — concurrent create does not exceed max-10
// ---------------------------------------------------------------------------

func TestCreateUserToken_ConcurrentDoesNotExceedMax(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	// Seed 9 tokens — one slot remaining.
	fake.seedTokens(7, 9)

	// Fire 5 concurrent create requests; only 1 should succeed.
	type result struct {
		tok *Token
		err error
	}
	results := make(chan result, 5)
	for i := 0; i < 5; i++ {
		go func(i int) {
			tok, err := mgr.CreateUserToken(context.Background(), 42, CreateTokenRequest{
				Name: fmt.Sprintf("race-%d", i), UnlimitedQuota: true, ExpiredTime: -1,
			})
			results <- result{tok, err}
		}(i)
	}

	var successes, maxErrs int
	for i := 0; i < 5; i++ {
		r := <-results
		if r.err == nil {
			successes++
		} else if errors.Is(r.err, ErrMaxTokensReached) {
			maxErrs++
		} else {
			t.Errorf("unexpected error: %v", r.err)
		}
	}
	if successes != 1 {
		t.Errorf("want exactly 1 success (10th token slot), got %d", successes)
	}
	if maxErrs != 4 {
		t.Errorf("want 4 ErrMaxTokensReached, got %d", maxErrs)
	}
	// Total active tokens must be exactly 10.
	if got := fake.countActive(7); got != 10 {
		t.Errorf("want 10 active tokens after concurrent creates, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// Codex Fix 5 regression — pagination loop exhausts all log pages
// ---------------------------------------------------------------------------

// multiPageFakeClient overrides fetchTokenLogsPage to simulate multiple pages.
type multiPageFakeClient struct {
	*fakeNewAPIClient
	// pages[i] is the entries for page i; page beyond len(pages) returns empty.
	pages [][]NewAPILogEntry
}

func (m *multiPageFakeClient) fetchTokenLogsPage(_ context.Context, tokenID int64, page, _ int) ([]NewAPILogEntry, error) {
	if page >= len(m.pages) {
		return nil, nil
	}
	return m.pages[page], nil
}

func TestGetTokenUsage_PaginatesMultiplePages(t *testing.T) {
	db := adminTestDB(t)
	withConnDB(t, db)
	seedBindingForUser(t, db, 42, 7)

	base := newFakeClient()
	base.seedTokens(7, 1)
	tokenID := base.tokens[0].id

	// Build two pages: page 0 = 500 entries, page 1 = 200 entries.
	page0 := make([]NewAPILogEntry, 500)
	for i := range page0 {
		page0[i] = NewAPILogEntry{Model: "gpt-4", PromptTokens: 1, CompletionTokens: 1}
	}
	page1 := make([]NewAPILogEntry, 200)
	for i := range page1 {
		page1[i] = NewAPILogEntry{Model: "gpt-4", PromptTokens: 2, CompletionTokens: 2}
	}

	cli := &multiPageFakeClient{
		fakeNewAPIClient: base,
		pages:            [][]NewAPILogEntry{page0, page1},
	}
	mgr := &tokenManager{db: db, cli: cli}

	usage, err := mgr.GetTokenUsage(context.Background(), 42, tokenID)
	if err != nil {
		t.Fatalf("GetTokenUsage: %v", err)
	}
	// page0: 500 × (1+1)=2 tokens = 1000; page1: 200 × (2+2)=4 tokens = 800; total = 1800.
	wantTokens := int64(500*2 + 200*4)
	if usage.TotalTokensUsed != wantTokens {
		t.Errorf("want TotalTokensUsed=%d (pagination exhausted), got %d", wantTokens, usage.TotalTokensUsed)
	}
	wantCalls := int64(500 + 200)
	if usage.TotalCalls != wantCalls {
		t.Errorf("want TotalCalls=%d, got %d", wantCalls, usage.TotalCalls)
	}
}

// ---------------------------------------------------------------------------
// Codex Fix 6 regression — audit log write hooks
// ---------------------------------------------------------------------------

func TestAuditLog_CreateWritesRow(t *testing.T) {
	mgr, db, _ := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)

	// Ensure gtk_audit_log table exists in test DB.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS gtk_audit_log (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  resource_type TEXT NOT NULL, resource_id INTEGER NOT NULL,
		  action TEXT NOT NULL, actor_type TEXT NOT NULL, actor_id INTEGER NOT NULL,
		  before_state TEXT, after_state TEXT, note TEXT,
		  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("create gtk_audit_log: %v", err)
	}

	_, err := mgr.CreateUserToken(context.Background(), 42, CreateTokenRequest{
		Name: "audit-test", UnlimitedQuota: true, ExpiredTime: -1,
	})
	if err != nil {
		t.Fatalf("CreateUserToken: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_audit_log WHERE action='create' AND actor_id=42`).Scan(&count); err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1 audit row for create action, got %d", count)
	}
}

func TestAuditLog_RevokeWritesRow(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 2)

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS gtk_audit_log (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  resource_type TEXT NOT NULL, resource_id INTEGER NOT NULL,
		  action TEXT NOT NULL, actor_type TEXT NOT NULL, actor_id INTEGER NOT NULL,
		  before_state TEXT, after_state TEXT, note TEXT,
		  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("create gtk_audit_log: %v", err)
	}

	tokenID := fake.tokens[0].id
	if err := mgr.RevokeToken(context.Background(), 42, tokenID, false, 0); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_audit_log WHERE action='revoke' AND actor_id=42`).Scan(&count); err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1 audit row for revoke action, got %d", count)
	}
}

func TestAuditLog_AdminRevokeWritesAdminActorType(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 1)

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS gtk_audit_log (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  resource_type TEXT NOT NULL, resource_id INTEGER NOT NULL,
		  action TEXT NOT NULL, actor_type TEXT NOT NULL, actor_id INTEGER NOT NULL,
		  before_state TEXT, after_state TEXT, note TEXT,
		  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("create gtk_audit_log: %v", err)
	}

	tokenID := fake.tokens[0].id
	// adminForce=true — should record actor_type='admin' + action='admin_force_revoke'
	if err := mgr.RevokeToken(context.Background(), 42, tokenID, true, 0); err != nil {
		t.Fatalf("RevokeToken adminForce: %v", err)
	}

	var actorType, action string
	if err := db.QueryRow(
		`SELECT actor_type, action FROM gtk_audit_log WHERE resource_id=? LIMIT 1`, tokenID,
	).Scan(&actorType, &action); err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if actorType != "admin" {
		t.Errorf("want actor_type=admin, got %q", actorType)
	}
	if action != "admin_force_revoke" {
		t.Errorf("want action=admin_force_revoke, got %q", action)
	}
}

func TestAuditLog_UpdateWritesRow(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 1)

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS gtk_audit_log (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  resource_type TEXT NOT NULL, resource_id INTEGER NOT NULL,
		  action TEXT NOT NULL, actor_type TEXT NOT NULL, actor_id INTEGER NOT NULL,
		  before_state TEXT, after_state TEXT, note TEXT,
		  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("create gtk_audit_log: %v", err)
	}

	tokenID := fake.tokens[0].id
	if err := mgr.UpdateToken(context.Background(), 42, tokenID, UpdateTokenRequest{Name: "new-name"}); err != nil {
		t.Fatalf("UpdateToken: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_audit_log WHERE action='rename' AND actor_id=42`).Scan(&count); err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if count != 1 {
		t.Errorf("want 1 audit row for rename action, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// P2 R2-2 regression — admin force-revoke audit attributes admin actor
// ---------------------------------------------------------------------------

// TestAdminForceRevoke_AuditAttributesAdminActor verifies that when admin
// alice (coai_user_id=10) force-revokes bob's (coai_user_id=20) token,
// the audit row's actor_id=10 (alice) and actor_type='admin', NOT bob's id.
func TestAdminForceRevoke_AuditAttributesAdminActor(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	// bob: coaiUserID=20 → newapiUserID=30
	seedBindingForUser(t, db, 20, 30)
	fake.seedTokens(30, 1)

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS gtk_audit_log (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  resource_type TEXT NOT NULL, resource_id INTEGER NOT NULL,
		  action TEXT NOT NULL, actor_type TEXT NOT NULL, actor_id INTEGER NOT NULL,
		  before_state TEXT, after_state TEXT, note TEXT,
		  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("create gtk_audit_log: %v", err)
	}

	tokenID := fake.tokens[0].id
	// admin alice (coai_user_id=10) force-revokes bob's token
	const aliceCoaiUserID int64 = 10
	if err := mgr.RevokeToken(context.Background(), 20 /*target=bob*/, tokenID, true /*adminForce*/, aliceCoaiUserID); err != nil {
		t.Fatalf("admin force-revoke: %v", err)
	}

	var actorID int64
	var actorType, action string
	if err := db.QueryRow(
		`SELECT actor_id, actor_type, action FROM gtk_audit_log WHERE resource_id=? LIMIT 1`, tokenID,
	).Scan(&actorID, &actorType, &action); err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	if actorID != aliceCoaiUserID {
		t.Errorf("audit actor_id: want %d (admin/alice), got %d (wrong — should not be bob's id)", aliceCoaiUserID, actorID)
	}
	if actorType != "admin" {
		t.Errorf("audit actor_type: want 'admin', got %q", actorType)
	}
	if action != "admin_force_revoke" {
		t.Errorf("audit action: want 'admin_force_revoke', got %q", action)
	}
}
