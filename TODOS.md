# TODOS — greentokey v0.6.1 Product window

Captured during /plan-eng-review on 2026-04-28 (branch: feat/v0.6.1-product).
Scope: deferred work from the service-dashboard + chat-repositioning PR.

## Backend

### chat-count endpoint for HomeDashboard
**What:** Add CoAI backend endpoint `GET /v1/usage/summary` returning `{chats_this_month: int, by_model: [...]}`. Wire HomeDashboard's Chat Playground card to display "本月已聊 N 条" instead of placeholder em-dash.

**Why:** Real usage number = real perceived value. Without it, the dashboard feels half-built. Currently the only number on the dashboard is carbon (which has its own endpoint).

**Pros:** Closes a visible gap in v0.6.1 dashboard polish. Pattern reuses existing `getCarbonSummary` selector approach.

**Cons:** Requires Go-side endpoint + DB aggregation query. Hot-path query — must be cheap (or pre-aggregated).

**Context:** `routes/Dashboard.tsx` shows the pattern: fetch on mount → set redux → component reads selector. The new endpoint should follow the same shape.

**Depends on:** Nothing.

**Effort:** ~2h human / ~30 min CC for endpoint + ~10 min wiring on FE.

## Frontend testing

### @testing-library/react + jsdom setup
**What:** Add @testing-library/react + @testing-library/user-event + jsdom env to vitest.config.ts. Write smoke tests for ChatPanel lazy-mount, HomeDashboard render, ChatFloatingButton useLocation guard, and the ChatWrapper-double-mount-doesn't-happen regression.

**Why:** The ChatWrapper double-mount regression risk introduced in v0.6.1 is exactly the kind of bug RTL would catch on the next refactor. Currently 0 component tests in the project; only 2 pure-function tests (`carbon.test.ts`, `tier.test.ts`).

**Pros:** Catches regressions on every PR. Infra is reusable forever.

**Cons:** ~3h infrastructure setup + per-component test cost. May surface other untested code paths during setup that look like bugs.

**Context:** `app/vitest.config.ts` is the entry point. Pattern from `app/src/components/Carbon/__tests__/tier.test.ts` for unit tests; need to extend with `environment: 'jsdom'` + setup file for RTL.

**Depends on:** Nothing.

**Effort:** 2-3h human / ~30 min CC for setup + ~15 min per component smoke test.

## UX polish

### Persist floating-panel draft input
**What:** zustand-persist the draft input text in ChatPanel (NOT the open/close state). When user types something in the floating panel, navigates away, and reopens — the text is still there.

**Why:** Mild UX win. Most chat widgets (Intercom, Drift) DON'T do this, so the locked decision was "don't persist". But if user feedback shows they keep losing drafts, revisit.

**Pros:** Recovery from accidental panel close.

**Cons:** Small win. May surprise users who expect "close = clear".

**Context:** `components/ChatFloating/store.ts` is the natural place. Add `draftInput: string; setDraftInput(text: string)` with persist middleware. ChatWrapper already has local input state — would need a controlled-input refactor.

**Depends on:** Nothing.

**Effort:** ~30 min human / ~5 min CC.
