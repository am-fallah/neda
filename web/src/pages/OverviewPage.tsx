import { useCallback } from "react";
import {
  AlertTriangle,
  ArrowRight,
  ChevronRight,
  Plus,
  RefreshCw,
  TrainFront,
} from "lucide-react";
import { Link } from "react-router-dom";
import { isApiError } from "@/api/client";
import { monitorsApi, overviewApi, providersApi } from "@/api/endpoints";
import type {
  DeliveryRecord,
  EventRecord,
  Monitor,
  Overview,
  ProviderInfo,
} from "@/api/types";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { PageHeader } from "@/components/layout/PageHeader";
import { MonitorOverviewRow } from "@/components/monitors/MonitorOverviewRow";
import { ProviderHealthTable } from "@/components/providers/ProviderHealthTable";
import { DeliveryStatusBadge } from "@/components/status";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Separator } from "@/components/ui/separator";
import { useApiResource } from "@/hooks/useApiResource";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";
import { formatElapsed, formatTehranTimestamp } from "@/lib/dates";
import { summarizeMonitorChecks } from "@/lib/monitorSummary";
import { cn } from "@/lib/utils";

interface OverviewData {
  overview: Overview | null;
  monitors: Monitor[] | null;
  providers: ProviderInfo[] | null;
  errors: {
    overview: Error | null;
    monitors: Error | null;
    providers: Error | null;
  };
}

interface MonitorCounts {
  total: number;
  active: number;
  paused: number;
  expired: number;
}

interface MonitorGroup {
  key: string;
  title: string;
  monitors: Monitor[];
}

interface ProviderHealthCounts {
  total: number;
  degraded: number;
  stale: number;
  healthy: number;
  other: number;
}

function normalizeError(reason: unknown): Error {
  return isApiError(reason)
    ? reason
    : new Error("The request could not be completed.");
}

function countMonitors(monitors: Monitor[]): MonitorCounts {
  return monitors.reduce<MonitorCounts>(
    (counts, monitor) => {
      counts.total += 1;
      counts[monitor.status] += 1;
      return counts;
    },
    { total: 0, active: 0, paused: 0, expired: 0 },
  );
}

function formatMonitorSummary(counts: MonitorCounts): string {
  if (counts.total === 0) return "No monitors yet";

  const parts = [
    `${counts.total} monitor${counts.total === 1 ? "" : "s"}`,
    `${counts.active} active`,
  ];
  if (counts.paused > 0) parts.push(`${counts.paused} paused`);
  if (counts.expired > 0) parts.push(`${counts.expired} expired`);
  return parts.join(" · ");
}

function groupMonitors(monitors: Monitor[]): MonitorGroup[] {
  const attention: Monitor[] = [];
  const active: Monitor[] = [];
  const inactive: Monitor[] = [];

  for (const monitor of monitors) {
    if (monitor.status !== "active") {
      inactive.push(monitor);
      continue;
    }
    const tone = summarizeMonitorChecks(monitor).tone;
    if (tone === "destructive" || tone === "warning") {
      attention.push(monitor);
    } else {
      active.push(monitor);
    }
  }

  const byDeparture = (a: Monitor, b: Monitor) =>
    a.departureDate.localeCompare(b.departureDate) ||
    a.label.localeCompare(b.label);

  attention.sort((a, b) => {
    const rank = (monitor: Monitor) =>
      summarizeMonitorChecks(monitor).tone === "destructive" ? 0 : 1;
    return rank(a) - rank(b) || byDeparture(a, b);
  });
  active.sort(byDeparture);
  inactive.sort((a, b) => {
    const rank = (monitor: Monitor) =>
      monitor.status === "paused" ? 0 : 1;
    return rank(a) - rank(b) || byDeparture(a, b);
  });

  return [
    { key: "attention", title: "Needs attention", monitors: attention },
    { key: "active", title: "Active", monitors: active },
    { key: "inactive", title: "Paused and expired", monitors: inactive },
  ].filter((group) => group.monitors.length > 0);
}

function limitGroups(groups: MonitorGroup[], limit: number): MonitorGroup[] {
  let remaining = limit;
  const visibleGroups: MonitorGroup[] = [];

  for (const group of groups) {
    if (remaining <= 0) break;
    const visibleMonitors = group.monitors.slice(0, remaining);
    if (visibleMonitors.length > 0) {
      visibleGroups.push({ ...group, monitors: visibleMonitors });
      remaining -= visibleMonitors.length;
    }
  }

  return visibleGroups;
}

