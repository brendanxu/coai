package carbon

import (
	"database/sql"

	"chat/globals"
)

// UsageEvent is a single chat-completion's worth of carbon-relevant data.
// Constructed at the end of the relay handler in manager/chat_completions.go
// and handed to GoLogUsage for fire-and-forget persistence.
type UsageEvent struct {
	UserID        int64
	Model         string // the model that was actually used (post-eco-route)
	Region        string // optional; "" → use default_region
	Tokens        int    // total_tokens from the upstream usage block (may be 0 on stream error)
	EcoRoutedFrom string // original requested model if eco-routed, else ""
	StreamError   bool   // true when the stream errored mid-completion (D9)
}

// LogUsage writes a single usage_carbon row. Rules:
//   - StreamError=true → co2g_estimate NULL, notes='stream_error'
//   - coefficient missing → co2g_estimate NULL, notes='coefficient_gap'
//   - both above paths still record the row (we want evidence the request happened)
//   - happy path → co2g_estimate computed, version recorded
//
// Returns the DB error; callers using GoLogUsage will receive nil from the
// goroutine and log errors internally.
func LogUsage(db *sql.DB, ev UsageEvent) error {
	var (
		co2g       sql.NullFloat64
		version    sql.NullString
		ecoFrom    sql.NullString
		notes      sql.NullString
		regionVal  sql.NullString
	)

	if ev.Region != "" {
		regionVal = sql.NullString{String: ev.Region, Valid: true}
	}
	if ev.EcoRoutedFrom != "" {
		ecoFrom = sql.NullString{String: ev.EcoRoutedFrom, Valid: true}
	}

	switch {
	case ev.StreamError:
		notes = sql.NullString{String: NoteStreamError, Valid: true}
	default:
		if g, ver, ok := EstimateCO2(ev.Tokens, ev.Model, ev.Region); ok {
			co2g = sql.NullFloat64{Float64: g, Valid: true}
			version = sql.NullString{String: ver, Valid: true}
		} else {
			notes = sql.NullString{String: NoteCoefficientGap, Valid: true}
		}
	}

	_, err := globals.ExecDb(db, `
		INSERT INTO usage_carbon
			(user_id, model, region, tokens, co2g_estimate,
			 coefficient_version, eco_routed_from, notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, ev.UserID, ev.Model, regionVal, ev.Tokens, co2g, version, ecoFrom, notes)
	return err
}

// GoLogUsage runs LogUsage in a fresh goroutine. Errors are logged via
// globals.Warn — never bubble up to the caller. This is the function the
// chat completion hook should call.
func GoLogUsage(db *sql.DB, ev UsageEvent) {
	go func() {
		if err := LogUsage(db, ev); err != nil {
			globals.Warn("carbon: LogUsage failed: " + err.Error())
		}
	}()
}
