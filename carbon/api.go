package carbon

import (
	"database/sql"
	"net/http"
	"time"

	"chat/auth"
	"chat/globals"
	"chat/utils"

	"github.com/gin-gonic/gin"
)

// ----- Response shapes -----------------------------------------------------

type ByModelEntry struct {
	Model   string  `json:"model"`
	G       float64 `json:"g"`
	Pct     float64 `json:"pct"`
	Tokens  int     `json:"tokens"`
}

type SummaryResponse struct {
	Month               string         `json:"month"`                // YYYY-MM
	TotalG              float64        `json:"total_g"`
	LastMonthG          float64        `json:"last_month_g"`
	DeltaPct            float64        `json:"delta_pct"` // negative = improvement
	ByModel             []ByModelEntry `json:"by_model"`
	CoefficientVersion  string         `json:"coefficient_version"`
	ErrorMarginPct      int            `json:"error_margin_pct"`
	FirstMonth          bool           `json:"first_month"`
}

type EcoModeBody struct {
	Enabled bool `json:"enabled"`
}

type EcoModeResponse struct {
	EcoMode   bool   `json:"eco_mode"`
	UpdatedAt string `json:"updated_at"`
}

// ----- Helpers -------------------------------------------------------------

// monthBounds returns [start, end) for the given YYYY-MM string.
func monthBounds(monthStr string) (start, end time.Time, ok bool) {
	t, err := time.Parse("2006-01", monthStr)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	start = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	end = start.AddDate(0, 1, 0)
	return start, end, true
}

// sumCarbon aggregates total_g + by-model breakdown for [start, end).
func sumCarbon(db *sql.DB, userID int64, start, end time.Time) (totalG float64, totalTokens int, byModel []ByModelEntry) {
	rows, err := globals.QueryDb(db, `
		SELECT model, COALESCE(SUM(co2g_estimate), 0) AS g, COALESCE(SUM(tokens), 0) AS tokens
		FROM usage_carbon
		WHERE user_id = ? AND created_at >= ? AND created_at < ? AND co2g_estimate IS NOT NULL
		GROUP BY model
		ORDER BY g DESC
	`, userID, start, end)
	if err != nil {
		globals.Warn("carbon: sumCarbon query error: " + err.Error())
		return 0, 0, nil
	}
	defer rows.Close()

	for rows.Next() {
		var entry ByModelEntry
		if err := rows.Scan(&entry.Model, &entry.G, &entry.Tokens); err != nil {
			globals.Warn("carbon: sumCarbon scan error: " + err.Error())
			continue
		}
		totalG += entry.G
		totalTokens += entry.Tokens
		byModel = append(byModel, entry)
	}
	// Compute percentages
	if totalG > 0 {
		for i := range byModel {
			byModel[i].Pct = byModel[i].G / totalG * 100.0
		}
	}
	return totalG, totalTokens, byModel
}

// userHasAnyData returns true if the user has at least one usage_carbon row
// before `end`. Used to distinguish first_month from "this month is just empty".
func userHasAnyData(db *sql.DB, userID int64, before time.Time) bool {
	row := globals.QueryRowDb(db, `
		SELECT 1 FROM usage_carbon WHERE user_id = ? AND created_at < ? LIMIT 1
	`, userID, before)
	var x int
	err := row.Scan(&x)
	return err == nil
}

// ----- Handlers ------------------------------------------------------------

// summaryHandler — GET /api/carbon/summary?month=YYYY-MM (default: current month)
func summaryHandler(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	db := utils.GetDBFromContext(c)
	userID := user.GetID(db)

	monthStr := c.Query("month")
	if monthStr == "" {
		monthStr = time.Now().UTC().Format("2006-01")
	}
	start, end, ok := monthBounds(monthStr)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid month format; expected YYYY-MM"})
		return
	}

	thisG, _, byModel := sumCarbon(db, userID, start, end)

	// Last-month delta
	lastStart := start.AddDate(0, -1, 0)
	lastG, _, _ := sumCarbon(db, userID, lastStart, start)

	var deltaPct float64
	if lastG > 0 {
		deltaPct = (thisG - lastG) / lastG * 100.0
	}

	firstMonth := lastG == 0 && !userHasAnyData(db, userID, start)

	tbl := GetFactorsTable()
	c.JSON(http.StatusOK, SummaryResponse{
		Month:              monthStr,
		TotalG:             round4(thisG),
		LastMonthG:         round4(lastG),
		DeltaPct:           round2(deltaPct),
		ByModel:            roundByModel(byModel),
		CoefficientVersion: tbl.Version,
		ErrorMarginPct:     tbl.ErrorMarginPct,
		FirstMonth:         firstMonth,
	})
}

