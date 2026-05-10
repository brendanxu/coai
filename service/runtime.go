// Agent runtime — the thing that actually makes the customer's
// purchase produce output.
//
// v0.10 is intentionally synchronous (single HTTP request returns the
// finished output) rather than streaming SSE. Customers wait 3-15s.
// v0.11 may add streaming once we know the actual perf envelope.
//
// State machine enforced by this file (one shot, no retries on success):
//
//   gtk_service_order.status starts at:
//     'paid'    — agent_run_id IS NULL, no run yet
//
//   On RunOrderAPI request:
//     'paid' + agent_run_id IS NULL  → acquire lock (atomic UPDATE),
//                                       proceed
//     'paid' + agent_run_id IS NOT NULL → 409 already_ran
//     'running'                     → 409 in_progress
//     'completed'                   → 409 already_completed
//     'refunded'/'failed'            → 410 gone
//     anything else                  → 400
//
//   On success: status = 'completed', completed_at = NOW.
//   On agent failure: status restored to 'paid', agent_run_id cleared,
//     so the customer can retry (or contact support).
//
// Credit accounting:
//   - We call NewAPI with greentokey's own runner api-key (configured
//     via service.runner_api_key — operator provisions a dedicated
//     NewAPI user for this with high quota).
//   - On response, NewAPI returns usage.prompt_tokens + completion_tokens.
//     We compute credits via the same formula as Layer 2 (1500 quota
//     units = 1 credit) and the agent's tier multiplier.
//   - Credits are debited from order.credits_granted, NOT from the
//     customer's 套餐 quota. Service orders carry their own credit
//     allocation; the customer's 套餐 is independent.
//
// Concurrency safety: the UPDATE at line ~120 uses
//   WHERE order_no = ? AND status = 'paid' AND agent_run_id IS NULL
// so two simultaneous requests deterministically resolve to one
// success + one 409 (the loser sees affected rows = 0).

package service

import (
	"bytes"
	"chat/auth"
	"chat/commerce"
	"chat/connection"
	"chat/globals"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/spf13/viper"
)

// agentRunResult bundles everything finalizeRun needs from one upstream
// call. Pre-PKG-2-Wave-4 executeAgent returned only (output, credits) —
// tokens were squashed into credits via computeRunCredits. Wave 4 D1 needs
// the raw token counts + model for commerce.ComputeAndWriteUsageCost so
// gtk_app_usage_log gets per-call cost rows for service orders. Adding a
// struct keeps the executeAgent signature ergonomic while avoiding the
// 5-positional-return code smell.
type agentRunResult struct {
	Output    string // assistant content
	Credits   int    // computed via computeRunCredits (existing contract)
	Model     string // upstream-reported model slug; pricing lookup key
	TokensIn  int64  // parsed.Usage.PromptTokens
	TokensOut int64  // parsed.Usage.CompletionTokens
}

// RunOrderRequest is the body of POST /api/gtk/v1/service/run/:order_no.
//
// user_input is the customer's free-form text for the agent (e.g. for
// xhs-copy-writer: a description of the photo + theme tags).
type RunOrderRequest struct {
	UserInput string `json:"user_input" binding:"required"`
}

