package service

import (
	"chat/globals"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// seedRunnableOrder seeds an order owned by userID, in 'paid' status,
// linked to xhs-copy-writer agent (which seedRefundFixture also creates
// indirectly — but here we explicitly seed the agent + service to keep
// each test self-contained).
func seedRunnableOrder(t *testing.T, db *sql.DB, orderNo string, userID int64, creditsGranted int) {
	t.Helper()
	_, _ = globals.ExecDb(db, `
		INSERT INTO gtk_agent (slug, name, system_prompt, preferred_model, status, min_tier)
		VALUES ('xhs-copy-writer', 'XHS', 'You are an agent.', 'deepseek-chat', 'active', 'light')
	`)
	_, _ = globals.ExecDb(db, `
		INSERT INTO gtk_service (slug, name, category, agent_slug,
			price_cny_cents, included_credits, billing_type, status)
		VALUES ('xhs-single-post', 'Single Post', 'diy_agent', 'xhs-copy-writer',
		        1900, 60, 'one_time', 'active')
	`)
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_service_order
			(order_no, coai_user_id, service_id, service_slug,
			 price_cny_cents_paid, credits_granted, payment_provider, status)
		VALUES (?, ?, 1, 'xhs-single-post', 1900, ?, 'lemonsqueezy', 'paid')
	`, orderNo, userID, creditsGranted); err != nil {
		t.Fatalf("seed order: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────
// loadRunnableOrder
// ─────────────────────────────────────────────────────────────────────

func TestLoadRunnableOrder_HappyPath(t *testing.T) {
	db := setupTestDB(t)
	seedRunnableOrder(t, db, "SVC-RUN1", 42, 60)

	order, agent, err := loadRunnableOrder(db, "SVC-RUN1", 42)
	if err != nil {
		t.Fatalf("loadRunnableOrder: %v", err)
	}
	if order.OrderNo != "SVC-RUN1" || order.CreditsGranted != 60 {
		t.Errorf("order data wrong: %+v", order)
	}
	if agent.Slug != "xhs-copy-writer" {
		t.Errorf("agent: %+v", agent)
	}
}

func TestLoadRunnableOrder_RejectsWrongOwner(t *testing.T) {
	db := setupTestDB(t)
	seedRunnableOrder(t, db, "SVC-OWN", 42, 60)

	_, _, err := loadRunnableOrder(db, "SVC-OWN", 99)
	if !errors.Is(err, errOrderNotOwned) {
		t.Errorf("want errOrderNotOwned, got %v", err)
	}
}

func TestLoadRunnableOrder_RejectsAlreadyRan(t *testing.T) {
	db := setupTestDB(t)
	seedRunnableOrder(t, db, "SVC-RAN", 1, 60)
	_, _ = globals.ExecDb(db,
		`UPDATE gtk_service_order SET agent_run_id = 'old-run' WHERE order_no = 'SVC-RAN'`)

	_, _, err := loadRunnableOrder(db, "SVC-RAN", 1)
	if !errors.Is(err, errOrderNotRunnable) {
		t.Errorf("want errOrderNotRunnable, got %v", err)
	}
}

func TestLoadRunnableOrder_RejectsRefunded(t *testing.T) {
	db := setupTestDB(t)
	seedRunnableOrder(t, db, "SVC-RFND", 1, 60)
	_, _ = globals.ExecDb(db,
		`UPDATE gtk_service_order SET status = 'refunded' WHERE order_no = 'SVC-RFND'`)

	_, _, err := loadRunnableOrder(db, "SVC-RFND", 1)
	if !errors.Is(err, errOrderTerminal) {
		t.Errorf("want errOrderTerminal, got %v", err)
	}
}

func TestLoadRunnableOrder_NotFound(t *testing.T) {
	db := setupTestDB(t)
	_, _, err := loadRunnableOrder(db, "SVC-NOPE", 1)
	if !errors.Is(err, ErrOrderNotFound) {
		t.Errorf("want ErrOrderNotFound, got %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────
// acquireRunLock + releaseRunLock — concurrency contract
// ─────────────────────────────────────────────────────────────────────

func TestAcquireRunLock_HappyPath(t *testing.T) {
	db := setupTestDB(t)
	seedRunnableOrder(t, db, "SVC-LOCK", 1, 60)

	if err := acquireRunLock(db, "SVC-LOCK", "run-1"); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	var status, runID string
	if err := db.QueryRow(`SELECT status, agent_run_id FROM gtk_service_order WHERE order_no = ?`,
		"SVC-LOCK").Scan(&status, &runID); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "running" || runID != "run-1" {
		t.Errorf("after acquire: status=%s runID=%s", status, runID)
	}
}

func TestAcquireRunLock_FailsOnDoubleAcquire(t *testing.T) {
	db := setupTestDB(t)
	seedRunnableOrder(t, db, "SVC-DBL", 1, 60)

	if err := acquireRunLock(db, "SVC-DBL", "run-1"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := acquireRunLock(db, "SVC-DBL", "run-2"); err == nil {
		t.Error("second acquire should fail (already locked)")
	}
}

func TestReleaseRunLock_RestoresStatus(t *testing.T) {
	db := setupTestDB(t)
	seedRunnableOrder(t, db, "SVC-REL", 1, 60)

	_ = acquireRunLock(db, "SVC-REL", "run-1")
	releaseRunLock(db, "SVC-REL", "run-1")

	var status string
	var runID sql.NullString
	if err := db.QueryRow(`SELECT status, agent_run_id FROM gtk_service_order WHERE order_no = ?`,
		"SVC-REL").Scan(&status, &runID); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "paid" {
		t.Errorf("status after release: %s, want paid", status)
	}
	if runID.Valid {
		t.Errorf("agent_run_id should be NULL after release, got %v", runID.String)
	}
}

func TestReleaseRunLock_DoesNotReleaseOthersLock(t *testing.T) {
	db := setupTestDB(t)
	seedRunnableOrder(t, db, "SVC-OTH", 1, 60)
	_ = acquireRunLock(db, "SVC-OTH", "run-1")

	// Try to release with a different runID. Should be no-op.
	releaseRunLock(db, "SVC-OTH", "different-run-id")

	var status, runID string
	if err := db.QueryRow(`SELECT status, agent_run_id FROM gtk_service_order WHERE order_no = ?`,
		"SVC-OTH").Scan(&status, &runID); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "running" || runID != "run-1" {
		t.Errorf("foreign release should be no-op: status=%s runID=%s", status, runID)
	}
}

// ─────────────────────────────────────────────────────────────────────
// computeRunCredits — credit math
// ─────────────────────────────────────────────────────────────────────

func TestComputeRunCredits_TierMultipliers(t *testing.T) {
	cases := []struct {
		tier   string
		tokens int
		want   int
	}{
		{"light", 1500, 1},          // 1500 × 0.5 / 1500 = 0.5 → ceil = 1
		{"standard", 1500, 1},       // 1500 × 1.0 / 1500 = 1.0 = 1
		{"premium", 1500, 3},        // 1500 × 3.0 / 1500 = 3.0 = 3
		{"light", 0, 0},             // edge: zero tokens
		{"premium", 1, 1},           // edge: 1 token, premium → ceil(0.002) = 1
		{"unknown_tier", 3000, 2},   // unknown defaults to standard (1.0)
		{"standard", 1000, 1},       // 1000/1500 = 0.67 → ceil = 1
	}
	for _, c := range cases {
		got := computeRunCredits(c.tier, c.tokens)
		if got != c.want {
			t.Errorf("computeRunCredits(%s, %d) = %d; want %d", c.tier, c.tokens, got, c.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────
// finalizeRun
// ─────────────────────────────────────────────────────────────────────

// ensureUsageLogTable seeds gtk_app_usage_log into the test SQLite DB.
// commerce.ComputeAndWriteUsageCost writes here; setupTestDB only runs
// service.Migrate, so without this helper the cost-ledger INSERT would
// fail "no such table". Mirrors the column shape of commerce/cost_ledger.go's
// INSERT — keep in sync if columns are added in a future PKG.
func ensureUsageLogTable(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS gtk_app_usage_log (
		  id           INTEGER PRIMARY KEY AUTOINCREMENT,
		  user_id      INTEGER NOT NULL,
		  plan_id      INTEGER,
		  service      TEXT    NOT NULL,
		  source       TEXT    NOT NULL DEFAULT 'chat'
		                CHECK (source IN ('chat','api','service_order','admin_test')),
		  order_id     TEXT,
		  provider     TEXT,
		  tokens_used  INTEGER NOT NULL DEFAULT 0,
		  cost_cents   INTEGER NOT NULL DEFAULT 0,
		  created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		t.Fatalf("seed gtk_app_usage_log: %v", err)
	}
}

func TestFinalizeRun_HappyPath(t *testing.T) {
	db := setupTestDB(t)
	ensureUsageLogTable(t, db)
	seedRunnableOrder(t, db, "SVC-FIN", 1, 60)
	_ = acquireRunLock(db, "SVC-FIN", "run-fin")

	order := &ServiceOrder{OrderNo: "SVC-FIN", CoaiUserID: 1, CreditsGranted: 60}
	run := agentRunResult{
		Output:    "ok",
		Credits:   5,
		Model:     "deepseek-chat",
		TokensIn:  100,
		TokensOut: 200,
	}
	remaining, err := finalizeRun(db, order, "run-fin", run)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if remaining != 55 {
		t.Errorf("remaining = %d, want 55", remaining)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM gtk_service_order WHERE order_no = ?`,
		"SVC-FIN").Scan(&status); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "completed" {
		t.Errorf("status: %s, want completed", status)
	}
}

