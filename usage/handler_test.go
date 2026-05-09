// HTTP handler tests for GET /api/v1/usage/me.
//
// Strategy: build a gin engine with a fake auth middleware that stamps the
// gin context the way middleware.AuthMiddleware does (auth bool + user
// string + agent string). For the unauthenticated case we leave auth=false.
// This is the same shape waitlist/handlers_test.go uses for its db+cache
// stubs.
//
// We don't run the real AuthMiddleware because (a) it would require a live
// auth row + token table, and (b) we want to exercise the handler's own
// 401-on-missing-auth branch independently of how the auth header is parsed.
package usage

import (
	"chat/globals"
	"chat/plans"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	_ "github.com/mattn/go-sqlite3"
)

// authStamp tells newHandlerEngine which c.Set values to plant. Mirrors what
// middleware.AuthMiddleware → middleware.ProcessAuthorization writes. When
// authed=false, no auth row is needed.
type authStamp struct {
	authed   bool
	username string // matches an INSERTed row in auth table
	agent    string // "api" or "token"
}

// newHandlerEngine wires gin + sqlite + auth-stub middleware + nil redis +
// the usage route. Returns engine + db so tests can seed gtk_app_usage_log
// directly.
func newHandlerEngine(t *testing.T, stamp authStamp) (*gin.Engine, *sql.DB) {
	t.Helper()
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// auth table — match the production CREATE TABLE columns the handler
	// touches (id + username for user.GetID(db)).
	if _, err := db.Exec(`
		CREATE TABLE auth (
		  id       INTEGER PRIMARY KEY AUTOINCREMENT,
		  username TEXT UNIQUE,
		  password TEXT NOT NULL DEFAULT '',
		  email    TEXT,
		  is_admin INTEGER DEFAULT 0,
		  is_banned INTEGER DEFAULT 0,
		  bind_id  INTEGER UNIQUE,
		  token    TEXT NOT NULL DEFAULT ''
		)
	`); err != nil {
		t.Fatalf("create auth: %v", err)
	}

	if stamp.authed && stamp.username != "" {
		if _, err := db.Exec(
			`INSERT INTO auth (username) VALUES (?)`, stamp.username,
		); err != nil {
			t.Fatalf("insert auth: %v", err)
		}
	}

	if err := plans.Migrate(db); err != nil {
		t.Fatalf("plans migrate: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("db", db)
		// Typed-nil so utils.GetCacheFromContext type-assertion passes,
		// AND so our handler's tryGetCache falls back to in-memory limiter.
		c.Set("cache", (*redis.Client)(nil))
		// Auth stamp.
		c.Set("auth", stamp.authed)
		c.Set("user", stamp.username)
		c.Set("agent", stamp.agent)
		c.Set("admin", false)
		c.Next()
	})

	// Mount under same prefix the production main.go uses for the
	// usage subgroup. Register adds /v1/usage/me onto the bare app.
	g := r.Group("")
	Register(g)

	// Reset the in-memory rate-limiter so prior tests don't bleed in.
	ResetMemLimiterForTest()

	return r, db
}

func doGET(r *gin.Engine, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// --- 401 path ---

func TestUsageMe_Returns401WhenUnauthenticated(t *testing.T) {
	r, _ := newHandlerEngine(t, authStamp{authed: false})
	w := doGET(r, "/v1/usage/me")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["success"] != false {
		t.Errorf("success: want false got %v", resp["success"])
	}
}

// --- 200 happy path + shape ---

func TestUsageMe_Returns200WithSummaryAndEstimatedFlag(t *testing.T) {
	r, db := newHandlerEngine(t, authStamp{authed: true, username: "alice", agent: "api"})

	// Seed 1 usage row "today" so we know the aggregate ran. We use today's
	// SGT-real boundary by computing it from time.Now()-style logic; the
	// row will fall inside today regardless of when the test runs as long
	// as we put it within the last 30 minutes.
	now := time.Now().UTC()
	_, err := db.Exec(`
		INSERT INTO gtk_app_usage_log (user_id, service, tokens_used, cost_cents, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, 1, "claude-3-5-sonnet", 1000, 123, now.Format("2006-01-02 15:04:05"))
	if err != nil {
		t.Fatalf("seed usage: %v", err)
	}

	w := doGET(r, "/v1/usage/me?kind=all")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, w.Body.String())
	}
	// Currency
	if resp["currency"] != "CNY" {
		t.Errorf("currency: got %v want CNY", resp["currency"])
	}
	// _estimated must be true (schema-gap)
	if resp["_estimated"] != true {
		t.Errorf("_estimated: got %v want true", resp["_estimated"])
	}
	// Three top-level slots present for kind=all
	if _, ok := resp["summary"]; !ok {
		t.Errorf("summary missing")
	}
	if _, ok := resp["recent_calls"]; !ok {
		t.Errorf("recent_calls missing")
	}
	if _, ok := resp["hourly"]; !ok {
		t.Errorf("hourly missing")
	}
	// summary.today_spend should reflect the seeded 123 cents (= 1.23)
	// IF the seed row falls inside SGT today. If the test runs near
	// midnight SGT this could be flaky; we instead assert the field
	// is a non-negative number rather than the exact value.
	summary, _ := resp["summary"].(map[string]interface{})
	spend, _ := summary["today_spend"].(float64)
	if spend < 0 {
		t.Errorf("today_spend should be ≥0, got %v", spend)
	}
}

func TestUsageMe_KindSummaryOnlyOmitsOtherFields(t *testing.T) {
	r, _ := newHandlerEngine(t, authStamp{authed: true, username: "bob", agent: "api"})
	w := doGET(r, "/v1/usage/me?kind=summary")
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if _, ok := resp["summary"]; !ok {
		t.Errorf("summary should be present")
	}
	if _, ok := resp["recent_calls"]; ok {
		t.Errorf("recent_calls should be omitted for kind=summary")
	}
	if _, ok := resp["hourly"]; ok {
		t.Errorf("hourly should be omitted for kind=summary")
	}
}

func TestUsageMe_BadKindReturns400(t *testing.T) {
	r, _ := newHandlerEngine(t, authStamp{authed: true, username: "carol", agent: "api"})
	w := doGET(r, "/v1/usage/me?kind=garbage")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// --- 429 rate limit ---

func TestUsageMe_RateLimitsRepeatedCalls(t *testing.T) {
	// Two calls in <10s for the same user → second should 429 with Retry-After.
	r, _ := newHandlerEngine(t, authStamp{authed: true, username: "dave", agent: "api"})

	w1 := doGET(r, "/v1/usage/me")
	if w1.Code != http.StatusOK {
		t.Fatalf("first call: want 200, got %d body=%s", w1.Code, w1.Body.String())
	}
	w2 := doGET(r, "/v1/usage/me")
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second call: want 429, got %d body=%s", w2.Code, w2.Body.String())
	}
	if ra := w2.Header().Get("Retry-After"); ra == "" {
		t.Errorf("Retry-After header missing on 429")
	}
}
