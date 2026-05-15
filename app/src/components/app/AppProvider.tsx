import { ThemeProvider } from "@/components/ThemeProvider.tsx";
import DialogManager from "@/dialogs";
import { useEffectAsync } from "@/utils/hook.ts";
import { useDispatch } from "react-redux";
import { stack, useMessageActions } from "@/store/chat.ts";
import { setTheme } from "@/store/globals.ts";
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
    // greentokey — fetch carbon factors once on app boot so the carbon
    // report renders exact estimates immediately.
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
