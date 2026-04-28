// Package carbon implements greentokey's v0.6 carbon footprint v1:
//   - per-completion CO2e estimation written to usage_carbon
//   - Eco Mode model substitution (chat completion model swap pre-handler)
//   - dashboard / methodology / by-model API endpoints
//
// The package is intentionally isolated from CoAI's existing `manager` and
// `auth` packages. Coupling stays one-way: carbon imports from auth/utils/
// globals; nothing imports carbon except main.go (route registration) and
// manager/chat_completions.go (two surgical hook calls).
package carbon

// Tier classifies a single message's carbon cost into low / mid / high
// for UI color signaling. Boundaries match the design contract — any change
// here must update tierFor() in app/src/components/Carbon/tier.ts to match.
type Tier int

const (
	TierLow  Tier = iota // < 0.5 g CO2e
	TierMid              // 0.5 g – 2.0 g CO2e
	TierHigh             // > 2.0 g CO2e
)

// TierFor returns the tier for a given gCO2e value. Used by tests; the actual
// frontend rendering uses the same boundaries in tier.ts (mirrored).
func TierFor(co2g float64) Tier {
	switch {
	case co2g < 0.5:
		return TierLow
	case co2g < 2.0:
		return TierMid
	default:
		return TierHigh
	}
}

// Notes column values for usage_carbon rows.
const (
	NoteStreamError    = "stream_error"
	NoteCoefficientGap = "coefficient_gap"
)
