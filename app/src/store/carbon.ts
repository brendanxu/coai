import { createSlice, PayloadAction } from "@reduxjs/toolkit";
import type { RootState } from "./index.ts";
import type { CarbonSummary, CarbonFactorsTable, FactorEntry } from "@/api/carbon.ts";

// State shape:
// - ecoMode: persisted server-side; mirrored here for sync UI updates
// - ecoOverrideUntil: per-chat override timestamp (ms epoch). When set in
//   the future, the next chat sends X-Eco-Override: off. Long-press toggle
//   sets this to now+60s. Self-clears via TTL check at read time.
// - ecoFirstFeedbackSeen: stops the "Eco saved X%" pill from showing forever
//   (after ~5 displays the FE marks it seen)
// - ecoFeedbackShownCount: counts ON-state chats since component mounted
// - summary: cached result of GET /api/carbon/summary?month=current
// - factors: cached result of GET /api/carbon/factors (loaded once on app boot)
// - loading flags

type CarbonState = {
  ecoMode: boolean;
  ecoOverrideUntil: number; // 0 if no override
  ecoFirstFeedbackSeen: boolean;
  ecoFeedbackShownCount: number;
  summary: CarbonSummary | null;
  summaryLoading: boolean;
  factors: CarbonFactorsTable | null;
  factorsLoading: boolean;
};

const initialState: CarbonState = {
  ecoMode: false,
  ecoOverrideUntil: 0,
  ecoFirstFeedbackSeen: false,
  ecoFeedbackShownCount: 0,
  summary: null,
  summaryLoading: false,
  factors: null,
  factorsLoading: false,
};

export const carbonSlice = createSlice({
  name: "carbon",
  initialState,
  reducers: {
    setEcoMode: (state, action: PayloadAction<boolean>) => {
      state.ecoMode = action.payload;
    },
    setEcoOverride: (state, action: PayloadAction<{ ms: number }>) => {
      state.ecoOverrideUntil = Date.now() + action.payload.ms;
    },
    clearEcoOverride: (state) => {
      state.ecoOverrideUntil = 0;
    },
    setFirstFeedbackSeen: (state, action: PayloadAction<boolean>) => {
      state.ecoFirstFeedbackSeen = action.payload;
    },
    incrementFeedbackShown: (state) => {
      state.ecoFeedbackShownCount += 1;
    },
    setSummary: (state, action: PayloadAction<CarbonSummary | null>) => {
      state.summary = action.payload;
      state.summaryLoading = false;
    },
    setSummaryLoading: (state, action: PayloadAction<boolean>) => {
      state.summaryLoading = action.payload;
    },
    setFactors: (state, action: PayloadAction<CarbonFactorsTable | null>) => {
      state.factors = action.payload;
      state.factorsLoading = false;
    },
    setFactorsLoading: (state, action: PayloadAction<boolean>) => {
      state.factorsLoading = action.payload;
    },
  },
});

export const {
  setEcoMode,
  setEcoOverride,
  clearEcoOverride,
  setFirstFeedbackSeen,
  incrementFeedbackShown,
  setSummary,
  setSummaryLoading,
  setFactors,
  setFactorsLoading,
} = carbonSlice.actions;

// ----- Selectors ----------------------------------------------------------

export const selectEcoMode = (s: RootState) => s.carbon.ecoMode;

// ecoOverrideActive checks the TTL — anything in the future means override is on.
export const selectEcoOverrideActive = (s: RootState) =>
  s.carbon.ecoOverrideUntil > Date.now();

export const selectEcoFirstFeedbackSeen = (s: RootState) =>
  s.carbon.ecoFirstFeedbackSeen;

export const selectShouldShowEcoFeedback = (s: RootState) =>
  s.carbon.ecoMode && !s.carbon.ecoFirstFeedbackSeen && s.carbon.ecoFeedbackShownCount < 5;

export const selectCarbonSummary = (s: RootState) => s.carbon.summary;
export const selectCarbonSummaryLoading = (s: RootState) => s.carbon.summaryLoading;
export const selectCarbonFactors = (s: RootState) => s.carbon.factors;

// Convenience: factor list for use with estimateCO2()
export const selectFactorList = (s: RootState): FactorEntry[] =>
  s.carbon.factors?.factors ?? [];

export default carbonSlice.reducer;
