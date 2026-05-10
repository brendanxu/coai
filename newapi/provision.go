// High-level provisioning: given a greentokey user_id and a 套餐 spec,
// ensure the user has a NewAPI account with the right token quota.
//
// Idempotent: safe to call multiple times for the same (user, plan) —
// subsequent calls top-up the existing token instead of creating a new one.
//
// Called from payment/lemonsqueezy.go after CoAI's subscription table is
// updated. The flow:
//
//   webhook  →  upsertSubscription (CoAI subscription level)
//            →  ProvisionForPlan   (NewAPI user + token + quota)
//
// Failure semantics: if ProvisionForPlan fails, the CoAI subscription
// is still active (user paid, level upgraded), but the NewAPI api-key
// is not yet usable. Caller should log + retry. Production must add a
// retry queue (gtk_newapi_pending_provisions) so transient NewAPI
// outages don't leave paid customers without keys — that's a follow-up.

package newapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"chat/globals"
)

// PlanSpec describes the 套餐 from greentokey's perspective. The two
// levers we send to NewAPI are: how much quota to grant, and when it
// expires (if ever).
type PlanSpec struct {
	// Code is the gtk_plan.code (e.g. "starter_99", "indie_19_monthly").
	// Used purely for logging / token name; NewAPI is not aware of it.
	Code string

	// QuotaUnits is the absolute quota allowance in NewAPI's internal
	// unit ($1 ≈ 500_000). Caller must convert from yuan / dollars / tokens
	// to this unit before calling. For "unlimited" plans, set to 0 and
	// Unlimited = true.
	QuotaUnits int64

	// Unlimited bypasses the quota cap. Greentokey shouldn't use this for
	// paid plans (LLM costs scale with usage); reserved for internal
	// dogfooding tokens.
	Unlimited bool

	// ExpiresAt is when the token should stop working. Zero value =
	// never expire (NewAPI sentinel: -1). For monthly plans we typically
	// set this to (now + 32 days) and let the renewal webhook push it
	// forward.
	ExpiresAt time.Time

	// Group is the NewAPI per-user routing group (PKG-2 Wave 1, Q2 / CR8 /
	// architecture §16 §19). Empty → "default" (handled by SaveBinding
	// normalization). Token-plan users typically stay on "default";
	// service-runtime users go to a dedicated group so admin can swap
	// channels per group without touching token plans.
	Group string
}

// ProvisionForPlan ensures the (greentokey user_id) has a NewAPI user +
// active token with the plan's quota. Returns the binding so callers can
// hand the token key back to the user.
//
// Algorithm:
//   1. LoadBinding — does the user already have a NewAPI account?
//      a) Yes → top up token quota + extend expiry (UpdateUserQuota +
//                CreateToken if existing token is consumed/expired).
//      b) No  → CreateUser, CreateToken, persist binding.
//
// At step 1.a we currently ALWAYS issue a new token rather than mutating
// the existing one — NewAPI's PUT /api/token endpoint exists but mutating
// remain_quota in-place silently changes a key the user is already using
// (bad if they're mid-stream). Issuing a fresh token and rotating is a
// follow-up; v0 just creates a new token and lets the old one drain.
//
// TODO(v2): rotate-vs-topup policy + retry queue + token name with
// expires-at suffix for easier admin debugging.
func ProvisionForPlan(ctx context.Context, db *sql.DB, coaiUserID int64, spec PlanSpec) (*Binding, error) {
	cli, err := Default()
	if err != nil {
		// Configuration gate: log and bail. Caller treats as warning.
		return nil, err
	}

	// Step 1: do we have a binding already?
	bind, err := LoadBinding(db, coaiUserID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load gtk_newapi_binding: %w", err)
	}

	// Step 2: ensure NewAPI user exists.
	var newapiUser *User
	if bind != nil {
		// Trust the binding — don't re-fetch unless we hit a hard error.
		newapiUser = &User{ID: bind.NewapiUserID}
	} else {
		// First-time provision. Use a stable username derived from the
		// greentokey user id so the search-then-create dance is
		// deterministic across retries.
		username := fmt.Sprintf("gtk-%d", coaiUserID)

		existing, errSearch := cli.SearchUserByUsername(ctx, username)
		if errSearch != nil && !errors.Is(errSearch, ErrUserNotFound) {
			return nil, fmt.Errorf("search newapi user: %w", errSearch)
		}
		if existing != nil {
			// Recovered from a partial-state (binding lost but user existed).
			newapiUser = existing
			globals.Warn(fmt.Sprintf(
				"newapi: recovered existing user %d for coai_user %d (binding was missing)",
				existing.ID, coaiUserID,
			))
		} else {
			// PKG-2 Wave 1 (Q2 / CR8): plumb spec.Group through to NewAPI so
			// admin can route token-plan vs service-runtime traffic to
			// different upstream channels via NewAPI's group config. Empty
			// spec.Group → NewAPI defaults to "default" (omitempty).
			created, errCreate := cli.CreateUser(ctx, CreateUserRequest{
				Username:    username,
				DisplayName: fmt.Sprintf("greentokey user %d", coaiUserID),
				Group:       spec.Group,
			})
			if errCreate != nil {
				return nil, fmt.Errorf("create newapi user: %w", errCreate)
			}
			newapiUser = created
		}
	}

	// Step 3: set quota on the user. NewAPI's user-level quota is the
	// ceiling; per-token quota is sub-cap. We set them both for clarity.
	if !spec.Unlimited {
		if err := cli.UpdateUserQuota(ctx, newapiUser.ID, spec.QuotaUnits); err != nil {
			return nil, fmt.Errorf("update newapi user quota: %w", err)
		}
	}

	// Step 4: issue a new token for this provisioning event.
	tokenName := fmt.Sprintf("gtk-%s-%s", spec.Code, time.Now().Format("20060102"))
	expiredTime := int64(-1)
	if !spec.ExpiresAt.IsZero() {
		expiredTime = spec.ExpiresAt.Unix()
	}
	tok, err := cli.CreateToken(ctx, newapiUser.ID, CreateTokenRequest{
		Name:           tokenName,
		RemainQuota:    spec.QuotaUnits,
		ExpiredTime:    expiredTime,
		UnlimitedQuota: spec.Unlimited,
	})
	if err != nil {
		return nil, fmt.Errorf("create newapi token: %w", err)
	}

	// Step 5: persist the binding.
	// PKG-2 Wave 1 (Q2 / CR8): write spec.Group into the binding so future
	// reads (admin UI, dashboards, channel-routing decisions) match the
	// NewAPI side. SaveBinding normalizes empty Group → "default".
	out := &Binding{
		CoaiUserID:     coaiUserID,
		NewapiUserID:   newapiUser.ID,
		NewapiTokenID:  tok.ID,
		NewapiTokenKey: tok.Key,
		Group:          spec.Group,
		LastKnownQuota: spec.QuotaUnits,
	}
	if err := SaveBinding(db, out); err != nil {
		return nil, fmt.Errorf("save gtk_newapi_binding: %w", err)
	}
	return out, nil
}
