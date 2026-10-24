import { apiRequest } from "./client";
import type {
  DeliveryListParams,
  DeliveryListResponse,
  EventListParams,
  EventListResponse,
  EventRecord,
  LoginResponse,
  Monitor,
  MonitorCreateRequest,
  MonitorDetail,
  MonitorListResponse,
  MonitorPatchRequest,
  Overview,
  ProviderListResponse,
  SessionResponse,
  Settings,
  SettingsUpdateRequest,
  TelegramConfigRequest,
} from "./types";

export const authApi = {
  login(password: string) {
    return apiRequest<LoginResponse>("/auth/login", {
      method: "POST",
      body: { password },
    });
  },
  session(signal?: AbortSignal) {
    return apiRequest<SessionResponse>("/auth/session", { signal });
  },
  logout() {
    return apiRequest<void>("/auth/logout", { method: "POST" });
  },
  changePassword(currentPassword: string, newPassword: string) {
    return apiRequest<void>("/auth/password", {
      method: "POST",
      body: { currentPassword, newPassword },
    });
  },
};

export const monitorsApi = {
  list(signal?: AbortSignal) {
    return apiRequest<MonitorListResponse>("/monitors", { signal });
  },
  get(id: string, signal?: AbortSignal) {
    return apiRequest<MonitorDetail>(`/monitors/${encodeURIComponent(id)}`, {
      signal,
    });
  },
  create(body: MonitorCreateRequest) {
    return apiRequest<Monitor>("/monitors", { method: "POST", body });
  },
  update(id: string, body: MonitorPatchRequest) {
    return apiRequest<Monitor>(`/monitors/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body,
    });
  },
  pause(id: string) {
    return apiRequest<Monitor>(
      `/monitors/${encodeURIComponent(id)}/pause`,
      { method: "POST" },
    );
  },
  resume(id: string) {
    return apiRequest<Monitor>(
      `/monitors/${encodeURIComponent(id)}/resume`,
      { method: "POST" },
    );
  },
  remove(id: string) {
    return apiRequest<void>(`/monitors/${encodeURIComponent(id)}`, {
      method: "DELETE",
    });
  },
};

export const providersApi = {
  list(signal?: AbortSignal) {
    return apiRequest<ProviderListResponse>("/providers", { signal });
  },
};

export const eventsApi = {
  list(params: EventListParams, signal?: AbortSignal) {
    return apiRequest<EventListResponse>("/events", {
      query: {
        kind: params.kind,
        monitorId: params.monitorId,
        providerId: params.providerId,
        limit: params.limit,
        cursor: params.cursor,
      },
      signal,
    });
  },
  get(id: string, signal?: AbortSignal) {
    return apiRequest<EventRecord>(`/events/${encodeURIComponent(id)}`, {
      signal,
    });
  },
};

export const deliveriesApi = {
  list(params: DeliveryListParams, signal?: AbortSignal) {
    return apiRequest<DeliveryListResponse>("/deliveries", {
      query: {
        channel: params.channel,
        status: params.status,
        eventId: params.eventId,
        kind: params.kind,
        limit: params.limit,
        cursor: params.cursor,
      },
      signal,
    });
  },
};

export const overviewApi = {
  get(signal?: AbortSignal) {
    return apiRequest<Overview>("/overview", { signal });
  },
};

export const settingsApi = {
  get(signal?: AbortSignal) {
    return apiRequest<Settings>("/settings", { signal });
  },
  update(body: SettingsUpdateRequest) {
    return apiRequest<Settings>("/settings", { method: "PUT", body });
  },
  updateTelegram(body: TelegramConfigRequest) {
    return apiRequest<Settings>("/settings/telegram", {
      method: "PUT",
      body,
    });
  },
  testTelegram() {
    return apiRequest<Settings>("/settings/telegram/test", { method: "POST" });
  },
};