func TestFinalizeRun_HandlesOverrunGracefully(t *testing.T) {
	db := setupTestDB(t)
	ensureUsageLogTable(t, db)
	seedRunnableOrder(t, db, "SVC-OVR", 1, 10)
	_ = acquireRunLock(db, "SVC-OVR", "run-ovr")

	order := &ServiceOrder{OrderNo: "SVC-OVR", CoaiUserID: 1, CreditsGranted: 10}
	run := agentRunResult{
		Output:    "ok",
		Credits:   50, // 50 > 10 granted
		Model:     "deepseek-chat",
		TokensIn:  1000,
		TokensOut: 2000,
	}
	remaining, err := finalizeRun(db, order, "run-ovr", run)
	if err != nil {
		t.Fatalf("finalize should not error on overrun (logs warning instead): %v", err)
	}
	if remaining != 0 {
		t.Errorf("remaining on overrun should clamp to 0, got %d", remaining)
	}

	var status string
	if err := db.QueryRow(`SELECT status FROM gtk_service_order WHERE order_no = ?`,
		"SVC-OVR").Scan(&status); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "completed" {
		t.Errorf("overrun should still complete: status = %s", status)
	}
}

// TestFinalizeRun_WritesUsageCost — Wave 4 D1 spec: happy path includes
// a per-call cost write to gtk_app_usage_log keyed by source='service_order'
// + order_id=order.OrderNo. Verifies the row lands and the looked-up
// provider matches commerce/pricing.go.
func TestFinalizeRun_WritesUsageCost(t *testing.T) {
	db := setupTestDB(t)
	ensureUsageLogTable(t, db)
	seedRunnableOrder(t, db, "SVC-LEDGER", 42, 60)
	_ = acquireRunLock(db, "SVC-LEDGER", "run-ledger")

	order := &ServiceOrder{
		OrderNo:        "SVC-LEDGER",
		CoaiUserID:     42,
		CreditsGranted: 60,
	}
	run := agentRunResult{
		Output:    "test output",
		Credits:   1,
		Model:     "deepseek-chat", // known in pricing.go (provider=deepseek)
		TokensIn:  1000,
		TokensOut: 2000,
	}
	if _, err := finalizeRun(db, order, "run-ledger", run); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	// Verify the usage row landed with the right shape.
	var (
		userID     int64
		service    string
		source     string
		orderID    sql.NullString
		provider   sql.NullString
		tokensUsed int64
		costCents  int64
	)
	err := db.QueryRow(`
		SELECT user_id, service, source, order_id, provider, tokens_used, cost_cents
		FROM gtk_app_usage_log
		WHERE order_id = ? AND source = 'service_order'
	`, "SVC-LEDGER").Scan(&userID, &service, &source, &orderID, &provider, &tokensUsed, &costCents)
	if err != nil {
		t.Fatalf("usage row not found: %v", err)
	}
	if userID != 42 {
		t.Errorf("user_id = %d, want 42", userID)
	}
	if service != "deepseek-chat" {
		t.Errorf("service = %q, want deepseek-chat", service)
	}
	if source != "service_order" {
		t.Errorf("source = %q, want service_order", source)
	}
	if !orderID.Valid || orderID.String != "SVC-LEDGER" {
		t.Errorf("order_id = %v, want SVC-LEDGER", orderID)
	}
	if !provider.Valid || provider.String != "deepseek" {
		t.Errorf("provider = %v, want deepseek (auto-stamped from pricing)", provider)
	}
	if tokensUsed != 3000 {
		t.Errorf("tokens_used = %d, want 3000 (1000 in + 2000 out)", tokensUsed)
	}
	// deepseek-chat: 14 µ¢/1k input, 28 µ¢/1k output.
	// = (14 * 1000 / 1000) + (28 * 2000 / 1000) = 14 + 56 = 70 µ¢
	// 70 µ¢ / 1000 = 0 cents (sub-cent rounds down — pricing.go documented behavior)
	if costCents != 0 {
		t.Errorf("cost_cents = %d, want 0 (sub-cent rounds down per pricing.go)", costCents)
	}
}

