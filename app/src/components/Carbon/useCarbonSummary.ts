// v0.6.1 — shared carbon summary hook.
// Consumers: MonthlyWidget (sidebar tile), MonthlyCarbonSummary (dashboard
// footer), routes/Dashboard.tsx (carbon report). Centralises the fetch +
// cancellation pattern so each consumer doesn't refetch on every nav.
//
// Refetch policy: fetch once per session unless ecoMode toggles (good signal
// that usage pattern just changed). Subsequent mounts read cached redux state.

import { useEffect } from "react";
import { useDispatch, useSelector } from "react-redux";
import {
  selectCarbonSummary,
  selectCarbonSummaryLoading,
  selectEcoMode,
  setSummary,
  setSummaryLoading,
} from "@/store/carbon.ts";
import { getCarbonSummary } from "@/api/carbon.ts";
import type { AppDispatch } from "@/store/index.ts";
import type { CarbonSummary } from "@/api/carbon.ts";

export type UseCarbonSummary = {
  summary: CarbonSummary | null;
  loading: boolean;
};

export function useCarbonSummary(): UseCarbonSummary {
  const dispatch = useDispatch<AppDispatch>();
  const summary = useSelector(selectCarbonSummary);
  const loading = useSelector(selectCarbonSummaryLoading);
  const ecoMode = useSelector(selectEcoMode);

  useEffect(() => {
    // Skip refetch if we already have data and aren't loading. ecoMode in
    // the dep array means a toggle still triggers a fresh pull (intentional).
    if (summary && !loading) return;

    let cancelled = false;
    dispatch(setSummaryLoading(true));
    getCarbonSummary()
      .then((s) => {
        if (!cancelled) dispatch(setSummary(s));
      })
      .catch(() => {
        if (!cancelled) dispatch(setSummaryLoading(false));
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dispatch, ecoMode]);

  return { summary, loading };
}
