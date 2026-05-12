import { afterEach, describe, expect, it, vi } from "vitest";

// Mock @/utils/path.ts BEFORE importing Auth so the module under test
// picks up the mocked getQueryParam. We re-import getQueryParam from the
// mocked module to drive each test case via mockReturnValueOnce.
vi.mock("@/utils/path.ts", () => ({
  getQueryParam: vi.fn<[string], string>(() => ""),
}));

// HI-02 (REVIEW.md 2026-05-13): CI defense for the open-redirect regex in
// Auth.tsx::nextPathFromQuery. If this regex regresses — e.g. someone drops
// the `(?!\/)` lookahead — an attacker could redirect users via
// /login?next=//evil.com post-login. These tests are the canary.
import { nextPathFromQuery, SAFE_NEXT_PATH } from "./Auth";
import { getQueryParam } from "@/utils/path.ts";

const mockedGetQueryParam = vi.mocked(getQueryParam);

afterEach(() => {
  mockedGetQueryParam.mockReset();
  // Restore the default no-op return so unrelated tests in this file
  // don't leak state between cases.
  mockedGetQueryParam.mockReturnValue("");
});

describe("nextPathFromQuery — safe inputs", () => {
  it("returns a simple relative path unchanged", () => {
    mockedGetQueryParam.mockReturnValueOnce("/token-plans");
    expect(nextPathFromQuery()).toBe("/token-plans");
  });

  it("returns a nested relative path unchanged", () => {
    mockedGetQueryParam.mockReturnValueOnce("/admin/orders");
    expect(nextPathFromQuery()).toBe("/admin/orders");
  });

  it("preserves query strings and fragments on safe paths", () => {
    mockedGetQueryParam.mockReturnValueOnce("/token-plans?tier=pro#top");
    expect(nextPathFromQuery()).toBe("/token-plans?tier=pro#top");
  });
});

describe("nextPathFromQuery — empty / missing inputs", () => {
  it("returns '/' for empty string", () => {
    mockedGetQueryParam.mockReturnValueOnce("");
    expect(nextPathFromQuery()).toBe("/");
  });

  it("returns '/' when getQueryParam yields a whitespace-only value", () => {
    mockedGetQueryParam.mockReturnValueOnce("   ");
    expect(nextPathFromQuery()).toBe("/");
  });

  it("returns '/' when the query param is missing (default mock)", () => {
    // No mockReturnValueOnce — falls through to the default "" return.
    expect(nextPathFromQuery()).toBe("/");
  });
});

describe("nextPathFromQuery — open-redirect attack vectors must fall back to '/'", () => {
  it.each([
    ["scheme-relative URL", "//evil.com"],
    ["scheme-relative URL with path", "//evil.com/steal"],
    ["absolute http URL", "http://evil.com/path"],
    ["absolute https URL", "https://evil.com/path"],
    ["javascript: pseudo-scheme", "javascript:alert(1)"],
    ["data: pseudo-scheme", "data:text/html,<script>alert(1)</script>"],
    ["path with whitespace", "/path with space"],
    ["path with tab", "/path\twith-tab"],
    ["path with HTML angle bracket", "/path<script>"],
    ["path with double-quote", '/path"injected'],
    ["bare relative without leading slash", "token-plans"],
    // NOTE: "/\\evil.com" (single slash + backslash) is currently accepted by
    // SAFE_NEXT_PATH because `\` is outside the deny-set. Chromium/Firefox
    // normalise `\` → `/` in path resolution, so this is a latent gap. Track
    // as a follow-up regex hardening; out of scope for HI-02 which only
    // pinned current behaviour. Add the case here when the regex is tightened.
  ])("rejects %s → '/'", (_label, attack) => {
    mockedGetQueryParam.mockReturnValueOnce(attack);
    expect(nextPathFromQuery()).toBe("/");
  });
});

describe("SAFE_NEXT_PATH regex contract", () => {
  // The regex shape itself is part of the security contract — if someone
  // edits it, these tests pin down the invariants we rely on.
  it("requires a leading single slash", () => {
    expect(SAFE_NEXT_PATH.test("/foo")).toBe(true);
    expect(SAFE_NEXT_PATH.test("foo")).toBe(false);
  });

  it("forbids a second leading slash (scheme-relative defense)", () => {
    expect(SAFE_NEXT_PATH.test("//evil.com")).toBe(false);
  });

  it("forbids whitespace and angle brackets and quotes", () => {
    expect(SAFE_NEXT_PATH.test("/ok")).toBe(true);
    expect(SAFE_NEXT_PATH.test("/with space")).toBe(false);
    expect(SAFE_NEXT_PATH.test("/with<tag>")).toBe(false);
    expect(SAFE_NEXT_PATH.test('/with"quote')).toBe(false);
  });
});
