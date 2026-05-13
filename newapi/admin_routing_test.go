// admin_routing_test.go — exercises the PKG-4 admin routing handlers
// (architecture §19, L23 Gap 7).
//
// Strategy:
//   - Use SQLite + connection.DB swap to drive the storage path under
//     test (same pattern as commerce/entitlement_test.go).
//   - For handler-level tests, drive each Handler() directly with a gin
//     context constructed via httptest.NewRecorder + gin.CreateTestContext.
//     The auth gate is satisfied by pre-seeding an admin auth row + setting
//     the gin context keys (utils.GetUserFromContext / GetDBFromContext).
//   - The listChannelsFn + syncBindingGroupFn package vars are swapped in
//     tests to avoid real NewAPI HTTP roundtrips.

package newapi

import (
	"bytes"
	"chat/connection"
	"chat/globals"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// adminTestDB spins up a SQLite in-memory DB with auth + gtk_newapi_binding
// seeded for admin tests. The 'admin' user is row id=1 with is_admin=1.
func adminTestDB(t *testing.T) *sql.DB {
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

	// auth schema (subset that newapi tests + the admin gate touch).
	if _, err := db.Exec(`
		CREATE TABLE auth (
		  id INTEGER PRIMARY KEY,
		  username TEXT UNIQUE,
		  is_admin INTEGER DEFAULT 0,
		  is_banned INTEGER DEFAULT 0
		)
	`); err != nil {
		t.Fatalf("seed auth: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO auth (id, username, is_admin) VALUES (1, 'admin-fixture', 1)`); err != nil {
		t.Fatalf("seed admin row: %v", err)
	}

	// Bare gtk_newapi_binding (skip plans/service migrations — admin
	// routing tests don't need pending_provisions, and pulling those in
	// would force seeding gtk_plan rows for FK satisfaction).
	if _, err := db.Exec(`
		CREATE TABLE gtk_newapi_binding (
		  coai_user_id      INTEGER PRIMARY KEY,
		  newapi_user_id    INTEGER NOT NULL UNIQUE,
		  newapi_token_id   INTEGER NOT NULL UNIQUE,
		  newapi_token_key  TEXT    NOT NULL,
		  newapi_group      TEXT    NOT NULL DEFAULT 'default',
		  last_known_quota  INTEGER NOT NULL DEFAULT 0,
		  created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  FOREIGN KEY (coai_user_id) REFERENCES auth(id) ON DELETE CASCADE
		)
	`); err != nil {
		t.Fatalf("seed gtk_newapi_binding: %v", err)
	}

	return db
}

// withConnDB swaps connection.DB for the test DB and restores on cleanup.
// Required because handlers (PoolAPI / BindingAPI / admin_routing) read
// the package-global connection.DB rather than DB-from-context.
func withConnDB(t *testing.T, db *sql.DB) {
	t.Helper()
	prev := connection.DB
	connection.DB = db
	t.Cleanup(func() { connection.DB = prev })
}

// seedBinding inserts an auth row + gtk_newapi_binding row for the given
// coai_user_id with the given group. Used to populate fixtures in the
// list/get/update tests.
func seedBinding(t *testing.T, db *sql.DB, coaiID int64, username, group string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO auth (id, username) VALUES (?, ?)`, coaiID, username); err != nil {
		t.Fatalf("seed auth(%d): %v", coaiID, err)
	}
	if _, err := db.Exec(`
		INSERT INTO gtk_newapi_binding
		  (coai_user_id, newapi_user_id, newapi_token_id, newapi_token_key, newapi_group, last_known_quota)
		VALUES (?, ?, ?, ?, ?, ?)
	`, coaiID, coaiID*10, coaiID*100, "sk-test-"+username, group, 500_000); err != nil {
		t.Fatalf("seed binding(%d): %v", coaiID, err)
	}
}

// adminGinCtx builds a gin.Context that satisfies auth.RequireAdmin: sets
// user="admin-fixture" + db=db on the context, so utils.GetUserFromContext
// + utils.GetDBFromContext + IsAdmin(db) all return the admin user.
//
// Returns the recorder + context. Caller wires path/query/body onto
// ctx.Request before invoking the handler.
func adminGinCtx(method, target string, body []byte, db *sql.DB) (*httptest.ResponseRecorder, *gin.Context) {
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
	// utils.GetUserFromContext / GetDBFromContext reach for these keys.
	c.Set("user", "admin-fixture")
	c.Set("db", db)
	return w, c
}

// stubChannels swaps listChannelsFn for the duration of the test.
func stubChannels(t *testing.T, fn func(ctx context.Context) ([]ChannelInfo, error)) {
	t.Helper()
	prev := listChannelsFn
	listChannelsFn = fn
	t.Cleanup(func() { listChannelsFn = prev })
}

// stubSyncBindingGroup swaps syncBindingGroupFn for the duration of the
// test so handlers exercise their branch-on-error paths without real
// NewAPI HTTP.
func stubSyncBindingGroup(t *testing.T, fn func(ctx context.Context, db *sql.DB, coaiUserID int64, newGroup string) error) {
	t.Helper()
	prev := syncBindingGroupFn
	syncBindingGroupFn = fn
	t.Cleanup(func() { syncBindingGroupFn = prev })
}

// ---------------------------------------------------------------------
// Storage layer tests (DB only — no HTTP).
// ---------------------------------------------------------------------

func TestListUserRouting_HappyPath(t *testing.T) {
	db := adminTestDB(t)
	seedBinding(t, db, 10, "alice", "default")
	seedBinding(t, db, 20, "bob", "official-api")
	seedBinding(t, db, 30, "carol", "friend-pool")

	rows, total, err := listUserRouting(db, "", 50, 0)
	if err != nil {
		t.Fatalf("listUserRouting: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(rows) != 3 {
		t.Errorf("len(rows) = %d, want 3", len(rows))
	}
	// ORDER BY coai_user_id ASC: alice first.
	if rows[0].Username != "alice" || rows[0].NewapiGroup != "default" {
		t.Errorf("rows[0] = %+v, want alice/default", rows[0])
	}
	if rows[2].Username != "carol" || rows[2].NewapiGroup != "friend-pool" {
		t.Errorf("rows[2] = %+v, want carol/friend-pool", rows[2])
	}
}

func TestListUserRouting_GroupFilter(t *testing.T) {
	db := adminTestDB(t)
	seedBinding(t, db, 10, "alice", "default")
	seedBinding(t, db, 20, "bob", "official-api")
	seedBinding(t, db, 21, "bobby", "official-api")

	rows, total, err := listUserRouting(db, "official-api", 50, 0)
	if err != nil {
		t.Fatalf("listUserRouting: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	for _, r := range rows {
		if r.NewapiGroup != "official-api" {
			t.Errorf("row %+v has wrong group", r)
		}
	}
}

func TestListUserRouting_Pagination(t *testing.T) {
	db := adminTestDB(t)
	for i := int64(10); i < 20; i++ {
		seedBinding(t, db, i, "user"+string(rune('0'+i-10)), "default")
	}

	rows, total, err := listUserRouting(db, "", 3, 5)
	if err != nil {
		t.Fatalf("listUserRouting: %v", err)
	}
	if total != 10 {
		t.Errorf("total = %d, want 10 (filter ignores limit/offset)", total)
	}
	if len(rows) != 3 {
		t.Errorf("len(rows) = %d, want 3 (limit honored)", len(rows))
	}
	// offset=5 + ORDER BY coai_user_id ASC starts at id=15.
	if rows[0].CoaiUserID != 15 {
		t.Errorf("rows[0].CoaiUserID = %d, want 15", rows[0].CoaiUserID)
	}
}

// ---------------------------------------------------------------------
// HTTP handler tests.
// ---------------------------------------------------------------------

func TestListUserRoutingAPI_AdminSucceeds(t *testing.T) {
	db := adminTestDB(t)
	withConnDB(t, db)
	seedBinding(t, db, 10, "alice", "default")

	w, c := adminGinCtx("GET", "/api/gtk/v1/admin/user-routing", nil, db)
	ListUserRoutingAPI(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Users []UserRoutingRow `json:"users"`
			Total int64            `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, w.Body.String())
	}
	if !resp.Success {
		t.Fatal("success=false")
	}
	if resp.Data.Total != 1 || len(resp.Data.Users) != 1 {
		t.Errorf("unexpected payload: %+v", resp.Data)
	}
	if resp.Data.Users[0].Username != "alice" {
		t.Errorf("username = %q, want alice", resp.Data.Users[0].Username)
	}
}

