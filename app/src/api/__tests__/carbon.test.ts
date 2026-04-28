import { describe, expect, it } from "vitest";
import { estimateCO2, type FactorEntry } from "../carbon";

// Mirrors a slice of the backend's data/carbon_factors.json. If the embedded
// JSON's coefficients change, the FE/BE invariant unit test
// TestEstimateCO2_HappyPath in coai/carbon/carbon_test.go tests the Go side
// using the SAME numbers — when these tests both pass, badge-on-message
// equals dashboard-aggregate equals what the database stores.
const FACTORS: FactorEntry[] = [
  {
    model: "gpt-4o",
    region: "default",
    gco2e_per_1k_tokens: 0.45,
    valid_from: "2026-04-01",
    version: "2026-04",
  },
  {
    model: "gpt-4o-mini",
    region: "default",
    gco2e_per_1k_tokens: 0.07,
    valid_from: "2026-04-01",
    version: "2026-04",
  },
];

describe("estimateCO2", () => {
  it("1000 tokens × coefficient = exactly the per-1k value", () => {
    const r = estimateCO2(1000, "gpt-4o-mini", FACTORS);
    expect(r.coefficientFound).toBe(true);
    expect(r.co2g).toBeCloseTo(0.07, 6);
    expect(r.version).toBe("2026-04");
  });

  it("500 tokens = half the per-1k coefficient", () => {
    const r = estimateCO2(500, "gpt-4o-mini", FACTORS);
    expect(r.co2g).toBeCloseTo(0.035, 6);
  });

  it("zero tokens = 0g", () => {
    const r = estimateCO2(0, "gpt-4o", FACTORS);
    expect(r.coefficientFound).toBe(true);
    expect(r.co2g).toBe(0);
  });

  it("unknown model → coefficientFound=false (badge will render '~?g')", () => {
    const r = estimateCO2(1000, "totally-made-up-model", FACTORS);
    expect(r.coefficientFound).toBe(false);
    expect(r.co2g).toBe(0);
    expect(r.version).toBe("");
  });

  it("case-insensitive model lookup", () => {
    const r = estimateCO2(1000, "GPT-4o", FACTORS);
    expect(r.coefficientFound).toBe(true);
    expect(r.co2g).toBeCloseTo(0.45, 6);
  });

  it("unknown region falls back to any same-model entry", () => {
    const r = estimateCO2(1000, "gpt-4o", FACTORS, "made-up-region");
    expect(r.coefficientFound).toBe(true);
    expect(r.co2g).toBeCloseTo(0.45, 6);
  });
});