function countProviderHealth(providers: ProviderInfo[]): ProviderHealthCounts {
  const counts: ProviderHealthCounts = {
    total: providers.length,
    degraded: 0,
    stale: 0,
    healthy: 0,
    other: 0,
  };

  for (const provider of providers) {
    const health = provider.health;
    if (!health) {
      counts.other += 1;
    } else if (health.openEpisode) {
      counts.degraded += 1;
    } else if (health.stale) {
      counts.stale += 1;
    } else if (health.lastSuccessAt) {
      counts.healthy += 1;
    } else {
      counts.other += 1;
    }
  }

  return counts;
}

function formatProviderHealthSummary(counts: ProviderHealthCounts): string {
  if (counts.total === 0) return "No ticket providers are registered.";

  const parts = [`${counts.total} provider${counts.total === 1 ? "" : "s"}`];
  if (counts.degraded > 0) {
    parts.push(`${counts.degraded} degraded`);
  }
  if (counts.stale > 0) {
    parts.push(`${counts.stale} stale`);
  }
  if (counts.healthy > 0) {
    parts.push(`${counts.healthy} healthy`);
  }
  if (counts.other > 0) {
    parts.push(`${counts.other} without recent success`);
  }
  return parts.join(" · ");
}

function pluralize(count: number, singular: string, plural = `${singular}s`) {
  return count === 1 ? singular : plural;
}

function eventKindLabel(kind: EventRecord["kind"]): string {
  switch (kind) {
    case "availability":
      return "Availability";
    case "provider_degraded":
      return "Provider degraded";
    case "provider_recovered":
      return "Provider recovered";
  }
}

function deliveryChannelLabel(channel: DeliveryRecord["channel"]): string {
  return channel === "telegram" ? "Telegram" : "Push";
}

function OverviewActions({
  onRefresh,
  refreshing,
  disabled = false,
}: {
  onRefresh: () => void;
  refreshing: boolean;
  disabled?: boolean;
}) {
  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="sm"
        onClick={onRefresh}
        disabled={disabled || refreshing}
        aria-label={refreshing ? "Refreshing overview" : "Refresh overview"}
      >
        <RefreshCw
          className={refreshing ? "animate-spin" : undefined}
          aria-hidden="true"
          data-icon="inline-start"
        />
        <span className="hidden sm:inline">
          {refreshing ? "Refreshing…" : "Refresh"}
        </span>
      </Button>
      <Button asChild size="sm">
        <Link to="/monitors/new">
          <Plus data-icon="inline-start" aria-hidden="true" />
          New monitor
        </Link>
      </Button>
    </>
  );
}

function AttentionStrip({
  providers,
  recentDeliveries,
}: {
  providers: ProviderInfo[] | null;
  recentDeliveries: DeliveryRecord[] | null;
}) {
  const providerCounts = providers ? countProviderHealth(providers) : null;
  const failedDeliveries =
    recentDeliveries?.filter((delivery) => delivery.status === "failed")
      .length ?? 0;
  const retryDeliveries =
    recentDeliveries?.filter(
      (delivery) => delivery.status === "retry_scheduled",
    ).length ?? 0;

  const providerIssues: string[] = [];
  if (providerCounts && providerCounts.degraded > 0) {
    providerIssues.push(
      `${providerCounts.degraded} degraded ${pluralize(providerCounts.degraded, "provider")}`,
    );
  }
  if (providerCounts && providerCounts.stale > 0) {
    providerIssues.push(
      `${providerCounts.stale} stale ${pluralize(providerCounts.stale, "provider")}`,
    );
  }

  const deliveryIssues: string[] = [];
  if (failedDeliveries > 0) {
    deliveryIssues.push(
      `${failedDeliveries} recent failed ${pluralize(failedDeliveries, "delivery")}`,
    );
  }
  if (retryDeliveries > 0) {
    deliveryIssues.push(
      `${retryDeliveries} recent retry-scheduled ${pluralize(retryDeliveries, "delivery")}`,
    );
  }

  if (providerIssues.length === 0 && deliveryIssues.length === 0) {
    return null;
  }

  const deliveryHistoryHref =
    failedDeliveries > 0 && retryDeliveries === 0
      ? "/events?tab=deliveries&status=failed"
      : retryDeliveries > 0 && failedDeliveries === 0
        ? "/events?tab=deliveries&status=retry_scheduled"
        : "/events?tab=deliveries";

  return (
    <Alert variant="warning">
      <AlertTriangle aria-hidden="true" />
      <AlertTitle>Operational issues</AlertTitle>
      <AlertDescription>
        <div className="flex flex-col gap-1">
          {providerIssues.length > 0 ? (
            <p>
              {providerIssues.join(" · ")}.{" "}
              <a
                href="#provider-health"
                className="font-medium text-foreground underline underline-offset-2 whitespace-nowrap"
              >
                Review provider health
              </a>
            </p>
          ) : null}
          {deliveryIssues.length > 0 ? (
            <p>
              {deliveryIssues.join(" · ")}.{" "}
              <Link
                to={deliveryHistoryHref}
                className="font-medium text-foreground underline underline-offset-2 whitespace-nowrap"
              >
                Inspect recent deliveries
              </Link>
            </p>
          ) : null}
        </div>
      </AlertDescription>
    </Alert>
  );
}