func TestGetUserRoutingAPI_NotFound(t *testing.T) {
	db := adminTestDB(t)
	withConnDB(t, db)

	w, c := adminGinCtx("GET", "/api/gtk/v1/admin/user-routing/999", nil, db)
	c.Params = gin.Params{{Key: "user_id", Value: "999"}}
	GetUserRoutingAPI(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestGetUserRoutingAPI_InvalidID(t *testing.T) {
	db := adminTestDB(t)
	withConnDB(t, db)

	w, c := adminGinCtx("GET", "/api/gtk/v1/admin/user-routing/abc", nil, db)
	c.Params = gin.Params{{Key: "user_id", Value: "abc"}}
	GetUserRoutingAPI(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestGetUserRoutingAPI_HappyPath(t *testing.T) {
	db := adminTestDB(t)
	withConnDB(t, db)
	seedBinding(t, db, 42, "charlie", "service-runtime")

	w, c := adminGinCtx("GET", "/api/gtk/v1/admin/user-routing/42", nil, db)
	c.Params = gin.Params{{Key: "user_id", Value: "42"}}
	GetUserRoutingAPI(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Success bool           `json:"success"`
		Data    UserRoutingRow `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.NewapiGroup != "service-runtime" {
		t.Errorf("group = %q, want service-runtime", resp.Data.NewapiGroup)
	}
}

func TestUpdateUserRoutingAPI_GroupChange(t *testing.T) {
	db := adminTestDB(t)
	withConnDB(t, db)
	seedBinding(t, db, 7, "dave", "default")

	// Stub SyncBindingGroup so we exercise the handler without a real
	// NewAPI roundtrip; mirror its DB-write side-effect manually.
	called := false
	stubSyncBindingGroup(t, func(ctx context.Context, db *sql.DB, coaiUserID int64, newGroup string) error {
		called = true
		if coaiUserID != 7 {
			t.Errorf("stub: coaiUserID=%d, want 7", coaiUserID)
		}
		if newGroup != "friend-pool" {
			t.Errorf("stub: newGroup=%q, want friend-pool", newGroup)
		}
		_, err := db.Exec(`UPDATE gtk_newapi_binding SET newapi_group=? WHERE coai_user_id=?`, newGroup, coaiUserID)
		return err
	})

	body := []byte(`{"newapi_group":"friend-pool"}`)
	w, c := adminGinCtx("PUT", "/api/gtk/v1/admin/user-routing/7", body, db)
	c.Params = gin.Params{{Key: "user_id", Value: "7"}}
	UpdateUserRoutingAPI(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("syncBindingGroupFn was not called")
	}
	// Confirm DB shows the new group.
	var got string
	if err := db.QueryRow(`SELECT newapi_group FROM gtk_newapi_binding WHERE coai_user_id=7`).Scan(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != "friend-pool" {
		t.Errorf("group after update = %q, want friend-pool", got)
	}
}

func TestUpdateUserRoutingAPI_RejectsEmptyGroup(t *testing.T) {
	db := adminTestDB(t)
	withConnDB(t, db)
	seedBinding(t, db, 7, "dave", "default")

	// Stub should NOT be called — validation fires first.
	stubSyncBindingGroup(t, func(ctx context.Context, db *sql.DB, coaiUserID int64, newGroup string) error {
		t.Fatal("syncBindingGroupFn called for empty group — validation skipped?")
		return nil
	})

	body := []byte(`{"newapi_group":"   "}`)
	w, c := adminGinCtx("PUT", "/api/gtk/v1/admin/user-routing/7", body, db)
	c.Params = gin.Params{{Key: "user_id", Value: "7"}}
	UpdateUserRoutingAPI(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateUserRoutingAPI_NoBindingReturns404(t *testing.T) {
	db := adminTestDB(t)
	withConnDB(t, db)

	stubSyncBindingGroup(t, func(ctx context.Context, db *sql.DB, coaiUserID int64, newGroup string) error {
		// Mirror SyncBindingGroup's "load binding" wrap when the row is missing.
		return errors.New("load binding: " + sql.ErrNoRows.Error())
	})

	body := []byte(`{"newapi_group":"x"}`)
	w, c := adminGinCtx("PUT", "/api/gtk/v1/admin/user-routing/999", body, db)
	c.Params = gin.Params{{Key: "user_id", Value: "999"}}
	UpdateUserRoutingAPI(c)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateUserRoutingAPI_NewAPIErrorReturns502(t *testing.T) {
	db := adminTestDB(t)
	withConnDB(t, db)
	seedBinding(t, db, 7, "dave", "default")

	stubSyncBindingGroup(t, func(ctx context.Context, db *sql.DB, coaiUserID int64, newGroup string) error {
		return errors.New("newapi update group: HTTP 500")
	})

	body := []byte(`{"newapi_group":"friend-pool"}`)
	w, c := adminGinCtx("PUT", "/api/gtk/v1/admin/user-routing/7", body, db)
	c.Params = gin.Params{{Key: "user_id", Value: "7"}}
	UpdateUserRoutingAPI(c)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", w.Code, w.Body.String())
	}
}

func TestListChannelsAPI_StubsNewAPI(t *testing.T) {
	db := adminTestDB(t)
	withConnDB(t, db)

	stubChannels(t, func(ctx context.Context) ([]ChannelInfo, error) {
		return []ChannelInfo{
			{ID: 1, Type: 43, Name: "deepseek-official", Status: 1, Group: "default", Models: []string{"deepseek-chat"}},
			{ID: 2, Type: 8, Name: "claude-pro-mirror", Status: 1, Group: "friend-pool", Models: []string{"claude-3-opus"}, IsSub2API: true},
		}, nil
	})

	w, c := adminGinCtx("GET", "/api/gtk/v1/admin/channels", nil, db)
	ListChannelsAPI(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			Channels []ChannelInfo `json:"channels"`
			Total    int           `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Fatal("success=false")
	}
	if resp.Data.Total != 2 || len(resp.Data.Channels) != 2 {
		t.Errorf("unexpected payload: total=%d, len=%d", resp.Data.Total, len(resp.Data.Channels))
	}
	if resp.Data.Channels[1].Group != "friend-pool" {
		t.Errorf("channels[1].Group = %q, want friend-pool", resp.Data.Channels[1].Group)
	}
}

func TestListChannelsAPI_DegradedWhenUnconfigured(t *testing.T) {
	db := adminTestDB(t)
	withConnDB(t, db)

	stubChannels(t, func(ctx context.Context) ([]ChannelInfo, error) {
		return nil, ErrNotConfigured
	})

	w, c := adminGinCtx("GET", "/api/gtk/v1/admin/channels", nil, db)
	ListChannelsAPI(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"degraded":true`) {
		t.Errorf("expected degraded marker; body=%s", w.Body.String())
	}
}

func TestUpdateChannelAPI_NotConfiguredReturns503(t *testing.T) {
	db := adminTestDB(t)
	withConnDB(t, db)

	// Without a configured NewAPI admin token, the handler must return 503.
	body := []byte(`{"type":1,"name":"test","key":"sk-test"}`)
	w, c := adminGinCtx("PUT", "/api/gtk/v1/admin/channels/1", body, db)
	c.Params = gin.Params{{Key: "channel_id", Value: "1"}}
	UpdateChannelAPI(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", w.Code, w.Body.String())
	}
}
