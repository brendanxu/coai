package carbon

import (
	_ "embed"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"

	"chat/globals"
)

//go:embed data/eco_routing.json
var ecoRoutingJSON []byte

// EcoMapping is one row of the eco model substitution table.
type EcoMapping struct {
	From               string `json:"from"`
	To                 string `json:"to"`
	ExpectedSavingsPct int    `json:"expected_savings_pct"`
	Notes              string `json:"notes,omitempty"`
}

// EcoRoutingTable is the full JSON shape of eco_routing.json.
type EcoRoutingTable struct {
	Version              string       `json:"version"`
	Description          string       `json:"description"`
	Mappings             []EcoMapping `json:"mappings"`
	SkipAlreadyEfficient []string     `json:"skip_already_efficient"`
	CalibrationNote      string       `json:"calibration_note"`
}

var (
	ecoTable      EcoRoutingTable
	ecoIdx        map[string]EcoMapping // key = lower(from)
	ecoSkipSet    map[string]struct{}   // lower(model)
	ecoOnce       sync.Once
)

func loadEco() {
	if err := json.Unmarshal(ecoRoutingJSON, &ecoTable); err != nil {
		globals.Warn("carbon: failed to parse embedded eco_routing.json: " + err.Error())
		return
	}
	ecoIdx = make(map[string]EcoMapping, len(ecoTable.Mappings))
	for _, m := range ecoTable.Mappings {
		ecoIdx[strings.ToLower(m.From)] = m
	}
	ecoSkipSet = make(map[string]struct{}, len(ecoTable.SkipAlreadyEfficient))
	for _, m := range ecoTable.SkipAlreadyEfficient {
		ecoSkipSet[strings.ToLower(m)] = struct{}{}
	}
}

// EcoRouteResult describes the outcome of a route lookup.
type EcoRouteResult struct {
	NewModel             string // model to actually use (== orig if no route)
	OriginalModel        string // model the user requested
	DidRoute             bool   // true iff a substitution happened
	AlreadyEfficient     bool   // true iff orig is in skip_already_efficient list
	ExpectedSavingsPct   int    // 0 unless DidRoute=true
}

// MaybeRouteEco decides whether to substitute origModel with a smaller variant.
// It does NOT consult user.eco_mode — that gate lives in the caller (so tests
// can exercise routing logic independently of DB state).
func MaybeRouteEco(origModel string) EcoRouteResult {
	ecoOnce.Do(loadEco)
	res := EcoRouteResult{NewModel: origModel, OriginalModel: origModel}
	if ecoIdx == nil {
		return res
	}
	if _, skip := ecoSkipSet[strings.ToLower(origModel)]; skip {
		res.AlreadyEfficient = true
		return res
	}
	if m, ok := ecoIdx[strings.ToLower(origModel)]; ok {
		res.NewModel = m.To
		res.DidRoute = true
		res.ExpectedSavingsPct = m.ExpectedSavingsPct
	}
	return res
}

// GetEcoRoutingTable exposes the parsed table for the /api/carbon/factors
// endpoint companion + tests.
func GetEcoRoutingTable() *EcoRoutingTable {
	ecoOnce.Do(loadEco)
	return &ecoTable
}

// User prefs ------------------------------------------------------------

// UserCarbonPrefs is the per-user state stored in user_carbon_prefs.
type UserCarbonPrefs struct {
	UserID                    int  `json:"user_id"`
	EcoMode                   bool `json:"eco_mode"`
	EcoModeFirstFeedbackSeen  bool `json:"eco_mode_first_feedback_seen"`
}

// GetUserCarbonPrefs reads the user's carbon prefs row. If no row exists,
// returns the zero-value struct (eco_mode=false) — matches the D7 design
// decision (Eco Mode default OFF).
func GetUserCarbonPrefs(db *sql.DB, userID int) UserCarbonPrefs {
	prefs := UserCarbonPrefs{UserID: userID}
	row := globals.QueryRowDb(db, `
		SELECT eco_mode, eco_mode_first_feedback_seen
		FROM user_carbon_prefs WHERE user_id = ?
	`, userID)
	// Both columns may be NULL/0 if no row; sql.Scan into bool handles that.
	if err := row.Scan(&prefs.EcoMode, &prefs.EcoModeFirstFeedbackSeen); err != nil && err != sql.ErrNoRows {
		// Not a hard failure — log and return zero prefs.
		globals.Warn("carbon: GetUserCarbonPrefs scan error for user " + itoa(userID) + ": " + err.Error())
	}
	return prefs
}

// SetUserEcoMode upserts the user's eco_mode flag. Lazy-creates the row on
// first toggle.
func SetUserEcoMode(db *sql.DB, userID int, enabled bool) error {
	_, err := globals.ExecDb(db, `
		INSERT INTO user_carbon_prefs (user_id, eco_mode)
		VALUES (?, ?)
		ON DUPLICATE KEY UPDATE eco_mode = VALUES(eco_mode), updated_at = CURRENT_TIMESTAMP
	`, userID, enabled)
	return err
}

// MarkEcoFirstFeedbackSeen records that the user has seen the "Eco saved X%"
// pill enough times. Frontend caps the show count at 5 then calls this to
// stop showing.
func MarkEcoFirstFeedbackSeen(db *sql.DB, userID int) error {
	_, err := globals.ExecDb(db, `
		INSERT INTO user_carbon_prefs (user_id, eco_mode_first_feedback_seen)
		VALUES (?, TRUE)
		ON DUPLICATE KEY UPDATE eco_mode_first_feedback_seen = TRUE, updated_at = CURRENT_TIMESTAMP
	`, userID)
	return err
}

// itoa is a tiny inline helper to avoid pulling in strconv just for log lines.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