function MonitorSection({ monitors }: { monitors: Monitor[] }) {
  const groups = limitGroups(groupMonitors(monitors), 5);
  const hasMore = monitors.length > 5;

  return (
    <section
      aria-labelledby="monitors-heading"
      className="flex min-w-0 flex-col gap-3"
    >
      <h2 id="monitors-heading" className="text-sm font-semibold">
        Monitors
      </h2>
      <div className="overflow-hidden rounded-xl border bg-card">
        {groups.map((group, index) => (
          <section
            key={group.key}
            aria-labelledby={`monitor-group-${group.key}`}
            className={cn(index > 0 && "border-t")}
          >
            <h3
              id={`monitor-group-${group.key}`}
              className="px-4 pt-4 pb-1 text-xs font-medium text-muted-foreground"
            >
              {group.title}
            </h3>
            <ul className="divide-y">
              {group.monitors.map((monitor) => (
                <li key={monitor.id}>
                  <MonitorOverviewRow monitor={monitor} />
                </li>
              ))}
            </ul>
          </section>
        ))}
        {hasMore ? (
          <div className="border-t px-4 py-3">
            <Link
              to="/monitors"
              className="inline-flex items-center gap-1 text-sm font-medium text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
            >
              View all monitors
              <ArrowRight className="size-3.5" aria-hidden="true" />
            </Link>
          </div>
        ) : null}
      </div>
    </section>
  );
}

function AvailabilityItem({ event }: { event: EventRecord }) {
  const title =
    event.monitorLabel?.trim() ||
    [event.originName, event.destinationName].filter(Boolean).join(" → ") ||
    "Availability event";
  const details = [
    event.trainNumber ? `Train ${event.trainNumber}` : null,
    event.wagonName,
    event.className,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <li>
      <Link
        to={`/events/${encodeURIComponent(event.id)}`}
        className="group flex items-start justify-between gap-3 rounded-md py-2 transition-colors hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset focus-visible:outline-none"
      >
        <span className="min-w-0">
          <span className="block truncate text-sm font-medium">{title}</span>
          <span className="mt-0.5 block text-xs text-muted-foreground">
            {details ? `${details} · ` : null}
            <time dateTime={event.occurredAt}>
              {formatTehranTimestamp(event.occurredAt)}
            </time>
          </span>
        </span>
        <ChevronRight
          className="mt-0.5 size-3.5 shrink-0 text-muted-foreground group-hover:text-foreground"
          aria-hidden="true"
        />
      </Link>
    </li>
  );
}

function DeliveryItem({
  delivery,
  monitor,
}: {
  delivery: DeliveryRecord;
  monitor?: Monitor;
}) {
  return (
    <li>
      <Link
        to={`/events/${encodeURIComponent(delivery.eventId)}`}
        className="group flex items-start justify-between gap-3 rounded-md py-2 transition-colors hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset focus-visible:outline-none"
      >
        <span className="min-w-0">
          <span className="block truncate text-sm font-medium">
            {monitor?.label ?? eventKindLabel(delivery.eventKind)}
          </span>
          <span className="mt-0.5 block text-xs text-muted-foreground">
            {deliveryChannelLabel(delivery.channel)} ·{" "}
            <time dateTime={delivery.createdAt}>
              {formatElapsed(delivery.createdAt)}
            </time>
          </span>
        </span>
        <DeliveryStatusBadge status={delivery.status} />
      </Link>
    </li>
  );
}

