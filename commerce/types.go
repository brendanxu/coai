// Package commerce is greentokey's shared commerce backbone (PKG-2, v0.18,
// L23 §6 §7 §8 §9 §16 §19). It unifies the two product types — Token Plan
// (rented bundle of LLM quota) and Service Product (one-shot or recurring
// agent-run rental) — behind a single set of commerce primitives:
//
//   - PaymentSession: ephemeral state between "user clicks Buy" and
//     "webhook acks payment" (D2 in plan v2).
//   - UsageCostEntry: per-call cost write to gtk_app_usage_log; the only
//     write path for usage cost across chat / api / service-order callers
//     (D1 in plan v2 + CR2: greenfield "add usage write").
//   - EntitlementGrant: post-payment provisioning + state flip
//     (token plan → upsert gtk_user_plan + provision NewAPI;
//      service product → flip gtk_service_order to 'paid').
//   - OrderRef: read-only abstraction over the two physical order tables
//     (gtk_user_plan for tokens, gtk_service_order for services). Lets
//     callers (refund flows, webhook dispatchers, admin UI) operate on
//     orders without knowing the storage shape.
//   - Pricing: per-model upstream cost lookup. v0 = hardcoded micro-cents
//     table here (Q6 Option A); follow-up PKG reads NewAPI model_ratio.
//
// Architecture refs:
//   - docs/strategy/2026-05-09-token-product-service-product-architecture.md
//     §6 (cost ledger), §7 (entitlement state), §8 (commerce primitives),
//     §9 (refund state machine), §16 (newapi_group), §19 (per-user channel).
//   - docs/strategy/2026-05-10-PKG-2-shared-commerce-backbone-plan-v2.md
//     (chunk A foundation; D1–D8 design decisions).
//
// ⚠️ FROZEN at end of PKG-2 Wave 1 (2026-05-10). Wave 2/3/4 agents may NOT
// add fields to the types defined in this file. New types live in their own
// files (e.g. commerce/entitlement_types.go for B3-only structs). This is
// the merge-collision firewall; if Wave 2/3/4 needs a new field, it adds a
// new type alongside or extends in its own file.
package commerce

import (
	"database/sql"
	"time"
)

// ProductType discriminates the two commerce flows the backbone unifies.
// Mirrors the ENUM('token','service') on gtk_plan.product_type and
// gtk_user_plan.product_type added in PKG-1 (v0.17). Stored as the literal
// string in DB; do not renumber — these are the wire format.
type ProductType string

const (
	// ProductToken is a Token Plan rental: the user buys an N-month bundle
	// of LLM quota that materializes as a NewAPI api-key + gtk_user_plan
	// row. Provisioning = ProvisionForPlan.
	ProductToken ProductType = "token"

	// ProductService is a Service Product rental: the user buys a single
	// agent-run (民宿 SaaS workflows etc.). Provisioning = flip
	// gtk_service_order.status='paid' and let runtime pick it up.
	ProductService ProductType = "service"
)

// EntitlementState is the post-payment lifecycle state of a user's
// entitlement to a product. For token plans, mirrors gtk_user_plan.status;
// for service products, derived from gtk_service_order.status.
//
// Why an explicit type rather than reusing the per-table status strings:
// gives callers (admin UI, refund flow, dashboards) a single vocabulary
// across both product types so they don't have to translate
// 'paid'/'completed'/'refunded' (service) ↔ 'active'/'expired'/'canceled'
// (token).
type EntitlementState string

const (
	// EntitlementActive — user can use the product. Token: gtk_user_plan
	// status='active' AND not past expire_at. Service:
	// gtk_service_order status in ('paid','running').
	EntitlementActive EntitlementState = "active"

	// EntitlementExpired — natural end-of-life. Token: past expire_at.
	// Service: not used (services don't naturally expire; they complete
	// or refund). Defensive value for future per-service rental tiers.
	EntitlementExpired EntitlementState = "expired"

	// EntitlementCanceled — user-initiated or webhook-driven cancel.
	// Token: gtk_user_plan status='canceled' (e.g. cancel_at_period_end).
	// Service: status='canceled_mid_flight' (refund during run, CR5).
	EntitlementCanceled EntitlementState = "canceled"

	// EntitlementRevoked — admin or refund-driven hard removal.
	// Token: gtk_user_plan status='canceled' AND cancellation_reason
	// startsWith 'refund_'. Service: status='refunded_post_delivery'
	// (refund after completion, CR5).
	EntitlementRevoked EntitlementState = "revoked"
)

