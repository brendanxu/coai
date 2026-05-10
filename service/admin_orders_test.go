// Tests for the PKG-N1 admin orders list endpoint.
//
// Strategy mirrors customer_orders_test.go: SQLite in-memory + Migrate(),
// but we ALSO create a minimal `auth` table because ListAdminOrders does
// LEFT JOIN auth. The username JOIN is a convenience field on the row;
// the LEFT JOIN means orders without a matching auth row still surface
// (with username=""). We test both the present-username and orphan-FK
// cases.
//
// The HTTP handler tests follow the newapi/admin_routing_test.go shape:
// adminGinCtx-style helper that sets c.Set("user", ...) so RequireAdmin
// passes, then call the handler directly with httptest recorder.

package service

import (
	"bytes"
	"chat/connection"
	"chat/globals"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// adminOrdersTestDB extends the standard service test DB with a minimal
// `auth` table + an admin row id=1. Mirrors newapi/admin_routing_test.go
// adminTestDB pattern.
//
// Why we need our own helper rather than reusing setupTestDB: the
// service.Migrate() call doesn't create auth (auth is owned by the CoAI
// upstream auth package), but ListAdminOrders LEFT JOINs to it. Without
// the table, SQLite throws "no such table: auth" instead of returning
// rows with empty username.
func adminOrdersTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := setupTestDB(t)

	if _, err := db.Exec(`
		CREATE TABLE auth (
		  id INTEGER PRIMARY KEY,
		  username TEXT UNIQUE,
		  is_admin INTEGER DEFAULT 0,
		  is_banned INTEGER DEFAULT 0
		)
	`); err != nil {
		t.Fatalf("create auth: %v", err)
	}
	// Admin fixture so RequireAdmin passes when handler tests ride this DB.
	if _, err := db.Exec(
		`INSERT INTO auth (id, username, is_admin) VALUES (1, 'admin-fixture', 1)`,
	); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	return db
}

