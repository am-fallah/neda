import { useCallback, useMemo } from "react";
import { FilterX, Plus, RefreshCw, Search, TrainFront } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import { monitorsApi, providersApi } from "@/api/endpoints";
import type { Monitor, MonitorStatus, ProviderInfo } from "@/api/types";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { PageHeader } from "@/components/layout/PageHeader";
import { MonitorCard } from "@/components/monitors/MonitorCard";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useApiResource } from "@/hooks/useApiResource";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";
import { cn } from "@/lib/utils";

interface MonitorListData {
  monitors: Monitor[];
  providers: ProviderInfo[];
}

const STATUS_OPTIONS: Array<{ value: "" | MonitorStatus; label: string }> = [
  { value: "", label: "All" },
  { value: "active", label: "Active" },
  { value: "paused", label: "Paused" },
  { value: "expired", label: "Expired" },
];

export function MonitorListPage() {
  useDocumentTitle("Monitors");
  const [searchParams, setSearchParams] = useSearchParams();
  const status = (searchParams.get("status") ?? "") as "" | MonitorStatus;
  const query = searchParams.get("q") ?? "";

  const loader = useCallback(async (signal: AbortSignal): Promise<MonitorListData> => {
    const [monitorList, providerList] = await Promise.all([
      monitorsApi.list(signal),
      providersApi.list(signal),
    ]);
    return { monitors: monitorList.monitors, providers: providerList.providers };
  }, []);
  const { data, error, loading, refreshing, refresh } = useApiResource(loader, {
    refetchIntervalMs: 30_000,
  });

  const providerMap = useMemo(
    () =>
      new Map(data?.providers.map((provider) => [provider.id, provider]) ?? []),
    [data?.providers],
  );

  const filtered = useMemo(() => {
    const monitors = data?.monitors ?? [];
    const needle = query.trim().toLowerCase();
    return monitors
      .filter((monitor) => (status ? monitor.status === status : true))
      .filter((monitor) =>
        needle
          ? [monitor.label, monitor.originName, monitor.destinationName].some(
              (value) => value.toLowerCase().includes(needle),
            )
          : true,
      )
      .sort((a, b) => {
        const rank: Record<MonitorStatus, number> = {
          active: 0,
          paused: 1,
          expired: 2,
        };
        return (
          rank[a.status] - rank[b.status] ||
          a.departureDate.localeCompare(b.departureDate) ||
          a.label.localeCompare(b.label)
        );
      });
  }, [data?.monitors, query, status]);

  const counts = useMemo(() => {
    const monitors = data?.monitors ?? [];
    return {
      "": monitors.length,
      active: monitors.filter((monitor) => monitor.status === "active").length,
      paused: monitors.filter((monitor) => monitor.status === "paused").length,
      expired: monitors.filter((monitor) => monitor.status === "expired").length,
    } satisfies Record<"" | MonitorStatus, number>;
  }, [data?.monitors]);

  function updateFilter(next: { status?: string; q?: string }) {
    const params = new URLSearchParams(searchParams);
    if (next.status !== undefined) {
      if (next.status) params.set("status", next.status);
      else params.delete("status");
    }
    if (next.q !== undefined) {
      if (next.q) params.set("q", next.q);
      else params.delete("q");
    }
    setSearchParams(params, { replace: true });
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <PageHeader title="Monitors" description="Loading monitors…" />
        <LoadingState rows={3} label="Loading monitors" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="space-y-6">
        <PageHeader title="Monitors" />
        <ErrorState
          error={error}
          title="Could not load monitors"
          onRetry={refresh}
        />
      </div>
    );
  }

  const total = data?.monitors.length ?? 0;
  const hasFilters = Boolean(status || query);
  const noProviders = (data?.providers.length ?? 0) === 0;
  const activeCount = counts.active;

  return (
    <div className="space-y-5">
      <PageHeader
        eyebrow="Workspace"
        title="Monitors"
        description="Each monitor tracks one route and departure date across one or more providers. Monitoring runs on the server, independent of this browser."
        actions={
          <>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={refresh}
              disabled={refreshing}
            >
              <RefreshCw
                className={refreshing ? "animate-spin" : undefined}
                aria-hidden="true"
              />
              {refreshing ? "Refreshing…" : "Refresh"}
            </Button>
            <Button asChild size="sm">
              <Link to="/monitors/new">
                <Plus aria-hidden="true" />
                New monitor
              </Link>
            </Button>
          </>
        }
      />

      {noProviders ? (
        <Alert variant="warning">
          <TrainFront aria-hidden="true" />
          <AlertTitle>No ticket providers are registered</AlertTitle>
          <AlertDescription>
            A monitor needs at least one registered provider. In this build,
            provider registration is still pending; the development environment
            can explicitly enable the test-only provider.
          </AlertDescription>
        </Alert>
      ) : null}

      <div className="flex flex-col gap-3 rounded-lg border bg-card p-2.5 shadow-xs sm:flex-row sm:items-center">
        <div className="relative min-w-0 flex-1">
          <label htmlFor="monitor-search" className="sr-only">
            Search monitors
          </label>
          <Search
            className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
            aria-hidden="true"
          />
          <Input
            id="monitor-search"
            type="search"
            placeholder="Search label or route"
            value={query}
            onChange={(event) => updateFilter({ q: event.target.value })}
            className="h-10 border-transparent bg-muted/45 pl-9 hover:border-input focus-visible:bg-card sm:h-9"
          />
        </div>
        <div
          className="flex flex-wrap items-center gap-1"
          role="group"
          aria-label="Filter by status"
        >
          {STATUS_OPTIONS.map((option) => {
            const selected = status === option.value;
            return (
              <button
                key={option.value || "all"}
                type="button"
                aria-pressed={selected}
                onClick={() => updateFilter({ status: option.value })}
                className={cn(
                  "inline-flex min-h-9 cursor-pointer items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium transition-[background-color,color,border-color] duration-150 max-sm:min-h-11 motion-reduce:transition-none",
                  selected
                    ? "border-primary/25 bg-accent text-accent-foreground"
                    : "border-transparent text-muted-foreground hover:bg-muted hover:text-foreground",
                )}
              >
                {option.label}
                <span className="font-mono text-[0.65rem] tabular-nums opacity-70">
                  {counts[option.value]}
                </span>
              </button>
            );
          })}
        </div>
        {hasFilters ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={() => setSearchParams({}, { replace: true })}
            className="justify-start sm:justify-center"
          >
            <FilterX aria-hidden="true" />
            Clear
          </Button>
        ) : null}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2">
        <p className="text-xs text-muted-foreground" aria-live="polite">
          Showing {filtered.length} of {total} monitor{total === 1 ? "" : "s"}.
        </p>
        <p className="font-mono text-[0.65rem] tracking-wide text-muted-foreground uppercase">
          {activeCount} active
        </p>
      </div>

      {total === 0 ? (
        <EmptyState
          icon={TrainFront}
          title="No monitors yet"
          description="Create a monitor with a route, Jalali departure date, passenger counts, and at least one provider."
          action={
            <Button asChild>
              <Link to="/monitors/new">
                <Plus aria-hidden="true" />
                New monitor
              </Link>
            </Button>
          }
        />
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={FilterX}
          title="No monitors match these filters"
          description="Adjust the search or status filter."
          action={
            <Button
              type="button"
              variant="outline"
              onClick={() => setSearchParams({}, { replace: true })}
            >
              Clear filters
            </Button>
          }
        />
      ) : (
        <div
          className="grid gap-3 md:grid-cols-2"
          aria-live="polite"
        >
          {filtered.map((monitor) => (
            <MonitorCard
              key={monitor.id}
              monitor={monitor}
              providerMap={providerMap}
            />
          ))}
        </div>
      )}
    </div>
  );
}
