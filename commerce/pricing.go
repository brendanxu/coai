// pricing.go — hardcoded per-model upstream cost lookup (Q6 Option A,
// PKG-2 Wave 1, L23 §6).
//
// Why hardcoded for v0:
//   The honest source of truth is NewAPI's model_ratio config, but reading
//   it on the hot path adds a NewAPI round-trip per call. v0 ships
//   constants we can ship today; follow-up PKG (Q6 Option B) replaces
//   modelPrices with a NewAPI model_ratio reader (cached) or (Option C)
//   a hot-reloadable gtk_model_pricing table.
//
// Source for as-of-2026-05-10 prices:
//   - DeepSeek: https://api-docs.deepseek.com/quick_start/pricing
//   - OpenAI: https://openai.com/api/pricing/
//   - Anthropic: https://www.anthropic.com/pricing#anthropic-api
//
// Unit: micro-cents per 1k tokens. 1 µ¢ = 1e-5 USD/CNY. Float-free; all
// math is integer to avoid the rounding drift that plagues per-call
// cost accounting at scale.
//
// Conversion to whole cents (LookupCostCents) rounds DOWN. This is
// documented behavior, not a bug — sub-cent calls cost the user 0¢
// for accounting purposes. Provider margin math should aggregate
// micro-cents internally before rounding for reports.

package commerce

// modelPrices is the v0 hardcoded table. Add entries here as new upstream
// models become reachable through the gateway. Unknown models return
// (0, false) from LookupCostCents — caller decides whether to log + write
// a 0-cost row (preserves the call audit) or fail closed.
//
// Maintenance: when adjusting prices, update both columns + the comment
// timestamp in the file header. Keep entries sorted by provider then by
// price tier (cheapest → most expensive) for readability.
var modelPrices = map[string]ModelPrice{
	// DeepSeek — cheapest tier; primary indie/民宿 model.
	"deepseek-chat": {
		MicroCentsPer1kInput:  14, // $0.00014 / 1K input
		MicroCentsPer1kOutput: 28, // $0.00028 / 1K output
		Provider:              "deepseek",
	},
	"deepseek-reasoner": {
		MicroCentsPer1kInput:  55,  // $0.00055 / 1K input
		MicroCentsPer1kOutput: 219, // $0.00219 / 1K output
		Provider:              "deepseek",
	},
	// OpenAI — mid tier; gpt-4o-mini is the cheap default.
	"gpt-4o-mini": {
		MicroCentsPer1kInput:  15, // $0.00015 / 1K input
		MicroCentsPer1kOutput: 60, // $0.00060 / 1K output
		Provider:              "openai",
	},
	"gpt-4o": {
		MicroCentsPer1kInput:  250,  // $0.0025 / 1K input
		MicroCentsPer1kOutput: 1000, // $0.0100 / 1K output
		Provider:              "openai",
	},
	// Anthropic — premium tier; claude-3-5-haiku is the cheap option.
	"claude-3-5-haiku-20241022": {
		MicroCentsPer1kInput:  80,  // $0.0008 / 1K input
		MicroCentsPer1kOutput: 400, // $0.0040 / 1K output
		Provider:              "anthropic",
	},
	"claude-3-5-sonnet-20241022": {
		MicroCentsPer1kInput:  300,  // $0.003 / 1K input
		MicroCentsPer1kOutput: 1500, // $0.015 / 1K output
		Provider:              "anthropic",
	},
}

// LookupCostCents returns the cost in WHOLE CENTS (rounded down from
// micro-cents) and a boolean indicating whether the model was found in
// the pricing table.
//
// Returns:
//
//	(cost in cents, true)   when model is known
//	(0, false)              when model is unknown — caller decides
//
// Caller pattern (Wave 2 B2):
//
//	cost, known := pricing.LookupCostCents(model, tokensIn, tokensOut)
//	if !known {
//	    globals.Warn("unknown model in pricing table: " + model)
//	    cost = 0  // write the row anyway for audit; bill 0
//	}
//
// Math: micro-cents-per-1k * tokens / 1000 = micro-cents.
// Then divide by 1000 again for cents (floor).
//
// The two-step divide is integer-safe: a single big multiply that
// overflows int64 would need >2^53 token-cents, which is not realistic
// for one call (gpt-4o at 1M tokens out = 1M * 1000 µ¢ / 1k = 1M µ¢ =
// $10 — fits comfortably).
func LookupCostCents(model string, tokensIn, tokensOut int64) (costCents int64, knownModel bool) {
	mp, ok := modelPrices[model]
	if !ok {
		return 0, false
	}
	// micro-cents = µ¢/1k * tokens / 1000
	microCents := (mp.MicroCentsPer1kInput*tokensIn)/1000 +
		(mp.MicroCentsPer1kOutput*tokensOut)/1000
	// cents = micro-cents / 1000 (floor)
	return microCents / 1000, true
}

// LookupProvider returns the provider tag for a model (e.g. "openai",
// "anthropic", "deepseek"), or empty string if the model is not in the
// pricing table. Wave 2 B2 stamps this into UsageCostEntry.Provider so
// margin math can group by provider without a second lookup.
func LookupProvider(model string) string {
	mp, ok := modelPrices[model]
	if !ok {
		return ""
	}
	return mp.Provider
}