// RunOrderAPI executes the agent bound to a paid order, debits credits,
// returns the agent's output. Synchronous (waits for full completion).
//
// Auth: the calling user must own the order.
//
// HTTP responses:
//   200 — success {output, credits_used, credits_remaining}
//   400 — bad input
//   401 — unauthenticated (RequireAuth)
//   403 — order belongs to another user
//   404 — order_no not found
//   409 — already running / completed / has agent_run_id
//   410 — order in terminal state (refunded/failed)
//   502 — agent call upstream failure (retryable; lock released)
//   500 — internal error
func RunOrderAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	userID := user.GetID(connection.DB)

	orderNo := c.Param("order_no")
	if orderNo == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "missing order_no path parameter",
		})
		return
	}

	var req RunOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid body: " + err.Error(),
		})
		return
	}

	// 1. Load order + agent. Validates ownership + status.
	order, agent, err := loadRunnableOrder(connection.DB, orderNo, userID)
	if err != nil {
		statusCodeForRunErr(c, err)
		return
	}

	// 2. Acquire one-shot lock. Atomic — two concurrent calls won't
	// both succeed.
	runID := uuid.NewString()
	if err := acquireRunLock(connection.DB, orderNo, runID); err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "order already running or already ran",
		})
		return
	}

	// 3. Call the agent. If anything fails, release the lock so the
	// customer can retry.
	run, err := executeAgent(c.Request.Context(), agent, req.UserInput)
	if err != nil {
		releaseRunLock(connection.DB, orderNo, runID)
		globals.Warn(fmt.Sprintf("service: agent run failed for %s: %v", orderNo, err))
		c.JSON(http.StatusBadGateway, gin.H{
			"success":  false,
			"message":  "agent execution failed: " + err.Error(),
			"order_no": orderNo,
			"retry":    true,
		})
		return
	}

	// 4. Finalize: CAS running → completed (CR4-safe vs concurrent
	//    refund), debit credits, write per-call usage cost row.
	remaining, err := finalizeRun(connection.DB, order, runID, run)
	if err != nil {
		// We have output but couldn't persist accounting. Log loud
		// but still return output so customer gets value — the
		// credit-tracking inconsistency is a founder cleanup task.
		globals.Warn(fmt.Sprintf("service: finalize bookkeeping failed for %s (output delivered anyway): %v",
			orderNo, err))
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"order_no":          orderNo,
			"agent_run_id":      runID,
			"output":            run.Output,
			"credits_used":      run.Credits,
			"credits_remaining": remaining,
		},
	})
}

// ─────────────────────────────────────────────────────────────────────
// Storage primitives
// ─────────────────────────────────────────────────────────────────────

var (
	errOrderNotOwned        = errors.New("service: order belongs to another user")
	errOrderNotRunnable     = errors.New("service: order not in 'paid' state with no prior run")
	errOrderTerminal        = errors.New("service: order in terminal state")
	errAgentNotFound        = errors.New("service: agent referenced by order not found in registry")
	errAgentNotConfigured   = errors.New("service: agent runner not configured (missing service.runner_api_key)")
)

// loadRunnableOrder fetches the order + verifies ownership + status.
// Returns the order row + the resolved agent (joined by service.agent_slug).
func loadRunnableOrder(db *sql.DB, orderNo string, callerUserID int64) (*ServiceOrder, *Agent, error) {
	row := globals.QueryRowDb(db, `
		SELECT o.coai_user_id, o.service_id, o.service_slug,
		       o.credits_granted, o.status, COALESCE(o.agent_run_id, ''),
		       s.agent_slug
		FROM gtk_service_order o
		JOIN gtk_service s ON s.id = o.service_id
		WHERE o.order_no = ?
	`, orderNo)
	var (
		ownerID    int64
		serviceID  int64
		serviceSlug, status, agentRunID, agentSlug string
		creditsGranted int
	)
	if err := row.Scan(&ownerID, &serviceID, &serviceSlug, &creditsGranted,
		&status, &agentRunID, &agentSlug); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrOrderNotFound
		}
		return nil, nil, fmt.Errorf("load order: %w", err)
	}
	if ownerID != callerUserID {
		return nil, nil, errOrderNotOwned
	}
	switch status {
	case "paid":
		if agentRunID != "" {
			return nil, nil, errOrderNotRunnable
		}
	case "running", "completed":
		return nil, nil, errOrderNotRunnable
	case "refunded", "failed":
		return nil, nil, errOrderTerminal
	default:
		return nil, nil, errOrderNotRunnable
	}

	// Load agent.
	agentRow := globals.QueryRowDb(db, `
		SELECT slug, name, system_prompt, preferred_model, min_tier, status
		FROM gtk_agent WHERE slug = ?
	`, agentSlug)
	var a Agent
	if err := agentRow.Scan(&a.Slug, &a.Name, &a.SystemPrompt,
		&a.PreferredModel, &a.MinTier, &a.Status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, errAgentNotFound
		}
		return nil, nil, fmt.Errorf("load agent: %w", err)
	}
	if a.Status != "active" {
		return nil, nil, errAgentNotFound
	}

	return &ServiceOrder{
		OrderNo:        orderNo,
		CoaiUserID:     ownerID,
		ServiceID:      serviceID,
		ServiceSlug:    serviceSlug,
		CreditsGranted: creditsGranted,
		Status:         status,
	}, &a, nil
}

