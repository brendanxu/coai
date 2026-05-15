import { configureStore } from "@reduxjs/toolkit";
import infoReducer from "./info";
import globalReducer from "./globals";
import menuReducer from "./menu";
import authReducer from "./auth";
import quotaReducer from "./quota";
import packageReducer from "./package";
import subscriptionReducer from "./subscription";
import apiReducer from "./api";
import settingsReducer from "./settings";
import recordReducer from "./record";
import avatarReducer from "./avatar";
import carbonReducer from "./carbon";

const store = configureStore({
  reducer: {
    info: infoReducer,
    global: globalReducer,
    menu: menuReducer,
    auth: authReducer,
    quota: quotaReducer,
    package: packageReducer,
    subscription: subscriptionReducer,
    api: apiReducer,
    settings: settingsReducer,
    record: recordReducer,
    avatar: avatarReducer,
    // v0.6 carbon
    carbon: carbonReducer,
  },
});

type RootState = ReturnType<typeof store.getState>;
type AppDispatch = typeof store.dispatch;

export function createCronJob(
  dispatch: AppDispatch,
  method: Function,
  interval: number,
  runWhenInit?: boolean,
) {
  if (runWhenInit) dispatch(method());
  return setInterval(() => dispatch(method()), interval * 1000);
}

export function clearCronJob(job: ReturnType<typeof setInterval>) {
  clearInterval(job);
}

export function clearCronJobs(jobs: ReturnType<typeof setInterval>[]) {
  jobs.forEach((job) => clearInterval(job));
}

export type { RootState, AppDispatch };
export default store;
