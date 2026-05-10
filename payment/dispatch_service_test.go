package payment

import (
	"chat/commerce"
	"chat/globals"
	"chat/newapi"
	"chat/plans"
	"chat/service"
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// newServiceTestEngine spins up the schema dispatch_service.go needs:
//   - auth (FK target)
//   - gtk_service_order (service.Migrate)
//   - gtk_payment_session (commerce.MigrateSessions)
//
// We re-use commerce migrations because handleServiceOrderPaid calls into
// commerce.GrantEntitlement (CAS on gtk_service_order) and
// commerce.ClosePaymentSession (UPDATE on gtk_payment_session).
func newServiceTestEngine(t *testing.T) *sql.DB {
	t.Helper()
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`CREATE TABLE auth (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id) VALUES (1), (42)`); err != nil {
		t.Fatalf("seed auth rows: %v", err)
	}

	if err := service.Migrate(db); err != nil {
		t.Fatalf("service.Migrate: %v", err)
	}
	if err := plans.Migrate(db); err != nil {
		t.Fatalf("plans.Migrate: %v", err)
	}
	if err := newapi.Migrate(db); err != nil {
		t.Fatalf("newapi.Migrate: %v", err)
	}
	if err := commerce.Migrate(db); err != nil {
		t.Fatalf("commerce.Migrate: %v", err)
	}

	// Seed the agent + service rows that gtk_service_order FKs to.
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_agent (slug, name, system_prompt, preferred_model, status)
		VALUES ('agent-stub', 'Stub Agent', 'p', 'm', 'active')
	`); err != nil {
		t.Fatalf("seed gtk_agent: %v", err)
	}
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_service (slug, name, category, agent_slug,
			price_cny_cents, billing_type, status)
		VALUES ('svc-stub', 'Stub Service', 'diy_agent', 'agent-stub',
			198000, 'one_time', 'active')
	`); err != nil {
		t.Fatalf("seed gtk_service: %v", err)
	}
	return db
}

// makeServicePayload builds a service-order webhook payload (custom_data
// carries greentokey_order_no — that's how dispatch() routes here).
// sessionID may be empty to simulate a pre-Wave-4 checkout.
func makeServicePayload(eventName, lsID, orderNo, sessionID string) *webhookPayload {
	custom := map[string]interface{}{
		"user_id":             "1",
		"greentokey_order_no": orderNo,
	}
	if sessionID != "" {
		custom["greentokey_session_id"] = sessionID
	}
	p := &webhookPayload{}
	p.Meta.EventName = eventName
	p.Meta.TestMode = true
	p.Meta.CustomData = custom
	p.Data.ID = lsID
	p.Data.Attributes.VariantID = 999
	p.Data.Attributes.Status = "active"
	p.Data.Attributes.RenewsAt = "2026-06-01T00:00:00Z"
	p.Data.Attributes.TestMode = true
	return p
}

