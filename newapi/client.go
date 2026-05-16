// HTTP client for NewAPI v0.13.x admin REST API.
//
// Auth model:
//   - Header `Authorization: <admin_access_token>` (NO `Bearer ` prefix in
//     v0.13.x admin scope — that prefix is for sk-xxx api-key calls instead).
//   - Header `New-Api-User: <id>` to act on behalf of a specific user. Set
//     to the admin's own id for system-level calls; set to a target user id
//     when creating tokens for that user.
//
// Errors returned by Client methods include the NewAPI response body's
// `message` field so callers see why an admin call failed.
//
// Config (viper keys):
//   - newapi.base_url (default "http://newapi:3000")
//   - newapi.admin_user_id (default 2 — the bootstrap admin)
//   - newapi.admin_access_token (REQUIRED — 32-char value from users.access_token)
//   - newapi.timeout_seconds (default 10)

package newapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
)

// ErrNotConfigured is returned when newapi.admin_access_token is empty.
// Callers should treat this as "feature off" not "fatal" — provisioning
// is gated on configuration, not silent failure.
var ErrNotConfigured = errors.New("newapi: admin access token not configured")

// ErrUserNotFound / ErrTokenNotFound aid idempotent flows.
var (
	ErrUserNotFound  = errors.New("newapi: user not found")
	ErrTokenNotFound = errors.New("newapi: token not found")
)

// Client is the admin-scope HTTP client for NewAPI. Single shared instance
// is fine — all methods are safe for concurrent use (the underlying
// http.Client is concurrency-safe; we never mutate Client fields after init).
type Client struct {
	baseURL       string
	adminUserID   int64
	adminToken    string
	httpClient    *http.Client
}

var (
	defaultClient     *Client
	defaultClientOnce sync.Once
)

// Default returns the package-level Client built from viper config.
// Returns ErrNotConfigured if the admin token is missing — callers should
// log and skip provisioning rather than panic (this lets greentokey boot
// even when NewAPI integration is intentionally deferred).
func Default() (*Client, error) {
	defaultClientOnce.Do(func() {
		token := viper.GetString("newapi.admin_access_token")
		if token == "" {
			return // leave defaultClient nil; ErrNotConfigured surfaces below
		}
		baseURL := viper.GetString("newapi.base_url")
		if baseURL == "" {
			baseURL = "http://newapi:3000"
		}
		adminID := viper.GetInt64("newapi.admin_user_id")
		if adminID == 0 {
			adminID = 2 // historical bootstrap admin id
		}
		timeoutSec := viper.GetInt("newapi.timeout_seconds")
		if timeoutSec == 0 {
			timeoutSec = 10
		}
		defaultClient = &Client{
			baseURL:     baseURL,
			adminUserID: adminID,
			adminToken:  token,
			httpClient:  &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
		}
	})
	if defaultClient == nil {
		return nil, ErrNotConfigured
	}
	return defaultClient, nil
}

// IsConfigured returns true when newapi.admin_access_token is set. Useful
// for boot-time checks: log a warning if greentokey starts without NewAPI
// integration so silent misconfig doesn't bite later.
func IsConfigured() bool {
	return viper.GetString("newapi.admin_access_token") != ""
}