// seedAuthUser inserts a non-admin auth row so the JOIN in
// ListAdminOrders can resolve a username for the corresponding order.
// Tests that want to exercise the orphan-FK branch (username empty) just
// skip calling this for that user_id.
func seedAuthUser(t *testing.T, db *sql.DB, id int64, username string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO auth (id, username) VALUES (?, ?)`, id, username,
	); err != nil {
		t.Fatalf("seed auth(%d, %s): %v", id, username, err)
	}
}

// withConnDBOrders swaps connection.DB for the duration of the test so
// the handler's connection.DB reference points at the in-memory test
// instance.
func withConnDBOrders(t *testing.T, db *sql.DB) {
	t.Helper()
	prev := connection.DB
	connection.DB = db
	t.Cleanup(func() { connection.DB = prev })
}

// adminOrdersGinCtx builds a gin.Context that satisfies RequireAdmin.
func adminOrdersGinCtx(method, target string, body []byte, db *sql.DB) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var bodyReader *bytes.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	var req *http.Request
	if bodyReader != nil {
		req, _ = http.NewRequest(method, target, bodyReader)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, _ = http.NewRequest(method, target, nil)
	}
	c.Request = req
	c.Set("user", "admin-fixture")
	c.Set("db", db)
	return w, c
}

// ─────────────────────────────────────────────────────────────────────
// Storage layer
// ─────────────────────────────────────────────────────────────────────

func TestListAdminOrders_AllUsers(t *testing.T) {
	db := adminOrdersTestDB(t)
	seedAuthUser(t, db, 42, "alice")
	seedAuthUser(t, db, 99, "bob")
	seedCustomerOrder(t, db, "SVC-A1", 42, "paid")
	seedCustomerOrder(t, db, "SVC-A2", 42, "completed")
	seedCustomerOrder(t, db, "SVC-B1", 99, "pending_payment")

	rows, total, err := ListAdminOrders(db, AdminOrderListFilters{})
	if err != nil {
		t.Fatalf("ListAdminOrders: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	// Newest first (ORDER BY id DESC); SVC-B1 inserted last.
	if rows[0].OrderNo != "SVC-B1" {
		t.Errorf("rows[0].OrderNo = %q, want SVC-B1", rows[0].OrderNo)
	}
	// Username JOIN works.
	gotUsernames := map[string]string{}
	for _, r := range rows {
		gotUsernames[r.OrderNo] = r.Username
	}
	if gotUsernames["SVC-A1"] != "alice" || gotUsernames["SVC-B1"] != "bob" {
		t.Errorf("username JOIN broken: %+v", gotUsernames)
	}
	// Service name JOIN works (seedCustomerOrder seeds 小红书单篇文案).
	if rows[0].ServiceName != "小红书单篇文案" {
		t.Errorf("service name JOIN failed: %q", rows[0].ServiceName)
	}
	// Price formatting.
	if rows[0].PriceDisplayCNY != "¥19" {
		t.Errorf("price formatting: %q", rows[0].PriceDisplayCNY)
	}
}

func TestListAdminOrders_StatusFilter(t *testing.T) {
	db := adminOrdersTestDB(t)
	seedAuthUser(t, db, 7, "carol")
	seedCustomerOrder(t, db, "SVC-PEN", 7, "pending_payment")
	seedCustomerOrder(t, db, "SVC-PAY", 7, "paid")
	seedCustomerOrder(t, db, "SVC-DONE", 7, "completed")

	rows, total, err := ListAdminOrders(db, AdminOrderListFilters{Status: "paid"})
	if err != nil {
		t.Fatalf("paid filter: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].OrderNo != "SVC-PAY" {
		t.Fatalf("paid filter: total=%d rows=%+v", total, rows)
	}

	// Unknown status → error (handler converts to 400).
	_, _, err = ListAdminOrders(db, AdminOrderListFilters{Status: "bogus"})
	if err == nil {
		t.Fatalf("want error for bogus status, got nil")
	}
}

func TestListAdminOrders_UserFilter(t *testing.T) {
	db := adminOrdersTestDB(t)
	seedAuthUser(t, db, 42, "alice")
	seedAuthUser(t, db, 99, "bob")
	seedCustomerOrder(t, db, "SVC-A1", 42, "paid")
	seedCustomerOrder(t, db, "SVC-A2", 42, "completed")
	seedCustomerOrder(t, db, "SVC-B1", 99, "paid")

	rows, total, err := ListAdminOrders(db, AdminOrderListFilters{UserID: 42})
	if err != nil {
		t.Fatalf("user filter: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("user filter: total=%d len=%d", total, len(rows))
	}
	for _, r := range rows {
		if r.CoaiUserID != 42 {
			t.Errorf("leaked user %d in user_id=42 filter", r.CoaiUserID)
		}
	}
}

func TestListAdminOrders_SearchByOrderNo(t *testing.T) {
	db := adminOrdersTestDB(t)
	seedAuthUser(t, db, 11, "alice")
	seedCustomerOrder(t, db, "SVC-PRE-1", 11, "paid")
	seedCustomerOrder(t, db, "SVC-PRE-2", 11, "paid")
	seedCustomerOrder(t, db, "OTHER-X", 11, "paid")

	rows, total, err := ListAdminOrders(db, AdminOrderListFilters{Search: "SVC-PRE"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("search by order_no: total=%d len=%d", total, len(rows))
	}
}

func TestListAdminOrders_SearchByUsername(t *testing.T) {
	db := adminOrdersTestDB(t)
	seedAuthUser(t, db, 11, "alice")
	seedAuthUser(t, db, 12, "alex")
	seedAuthUser(t, db, 13, "bob")
	seedCustomerOrder(t, db, "SVC-X1", 11, "paid")
	seedCustomerOrder(t, db, "SVC-X2", 12, "paid")
	seedCustomerOrder(t, db, "SVC-X3", 13, "paid")

	rows, total, err := ListAdminOrders(db, AdminOrderListFilters{Search: "al"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("search by username: total=%d len=%d", total, len(rows))
	}
	for _, r := range rows {
		if r.Username != "alice" && r.Username != "alex" {
			t.Errorf("leaked user %q under 'al' search", r.Username)
		}
	}
}

func TestListAdminOrders_OrphanFKEmptyUsername(t *testing.T) {
	// User row is NEVER seeded for coai_user_id=777 — orphan FK
	// (in production this would mean a deleted user; we treat it as
	// data-quality issue, surface the order with empty username so
	// ops can investigate, not silently drop).
	db := adminOrdersTestDB(t)
	seedCustomerOrder(t, db, "SVC-ORPHAN", 777, "paid")

	rows, total, err := ListAdminOrders(db, AdminOrderListFilters{})
	if err != nil {
		t.Fatalf("ListAdminOrders: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("orphan still surfaces: total=%d len=%d", total, len(rows))
	}
	if rows[0].Username != "" {
		t.Errorf("expected empty username for orphan, got %q", rows[0].Username)
	}
	if rows[0].CoaiUserID != 777 {
		t.Errorf("CoaiUserID = %d, want 777", rows[0].CoaiUserID)
	}
}

func TestListAdminOrders_LimitClamped(t *testing.T) {
	db := adminOrdersTestDB(t)
	seedAuthUser(t, db, 11, "alice")
	for i := 1; i <= 5; i++ {
		seedCustomerOrder(t, db, "SVC-PG-"+string(rune('A'+i)), 11, "paid")
	}

	// limit 2 + offset 1 — pagination math
	rows, total, err := ListAdminOrders(db, AdminOrderListFilters{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("paginate: %v", err)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5 (filter ignores limit/offset)", total)
	}
	if len(rows) != 2 {
		t.Errorf("len(rows) = %d, want 2 (limit honored)", len(rows))
	}

	// Excessive limit clamped to adminListLimitMax.
	_, _, err = ListAdminOrders(db, AdminOrderListFilters{Limit: 99999})
	if err != nil {
		t.Errorf("oversize limit shouldn't error, just clamp: %v", err)
	}
}

func TestListAdminOrders_RefundReasonSurfaces(t *testing.T) {
	db := adminOrdersTestDB(t)
	seedAuthUser(t, db, 11, "alice")
	seedCustomerOrder(t, db, "SVC-REF", 11, "refunded")
	if _, err := globals.ExecDb(db,
		`UPDATE gtk_service_order SET refund_reason = 'customer changed mind' WHERE order_no = 'SVC-REF'`,
	); err != nil {
		t.Fatalf("annotate refund: %v", err)
	}

	rows, _, err := ListAdminOrders(db, AdminOrderListFilters{Status: "refunded"})
	if err != nil {
		t.Fatalf("ListAdminOrders: %v", err)
	}
	if len(rows) != 1 || rows[0].RefundReason != "customer changed mind" {
		t.Errorf("refund_reason not surfaced: %+v", rows)
	}
}

// ─────────────────────────────────────────────────────────────────────
// HTTP handler
// ─────────────────────────────────────────────────────────────────────

func TestListAdminOrdersAPI_AsAdminAllUsers(t *testing.T) {
	db := adminOrdersTestDB(t)
	withConnDBOrders(t, db)
	seedAuthUser(t, db, 42, "alice")
	seedAuthUser(t, db, 99, "bob")
	seedCustomerOrder(t, db, "SVC-A", 42, "paid")
	seedCustomerOrder(t, db, "SVC-B", 99, "completed")

	w, c := adminOrdersGinCtx("GET", "/api/gtk/v1/admin/orders", nil, db)
	ListAdminOrdersAPI(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Orders []AdminOrderRow `json:"orders"`
			Total  int64           `json:"total"`
			Limit  int             `json:"limit"`
			Offset int             `json:"offset"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, w.Body.String())
	}
	if !resp.Success {
		t.Fatal("success=false")
	}
	if resp.Data.Total != 2 || len(resp.Data.Orders) != 2 {
		t.Errorf("payload: total=%d orders=%d", resp.Data.Total, len(resp.Data.Orders))
	}
	if resp.Data.Limit != adminListLimitDefault {
		t.Errorf("default limit not applied: %d", resp.Data.Limit)
	}
}

