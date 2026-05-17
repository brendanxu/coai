// admin_tokens.go — personal access token management for PKG-A-3 Wave 1.
//
// Exposes NewAPI token CRUD as greentokey Go functions + HTTP handlers.
// Callers talk to this file; this file talks to NewAPI via the Client HTTP
// wrapper (no direct DB reads of NewAPI's own tables — that would violate
// 三条隔离原则).
//
// Wave 1.5b data-source correction (GetTokenUsage):
// Why query NewAPI logs via REST API (not gtk_app_usage_log):
// chat completion writes usage to NewAPI's native logs table (via NewAPI's
// own relay path); greentokey gtk_app_usage_log only captures service-order
// calls via commerce.WriteUsageCost. The usage/WriteUsageLog path (chat
// handler) does not set token_id, so querying gtk_app_usage_log WHERE
// token_id=X always returns 0 for chat-completion traffic.
// Until a future PKG migrates chat completion to commerce.WriteUsageCost
// with token_id populated, NewAPI REST API (GET /api/log/?token_id=<id>)
// is the source of truth for per-token chat usage.
//
// Architecture invariants respected:
//   - L23: binding is 1:1 (coai_user ↔ newapi_user), tokens are 1:N under binding
//   - No gtk_user_tokens mirror table — we read/write NewAPI tokens directly
//   - sk-xxx plaintext is returned ONLY on create; subsequent list calls mask
//   - Revoke = soft delete (status=2); last-token guard enforced server-side
//   - Max 10 active tokens per user; enforced before NewAPI call
//
// HTTP routes registered in router.go:
//
//	User-side (5 routes):
//	  GET    /api/gtk/v1/tokens
//	  POST   /api/gtk/v1/tokens
//	  PATCH  /api/gtk/v1/tokens/:id
//	  DELETE /api/gtk/v1/tokens/:id
//	  GET    /api/gtk/v1/tokens/:id/usage
//
//	Admin-side (3 routes, registered in admin_routing.go):
//	  GET    /api/gtk/v1/admin/tokens
//	  DELETE /api/gtk/v1/admin/tokens/:id
//	  GET    /api/gtk/v1/admin/tokens/:id/audit

package newapi

