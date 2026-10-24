import type { ErrorBody } from "./types";

const API_BASE = "/api/v1";

let csrfToken = "";
let unauthorizedHandler: (() => void) | null = null;

/** Keeps the session CSRF token in memory only. Never persist it. */
export function setCsrfToken(token: string | undefined | null): void {
  csrfToken = token ?? "";
}

export function getCsrfToken(): string {
  return csrfToken;
}

export function setUnauthorizedHandler(handler: (() => void) | null): void {
  unauthorizedHandler = handler;
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly fields: Record<string, string>;
  readonly requestId?: string;
  readonly retryAfterSeconds?: number;

  constructor(options: {
    status: number;
    code: string;
    message: string;
    fields?: Record<string, string>;
    requestId?: string;
    retryAfterSeconds?: number;
  }) {
    super(options.message);
    this.name = "ApiError";
    this.status = options.status;
    this.code = options.code;
    this.fields = options.fields ?? {};
    this.requestId = options.requestId;
    this.retryAfterSeconds = options.retryAfterSeconds;
  }
}

export function isApiError(error: unknown): error is ApiError {
  return error instanceof ApiError;
}

function errorFromResponse(response: Response): ApiError {
  const retryAfter = Number(response.headers.get("Retry-After"));
  return new ApiError({
    status: response.status,
    code: "request_failed",
    message: "The request could not be completed.",
    retryAfterSeconds:
      Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : undefined,
  });
}

async function parseError(response: Response): Promise<ApiError> {
  const retryAfter = Number(response.headers.get("Retry-After"));
  const retryAfterSeconds =
    Number.isFinite(retryAfter) && retryAfter > 0 ? retryAfter : undefined;
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    return errorFromResponse(response);
  }
  if (
    typeof body !== "object" ||
    body === null ||
    !("error" in body) ||
    typeof (body as ErrorBody).error !== "object" ||
    (body as ErrorBody).error === null
  ) {
    return errorFromResponse(response);
  }
  const parsed = body as ErrorBody;
  return new ApiError({
    status: response.status,
    code:
      typeof parsed.error.code === "string"
        ? parsed.error.code
        : "request_failed",
    message:
      typeof parsed.error.message === "string" && parsed.error.message
        ? parsed.error.message
        : "The request could not be completed.",
    fields: parsed.error.fields ?? {},
    requestId: typeof parsed.requestId === "string" ? parsed.requestId : undefined,
    retryAfterSeconds,
  });
}

export interface RequestOptions {
  method?: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  body?: unknown;
  query?: Record<string, string | number | boolean | undefined | null>;
  signal?: AbortSignal;
}

function buildPath(
  path: string,
  query?: RequestOptions["query"],
): string {
  const url = new URL(`${API_BASE}${path}`, window.location.origin);
  if (query) {
    for (const [key, value] of Object.entries(query)) {
      if (value === undefined || value === null || value === "") continue;
      url.searchParams.set(key, String(value));
    }
  }
  return `${url.pathname}${url.search}`;
}

export async function apiRequest<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  const method = options.method ?? "GET";
  const headers: Record<string, string> = { Accept: "application/json" };
  if (options.body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  if (method !== "GET" && csrfToken) {
    headers["X-CSRF-Token"] = csrfToken;
  }

  let response: Response;
  try {
    response = await fetch(buildPath(path, options.query), {
      method,
      headers,
      credentials: "same-origin",
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: options.signal,
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") {
      throw error;
    }
    throw new ApiError({
      status: 0,
      code: "network_error",
      message: "Unable to reach the Neda server. Check your connection.",
    });
  }

  if (!response.ok) {
    const apiError = await parseError(response);
    if (
      response.status === 401 &&
      apiError.code === "unauthenticated" &&
      !path.startsWith("/auth/login")
    ) {
      setCsrfToken("");
      unauthorizedHandler?.();
    }
    throw apiError;
  }

  if (response.status === 204) {
    return undefined as T;
  }

  try {
    return (await response.json()) as T;
  } catch {
    throw new ApiError({
      status: response.status,
      code: "invalid_response",
      message: "The server returned an unexpected response.",
    });
  }
}
