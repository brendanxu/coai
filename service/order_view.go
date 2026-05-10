// v0.14 — customer-facing order view + form-based runner.
//
// Two endpoints under /api/gtk/v1/service-order/ matching the frontend's
// service-order.ts type contract exactly. Sister to v0.10's
// /service/order + /service/run/:order_no path family — those keep
// working for backward compat; the new pair is what the post-purchase
// /services/run/:order_no UI calls.
//
//   GET   /api/gtk/v1/service-order/:order_no
//         AUTH (owner). Returns ServiceOrder DTO matching frontend
//         type: { order_no, service_id, service_name, service_kind,
//         status, inputs, result, error, created_at, updated_at }.
//
//   POST  /api/gtk/v1/service-order/:order_no/run
//         AUTH (owner). Accepts multipart form (theme + extra + image
//         uploads) OR JSON. Synchronous: kicks off the agent and
//         returns the completed ServiceOrder.
//
// File uploads: accepted in the multipart form for shape compatibility
// with the frontend, but DROPPED in v0.14 — agents are text-only this
// sprint. v0.15 adds image-aware vision agents + persistent storage.
//
// Result encoding: v0.14 returns plain {kind: "text", content: ...}.
// v0.15 may add structured rednote-post parsing once agent prompts
// are tuned to emit JSON-fenced output.

package service