// do sends a request with admin auth headers + JSON envelope decoding.
// actingUserID == 0 means "act as admin" (uses adminUserID).
func (c *Client) do(ctx context.Context, method, path string, body any, actingUserID int64, decodeInto any) error {
	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("newapi: marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("newapi: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", c.adminToken)
	if actingUserID == 0 {
		actingUserID = c.adminUserID
	}
	req.Header.Set("New-Api-User", strconv.FormatInt(actingUserID, 10))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("newapi: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("newapi: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("newapi: %s %s: HTTP %d: %s", method, path, resp.StatusCode, truncate(string(respBody), 256))
	}
	if decodeInto != nil {
		if err := json.Unmarshal(respBody, decodeInto); err != nil {
			return fmt.Errorf("newapi: decode response: %w (body: %s)", err, truncate(string(respBody), 256))
		}
	}
	return nil
}

// SearchUserByUsername finds an existing NewAPI user by username (used for
// idempotency: greentokey user_id 42 maps to NewAPI username "gtk-42";
// before creating, we check if it already exists).
//
// Returns (user, nil) if found, (nil, ErrUserNotFound) if not, or wrapped
// error on transport / decode failures.
func (c *Client) SearchUserByUsername(ctx context.Context, username string) (*User, error) {
	// NewAPI: GET /api/user/search?keyword=<q> returns paginated matches
	// over username, display_name, email. We filter the results client-side
	// to ensure exact username match (the keyword search is permissive).
	var env listEnvelope[User]
	path := "/api/user/search?keyword=" + username
	if err := c.do(ctx, "GET", path, nil, 0, &env); err != nil {
		return nil, err
	}
	if !env.Success {
		return nil, fmt.Errorf("newapi: search user: %s", env.Message)
	}
	for i := range env.Data.Items {
		if env.Data.Items[i].Username == username {
			u := env.Data.Items[i]
			return &u, nil
		}
	}
	return nil, ErrUserNotFound
}

// CreateUser provisions a new NewAPI user. Username must be unique;
// password may be left empty (NewAPI auto-generates one — we don't need
// it because users authenticate via api-key, not username/password,
// once provisioned).
func (c *Client) CreateUser(ctx context.Context, req CreateUserRequest) (*User, error) {
	var env envelope[User]
	if err := c.do(ctx, "POST", "/api/user/", req, 0, &env); err != nil {
		return nil, err
	}
	if !env.Success {
		return nil, fmt.Errorf("newapi: create user: %s", env.Message)
	}
	return &env.Data, nil
}

// UpdateUserQuota sets the absolute quota on a user. NewAPI's quota unit
// is internal: $1 ≈ 500_000. Caller converts to that unit before calling.
//
// This is an absolute set (not a delta). Topping up = read current quota
// then set to current + topup.
func (c *Client) UpdateUserQuota(ctx context.Context, userID, quota int64) error {
	req := UpdateUserQuotaRequest{ID: userID, Quota: quota}
	var env envelope[any]
	if err := c.do(ctx, "PUT", "/api/user/", req, 0, &env); err != nil {
		return err
	}
	if !env.Success {
		return fmt.Errorf("newapi: update user quota: %s", env.Message)
	}
	return nil
}

// CreateToken issues an api-key (sk-xxx) for the target user.
//
// IMPORTANT: NewAPI scopes token-creation by the New-Api-User header;
// passing actingUserID = the target user's id makes the token belong to
// that user. Passing 0 (admin) creates an admin-owned token, which is
// NOT what end-user provisioning wants.
func (c *Client) CreateToken(ctx context.Context, targetUserID int64, req CreateTokenRequest) (*Token, error) {
	var env envelope[Token]
	if err := c.do(ctx, "POST", "/api/token/", req, targetUserID, &env); err != nil {
		return nil, err
	}
	if !env.Success {
		return nil, fmt.Errorf("newapi: create token: %s", env.Message)
	}
	return &env.Data, nil
}

// DisableToken flips a NewAPI token's status to 2 (disabled). Used by
// commerce/entitlement.go RevokeEntitlement on full-refund of a token
// plan: the user paid, got an sk-xxx api-key, then refunded — we have to
// stop that key from working before the refund settles or they'll keep
// using it on our dime.
//
// PKG-2 Wave 2.5 B3 added this. Mirrors UpdateUserQuota's partial-update
// pattern: send only id + status, NewAPI honors partial PUT body.
// Status=2 is NewAPI's "disabled" value (see types.go Token.Status:
// 1=enabled, 2=disabled).
//
// Idempotent on the NewAPI side: re-disabling an already-disabled token
// is a no-op success. Callers (RevokeEntitlement) rely on this for their
// own H4 idempotency contract — re-revoking should not error.
//
// Returns nil on success, ErrTokenNotFound when NewAPI replies success=false
// with a "token not found" message (caller should treat as already-removed,
// matches Revoke idempotency), or wrapped error on transport / decode
// failures (treated as transient by RevokeEntitlement, which still flips
// the local DB state).
func (c *Client) DisableToken(ctx context.Context, tokenID int64) error {
	// Reuse the partial-update endpoint shape (PUT /api/token/). We only
	// need to send id + status — NewAPI leaves other fields untouched.
	req := struct {
		ID     int64 `json:"id"`
		Status int   `json:"status"`
	}{ID: tokenID, Status: 2}
	var env envelope[any]
	if err := c.do(ctx, "PUT", "/api/token/", req, 0, &env); err != nil {
		return err
	}
	if !env.Success {
		// Common NewAPI error: "token不存在" / "token not found" when the
		// id is stale (e.g. an old binding referencing a token NewAPI
		// already pruned). Map to typed sentinel for caller convenience.
		if env.Message == "token不存在" || env.Message == "token not found" {
			return ErrTokenNotFound
		}
		return fmt.Errorf("newapi: disable token: %s", env.Message)
	}
	return nil
}

// RevokeUser deletes a NewAPI user via the admin API, revoking all tokens
// they hold. Used by bin/delete-customer-data.sh to cut external API access
// before clearing the local gtk_newapi_binding row.
//
// Status semantics:
//
//	"success" — NewAPI confirmed the user was deleted (200 + success=true)
//	"skipped" — user not found in NewAPI (404 or success=false "not found")
//	            treated as idempotent OK; repeat invocations are safe
//	"failed"  — unexpected error; caller should log and mark audit row
//
// NewAPI v0.13.x endpoint: DELETE /api/user/<id>  (admin scope, no body).
// A 404 from the HTTP layer surfaces as an error from do() because do()
// treats all 4xx as errors; we intercept that error string to detect 404.
func (c *Client) RevokeUser(ctx context.Context, newapiUserID int64) (status string, err error) {
	path := fmt.Sprintf("/api/user/%d", newapiUserID)
	var env envelope[any]
	doErr := c.do(ctx, "DELETE", path, nil, 0, &env)
	if doErr != nil {
		// do() includes "HTTP 404" in the error string for 404 responses.
		// Treat 404 as "user already gone" — idempotent OK.
		errStr := doErr.Error()
		if strings.Contains(errStr, "HTTP 404") {
			return "skipped", nil
		}
		return "failed", doErr
	}
	if !env.Success {
		// NewAPI sometimes returns 200 with success=false and a "not found"
		// message — map those to skipped too.
		msg := env.Message
		if strings.Contains(msg, "not found") || strings.Contains(msg, "不存在") {
			return "skipped", nil
		}
		return "failed", fmt.Errorf("newapi: revoke user %d: %s", newapiUserID, msg)
	}
	return "success", nil
}

// truncate keeps log strings bounded so a malformed response body doesn't
// flood logs.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
