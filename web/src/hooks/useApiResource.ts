import { useCallback, useEffect, useRef, useState } from "react";
import { isApiError, ApiError, type ApiError as ApiErrorType } from "@/api/client";

interface ResourceState<T> {
  data: T | null;
  error: ApiErrorType | null;
  loading: boolean;
  refreshing: boolean;
  updatedAt: Date | null;
}

export interface ApiResource<T> extends ResourceState<T> {
  refresh: () => void;
}

interface Options {
  /** Poll only while the document is visible. */
  refetchIntervalMs?: number;
  enabled?: boolean;
}

/**
 * Fetches one API resource with abort handling, manual refresh, and optional
 * visible-tab polling. The loader must be stable (useCallback) so that
 * changing filters triggers a new request.
 */
export function useApiResource<T>(
  loader: (signal: AbortSignal) => Promise<T>,
  options: Options = {},
): ApiResource<T> {
  const { refetchIntervalMs, enabled = true } = options;
  const [version, setVersion] = useState(0);
  const [state, setState] = useState<ResourceState<T>>({
    data: null,
    error: null,
    loading: enabled,
    refreshing: false,
    updatedAt: null,
  });
  const inFlight = useRef(false);

  const refresh = useCallback(() => {
    setVersion((current) => current + 1);
  }, []);

  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    let active = true;
    inFlight.current = true;
    setState((current) => ({
      ...current,
      loading: current.data === null,
      refreshing: current.data !== null,
      error: null,
    }));
    loader(controller.signal)
      .then((data) => {
        if (!active) return;
        setState({
          data,
          error: null,
          loading: false,
          refreshing: false,
          updatedAt: new Date(),
        });
      })
      .catch((caught: unknown) => {
        if (!active) return;
        if (caught instanceof DOMException && caught.name === "AbortError") {
          return;
        }
        setState((current) => ({
          ...current,
          loading: false,
          refreshing: false,
          error: isApiError(caught)
            ? caught
            : new ApiError({
                status: 0,
                code: "unknown_error",
                message: "The request could not be completed.",
              }),
        }));
      })
      .finally(() => {
        if (active) inFlight.current = false;
      });
    return () => {
      active = false;
      controller.abort();
    };
  }, [loader, version, enabled]);

  useEffect(() => {
    if (!enabled || !refetchIntervalMs || refetchIntervalMs <= 0) return;
    const interval = window.setInterval(() => {
      if (!document.hidden && !inFlight.current) {
        setVersion((current) => current + 1);
      }
    }, refetchIntervalMs);
    const onVisible = () => {
      if (!document.hidden && !inFlight.current) {
        setVersion((current) => current + 1);
      }
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [enabled, refetchIntervalMs]);

  return { ...state, refresh };
}