// OrderRef is the read-only abstraction over the two physical order tables
// (gtk_user_plan for token plans, gtk_service_order for service products).
//
// Why an interface (D1 in plan v2): we rejected the "create a new
// gtk_order header table" approach because both physical tables already
// have all fields refund/dispatch flows need. The interface unifies access
// without forcing a write-time double-write or a runtime denormalization.
//
// Implementations live next to their physical table (e.g.
// service.OrderRefAdapter wraps a gtk_service_order row). Wave 2 B1
// implements both adapters when needed.
type OrderRef interface {
	// OrderNo returns the human-facing order ID (gtk_user_plan.order_id
	// for tokens, gtk_service_order.order_no for services).
	OrderNo() string

	// UserID returns the greentokey auth.id that owns this order.
	UserID() int64

	// ProductType discriminates token vs service so refund/dispatch
	// flows can branch without a second DB read.
	ProductType() ProductType

	// AmountCents is the total amount the user paid in cents
	// (CNY for service, USD-converted-to-cents for LemonSqueezy token).
	AmountCents() int64

	// Status is the per-table raw status string (for audit and admin
	// UI). Callers that want the unified vocabulary read CheckEntitlement
	// instead.
	Status() string

	// CreatedAt is the order creation timestamp.
	CreatedAt() time.Time
}

// PaymentSession is the ephemeral state between "user clicks Buy" and
// "webhook acks payment" (D2 in plan v2). Stored in gtk_payment_session.
//
// Lifecycle:
//
//  1. User triggers checkout → OpenPaymentSession returns a fresh session
//     with a UUID SessionID + Status='pending' + ExpiresAt = now+TTL.
//  2. SessionID is embedded in the LemonSqueezy custom_data /
//     hupijiao prepay payload (CR7 fix: lets ClosePaymentSession match
//     by session_id instead of order_no).
//  3. Webhook arrives → ClosePaymentSession(sessionID) flips
//     Status='paid' and stamps ClosedAt.
//  4. Cron (ExpirePaymentSessions) flips stuck sessions past ExpiresAt
//     to Status='expired'.
//
// IMPORTANT (per Codex M1 in plan v2): gtk_payment_session has FK ONLY to
// auth(id). It does NOT FK to gtk_user_plan or gtk_service_order — it's an
// audit / visibility table, NOT commerce of record. The actual commerce
// state lives in those two tables (D1 unification).
type PaymentSession struct {
	// SessionID is the UUID we embed in the provider's custom_data so
	// the inbound webhook can match back to this session. Unique across
	// all sessions ever (DB constraint).
	SessionID string

	// OrderNo is the human-facing order id (gtk_user_plan.order_id or
	// gtk_service_order.order_no). May be the same as SessionID for
	// token plans; usually distinct for service plans.
	OrderNo string

	// ProductType — token vs service. Determines which entitlement
	// branch ClosePaymentSession+GrantEntitlement takes downstream.
	ProductType ProductType

	// Provider is the payment provider tag: 'lemonsqueezy' | 'hupijiao'
	// | 'manual' (per service.go allowlist).
	Provider string

	// AmountCents is the expected total. Webhook payload must match;
	// mismatch is logged + alerted (not auto-rejected — we trust the
	// provider's authority on the actual collected amount).
	AmountCents int64

	// Status is the session lifecycle: 'pending' | 'paid' | 'expired'
	// | 'failed'. ENUM enforced at DB.
	Status string

	// CoaiUserID is the auth.id paying for this session.
	CoaiUserID int64

	// CreatedAt is when OpenPaymentSession was called.
	CreatedAt time.Time

	// ExpiresAt is when ExpirePaymentSessions will reap this session
	// if Status is still 'pending'. TTL: D3 spec says 30 min for
	// LS auto-redirect, 72h for manual.
	ExpiresAt time.Time

	// ClosedAt is stamped by ClosePaymentSession (paid) or
	// ExpirePaymentSessions (expired). NULL while pending.
	ClosedAt sql.NullTime
}

