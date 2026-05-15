import { createSlice } from "@reduxjs/toolkit";
import { Plans } from "@/admin/types.ts";
import { AppDispatch, RootState } from "@/store/index.ts";
import { getTheme, Theme } from "@/components/ThemeProvider.tsx";
import { getMemory, setMemory } from "@/utils/memory.ts";

// Inlined from conf/storage.ts (excise-1c-sweep — storage.ts deleted)
function getOfflinePlans(): Plans {
  const memory = getMemory("plan_offline");
  if (!memory || !memory.length) return [];
  try {
    const parsed = JSON.parse(memory);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((item) => typeof item === "object");
  } catch {
    return [];
  }
}

function setOfflinePlans(plans: Plans): void {
  setMemory("plan_offline", JSON.stringify(plans));
}

type GlobalState = {
  theme: Theme;
  subscription: Plans;
};

export const globalSlice = createSlice({
  name: "global",
  initialState: {
    theme: getTheme(),
    subscription: getOfflinePlans(),
  } as GlobalState,
  reducers: {
    setSubscription: (state, action) => {
      const plans = action.payload as Plans;
      state.subscription = plans;
      setOfflinePlans(plans);
    },
    setTheme: (state, action) => {
      state.theme = action.payload;
    },
  },
});

export const { setSubscription, setTheme } = globalSlice.actions;

export default globalSlice.reducer;

export const subscriptionDataSelector = (state: RootState): Plans =>
  state.global.subscription;
export const themeSelector = (state: RootState): Theme => state.global.theme;

export const dispatchSubscriptionData = (
  dispatch: AppDispatch,
  subscription: Plans,
) => {
  dispatch(setSubscription(subscription));
};
