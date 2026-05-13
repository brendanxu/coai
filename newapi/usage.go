// usage.go — usage aggregation HTTP handlers for /api/gtk/v1/usage/*
//
// Endpoints:
//
//	GET /gtk/v1/usage/summary?range=month|7d|30d   → UsageSummary
//	GET /gtk/v1/usage/timeseries?range=...          → []UsageBucket (daily buckets)
//	GET /gtk/v1/usage/by-model?range=...            → []UsageByModel
//	GET /gtk/v1/usage/recent?since=<unix_ts>        → []UsageRecentRow (Live page polling)
//
// All endpoints are user-scoped (auth.RequireAuth). Rows are filtered to
// the calling user's ID — users cannot see each other's usage.
//
// DB: queries run against gtk_app_usage_log (V2 schema with model_id,
// input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
// client_charge_micro, markup_multiplier columns). Missing/NULL values
// in V1-era rows are handled via COALESCE(col, 0).

package newapi

import (
	"chat/auth"
	"chat/connection"
	"chat/globals"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// parseRange returns (start, end time.Time) for the given ?range= param.
// Supports "month" (current calendar month), "7d" (last 7 days), "30d"
// (last 30 days). Defaults to current month when param is empty or unknown.
func parseRange(rangeParam string) (start, end time.Time) {
	now := time.Now()
	switch rangeParam {
	case "7d":
		end = now
		start = now.AddDate(0, 0, -7)
	case "30d":
		end = now
		start = now.AddDate(0, 0, -30)
	default: // "month" or anything unrecognised → current calendar month
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		end = now
	}
	return start, end
}

// UsageSummary is the response for GET /gtk/v1/usage/summary.
type UsageSummary struct {
	CreditsUsed      int64 `json:"credits_used"`       // SUM(tokens_used)
	TotalCalls       int64 `json:"total_calls"`         // COUNT(*)
	CacheSavedMicro  int64 `json:"cache_saved_micro"`   // approx cache savings
	ActualSpentMicro int64 `json:"actual_spent_micro"`  // SUM(client_charge_micro)
}

// UsageBucket is one daily data point in the timeseries response.
type UsageBucket struct {
	Date             string `json:"date"`               // "2026-05-01"
	CacheMissCredits int64  `json:"cache_miss_credits"` // tokens not from cache
	CacheHitCredits  int64  `json:"cache_hit_credits"`  // cache_read_tokens
}

// UsageByModel is one row in the by-model breakdown response.
type UsageByModel struct {
	ModelID          string `json:"model_id"`
	TotalCalls       int64  `json:"total_calls"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheSavedMicro  int64  `json:"cache_saved_micro"`
	CreditsUsed      int64  `json:"credits_used"`
	ActualSpentMicro int64  `json:"actual_spent_micro"`
}

// UsageRecentRow is one row in the recent-activity response (Live page polling).
type UsageRecentRow struct {
	ID                int64  `json:"id"`
	CreatedAt         string `json:"created_at"`          // RFC3339
	ModelID           string `json:"model_id"`
	Provider          string `json:"provider"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	CacheReadTokens   int64  `json:"cache_read_tokens"`
	ClientChargeMicro int64  `json:"client_charge_micro"`
	Source            string `json:"source"`
}

// UsageSummaryAPI handles GET /gtk/v1/usage/summary.
//
// Query params:
//
//	range  "month" (default) | "7d" | "30d"
func UsageSummaryAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	userID := int64(user.GetID(connection.DB))
	start, end := parseRange(c.Query("range"))

	var summary UsageSummary
	err := globals.QueryRowDb(connection.DB, `
		SELECT
			COALESCE(SUM(tokens_used), 0),
			COUNT(*),
			COALESCE(SUM(COALESCE(cache_read_tokens, 0) * COALESCE(markup_multiplier, 1.0)), 0),
			COALESCE(SUM(COALESCE(client_charge_micro, 0)), 0)
		FROM gtk_app_usage_log
		WHERE user_id = ?
		  AND created_at BETWEEN ? AND ?
	`, userID, start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05")).
		Scan(&summary.CreditsUsed, &summary.TotalCalls,
			&summary.CacheSavedMicro, &summary.ActualSpentMicro)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "usage summary query failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    summary,
	})
}

// UsageTimeseriesAPI handles GET /gtk/v1/usage/timeseries.
//
// Returns daily buckets ordered ascending. Dates with no usage are omitted
// (zero-fill is left to the client).
//
// Query params:
//
//	range  "month" (default) | "7d" | "30d"
func UsageTimeseriesAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	userID := int64(user.GetID(connection.DB))
	start, end := parseRange(c.Query("range"))

	rows, err := globals.QueryDb(connection.DB, `
		SELECT
			DATE(created_at)                                  AS day,
			COALESCE(SUM(COALESCE(tokens_used, 0)
			         - COALESCE(cache_read_tokens, 0)), 0)   AS cache_miss,
			COALESCE(SUM(COALESCE(cache_read_tokens, 0)), 0) AS cache_hit
		FROM gtk_app_usage_log
		WHERE user_id = ?
		  AND created_at BETWEEN ? AND ?
		GROUP BY DATE(created_at)
		ORDER BY day ASC
		LIMIT 30
	`, userID, start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "usage timeseries query failed: " + err.Error(),
		})
		return
	}
	defer rows.Close()

	buckets := make([]UsageBucket, 0, 30)
	for rows.Next() {
		var b UsageBucket
		if err := rows.Scan(&b.Date, &b.CacheMissCredits, &b.CacheHitCredits); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "usage timeseries scan failed: " + err.Error(),
			})
			return
		}
		buckets = append(buckets, b)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "usage timeseries iter failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    buckets,
	})
}

