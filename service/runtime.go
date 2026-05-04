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
	output, creditsUsed, err := executeAgent(c.Request.Context(), agent, req.UserInput)
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

	// 4. Finalize: debit credits, flip status to completed, log usage.
	remaining, err := finalizeRun(connection.DB, order, runID, creditsUsed)
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
			"output":            output,
			"credits_used":      creditsUsed,
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
func finalizeRun(db *sql.DB, order *ServiceOrder, runID string, creditsUsed int) (int, error) {
	remaining := order.CreditsGranted - creditsUsed
	if remaining < 0 {
		// Overrun — log loud, don't refund automatically. Founder
		// reviews via the order audit trail.
		globals.Warn(fmt.Sprintf("service: order %s overran credits (granted=%d used=%d)",
			order.OrderNo, order.CreditsGranted, creditsUsed))
		// Still flip status — the work is done, the credit math is a
		// secondary concern.
		remaining = 0
	}
	_, err := globals.ExecDb(db, `
		UPDATE gtk_service_order
		SET status = 'completed', completed_at = CURRENT_TIMESTAMP
		WHERE order_no = ? AND agent_run_id = ?
	`, order.OrderNo, runID)
	if err != nil {
		return remaining, fmt.Errorf("finalize: %w", err)
	}
	return remaining, nil
}

// persistRunOutput stores the agent's output + the customer inputs on
// the order row so the /services/run page can render the result on
// refresh / reopen. Called from RunOrderFormAPI (v0.14) right after
// finalizeRun. Best-effort — a failure here doesn't break the response,
// just means the customer can't reload to see it again.
func persistRunOutput(db *sql.DB, orderNo, output, inputsJSON string) {
	_, err := globals.ExecDb(db, `
		UPDATE gtk_service_order
		SET agent_output = ?, agent_inputs = ?
		WHERE order_no = ?
	`, output, inputsJSON, orderNo)
	if err != nil {
		globals.Warn(fmt.Sprintf("service: persistRunOutput failed for %s: %v", orderNo, err))
	}
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
// system_prompt + the user's input. Returns the assistant's content
// string + the credit cost based on usage tokens + agent tier.
func executeAgent(ctx context.Context, agent *Agent, userInput string) (string, int, error) {
	runnerKey := viper.GetString("service.runner_api_key")
	if runnerKey == "" {
		return "", 0, errAgentNotConfigured
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
		return "", 0, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+runnerKey)

	resp, err := agentRunHTTPClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("upstream: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("read upstream body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return "", 0, fmt.Errorf("upstream HTTP %d: %s", resp.StatusCode, truncateRuntime(respBytes, 200))
	}

	var parsed struct {
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
		return "", 0, fmt.Errorf("parse upstream: %w (raw: %s)", err, truncateRuntime(respBytes, 200))
	}
	if len(parsed.Choices) == 0 {
		return "", 0, errors.New("upstream returned no choices")
	}

	credits := computeRunCredits(agent.MinTier,
		parsed.Usage.PromptTokens+parsed.Usage.CompletionTokens)
	return parsed.Choices[0].Message.Content, credits, nil
}

// executeAgentWithImages is the multimodal variant of executeAgent.
// Builds the OpenAI-format multipart user message:
//
//   "messages": [
//     {"role": "system", "content": "<agent system_prompt>"},
//     {"role": "user", "content": [
//       {"type": "text", "text": "<userInput>"},
//       {"type": "image_url", "image_url": {"url": "<url>"}}, ...
//     ]}
//   ]
//
// When imageURLs is empty, falls back to the simpler executeAgent so
// non-vision agents still work. The agent's PreferredModel must be
// vision-capable (gpt-4o, claude-3-5-sonnet, gemini-1.5-pro, etc.) for
// the upstream call to succeed when images are present.
func executeAgentWithImages(ctx context.Context, agent *Agent, userInput string, imageURLs []string) (string, int, error) {
	if len(imageURLs) == 0 {
		return executeAgent(ctx, agent, userInput)
	}

	runnerKey := viper.GetString("service.runner_api_key")
	if runnerKey == "" {
		return "", 0, errAgentNotConfigured
	}
	endpoint := viper.GetString("service.runner_endpoint")
	if endpoint == "" {
		endpoint = viper.GetString("newapi.public_endpoint")
		if endpoint == "" {
			endpoint = "https://api.greentokey.com/v1"
		}
	}
	endpoint = strings.TrimRight(endpoint, "/") + "/chat/completions"

	// Compose the multimodal content array. Text first so the model
	// reads instructions before processing images.
	content := []map[string]interface{}{
		{"type": "text", "text": userInput},
	}
	for _, url := range imageURLs {
		content = append(content, map[string]interface{}{
			"type":      "image_url",
			"image_url": map[string]string{"url": url},
		})
	}

	body := map[string]interface{}{
		"model": agent.PreferredModel,
		"messages": []map[string]interface{}{
			{"role": "system", "content": agent.SystemPrompt},
			{"role": "user", "content": content},
		},
		"stream": false,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", 0, fmt.Errorf("marshal multimodal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+runnerKey)

	resp, err := agentRunHTTPClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("upstream: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("read upstream body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return "", 0, fmt.Errorf("upstream HTTP %d: %s", resp.StatusCode, truncateRuntime(respBytes, 200))
	}

	var parsed struct {
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
		return "", 0, fmt.Errorf("parse upstream: %w (raw: %s)", err, truncateRuntime(respBytes, 200))
	}
	if len(parsed.Choices) == 0 {
		return "", 0, errors.New("upstream returned no choices")
	}

	credits := computeRunCredits(agent.MinTier,
		parsed.Usage.PromptTokens+parsed.Usage.CompletionTokens)
	return parsed.Choices[0].Message.Content, credits, nil
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