// acquireRunLock UPDATEs status='running' + agent_run_id=runID
// only if (status='paid' AND agent_run_id IS NULL). Race-free.
func acquireRunLock(db *sql.DB, orderNo, runID string) error {
	res, err := globals.ExecDb(db, `
		UPDATE gtk_service_order
		SET status = 'running', agent_run_id = ?
		WHERE order_no = ? AND status = 'paid' AND agent_run_id IS NULL
	`, runID, orderNo)
	if err != nil {
		return fmt.Errorf("acquire run lock: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return errors.New("could not acquire run lock — concurrent run or status drift")
	}
	return nil
}

// releaseRunLock returns the order to 'paid' state with agent_run_id
// cleared, so the customer can retry. Only does anything if our runID
// matches (don't accidentally release someone else's lock).
func releaseRunLock(db *sql.DB, orderNo, runID string) {
	_, err := globals.ExecDb(db, `
		UPDATE gtk_service_order
		SET status = 'paid', agent_run_id = NULL
		WHERE order_no = ? AND agent_run_id = ?
	`, orderNo, runID)
	if err != nil {
		globals.Warn(fmt.Sprintf("service: releaseRunLock failed for %s/%s: %v", orderNo, runID, err))
	}
}

// finalizeRun debits credits + marks completed. Returns remaining
// credits in the order's pool.
//
// PKG-2 Wave 4 D1 changes (CR4 + GREENFIELD usage write):
//
//  1. Status flip is now a CAS via commerce.CompareAndSwapServiceOrderStatus
//     (running → completed). A concurrent refund webhook may flip
//     running → canceled_mid_flight via commerce.RevokeEntitlement. Both
//     callers using the shared CAS helper means the loser observes
//     changed=false and gives up rather than overwriting the winner's
//     terminal state.
//
//  2. On CAS true: writes a per-call cost row to gtk_app_usage_log via
//     commerce.ComputeAndWriteUsageCost. This is the FIRST place service
//     orders contribute to the unified cost ledger; pre-Wave-4 the
//     gateway only logged token-plan calls. The row uses
//     source='service_order' + order_id=order.OrderNo so margin_view +
//     monitoring scripts can roll up cost-per-order.
//
//  3. On CAS false: log a warning ("status changed during run; refund
//     landed first?") and skip the usage write. The customer's run already
//     completed (we hold the output) so the output still ships, but
//     accounting reflects the refund-driven terminal state, not a
//     completed-and-billed flow.
func finalizeRun(db *sql.DB, order *ServiceOrder, runID string, run agentRunResult) (int, error) {
	remaining := order.CreditsGranted - run.Credits
	if remaining < 0 {
		// Overrun — log loud, don't refund automatically. Founder
		// reviews via the order audit trail.
		globals.Warn(fmt.Sprintf("service: order %s overran credits (granted=%d used=%d)",
			order.OrderNo, order.CreditsGranted, run.Credits))
		// Still flip status — the work is done, the credit math is a
		// secondary concern.
		remaining = 0
	}

	// CR4: CAS running → completed. The CAS helper also bumps updated_at.
	// We DON'T set completed_at via the CAS because the helper is a shared
	// primitive — instead, after a successful CAS, we issue a tiny follow-up
	// UPDATE to stamp completed_at. (Keeps the CAS helper general-purpose
	// for future callers that don't have a "completed_at"-style column.)
	changed, err := commerce.CompareAndSwapServiceOrderStatus(
		db, order.OrderNo, "running", "completed")
	if err != nil {
		return remaining, fmt.Errorf("finalize CAS: %w", err)
	}
	if !changed {
		// Status drift: a concurrent refund (RevokeEntitlement) flipped
		// us to canceled_mid_flight (or another terminal state) before we
		// reached this line. Skip the usage write so we don't bill a run
		// that was just refunded; the output still ships back to the
		// customer (they got value in the wall-clock window before the
		// refund landed).
		globals.Warn(fmt.Sprintf(
			"service: finalizeRun for %s observed status change during run "+
				"(CAS running→completed returned changed=false; refund landed first?) — "+
				"skipping usage write to avoid charging a refunded run",
			order.OrderNo))
		return remaining, nil
	}

	// Stamp completed_at + ensure agent_run_id matches (defensive).
	if _, err := globals.ExecDb(db, `
		UPDATE gtk_service_order
		SET completed_at = CURRENT_TIMESTAMP
		WHERE order_no = ? AND agent_run_id = ?
	`, order.OrderNo, runID); err != nil {
		// Audit field write failure is non-fatal: status is already
		// 'completed' authoritative-state-wise. Log + continue so we
		// still write the cost ledger row.
		globals.Warn(fmt.Sprintf(
			"service: stamp completed_at for %s/%s failed (status already 'completed'): %v",
			order.OrderNo, runID, err))
	}

	// GREENFIELD usage write (Wave 4 D1 + CR2). Funnel through the unified
	// commerce.ComputeAndWriteUsageCost so service-order calls land in
	// gtk_app_usage_log with the same shape as chat / api calls.
	//   source = 'service_order' (per CHECK constraint)
	//   order_id = order.OrderNo (links the row back for margin view rollup)
	//   plan_id = NULL (service orders bill via gtk_service_order, not plan quota)
	//   service = run.Model (used as pricing.go lookup key by ComputeAndWriteUsageCost)
	//   provider = NULL (auto-stamped from pricing.LookupProvider on known model)
	//
	// Unknown-model callers degrade gracefully: ComputeAndWriteUsageCost
	// logs a warning and writes a 0-cost row preserving the audit trail
	// (per Q6 Option A documented behavior).
	entry := commerce.UsageCostEntry{
		UserID:     order.CoaiUserID,
		PlanID:     sql.NullInt64{}, // NULL — service orders don't bill via plans
		Service:    run.Model,
		Source:     "service_order",
		OrderID:    sql.NullString{String: order.OrderNo, Valid: true},
		Provider:   sql.NullString{}, // ComputeAndWriteUsageCost stamps from pricing
		TokensUsed: run.TokensIn + run.TokensOut,
		CostCents:  0, // ComputeAndWriteUsageCost overwrites with the looked-up cost
	}
	if err := commerce.ComputeAndWriteUsageCost(db, entry, run.TokensIn, run.TokensOut); err != nil {
		// Cost-ledger write failure is non-fatal at the user-facing layer
		// (the run completed, the customer has output). Log loud so ops
		// can investigate the data integrity gap.
		globals.Warn(fmt.Sprintf(
			"service: cost-ledger write failed for %s (status='completed' already; ledger row missing): %v",
			order.OrderNo, err))
	}

	return remaining, nil
}

// statusCodeForRunErr maps loadRunnableOrder errors to HTTP responses.
func statusCodeForRunErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrOrderNotFound):
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "order not found"})
	case errors.Is(err, errOrderNotOwned):
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "order belongs to another user"})
	case errors.Is(err, errOrderNotRunnable):
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "order is not in a runnable state (already running, completed, or unpaid)"})
	case errors.Is(err, errOrderTerminal):
		c.JSON(http.StatusGone, gin.H{"success": false, "message": "order is refunded or failed; no run possible"})
	case errors.Is(err, errAgentNotFound):
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "agent registry inconsistent — contact support"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "load failed: " + err.Error()})
	}
}

