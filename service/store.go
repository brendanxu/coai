// Storage layer — thin wrappers around `database/sql` for Layer 3
// service catalog reads + order writes. Kept separate from router.go so
// tests can swap in *sql.DB backed by SQLite without HTTP plumbing.
//
// Convention matches newapi/migration.go + plans/store.go: queries are
// raw SQL (no ORM), errors wrap with %w so callers can `errors.Is`
// against sql.ErrNoRows.

package service

import (
	"chat/globals"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
)

// ErrServiceNotFound is returned by LoadServiceBySlug when the slug
// doesn't match an active row. Distinct from generic sql.ErrNoRows so
// the router can return a clean 404 message.
var ErrServiceNotFound = errors.New("service: not found or not active")

// LoadActiveServices returns all gtk_service rows with status='active',
// ordered by display_order ASC then id ASC for stable output.
//
// The PublicService DTO is what the catalog endpoint emits. Internal
// fields (agent_slug, ls_variant_id) are dropped here.
func LoadActiveServices(db *sql.DB) ([]PublicService, error) {
	rows, err := globals.QueryDb(db, `
		SELECT slug, name, COALESCE(description, ''), category,
		       price_cny_cents, included_credits, billing_type,
		       display_order, updated_at
		FROM gtk_service
		WHERE status = 'active'
		ORDER BY display_order ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("service: load active services: %w", err)
	}
	defer rows.Close()

	var out []PublicService
	for rows.Next() {
		var s PublicService
		if err := rows.Scan(
			&s.Slug, &s.Name, &s.Description, &s.Category,
			&s.PriceCNYCents, &s.IncludedCredits, &s.BillingType,
			&s.DisplayOrder, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("service: scan service row: %w", err)
		}
		s.PriceDisplayCNY = FormatPriceCNY(s.PriceCNYCents)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("service: iterate services: %w", err)
	}
	return out, nil
}

// LoadServiceBySlug returns the full Service row by slug. Returns
// ErrServiceNotFound when the row is missing or inactive — callers
// should treat both cases as "customer can't buy this".
func LoadServiceBySlug(db *sql.DB, slug string) (*Service, error) {
	row := globals.QueryRowDb(db, `
		SELECT id, slug, name, COALESCE(description, ''), category,
		       agent_slug, price_cny_cents, included_credits, billing_type,
		       status, COALESCE(ls_variant_id, ''), display_order,
		       created_at, updated_at
		FROM gtk_service
		WHERE slug = ?
	`, slug)

	var s Service
	if err := row.Scan(
		&s.ID, &s.Slug, &s.Name, &s.Description, &s.Category,
		&s.AgentSlug, &s.PriceCNYCents, &s.IncludedCredits, &s.BillingType,
		&s.Status, &s.LSVariantID, &s.DisplayOrder,
		&s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrServiceNotFound
		}
		return nil, fmt.Errorf("service: load by slug: %w", err)
	}
	if s.Status != "active" {
		return nil, ErrServiceNotFound
	}
	return &s, nil
}

// NewOrderNo generates a human-friendly order id like SVC-AB12CD34.
// 8 hex chars from crypto/rand. Collision risk is ~1 in 4.3B per
// generation, plus the table has UNIQUE (order_no), so accidental
// duplicates surface at INSERT time as a constraint error.
func NewOrderNo() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is system-level — return a sentinel
		// the INSERT will reject so we never silently create dupes.
		return "SVC-RANDFAIL"
	}
	return "SVC-" + hex.EncodeToString(b[:])
}

// CreateOrder writes a gtk_service_order row in status='pending_payment'.
// Returns the new order's order_no on success. Caller is responsible for
// kicking off the payment flow (LemonSqueezy checkout URL or hupijiao
// QR generation) and updating status='paid' on webhook receipt.
//
// We snapshot service_slug + price + included_credits at order time so
// retroactive catalog edits don't change historical orders. This is the
// same denormalization pattern as gtk_app_usage_log.
func CreateOrder(db *sql.DB, coaiUserID int64, svc *Service, paymentProvider string) (string, error) {
	orderNo, _, err := CreateOrderWithToken(db, coaiUserID, svc, paymentProvider)
	return orderNo, err
}

// CreateOrderWithToken is the v0.15 variant: also returns the per-order
// access_token so the caller (router.go) can bake it into the
// /services/run/<order_no>?token=... URL it shares with the customer
// in WeChat. The token is generated server-side and stored on the row;
// it never expires (the URL is the secret).
func CreateOrderWithToken(db *sql.DB, coaiUserID int64, svc *Service, paymentProvider string) (string, string, error) {
	if svc == nil {
		return "", "", errors.New("service: CreateOrder requires non-nil service")
	}
	switch paymentProvider {
	case "lemonsqueezy", "hupijiao", "manual":
	default:
		return "", "", fmt.Errorf("service: unknown payment_provider %q", paymentProvider)
	}

	orderNo := NewOrderNo()
	accessToken, err := newAccessToken()
	if err != nil {
		return "", "", fmt.Errorf("service: generate access_token: %w", err)
	}
	if _, err := globals.ExecDb(db, `
		INSERT INTO gtk_service_order (
		  order_no, coai_user_id, service_id, service_slug,
		  price_cny_cents_paid, credits_granted, payment_provider,
		  access_token, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending_payment')
	`, orderNo, coaiUserID, svc.ID, svc.Slug,
		svc.PriceCNYCents, svc.IncludedCredits, paymentProvider,
		accessToken); err != nil {
		return "", "", fmt.Errorf("service: insert order: %w", err)
	}
	return orderNo, accessToken, nil
}

// newAccessToken returns a 32-char hex token (16 random bytes). Long
// enough to brute-force-resist (2^128 keyspace) without bloating the
// shareable URL beyond what fits in a WeChat message.
func newAccessToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
