// entitlement_test.go — dual-engine tests for the entitlement state
// machine (PKG-2 Wave 2.5 B3).
//
// SQLite in-memory + plans.Migrate + service.Migrate + newapi.Migrate to
// build the schema this file actually exercises (gtk_user_plan,
// gtk_service_order, gtk_newapi_binding, gtk_newapi_pending_provisions).
// We use the real Migrate funcs rather than hand-rolled CREATE TABLE so
// any future schema drift in those packages is caught here too.
//
// Test seam: provisionForPlanFn / disableTokenFn are package-level
// function vars (see entitlement.go header comment for rationale). Each
// test that needs a stub overrides them inside a t.Cleanup-restored block.
//
// Test matrix per plan v2 §B3 acceptance:
//
//   1.  TestGrantEntitlement_TokenPlan_Success
//   2.  TestGrantEntitlement_TokenPlan_NewAPITransientFailure
//   3.  TestGrantEntitlement_ServiceOrder_Success
//   4.  TestRevokeEntitlement_TokenFullRefund_Idempotent
//   5.  TestRevokeEntitlement_ServiceMidFlight
//   6.  TestRevokeEntitlement_ServicePostDelivery
//   7.  TestRevokeEntitlement_NotFound
//   8.  TestCompareAndSwap_HappyPath
//   9.  TestCompareAndSwap_StatusMismatch
//   10. TestCheckEntitlement_TokenActive
//   11. TestCheckEntitlement_NoPlan
//   12. TestCompareAndSwap_Race_FinalizeRunVsRevoke (CR4 headline test)

package commerce