// UsageCostEntry is the single row written to gtk_app_usage_log per LLM
// upstream call. WriteUsageCost (Wave 2 B2) is the ONLY write path —
// chat handler, api gateway, and service-order runtime all funnel through
// it so cost accounting is consistent across origins.
//
// Source classifies the write origin:
//   - 'chat'          — chat UI / web frontend
//   - 'api'           — direct sk-xxx api-key call
//   - 'service_order' — service-order agent runtime (D1 + CR2 greenfield)
//   - 'admin_test'    — admin-initiated dry runs (PKG-4 admin UI)
//
// Mirrors gtk_app_usage_log columns (PKG-1 added source/order_id/provider).
type UsageCostEntry struct {
	// UserID is the auth.id that incurred the cost.
	UserID int64

	// PlanID links to the gtk_user_plan row that "covered" this call,
	// if any. NULL for pay-as-you-go calls (no active plan) and for
	// service_order calls (those bill through gtk_service_order, not
	// the plan's quota_grant).
	PlanID sql.NullInt64

	// Service is the upstream service slug (e.g. 'openai', 'deepseek',
	// 'anthropic'). Free text; canonicalize via lookup if too many
	// distinct values surface.
	Service string

	// Source is one of 'chat' | 'api' | 'service_order' | 'admin_test'.
	// CHECK constraint on the column (CHECK on SQLite, ENUM on MySQL).
	Source string

	// OrderID is set when Source='service_order' to link the usage row
	// back to gtk_service_order.order_no. NULL otherwise (chat / api
	// callers don't have an order context).
	OrderID sql.NullString

	// Provider is the upstream provider tag (e.g. 'openai',
	// 'anthropic', 'deepseek', 'sub2api'). Pulled from
	// pricing.LookupProvider(model). NULL when model is unknown.
	Provider sql.NullString

	// TokensUsed is the total tokens (input + output) consumed.
	// Granularity is upstream-reported; we don't reconcile.
	TokensUsed int64

	// CostCents is the upstream price in whole cents, computed by
	// pricing.LookupCostCents(model, in, out). Sub-cent rounds DOWN
	// (documented behavior, not bug — see pricing.go). NULL model =
	// 0 cost + warning log.
	CostCents int64

	// TokenID is the NewAPI token ID (gtk_tokens.id) used for this call.
	// 0 means "no token context" (legacy rows, or calls made without an
	// sk-tnx-xxx Authorization header). Set by the middleware-to-context
	// pipeline; WriteUsageCost writes this into gtk_app_usage_log.token_id.
	TokenID int64
}

// EntitlementGrant is the post-payment provisioning request handed to
// GrantEntitlement (Wave 2.5 B3). One struct serves both product types
// because the dispatcher branches on ProductType internally.
//
// For ProductToken: PlanID + QuotaUnits + ExpiresAt are required;
// ServiceID is ignored. GrantEntitlement upserts gtk_user_plan ACTIVE
// and provisions NewAPI (or enqueues gtk_newapi_pending_provisions on
// transient failure per CR8).
//
// For ProductService: ServiceID + OrderNo are required; PlanID +
// QuotaUnits + ExpiresAt are ignored. GrantEntitlement flips
// gtk_service_order.status='paid' (existing service.MarkOrderPaid path,
// just unified call site).
type EntitlementGrant struct {
	// UserID is the recipient auth.id.
	UserID int64

	// ProductType discriminates the dispatch branch.
	ProductType ProductType

	// OrderNo links the grant back to the human-facing order id (and
	// for service products, back to the gtk_service_order row to flip).
	OrderNo string

	// PlanID is the gtk_plan.id for token plans. Ignored for service.
	PlanID int64

	// ServiceID is the gtk_service.id for service products. Ignored
	// for token plans.
	ServiceID int64

	// ExpiresAt is when the entitlement naturally ends. Used for token
	// plans (sets gtk_user_plan.expire_at + NewAPI token expired_time).
	// Ignored for service products (one-shot, no expiry).
	ExpiresAt time.Time

	// QuotaUnits is the NewAPI quota allowance for token plans
	// ($1 ≈ 500_000). Ignored for service products.
	QuotaUnits int64
}

// ModelPrice is the per-model upstream cost in micro-cents per 1k tokens.
// 1 micro-cent = 1/1000 of a cent, i.e. 1/100,000 of a yuan/dollar.
// This precision is needed because some models (e.g. gpt-4o-mini) cost
// fractions of a cent per 1k tokens; rounding to whole cents at storage
// time would lose accuracy across millions of calls.
//
// LookupCostCents (pricing.go) does the multiply + divide-by-1k +
// floor-to-cents conversion; UsageCostEntry.CostCents is the rounded
// integer cents value.
type ModelPrice struct {
	// MicroCentsPer1kInput is the cost per 1k input tokens in
	// micro-cents (1 µ¢ = 1e-5 USD/CNY).
	MicroCentsPer1kInput int64

	// MicroCentsPer1kOutput is the cost per 1k output tokens in
	// micro-cents.
	MicroCentsPer1kOutput int64

	// Provider is the upstream tag (e.g. 'openai', 'anthropic',
	// 'deepseek') — populated into UsageCostEntry.Provider so margin
	// math can group by provider without a second lookup.
	Provider string
}

// Pricing is the in-memory pricing table read by WriteUsageCost. v0:
// hardcoded constants in pricing.go (Q6 Option A stop-gap). Follow-up
// PKG: replace Models with a NewAPI model_ratio reader (Q6 Option B) or
// a hot-reload gtk_model_pricing table (Q6 Option C).
//
// This struct exists so future swaps can inject a different Pricing into
// pricing.go without changing call sites. Wave 1 only ships the
// hardcoded var (no struct field is mutated).
type Pricing struct {
	// Models maps the upstream model name (as it appears in the gateway
	// request, e.g. "gpt-4o", "deepseek-chat") to the cost record.
	Models map[string]ModelPrice
}
