import { get, patch, post, request } from "@zonventure/api";

/** webhookEndpointDTO, internal/admin/webhooks.go. */
export interface WebhookEndpoint {
  id: number;
  name: string;
  url: string;
  event_kinds: string[] | null;
  active: boolean;
  secret_hint: string;
  created_at: string;
}

/** webhookDeliveryDTO, internal/admin/webhooks.go. */
export interface WebhookDelivery {
  id: number;
  endpoint_id: number;
  endpoint_name: string;
  delivery_uuid: string;
  event_kind: string;
  status: "pending" | "delivered" | "dead" | string;
  attempts: number;
  last_status?: number;
  last_error?: string;
  next_attempt_at: string;
  created_at: string;
  delivered_at?: string;
  payload: string;
}

const opts = (signal?: AbortSignal) => (signal ? { signal } : {});

export const listWebhooks = (signal?: AbortSignal) => get<{ endpoints: WebhookEndpoint[] | null }>("/api/admin/webhooks", opts(signal));
export const listKinds = (signal?: AbortSignal) => get<{ event_kinds: string[] | null }>("/api/admin/webhook-event-kinds", opts(signal));
export const createWebhook = (body: { name: string; url: string; event_kinds: string[] }) => post<{ secret?: string }>("/api/admin/webhooks", body);
export const updateWebhook = (id: number, body: { name?: string; url?: string; event_kinds?: string[]; active?: boolean }) => patch<void>(`/api/admin/webhooks/${id}`, body);
export const deleteWebhook = (id: number) => request<void>(`/api/admin/webhooks/${id}`, { method: "DELETE" });
export const rotateSecret = (id: number) => post<{ secret?: string }>(`/api/admin/webhooks/${id}/rotate-secret`);
export const testWebhook = (id: number) => post<void>(`/api/admin/webhooks/${id}/test`);

export function listDeliveries(q: { endpoint: number; status: string; before: number }, signal?: AbortSignal) {
  const p = new URLSearchParams({ limit: "50", endpoint_id: String(q.endpoint) });
  if (q.status) p.set("status", q.status);
  if (q.before) p.set("before", String(q.before));
  return get<{ deliveries: WebhookDelivery[] | null; next_before?: number }>("/api/admin/webhooks/deliveries?" + p.toString(), opts(signal));
}
