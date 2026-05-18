// handler_test.go — PKG-A-4 integration tests for the /v1/* relay handler.
//
// Strategy:
//   - SQLite in-memory DB (same pattern as waitlist and newapi tests).
//   - httptest.NewServer mocks NewAPI :3000 — no real network calls.
//   - newAPIBase is overridden per test to point at the mock server.
//   - AuthMiddleware is NOT used; tests inject "auth", "user" into context
//     directly via a setup middleware, testing the handler in isolation.
//
// Cases:
//   TestRelay_HappyPath           — sk-xxx in apikey table + binding → 200, body+header forwarded
//   TestRelay_NoAuth              — no auth header → 401
//   TestRelay_ValidKeyNobinding   — sk-xxx valid but no gtk_newapi_binding row → 401
//   TestRelay_StreamingSSE        — mock returns chunked SSE → client receives all chunks
//   TestRelay_UpstreamError       — mock server closes connection → 502

package relay

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"chat/globals"

	"github.com/gin-gonic/gin"
	_ "github.com/mattn/go-sqlite3"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// relayTestDB creates an in-memory SQLite DB with:
//   - auth table (id, username)
//   - gtk_newapi_binding table (coai_user_id → newapi_user_id etc.)
func relayTestDB(t *testing.T) *sql.DB {
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

	// Minimal auth schema.
	if _, err := db.Exec(`
		CREATE TABLE auth (
		  id INTEGER PRIMARY KEY,
		  username TEXT UNIQUE NOT NULL
		)
	`); err != nil {
		t.Fatalf("create auth: %v", err)
	}

	// Minimal gtk_newapi_binding schema.
	if _, err := db.Exec(`
		CREATE TABLE gtk_newapi_binding (
		  coai_user_id      INTEGER PRIMARY KEY,
		  newapi_user_id    INTEGER NOT NULL,
		  newapi_token_id   INTEGER NOT NULL DEFAULT 0,
		  newapi_token_key  TEXT    NOT NULL DEFAULT '',
		  newapi_group      TEXT    NOT NULL DEFAULT 'default',
		  last_known_quota  INTEGER NOT NULL DEFAULT 0,
		  created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		  updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		t.Fatalf("create gtk_newapi_binding: %v", err)
	}

	return db
}

// seedUser inserts a user row and returns the user id.
func seedUser(t *testing.T, db *sql.DB, username string) int64 {
	t.Helper()
	res, err := db.Exec("INSERT INTO auth (username) VALUES (?)", username)
	if err != nil {
		t.Fatalf("seed user %q: %v", username, err)
	}
	id, _ := res.LastInsertId()
	return id
}

// seedBinding inserts a gtk_newapi_binding row for the given coai_user_id.
func seedBinding(t *testing.T, db *sql.DB, coaiUserID, newAPIUserID int64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO gtk_newapi_binding
		  (coai_user_id, newapi_user_id, newapi_token_id, newapi_token_key, newapi_group, last_known_quota)
		VALUES (?, ?, 0, 'placeholder', 'default', 0)
	`, coaiUserID, newAPIUserID); err != nil {
		t.Fatalf("seed binding: %v", err)
	}
}

// newRelayEngine creates a gin engine with a per-test middleware that:
//   - injects the test DB as "db" in context (so utils.GetDBFromContext works)
//   - sets "auth" and "user" from the supplied authFunc
//
// authFunc returns (isAuthenticated, username).
func newRelayEngine(t *testing.T, db *sql.DB, authFunc func(*http.Request) (bool, string)) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("db", db)
		ok, uname := authFunc(c.Request)
		c.Set("auth", ok)
		c.Set("user", uname)
		c.Next()
	})
	r.Any("/v1/*path", HandleRelay)
	return r
}

