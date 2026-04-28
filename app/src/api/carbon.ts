import axios from "axios";

// ----- Types -------------------------------------------------------------

export type ByModelEntry = {
  model: string;
  g: number;
  pct: number;
  tokens: number;
};

export type CarbonSummary = {
  month: string;
  total_g: number;
  last_month_g: number;
  delta_pct: number;
  by_model: ByModelEntry[];
  coefficient_version: string;
  error_margin_pct: number;
  first_month: boolean;
};

export type FactorEntry = {
  model: string;
  region: string;
  gco2e_per_1k_tokens: number;
  valid_from: string;
  version: string;
  notes?: string;
};

export type CarbonFactorsTable = {
  version: string;
  error_margin_pct: number;
  default_region: string;
  sources: { name: string; url: string; note?: string }[];
  factors: FactorEntry[];
  what_we_dont_measure: string[];
  calibration_note: string;
};

export type CarbonByModel = {
  from: string;
  to: string;
  total_g: number;
  tokens: number;
  by_model: ByModelEntry[];
};

export type CarbonPrefs = {
  user_id: number;
  eco_mode: boolean;
  eco_mode_first_feedback_seen: boolean;
};

// ----- API calls ---------------------------------------------------------

export async function getCarbonSummary(month?: string): Promise<CarbonSummary> {
  const q = month ? `?month=${encodeURIComponent(month)}` : "";
  const resp = await axios.get(`/carbon/summary${q}`);
  return resp.data as CarbonSummary;
}

export async function getCarbonByModel(from?: string, to?: string): Promise<CarbonByModel> {
  const params = new URLSearchParams();
  if (from) params.set("from", from);
  if (to) params.set("to", to);
  const q = params.toString() ? `?${params}` : "";
  const resp = await axios.get(`/carbon/by-model${q}`);
  return resp.data as CarbonByModel;
}

export async function getCarbonFactors(): Promise<CarbonFactorsTable> {
  const resp = await axios.get(`/carbon/factors`);
  return resp.data as CarbonFactorsTable;
}

export async function getEcoMode(): Promise<CarbonPrefs> {
  const resp = await axios.get(`/user/eco-mode`);
  return resp.data as CarbonPrefs;
}

export async function setEcoMode(enabled: boolean): Promise<{ eco_mode: boolean; updated_at: string }> {
  const resp = await axios.put(`/user/eco-mode`, { enabled });
  return resp.data;
}

export async function markEcoFeedbackSeen(): Promise<void> {
  await axios.post(`/user/eco-mode/feedback-seen`);
}

// ----- Pure helpers ------------------------------------------------------

// estimateCO2 mirrors the backend EstimateCO2 calculation so the badge can
// render instantly from the response's usage.total_tokens without a roundtrip.
// Coefficients come from getCarbonFactors() and are cached in the redux store.
export function estimateCO2(
  tokens: number,
  model: string,
  factors: FactorEntry[],
  defaultRegion = "default",
): { co2g: number; coefficientFound: boolean; version: string } {
  const m = model.toLowerCase();
  // Try exact (model, defaultRegion) then any (model, *)
  const exact = factors.find(
    (f) => f.model.toLowerCase() === m && f.region.toLowerCase() === defaultRegion.toLowerCase(),
  );
  const fallback = exact ?? factors.find((f) => f.model.toLowerCase() === m);
  if (!fallback) {
    return { co2g: 0, coefficientFound: false, version: "" };
  }
  return {
    co2g: (tokens / 1000) * fallback.gco2e_per_1k_tokens,
    coefficientFound: true,
    version: fallback.version,
  };
}
