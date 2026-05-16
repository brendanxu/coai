package newapi

// PKG-D5: unit tests for Client.RevokeUser using an httptest stub server.
// Three cases cover the three status values returned by RevokeUser:
//   - 200 + success=true  → "success"
//   - 404               → "skipped"  (user already gone, idempotent)
//   - 200 + success=false with "not found" body → "skipped"
//   - 500               → "failed"   (unexpected server error)

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestClient builds a Client pointed at the given stub server URL.
func newTestClient(baseURL string) *Client {
	return &Client{
		baseURL:     baseURL,
		adminUserID: 2,
		adminToken:  "test-token",
		httpClient:  &http.Client{Timeout: 5 * time.Second},
	}
}

func TestRevokeUser_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"success":true,"message":""}`)
	}))
	defer srv.Close()

	cli := newTestClient(srv.URL)
	status, err := cli.RevokeUser(t.Context(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != "success" {
		t.Errorf("want status=success, got %q", status)
	}
}

func TestRevokeUser_404Skipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"success":false,"message":"user not found"}`)
	}))
	defer srv.Close()

	cli := newTestClient(srv.URL)
	status, err := cli.RevokeUser(t.Context(), 999)
	if err != nil {
		t.Fatalf("404 should map to skipped (no error), got: %v", err)
	}
	if status != "skipped" {
		t.Errorf("want status=skipped, got %q", status)
	}
}

func TestRevokeUser_SuccessFalseNotFound_Skipped(t *testing.T) {
	// NewAPI occasionally returns HTTP 200 but success=false with a "not found"
	// body (e.g. when the user was already deleted by another process).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"success": false,
			"message": "用户不存在",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cli := newTestClient(srv.URL)
	status, err := cli.RevokeUser(t.Context(), 77)
	if err != nil {
		t.Fatalf("not-found body should map to skipped (no error), got: %v", err)
	}
	if status != "skipped" {
		t.Errorf("want status=skipped, got %q", status)
	}
}

func TestRevokeUser_ServerError_Failed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, `{"success":false,"message":"internal error"}`)
	}))
	defer srv.Close()

	cli := newTestClient(srv.URL)
	status, err := cli.RevokeUser(t.Context(), 55)
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
	if status != "failed" {
		t.Errorf("want status=failed, got %q", status)
	}
}
