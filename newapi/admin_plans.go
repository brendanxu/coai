// admin_plans.go — GTK admin CRUD for plans, billing-config, and
// provider-pricing tables (PKG-ADMIN-L2-1, Phase 2).
//
// Surface:
//
//	GET  /api/gtk/v1/admin/plans                   paginated plan list
//	GET  /api/gtk/v1/admin/plans/:id               single plan
//	POST /api/gtk/v1/admin/plans                   create plan (201)
//	PUT  /api/gtk/v1/admin/plans/:id               update plan (all fields except id + code)
//	DELETE /api/gtk/v1/admin/plans/:id             soft-delete (is_active=false); 409 on FK conflict
//
//	GET  /api/gtk/v1/admin/billing-config          {config: {k: v, ...}}
//	PUT  /api/gtk/v1/admin/billing-config          {key, value} — whitelist: markup_multiplier
//
//	GET  /api/gtk/v1/admin/provider-pricing        list ordered effective_from DESC + optional filters
//	POST /api/gtk/v1/admin/provider-pricing        append new row (append-only, no UPDATE/DELETE)
//
// All routes gated by auth.RequireAdmin.

package newapi

import (
	"chat/auth"
	"chat/connection"
	"chat/globals"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ── Plan JSON shape ────────────────────────────────────────────────────────

// AdminPlanRow is the JSON representation returned by list + detail endpoints.
// QuotaGrant and ServiceID are omitted when NULL (omitempty).
type AdminPlanRow struct {
	ID           int64   `json:"id"`
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	ProductType  string  `json:"product_type"`
	BillingMode  string  `json:"billing_mode"`
	PriceCents   int64   `json:"price_cents"`
	DurationDays int64   `json:"duration_days"`
	QuotaGrant   *int64  `json:"quota_grant,omitempty"`
	ServiceID    *int64  `json:"service_id,omitempty"`
	QuotaConfig  *string `json:"quota_config,omitempty"`
	IsActive     bool    `json:"is_active"`
	CreatedAt    string  `json:"created_at"`
}

// CreatePlanRequest is the JSON body for POST /admin/plans.
type CreatePlanRequest struct {
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	Type         string  `json:"type"`
	ProductType  string  `json:"product_type"`
	BillingMode  string  `json:"billing_mode"`
	PriceCents   int64   `json:"price_cents"`
	DurationDays int64   `json:"duration_days"`
	QuotaGrant   *int64  `json:"quota_grant"`
	ServiceID    *int64  `json:"service_id"`
	QuotaConfig  *string `json:"quota_config"`
}

// UpdatePlanRequest is the JSON body for PUT /admin/plans/:id.
// Code is intentionally absent — it is immutable after creation.
type UpdatePlanRequest struct {
	Name         *string `json:"name"`
	Type         *string `json:"type"`
	ProductType  *string `json:"product_type"`
	BillingMode  *string `json:"billing_mode"`
	PriceCents   *int64  `json:"price_cents"`
	DurationDays *int64  `json:"duration_days"`
	QuotaGrant   *int64  `json:"quota_grant"`
	ServiceID    *int64  `json:"service_id"`
	QuotaConfig  *string `json:"quota_config"`
	IsActive     *bool   `json:"is_active"`
}

// ── Plan handlers ──────────────────────────────────────────────────────────

// ListPlansAPI handles GET /api/gtk/v1/admin/plans.
// Query params: limit (default 50, max 500), offset (default 0).
func ListPlansAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}

	limit := parseIntDefault(c.Query("limit"), 50)
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := parseIntDefault(c.Query("offset"), 0)
	if offset < 0 {
		offset = 0
	}

	plans, total, err := listPlans(connection.DB, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "list plans failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"plans":  plans,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		},
	})
}

// GetPlanAPI handles GET /api/gtk/v1/admin/plans/:id.
func GetPlanAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	id, ok := parsePlanIDParam(c)
	if !ok {
		return
	}
	plan, err := loadPlan(connection.DB, id)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": fmt.Sprintf("plan %d not found", id),
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "load plan failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"plan": plan},
	})
}

