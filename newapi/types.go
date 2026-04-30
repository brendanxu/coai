// Package newapi is greentokey's bridge to the NewAPI v0.13.x admin REST API.
//
// Why this exists: NewAPI is the Layer 1+2 token-aggregation engine in the
// 3-layer product architecture (Layer 3 services / Layer 2 token bundle /
// Layer 1 channel routing). When a greentokey user purchases a 套餐, we
// need to:
//
//   1. Ensure the user has a NewAPI user account (1:1 with greentokey
//      auth.User), idempotent — create if not exists.
//   2. Issue (or top-up) a NewAPI api-key (token) for that user with the
//      套餐's quota allowance.
//   3. Persist the (greentokey_user_id ↔ newapi_user_id, newapi_token_id)
//      binding in gtk_newapi_binding so subsequent purchases / renewals
//      can find the user without re-creating.
//
// 三条隔离原则 (PROJECT_BRIEF.md) reaffirmed:
//   - We talk to NewAPI ONLY through the admin REST API + OpenAI-compat
//     relay endpoint. We do NOT read NewAPI's internal MySQL tables.
//   - NewAPI stays internal (caddy doesn't proxy /api/* to it). All
//     greentokey ↔ NewAPI traffic is on the docker bridge network.
//   - 套餐 pricing semantics live in greentokey (gtk_plan); NewAPI only
//     knows quota numbers.
//
// NewAPI auth model (v0.13.x):
//   - Each NewAPI user has a unique 32-char `access_token` for admin REST.
//   - For end-user API calls (sk-xxx tokens) the auth flow is different:
//     header `Authorization: Bearer sk-xxx` is the api-key from `tokens`
//     table. That's NOT what we use here — we use access_token from the
//     admin user (greentokey-system role) to call the admin endpoints.
//   - Some endpoints accept `New-Api-User: <id>` to impersonate (we use
//     this to create tokens on behalf of a specific user_id).

package newapi

import "time"

// User is the subset of NewAPI's user record that greentokey cares about.
// Full schema has 30+ fields (oauth bindings, 2FA, affiliate, etc.) — we
// don't surface those.
type User struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Role        int       `json:"role"`        // 1=user, 10=admin, 100=super-admin
	Status      int       `json:"status"`      // 1=active, 2=disabled
	Quota       int64     `json:"quota"`       // remaining quota in NewAPI's internal unit ($1 ≈ 500_000)
	UsedQuota   int64     `json:"used_quota"`
	Group       string    `json:"group"`       // pricing group, default "default"
	CreatedAt   time.Time `json:"-"`
}

// Token is a NewAPI api-key (sk-xxx) belonging to a User. End users call
// our gateway with this in `Authorization: Bearer <Key>`.
type Token struct {
	ID                  int64     `json:"id"`
	UserID              int64     `json:"user_id"`
	Name                string    `json:"name"`
	Key                 string    `json:"key"`                  // sk-xxx (returned only on create / list)
	Status              int       `json:"status"`               // 1=enabled, 2=disabled
	RemainQuota         int64     `json:"remain_quota"`
	UnlimitedQuota      bool      `json:"unlimited_quota"`
	ExpiredTime         int64     `json:"expired_time"`         // unix seconds, -1 = never
	ModelLimitsEnabled  bool      `json:"model_limits_enabled"`
	ModelLimits         string    `json:"model_limits"`         // comma-separated model names
	CreatedAt           time.Time `json:"-"`
}

// CreateUserRequest mirrors POST /api/user (admin scope).
// Required: Username (unique). Password is auto-generated if empty.
type CreateUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// CreateTokenRequest mirrors POST /api/token. The acting user is set
// via the `New-Api-User: <id>` header — the admin client sets this.
type CreateTokenRequest struct {
	Name           string `json:"name"`
	RemainQuota    int64  `json:"remain_quota"`
	ExpiredTime    int64  `json:"expired_time"`            // -1 = never expire
	UnlimitedQuota bool   `json:"unlimited_quota,omitempty"`
}

// UpdateUserQuotaRequest mirrors PUT /api/user/admin/{id}.
// We send only the field we want changed; NewAPI honors partial updates
// for these fields specifically.
type UpdateUserQuotaRequest struct {
	ID    int64 `json:"id"`
	Quota int64 `json:"quota"`
}

// envelope is NewAPI's standard response wrapper.
type envelope[T any] struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    T      `json:"data"`
}

// listEnvelope is used for paginated endpoints (e.g. /api/user/?p=0).
type listEnvelope[T any] struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    struct {
		Items     []T   `json:"items"`
		Total     int64 `json:"total"`
		Page      int64 `json:"page"`
		PageSize  int64 `json:"page_size"`
	} `json:"data"`
}
