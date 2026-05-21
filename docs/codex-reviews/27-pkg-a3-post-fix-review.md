The user-side token flow is mostly intact, but admin token search/force-revoke and audit attribution/details have functional issues. These break stated admin acceptance criteria and should be fixed before considering the patch correct.

Full review comments:

- [P2] Search tokens by coai_user_id — /Users/brendanxu/tanaxu/greentokey/coai-v0.22-token-checkout/app/src/routes/admin/TokensTab.tsx:235-235
  The admin acceptance flow searches by the visible CoAI user ID and the revoke endpoint requires `coai_user_id`, but this sends `?user_id=`, which uses the backend's legacy NewAPI-user path and returns rows with `coai_user_id` left as 0. After a filtered search, entering a CoAI ID can return the wrong/empty set, and entering a NewAPI ID makes force-revoke call `coai_user_id=0` and fail the binding lookup; use the backend's `coai_user_id` filter or populate the reverse binding.

- [P2] Preserve the real admin actor in audit rows — /Users/brendanxu/tanaxu/greentokey/coai-v0.22-token-checkout/newapi/admin_tokens.go:954-954
  When this path force-revokes a token, `coaiUserID` is the target customer from the query string, not the authenticated admin. `RevokeToken` later writes that same ID as `actor_id` with `actor_type='admin'`, so admin revokes are audited as if the customer performed them and the actual admin identity is lost; pass the admin user's ID separately for audit while keeping the target ID for ownership checks.

- [P3] Render the audit fields returned by the API — /Users/brendanxu/tanaxu/greentokey/coai-v0.22-token-checkout/app/src/routes/admin/TokensTab.tsx:79-79
  The audit endpoint returns `before_state`, `after_state`, `note`, and `actor_type`, but the UI model expects a `detail` field that is never present. For any audit entry with rename/quota/revoke context, the drawer only shows action/time/actor ID and silently drops the state details; update the client to consume the actual response fields or have the API include `detail`.