// CreatePlanAPI handles POST /api/gtk/v1/admin/plans.
func CreatePlanAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	var req CreatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}
	if err := validatePlanFields(req.Code, req.Name, req.PriceCents, req.DurationDays); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	plan, err := createPlan(connection.DB, req)
	if err != nil {
		if isUniqueConflict(err) {
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"message": fmt.Sprintf("plan code %q already exists", req.Code),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "create plan failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    gin.H{"plan": plan},
	})
}

// UpdatePlanAPI handles PUT /api/gtk/v1/admin/plans/:id.
// Code is immutable — ignored if present in the body.
func UpdatePlanAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	id, ok := parsePlanIDParam(c)
	if !ok {
		return
	}
	var req UpdatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}
	// Validate fields that are being changed.
	if req.PriceCents != nil && *req.PriceCents <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "price_cents must be > 0",
		})
		return
	}
	if req.DurationDays != nil && *req.DurationDays <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "duration_days must be > 0",
		})
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "name must not be empty",
		})
		return
	}

	plan, err := updatePlan(connection.DB, id, req)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": fmt.Sprintf("plan %d not found", id),
		})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "update plan failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"plan": plan},
	})
}

// RetirePlanAPI handles DELETE /api/gtk/v1/admin/plans/:id.
// Performs soft-delete: UPDATE is_active=false.
// Returns 409 when active gtk_user_plan rows reference this plan.
func RetirePlanAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	id, ok := parsePlanIDParam(c)
	if !ok {
		return
	}

	// Check for active subscriptions first (FK RESTRICT would block hard delete,
	// but we soft-delete — still want to warn the admin).
	var activeCount int64
	err := globals.QueryRowDb(connection.DB,
		`SELECT COUNT(*) FROM gtk_user_plan WHERE plan_id = ? AND status = 'active'`, id).Scan(&activeCount)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "check active subscriptions failed: " + err.Error(),
		})
		return
	}
	if activeCount > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": fmt.Sprintf("plan has %d active subscription(s) and cannot be retired", activeCount),
		})
		return
	}

	res, err := globals.ExecDb(connection.DB,
		`UPDATE gtk_plan SET is_active = 0 WHERE id = ?`, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "retire plan failed: " + err.Error(),
		})
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"message": fmt.Sprintf("plan %d not found", id),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// ── Billing-config handlers ────────────────────────────────────────────────

// allowedBillingKeys is the whitelist of keys accepted by UpdateBillingConfigAPI.
var allowedBillingKeys = map[string]bool{
	"markup_multiplier": true,
}

// ListBillingConfigAPI handles GET /api/gtk/v1/admin/billing-config.
// Returns {success, data: {config: {k: v, ...}}}.
func ListBillingConfigAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	rows, err := globals.QueryDb(connection.DB, `SELECT k, v FROM gtk_billing_config`)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "load billing config failed: " + err.Error(),
		})
		return
	}
	defer rows.Close()

	config := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "scan billing config: " + err.Error(),
			})
			return
		}
		config[k] = v
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "iter billing config: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"config": config},
	})
}

// UpdateBillingConfigRequest is the JSON body for PUT /admin/billing-config.
type UpdateBillingConfigRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// UpdateBillingConfigAPI handles PUT /api/gtk/v1/admin/billing-config.
// Accepts {key, value}. Only whitelisted keys are accepted.
// For markup_multiplier: validates 0 < v < 10.
func UpdateBillingConfigAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	var req UpdateBillingConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}
	key := strings.TrimSpace(req.Key)
	if !allowedBillingKeys[key] {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": fmt.Sprintf("unknown billing config key %q (accepted: markup_multiplier)", key),
		})
		return
	}
	val := strings.TrimSpace(req.Value)
	// Validate markup_multiplier range.
	if key == "markup_multiplier" {
		f, err := strconv.ParseFloat(val, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "markup_multiplier must be a valid finite decimal number",
			})
			return
		}
		if f <= 0 || f >= 10 {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "markup_multiplier must be in range (0, 10)",
			})
			return
		}
	}

	// UPSERT — INSERT or replace depending on engine.
	if globals.SqliteEngine {
		_, err := globals.ExecDb(connection.DB,
			`INSERT OR REPLACE INTO gtk_billing_config (k, v) VALUES (?, ?)`, key, val)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "update billing config failed: " + err.Error(),
			})
			return
		}
	} else {
		_, err := globals.ExecDb(connection.DB,
			`INSERT INTO gtk_billing_config (k, v) VALUES (?, ?)
			 ON DUPLICATE KEY UPDATE v = VALUES(v)`, key, val)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "update billing config failed: " + err.Error(),
			})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{"key": key, "value": val},
	})
}

