import { ThemeProvider } from "@/components/ThemeProvider.tsx";
import DialogManager from "@/dialogs";
import { useEffectAsync } from "@/utils/hook.ts";
import { bindMarket, getApiPlans } from "@/api/v1.ts";
import { useDispatch } from "react-redux";
import {
  stack,
  updateMasks,
  updateSupportModels,
  useMessageActions,
} from "@/store/chat.ts";
import { dispatchSubscriptionData, setTheme } from "@/store/globals.ts";
import { infoEvent } from "@/events/info.ts";
import { setForm } from "@/store/info.ts";
import { themeEvent } from "@/events/theme.ts";
import { setFactors, setFactorsLoading } from "@/store/carbon.ts";
import { getCarbonFactors } from "@/api/carbon.ts";
import { useEffect } from "react";

function AppProvider({ children }: { children?: React.ReactNode }) {
  const dispatch = useDispatch();
  const { receive } = useMessageActions();

  useEffect(() => {
    infoEvent.bind((data) => dispatch(setForm(data)));
    themeEvent.bind((theme) => dispatch(setTheme(theme)));

    stack.setCallback(async (id, message) => {
      await receive(id, message);
    });
  }, []);

  useEffectAsync(async () => {
    updateSupportModels(dispatch, await bindMarket());
    dispatchSubscriptionData(dispatch, await getApiPlans());
    await updateMasks(dispatch);
    // greentokey v0.6 — fetch carbon factors once on app boot so CarbonBadge
    // on chat messages can render exact estimates immediately. Without this
    // the factors only loaded when /methodology was visited, leaving every
    // chat badge stuck on the "?g" coefficient-gap fallback.
    dispatch(setFactorsLoading(true));
    try {
      const factors = await getCarbonFactors();
      dispatch(setFactors(factors));
    } catch {
      dispatch(setFactorsLoading(false));
    }
  }, []);

  return (
    <ThemeProvider>
      <DialogManager />
      {children}
    </ThemeProvider>
  );
}

export default AppProvider;
