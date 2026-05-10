// cost_ledger.go — unified write path for gtk_app_usage_log (PKG-2 Wave 2 B2,
// L23 §6 + plan v2 §B2).
//
// Background:
//
//	gtk_app_usage_log is the per-call cost record. Pre-PKG-2 the chat handler
//	wrote rows directly; service/runtime.go (D1, Wave 4) needs to write the
//	same shape from a service-order context, and PKG-1 added three
//	attribution columns (source / order_id / provider) that callers must
//	populate consistently. To keep schema evolution cheap and to make
//	"how did this row get written?" a one-grep question, this file defines
//	the SINGLE write path:
//
//	  WriteUsageCost              — dumb persister; trust caller's CostCents
//	  ComputeAndWriteUsageCost    — convenience wrapper: pricing.LookupCostCents
//	                                 + WriteUsageCost in one call
//
//	Chat / api / service-order callers funnel through one of these. Future
//	column additions only need to land here, not at every call site.
//
// Unknown-model behavior (per Q6 Option A spec):
//
//	When pricing.LookupCostCents returns (_, false) inside
//	ComputeAndWriteUsageCost, the row is still written with cost_cents=0
//	and a warning is logged identifying the model. Rationale: a new
//	upstream model not yet listed in commerce/pricing.go shouldn't fail
//	the caller's flow (chat completion / service run); it should preserve
//	the call audit and let an operator add the entry to pricing.go.
//
// Reference:
//
//	docs/strategy/2026-05-10-PKG-2-shared-commerce-backbone-plan-v2.md §B2
//	docs/strategy/2026-05-09-token-product-service-product-architecture.md §6

package commerce

import (
	"chat/globals"
	"database/sql"
	"fmt"
)

// WriteUsageCost inserts a row into gtk_app_usage_log with all PKG-1
// attribution fields (source / order_id / provider) populated from entry.
// This is the SINGLE write path for usage data — service/runtime.go and
// any future chat/api hook must call this rather than writing the table
// directly. Single-write-path discipline lets the cost ledger schema
// evolve (e.g. add cost_micro_cents in a follow-up PKG) without touching
// every caller.
//
// Behavior:
//   - Trusts entry.CostCents as-is. The caller is responsible for computing
//     the cost they want billed (use ComputeAndWriteUsageCost if you want
//     pricing.LookupCostCents to do it for you).
//   - source / order_id / provider are written exactly as the caller supplies.
//     entry.Source MUST be one of 'chat' | 'api' | 'service_order' |
//     'admin_test' (CHECK constraint at the DB layer; we don't re-validate
//     to avoid a second source of truth).
//   - For source='service_order', set entry.OrderID (Valid=true) to
//     gtk_service_order.order_no — this enables per-order cost rollup
//     queries (idx_gtk_usage_source_order covers the common shape).
//   - For source='chat'|'api', entry.OrderID is typically Valid=false
//     since those callers have no order context.
//
// Returns the underlying ExecDb error (wrapped with %w) so callers can
// errors.Is / errors.As the original db error if needed.
func WriteUsageCost(db *sql.DB, entry UsageCostEntry) error {
	_, err := globals.ExecDb(db, `
		INSERT INTO gtk_app_usage_log
		  (user_id, plan_id, service, source, order_id, provider, tokens_used, cost_cents)
		VALUES
		  (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		entry.UserID,
		entry.PlanID,
		entry.Service,
		entry.Source,
		entry.OrderID,
		entry.Provider,
		entry.TokensUsed,
		entry.CostCents,
	)
	if err != nil {
		return fmt.Errorf("insert gtk_app_usage_log: %w", err)
	}
	return nil
}

// ComputeAndWriteUsageCost is a convenience wrapper for callers who hold
// raw token counts but don't want to invoke pricing.LookupCostCents +
// WriteUsageCost themselves. Used by Wave 4 D1 (service/runtime.go
// finalizeRun), which has parsed.Usage.PromptTokens + CompletionTokens
// straight off the upstream response.
//
// Lookup key:
//
//	The model name is taken from entry.Service. This is the same string
//	the chat layer puts in gtk_app_usage_log.service today (e.g.
//	"deepseek-chat", "gpt-4o"), so reusing the field avoids an extra
//	parameter and keeps the contract: "service is the model slug".
//
// Behavior:
//   - On known model: stamps entry.CostCents from LookupCostCents and
//     entry.Provider from LookupProvider, then INSERTs.
//   - On unknown model: logs a warning identifying the model, sets
//     entry.CostCents=0 and leaves entry.Provider as the caller passed it
//     (typically Valid=false), then INSERTs anyway. Returns nil — the
//     caller's primary flow does NOT fail because pricing.go is missing
//     an entry. (DB error during INSERT is still surfaced, as in
//     WriteUsageCost.)
//
// Caller's responsibility: tokensIn + tokensOut should sum to
// entry.TokensUsed. We don't enforce this (callers occasionally have
// only the total, e.g. legacy upstream responses) but downstream rollup
// math assumes consistency. If you only have the total, split it via
// the upstream's typical ratio or pass tokensIn=0, tokensOut=total to
// charge as if everything were output (more conservative for cost).
func ComputeAndWriteUsageCost(db *sql.DB, entry UsageCostEntry, tokensIn, tokensOut int64) error {
	cost, known := LookupCostCents(entry.Service, tokensIn, tokensOut)
	if !known {
		globals.Warn(fmt.Sprintf(
			"commerce/cost_ledger: unknown model %q (user_id=%d source=%s) — writing 0 cost; add entry to commerce/pricing.go",
			entry.Service, entry.UserID, entry.Source))
		entry.CostCents = 0
		// Leave entry.Provider as caller passed it (typically NullString
		// Valid=false). We don't overwrite with "" because that'd persist
		// the literal empty string instead of NULL.
	} else {
		entry.CostCents = cost
		entry.Provider = sql.NullString{String: LookupProvider(entry.Service), Valid: true}
	}
	return WriteUsageCost(db, entry)
}
