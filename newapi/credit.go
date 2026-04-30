// Credit unit and tier classification — greentokey customer-facing
// pricing primitives.
//
// Why credits not tokens (founder decision 2026-04-30):
//
//   * Token transparency would expose the per-model cost spread (40-75x
//     between sub2API Claude and official Anthropic API). Users would
//     game the cheap channel and farm the founder's Plus/Max accounts.
//
//   * Credits abstract the source. Whatever channel NewAPI routes to,
//     1 credit costs the user a fixed amount; cost optimization stays
//     internal to greentokey.
//
//   * Marketing simplicity: "5000 credits / 月" parses as a value bundle.
//     "7,500,000 tokens" doesn't.
//
// Mapping math (locked 2026-04-30):
//
//   ¥99/月 套餐 = $15/月 套餐 = 5000 credits
//   1 credit = 1500 NewAPI quota units (≈ $0.003 / credit)
//   5000 credits = 7,500,000 quota units = $15.00 in NewAPI's default
//                  $1 = 500_000 quota internal accounting.
//
// Per-call burn (assuming a "standard" call ≈ 1k input + 1k output):
//
//   轻量 (light)    0.5 credits/call  — DeepSeek-chat, Qwen-flash, GLM-4-air
//   标准 (standard) 1.0 credits/call  — DeepSeek-r1, Claude-haiku-4.5,
//                                        GPT-4o-mini, Qwen-max, Gemini-2.0
//   高级 (premium)  3.0 credits/call  — GPT-4o, Claude-sonnet-4,
//                                        Claude-opus-4
//
// Reality check: token volume varies. A 100-token query on a "standard"
// model costs ~0.05 credit; a 10k-context query costs ~5 credits. The
// per-call numbers above are illustrative averages.
//
// These tier multipliers correspond to NewAPI model_ratio values:
//
//   tier_multiplier = NewAPI model_ratio (since 1 credit = 1500 quota
//                                         and 1500 quota = 1k+1k tokens
//                                         at model_ratio = 0.75 each)
//
// To apply: configure NewAPI's per-model ratio in admin settings such
// that lightweight models have ratio 0.375, standard 0.75, premium 2.25.
// (This is the founder/admin one-shot setup — not done in code here.)

package newapi

import "strings"

// QuotaPerCredit is the locked conversion: 1 credit = 1500 NewAPI quota
// units. Derived from "5000 credits = $15 = 7,500,000 quota". Changing
// this rebalances the entire pricing structure — coordinate with marketing.
const QuotaPerCredit = 1500

// CreditTier classifies a model into greentokey's 3-tier pricing band.
type CreditTier int

const (
	TierLight    CreditTier = iota // 0.5 credit/call
	TierStandard                   // 1.0 credit/call (default)
	TierPremium                    // 3.0 credit/call
)

// Multiplier returns the per-call credit multiplier for the tier.
// Used for the "1 credit per Claude call" copy + dashboard display.
func (t CreditTier) Multiplier() float64 {
	switch t {
	case TierLight:
		return 0.5
	case TierStandard:
		return 1.0
	case TierPremium:
		return 3.0
	}
	return 1.0
}

// Label returns the Chinese band name for UI surfaces.
func (t CreditTier) Label() string {
	switch t {
	case TierLight:
		return "轻量"
	case TierStandard:
		return "标准"
	case TierPremium:
		return "高级"
	}
	return "标准"
}

// tierBindings — model-name prefix or substring → tier. The match is:
//   1. exact match on full model id (e.g. "claude-sonnet-4-20250514")
//   2. exact match on canonical name (e.g. "claude-sonnet-4")
//   3. substring match on family token (e.g. "haiku" → TierStandard,
//      "sonnet" → TierPremium, "opus" → TierPremium)
//
// New models added to the pool fall through to TierStandard until the
// admin extends this map. That's intentionally conservative — premium
// models that should be classified as TierPremium will be undercharging
// users until reclassified, but lightweight models classified as
// TierStandard just slightly overcharge (manageable).
var tierBindings = []struct {
	match string
	tier  CreditTier
}{
	// === Light tier (0.5x) — fast, cheap, often sub2API or near-cost ===
	{"deepseek-chat", TierLight},
	{"qwen-flash", TierLight},
	{"glm-4-air", TierLight},
	{"gpt-3.5-turbo", TierLight},

	// === Premium tier (3x) — frontier flagships ===
	{"gpt-4o", TierPremium},        // OpenAI flagship
	{"gpt-4-turbo", TierPremium},   // OpenAI legacy frontier
	{"claude-opus", TierPremium},   // Anthropic flagship
	{"claude-sonnet", TierPremium}, // Anthropic mid-flagship; covers sonnet-4, sonnet-4.5
	{"o1", TierPremium},            // OpenAI reasoning
	{"o3", TierPremium},

	// === Standard tier (1x) — workhorse models ===
	// Anything matched here OR not matched above defaults to standard.
	{"deepseek-r1", TierStandard},
	{"deepseek-v3", TierStandard},
	{"claude-haiku", TierStandard},  // includes haiku-4, haiku-4.5
	{"gpt-4o-mini", TierStandard},   // cheaper OpenAI variant
	{"qwen-max", TierStandard},
	{"qwen-vl", TierStandard},
	{"glm-4-plus", TierStandard},
	{"moonshot", TierStandard},      // Kimi standard
	{"gemini-2", TierStandard},
	{"gemini-1.5-flash", TierLight}, // override: flash is light
}

// CreditTierFor returns the tier for a given model id. Substring matching
// is case-insensitive. Falls back to TierStandard for unknown models.
func CreditTierFor(model string) CreditTier {
	m := strings.ToLower(model)
	// First pass: exact match (covers full canonical ids first).
	for _, b := range tierBindings {
		if m == strings.ToLower(b.match) {
			return b.tier
		}
	}
	// Second pass: family-name substring (covers versioned variants).
	// Order matters — TierLight binds first to overrides like
	// "gemini-1.5-flash" before "gemini-2" matches it as standard.
	for _, b := range tierBindings {
		if strings.Contains(m, strings.ToLower(b.match)) {
			return b.tier
		}
	}
	return TierStandard
}

// CreditsToQuota converts a credit count to NewAPI's internal quota units.
// Used when topping up a user's NewAPI quota after a purchase: the LS
// webhook says "user bought 5000 credits", we set NewAPI quota to
// 5000 * 1500 = 7,500,000 units.
func CreditsToQuota(credits int64) int64 {
	return credits * QuotaPerCredit
}

// QuotaToCredits converts NewAPI's quota units to credits for display.
// User-facing balance uses this. Floor division — we never display
// fractional credits ("you have 4823 credits", not "4823.4").
func QuotaToCredits(quota int64) int64 {
	return quota / QuotaPerCredit
}

// QuotaToCreditsPrecise is the float version, used for "you used X.YY
// credits this call" displays. Returns 0 for negative or zero quota.
func QuotaToCreditsPrecise(quota int64) float64 {
	if quota <= 0 {
		return 0
	}
	return float64(quota) / float64(QuotaPerCredit)
}
