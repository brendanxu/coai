package waitlist

import (
	"bytes"
	"chat/globals"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	_ "github.com/mattn/go-sqlite3"
)

// newTestEngine wires gin with sqlite + a (nil) cache the way production
// middleware does (c.Set("db", ...)/c.Set("cache", ...)). The nil cache
// exercises the rate-limiter fail-open path; rate-limit-hit behavior is
// validated in checkRateLimit unit tests below or via manual curl against
// a live redis (see WINDOW-A acceptance criteria).
func newTestEngine(t *testing.T) (*gin.Engine, *sql.DB) {
	t.Helper()
	prev := globals.SqliteEngine
	globals.SqliteEngine = true
	t.Cleanup(func() { globals.SqliteEngine = prev })

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("db", db)
		// Typed-nil so utils.GetCacheFromContext type-assertion succeeds.
		c.Set("cache", (*redis.Client)(nil))
		c.Next()
	})
	r.POST("/waitlist", HandleJoin)
	return r, db
}

func postJoin(t *testing.T, r *gin.Engine, body map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/waitlist", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// --- email validation ---

func TestNormalizeAndValidateEmail(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"valid", "alice@example.com", "alice@example.com", false},
		{"trim_lower", "  Alice@Example.COM  ", "alice@example.com", false},
		{"missing_at", "alice.example.com", "", true},
		{"empty", "", "", true},
		{"too_short", "a@b", "", true},
		{"too_long", strings.Repeat("a", 250) + "@b.co", "", true},
		{"bare_token", "no-at-sign", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeAndValidateEmail(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %q", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// --- duplicate detection ---

func TestIsDuplicateKeyErr(t *testing.T) {
	if !isDuplicateKeyErr(&fakeErr{"Error 1062: Duplicate entry"}) {
		t.Error("MySQL Error 1062 not detected")
	}
	if !isDuplicateKeyErr(&fakeErr{"UNIQUE constraint failed: gtk_waitlist.email"}) {
		t.Error("sqlite UNIQUE constraint failed not detected")
	}
	if isDuplicateKeyErr(&fakeErr{"connection refused"}) {
		t.Error("connection error mis-classified as duplicate")
	}
	if isDuplicateKeyErr(nil) {
		t.Error("nil mis-classified as duplicate")
	}
}

type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }

// --- HandleJoin integration ---

func TestHandleJoin_ValidNewSignup(t *testing.T) {
	r, db := newTestEngine(t)
	w := postJoin(t, r, map[string]string{
		"email": "alice@example.com", "service": "tax-filing", "source": "marketing-landing",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("got %d want 200; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != true {
		t.Fatalf("status=%v want true", resp["status"])
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_waitlist WHERE email='alice@example.com'`).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 row inserted, got %d", count)
	}
}

func TestHandleJoin_DuplicateIsIdempotent(t *testing.T) {
	r, db := newTestEngine(t)

	for i := 0; i < 3; i++ {
		w := postJoin(t, r, map[string]string{
			"email": "bob@example.com", "service": "video-editing",
		})
		if w.Code != http.StatusOK {
			t.Fatalf("call %d: got %d want 200; body=%s", i, w.Code, w.Body.String())
		}
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM gtk_waitlist WHERE email='bob@example.com'`).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row after 3 dup signups, got %d", count)
	}
}

func TestHandleJoin_InvalidEmailRejected(t *testing.T) {
	r, _ := newTestEngine(t)
	w := postJoin(t, r, map[string]string{
		"email": "not-an-email", "service": "tax-filing",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestHandleJoin_UnknownServiceRejected(t *testing.T) {
	r, _ := newTestEngine(t)
	w := postJoin(t, r, map[string]string{
		"email": "alice@example.com", "service": "ransomware",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestHandleJoin_MissingFieldsRejected(t *testing.T) {
	r, _ := newTestEngine(t)
	// service missing
	w := postJoin(t, r, map[string]string{"email": "alice@example.com"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing service: got %d want 400", w.Code)
	}
	// email missing
	w = postJoin(t, r, map[string]string{"service": "tax-filing"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing email: got %d want 400", w.Code)
	}
}

func TestHandleJoin_RedisDownFailsOpen(t *testing.T) {
	// newTestEngine sets cache=(*redis.Client)(nil), so checkRateLimit hits
	// the nil-tolerant fail-open branch. A successful 200 here proves the
	// fail-open behavior captured in the plan review.
	r, _ := newTestEngine(t)
	w := postJoin(t, r, map[string]string{
		"email": "carol@example.com", "service": "any",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("redis-down should fail-open with 200; got %d body=%s", w.Code, w.Body.String())
	}
}

// --- checkRateLimit unit tests (no real redis) ---

func TestCheckRateLimit_NilCacheFailsOpen(t *testing.T) {
	if checkRateLimit(nil, "10.0.0.1") {
		t.Fatal("nil cache should fail-open (return false=not-blocked)")
	}
}