// UsageByModelAPI handles GET /gtk/v1/usage/by-model.
//
// Returns per-model aggregates ordered by total_calls DESC.
//
// Query params:
//
//	range  "month" (default) | "7d" | "30d"
func UsageByModelAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	userID := int64(user.GetID(connection.DB))
	start, end := parseRange(c.Query("range"))

	rows, err := globals.QueryDb(connection.DB, `
		SELECT
			COALESCE(model_id, '')                                          AS model_id,
			COUNT(*)                                                        AS total_calls,
			COALESCE(SUM(COALESCE(input_tokens, 0)), 0)                    AS input_tokens,
			COALESCE(SUM(COALESCE(output_tokens, 0)), 0)                   AS output_tokens,
			COALESCE(SUM(COALESCE(cache_read_tokens, 0)
			         * COALESCE(markup_multiplier, 1.0)), 0)               AS cache_saved_micro,
			COALESCE(SUM(COALESCE(tokens_used, 0)), 0)                     AS credits_used,
			COALESCE(SUM(COALESCE(client_charge_micro, 0)), 0)             AS actual_spent_micro
		FROM gtk_app_usage_log
		WHERE user_id = ?
		  AND created_at BETWEEN ? AND ?
		GROUP BY model_id
		ORDER BY total_calls DESC
	`, userID, start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "usage by-model query failed: " + err.Error(),
		})
		return
	}
	defer rows.Close()

	models := make([]UsageByModel, 0, 20)
	for rows.Next() {
		var m UsageByModel
		if err := rows.Scan(&m.ModelID, &m.TotalCalls, &m.InputTokens,
			&m.OutputTokens, &m.CacheSavedMicro, &m.CreditsUsed,
			&m.ActualSpentMicro); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "usage by-model scan failed: " + err.Error(),
			})
			return
		}
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "usage by-model iter failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    models,
	})
}

// UsageRecentAPI handles GET /gtk/v1/usage/recent.
//
// Returns the last 50 usage rows newer than the given since_id or since
// Unix timestamp. Designed for Live page long-polling.
//
// Query params:
//
//	since  Unix timestamp (seconds). Rows with created_at > since are returned.
//	       Defaults to last 50 rows (no time filter) when omitted.
func UsageRecentAPI(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	userID := int64(user.GetID(connection.DB))

	// Build WHERE clause: optionally filter by created_at > since.
	var (
		query string
		args  []any
	)
	sinceParam := c.Query("since")
	if sinceParam != "" {
		sinceUnix, err := strconv.ParseInt(sinceParam, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": fmt.Sprintf("invalid since param %q: must be unix timestamp", sinceParam),
			})
			return
		}
		sinceTime := time.Unix(sinceUnix, 0)
		query = `
			SELECT id, created_at,
			       COALESCE(model_id, ''),
			       COALESCE(provider, ''),
			       COALESCE(input_tokens, 0),
			       COALESCE(output_tokens, 0),
			       COALESCE(cache_read_tokens, 0),
			       COALESCE(client_charge_micro, 0),
			       COALESCE(source, '')
			FROM gtk_app_usage_log
			WHERE user_id = ? AND created_at > ?
			ORDER BY id DESC
			LIMIT 50
		`
		args = []any{userID, sinceTime.Format("2006-01-02 15:04:05")}
	} else {
		query = `
			SELECT id, created_at,
			       COALESCE(model_id, ''),
			       COALESCE(provider, ''),
			       COALESCE(input_tokens, 0),
			       COALESCE(output_tokens, 0),
			       COALESCE(cache_read_tokens, 0),
			       COALESCE(client_charge_micro, 0),
			       COALESCE(source, '')
			FROM gtk_app_usage_log
			WHERE user_id = ?
			ORDER BY id DESC
			LIMIT 50
		`
		args = []any{userID}
	}

	rows, err := globals.QueryDb(connection.DB, query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "usage recent query failed: " + err.Error(),
		})
		return
	}
	defer rows.Close()

	recent := make([]UsageRecentRow, 0, 50)
	for rows.Next() {
		var r UsageRecentRow
		var createdAt time.Time
		if err := rows.Scan(&r.ID, &createdAt, &r.ModelID, &r.Provider,
			&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens,
			&r.ClientChargeMicro, &r.Source); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "usage recent scan failed: " + err.Error(),
			})
			return
		}
		r.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		recent = append(recent, r)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "usage recent iter failed: " + err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    recent,
	})
}