// seedServiceOrder inserts a gtk_service_order row in the given status.
func seedServiceOrder(t *testing.T, db *sql.DB, orderNo, status string) {
	t.Helper()
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_service_order
		  (order_no, coai_user_id, service_id, service_slug,
		   price_cny_cents_paid, payment_provider, status)
		VALUES (?, 1, 1, 'svc-stub', 198000, 'lemonsqueezy', ?)
	`, orderNo, status); err != nil {
		t.Fatalf("seed gtk_service_order(%s,%s): %v", orderNo, status, err)
	}
}

// --- C2.1 happy path ----------------------------------------------------

func TestHandleServiceEvent_OrderCreated(t *testing.T) {
	db := newServiceTestEngine(t)

	// Pre-seed the order in pending_payment + open a payment session that
	// the webhook should close.
	const orderNo = "SVC-OC-001"
	seedServiceOrder(t, db, orderNo, "pending_payment")
	sess, err := commerce.OpenPaymentSession(db, orderNo,
		commerce.ProductService, "lemonsqueezy", 198000, 1)
	if err != nil {
		t.Fatalf("OpenPaymentSession: %v", err)
	}

	p := makeServicePayload(eventOrderCreated, "ls-order-xyz", orderNo, sess.SessionID)
	if err := handleServiceEvent(db, p, orderNo); err != nil {
		t.Fatalf("handleServiceEvent: %v", err)
	}

	// (1) gtk_service_order flipped to paid + ls_order_id recorded
	//     (MarkOrderPaid did this).
	var status, lsOrderID string
	if err := db.QueryRow(
		`SELECT status, ls_order_id FROM gtk_service_order WHERE order_no = ?`,
		orderNo,
	).Scan(&status, &lsOrderID); err != nil {
		t.Fatalf("read gtk_service_order: %v", err)
	}
	if status != "paid" {
		t.Errorf("status=%q want paid", status)
	}
	if lsOrderID != "ls-order-xyz" {
		t.Errorf("ls_order_id=%q want ls-order-xyz", lsOrderID)
	}

	// (2) GrantEntitlement is idempotent — CAS observed status='paid' set
	//     by step 1 and returned changed=false. Nothing more to assert
	//     beyond no error from handleServiceEvent.

	// (3) gtk_payment_session closed: status='paid', closed_at set.
	// NullString (not NullTime) because SQLite stores DATETIME as a string
	// and the go-sqlite3 driver doesn't auto-coerce to time.Time without
	// explicit DSN flags.
	var sessStatus string
	var closedAt sql.NullString
	if err := db.QueryRow(
		`SELECT status, closed_at FROM gtk_payment_session WHERE session_id = ?`,
		sess.SessionID,
	).Scan(&sessStatus, &closedAt); err != nil {
		t.Fatalf("read gtk_payment_session: %v", err)
	}
	if sessStatus != "paid" {
		t.Errorf("session status=%q want paid", sessStatus)
	}
	if !closedAt.Valid || closedAt.String == "" {
		t.Errorf("session closed_at is NULL/empty; expected stamped time")
	}
}

// Same path but no greentokey_session_id in custom_data — exercises the
// pre-Wave-4 fall-through branch (skip ClosePaymentSession with debug log).
func TestHandleServiceEvent_OrderCreated_NoSessionID(t *testing.T) {
	db := newServiceTestEngine(t)
	const orderNo = "SVC-OC-NOSESS"
	seedServiceOrder(t, db, orderNo, "pending_payment")

	p := makeServicePayload(eventOrderCreated, "ls-order-nosess", orderNo, "")
	if err := handleServiceEvent(db, p, orderNo); err != nil {
		t.Fatalf("handleServiceEvent: %v", err)
	}

	var status string
	if err := db.QueryRow(
		`SELECT status FROM gtk_service_order WHERE order_no = ?`, orderNo,
	).Scan(&status); err != nil {
		t.Fatalf("read gtk_service_order: %v", err)
	}
	if status != "paid" {
		t.Errorf("status=%q want paid (MarkOrderPaid path runs without session)", status)
	}
}

// --- C2.2 cancellation event: log only, no state change -----------------

func TestHandleServiceEvent_SubscriptionCancelled_LogsOnly(t *testing.T) {
	db := newServiceTestEngine(t)
	const orderNo = "SVC-CANCEL"
	seedServiceOrder(t, db, orderNo, "paid")

	p := makeServicePayload(eventCancelled, "ls-cancel-1", orderNo, "")
	if err := handleServiceEvent(db, p, orderNo); err != nil {
		t.Fatalf("handleServiceEvent(cancelled): %v", err)
	}

	// Order status MUST remain paid — cancellations of the LS subscription
	// don't touch the already-paid order.
	var status string
	if err := db.QueryRow(
		`SELECT status FROM gtk_service_order WHERE order_no = ?`, orderNo,
	).Scan(&status); err != nil {
		t.Fatalf("read: %v", err)
	}
	if status != "paid" {
		t.Errorf("status=%q changed on cancel; want paid", status)
	}
}

// --- C2.3 refund flow: RefundServiceOrder calls commerce.RevokeEntitlement -

func TestHandleServiceEvent_RefundFlow(t *testing.T) {
	db := newServiceTestEngine(t)
	const orderNo = "SVC-REF"
	seedServiceOrder(t, db, orderNo, "paid")

	state, err := RefundServiceOrder(context.Background(), db, orderNo, "refund_full")
	if err != nil {
		t.Fatalf("RefundServiceOrder: %v", err)
	}
	// 'paid' → CAS to 'refunded' → maps to EntitlementRevoked.
	if state != commerce.EntitlementRevoked {
		t.Errorf("state=%q want %q", state, commerce.EntitlementRevoked)
	}

	// DB reflects the flip.
	var status string
	var refundReason sql.NullString
	if err := db.QueryRow(
		`SELECT status, refund_reason FROM gtk_service_order WHERE order_no = ?`,
		orderNo,
	).Scan(&status, &refundReason); err != nil {
		t.Fatalf("read gtk_service_order: %v", err)
	}
	if status != "refunded" {
		t.Errorf("status=%q want refunded", status)
	}
	if !refundReason.Valid || refundReason.String != "refund_full" {
		t.Errorf("refund_reason=%q want refund_full", refundReason.String)
	}

	// Idempotent: second call returns same state, no error.
	state2, err := RefundServiceOrder(context.Background(), db, orderNo, "refund_full")
	if err != nil {
		t.Fatalf("RefundServiceOrder (second call): %v", err)
	}
	if state2 != commerce.EntitlementRevoked {
		t.Errorf("idempotent state=%q want %q", state2, commerce.EntitlementRevoked)
	}
}

func TestRefundServiceOrder_RejectsEmptyOrderNo(t *testing.T) {
	db := newServiceTestEngine(t)
	if _, err := RefundServiceOrder(context.Background(), db, "", "refund_full"); err == nil {
		t.Fatal("RefundServiceOrder accepted empty order no")
	}
}

// --- session_id extraction edge cases ----------------------------------

func TestSessionIDFromCustomData_Present(t *testing.T) {
	got := sessionIDFromCustomData(map[string]interface{}{
		"greentokey_session_id": "uuid-abc-123",
	})
	if got != "uuid-abc-123" {
		t.Errorf("got %q want uuid-abc-123", got)
	}
}

func TestSessionIDFromCustomData_Absent(t *testing.T) {
	got := sessionIDFromCustomData(map[string]interface{}{})
	if got != "" {
		t.Errorf("got %q want empty", got)
	}
}

// --- routing smoke: dispatch() routes to service when greentokey_order_no set --

func TestDispatch_RoutesToServiceWhenOrderNoPresent(t *testing.T) {
	db := newServiceTestEngine(t)
	const orderNo = "SVC-DISP"
	seedServiceOrder(t, db, orderNo, "pending_payment")

	p := makeServicePayload(eventOrderCreated, "ls-disp-1", orderNo, "")
	if err := dispatch(db, p); err != nil {
		t.Fatalf("dispatch: %v", err)
	}

	var status string
	_ = db.QueryRow(
		`SELECT status FROM gtk_service_order WHERE order_no = ?`, orderNo,
	).Scan(&status)
	if status != "paid" {
		t.Errorf("after dispatch: status=%q want paid (router must hit service path)", status)
	}
}

// Sanity check that a token-plan payload (no greentokey_order_no) does NOT
// touch the service table.
func TestDispatch_DoesNotRouteServiceWhenOrderNoAbsent(t *testing.T) {
	db := newServiceTestEngine(t)

	p := &webhookPayload{}
	p.Meta.EventName = eventCreated
	p.Meta.TestMode = true
	p.Meta.CustomData = map[string]interface{}{"user_id": "42"}
	p.Data.ID = "ls-tok-not-service"
	p.Data.Attributes.VariantID = 999
	p.Data.Attributes.Status = "active"
	p.Data.Attributes.RenewsAt = time.Now().UTC().Format(time.RFC3339)

	// Need the subscription table for the token path. Add it minimally so
	// dispatch doesn't err on a missing schema.
	if _, err := globals.ExecDb(db, `
		CREATE TABLE IF NOT EXISTS subscription (
		  id INT PRIMARY KEY AUTO_INCREMENT,
		  level INT DEFAULT 1,
		  user_id INT UNIQUE,
		  expired_at DATETIME,
		  total_month INT DEFAULT 0,
		  enterprise BOOLEAN DEFAULT FALSE
		);
	`); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("payment.Migrate: %v", err)
	}

	if err := dispatch(db, p); err != nil {
		t.Fatalf("dispatch token path: %v", err)
	}

	// service order was untouched (no row exists for that ls id).
	var n int
	_ = db.QueryRow(
		`SELECT COUNT(*) FROM gtk_service_order WHERE ls_order_id = ?`,
		"ls-tok-not-service",
	).Scan(&n)
	if n != 0 {
		t.Errorf("token-plan payload reached service path; count=%d want 0", n)
	}
}