// TestFinalizeRun_RefundDuringRun_NoUsageWrite — Wave 4 D1 CR4 contract:
// if a concurrent refund flips the order to canceled_mid_flight before
// finalizeRun can CAS to completed, the CAS returns false + finalizeRun
// must skip the usage write (don't bill a refunded run).
func TestFinalizeRun_RefundDuringRun_NoUsageWrite(t *testing.T) {
	db := setupTestDB(t)
	ensureUsageLogTable(t, db)
	seedRunnableOrder(t, db, "SVC-RACE", 42, 60)
	_ = acquireRunLock(db, "SVC-RACE", "run-race")

	// Simulate concurrent refund: a Revoke flipped status to
	// canceled_mid_flight before finalizeRun ran.
	if _, err := globals.ExecDb(db,
		`UPDATE gtk_service_order SET status = 'canceled_mid_flight' WHERE order_no = ?`,
		"SVC-RACE"); err != nil {
		t.Fatalf("simulate refund: %v", err)
	}

	order := &ServiceOrder{
		OrderNo:        "SVC-RACE",
		CoaiUserID:     42,
		CreditsGranted: 60,
	}
	run := agentRunResult{
		Output:    "delivered output",
		Credits:   1,
		Model:     "deepseek-chat",
		TokensIn:  1000,
		TokensOut: 2000,
	}
	// Should NOT error — the contract documents this as "log + skip" not
	// "fail loud".
	if _, err := finalizeRun(db, order, "run-race", run); err != nil {
		t.Fatalf("finalize on race should not error: %v", err)
	}

	// Status MUST still be canceled_mid_flight (CAS lost the race).
	var status string
	if err := db.QueryRow(`SELECT status FROM gtk_service_order WHERE order_no = ?`,
		"SVC-RACE").Scan(&status); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if status != "canceled_mid_flight" {
		t.Errorf("status = %q, want canceled_mid_flight (CAS should not have flipped)", status)
	}

	// Usage row MUST NOT have been written.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_app_usage_log WHERE order_id = ?`,
		"SVC-RACE").Scan(&count); err != nil {
		t.Fatalf("count usage rows: %v", err)
	}
	if count != 0 {
		t.Errorf("usage rows for refunded run = %d, want 0 (must not bill refunded run)", count)
	}
}