import (
	"chat/globals"
	"chat/newapi"
	"chat/plans"
	"chat/service"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// ----------------------------------------------------------------------
// Test fixtures
// ----------------------------------------------------------------------

// newSqliteEntitlementDB spins up an in-memory SQLite with the schema this
// file needs:
//   - auth         (FK target stub — CREATE TABLE only; rows inserted on demand)
//   - gtk_plan / gtk_user_plan / gtk_app_usage_log   (plans.Migrate)
//   - gtk_agent / gtk_service / gtk_service_order    (service.Migrate)
//   - gtk_newapi_binding / gtk_newapi_pending_provisions (newapi.Migrate)
//
// Returns a ready-to-use *sql.DB with three auth rows seeded (id=1,2,3) so
// callers don't repeat the boilerplate. Cleanup restores SqliteEngine
// and closes the DB.
func newSqliteEntitlementDB(t *testing.T) *sql.DB {
	t.Helper()
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Force a single underlying connection. SQLite's :memory: DSN gives
	// each new connection a SEPARATE in-memory database — a goroutine
	// that opens a second connection (e.g. the CR4 race test's parallel
	// CAS calls) would see "no such table". Pinning to 1 connection makes
	// the in-memory schema visible to every caller. This serializes
	// writes — fine for tests, and the CR4 invariant (exactly one CAS
	// wins) still holds because both callers contend for the same row.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE auth (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (1), (2), (3)`); err != nil {
		t.Fatalf("seed auth rows: %v", err)
	}

	// service.Migrate first (it creates gtk_agent / gtk_service /
	// gtk_service_order — gtk_plan in plans.Migrate may FK to gtk_service).
	if err := service.Migrate(db); err != nil {
		t.Fatalf("service.Migrate: %v", err)
	}
	if err := plans.Migrate(db); err != nil {
		t.Fatalf("plans.Migrate: %v", err)
	}
	if err := newapi.Migrate(db); err != nil {
		t.Fatalf("newapi.Migrate: %v", err)
	}

	// Seed a gtk_plan row used by the token tests (plan_id=1).
	if _, err := db.Exec(`
		INSERT INTO gtk_plan (id, code, name, type, price_cents, duration_days)
		VALUES (1, 'test-token-plan', 'Test Token Plan', 'subscription', 1500, 30)
	`); err != nil {
		t.Fatalf("seed gtk_plan: %v", err)
	}

	// Seed a gtk_service + gtk_agent baseline used by service tests.
	if _, err := db.Exec(`
		INSERT INTO gtk_agent
		  (id, slug, name, system_prompt, preferred_model, status)
		VALUES (1, 'agent-stub', 'Stub Agent', 'you are a stub', 'gpt-4o-mini', 'active')
	`); err != nil {
		t.Fatalf("seed gtk_agent: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO gtk_service
		  (id, slug, name, category, agent_slug, price_cny_cents, status)
		VALUES (1, 'service-stub', 'Stub Service', 'diy_agent', 'agent-stub', 198000, 'active')
	`); err != nil {
		t.Fatalf("seed gtk_service: %v", err)
	}

	return db
}

// withProvisionStub replaces the package-level provisionForPlanFn for the
// duration of the test and restores it on Cleanup. fn receives the same
// args as newapi.ProvisionForPlan.
func withProvisionStub(t *testing.T, fn func(ctx context.Context, db *sql.DB, coaiUserID int64, spec newapi.PlanSpec) (*newapi.Binding, error)) {
	t.Helper()
	prev := provisionForPlanFn
	provisionForPlanFn = fn
	t.Cleanup(func() { provisionForPlanFn = prev })
}

// withDisableStub replaces disableTokenFn for the duration of the test.
func withDisableStub(t *testing.T, fn func(ctx context.Context, tokenID int64) error) {
	t.Helper()
	prev := disableTokenFn
	disableTokenFn = fn
	t.Cleanup(func() { disableTokenFn = prev })
}

// seedServiceOrder inserts a gtk_service_order row with the given status.
func seedServiceOrder(t *testing.T, db *sql.DB, orderNo string, userID int64, status string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO gtk_service_order
		  (order_no, coai_user_id, service_id, service_slug,
		   price_cny_cents_paid, payment_provider, status)
		VALUES (?, ?, 1, 'service-stub', 198000, 'lemonsqueezy', ?)
	`, orderNo, userID, status); err != nil {
		t.Fatalf("seed gtk_service_order(%s): %v", status, err)
	}
}

// seedUserPlan inserts a gtk_user_plan row directly (bypasses GrantEntitlement)
// so tests that exercise Revoke / Check don't require a full grant first.
func seedUserPlan(t *testing.T, db *sql.DB, userID, planID int64, orderNo, status string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO gtk_user_plan
		  (user_id, plan_id, product_type, status, expire_at, order_id)
		VALUES (?, ?, 'token', ?, ?, ?)
	`, userID, planID, status, time.Now().Add(30*24*time.Hour), orderNo); err != nil {
		t.Fatalf("seed gtk_user_plan(%s): %v", status, err)
	}
}

// seedNewAPIBinding inserts a gtk_newapi_binding row so RevokeEntitlement's
// DisableToken hook has a token id to act on.
func seedNewAPIBinding(t *testing.T, db *sql.DB, coaiUserID, newapiUserID, tokenID int64) {
	t.Helper()
	if err := newapi.SaveBinding(db, &newapi.Binding{
		CoaiUserID:     coaiUserID,
		NewapiUserID:   newapiUserID,
		NewapiTokenID:  tokenID,
		NewapiTokenKey: fmt.Sprintf("sk-test-%d", coaiUserID),
		Group:          "default",
		LastKnownQuota: 1_500_000,
	}); err != nil {
		t.Fatalf("seed newapi binding: %v", err)
	}
}

// readUserPlanStatus reads back (status, cancellation_reason) for assertion.
func readUserPlanStatus(t *testing.T, db *sql.DB, orderNo string) (string, sql.NullString) {
	t.Helper()
	var (
		status string
		reason sql.NullString
	)
	if err := globals.QueryRowDb(db,
		`SELECT status, cancellation_reason FROM gtk_user_plan WHERE order_id = ?`,
		orderNo,
	).Scan(&status, &reason); err != nil {
		t.Fatalf("read gtk_user_plan(%s): %v", orderNo, err)
	}
	return status, reason
}

// readServiceOrderStatus reads back the status of a gtk_service_order row.
func readServiceOrderStatus(t *testing.T, db *sql.DB, orderNo string) string {
	t.Helper()
	var status string
	if err := globals.QueryRowDb(db,
		`SELECT status FROM gtk_service_order WHERE order_no = ?`, orderNo,
	).Scan(&status); err != nil {
		t.Fatalf("read gtk_service_order(%s): %v", orderNo, err)
	}
	return status
}

// ----------------------------------------------------------------------
// 1. TestGrantEntitlement_TokenPlan_Success
// ----------------------------------------------------------------------

// Stubs newapi.ProvisionForPlan to return a fake binding successfully.
// Asserts gtk_user_plan row created (status='active', cancellation_reason
// NULL, product_type='token') AND no row was enqueued in the pending
// queue (because the provision succeeded).
func TestGrantEntitlement_TokenPlan_Success(t *testing.T) {
	db := newSqliteEntitlementDB(t)

	provisionCalls := 0
	withProvisionStub(t, func(ctx context.Context, _ *sql.DB, coaiUserID int64, spec newapi.PlanSpec) (*newapi.Binding, error) {
		provisionCalls++
		// The stub also persists the binding so a downstream RevokeEntitlement
		// can find it (mirrors what the real ProvisionForPlan does).
		_ = newapi.SaveBinding(db, &newapi.Binding{
			CoaiUserID:     coaiUserID,
			NewapiUserID:   100,
			NewapiTokenID:  200,
			NewapiTokenKey: "sk-stub-success",
			Group:          "default",
			LastKnownQuota: spec.QuotaUnits,
		})
		return &newapi.Binding{CoaiUserID: coaiUserID, NewapiUserID: 100, NewapiTokenID: 200}, nil
	})

	err := GrantEntitlement(context.Background(), db, EntitlementGrant{
		UserID:      1,
		ProductType: ProductToken,
		OrderNo:     "ord_token_happy",
		PlanID:      1,
		QuotaUnits:  7_500_000,
		ExpiresAt:   time.Now().Add(30 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("GrantEntitlement: %v", err)
	}
	if provisionCalls != 1 {
		t.Errorf("provisionForPlanFn called %d times, want 1", provisionCalls)
	}

	// gtk_user_plan: status='active', cancellation_reason NULL.
	status, reason := readUserPlanStatus(t, db, "ord_token_happy")
	if status != "active" {
		t.Errorf("gtk_user_plan.status = %q, want active", status)
	}
	if reason.Valid {
		t.Errorf("gtk_user_plan.cancellation_reason = %q, want NULL", reason.String)
	}

	// No pending provision row.
	var pending int
	if err := globals.QueryRowDb(db,
		`SELECT COUNT(*) FROM gtk_newapi_pending_provisions WHERE user_id = 1`,
	).Scan(&pending); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pending != 0 {
		t.Errorf("pending provisions = %d, want 0 (provision succeeded)", pending)
	}

	// Binding exists (stub persisted it).
	bind, err := newapi.LoadBinding(db, 1)
	if err != nil {
		t.Fatalf("LoadBinding: %v", err)
	}
	if bind.NewapiTokenID != 200 {
		t.Errorf("binding token id = %d, want 200", bind.NewapiTokenID)
	}
}

// ----------------------------------------------------------------------
// 2. TestGrantEntitlement_TokenPlan_NewAPITransientFailure
// ----------------------------------------------------------------------

// Stubs ProvisionForPlan to return a transient error. Asserts:
//   - GrantEntitlement returns nil (webhook delivery contract).
//   - gtk_newapi_pending_provisions row enqueued with status='pending'.
//   - gtk_user_plan still upserted as 'active' (entitlement record is
//     consistent with "user paid"; binding row will land via retry worker).
func TestGrantEntitlement_TokenPlan_NewAPITransientFailure(t *testing.T) {
	db := newSqliteEntitlementDB(t)

	transientErr := errors.New("newapi: post /api/user: dial tcp: connection refused")
	withProvisionStub(t, func(ctx context.Context, _ *sql.DB, coaiUserID int64, spec newapi.PlanSpec) (*newapi.Binding, error) {
		return nil, transientErr
	})

	err := GrantEntitlement(context.Background(), db, EntitlementGrant{
		UserID:      1,
		ProductType: ProductToken,
		OrderNo:     "ord_token_transient",
		PlanID:      1,
		QuotaUnits:  7_500_000,
		ExpiresAt:   time.Now().Add(30 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("GrantEntitlement should swallow transient err, got %v", err)
	}

	// Pending row exists.
	var (
		pendingCount   int
		pendingStatus  string
		pendingPlanID  int64
		pendingLastErr sql.NullString
	)
	if err := globals.QueryRowDb(db,
		`SELECT COUNT(*) FROM gtk_newapi_pending_provisions WHERE user_id = 1`,
	).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending: %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("pending count = %d, want 1", pendingCount)
	}
	if err := globals.QueryRowDb(db,
		`SELECT status, plan_id, last_error FROM gtk_newapi_pending_provisions WHERE user_id = 1`,
	).Scan(&pendingStatus, &pendingPlanID, &pendingLastErr); err != nil {
		t.Fatalf("read pending row: %v", err)
	}
	if pendingStatus != "pending" {
		t.Errorf("pending.status = %q, want pending", pendingStatus)
	}
	if pendingPlanID != 1 {
		t.Errorf("pending.plan_id = %d, want 1", pendingPlanID)
	}
	if !pendingLastErr.Valid || pendingLastErr.String != transientErr.Error() {
		t.Errorf("pending.last_error = %q, want %q", pendingLastErr.String, transientErr.Error())
	}

	// gtk_user_plan still upserted as active (entitlement record consistent).
	status, _ := readUserPlanStatus(t, db, "ord_token_transient")
	if status != "active" {
		t.Errorf("gtk_user_plan.status = %q, want active (entitlement should still be granted)", status)
	}
}

// ----------------------------------------------------------------------
// 3. TestGrantEntitlement_ServiceOrder_Success
// ----------------------------------------------------------------------

// Pre-seed a gtk_service_order in 'pending_payment'. GrantEntitlement
// for product_type='service' must CAS to 'paid'.
func TestGrantEntitlement_ServiceOrder_Success(t *testing.T) {
	db := newSqliteEntitlementDB(t)
	seedServiceOrder(t, db, "ord_svc_pending", 1, "pending_payment")

	err := GrantEntitlement(context.Background(), db, EntitlementGrant{
		UserID:      1,
		ProductType: ProductService,
		OrderNo:     "ord_svc_pending",
		ServiceID:   1,
	})
	if err != nil {
		t.Fatalf("GrantEntitlement(service): %v", err)
	}
	if got := readServiceOrderStatus(t, db, "ord_svc_pending"); got != "paid" {
		t.Errorf("status = %q, want paid", got)
	}
}

// ----------------------------------------------------------------------
// 4. TestRevokeEntitlement_TokenFullRefund_Idempotent
// ----------------------------------------------------------------------

// Seeds an active token plan + binding. First Revoke flips to canceled +
// calls DisableToken (verified via stub call counter). Second Revoke
// returns the same EntitlementRevoked state, no error, no extra DisableToken.
func TestRevokeEntitlement_TokenFullRefund_Idempotent(t *testing.T) {
	db := newSqliteEntitlementDB(t)
	seedUserPlan(t, db, 1, 1, "ord_revoke_tk", "active")
	seedNewAPIBinding(t, db, 1, 100, 200)

	disableCalls := 0
	withDisableStub(t, func(ctx context.Context, tokenID int64) error {
		disableCalls++
		if tokenID != 200 {
			t.Errorf("DisableToken called with %d, want 200", tokenID)
		}
		return nil
	})

	state, err := RevokeEntitlement(context.Background(), db,
		"ord_revoke_tk", ProductToken, "refund_full")
	if err != nil {
		t.Fatalf("first Revoke: %v", err)
	}
	if state != EntitlementRevoked {
		t.Errorf("first Revoke state = %q, want %q", state, EntitlementRevoked)
	}
	if disableCalls != 1 {
		t.Errorf("DisableToken calls after first Revoke = %d, want 1", disableCalls)
	}

	// gtk_user_plan flipped.
	status, reason := readUserPlanStatus(t, db, "ord_revoke_tk")
	if status != "canceled" {
		t.Errorf("status = %q, want canceled", status)
	}
	if !reason.Valid || reason.String != "refund_full" {
		t.Errorf("cancellation_reason = %q (valid=%v), want refund_full", reason.String, reason.Valid)
	}

	// Second Revoke: idempotent — same state, no error, no extra DisableToken.
	state2, err := RevokeEntitlement(context.Background(), db,
		"ord_revoke_tk", ProductToken, "refund_full")
	if err != nil {
		t.Fatalf("second Revoke (must be idempotent): %v", err)
	}
	if state2 != EntitlementRevoked {
		t.Errorf("second Revoke state = %q, want %q", state2, EntitlementRevoked)
	}
	if disableCalls != 1 {
		t.Errorf("DisableToken calls after second Revoke = %d, want 1 (no extra call)", disableCalls)
	}
}

// ----------------------------------------------------------------------
// 5. TestRevokeEntitlement_ServiceMidFlight
// ----------------------------------------------------------------------

// Seeds a 'running' service order. Revoke must CAS to 'canceled_mid_flight'
// (NOT 'refunded') and return EntitlementCanceled.
func TestRevokeEntitlement_ServiceMidFlight(t *testing.T) {
	db := newSqliteEntitlementDB(t)
	seedServiceOrder(t, db, "ord_running", 1, "running")

	state, err := RevokeEntitlement(context.Background(), db,
		"ord_running", ProductService, "refund_during_run")
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if state != EntitlementCanceled {
		t.Errorf("state = %q, want %q", state, EntitlementCanceled)
	}
	if got := readServiceOrderStatus(t, db, "ord_running"); got != "canceled_mid_flight" {
		t.Errorf("DB status = %q, want canceled_mid_flight (CR5 ENUM)", got)
	}
	// refund_reason recorded.
	var reason sql.NullString
	if err := globals.QueryRowDb(db,
		`SELECT refund_reason FROM gtk_service_order WHERE order_no = ?`, "ord_running",
	).Scan(&reason); err != nil {
		t.Fatalf("read refund_reason: %v", err)
	}
	if !reason.Valid || reason.String != "refund_during_run" {
		t.Errorf("refund_reason = %q, want refund_during_run", reason.String)
	}
}

// ----------------------------------------------------------------------
// 6. TestRevokeEntitlement_ServicePostDelivery
// ----------------------------------------------------------------------

// Seeds a 'completed' service order. Revoke must CAS to
// 'refunded_post_delivery' and return EntitlementRevoked.
func TestRevokeEntitlement_ServicePostDelivery(t *testing.T) {
	db := newSqliteEntitlementDB(t)
	seedServiceOrder(t, db, "ord_completed", 1, "completed")

	state, err := RevokeEntitlement(context.Background(), db,
		"ord_completed", ProductService, "refund_post_delivery")
	if err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if state != EntitlementRevoked {
		t.Errorf("state = %q, want %q", state, EntitlementRevoked)
	}
	if got := readServiceOrderStatus(t, db, "ord_completed"); got != "refunded_post_delivery" {
		t.Errorf("DB status = %q, want refunded_post_delivery (CR5 ENUM)", got)
	}
}

// ----------------------------------------------------------------------
// 7. TestRevokeEntitlement_NotFound
// ----------------------------------------------------------------------

// Revoke on a non-existent orderNo must return nil + zero-value state
// (don't error; webhook race tolerance per H4).
func TestRevokeEntitlement_NotFound(t *testing.T) {
	db := newSqliteEntitlementDB(t)

	// Service path.
	state, err := RevokeEntitlement(context.Background(), db,
		"ord_nonexistent_svc", ProductService, "refund_full")
	if err != nil {
		t.Errorf("Revoke(service, not found): err = %v, want nil (webhook race tolerated)", err)
	}
	if state != "" {
		t.Errorf("Revoke(service, not found): state = %q, want zero value", state)
	}

	// Token path (no plan + no binding).
	state, err = RevokeEntitlement(context.Background(), db,
		"ord_nonexistent_tk", ProductToken, "refund_full")
	if err != nil {
		t.Errorf("Revoke(token, not found): err = %v, want nil", err)
	}
	if state != "" {
		t.Errorf("Revoke(token, not found): state = %q, want zero value", state)
	}
}

// ----------------------------------------------------------------------
// 8. TestCompareAndSwap_HappyPath
// ----------------------------------------------------------------------

// CAS('running','completed') on a row currently in 'running' returns
// (true, nil) and flips the row.
func TestCompareAndSwap_HappyPath(t *testing.T) {
	db := newSqliteEntitlementDB(t)
	seedServiceOrder(t, db, "ord_cas_happy", 1, "running")

	changed, err := CompareAndSwapServiceOrderStatus(db, "ord_cas_happy", "running", "completed")
	if err != nil {
		t.Fatalf("CAS: %v", err)
	}
	if !changed {
		t.Errorf("changed = false, want true")
	}
	if got := readServiceOrderStatus(t, db, "ord_cas_happy"); got != "completed" {
		t.Errorf("status = %q, want completed", got)
	}
}

// ----------------------------------------------------------------------
// 9. TestCompareAndSwap_StatusMismatch
// ----------------------------------------------------------------------

// CAS('running','completed') on a row already in 'completed' returns
// (false, nil) and leaves the row alone (no error — caller observed the race).
func TestCompareAndSwap_StatusMismatch(t *testing.T) {
	db := newSqliteEntitlementDB(t)
	seedServiceOrder(t, db, "ord_cas_mismatch", 1, "completed")

	changed, err := CompareAndSwapServiceOrderStatus(db, "ord_cas_mismatch", "running", "completed")
	if err != nil {
		t.Errorf("CAS should not error on status mismatch, got %v", err)
	}
	if changed {
		t.Errorf("changed = true, want false (status was 'completed' not 'running')")
	}
	if got := readServiceOrderStatus(t, db, "ord_cas_mismatch"); got != "completed" {
		t.Errorf("status = %q, want completed (no mutation)", got)
	}
}

// ----------------------------------------------------------------------
// 10. TestCheckEntitlement_TokenActive
// ----------------------------------------------------------------------

// User has an active gtk_user_plan token row → CheckEntitlement returns
// EntitlementActive.
func TestCheckEntitlement_TokenActive(t *testing.T) {
	db := newSqliteEntitlementDB(t)
	seedUserPlan(t, db, 1, 1, "ord_check_active", "active")

	state, err := CheckEntitlement(db, 1, ProductToken)
	if err != nil {
		t.Fatalf("CheckEntitlement: %v", err)
	}
	if state != EntitlementActive {
		t.Errorf("state = %q, want %q", state, EntitlementActive)
	}
}

// ----------------------------------------------------------------------
// 11. TestCheckEntitlement_NoPlan
// ----------------------------------------------------------------------

// User has no gtk_user_plan rows → returns EntitlementExpired (not an
// error). Documents the zero-value choice for billing-guard callers.
func TestCheckEntitlement_NoPlan(t *testing.T) {
	db := newSqliteEntitlementDB(t)

	state, err := CheckEntitlement(db, 99 /* user with no plans */, ProductToken)
	if err != nil {
		t.Fatalf("CheckEntitlement: %v", err)
	}
	if state != EntitlementExpired {
		t.Errorf("state = %q, want %q (zero-value for no-plan user)", state, EntitlementExpired)
	}

	// Same for service product type.
	state, err = CheckEntitlement(db, 99, ProductService)
	if err != nil {
		t.Fatalf("CheckEntitlement(service): %v", err)
	}
	if state != EntitlementExpired {
		t.Errorf("service state = %q, want %q", state, EntitlementExpired)
	}
}

// ----------------------------------------------------------------------
// 12. TestCompareAndSwap_Race_FinalizeRunVsRevoke (CR4 headline test)
// ----------------------------------------------------------------------

// Headline test for CR4: simulate finalizeRun (Wave 4 D1) and Revoke
// racing on the same gtk_service_order row in 'running' state.
//
// Goroutine A: CAS('running','completed')   — finalizeRun
// Goroutine B: CAS('running','canceled_mid_flight') — Revoke during run
//
// Expected: exactly ONE wins (returns true), the other returns false.
// DB end state is whichever flipped first; both goroutines exit cleanly.
//
// SQLite caveat: writes are serialized at the connection level (not truly
// concurrent), so this test is more about the CAS LOGIC being mutually
// exclusive than a wall-clock race. Real MySQL with row-level locking
// fully exercises the CAS in actual production scenarios. The per-iteration
// "exactly one wins" invariant is what we assert.
//
// We run multiple iterations with a barrier so the goroutines start
// closer together; even with serialization, both orderings of "who wins"
// are valid and the test asserts exactly one wins per iteration.
func TestCompareAndSwap_Race_FinalizeRunVsRevoke(t *testing.T) {
	db := newSqliteEntitlementDB(t)

	const iterations = 20
	winsA := 0
	winsB := 0

	for i := 0; i < iterations; i++ {
		orderNo := fmt.Sprintf("ord_race_%d", i)
		seedServiceOrder(t, db, orderNo, 1, "running")

		var (
			wg      sync.WaitGroup
			barrier = make(chan struct{})

			changedA, changedB bool
			errA, errB         error
		)

		wg.Add(2)
		// Goroutine A: finalizeRun simulator.
		go func() {
			defer wg.Done()
			<-barrier
			changedA, errA = CompareAndSwapServiceOrderStatus(db, orderNo, "running", "completed")
		}()
		// Goroutine B: Revoke-during-run simulator.
		go func() {
			defer wg.Done()
			<-barrier
			changedB, errB = CompareAndSwapServiceOrderStatus(db, orderNo, "running", "canceled_mid_flight")
		}()

		// Release both goroutines simultaneously.
		close(barrier)
		wg.Wait()

		if errA != nil {
			t.Fatalf("iter %d goroutine A errored: %v", i, errA)
		}
		if errB != nil {
			t.Fatalf("iter %d goroutine B errored: %v", i, errB)
		}
		// Exactly one must win — the headline CR4 invariant.
		if changedA == changedB {
			t.Fatalf("iter %d: changedA=%v changedB=%v — exactly one must win",
				i, changedA, changedB)
		}

		// DB end state must match the winner.
		got := readServiceOrderStatus(t, db, orderNo)
		switch {
		case changedA:
			winsA++
			if got != "completed" {
				t.Errorf("iter %d: A won but DB status = %q, want completed", i, got)
			}
		case changedB:
			winsB++
			if got != "canceled_mid_flight" {
				t.Errorf("iter %d: B won but DB status = %q, want canceled_mid_flight", i, got)
			}
		}
	}

	t.Logf("CR4 race: A (finalizeRun) wins %d, B (Revoke) wins %d over %d iterations",
		winsA, winsB, iterations)
	if winsA+winsB != iterations {
		t.Errorf("total wins (%d) != iterations (%d) — should never happen",
			winsA+winsB, iterations)
	}
}
