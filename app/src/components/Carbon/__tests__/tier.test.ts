import { describe, expect, it } from "vitest";
import { tierFor, tierClasses, formatCO2 } from "../tier";

describe("tierFor", () => {
  // Boundaries must mirror backend carbon.TierFor in coai/carbon/carbon.go.
  // If you change one, update the other and the corresponding test.
  it.each([
    [0, "low"],
    [0.499, "low"],
    [0.5, "mid"],
    [1.0, "mid"],
    [1.999, "mid"],
    [2.0, "high"],
    [100, "high"],
  ])("%f → %s", (g, want) => {
    expect(tierFor(g)).toBe(want);
  });
});

describe("tierClasses", () => {
  it("low uses fern accent + forest text — never red", () => {
    const c = tierClasses("low");
    expect(c.bg).toContain("--accent");
    expect(c.text).toContain("--accent-foreground");
    expect(c.bg).not.toContain("destructive");
    expect(c.text).not.toContain("destructive");
  });

  it("mid uses sage secondary — never red", () => {
    const c = tierClasses("mid");
    expect(c.bg).toContain("--secondary");
    expect(c.bg).not.toContain("destructive");
  });

  it("high uses sun/gold (NOT red — anti-guilt-trip rule from D7)", () => {
    const c = tierClasses("high");
    expect(c.bg).toContain("--gold");
    expect(c.text).toContain("--gold-foreground");
    // Critical anti-greenwashing rule: high tier MUST NOT signal danger
    // with red. Guilt-trip → user churn.
    expect(c.bg).not.toContain("destructive");
    expect(c.bg).not.toContain("--failure");
    expect(c.text).not.toContain("destructive");
  });

  it("each tier returns a non-empty label", () => {
    expect(tierClasses("low").label).toBe("low");
    expect(tierClasses("mid").label).toBe("mid");
    expect(tierClasses("high").label).toBe("high");
  });
});

describe("formatCO2", () => {
  it.each([
    [0, "0g"],
    [-1, "0g"], // negatives clamped to 0
    [0.05, "<0.1g"],
    [0.42, "0.42g"],
    [0.999, "1.00g"], // <1g branch uses toFixed(2)
    [12.345, "12.3g"],
    [100, "100g"],
    [567.8, "568g"], // ≥100 rounds to integer
    [1000, "1.00kg"], // ≥1000g shows kg
    [12345, "12.35kg"],
  ])("%f → %s", (g, want) => {
    expect(formatCO2(g)).toBe(want);
  });
});
