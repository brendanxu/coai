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
	"encoding/json"
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

// tokenMu provides per-user mutual exclusion around the count-then-create and
// count-then-revoke patterns in CreateUserToken and RevokeToken. Keyed by
// coai_user_id (int64). Using a single sync.Map for both operations ensures
// that a concurrent create + revoke on the same user account cannot race past
// the max-10 and last-token invariants simultaneously.
//
// This is correct for single-instance deployments. For multi-instance
// deployments a distributed lock (e.g. Redis SETNX) would be required — add
// that when horizontal scaling is needed.
var tokenMu sync.Map

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
// CoaiUserID is populated in admin list responses via the binding reverse-lookup;
// it is zero in user-side list responses (callers already know their own ID).
type MaskedToken struct {
	ID             int64  `json:"id"`
	UserID         int64  `json:"user_id"`       // newapi_user_id
	CoaiUserID     int64  `json:"coai_user_id"`  // greentokey user id; 0 for user-side responses
	Name           string `json:"name"`
	Key            string `json:"key"`           // masked: "sk-tnx-***-abcd"
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

// listTokensForUser fetches all tokens for newapiUserID from NewAPI's
// paginated endpoint GET /api/token/?user_id=X&p=<page>&page_size=100.
//
// NewAPI v0.13.x returns a paginated envelope:
//
//	{"success":true,"data":{"items":[...],"total":N,"page":P,"page_size":100}}
//
// We loop pages until collected >= total or the page is empty, with a safety
// cap of 50 pages (5 000 tokens max — well above the per-user max-10 limit).
func (r *realNewAPIClient) listTokensForUser(ctx context.Context, newapiUserID int64) ([]*Token, error) {
	const pageSize = 100
	const maxPages = 50

	var allTokens []*Token
	for page := 0; page < maxPages; page++ {
		path := fmt.Sprintf("/api/token/?user_id=%d&p=%d&page_size=%d", newapiUserID, page, pageSize)
		var env listEnvelope[Token]
		if err := r.c.do(ctx, "GET", path, nil, newapiUserID, &env); err != nil {
			return nil, fmt.Errorf("newapi: list tokens for user %d page %d: %w", newapiUserID, page, err)
		}
		if !env.Success {
			return nil, fmt.Errorf("newapi: list tokens: %s", env.Message)
		}
		for i := range env.Data.Items {
			allTokens = append(allTokens, &env.Data.Items[i])
		}
		if int64(len(allTokens)) >= env.Data.Total || len(env.Data.Items) == 0 {
			break
		}
	}
	return allTokens, nil
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
// Audit log — writeAuditLog
// ---------------------------------------------------------------------------

// writeAuditLog inserts one row into gtk_audit_log. Failures are logged but
// never propagated — audit writes must not fail business operations.
//
// actorType is "user" or "admin"; actorID is the coai_user_id of the actor.
// beforeState / afterState are JSON-serialisable values; pass nil where not
// applicable (e.g. beforeState=nil on create, afterState=nil on revoke).
// note is optional free-text context.
func writeAuditLog(db *sql.DB, resourceType string, resourceID int64, action string,
	actorType string, actorID int64, beforeState, afterState interface{}, note string) {
	var beforeJSON, afterJSON *string
	if beforeState != nil {
		if b, err := json.Marshal(beforeState); err == nil {
			s := string(b)
			beforeJSON = &s
		}
	}
	if afterState != nil {
		if b, err := json.Marshal(afterState); err == nil {
			s := string(b)
			afterJSON = &s
		}
	}
	var notePtr *string
	if note != "" {
		notePtr = &note
	}
	_, _ = globals.ExecDb(db, `
		INSERT INTO gtk_audit_log
		  (resource_type, resource_id, action, actor_type, actor_id, before_state, after_state, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, resourceType, resourceID, action, actorType, actorID, beforeJSON, afterJSON, notePtr)
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

// isActiveToken returns true when the token is both enabled (status=1) AND not
// yet expired. Expired tokens retain status=1 in NewAPI but can no longer be
// used for API calls, so they must not count toward the active-token limit or
// the last-token guard.
//
// ExpiredTime semantics (per NewAPI Token struct comment):
//   -1 = never expires (always active if status=1)
//    0 = treat as never expires (legacy default)
//   >0 = Unix timestamp; active only while time.Now().Unix() < ExpiredTime
func isActiveToken(t *Token) bool {
	if t.Status != 1 {
		return false
	}
	// -1 and 0 both mean "never expires".
	if t.ExpiredTime <= 0 {
		return true
	}
	return time.Now().Unix() < t.ExpiredTime
}

// activeTokenCount returns how many strictly-active (status=1 AND not expired)
// tokens the NewAPI user currently holds. Used for max-10 enforcement and
// last-token guard.
func (m *tokenManager) activeTokenCount(ctx context.Context, newapiUserID int64) (int, error) {
	toks, err := m.cli.listTokensForUser(ctx, newapiUserID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, t := range toks {
		if isActiveToken(t) {
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
	lockAny, _ := tokenMu.LoadOrStore(coaiUserID, &sync.Mutex{})
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

	// Audit: record token creation (best-effort, never fails the operation).
	writeAuditLog(m.db, "token", tok.ID, "create", "user", coaiUserID, nil, map[string]interface{}{
		"name":            tok.Name,
		"expired_time":    tok.ExpiredTime,
		"unlimited_quota": tok.UnlimitedQuota,
		"remain_quota":    tok.RemainQuota,
	}, "")

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
	if err := m.cli.updateToken(ctx, tokenID, req); err != nil {
		return err
	}

	// Audit: record rename/update (best-effort).
	writeAuditLog(m.db, "token", tokenID, "rename", "user", coaiUserID, nil, map[string]interface{}{
		"name":         req.Name,
		"expired_time": req.ExpiredTime,
		"remain_quota": req.RemainQuota,
	}, "")
	return nil
}

// RevokeToken soft-deletes a token (status=2).
//
// If adminForce is false, the last-token guard fires: if this is the user's
// only remaining active token, the call returns ErrCannotRevokeLastToken.
// If adminForce is true, the guard is bypassed (admin use only).
//
// Ownership check is always performed regardless of adminForce.
//
// actorCoaiUserID is the coai_user_id written to the audit row's actor_id field.
// For user self-revoke (adminForce=false), pass 0 — the function uses coaiUserID
// (the token owner) as the actor. For admin force-revoke (adminForce=true), pass
// the admin's own coai_user_id so the audit row correctly attributes the action to
// the admin, not the token owner.
func (m *tokenManager) RevokeToken(ctx context.Context, coaiUserID, tokenID int64, adminForce bool, actorCoaiUserID int64) error {
	// PKG-A-3 R3-2: serialize revoke per user to prevent last-token race.
	// Two concurrent DELETE requests for a user with 2 active tokens could
	// both pass the active==2 guard and both succeed, leaving 0 active tokens.
	// Using the same tokenMu as CreateUserToken also prevents a concurrent
	// create+revoke from bypassing both the max-10 and last-token invariants.
	lockAny, _ := tokenMu.LoadOrStore(coaiUserID, &sync.Mutex{})
	lock := lockAny.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

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

	if err := m.cli.disableToken(ctx, tokenID); err != nil {
		return err
	}

	// Audit: record revocation (best-effort). actor_type distinguishes
	// user self-revoke from admin force-revoke.
	// For user self-revoke: actor = token owner (coaiUserID).
	// For admin force-revoke: actor = the admin (actorCoaiUserID), NOT the owner.
	action := "revoke"
	actorType := "user"
	auditActorID := coaiUserID // default: owner is the actor
	if adminForce {
		action = "admin_force_revoke"
		actorType = "admin"
		if actorCoaiUserID != 0 {
			auditActorID = actorCoaiUserID
		}
	}
	writeAuditLog(m.db, "token", tokenID, action, actorType, auditActorID,
		map[string]interface{}{"token_id": tokenID}, nil, "")
	return nil
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

// resolveQuota determines the final unlimited/quota values to send to NewAPI
// from the three request fields that influence quota behaviour.
//
// Resolution rules (R4-1):
//  1. unlimitedQuota=true              → unlimited, ignore remainQuota
//  2. hasExplicitQuota=true            → limited, use remainQuota verbatim (0 = 0 credits)
//  3. remainQuota > 0                  → limited (API-client path, no UI flag needed)
//  4. remainQuota=0, no explicit flag  → unlimited (UI default: checkbox not ticked)
//
// The old inline logic silently forced unlimited whenever hasExplicitQuota was
// absent, breaking API clients that send {remain_quota:100, unlimited_quota:false}.
func resolveQuota(unlimitedQuota bool, hasExplicitQuota bool, remainQuota int64) (unlimited bool, quota int64) {
	switch {
	case unlimitedQuota:
		return true, 0
	case hasExplicitQuota:
		return false, remainQuota
	case remainQuota > 0:
		return false, remainQuota
	default:
		return true, 0
	}
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

	unlimitedQuota, remainQuota := resolveQuota(body.UnlimitedQuota, body.HasExplicitQuota, body.RemainQuota)

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

	if err := mgr.RevokeToken(c.Request.Context(), coaiUserID, tokenID, false, 0); errors.Is(err, ErrCannotRevokeLastToken) {
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
// Returns all tokens across all users.
//
// Filter params:
//   ?coai_user_id=N  — preferred: looks up binding to get newapi_user_id, then
//                      fetches tokens for that user. Returns MaskedToken[] with
//                      coai_user_id populated.
//   ?user_id=N       — legacy: treats N as newapi_user_id directly (no binding
//                      lookup). coai_user_id will be 0 in the response.
//
// No param → global list across all bindings.
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

	// Preferred path: filter by coai_user_id with binding lookup.
	if coaiIDParam := c.Query("coai_user_id"); coaiIDParam != "" {
		coaiID, parseErr := strconv.ParseInt(coaiIDParam, 10, 64)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid coai_user_id"})
			return
		}
		bind, bindErr := LoadBinding(connection.DB, coaiID)
		if errors.Is(bindErr, sql.ErrNoRows) {
			c.JSON(http.StatusOK, gin.H{"success": true, "data": []MaskedToken{}})
			return
		}
		if bindErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "load binding: " + bindErr.Error()})
			return
		}
		toks, listErr := rc.listTokensForUser(c.Request.Context(), bind.NewapiUserID)
		if listErr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": "list tokens failed: " + listErr.Error()})
			return
		}
		masked := make([]MaskedToken, 0, len(toks))
		for _, t := range toks {
			m := tokenFromNewAPI(t)
			m.CoaiUserID = coaiID
			masked = append(masked, m)
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": masked})
		return
	}

	// Legacy path: ?user_id=<newapi_user_id> — no binding lookup.
	if userIDParam := c.Query("user_id"); userIDParam != "" {
		newapiUserID, parseErr := strconv.ParseInt(userIDParam, 10, 64)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid user_id"})
			return
		}
		toks, listErr := rc.listTokensForUser(c.Request.Context(), newapiUserID)
		if listErr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"success": false, "message": "list tokens failed: " + listErr.Error()})
			return
		}
		masked := make([]MaskedToken, 0, len(toks))
		for _, t := range toks {
			masked = append(masked, tokenFromNewAPI(t))
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": masked})
		return
	}

	// Global list: iterate all bindings and fetch tokens per user.
	rows, err := globals.QueryDb(connection.DB, `
		SELECT coai_user_id, newapi_user_id FROM gtk_newapi_binding ORDER BY coai_user_id
	`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "load bindings failed: " + err.Error()})
		return
	}
	defer rows.Close()

	var all []MaskedToken
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
			m := tokenFromNewAPI(t)
			m.CoaiUserID = coaiID
			all = append(all, m)
		}
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "iterate bindings: " + err.Error()})
		return
	}
	if all == nil {
		all = []MaskedToken{}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": all})
}

// AdminRevokeTokenAPI handles DELETE /api/gtk/v1/admin/tokens/:id
// Admin can force-revoke any token, including the user's last active one.
// Requires coai_user_id as a query param to verify the binding exists.
func AdminRevokeTokenAPI(c *gin.Context) {
	admin := auth.RequireAdmin(c)
	if admin == nil {
		return
	}
	// actor = the admin performing the revoke (not the token owner)
	adminCoaiUserID := int64(admin.GetID(connection.DB))

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
	targetCoaiUserID, parseErr := strconv.ParseInt(coaiUserIDParam, 10, 64)
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

	if err := mgr.RevokeToken(c.Request.Context(), targetCoaiUserID, tokenID, true /*adminForce*/, adminCoaiUserID); errors.Is(err, ErrTokenNotOwnedByUser) {
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
	rows, err := globals.QueryDb(connection.DB, `
		SELECT id, actor_id, actor_type, action, resource_type, resource_id,
		       before_state, after_state, note, created_at
		FROM gtk_audit_log
		WHERE resource_type = 'token' AND resource_id = ?
		ORDER BY id DESC
		LIMIT 100
	`, tokenID)
	if err != nil {
		// Table may not exist on older deployments — return empty gracefully.
		c.JSON(http.StatusOK, gin.H{"success": true, "data": []interface{}{}})
		return
	}
	defer rows.Close()

	type auditEntry struct {
		ID           int64   `json:"id"`
		ActorID      int64   `json:"actor_id"`
		ActorType    string  `json:"actor_type"`
		Action       string  `json:"action"`
		ResourceType string  `json:"resource_type"`
		ResourceID   int64   `json:"resource_id"`
		BeforeState  *string `json:"before_state"`
		AfterState   *string `json:"after_state"`
		Note         *string `json:"note"`
		CreatedAt    string  `json:"created_at"`
	}
	// R4-3 fix: scan created_at as a string to handle both MySQL (no
	// parseTime=true in DSN) and SQLite (TEXT column from migration).
	// Both drivers return the timestamp as []byte or string, not time.Time,
	// so scanning directly into time.Time fails with a type-mismatch error
	// that was silently swallowed by the previous `continue`, causing the
	// audit drawer to always appear empty.
	var entries []auditEntry
	for rows.Next() {
		var e auditEntry
		var createdAtStr sql.NullString
		if err := rows.Scan(
			&e.ID, &e.ActorID, &e.ActorType, &e.Action,
			&e.ResourceType, &e.ResourceID,
			&e.BeforeState, &e.AfterState, &e.Note, &createdAtStr,
		); err != nil {
			// True scan error (wrong column count, type the driver cannot
			// convert to string, etc.) — log and skip this row rather than
			// aborting the whole response.
			globals.Logger.Warnf("audit scan row failed token_id=%d: %v", tokenID, err)
			continue
		}
		// Parse the timestamp string into RFC3339 for the JSON response.
		// Try common formats emitted by MySQL (no parseTime) and SQLite.
		var createdAt time.Time
		if createdAtStr.Valid && createdAtStr.String != "" {
			for _, layout := range []string{
				"2006-01-02 15:04:05",
				time.RFC3339,
				time.RFC3339Nano,
			} {
				if t, parseErr := time.Parse(layout, createdAtStr.String); parseErr == nil {
					createdAt = t
					break
				}
			}
			if createdAt.IsZero() {
				globals.Logger.Warnf("audit created_at unparseable token_id=%d raw=%q", tokenID, createdAtStr.String)
			}
		}
		if createdAt.IsZero() {
			e.CreatedAt = ""
		} else {
			e.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		}
		entries = append(entries, e)
	}
	_ = rows.Err()
	if entries == nil {
		entries = []auditEntry{}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": entries})
}