// ── Provider-pricing handlers ──────────────────────────────────────────────

// AdminProviderPricingRow is the JSON shape returned by the list endpoint.
type AdminProviderPricingRow struct {
	ID            int64   `json:"id"`
	Provider      string  `json:"provider"`
	ModelID       string  `json:"model_id"`
	TokenType     string  `json:"token_type"`
	UpstreamPerM  float64 `json:"upstream_per_m"`
	EffectiveFrom string  `json:"effective_from"`
	Notes         *string `json:"notes,omitempty"`
}

// validTokenTypes is the whitelist for the token_type field.
var validTokenTypes = map[string]bool{
	"input":          true,
	"output":         true,
	"cache_write_5m": true,
	"cache_write_1h": true,
	"cache_read":     true,
}

// ListProviderPricingAPI handles GET /api/gtk/v1/admin/provider-pricing.
// Query params: provider, model_id, token_type (optional filters).
// Results ordered by effective_from DESC.
func ListProviderPricingAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	providerFilter := strings.TrimSpace(c.Query("provider"))
	modelFilter := strings.TrimSpace(c.Query("model_id"))
	ttFilter := strings.TrimSpace(c.Query("token_type"))

	where := []string{}
	args := []any{}
	if providerFilter != "" {
		where = append(where, "provider = ?")
		args = append(args, providerFilter)
	}
	if modelFilter != "" {
		where = append(where, "model_id = ?")
		args = append(args, modelFilter)
	}
	if ttFilter != "" {
		where = append(where, "token_type = ?")
		args = append(args, ttFilter)
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}
	q := `SELECT id, provider, model_id, token_type, upstream_per_m, effective_from, notes
	      FROM gtk_provider_pricing ` + whereClause + ` ORDER BY effective_from DESC`

	rows, err := globals.QueryDb(connection.DB, q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "list provider pricing failed: " + err.Error(),
		})
		return
	}
	defer rows.Close()

	out := []AdminProviderPricingRow{}
	for rows.Next() {
		var r AdminProviderPricingRow
		var notesNull sql.NullString
		var ts time.Time
		if err := rows.Scan(&r.ID, &r.Provider, &r.ModelID, &r.TokenType,
			&r.UpstreamPerM, &ts, &notesNull); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "scan provider pricing: " + err.Error(),
			})
			return
		}
		r.EffectiveFrom = ts.UTC().Format(time.RFC3339)
		if notesNull.Valid {
			s := notesNull.String
			r.Notes = &s
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "iter provider pricing: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"rows":  out,
			"total": len(out),
		},
	})
}

// CreateProviderPricingRequest is the JSON body for POST /admin/provider-pricing.
type CreateProviderPricingRequest struct {
	Provider     string  `json:"provider"`
	ModelID      string  `json:"model_id"`
	TokenType    string  `json:"token_type"`
	UpstreamPerM float64 `json:"upstream_per_m"`
	Notes        string  `json:"notes"`
}