import (
	"chat/auth"
	"chat/connection"
	"chat/globals"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

// ErrCannotRevokeLastToken is returned when a user tries to revoke their only
// remaining active token. This prevents users from accidentally locking
// themselves out. Admin force-revoke bypasses this guard.
var ErrCannotRevokeLastToken = errors.New("newapi: cannot revoke last active token")

// ErrMaxTokensReached is returned when a user tries to create an 11th token.
// Hard limit is 10 active tokens per greentokey user.
var ErrMaxTokensReached = errors.New("newapi: max tokens per user reached (limit 10)")

// ErrTokenNotOwnedByUser is returned when a token operation targets a token
// that does not belong to the requesting user's NewAPI account.
var ErrTokenNotOwnedByUser = errors.New("newapi: token does not belong to this user")

const maxTokensPerUser = 10

// createTokenMu provides per-user mutual exclusion around the count-then-create
// pattern in CreateUserToken. Keyed by coai_user_id (int64).
// This is sufficient for single-instance deployments. For multi-instance
// deployments a distributed lock (e.g. Redis SETNX) would be required — add
// that when horizontal scaling is needed.
var createTokenMu sync.Map

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// UpdateTokenRequest holds the fields a user can mutate on an existing token.
// Zero values are ignored — only non-zero fields are sent to NewAPI.
type UpdateTokenRequest struct {
	Name        string `json:"name,omitempty"`
	ExpiredTime int64  `json:"expired_time,omitempty"` // unix seconds; -1 = never
	RemainQuota int64  `json:"remain_quota,omitempty"`
}

// TokenUsage is the per-token usage aggregation sourced from NewAPI's
// native logs (fetched via GET /api/log/?token_id=<id>).
type TokenUsage struct {
	TokenID         int64          `json:"token_id"`
	TotalCalls      int64          `json:"total_calls"`
	TotalTokensUsed int64          `json:"total_tokens_used"`
	InputTokens     int64          `json:"input_tokens"`
	OutputTokens    int64          `json:"output_tokens"`
	ByModel         []UsageByModel `json:"by_model"`
}

// NewAPILogEntry is one row from NewAPI's GET /api/log/ endpoint.
// Only the fields greentokey needs for usage aggregation are mapped; the
// full NewAPI log record has additional fields (channel_id, type, etc.)
// that we discard.
type NewAPILogEntry struct {
	Model            string `json:"model_name"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	// Quota is NewAPI's internal cost unit for this call (not directly
	// used in aggregation but available for future billing attribution).
	Quota int64 `json:"quota"`
}

// MaskedToken is a Token with its Key field masked for list responses.
// Only creation returns the full plaintext key.
type MaskedToken struct {
	ID             int64 `json:"id"`
	UserID         int64 `json:"user_id"`
	Name           string `json:"name"`
	Key            string `json:"key"` // masked: "sk-tnx-***-abcd"
	Status         int    `json:"status"`
	RemainQuota    int64  `json:"remain_quota"`
	UnlimitedQuota bool   `json:"unlimited_quota"`
	ExpiredTime    int64  `json:"expired_time"`
}

// maskKey returns a partially-redacted form of an sk-xxx key.
// We show the first 6 chars + "..." + last 4 chars.
func maskKey(key string) string {
	if len(key) <= 10 {
		return "***"
	}
	return key[:6] + "..." + key[len(key)-4:]
}

// tokenFromNewAPI converts a NewAPI Token into a MaskedToken for list/get responses.
func tokenFromNewAPI(t *Token) MaskedToken {
	return MaskedToken{
		ID:             t.ID,
		UserID:         t.UserID,
		Name:           t.Name,
		Key:            maskKey(t.Key),
		Status:         t.Status,
		RemainQuota:    t.RemainQuota,
		UnlimitedQuota: t.UnlimitedQuota,
		ExpiredTime:    t.ExpiredTime,
	}
}

// ---------------------------------------------------------------------------
// tokenClientIface — swapped in tests to avoid real HTTP calls
// ---------------------------------------------------------------------------

type tokenClientIface interface {
	listTokensForUser(ctx context.Context, newapiUserID int64) ([]*Token, error)
	createToken(ctx context.Context, newapiUserID int64, req CreateTokenRequest) (*Token, error)
	updateToken(ctx context.Context, tokenID int64, req UpdateTokenRequest) error
	disableToken(ctx context.Context, tokenID int64) error
	// fetchTokenLogsPage returns one page of NewAPI log entries for the given
	// token_id via GET /api/log/?p=<page>&size=<size>&token_id=<id>.
	// Callers use GetTokenUsage which loops pages until exhausted.
	fetchTokenLogsPage(ctx context.Context, tokenID int64, page, size int) ([]NewAPILogEntry, error)
}

// realNewAPIClient wraps the package Client to satisfy tokenClientIface.
type realNewAPIClient struct {
	c *Client
}

func (r *realNewAPIClient) listTokensForUser(ctx context.Context, newapiUserID int64) ([]*Token, error) {
	path := fmt.Sprintf("/api/token/?user_id=%d", newapiUserID)
	var env struct {
		Success bool     `json:"success"`
		Message string   `json:"message,omitempty"`
		Data    []*Token `json:"data"`
	}
	if err := r.c.do(ctx, "GET", path, nil, newapiUserID, &env); err != nil {
		return nil, fmt.Errorf("newapi: list tokens for user %d: %w", newapiUserID, err)
	}
	if !env.Success {
		return nil, fmt.Errorf("newapi: list tokens: %s", env.Message)
	}
	return env.Data, nil
}

func (r *realNewAPIClient) createToken(ctx context.Context, newapiUserID int64, req CreateTokenRequest) (*Token, error) {
	return r.c.CreateToken(ctx, newapiUserID, req)
}

func (r *realNewAPIClient) updateToken(ctx context.Context, tokenID int64, req UpdateTokenRequest) error {
	body := struct {
		ID          int64  `json:"id"`
		Name        string `json:"name,omitempty"`
		ExpiredTime int64  `json:"expired_time,omitempty"`
		RemainQuota int64  `json:"remain_quota,omitempty"`
	}{
		ID:          tokenID,
		Name:        req.Name,
		ExpiredTime: req.ExpiredTime,
		RemainQuota: req.RemainQuota,
	}
	var env envelope[any]
	if err := r.c.do(ctx, "PUT", "/api/token/", body, 0, &env); err != nil {
		return err
	}
	if !env.Success {
		if env.Message == "token不存在" || env.Message == "token not found" {
			return ErrTokenNotFound
		}
		return fmt.Errorf("newapi: update token: %s", env.Message)
	}
	return nil
}

func (r *realNewAPIClient) disableToken(ctx context.Context, tokenID int64) error {
	return r.c.DisableToken(ctx, tokenID)
}

// fetchTokenLogsPage calls GET /api/log/?p=<page>&size=<size>&token_id=<id>
// and returns one page of log entries.
//
// NewAPI v0.13.x log endpoint returns a paginated list; the response shape
// is {"success":true,"data":{"items":[...],"total":N,...}}.
func (r *realNewAPIClient) fetchTokenLogsPage(ctx context.Context, tokenID int64, page, size int) ([]NewAPILogEntry, error) {
	path := fmt.Sprintf("/api/log/?p=%d&size=%d&token_id=%d", page, size, tokenID)
	var env listEnvelope[NewAPILogEntry]
	if err := r.c.do(ctx, "GET", path, nil, 0, &env); err != nil {
		return nil, fmt.Errorf("newapi: fetch logs page %d for token %d: %w", page, tokenID, err)
	}
	if !env.Success {
		return nil, fmt.Errorf("newapi: fetch logs: %s", env.Message)
	}
	return env.Data.Items, nil
}

// ---------------------------------------------------------------------------
// tokenManager — business logic layer
// ---------------------------------------------------------------------------

// tokenManager holds the dependencies for token management operations.
// In production, cli is a *realNewAPIClient wrapping the package Client.
// In tests, cli is a *fakeNewAPIClient.
type tokenManager struct {
	db  *sql.DB
	cli tokenClientIface
}

// defaultManager returns a tokenManager backed by the package-level Client
// and connection.DB. Used by HTTP handlers.
func defaultManager() (*tokenManager, error) {
	cli, err := Default()
	if err != nil {
		return nil, err
	}
	return &tokenManager{db: connection.DB, cli: &realNewAPIClient{c: cli}}, nil
}

// bindingForUser loads the gtk_newapi_binding for coaiUserID.
// Returns (nil, sql.ErrNoRows) when not provisioned.
func (m *tokenManager) bindingForUser(coaiUserID int64) (*Binding, error) {
	return LoadBinding(m.db, coaiUserID)
}

// ListUserTokens returns all tokens (active + disabled) for a greentokey
// user. Keys are masked in the returned slice — callers must NOT forward
// the NewAPI plaintext key from here.
func (m *tokenManager) ListUserTokens(ctx context.Context, coaiUserID int64) ([]MaskedToken, error) {
	bind, err := m.bindingForUser(coaiUserID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: load binding: %w", err)
	}
	raw, err := m.cli.listTokensForUser(ctx, bind.NewapiUserID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	out := make([]MaskedToken, 0, len(raw))
	for _, t := range raw {
		out = append(out, tokenFromNewAPI(t))
	}
	return out, nil
}

// activeTokenCount returns how many active (status=1) tokens the NewAPI user
// currently holds. Used for max-10 enforcement and last-token guard.
func (m *tokenManager) activeTokenCount(ctx context.Context, newapiUserID int64) (int, error) {
	toks, err := m.cli.listTokensForUser(ctx, newapiUserID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, t := range toks {
		if t.Status == 1 {
			n++
		}
	}
	return n, nil
}

// ownsToken returns true when newapiUserID owns the token with tokenID.
func (m *tokenManager) ownsToken(ctx context.Context, newapiUserID, tokenID int64) (bool, error) {
	toks, err := m.cli.listTokensForUser(ctx, newapiUserID)
	if err != nil {
		return false, err
	}
	for _, t := range toks {
		if t.ID == tokenID {
			return true, nil
		}
	}
	return false, nil
}

// CreateUserToken creates a new NewAPI token for the greentokey user and
// returns it with the FULL plaintext key. This is the ONLY call-site where
// the plaintext sk-xxx must be returned. All subsequent reads must mask.
//
// Returns ErrMaxTokensReached when the user already has 10 active tokens.
//
// A per-user mutex (createTokenMu) guards the count-then-create sequence to
// prevent concurrent requests from racing past the max-10 check. This is
// correct for single-instance deployments; for multi-instance a distributed
// lock would be required (see createTokenMu declaration for details).
func (m *tokenManager) CreateUserToken(ctx context.Context, coaiUserID int64, req CreateTokenRequest) (*Token, error) {
	// Acquire per-user lock to prevent concurrent create requests from
	// both reading count=9 and both succeeding, yielding 11 tokens.
	lockAny, _ := createTokenMu.LoadOrStore(coaiUserID, &sync.Mutex{})
	lock := lockAny.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	bind, err := m.bindingForUser(coaiUserID)
	if err != nil {
		return nil, fmt.Errorf("create token: load binding: %w", err)
	}

	count, err := m.activeTokenCount(ctx, bind.NewapiUserID)
	if err != nil {
		return nil, fmt.Errorf("create token: count active: %w", err)
	}
	if count >= maxTokensPerUser {
		return nil, ErrMaxTokensReached
	}

	tok, err := m.cli.createToken(ctx, bind.NewapiUserID, req)
	if err != nil {
		return nil, fmt.Errorf("create token: %w", err)
	}
	// Return the full plaintext token — the HTTP handler MUST forward this
	// exactly once in the create response and never again.
	return tok, nil
}

// UpdateToken modifies name / expire / quota on an existing token.
// Verifies the token belongs to the user before updating.
func (m *tokenManager) UpdateToken(ctx context.Context, coaiUserID, tokenID int64, req UpdateTokenRequest) error {
	bind, err := m.bindingForUser(coaiUserID)
	if err != nil {
		return fmt.Errorf("update token: load binding: %w", err)
	}
	owns, err := m.ownsToken(ctx, bind.NewapiUserID, tokenID)
	if err != nil {
		return fmt.Errorf("update token: ownership check: %w", err)
	}
	if !owns {
		return ErrTokenNotOwnedByUser
	}
	return m.cli.updateToken(ctx, tokenID, req)
}

// RevokeToken soft-deletes a token (status=2).
//
// If adminForce is false, the last-token guard fires: if this is the user's
// only remaining active token, the call returns ErrCannotRevokeLastToken.
// If adminForce is true, the guard is bypassed (admin use only).
//
// Ownership check is always performed regardless of adminForce.
func (m *tokenManager) RevokeToken(ctx context.Context, coaiUserID, tokenID int64, adminForce bool) error {
	bind, err := m.bindingForUser(coaiUserID)
	if err != nil {
		return fmt.Errorf("revoke token: load binding: %w", err)
	}
	owns, err := m.ownsToken(ctx, bind.NewapiUserID, tokenID)
	if err != nil {
		return fmt.Errorf("revoke token: ownership check: %w", err)
	}
	if !owns {
		return ErrTokenNotOwnedByUser
	}

	if !adminForce {
		active, err := m.activeTokenCount(ctx, bind.NewapiUserID)
		if err != nil {
			return fmt.Errorf("revoke token: count active: %w", err)
		}
		if active <= 1 {
			return ErrCannotRevokeLastToken
		}
	}

	return m.cli.disableToken(ctx, tokenID)
}

// GetTokenUsage returns aggregated usage for a specific token sourced from
// NewAPI's native log endpoint (GET /api/log/?token_id=<id>).
//
// Ownership is verified before querying. The aggregation is done in-process
// over the fetched log entries; model breakdown is built by grouping on
// model_name.
//
// Why NewAPI logs (not gtk_app_usage_log):
// gtk_app_usage_log.token_id is only populated by the service-order path
// (commerce.WriteUsageCost). Chat-completion calls go through the
// usage.WriteUsageLog path which does not record token_id, so a SQL query
// on that table always returns 0 for chat-completion traffic. NewAPI records
// every relay call in its own logs table keyed by token_id, making it the
// authoritative source for per-token usage until a future PKG unifies the
// write paths.
func (m *tokenManager) GetTokenUsage(ctx context.Context, coaiUserID, tokenID int64) (*TokenUsage, error) {
	bind, err := m.bindingForUser(coaiUserID)
	if err != nil {
		return nil, fmt.Errorf("get token usage: load binding: %w", err)
	}
	owns, err := m.ownsToken(ctx, bind.NewapiUserID, tokenID)
	if err != nil {
		return nil, fmt.Errorf("get token usage: ownership check: %w", err)
	}
	if !owns {
		return nil, ErrTokenNotOwnedByUser
	}

	// Paginate through NewAPI logs until we get a partial page (< pageSize)
	// or hit the safety cap of 100 pages (50 000 rows). This fixes the
	// original single-fetch of 500 rows which silently under-counted heavy
	// token users.
	const pageSize = 500
	const maxPages = 100
	var entries []NewAPILogEntry
	for page := 0; page < maxPages; page++ {
		batch, err := m.cli.fetchTokenLogsPage(ctx, tokenID, page, pageSize)
		if err != nil {
			return nil, fmt.Errorf("get token usage: fetch logs page %d: %w", page, err)
		}
		entries = append(entries, batch...)
		if len(batch) < pageSize {
			break // last page reached
		}
	}

	u := &TokenUsage{TokenID: tokenID}

	// Aggregate totals and build per-model breakdown map.
	byModel := make(map[string]*UsageByModel)
	for _, e := range entries {
		totalTokens := e.PromptTokens + e.CompletionTokens
		u.TotalCalls++
		u.TotalTokensUsed += totalTokens
		u.InputTokens += e.PromptTokens
		u.OutputTokens += e.CompletionTokens

		bm, ok := byModel[e.Model]
		if !ok {
			bm = &UsageByModel{ModelID: e.Model}
			byModel[e.Model] = bm
		}
		bm.TotalCalls++
		bm.InputTokens += e.PromptTokens
		bm.OutputTokens += e.CompletionTokens
		bm.CreditsUsed += totalTokens
	}

	// Flatten map into slice, ordered by total_calls desc.
	// Simple insertion sort is fine for small model counts (<20).
	u.ByModel = make([]UsageByModel, 0, len(byModel))
	for _, bm := range byModel {
		u.ByModel = append(u.ByModel, *bm)
	}
	sortByModelByCallsDesc(u.ByModel)

	return u, nil
}

// sortByModelByCallsDesc sorts in place, highest TotalCalls first.
// Insertion sort — model count is always small (<20).
func sortByModelByCallsDesc(s []UsageByModel) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].TotalCalls > s[j-1].TotalCalls; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ---------------------------------------------------------------------------
// HTTP Handlers — User-side
// ---------------------------------------------------------------------------

// ListUserTokensAPI handles GET /api/gtk/v1/tokens
func ListUserTokensAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	coaiUserID := int64(user.GetID(connection.DB))

	mgr, err := defaultManager()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "newapi not configured: " + err.Error()})
		return
	}

	toks, err := mgr.ListUserTokens(c.Request.Context(), coaiUserID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []MaskedToken{}})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "list tokens failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": toks})
}

// createTokenBody is the JSON body for POST /api/gtk/v1/tokens.
type createTokenBody struct {
	Name            string `json:"name"`
	ExpiredTime     int64  `json:"expired_time"`     // unix seconds; 0 or -1 = never
	RemainQuota     int64  `json:"remain_quota"`     // 0 = unlimited when unlimited_quota=true
	UnlimitedQuota  bool   `json:"unlimited_quota"`  // when true (default), ignore remain_quota
	HasExplicitQuota bool  `json:"has_explicit_quota"` // frontend sets true when user typed a quota
}

// CreateUserTokenAPI handles POST /api/gtk/v1/tokens
// Returns the full plaintext key in the response — only shown once.
func CreateUserTokenAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	coaiUserID := int64(user.GetID(connection.DB))

	var body createTokenBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request: " + err.Error()})
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "name is required"})
		return
	}

	expiredTime := body.ExpiredTime
	if expiredTime == 0 {
		expiredTime = -1
	}

	// Default to unlimited quota when the frontend did not explicitly set a
	// quota value. This prevents NewAPI from treating remain_quota=0 as
	// "limited to 0 credits" (which makes the token immediately unusable).
	unlimitedQuota := body.UnlimitedQuota
	remainQuota := body.RemainQuota
	if !body.HasExplicitQuota || body.RemainQuota == 0 {
		unlimitedQuota = true
		remainQuota = 0
	}

	mgr, err := defaultManager()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "newapi not configured: " + err.Error()})
		return
	}

	tok, err := mgr.CreateUserToken(c.Request.Context(), coaiUserID, CreateTokenRequest{
		Name:           body.Name,
		RemainQuota:    remainQuota,
		UnlimitedQuota: unlimitedQuota,
		ExpiredTime:    expiredTime,
	})
	if errors.Is(err, ErrMaxTokensReached) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "MAX_TOKENS_REACHED",
			"detail":  fmt.Sprintf("you have reached the maximum of %d tokens per account", maxTokensPerUser),
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "create token failed: " + err.Error()})
		return
	}

	// Return full plaintext key — this is the ONE-TIME reveal.
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data": gin.H{
			"id":           tok.ID,
			"name":         tok.Name,
			"key":          tok.Key, // plaintext sk-xxx — shown exactly once
			"status":       tok.Status,
			"remain_quota": tok.RemainQuota,
			"expired_time": tok.ExpiredTime,
			"created_at":   time.Now().UTC().Format(time.RFC3339),
			"one_time_key": true, // signal to frontend: display and discard
		},
	})
}

// updateTokenBody is the JSON body for PATCH /api/gtk/v1/tokens/:id.
type updateTokenBody struct {
	Name        string `json:"name"`
	ExpiredTime int64  `json:"expired_time"`
	RemainQuota int64  `json:"remain_quota"`
}

// UpdateTokenAPI handles PATCH /api/gtk/v1/tokens/:id
func UpdateTokenAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	coaiUserID := int64(user.GetID(connection.DB))

	tokenID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid token id"})
		return
	}

	var body updateTokenBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request: " + err.Error()})
		return
	}

	mgr, err := defaultManager()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "newapi not configured: " + err.Error()})
		return
	}

	if err := mgr.UpdateToken(c.Request.Context(), coaiUserID, tokenID, UpdateTokenRequest{
		Name:        body.Name,
		ExpiredTime: body.ExpiredTime,
		RemainQuota: body.RemainQuota,
	}); errors.Is(err, ErrTokenNotOwnedByUser) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "token does not belong to your account"})
		return
	} else if errors.Is(err, ErrTokenNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "token not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "update token failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// RevokeUserTokenAPI handles DELETE /api/gtk/v1/tokens/:id
func RevokeUserTokenAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	coaiUserID := int64(user.GetID(connection.DB))

	tokenID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid token id"})
		return
	}

	mgr, err := defaultManager()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "newapi not configured: " + err.Error()})
		return
	}

	if err := mgr.RevokeToken(c.Request.Context(), coaiUserID, tokenID, false); errors.Is(err, ErrCannotRevokeLastToken) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "CANNOT_REVOKE_LAST_TOKEN",
			"detail":  "you must have at least one active token at all times",
		})
		return
	} else if errors.Is(err, ErrTokenNotOwnedByUser) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "token does not belong to your account"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "revoke token failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GetTokenUsageAPI handles GET /api/gtk/v1/tokens/:id/usage
func GetTokenUsageAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	coaiUserID := int64(user.GetID(connection.DB))

	tokenID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid token id"})
		return
	}

	mgr, err := defaultManager()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "newapi not configured: " + err.Error()})
		return
	}

	usage, err := mgr.GetTokenUsage(c.Request.Context(), coaiUserID, tokenID)
	if errors.Is(err, ErrTokenNotOwnedByUser) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "token does not belong to your account"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "get token usage failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": usage})
}

// ---------------------------------------------------------------------------
// HTTP Handlers — Admin-side
// ---------------------------------------------------------------------------

// AdminListTokensAPI handles GET /api/gtk/v1/admin/tokens
// Returns all tokens across all users. Supports ?user_id= and ?status= filters.
func AdminListTokensAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	cli, err := Default()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "newapi not configured: " + err.Error()})
		return
	}
	rc := &realNewAPIClient{c: cli}

	userIDParam := c.Query("user_id")
	if userIDParam != "" {
		newapiUserID, parseErr := strconv.ParseInt(userIDParam, 10, 64)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid user_id"})
			return
		}
		toks, err := rc.listTokensForUser(c.Request.Context(), newapiUserID)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": "list tokens failed: " + err.Error()})
			return
		}
		masked := make([]MaskedToken, 0, len(toks))
		for _, t := range toks {
			masked = append(masked, tokenFromNewAPI(t))
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": masked})
		return
	}

	// Global list: get all bindings, then list tokens per user.
	rows, err := globals.QueryDb(connection.DB, `
		SELECT coai_user_id, newapi_user_id FROM gtk_newapi_binding ORDER BY coai_user_id
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "load bindings failed: " + err.Error()})
		return
	}
	defer rows.Close()

	type adminTokenRow struct {
		MaskedToken
		CoaiUserID int64 `json:"coai_user_id"`
	}
	var all []adminTokenRow

	for rows.Next() {
		var coaiID, newapiID int64
		if err := rows.Scan(&coaiID, &newapiID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "scan binding: " + err.Error()})
			return
		}
		toks, err := rc.listTokensForUser(c.Request.Context(), newapiID)
		if err != nil {
			continue // skip users with failed lookups — don't abort entire list
		}
		for _, t := range toks {
			all = append(all, adminTokenRow{MaskedToken: tokenFromNewAPI(t), CoaiUserID: coaiID})
		}
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "iterate bindings: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": all})
}