// mockNewAPI starts an httptest.Server that responds to all /v1/* requests
// with the given status code, headers, and body.
// Returns the server (Cleanup registered) and its URL.
func mockNewAPI(t *testing.T, status int, headers map[string]string, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// overrideNewAPIBase points newAPIBase at the given server URL for the duration
// of the test, then restores the original function.
func overrideNewAPIBase(t *testing.T, baseURL string) {
	t.Helper()
	orig := newAPIBase
	newAPIBase = func() string { return baseURL }
	t.Cleanup(func() { newAPIBase = orig })
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

// TestRelay_HappyPath: authenticated user with valid binding → request forwarded,
// response status + body + custom header returned to caller.
func TestRelay_HappyPath(t *testing.T) {
	db := relayTestDB(t)
	uid := seedUser(t, db, "alice")
	seedBinding(t, db, uid, 42)

	upstream := mockNewAPI(t, 200,
		map[string]string{"X-Upstream-Id": "test-42"},
		`{"object":"chat.completion"}`,
	)
	overrideNewAPIBase(t, upstream.URL)

	r := newRelayEngine(t, db, func(_ *http.Request) (bool, string) {
		return true, "alice"
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4","messages":[]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "chat.completion") {
		t.Fatalf("body missing expected content; got %q", w.Body.String())
	}
	if w.Header().Get("X-Upstream-Id") != "test-42" {
		t.Fatalf("custom header not forwarded; got %q", w.Header().Get("X-Upstream-Id"))
	}
}

// TestRelay_NoAuth: no auth set → 401 with OpenAI-style error body.
func TestRelay_NoAuth(t *testing.T) {
	db := relayTestDB(t)

	// upstream never reached
	r := newRelayEngine(t, db, func(_ *http.Request) (bool, string) {
		return false, ""
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	errObj, _ := resp["error"].(map[string]interface{})
	if errObj == nil {
		t.Fatal("missing error object in response")
	}
	if errObj["code"] != "invalid_api_key" {
		t.Fatalf("unexpected error code: %v", errObj["code"])
	}
}

// TestRelay_ValidKeyNoBinding: user authenticates successfully but has no binding
// in gtk_newapi_binding → 401 with code=no_upstream_binding.
func TestRelay_ValidKeyNoBinding(t *testing.T) {
	db := relayTestDB(t)
	seedUser(t, db, "bob") // no binding row

	// upstream server exists but should NOT be called
	upstream := mockNewAPI(t, 200, nil, `{}`)
	overrideNewAPIBase(t, upstream.URL)

	r := newRelayEngine(t, db, func(_ *http.Request) (bool, string) {
		return true, "bob"
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	errObj, _ := resp["error"].(map[string]interface{})
	if errObj == nil {
		t.Fatal("missing error object")
	}
	if errObj["code"] != "no_upstream_binding" {
		t.Fatalf("unexpected error code: %v", errObj["code"])
	}
}

// TestRelay_UserNotInDB: auth=true but username doesn't exist in auth table → 401.
func TestRelay_UserNotInDB(t *testing.T) {
	db := relayTestDB(t)
	// Do NOT seed "ghost" user

	r := newRelayEngine(t, db, func(_ *http.Request) (bool, string) {
		return true, "ghost"
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	errObj, _ := resp["error"].(map[string]interface{})
	if errObj == nil {
		t.Fatal("missing error object")
	}
	// code is "user_not_found"
	if errObj["code"] != "user_not_found" {
		t.Fatalf("unexpected error code: %v", errObj["code"])
	}
}

// TestRelay_StreamingSSE: upstream responds with SSE-style chunked body →
// all chunks reach the caller (tests the flusher path).
func TestRelay_StreamingSSE(t *testing.T) {
	db := relayTestDB(t)
	uid := seedUser(t, db, "carol")
	seedBinding(t, db, uid, 99)

	// SSE body: two event chunks.
	sseBody := "data: {\"delta\":\"hello\"}\n\ndata: {\"delta\":\" world\"}\n\ndata: [DONE]\n\n"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(200)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("httptest.ResponseRecorder should support Flusher")
			return
		}
		for _, chunk := range strings.Split(sseBody, "\n\n") {
			if chunk == "" {
				continue
			}
			fmt.Fprintf(w, "%s\n\n", chunk)
			flusher.Flush()
		}
	}))
	t.Cleanup(upstream.Close)
	overrideNewAPIBase(t, upstream.URL)

	r := newRelayEngine(t, db, func(_ *http.Request) (bool, string) {
		return true, "carol"
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"stream":true,"model":"gpt-4","messages":[]}`))
	req.Header.Set("Accept", "text/event-stream")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "hello") || !strings.Contains(body, "[DONE]") {
		t.Fatalf("SSE body not fully forwarded; got %q", body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type not forwarded; got %q", ct)
	}
}

// TestRelay_UpstreamError: upstream closes connection immediately → 502.
func TestRelay_UpstreamError(t *testing.T) {
	db := relayTestDB(t)
	uid := seedUser(t, db, "dave")
	seedBinding(t, db, uid, 7)

	// Server that closes immediately without a response.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			// Fallback: just return 503 so the test can still verify error handling.
			w.WriteHeader(503)
			return
		}
		conn, _, _ := hj.Hijack()
		conn.Close() // close raw TCP — causes http.Client to get "EOF"
	}))
	t.Cleanup(upstream.Close)
	overrideNewAPIBase(t, upstream.URL)

	r := newRelayEngine(t, db, func(_ *http.Request) (bool, string) {
		return true, "dave"
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Either 502 (upstream EOF) or 503 (fallback path above) is acceptable.
	if w.Code != http.StatusBadGateway && w.Code != http.StatusServiceUnavailable {
		// Attempt to decode upstream body if it forwarded a non-502
		body, _ := io.ReadAll(w.Body)
		t.Fatalf("want 502 or 503, got %d; body=%s", w.Code, body)
	}
}

// TestRelay_GetModels: GET /v1/models (no request body) → forwarded correctly.
func TestRelay_GetModels(t *testing.T) {
	db := relayTestDB(t)
	uid := seedUser(t, db, "eve")
	seedBinding(t, db, uid, 55)

	upstream := mockNewAPI(t, 200,
		map[string]string{"Content-Type": "application/json"},
		`{"object":"list","data":[]}`,
	)
	overrideNewAPIBase(t, upstream.URL)

	r := newRelayEngine(t, db, func(_ *http.Request) (bool, string) {
		return true, "eve"
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("want 200, got %d; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"object":"list"`) {
		t.Fatalf("unexpected body: %q", w.Body.String())
	}
}