// CreateProviderPricingAPI handles POST /api/gtk/v1/admin/provider-pricing.
// Appends a new row with effective_from=NOW(). No UPDATE or DELETE.
func CreateProviderPricingAPI(c *gin.Context) {
	if a := auth.RequireAdmin(c); a == nil {
		return
	}
	var req CreateProviderPricingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request: " + err.Error(),
		})
		return
	}
	req.Provider = strings.TrimSpace(req.Provider)
	req.ModelID = strings.TrimSpace(req.ModelID)
	req.TokenType = strings.TrimSpace(req.TokenType)
	if req.Provider == "" || req.ModelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "provider and model_id are required",
		})
		return
	}
	if !validTokenTypes[req.TokenType] {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "token_type must be one of: input, output, cache_write_5m, cache_write_1h, cache_read",
		})
		return
	}
	if req.UpstreamPerM <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "upstream_per_m must be > 0",
		})
		return
	}

	now := time.Now().UTC()
	var notesArg any
	if req.Notes != "" {
		notesArg = req.Notes
	} else {
		notesArg = nil
	}

	res, err := globals.ExecDb(connection.DB,
		`INSERT INTO gtk_provider_pricing
		 (provider, model_id, token_type, upstream_per_m, effective_from, notes)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		req.Provider, req.ModelID, req.TokenType, req.UpstreamPerM, now, notesArg)
	if err != nil {
		if isUniqueConflict(err) {
			c.JSON(http.StatusConflict, gin.H{
				"success": false,
				"message": "a pricing row for this (provider, model_id, token_type, effective_from) already exists",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "insert provider pricing failed: " + err.Error(),
		})
		return
	}
	newID, _ := res.LastInsertId()
	var notesPtr *string
	if req.Notes != "" {
		s := req.Notes
		notesPtr = &s
	}
	row := AdminProviderPricingRow{
		ID:            newID,
		Provider:      req.Provider,
		ModelID:       req.ModelID,
		TokenType:     req.TokenType,
		UpstreamPerM:  req.UpstreamPerM,
		EffectiveFrom: now.Format(time.RFC3339),
		Notes:         notesPtr,
	}
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    gin.H{"row": row},
	})
}

// ── Storage helpers ────────────────────────────────────────────────────────

func listPlans(db *sql.DB, limit, offset int) ([]AdminPlanRow, int64, error) {
	var total int64
	if err := globals.QueryRowDb(db, `SELECT COUNT(*) FROM gtk_plan`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count gtk_plan: %w", err)
	}
	rows, err := globals.QueryDb(db, `
		SELECT id, code, name, type, product_type, billing_mode,
		       price_cents, duration_days, quota_grant, service_id, quota_config, is_active, created_at
		FROM gtk_plan
		ORDER BY id ASC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list gtk_plan: %w", err)
	}
	defer rows.Close()
	out := make([]AdminPlanRow, 0, limit)
	for rows.Next() {
		r, err := scanPlanRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iter gtk_plan: %w", err)
	}
	return out, total, nil
}

