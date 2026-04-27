import axios from "axios";

/**
 * LemonSqueezy checkout response from `GET /api/payment/checkout`.
 * The backend builds the URL with the authenticated user's ID baked
 * into `checkout[custom][user_id]` so the webhook can attribute the
 * payment correctly.
 */
export type LemonSqueezyCheckoutResponse = {
  status: boolean;
  url?: string;
  error?: string;
};

/**
 * Fetch a personalized LemonSqueezy Checkout URL for the current user.
 * Returns a non-null URL on success, or null on error (including auth failures).
 *
 * Errors are intentionally not thrown — callers (e.g. UpgradeCTA) should
 * surface a friendly toast instead of crashing the page.
 */
export async function getLemonSqueezyCheckoutURL(): Promise<string | null> {
  try {
    const response = await axios.get<LemonSqueezyCheckoutResponse>(
      "/payment/checkout",
    );
    if (response.data.status && response.data.url) {
      return response.data.url;
    }
    return null;
  } catch (e) {
    console.debug("[payment] checkout fetch failed", e);
    return null;
  }
}