// AdminRevokeTokenAPI handles DELETE /api/gtk/v1/admin/tokens/:id
// Admin can force-revoke any token, including the user's last active one.
// Requires coai_user_id as a query param to verify the binding exists.
func AdminRevokeTokenAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	tokenID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid token id"})
		return
	}

	coaiUserIDParam := c.Query("coai_user_id")
	if coaiUserIDParam == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "coai_user_id query param required"})
		return
	}
	coaiUserID, parseErr := strconv.ParseInt(coaiUserIDParam, 10, 64)
	if parseErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid coai_user_id"})
		return
	}

	cli, err := Default()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "newapi not configured: " + err.Error()})
		return
	}
	mgr := &tokenManager{db: connection.DB, cli: &realNewAPIClient{c: cli}}

	if err := mgr.RevokeToken(c.Request.Context(), coaiUserID, tokenID, true /*adminForce*/); errors.Is(err, ErrTokenNotOwnedByUser) {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "token does not belong to the specified user"})
		return
	} else if errors.Is(err, ErrTokenNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "token not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "admin revoke failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// AdminGetTokenAuditAPI handles GET /api/gtk/v1/admin/tokens/:id/audit
// Returns audit log entries for a specific token from gtk_audit_log.
func AdminGetTokenAuditAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	tokenID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid token id"})
		return
	}

	// Query gtk_audit_log for entries referencing this token.
	// The audit log schema stores resource_type + resource_id.
	rows, err := globals.QueryDb(connection.DB, `
		SELECT id, actor_id, action, resource_type, resource_id, detail, created_at
		FROM gtk_audit_log
		WHERE resource_type = 'token' AND resource_id = ?
		ORDER BY id DESC
		LIMIT 100
	`, tokenID)
	if err != nil {
		// gtk_audit_log may not exist in all environments — return empty gracefully.
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []interface{}{}})
		return
	}
	defer rows.Close()

	type auditEntry struct {
		ID           int64  `json:"id"`
		ActorID      int64  `json:"actor_id"`
		Action       string `json:"action"`
		ResourceType string `json:"resource_type"`
		ResourceID   int64  `json:"resource_id"`
		Detail       string `json:"detail"`
		CreatedAt    string `json:"created_at"`
	}
	var entries []auditEntry
	for rows.Next() {
		var e auditEntry
		var createdAt time.Time
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.ResourceType, &e.ResourceID, &e.Detail, &createdAt); err != nil {
			continue
		}
		e.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		entries = append(entries, e)
	}
	_ = rows.Err()
	if entries == nil {
		entries = []auditEntry{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": entries})
}