function RecentAvailabilitySection({
  overview,
  error,
}: {
  overview: Overview | null;
  error: Error | null;
}) {
  const events = overview?.recentEvents.slice(0, 3) ?? [];

  return (
    <section
      aria-labelledby="recent-availability-heading"
      className="flex flex-col gap-3"
    >
      <div className="flex items-baseline justify-between gap-3">
        <h2 id="recent-availability-heading" className="text-sm font-semibold">
          Recent availability
        </h2>
        <Link
          to="/events"
          className="text-xs font-medium text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
        >
          History
        </Link>
      </div>
      {error ? (
        <p className="text-sm text-muted-foreground">
          Recent availability could not be loaded.
        </p>
      ) : events.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No availability events yet.
        </p>
      ) : (
        <>
          <ul className="flex flex-col gap-1">
            {events.map((event) => (
              <AvailabilityItem key={event.id} event={event} />
            ))}
          </ul>
          <p className="text-xs text-muted-foreground">
            Saved observations, not live ticket availability.
          </p>
        </>
      )}
    </section>
  );
}

function ProviderHealthSection({
  providers,
  error,
  onRetry,
}: {
  providers: ProviderInfo[] | null;
  error: Error | null;
  onRetry: () => void;
}) {
  const counts = providers ? countProviderHealth(providers) : null;

  return (
    <section
      id="provider-health"
      aria-labelledby="provider-health-heading"
      className="flex scroll-mt-24 flex-col gap-3"
    >
      <h2 id="provider-health-heading" className="text-sm font-semibold">
        Provider health
      </h2>
      {error ? (
        <div className="flex flex-col items-start gap-1.5">
          <p className="text-sm text-muted-foreground">
            Provider health could not be loaded.
          </p>
          <Button
            type="button"
            variant="link"
            size="sm"
            className="h-auto p-0"
            onClick={onRetry}
          >
            Try again
          </Button>
        </div>
      ) : !providers || providers.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No ticket providers are registered. Monitors cannot poll until one is
          available.
        </p>
      ) : (
        <>
          <p className="text-sm text-muted-foreground">
            {counts ? formatProviderHealthSummary(counts) : null}
          </p>
          <Dialog>
            <DialogTrigger asChild>
              <Button
                type="button"
                variant="link"
                size="sm"
                className="h-auto w-fit p-0 text-xs text-muted-foreground hover:text-foreground"
              >
                View provider details
              </Button>
            </DialogTrigger>
            <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-5xl">
              <DialogHeader>
                <DialogTitle>Provider health</DialogTitle>
                <DialogDescription>
                  Provider health is independent of ticket availability. A
                  degraded or stale provider does not mean tickets are sold
                  out.
                </DialogDescription>
              </DialogHeader>
              <p className="text-xs text-muted-foreground lg:hidden">
                Scroll the table horizontally to see all provider columns.
              </p>
              <ProviderHealthTable providers={providers} />
            </DialogContent>
          </Dialog>
          <p className="text-xs text-muted-foreground">
            Provider health is independent of ticket availability.
          </p>
        </>
      )}
    </section>
  );
}

function RecentDeliveriesSection({
  overview,
  error,
  monitors,
}: {
  overview: Overview | null;
  error: Error | null;
  monitors: Monitor[];
}) {
  const deliveries = overview?.recentDeliveries.slice(0, 5) ?? [];
  const monitorMap = new Map(monitors.map((monitor) => [monitor.id, monitor]));

  return (
    <section
      aria-labelledby="recent-deliveries-heading"
      className="flex flex-col gap-3"
    >
      <div className="flex items-baseline justify-between gap-3">
        <h2 id="recent-deliveries-heading" className="text-sm font-semibold">
          Recent deliveries
        </h2>
        <Link
          to="/events?tab=deliveries"
          className="text-xs font-medium text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
        >
          History
        </Link>
      </div>
      {error ? (
        <p className="text-sm text-muted-foreground">
          Recent deliveries could not be loaded.
        </p>
      ) : deliveries.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No delivery records yet.
        </p>
      ) : (
        <>
          <ul className="flex flex-col gap-2">
            {deliveries.map((delivery) => (
              <DeliveryItem
                key={delivery.id}
                delivery={delivery}
                monitor={
                  delivery.monitorId
                    ? monitorMap.get(delivery.monitorId)
                    : undefined
                }
              />
            ))}
          </ul>
          <p className="text-xs text-muted-foreground">
            Recent records only, not the full delivery history.
          </p>
        </>
      )}
    </section>
  );
}