func TestListAdminOrdersAPI_StatusFilter(t *testing.T) {
	db := adminOrdersTestDB(t)
	withConnDBOrders(t, db)
	seedAuthUser(t, db, 11, "alice")
	seedCustomerOrder(t, db, "SVC-PEN", 11, "pending_payment")
	seedCustomerOrder(t, db, "SVC-PAY", 11, "paid")

	w, c := adminOrdersGinCtx("GET", "/api/gtk/v1/admin/orders?status=paid", nil, db)
	c.Request.URL.RawQuery = "status=paid"
	ListAdminOrdersAPI(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Orders []AdminOrderRow `json:"orders"`
			Total  int64           `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Total != 1 || resp.Data.Orders[0].OrderNo != "SVC-PAY" {
		t.Errorf("status filter wrong: %+v", resp.Data)
	}
}

func TestListAdminOrdersAPI_UnknownStatus400(t *testing.T) {
	db := adminOrdersTestDB(t)
	withConnDBOrders(t, db)

	w, c := adminOrdersGinCtx("GET", "/api/gtk/v1/admin/orders?status=bogus", nil, db)
	c.Request.URL.RawQuery = "status=bogus"
	ListAdminOrdersAPI(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestListAdminOrdersAPI_InvalidUserID400(t *testing.T) {
	db := adminOrdersTestDB(t)
	withConnDBOrders(t, db)

	w, c := adminOrdersGinCtx("GET", "/api/gtk/v1/admin/orders?user_id=abc", nil, db)
	c.Request.URL.RawQuery = "user_id=abc"
	ListAdminOrdersAPI(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestListAdminOrdersAPI_UserIDFilter(t *testing.T) {
	db := adminOrdersTestDB(t)
	withConnDBOrders(t, db)
	seedAuthUser(t, db, 42, "alice")
	seedAuthUser(t, db, 99, "bob")
	seedCustomerOrder(t, db, "SVC-A", 42, "paid")
	seedCustomerOrder(t, db, "SVC-B", 99, "paid")

	w, c := adminOrdersGinCtx("GET", "/api/gtk/v1/admin/orders?user_id=42", nil, db)
	c.Request.URL.RawQuery = "user_id=42"
	ListAdminOrdersAPI(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Data struct {
			Orders []AdminOrderRow `json:"orders"`
			Total  int64           `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.Total != 1 || resp.Data.Orders[0].CoaiUserID != 42 {
		t.Errorf("user filter wrong: %+v", resp.Data)
	}
}
