import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  apiRequest,
  isApiError,
  setCsrfToken,
  setUnauthorizedHandler,
} from "./client";

function jsonResponse(
  body: unknown,
  init: ResponseInit = {},
): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json", ...(init.headers ?? {}) },
    ...init,
  });
}

describe("apiRequest", () => {
  const fetchMock = vi.fn();

  beforeEach(() => {
    fetchMock.mockReset();
    vi.stubGlobal("fetch", fetchMock);
    setCsrfToken("");
    setUnauthorizedHandler(null);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("uses same-origin credentials and adds CSRF only to mutations", async () => {
    setCsrfToken("csrf-value");
    fetchMock.mockResolvedValueOnce(jsonResponse({ monitors: [] }));
    await apiRequest("/monitors");
    let [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("/api/v1/monitors");
    expect(init.credentials).toBe("same-origin");
    expect((init.headers as Record<string, string>)["X-CSRF-Token"]).toBeUndefined();

    fetchMock.mockResolvedValueOnce(jsonResponse({ id: "m1" }, { status: 201 }));
    await apiRequest("/monitors", {
      method: "POST",
      body: { label: "Test" },
    });
    [url, init] = fetchMock.mock.calls[1] as [string, RequestInit];
    expect(url).toBe("/api/v1/monitors");
    expect(init.method).toBe("POST");
    expect(init.credentials).toBe("same-origin");
    const headers = init.headers as Record<string, string>;
    expect(headers["X-CSRF-Token"]).toBe("csrf-value");
    expect(headers["Content-Type"]).toBe("application/json");
    expect(init.body).toBe(JSON.stringify({ label: "Test" }));
  });

  it("builds query strings and omits empty filters", async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse({ events: [], nextCursor: "" }),
    );
    await apiRequest("/events", {
      query: {
        kind: "availability",
        monitorId: "",
        providerId: undefined,
        limit: 25,
        cursor: null,
      },
    });
    const [url] = fetchMock.mock.calls[0] as [string];
    expect(url).toBe("/api/v1/events?kind=availability&limit=25");
  });

  it("normalizes structured errors with fields and Retry-After", async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        {
          error: {
            code: "validation_error",
            message: "validation failed",
            fields: { label: "is required" },
          },
          requestId: "req-123",
        },
        { status: 400, headers: { "Retry-After": "17" } },
      ),
    );
    try {
      await apiRequest("/monitors", { method: "POST", body: {} });
      throw new Error("expected failure");
    } catch (caught) {
      expect(isApiError(caught)).toBe(true);
      const error = caught as ApiError;
      expect(error.status).toBe(400);
      expect(error.code).toBe("validation_error");
      expect(error.fields.label).toBe("is required");
      expect(error.requestId).toBe("req-123");
      expect(error.retryAfterSeconds).toBe(17);
    }
  });

  it("does not leak non-JSON response bodies", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response("<html>internal</html>", {
        status: 500,
        headers: { "Content-Type": "text/html" },
      }),
    );
    await expect(apiRequest("/monitors")).rejects.toMatchObject({
      status: 500,
      code: "request_failed",
      message: "The request could not be completed.",
    });
  });

  it("notifies the auth layer on a private-route 401", async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    setCsrfToken("csrf-value");
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        {
          error: { code: "unauthenticated", message: "authentication required" },
          requestId: "req-1",
        },
        { status: 401 },
      ),
    );
    await expect(apiRequest("/overview")).rejects.toMatchObject({
      status: 401,
    });
    expect(handler).toHaveBeenCalledOnce();
  });

  it("does not treat a failed login as session expiry", async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        {
          error: { code: "invalid_credentials", message: "invalid credentials" },
          requestId: "req-2",
        },
        { status: 401 },
      ),
    );
    await expect(
      apiRequest("/auth/login", { method: "POST", body: { password: "bad" } }),
    ).rejects.toMatchObject({ code: "invalid_credentials" });
    expect(handler).not.toHaveBeenCalled();
  });

  it("does not log out on a wrong current password", async () => {
    const handler = vi.fn();
    setUnauthorizedHandler(handler);
    fetchMock.mockResolvedValueOnce(
      jsonResponse(
        {
          error: { code: "invalid_credentials", message: "invalid credentials" },
          requestId: "req-3",
        },
        { status: 401 },
      ),
    );
    await expect(
      apiRequest("/auth/password", {
        method: "POST",
        body: { currentPassword: "bad", newPassword: "new password 123" },
      }),
    ).rejects.toMatchObject({ code: "invalid_credentials" });
    expect(handler).not.toHaveBeenCalled();
  });

  it("handles 204 responses and network failures", async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    await expect(
      apiRequest("/monitors/m1", { method: "DELETE" }),
    ).resolves.toBeUndefined();

    fetchMock.mockRejectedValueOnce(new TypeError("Failed to fetch"));
    await expect(apiRequest("/monitors")).rejects.toMatchObject({
      status: 0,
      code: "network_error",
    });
  });

  it("rejects unexpected successful response bodies", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response("not json", { status: 200 }),
    );
    await expect(apiRequest("/monitors")).rejects.toMatchObject({
      code: "invalid_response",
    });
  });
});