// TestFinalizeRun_UnknownModel_StillWritesAuditRow — Wave 4 D1 + Q6
// Option A: an unknown model (not in commerce/pricing.go) still gets a
// row written to gtk_app_usage_log with cost_cents=0 + a warning log.
// Caller's flow does NOT fail because pricing.go lacks an entry — keeps
// the call audit intact while operators add the new model.
func TestFinalizeRun_UnknownModel_StillWritesAuditRow(t *testing.T) {
	db := setupTestDB(t)
	ensureUsageLogTable(t, db)
	seedRunnableOrder(t, db, "SVC-UNK", 42, 60)
	_ = acquireRunLock(db, "SVC-UNK", "run-unk")

	order := &ServiceOrder{
		OrderNo:        "SVC-UNK",
		CoaiUserID:     42,
		CreditsGranted: 60,
	}
	run := agentRunResult{
		Output:    "test output",
		Credits:   1,
		Model:     "future-model-not-in-pricing-table",
		TokensIn:  100,
		TokensOut: 200,
	}
	if _, err := finalizeRun(db, order, "run-unk", run); err != nil {
		t.Fatalf("finalize on unknown model should not error: %v", err)
	}

	// Audit row exists with cost_cents=0 + provider NULL.
	var (
		costCents int64
		provider  sql.NullString
	)
	err := db.QueryRow(`
		SELECT cost_cents, provider FROM gtk_app_usage_log WHERE order_id = ?
	`, "SVC-UNK").Scan(&costCents, &provider)
	if err != nil {
		t.Fatalf("audit row missing for unknown model (must still write): %v", err)
	}
	if costCents != 0 {
		t.Errorf("cost_cents = %d, want 0 for unknown model", costCents)
	}
	if provider.Valid {
		t.Errorf("provider should be NULL for unknown model, got %q", provider.String)
	}
}