// ─────────────────────────────────────────────────────────────────────
// Agent call (NewAPI integration)
// ─────────────────────────────────────────────────────────────────────

// agentRunHTTPClient is the HTTP client the runtime uses for NewAPI
// calls. Replace via the testAgentRunHTTPClient hook in tests to
// simulate upstream responses.
var agentRunHTTPClient = &http.Client{Timeout: 60 * time.Second}

// executeAgent calls NewAPI's /v1/chat/completions with the agent's
// system_prompt + the user's input. Returns an agentRunResult bundling
// the assistant content, the computed credit cost, and the raw token
// counts + model so the Wave 4 D1 finalizeRun path can write a usage
// cost row to gtk_app_usage_log via commerce.ComputeAndWriteUsageCost.
//
// PKG-2 Wave 4 D1 signature change:
//
//	pre:  func(ctx, *Agent, userInput) (output string, credits int, err error)
//	post: func(ctx, *Agent, userInput) (agentRunResult, err)
//
// All callers in this package were the test file + the single RunOrderAPI
// handler. No external Go pkg references this private symbol; safe to
// reshape.
func executeAgent(ctx context.Context, agent *Agent, userInput string) (agentRunResult, error) {
	runnerKey := viper.GetString("service.runner_api_key")
	if runnerKey == "" {
		return agentRunResult{}, errAgentNotConfigured
	}
	endpoint := viper.GetString("service.runner_endpoint")
	if endpoint == "" {
		// Default points at the same NewAPI gateway customers use, but
		// authenticated as the runner instead.
		endpoint = viper.GetString("newapi.public_endpoint")
		if endpoint == "" {
			endpoint = "https://api.greentokey.com/v1"
		}
	}
	endpoint = strings.TrimRight(endpoint, "/") + "/chat/completions"

	body := map[string]interface{}{
		"model": agent.PreferredModel,
		"messages": []map[string]string{
			{"role": "system", "content": agent.SystemPrompt},
			{"role": "user", "content": userInput},
		},
		"stream": false,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return agentRunResult{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return agentRunResult{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+runnerKey)

	resp, err := agentRunHTTPClient.Do(req)
	if err != nil {
		return agentRunResult{}, fmt.Errorf("upstream: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return agentRunResult{}, fmt.Errorf("read upstream body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return agentRunResult{}, fmt.Errorf("upstream HTTP %d: %s", resp.StatusCode, truncateRuntime(respBytes, 200))
	}

	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return agentRunResult{}, fmt.Errorf("parse upstream: %w (raw: %s)", err, truncateRuntime(respBytes, 200))
	}
	if len(parsed.Choices) == 0 {
		return agentRunResult{}, errors.New("upstream returned no choices")
	}

	credits := computeRunCredits(agent.MinTier,
		parsed.Usage.PromptTokens+parsed.Usage.CompletionTokens)

	// Model resolution: prefer upstream-reported model (which may differ
	// from agent.PreferredModel if the gateway routed to a fallback).
	// Falling back to agent.PreferredModel keeps pricing.go lookup keys
	// consistent if upstream omits the field.
	model := parsed.Model
	if model == "" {
		model = agent.PreferredModel
	}

	return agentRunResult{
		Output:    parsed.Choices[0].Message.Content,
		Credits:   credits,
		Model:     model,
		TokensIn:  int64(parsed.Usage.PromptTokens),
		TokensOut: int64(parsed.Usage.CompletionTokens),
	}, nil
}

// computeRunCredits maps total_tokens × tier_multiplier into the
// integer-credits unit gtk_service_order tracks. Mirror of newapi.QuotaToCredits
// but for outcome-tier rather than per-call cost. Always rounds UP so
// near-empty pools fail closed instead of going negative.
//
// Formula:
//   quota_units = total_tokens × 1 (1 token = 1 quota unit baseline)
//   credits     = ceil(quota_units × tier_multiplier / 1500)
//
// tier_multiplier:
//   light    → 0.5
//   standard → 1.0
//   premium  → 3.0
func computeRunCredits(tier string, totalTokens int) int {
	if totalTokens <= 0 {
		return 0
	}
	var mult float64
	switch tier {
	case "light":
		mult = 0.5
	case "premium":
		mult = 3.0
	default:
		mult = 1.0 // standard / unknown → conservative default
	}
	const quotaPerCredit = 1500
	q := float64(totalTokens) * mult
	credits := int(q / float64(quotaPerCredit))
	if int(q)%quotaPerCredit != 0 {
		credits++ // round up
	}
	return credits
}

// truncateRuntime is a copy of checkout.go's truncate to keep this file
// self-contained re: file boundaries (avoids cross-file private export).
// Both can converge in v0.11 cleanup.
func truncateRuntime(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// runtimeIntFromContext is a small helper used by tests to short-circuit
// the runner_api_key check. Production code never calls this; tests can
// set viper.Set("service.runner_api_key", "fake-key") to bypass.
//
// Kept here as documentation of the contract; not a real function.
var _ = strconv.Itoa // import-keepalive for strconv (used elsewhere if we expand)