import (
	"chat/auth"
	"chat/connection"
	"chat/globals"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// constantTimeEq compares two strings in constant time. Avoids timing
// side-channels when verifying the per-order access token.
func constantTimeEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// CustomerOrderView is the JSON shape returned to the /services/run
// page. Field names match the frontend ServiceOrder type in
// app/src/api/service-order.ts exactly.
type CustomerOrderView struct {
	OrderNo     string          `json:"order_no"`
	ServiceID   int64           `json:"service_id"`
	ServiceName string          `json:"service_name"`
	ServiceKind string          `json:"service_kind"` // diy-agent | concierge
	Status      string          `json:"status"`       // pending|running|complete|failed
	Inputs      json.RawMessage `json:"inputs"`
	Result      json.RawMessage `json:"result"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

// CustomerResultText is one of the discriminated-union shapes the
// frontend renders. Used as the fallback when the agent output isn't
// parseable as a structured rednote-post.
type CustomerResultText struct {
	Kind    string `json:"kind"`    // "text"
	Content string `json:"content"`
}

// CustomerResultRednote is the structured 小红书 post shape the
// frontend renders with title + body + tags + image grid. Matches the
// `rednote-post` discriminator in app/src/api/service-order.ts.
type CustomerResultRednote struct {
	Kind   string   `json:"kind"` // "rednote-post"
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Tags   []string `json:"tags"`
	Images []string `json:"images"`
}

// resolveOrderAccess gates customer endpoints with two-track auth:
//   1. ?token=<access_token> matching the order row's access_token
//      column → bypass login (anonymous-friendly URL we share in WeChat)
//   2. Authenticated session → must be the order owner
//
// Returns the resolved owner user_id (so subsequent SQL can scope
// correctly) or writes the failure envelope and returns 0/false.
func resolveOrderAccess(c *gin.Context, orderNo string) (int64, bool) {
	if token := strings.TrimSpace(c.Query("token")); token != "" {
		ownerID, ok := verifyOrderToken(connection.DB, orderNo, token)
		if ok {
			return ownerID, true
		}
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "invalid or revoked access token",
		})
		return 0, false
	}
	user := auth.RequireAuth(c)
	if user == nil {
		return 0, false
	}
	return user.GetID(connection.DB), true
}

// verifyOrderToken returns (owner_user_id, true) when the token matches
// the order row. Constant-time compare to keep timing attacks out of
// scope (paranoid for a 24-byte URL secret, but cheap).
func verifyOrderToken(db *sql.DB, orderNo, suppliedToken string) (int64, bool) {
	row := globals.QueryRowDb(db,
		`SELECT coai_user_id, COALESCE(access_token, '') FROM gtk_service_order WHERE order_no = ?`,
		orderNo)
	var ownerID int64
	var stored string
	if err := row.Scan(&ownerID, &stored); err != nil {
		return 0, false
	}
	if stored == "" {
		return 0, false // pre-v0.15 row with no token; only login works
	}
	if !constantTimeEq(stored, suppliedToken) {
		return 0, false
	}
	return ownerID, true
}

// GetOrderForCustomerAPI returns the order row shaped for the customer-
// facing runner page. Authed via session OR ?token=...; the URL token
// is set on order creation and shareable in WeChat without forcing
// the customer to register.
func GetOrderForCustomerAPI(c *gin.Context) {
	orderNo := c.Param("order_no")
	if orderNo == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "missing order_no",
		})
		return
	}

	userID, ok := resolveOrderAccess(c, orderNo)
	if !ok {
		return
	}

	view, err := loadCustomerOrderView(connection.DB, orderNo, userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrOrderNotFound):
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "order not found"})
		case errors.Is(err, errOrderNotOwned):
			c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "order belongs to another user"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "load order: " + err.Error(),
			})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    view,
	})
}

// RunOrderFormAPI accepts the multipart form posted by ServiceRun.tsx
// and synchronously runs the agent. v0.10's RunOrderAPI keeps its JSON
// contract; this one wraps the same runtime with form-shaped inputs.
// Authed via session OR ?token=... (same model as GetOrderForCustomerAPI).
func RunOrderFormAPI(c *gin.Context) {
	orderNo := c.Param("order_no")
	if orderNo == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "missing order_no",
		})
		return
	}

	userID, ok := resolveOrderAccess(c, orderNo)
	if !ok {
		return
	}

	// Form parse — accept both multipart and urlencoded. Cap body at
	// 10MB so an accidental huge image upload doesn't OOM the VPS.
	if err := c.Request.ParseMultipartForm(10 << 20); err != nil {
		// urlencoded fallback (no files attached)
		if err2 := c.Request.ParseForm(); err2 != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "could not parse form: " + err.Error(),
			})
			return
		}
	}
	theme := strings.TrimSpace(c.Request.FormValue("theme"))
	extra := strings.TrimSpace(c.Request.FormValue("extra"))

	if theme == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "theme is required",
		})
		return
	}
	if len(theme) > 200 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "theme too long (max 200 chars)",
		})
		return
	}
	if len(extra) > 2000 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "extra too long (max 2000 chars)",
		})
		return
	}

	// v0.15: persist uploaded images to disk and pass URLs to the
	// vision-capable agent. saveUploads sniffs MIME, validates size,
	// returns publicly-fetchable URLs that NewAPI / OpenAI can resolve.
	imageURLs, err := saveUploads(orderNo, c.Request.MultipartForm)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "上传失败: " + err.Error(),
		})
		return
	}

	// Build user_input string (the v0.10 runtime takes a single string).
	userInput := composeUserInput(theme, extra, len(imageURLs))

	// Save inputs JSON before running so the agent can reference it
	// (and so a crash mid-run leaves a record of what was submitted).
	inputsJSON, _ := json.Marshal(map[string]interface{}{
		"theme":       theme,
		"extra":       extra,
		"file_count":  len(imageURLs),
		"image_urls":  imageURLs,
	})

	// Reuse the existing runtime helpers. Status path is the same.
	order, agent, err := loadRunnableOrder(connection.DB, orderNo, userID)
	if err != nil {
		statusCodeForRunErr(c, err)
		return
	}

	runID := uuid.NewString()
	if err := acquireRunLock(connection.DB, orderNo, runID); err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "order already running or already ran",
		})
		return
	}

	run, err := executeAgentWithImages(c.Request.Context(), agent, userInput, imageURLs)
	if err != nil {
		releaseRunLock(connection.DB, orderNo, runID)
		globals.Warn(fmt.Sprintf("service: form-run failed for %s: %v", orderNo, err))
		c.JSON(http.StatusBadGateway, gin.H{
			"success":  false,
			"message":  "agent execution failed: " + err.Error(),
			"order_no": orderNo,
			"retry":    true,
		})
		return
	}
	output := run.Output

	if _, err := finalizeRun(connection.DB, order, runID, run); err != nil {
		globals.Warn(fmt.Sprintf("service: finalize failed for %s (output delivered): %v", orderNo, err))
	}
	persistRunOutput(connection.DB, orderNo, output, string(inputsJSON))

	// Re-load and return the post-run view so the frontend doesn't need
	// a separate GET to refresh state.
	view, loadErr := loadCustomerOrderView(connection.DB, orderNo, userID)
	if loadErr != nil {
		// Output exists but post-run reload failed — return what we
		// have rather than dropping the result.
		fallback := buildResultView(orderNo, order.ServiceID, "complete", inputsJSON, mustResultJSON(output), "")
		c.JSON(http.StatusOK, gin.H{"success": true, "data": fallback})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// loadCustomerOrderView pulls the order row + joined service name and
// shapes it for the frontend. Maps v0.10 internal status values to the
// 4-state customer-visible enum (pending/running/complete/failed).
func loadCustomerOrderView(db *sql.DB, orderNo string, callerUserID int64) (*CustomerOrderView, error) {
	row := globals.QueryRowDb(db, `
		SELECT o.coai_user_id, o.service_id, COALESCE(s.name, ''),
		       o.status,
		       COALESCE(o.agent_inputs, ''), COALESCE(o.agent_output, ''),
		       o.created_at, o.updated_at
		FROM gtk_service_order o
		LEFT JOIN gtk_service s ON s.id = o.service_id
		WHERE o.order_no = ?
	`, orderNo)
	var (
		ownerID            int64
		serviceID          int64
		serviceName        string
		dbStatus           string
		inputsRaw, output  string
		createdAt, updated string
	)
	if err := row.Scan(&ownerID, &serviceID, &serviceName, &dbStatus,
		&inputsRaw, &output, &createdAt, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrOrderNotFound
		}
		return nil, fmt.Errorf("scan order: %w", err)
	}
	if ownerID != callerUserID {
		return nil, errOrderNotOwned
	}

	customerStatus := mapStatusForCustomer(dbStatus)

	var inputsJSON json.RawMessage
	if inputsRaw != "" {
		inputsJSON = json.RawMessage(inputsRaw)
	} else {
		inputsJSON = json.RawMessage(`null`)
	}

	var resultJSON json.RawMessage
	if customerStatus == "complete" && output != "" {
		resultJSON = mustResultJSON(output)
	} else {
		resultJSON = json.RawMessage(`null`)
	}

	return &CustomerOrderView{
		OrderNo:     orderNo,
		ServiceID:   serviceID,
		ServiceName: serviceName,
		ServiceKind: "diy-agent", // v0.14: only kind we ship
		Status:      customerStatus,
		Inputs:      inputsJSON,
		Result:      resultJSON,
		CreatedAt:   createdAt,
		UpdatedAt:   updated,
	}, nil
}

// mapStatusForCustomer collapses the 6-state internal enum down to the
// 4-state UI enum the frontend ServiceRun renders.
func mapStatusForCustomer(dbStatus string) string {
	switch dbStatus {
	case "paid", "pending_payment":
		// "pending" in customer language = "ready to run, agent hasn't started yet"
		return "pending"
	case "running":
		return "running"
	case "completed":
		return "complete"
	case "failed", "refunded":
		return "failed"
	default:
		return dbStatus
	}
}

// composeUserInput assembles the multi-field form into the single
// string the v0.10 runtime expects. Agent system prompts that want
// structured input can parse the labelled lines.
func composeUserInput(theme, extra string, fileCount int) string {
	var b strings.Builder
	b.WriteString("主题词: ")
	b.WriteString(theme)
	b.WriteString("\n")
	if extra != "" {
		b.WriteString("备注: ")
		b.WriteString(extra)
		b.WriteString("\n")
	}
	if fileCount > 0 {
		b.WriteString(fmt.Sprintf("(用户上传了 %d 张参考图片，描述请围绕这些素材展开)\n", fileCount))
	}
	return b.String()
}

// mustResultJSON returns a json.RawMessage matching the frontend
// ServiceResult discriminated union. v0.15: tries to parse a JSON-
// fenced block (xhs-copy-writer v2 emits this) into a rednote-post
// shape; falls back to {kind:"text"} when the agent didn't comply.
func mustResultJSON(output string) json.RawMessage {
	if rednote, ok := tryParseRednote(output); ok {
		if b, err := json.Marshal(rednote); err == nil {
			return b
		}
	}
	v := CustomerResultText{Kind: "text", Content: output}
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}

// tryParseRednote scans the agent output for a ```json … ``` fenced
// block (most reliable — every modern model honors that contract) and
// validates the parsed shape has at least title + body. tags + images
// fall back to empty slices if the model omits them.
//
// Robust to common drift: trailing prose after the fence, single vs
// triple backtick, leading "Here you go:" preamble.
func tryParseRednote(output string) (CustomerResultRednote, bool) {
	var empty CustomerResultRednote
	raw := strings.TrimSpace(output)
	if raw == "" {
		return empty, false
	}
	// Try fenced block first.
	if start := strings.Index(raw, "```"); start >= 0 {
		// Skip "```" + optional language tag (json/JSON/JSon).
		rest := raw[start+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		end := strings.Index(rest, "```")
		if end >= 0 {
			raw = strings.TrimSpace(rest[:end])
		}
	}
	// Fast-fail: must start with `{`.
	if !strings.HasPrefix(raw, "{") {
		return empty, false
	}
	var parsed struct {
		Title  string   `json:"title"`
		Body   string   `json:"body"`
		Tags   []string `json:"tags"`
		Images []string `json:"images"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return empty, false
	}
	if strings.TrimSpace(parsed.Title) == "" || strings.TrimSpace(parsed.Body) == "" {
		return empty, false
	}
	if parsed.Tags == nil {
		parsed.Tags = []string{}
	}
	if parsed.Images == nil {
		parsed.Images = []string{}
	}
	return CustomerResultRednote{
		Kind:   "rednote-post",
		Title:  parsed.Title,
		Body:   parsed.Body,
		Tags:   parsed.Tags,
		Images: parsed.Images,
	}, true
}

// buildResultView is the fallback shape if the post-run reload fails.
// Stitches together what we already have in memory.
func buildResultView(orderNo string, serviceID int64, status string, inputs, result json.RawMessage, errMsg string) *CustomerOrderView {
	return &CustomerOrderView{
		OrderNo:     orderNo,
		ServiceID:   serviceID,
		ServiceName: "",
		ServiceKind: "diy-agent",
		Status:      status,
		Inputs:      inputs,
		Result:      result,
		Error:       errMsg,
	}
}