// byModelHandler — GET /api/carbon/by-model?from=YYYY-MM-DD&to=YYYY-MM-DD
// Defaults to current month when omitted.
func byModelHandler(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	db := utils.GetDBFromContext(c)
	userID := user.GetID(db)

	fromStr := c.Query("from")
	toStr := c.Query("to")
	var start, end time.Time
	if fromStr == "" || toStr == "" {
		now := time.Now().UTC()
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 1, 0)
	} else {
		s, err1 := time.Parse("2006-01-02", fromStr)
		e, err2 := time.Parse("2006-01-02", toStr)
		if err1 != nil || err2 != nil || !e.After(s) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid date range; expected from/to as YYYY-MM-DD"})
			return
		}
		start, end = s, e.AddDate(0, 0, 1) // inclusive
	}

	totalG, totalTokens, byModel := sumCarbon(db, userID, start, end)
	c.JSON(http.StatusOK, gin.H{
		"from":     start.Format("2006-01-02"),
		"to":       end.AddDate(0, 0, -1).Format("2006-01-02"),
		"total_g":  round4(totalG),
		"tokens":   totalTokens,
		"by_model": roundByModel(byModel),
	})
}

// factorsHandler — GET /api/carbon/factors
// Returns the embedded carbon_factors.json content for the Methodology page.
// Auth required to keep coefficient table behind the same trust boundary as
// the rest of the carbon API.
func factorsHandler(c *gin.Context) {
	if auth.RequireAuth(c) == nil {
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", GetFactorsJSON())
}

// setEcoModeHandler — PUT /api/user/eco-mode
func setEcoModeHandler(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	var body EcoModeBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body; expected { enabled: bool }"})
		return
	}
	db := utils.GetDBFromContext(c)
	userID := user.GetID(db)
	if err := SetUserEcoMode(db, userID, body.Enabled); err != nil {
		globals.Warn("carbon: SetUserEcoMode error: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save preference"})
		return
	}
	c.JSON(http.StatusOK, EcoModeResponse{
		EcoMode:   body.Enabled,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

// markFeedbackSeenHandler — POST /api/user/eco-mode/feedback-seen
// Records that the user has been shown the "Eco saved X%" pill enough times.
// Frontend caps at 5 then calls this to stop showing.
func markFeedbackSeenHandler(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	db := utils.GetDBFromContext(c)
	userID := user.GetID(db)
	if err := MarkEcoFirstFeedbackSeen(db, userID); err != nil {
		globals.Warn("carbon: MarkEcoFirstFeedbackSeen error: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save preference"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// prefsHandler — GET /api/user/eco-mode
// Returns the user's current carbon prefs. Used by the FE on app load.
func prefsHandler(c *gin.Context) {
	user := auth.RequireAuth(c)
	if user == nil {
		return
	}
	db := utils.GetDBFromContext(c)
	prefs := GetUserCarbonPrefs(db, user.GetID(db))
	c.JSON(http.StatusOK, prefs)
}

// ----- Rounding helpers (avoid sending 16-digit floats over the wire) ------

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100.0
}

func round4(f float64) float64 {
	return float64(int(f*10000+0.5)) / 10000.0
}

func roundByModel(in []ByModelEntry) []ByModelEntry {
	out := make([]ByModelEntry, len(in))
	for i, e := range in {
		out[i] = ByModelEntry{Model: e.Model, G: round4(e.G), Pct: round2(e.Pct), Tokens: e.Tokens}
	}
	return out
}
