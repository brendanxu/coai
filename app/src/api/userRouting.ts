// userRouting.ts — typed fetch helpers for the PKG-4 admin endpoints
// (newapi/admin_routing.go). Mirrors api/carbon.ts conventions: thin axios
// wrappers, no client-side cache (caller refreshes manually).
//
// Endpoints lifted in PKG-4:
//   GET  /api/gtk/v1/admin/user-routing            list (paginated)
//   GET  /api/gtk/v1/admin/user-routing/:user_id   single
//   PUT  /api/gtk/v1/admin/user-routing/:user_id   change group
//   GET  /api/gtk/v1/admin/channels                NewAPI channel inventory
//   PUT  /api/gtk/v1/admin/channels/:channel_id    501 stub in v0; helper
//                                                   present so frontend can
//                                                   show "coming soon" and
//                                                   future v0.20 wires the
//                                                   real call.

import axios from "axios";

// ----- Types -------------------------------------------------------------

export type UserRoutingRow = {
  coai_user_id: number;
  username: string;
  newapi_user_id: number;
  newapi_group: string;
  last_known_quota: number;
};

export type UserRoutingListResponse = {
  users: UserRoutingRow[];
  total: number;
  limit: number;
  offset: number;
};

export type ChannelRow = {
  id: number;
  type: number;
  provider: string;
  provider_label: string;
  name: string;
  status: number; // 1=enabled, 2=disabled
  models: string[] | null;
  response_time_ms: number;
  weight: number;
  priority: number;
  group: string; // comma-separated group whitelist from NewAPI
  is_sub2api: boolean;
};

export type ChannelListResponse = {
  channels: ChannelRow[];
  total: number;
};

type Envelope<T> = {
  success: boolean;
  message?: string;
  data?: T;
  degraded?: boolean;
};

// ----- API calls ---------------------------------------------------------

export async function listUserRouting(params: {
  limit?: number;
  offset?: number;
  group?: string;
}): Promise<UserRoutingListResponse> {
  const q = new URLSearchParams();
  if (params.limit != null) q.set("limit", String(params.limit));
  if (params.offset != null) q.set("offset", String(params.offset));
  if (params.group) q.set("group", params.group);
  const suffix = q.toString() ? `?${q}` : "";
  const resp = await axios.get<Envelope<UserRoutingListResponse>>(
    `/gtk/v1/admin/user-routing${suffix}`,
  );
  if (!resp.data.success || !resp.data.data) {
    throw new Error(resp.data.message || "list user-routing failed");
  }
  return resp.data.data;
}

export async function getUserRouting(userId: number): Promise<UserRoutingRow> {
  const resp = await axios.get<Envelope<UserRoutingRow>>(
    `/gtk/v1/admin/user-routing/${userId}`,
  );
  if (!resp.data.success || !resp.data.data) {
    throw new Error(resp.data.message || "load user-routing failed");
  }
  return resp.data.data;
}

export async function updateUserRouting(
  userId: number,
  newapiGroup: string,
): Promise<UserRoutingRow> {
  const resp = await axios.put<Envelope<UserRoutingRow>>(
    `/gtk/v1/admin/user-routing/${userId}`,
    { newapi_group: newapiGroup },
  );
  if (!resp.data.success || !resp.data.data) {
    throw new Error(resp.data.message || "update user-routing failed");
  }
  return resp.data.data;
}

export async function listAdminChannels(): Promise<ChannelListResponse> {
  const resp = await axios.get<Envelope<ChannelListResponse>>(
    `/gtk/v1/admin/channels`,
  );
  // Treat the documented degraded response (503 + degraded:true) as a
  // soft-empty rather than throw — page renders an "unconfigured"
  // placeholder instead of a red error toast.
  if (resp.data.degraded) {
    return resp.data.data || { channels: [], total: 0 };
  }
  if (!resp.data.success || !resp.data.data) {
    throw new Error(resp.data.message || "list channels failed");
  }
  return resp.data.data;
}

export async function updateChannelGroups(
  channelId: number,
  groups: string[],
): Promise<void> {
  // v0 backend stub returns 501. Surfacing the error message verbatim so
  // the UI shows the "edit via NewAPI dashboard" hint.
  const resp = await axios.put<Envelope<unknown>>(
    `/gtk/v1/admin/channels/${channelId}`,
    { groups },
  );
  if (!resp.data.success) {
    throw new Error(resp.data.message || "update channel groups failed");
  }
}

// Suggested values surfaced in the UI dropdown. Founder-defined per the
// product vision: friend pool (sub2API), official-API (DeepSeek/OpenAI/
// Anthropic direct), service-runtime (Layer-3 service products), default
// (NewAPI default — uncategorized). These are HINTS only — the textbox
// also accepts arbitrary group names so the founder can introduce new
// groups without a code change.
export const SUGGESTED_GROUPS = [
  "default",
  "official-api",
  "friend-pool",
  "service-runtime",
];