export function OverviewPage() {
  useDocumentTitle("Overview");

  const loader = useCallback(async (signal: AbortSignal): Promise<OverviewData> => {
    const [overviewResult, monitorsResult, providersResult] =
      await Promise.allSettled([
        overviewApi.get(signal),
        monitorsApi.list(signal),
        providersApi.list(signal),
      ]);

    if (signal.aborted) {
      throw new DOMException("Aborted", "AbortError");
    }

    const errors = {
      overview:
        overviewResult.status === "rejected"
          ? normalizeError(overviewResult.reason)
          : null,
      monitors:
        monitorsResult.status === "rejected"
          ? normalizeError(monitorsResult.reason)
          : null,
      providers:
        providersResult.status === "rejected"
          ? normalizeError(providersResult.reason)
          : null,
    };

    if (
      overviewResult.status === "rejected" &&
      monitorsResult.status === "rejected" &&
      providersResult.status === "rejected"
    ) {
      throw (
        errors.monitors ??
        errors.overview ??
        errors.providers ??
        new Error("The request could not be completed.")
      );
    }

    return {
      overview:
        overviewResult.status === "fulfilled" ? overviewResult.value : null,
      monitors:
        monitorsResult.status === "fulfilled"
          ? monitorsResult.value.monitors
          : null,
      providers:
        providersResult.status === "fulfilled"
          ? providersResult.value.providers
          : null,
      errors,
    };
  }, []);

  const { data, error, loading, refreshing, refresh } = useApiResource(loader, {
    refetchIntervalMs: 30_000,
  });

  if (loading) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Overview"
          description="Loading dashboard status…"
          actions={
            <OverviewActions
              onRefresh={refresh}
              refreshing={refreshing}
              disabled
            />
          }
        />
        <LoadingState rows={4} label="Loading overview" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader
          title="Overview"
          actions={
            <OverviewActions onRefresh={refresh} refreshing={refreshing} />
          }
        />
        <ErrorState
          error={error}
          title="Could not load the overview"
          onRetry={refresh}
        />
      </div>
    );
  }

  if (!data) return null;

  const { overview, monitors, providers, errors } = data;
  const summary = monitors
    ? formatMonitorSummary(countMonitors(monitors))
    : "Monitor list unavailable";
  const showAttention = monitors === null || monitors.length > 0;

  return (
    <div className="flex flex-col gap-6 sm:gap-8">
      <PageHeader
        title="Overview"
        description={summary}
        actions={
          <OverviewActions onRefresh={refresh} refreshing={refreshing} />
        }
      />

      {showAttention ? (
        <AttentionStrip
          providers={providers}
          recentDeliveries={overview?.recentDeliveries ?? null}
        />
      ) : null}

      {monitors === null ? (
        <ErrorState
          error={errors.monitors}
          title="Could not load monitors"
          onRetry={refresh}
        />
      ) : monitors.length === 0 ? (
        <EmptyState
          icon={TrainFront}
          title="Create your first monitor"
          description={
            providers && providers.length === 0
              ? "No ticket providers are registered yet. A monitor needs a provider before it can poll."
              : "Choose a route and departure date. Monitoring runs on the server once created."
          }
          action={
            <Button asChild>
              <Link to="/monitors/new">
                <Plus data-icon="inline-start" aria-hidden="true" />
                New monitor
              </Link>
            </Button>
          }
        />
      ) : (
        <div className="grid gap-8 xl:grid-cols-[minmax(0,1fr)_20rem] 2xl:grid-cols-[minmax(0,1fr)_22rem]">
          <MonitorSection monitors={monitors} />
          <aside
            className="flex min-w-0 flex-col gap-6"
            aria-label="Recent activity and system health"
          >
            <RecentAvailabilitySection
              overview={overview}
              error={errors.overview}
            />
            <Separator />
            <ProviderHealthSection
              providers={providers}
              error={errors.providers}
              onRetry={refresh}
            />
            <Separator />
            <RecentDeliveriesSection
              overview={overview}
              error={errors.overview}
              monitors={monitors}
            />
          </aside>
        </div>
      )}
    </div>
  );
}
