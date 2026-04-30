// Domain types for Layer 3. Mirror the gtk_agent / gtk_service /
// gtk_service_order schemas. Public JSON shape (used by router.go) is
// distinct from the storage struct so we can hide internal fields like
// system_prompt, hupijiao_trade_no etc. from the catalog endpoint.

package service

import "time"

// ─────────────────────────────────────────────────────────────────────
// Storage structs — match table columns 1:1.
// ─────────────────────────────────────────────────────────────────────

// Agent is the registry row for an AI agent definition.
//
// system_prompt is intentionally NOT exposed via the public catalog —
// the agent's behavior is the platform's IP. Customers see only the
// outputs.
type Agent struct {
	ID             int64     `json:"id"`
	Slug           string    `json:"slug"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	SystemPrompt   string    `json:"-"`
	PreferredModel string    `json:"preferred_model"`
	MinTier        string    `json:"min_tier"` // light|standard|premium
	InputsSchema   string    `json:"inputs_schema,omitempty"`
	Status         string    `json:"status"` // active|draft|retired
	Version        int       `json:"version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Service is a purchasable bundle: agent + price + credit allotment.
type Service struct {
	ID              int64     `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	Category        string    `json:"category"` // diy_agent|content_pack|managed_ops
	AgentSlug       string    `json:"agent_slug"`
	PriceCNYCents   int64     `json:"price_cny_cents"`
	IncludedCredits int       `json:"included_credits"`
	BillingType     string    `json:"billing_type"` // one_time|monthly|per_use
	Status          string    `json:"status"`       // active|draft|retired
	LSVariantID     string    `json:"ls_variant_id,omitempty"`
	DisplayOrder    int       `json:"display_order"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ServiceOrder is the audit trail for a single purchase. Most lifecycle
// is driven by the payment-provider webhook (LemonSqueezy or hupijiao);
// the agent_run_id field is set when the runtime kicks off a job.
type ServiceOrder struct {
	ID                int64      `json:"id"`
	OrderNo           string     `json:"order_no"`
	CoaiUserID        int        `json:"coai_user_id"`
	ServiceID         int64      `json:"service_id"`
	ServiceSlug       string     `json:"service_slug"`
	PriceCNYCentsPaid int64      `json:"price_cny_cents_paid"`
	CreditsGranted    int        `json:"credits_granted"`
	PaymentProvider   string     `json:"payment_provider"` // lemonsqueezy|hupijiao|manual
	LSOrderID         string     `json:"ls_order_id,omitempty"`
	HupijiaoTradeNo   string     `json:"hupijiao_trade_no,omitempty"`
	Status            string     `json:"status"` // pending_payment|paid|running|completed|refunded|failed
	PaidAt            *time.Time `json:"paid_at,omitempty"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
	AgentRunID        string     `json:"agent_run_id,omitempty"`
	RefundReason      string     `json:"refund_reason,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// ─────────────────────────────────────────────────────────────────────
// Public DTOs — what /api/gtk/v1/services emits.
// ─────────────────────────────────────────────────────────────────────

// PublicService is the customer-facing view of a service. Strips
// agent_slug (which would let a customer guess at our catalog
// taxonomy), keeps price + name + description.
type PublicService struct {
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	Category        string    `json:"category"`
	PriceCNYCents   int64     `json:"price_cny_cents"`
	PriceDisplayCNY string    `json:"price_display_cny"` // pre-formatted "¥19"
	IncludedCredits int       `json:"included_credits"`
	BillingType     string    `json:"billing_type"`
	DisplayOrder    int       `json:"display_order"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// CategoryLabel returns the Chinese-display label for a category.
// Single source of truth so router.go and any future admin UI agree.
func CategoryLabel(category string) string {
	switch category {
	case "diy_agent":
		return "DIY 智能体"
	case "content_pack":
		return "资料制作包"
	case "managed_ops":
		return "代运营"
	default:
		return category
	}
}

// FormatPriceCNY converts cents to "¥19" / "¥2,800" style display.
// Negative or zero returns "免费". Mirrors the customer-facing copy
// used in the UI.
func FormatPriceCNY(cents int64) string {
	if cents <= 0 {
		return "免费"
	}
	yuan := cents / 100
	// thousands separator
	s := ""
	for yuan > 0 {
		chunk := yuan % 1000
		yuan /= 1000
		if yuan > 0 {
			s = formatPad3(chunk) + s
			s = "," + s
		} else {
			s = formatStrip(chunk) + s
		}
	}
	if s == "" {
		s = "0"
	}
	return "¥" + s
}

func formatPad3(n int64) string {
	out := []byte("000")
	for i := 2; i >= 0 && n > 0; i-- {
		out[i] = byte('0' + n%10)
		n /= 10
	}
	return string(out)
}

func formatStrip(n int64) string {
	if n == 0 {
		return "0"
	}
	out := []byte("")
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}