func loadPlan(db *sql.DB, id int64) (*AdminPlanRow, error) {
	rows, err := globals.QueryDb(db, `
		SELECT id, code, name, type, product_type, billing_mode,
		       price_cents, duration_days, quota_grant, service_id, quota_config, is_active, created_at
		FROM gtk_plan WHERE id = ?
	`, id)
	if err != nil {
		return nil, fmt.Errorf("load gtk_plan: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	r, err := scanPlanRow(rows)
	if err != nil {
		return nil, err
	}
	return &r, rows.Err()
}

func createPlan(db *sql.DB, req CreatePlanRequest) (*AdminPlanRow, error) {
	// Defaults for optional enum fields.
	ptype := strings.TrimSpace(req.ProductType)
	if ptype == "" {
		ptype = "token"
	}
	bmode := strings.TrimSpace(req.BillingMode)
	if bmode == "" {
		bmode = "subscription"
	}
	planType := strings.TrimSpace(req.Type)
	if planType == "" {
		planType = "subscription"
	}

	res, err := globals.ExecDb(db, `
		INSERT INTO gtk_plan
		  (code, name, type, product_type, billing_mode, price_cents, duration_days,
		   quota_grant, service_id, quota_config, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
	`, req.Code, req.Name, planType, ptype, bmode,
		req.PriceCents, req.DurationDays, req.QuotaGrant, req.ServiceID, req.QuotaConfig)
	if err != nil {
		return nil, err
	}
	newID, _ := res.LastInsertId()
	return loadPlan(db, newID)
}

func updatePlan(db *sql.DB, id int64, req UpdatePlanRequest) (*AdminPlanRow, error) {
	// Build SET clauses dynamically from non-nil fields.
	setClauses := []string{}
	args := []any{}

	if req.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, strings.TrimSpace(*req.Name))
	}
	if req.Type != nil {
		setClauses = append(setClauses, "type = ?")
		args = append(args, *req.Type)
	}
	if req.ProductType != nil {
		setClauses = append(setClauses, "product_type = ?")
		args = append(args, *req.ProductType)
	}
	if req.BillingMode != nil {
		setClauses = append(setClauses, "billing_mode = ?")
		args = append(args, *req.BillingMode)
	}
	if req.PriceCents != nil {
		setClauses = append(setClauses, "price_cents = ?")
		args = append(args, *req.PriceCents)
	}
	if req.DurationDays != nil {
		setClauses = append(setClauses, "duration_days = ?")
		args = append(args, *req.DurationDays)
	}
	if req.QuotaGrant != nil {
		setClauses = append(setClauses, "quota_grant = ?")
		args = append(args, *req.QuotaGrant)
	}
	if req.ServiceID != nil {
		setClauses = append(setClauses, "service_id = ?")
		args = append(args, *req.ServiceID)
	}
	if req.QuotaConfig != nil {
		setClauses = append(setClauses, "quota_config = ?")
		args = append(args, *req.QuotaConfig)
	}
	if req.IsActive != nil {
		val := 0
		if *req.IsActive {
			val = 1
		}
		setClauses = append(setClauses, "is_active = ?")
		args = append(args, val)
	}
	if len(setClauses) == 0 {
		// Nothing to update — return current state.
		return loadPlan(db, id)
	}
	args = append(args, id)
	q := "UPDATE gtk_plan SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"
	res, err := globals.ExecDb(db, q, args...)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, sql.ErrNoRows
	}
	return loadPlan(db, id)
}

// scanPlanRow scans one gtk_plan row from an open *sql.Rows cursor.
func scanPlanRow(rows *sql.Rows) (AdminPlanRow, error) {
	var r AdminPlanRow
	var quotaGrant sql.NullInt64
	var serviceID sql.NullInt64
	var quotaConfig sql.NullString
	var isActiveInt int
	var createdAt time.Time
	if err := rows.Scan(
		&r.ID, &r.Code, &r.Name, &r.Type, &r.ProductType, &r.BillingMode,
		&r.PriceCents, &r.DurationDays, &quotaGrant, &serviceID, &quotaConfig,
		&isActiveInt, &createdAt,
	); err != nil {
		return r, fmt.Errorf("scan gtk_plan row: %w", err)
	}
	r.IsActive = isActiveInt != 0
	r.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	if quotaGrant.Valid {
		v := quotaGrant.Int64
		r.QuotaGrant = &v
	}
	if serviceID.Valid {
		v := serviceID.Int64
		r.ServiceID = &v
	}
	if quotaConfig.Valid {
		s := quotaConfig.String
		r.QuotaConfig = &s
	}
	return r, nil
}

// ── Shared utilities ───────────────────────────────────────────────────────

// parsePlanIDParam extracts :id and writes a 400 on failure.
func parsePlanIDParam(c *gin.Context) (int64, bool) {
	raw := c.Param("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid plan id: " + raw,
		})
		return 0, false
	}
	return id, true
}

// validatePlanFields validates the core required fields for create.
func validatePlanFields(code, name string, priceCents, durationDays int64) error {
	code = strings.TrimSpace(code)
	name = strings.TrimSpace(name)
	if code == "" {
		return fmt.Errorf("code is required")
	}
	if len(code) > 50 {
		return fmt.Errorf("code must be ≤ 50 characters")
	}
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if len(name) > 100 {
		return fmt.Errorf("name must be ≤ 100 characters")
	}
	if priceCents <= 0 {
		return fmt.Errorf("price_cents must be > 0")
	}
	if durationDays <= 0 {
		return fmt.Errorf("duration_days must be > 0")
	}
	return nil
}

// isUniqueConflict detects UNIQUE constraint errors from both MySQL and SQLite.
func isUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
