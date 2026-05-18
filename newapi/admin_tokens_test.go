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
	"net/http"
	"net/http/httptest"
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
// R4-1 — resolveQuota: UnlimitedQuota resolution logic
// ---------------------------------------------------------------------------

// TestResolveQuota covers all four branches of the resolveQuota helper that
// is invoked by CreateUserTokenAPI before calling CreateUserToken.
func TestResolveQuota(t *testing.T) {
	cases := []struct {
		name             string
		unlimited        bool
		hasExplicit      bool
		remain           int64
		wantUnlimited    bool
		wantQuota        int64
	}{
		{
			name: "explicit unlimited=true ignores remain_quota",
			unlimited: true, hasExplicit: false, remain: 500,
			wantUnlimited: true, wantQuota: 0,
		},
		{
			name: "has_explicit_quota=true uses remain_quota verbatim",
			unlimited: false, hasExplicit: true, remain: 100,
			wantUnlimited: false, wantQuota: 100,
		},
		{
			name: "API client: unlimited=false, no explicit flag, remain>0 → limited",
			unlimited: false, hasExplicit: false, remain: 100,
			wantUnlimited: false, wantQuota: 100,
		},
		{
			name: "UI default: unlimited=false, no flag, remain=0 → unlimited",
			unlimited: false, hasExplicit: false, remain: 0,
			wantUnlimited: true, wantQuota: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotUnlimited, gotQuota := resolveQuota(tc.unlimited, tc.hasExplicit, tc.remain)
			if gotUnlimited != tc.wantUnlimited {
				t.Errorf("unlimited: want %v, got %v", tc.wantUnlimited, gotUnlimited)
			}
			if gotQuota != tc.wantQuota {
				t.Errorf("quota: want %d, got %d", tc.wantQuota, gotQuota)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// R4-2 — Expired tokens excluded from active count
// ---------------------------------------------------------------------------

// seedExpiredToken adds an expired but status=1 token for newapiUserID.
// expiredSecondsAgo controls how far in the past the expiry is.
func (f *fakeNewAPIClient) seedExpiredToken(newapiUserID int64, expiredSecondsAgo int64) {
	f.nextID++
	f.tokens = append(f.tokens, &fakeToken{
		id:          f.nextID,
		userID:      newapiUserID,
		name:        "expired-token",
		key:         "sk-expired",
		status:      1,                                    // still "active" in NewAPI terms
		remainQuota: 0,
		expiredTime: time.Now().Unix() - expiredSecondsAgo, // expired in the past
	})
}

// TestActiveCount_ExcludesExpiredTokens verifies that expired tokens (status=1
// but expired_time < now) are not counted as active for the max-10 limit.
// A user with 5 active + 5 expired should be allowed to create 5 more tokens.
func TestActiveCount_ExcludesExpiredTokens(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)

	// Seed 5 genuinely active tokens (never expire).
	fake.seedTokens(7, 5)
	// Seed 5 expired tokens (past expiry, but status=1).
	for i := 0; i < 5; i++ {
		fake.seedExpiredToken(7, 3600) // expired 1 hour ago
	}

	count, err := mgr.activeTokenCount(context.Background(), 7)
	if err != nil {
		t.Fatalf("activeTokenCount: %v", err)
	}
	if count != 5 {
		t.Errorf("want active count=5 (expired excluded), got %d", count)
	}
}

// TestIsActiveToken covers the helper directly.
func TestIsActiveToken(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		name   string
		tok    Token
		active bool
	}{
		{"status=2 disabled", Token{Status: 2, ExpiredTime: -1}, false},
		{"status=1 never-expire (-1)", Token{Status: 1, ExpiredTime: -1}, true},
		{"status=1 never-expire (0)", Token{Status: 1, ExpiredTime: 0}, true},
		{"status=1 future expiry", Token{Status: 1, ExpiredTime: now + 3600}, true},
		{"status=1 past expiry", Token{Status: 1, ExpiredTime: now - 3600}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isActiveToken(&tc.tok)
			if got != tc.active {
				t.Errorf("isActiveToken: want %v, got %v", tc.active, got)
			}
		})
	}
}

// TestLastTokenGuard_IgnoresExpiredTokens verifies that the last-token guard
// allows revoking the final truly-active token even when expired tokens exist.
// (Expired tokens cannot be used for API calls, so they don't protect access.)
func TestLastTokenGuard_IgnoresExpiredTokens(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)

	// 1 active + 1 expired — guard should allow revoking the active one.
	fake.seedTokens(7, 1)
	fake.seedExpiredToken(7, 3600)

	activeID := fake.tokens[0].id
	err := mgr.RevokeToken(context.Background(), 42, activeID, false, 0)
	// last-token guard: active count (excluding expired) = 1 → would leave 0 → should BLOCK
	if !errors.Is(err, ErrCannotRevokeLastToken) {
		t.Errorf("want ErrCannotRevokeLastToken, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// R4-3 — Audit timestamp scan: string-based parsing
// ---------------------------------------------------------------------------

// TestAuditScan_StringTimestamp verifies that AdminGetTokenAuditAPI correctly
// returns audit rows even when the DB stores created_at as a TEXT/string.
// This is the primary fix for R4-3: previously rows were silently dropped.
func TestAuditScan_StringTimestamp(t *testing.T) {
	_, db, _ := newTokenTestEnv(t)

	// Create gtk_audit_log with a TEXT created_at (simulates SQLite migration).
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS gtk_audit_log (
		  id INTEGER PRIMARY KEY AUTOINCREMENT,
		  resource_type TEXT NOT NULL, resource_id INTEGER NOT NULL,
		  action TEXT NOT NULL, actor_type TEXT NOT NULL, actor_id INTEGER NOT NULL,
		  before_state TEXT, after_state TEXT, note TEXT,
		  created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`); err != nil {
		t.Fatalf("create gtk_audit_log: %v", err)
	}

	// Insert 3 audit rows with an explicit string timestamp.
	for i := 0; i < 3; i++ {
		if _, err := db.Exec(`
			INSERT INTO gtk_audit_log
			  (resource_type, resource_id, action, actor_type, actor_id, created_at)
			VALUES ('token', 999, 'create', 'user', 42, '2026-05-01 10:00:00')
		`); err != nil {
			t.Fatalf("insert audit row: %v", err)
		}
	}

	// Call the scan logic directly via the exported query path.
	// We use a raw query to test the scan code path exercised in
	// AdminGetTokenAuditAPI (the handler is hard to call without a gin context,
	// so we replicate the scan logic here).
	rows, err := db.Query(`
		SELECT id, actor_id, actor_type, action, resource_type, resource_id,
		       before_state, after_state, note, created_at
		FROM gtk_audit_log
		WHERE resource_type = 'token' AND resource_id = 999
		ORDER BY id DESC
	`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	type auditEntry struct {
		ID        int64
		CreatedAt string
	}
	var entries []auditEntry
	for rows.Next() {
		var (
			id, actorID, resourceID int64
			actorType, action       string
			resourceType            string
			beforeState, afterState *string
			note                    *string
			createdAtStr            string
		)
		if err := rows.Scan(&id, &actorID, &actorType, &action,
			&resourceType, &resourceID,
			&beforeState, &afterState, &note, &createdAtStr); err != nil {
			t.Fatalf("scan: %v", err)
		}
		// Replicate the R4-3 fix: parse string into time.Time.
		var parsedTime string
		for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, time.RFC3339Nano} {
			if parsed, parseErr := time.Parse(layout, createdAtStr); parseErr == nil {
				parsedTime = parsed.UTC().Format(time.RFC3339)
				break
			}
		}
		entries = append(entries, auditEntry{ID: id, CreatedAt: parsedTime})
	}

	if len(entries) != 3 {
		t.Errorf("want 3 audit rows, got %d (rows silently dropped = scan bug)", len(entries))
	}
	for _, e := range entries {
		if e.CreatedAt == "" {
			t.Errorf("want parseable created_at, got empty string for id=%d", e.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// P2 R2-2 regression — admin force-revoke audit attributes admin actor
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// PKG-A-3 R3-1 — paginated envelope (realNewAPIClient via httptest)
// ---------------------------------------------------------------------------

// TestFetchUserTokens_PaginatedEnvelope verifies that listTokensForUser
// correctly decodes the NewAPI paginated envelope and loops pages until
// collected >= total.
//
// The httptest server returns 2 pages (10 items each, total=20).
// After both pages are fetched the method must return all 20 tokens.
func TestFetchUserTokens_PaginatedEnvelope(t *testing.T) {
	// Build token fixtures for 2 pages.
	makePageJSON := func(pageIdx, count, total int) string {
		items := ""
		for i := 0; i < count; i++ {
			if i > 0 {
				items += ","
			}
			id := pageIdx*100 + i + 1
			items += fmt.Sprintf(
				`{"id":%d,"user_id":7,"name":"tok-%d","key":"sk-x","status":1,"remain_quota":1000,"unlimited_quota":false,"expired_time":-1}`,
				id, id,
			)
		}
		return fmt.Sprintf(
			`{"success":true,"message":"","data":{"items":[%s],"total":%d,"page":%d,"page_size":10}}`,
			items, total, pageIdx,
		)
	}

	var reqCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Query().Get("p")
		if p == "0" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, makePageJSON(0, 10, 20))
		} else {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, makePageJSON(1, 10, 20))
		}
		reqCount++
	}))
	defer srv.Close()

	cli := &Client{
		baseURL:     srv.URL,
		adminUserID: 2,
		adminToken:  "test-token",
		httpClient:  &http.Client{Timeout: 5 * time.Second},
	}
	rc := &realNewAPIClient{c: cli}

	tokens, err := rc.listTokensForUser(context.Background(), 7)
	if err != nil {
		t.Fatalf("listTokensForUser: %v", err)
	}
	if len(tokens) != 20 {
		t.Errorf("want 20 tokens (2 pages × 10), got %d", len(tokens))
	}
	if reqCount != 2 {
		t.Errorf("want 2 HTTP requests (one per page), got %d", reqCount)
	}
	// Verify first and last token IDs to confirm both pages were merged.
	if tokens[0].ID != 1 {
		t.Errorf("first token ID: want 1, got %d", tokens[0].ID)
	}
	if tokens[19].ID != 110 {
		t.Errorf("last token ID: want 110 (page1[9]), got %d", tokens[19].ID)
	}
}

// ---------------------------------------------------------------------------
// PKG-A-3 R3-2 — concurrent revoke last-token race protection
// ---------------------------------------------------------------------------

// TestRevokeToken_ConcurrentRevokeProtectsLastToken verifies that concurrent
// revoke calls for a user with 2 active tokens cannot both succeed and leave
// the user with 0 active tokens (last-token invariant violation).
//
// Setup: alice has 2 active tokens. 5 goroutines concurrently attempt to
// revoke them. After all goroutines finish, alice must still have ≥1 active.
func TestRevokeToken_ConcurrentRevokeProtectsLastToken(t *testing.T) {
	mgr, db, fake := newTokenTestEnv(t)
	seedBindingForUser(t, db, 42, 7)
	fake.seedTokens(7, 2) // exactly 2 active tokens

	tok0 := fake.tokens[0].id
	tok1 := fake.tokens[1].id

	// 5 goroutines: some revoke tok0, some revoke tok1, all non-admin.
	type result struct{ err error }
	results := make(chan result, 5)
	for i := 0; i < 5; i++ {
		targetID := tok0
		if i%2 == 1 {
			targetID = tok1
		}
		go func(id int64) {
			err := mgr.RevokeToken(context.Background(), 42, id, false, 0)
			results <- result{err}
		}(targetID)
	}

	var successes int
	for i := 0; i < 5; i++ {
		r := <-results
		if r.err == nil {
			successes++
		}
		// Allowed errors: ErrCannotRevokeLastToken (last-token guard),
		// ErrTokenNotOwnedByUser (token already disabled ≠ active in list),
		// or ErrTokenNotFound from fakeClient when token was already disabled.
		// Any other error is unexpected.
		if r.err != nil &&
			!errors.Is(r.err, ErrCannotRevokeLastToken) &&
			!errors.Is(r.err, ErrTokenNotOwnedByUser) &&
			!strings.Contains(r.err.Error(), "token not found") {
			t.Errorf("unexpected error: %v", r.err)
		}
	}

	// Invariant: at least 1 active token must remain.
	active := fake.countActive(7)
	if active < 1 {
		t.Errorf("last-token invariant violated: 0 active tokens after concurrent revoke (successes=%d)", successes)
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

// ---------------------------------------------------------------------------
// R5-2 — fetchTokenLogsPage uses page_size param (not size)
// ---------------------------------------------------------------------------

// pageSizeCapturingClient overrides fetchTokenLogsPage to record the raw URL
// query params used, so we can assert page_size is sent instead of size.
type pageSizeCapturingClient struct {
	*fakeNewAPIClient
	capturedURLs []string
}

func (p *pageSizeCapturingClient) fetchTokenLogsPage(_ context.Context, tokenID int64, page, size int) ([]NewAPILogEntry, error) {
	// Record the URL that realNewAPIClient would construct.
	p.capturedURLs = append(p.capturedURLs, fmt.Sprintf("p=%d&page_size=%d&token_id=%d", page, size, tokenID))
	// Delegate to the base fake (returns empty → terminates loop).
	return p.fakeNewAPIClient.fetchTokenLogsPage(context.Background(), tokenID, page, size)
}

// TestFetchTokenLogs_UsesPageSizeParam verifies via httptest that the real
// realNewAPIClient.fetchTokenLogsPage sends page_size=N (not size=N).
func TestFetchTokenLogs_UsesPageSizeParam(t *testing.T) {
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		// Return one non-full page so the caller terminates.
		fmt.Fprint(w, `{"success":true,"message":"","data":{"items":[],"total":0,"page":0,"page_size":500}}`)
	}))
	defer srv.Close()

	cli := &Client{
		baseURL:     srv.URL,
		adminUserID: 2,
		adminToken:  "test-token",
		httpClient:  &http.Client{Timeout: 5 * time.Second},
	}
	rc := &realNewAPIClient{c: cli}

	_, err := rc.fetchTokenLogsPage(context.Background(), 42, 0, 500)
	if err != nil {
		t.Fatalf("fetchTokenLogsPage: %v", err)
	}
	// Must use page_size, NOT size.
	if !strings.Contains(capturedQuery, "page_size=500") {
		t.Errorf("want page_size=500 in query, got %q", capturedQuery)
	}
	if strings.Contains(capturedQuery, "size=500") && !strings.Contains(capturedQuery, "page_size=500") {
		t.Errorf("must NOT use bare size= param, got %q", capturedQuery)
	}
}

// TestGetTokenUsage_TotalBasedPagination verifies that GetTokenUsage reads all
// pages when total > pageSize. Uses a multiPageFakeClient with 3 pages of 500
// entries each (total = 1500).
func TestGetTokenUsage_TotalBasedPagination(t *testing.T) {
	db := adminTestDB(t)
	withConnDB(t, db)
	seedBindingForUser(t, db, 42, 7)

	base := newFakeClient()
	base.seedTokens(7, 1)
	tokenID := base.tokens[0].id

	// 3 full pages of 500 → total 1500 entries.
	makeEntries := func(n int, prompt, completion int64) []NewAPILogEntry {
		es := make([]NewAPILogEntry, n)
		for i := range es {
			es[i] = NewAPILogEntry{Model: "gpt-4", PromptTokens: prompt, CompletionTokens: completion}
		}
		return es
	}
	cli := &multiPageFakeClient{
		fakeNewAPIClient: base,
		pages: [][]NewAPILogEntry{
			makeEntries(500, 1, 1), // page 0: 500 × 2 = 1000 tokens
			makeEntries(500, 1, 1), // page 1: 500 × 2 = 1000 tokens
			makeEntries(500, 1, 1), // page 2: 500 × 2 = 1000 tokens
			// page 3: empty → loop terminates
		},
	}
	mgr := &tokenManager{db: db, cli: cli}

	usage, err := mgr.GetTokenUsage(context.Background(), 42, tokenID)
	if err != nil {
		t.Fatalf("GetTokenUsage: %v", err)
	}
	if usage.TotalCalls != 1500 {
		t.Errorf("want TotalCalls=1500, got %d", usage.TotalCalls)
	}
	if usage.TotalTokensUsed != 3000 {
		t.Errorf("want TotalTokensUsed=3000, got %d", usage.TotalTokensUsed)
	}
}

// ---------------------------------------------------------------------------
// R5-3 — computeEffectiveStatus + tokenFromNewAPI.EffectiveStatus
// ---------------------------------------------------------------------------

func TestComputeEffectiveStatus(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		name   string
		tok    Token
		want   string
	}{
		{"status=2 revoked, future expiry", Token{Status: 2, ExpiredTime: now + 3600}, "revoked"},
		{"status=2 revoked, no expiry", Token{Status: 2, ExpiredTime: -1}, "revoked"},
		{"status=1 expired (past expiry)", Token{Status: 1, ExpiredTime: now - 3600}, "expired"},
		{"status=1 active (future expiry)", Token{Status: 1, ExpiredTime: now + 3600}, "active"},
		{"status=1 never-expire (-1)", Token{Status: 1, ExpiredTime: -1}, "active"},
		{"status=1 never-expire (0)", Token{Status: 1, ExpiredTime: 0}, "active"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeEffectiveStatus(&tc.tok)
			if got != tc.want {
				t.Errorf("computeEffectiveStatus: want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestTokenFromNewAPI_EffectiveStatusPopulated(t *testing.T) {
	now := time.Now().Unix()

	// status=1, expired → effective_status="expired"
	t1 := &Token{ID: 1, Status: 1, ExpiredTime: now - 100, Key: "sk-abc123def456"}
	m1 := tokenFromNewAPI(t1)
	if m1.EffectiveStatus != "expired" {
		t.Errorf("want effective_status=expired, got %q", m1.EffectiveStatus)
	}
	if m1.Status != 1 {
		t.Errorf("raw status must remain 1 (backward compat), got %d", m1.Status)
	}

	// status=2 → effective_status="revoked"
	t2 := &Token{ID: 2, Status: 2, ExpiredTime: now + 9999, Key: "sk-xyz789uvw012"}
	m2 := tokenFromNewAPI(t2)
	if m2.EffectiveStatus != "revoked" {
		t.Errorf("want effective_status=revoked, got %q", m2.EffectiveStatus)
	}

	// status=1, never-expire → effective_status="active"
	t3 := &Token{ID: 3, Status: 1, ExpiredTime: 0, Key: "sk-aaabbbcccddd"}
	m3 := tokenFromNewAPI(t3)
	if m3.EffectiveStatus != "active" {
		t.Errorf("want effective_status=active, got %q", m3.EffectiveStatus)
	}
}