// ─────────────────────────────────────────────────────────────────────
// executeAgent — upstream NewAPI integration
// ─────────────────────────────────────────────────────────────────────

func TestExecuteAgent_HappyPath_AgainstFakeNewAPI(t *testing.T) {
	// Fake NewAPI returns a canned chat completion.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header.
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			http.Error(w, "missing bearer", 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"choices": [{"message": {"content": "今日大理多云转晴..."}}],
			"usage": {"prompt_tokens": 100, "completion_tokens": 200}
		}`))
	}))
	defer server.Close()

	viper.Set("service.runner_api_key", "test-runner-key")
	viper.Set("service.runner_endpoint", server.URL)
	t.Cleanup(func() {
		viper.Set("service.runner_api_key", "")
		viper.Set("service.runner_endpoint", "")
	})

	prev := agentRunHTTPClient
	agentRunHTTPClient = &http.Client{Timeout: 2 * time.Second}
	t.Cleanup(func() { agentRunHTTPClient = prev })

	agent := &Agent{
		Slug:           "xhs-copy-writer",
		PreferredModel: "deepseek-chat",
		MinTier:        "light",
		SystemPrompt:   "你是民宿小红书内容生成器。",
	}
	run, err := executeAgent(context.Background(), agent, "今天天气很好，请写一篇")
	if err != nil {
		t.Fatalf("executeAgent: %v", err)
	}
	if run.Output != "今日大理多云转晴..." {
		t.Errorf("output: %q", run.Output)
	}
	// 100+200 = 300 tokens × 0.5 (light) / 1500 = 0.1 → ceil = 1 credit
	if run.Credits != 1 {
		t.Errorf("credits: %d, want 1", run.Credits)
	}
	// Wave 4 D1: tokens + model are now surfaced for cost ledger.
	if run.TokensIn != 100 || run.TokensOut != 200 {
		t.Errorf("token split: in=%d out=%d, want 100/200", run.TokensIn, run.TokensOut)
	}
	// Upstream didn't report `model` in the canned response → falls back to
	// agent.PreferredModel (defensive default in executeAgent).
	if run.Model != "deepseek-chat" {
		t.Errorf("model = %q, want deepseek-chat (upstream omitted; fallback)", run.Model)
	}
}

func TestExecuteAgent_DetectsMissingRunnerKey(t *testing.T) {
	viper.Set("service.runner_api_key", "")
	agent := &Agent{Slug: "x", PreferredModel: "y"}
	_, err := executeAgent(context.Background(), agent, "anything")
	if !errors.Is(err, errAgentNotConfigured) {
		t.Errorf("want errAgentNotConfigured, got %v", err)
	}
}

func TestExecuteAgent_HandlesUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"rate limit exceeded"}}`, 429)
	}))
	defer server.Close()

	viper.Set("service.runner_api_key", "test-key")
	viper.Set("service.runner_endpoint", server.URL)
	t.Cleanup(func() {
		viper.Set("service.runner_api_key", "")
		viper.Set("service.runner_endpoint", "")
	})

	prev := agentRunHTTPClient
	agentRunHTTPClient = &http.Client{Timeout: 2 * time.Second}
	t.Cleanup(func() { agentRunHTTPClient = prev })

	agent := &Agent{Slug: "x", PreferredModel: "y", MinTier: "light", SystemPrompt: "z"}
	_, err := executeAgent(context.Background(), agent, "hi")
	if err == nil {
		t.Fatal("expected error on upstream 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should surface status code: %v", err)
	}
}
