import { useCallback } from "react";
import { RefreshCw } from "lucide-react";
import { Link } from "react-router-dom";
import { observabilityApi } from "@/api/endpoints";
import type { ObservabilityOverview, ObservabilityProvider } from "@/api/types";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { PageHeader } from "@/components/layout/PageHeader";
import { ProviderStateBadge, TestOnlyBadge } from "@/components/status";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useApiResource } from "@/hooks/useApiResource";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";
import { formatElapsed, formatTehranTimestamp } from "@/lib/dates";

function formatSeconds(value: number): string {
  if (value <= 0) return "—";
  if (value % 3600 === 0) return `${value / 3600}h`;
  if (value % 60 === 0) return `${value / 60}m`;
  return `${value}s`;
}

function formatPercent(value: number | null): string {
  return value === null ? "—" : `${(value * 100).toFixed(1)}%`;
}

function formatInterval(value: number): string {
  return value <= 0 ? "None" : formatSeconds(value);
}

function cooldownLabel(provider: ObservabilityProvider): string {
  const cooldown = provider.adaptive.cooldown;
  if (cooldown.status === "active") {
    return `${formatSeconds(cooldown.remainingSeconds)} left`;
  }
  if (cooldown.status === "expired") return "Expired";
  return "—";
}

export function ProvidersPage() {
  useDocumentTitle("Providers");
  const loader = useCallback(
    async (signal: AbortSignal): Promise<ObservabilityOverview> =>
      observabilityApi.overview(signal),
    [],
  );
  const { data, error, loading, refreshing, refresh } = useApiResource(loader, {
    refetchIntervalMs: 30_000,
  });

  if (loading) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="Providers" description="Loading scheduler telemetry…" />
        <LoadingState rows={4} label="Loading provider observability" />
      </div>
    );
  }
  if (error) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="Providers" />
        <ErrorState
          error={error}
          title="Could not load provider observability"
          onRetry={refresh}
        />
      </div>
    );
  }
  if (!data) return null;

  const { scheduler, trafficSummary, providers, scheduling } = data;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Providers"
        description={`Provider checks, adaptive throttling, cooldowns, and queue health. Global polling interval ${formatSeconds(
          scheduling.configuredIntervalSeconds,
        )}.`}
        actions={
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
            Refresh
          </Button>
        }
      />

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
        <Summary label="Provider checks, 24h" value={trafficSummary.providerChecks.toLocaleString()} />
        <Summary label="Success rate, 24h" value={formatPercent(trafficSummary.successRate)} />
        <Summary label="Rate-limited checks, 24h" value={trafficSummary.rateLimitedChecks.toLocaleString()} />
        <Summary label="Incomplete checks, 24h" value={trafficSummary.incomplete.toLocaleString()} />
        <Summary label="Active cooldowns" value={scheduler.activeCooldowns.toLocaleString()} />
        <Summary label="Degraded providers" value={scheduler.degradedProviders.toLocaleString()} />
        <Summary label="Queued targets" value={scheduler.queuedTargets.toLocaleString()} />
        <Summary label={`Recorded, ${trafficSummary.recordedWindowDays}d`} value={trafficSummary.recorded7d.toLocaleString()} />
      </div>

      {!scheduler.available ? (
        <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
          The scheduler runtime snapshot is unavailable. Database-backed health and
          history remain visible; queue metrics are unavailable.
        </p>
      ) : null}
      {scheduler.available && !scheduler.running ? (
        <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
          The scheduler is stopped. The last runtime snapshot is shown but is not
          being refreshed; queue samples represent the final observed state.
        </p>
      ) : null}
      {scheduler.stale && scheduler.available && scheduler.running ? (
        <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
          The scheduler heartbeat is stale. Queue numbers are the last observed
          sample, not a live measurement.
        </p>
      ) : null}
      {!trafficSummary.historyAvailable ? (
        <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
          No logical provider checks have been rolled up yet. A zero is not
          evidence of provider inactivity; check coverage and scheduler state.
        </p>
      ) : null}
      {trafficSummary.historyAvailable && trafficSummary.providerChecks === 0 ? (
        <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
          No logical provider checks were recorded in the last 24 hours.
          {trafficSummary.recorded7d > 0
            ? ` ${trafficSummary.recorded7d.toLocaleString()} checks exist in the recorded 7-day window.`
            : ""}
        </p>
      ) : null}
      {scheduler.rateLimitEventsDropped > 0 ? (
        <p className="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">
          {scheduler.rateLimitEventsDropped.toLocaleString()} rate-limit history
          events were dropped by the bounded best-effort writer. Current provider
          state remains authoritative.
        </p>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Provider monitoring</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="mb-3 text-xs text-muted-foreground">
            “Provider checks” means one logical adapter check. It is not an HTTP
            request count. Global interval applies to all providers; adaptive and
            effective intervals are provider-level. Success rate excludes
            interrupted, discarded, and incomplete checks.
          </p>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Provider</TableHead>
                <TableHead>Health</TableHead>
                <TableHead>Last check</TableHead>
                <TableHead>Checks/min</TableHead>
                <TableHead>Checks/hour</TableHead>
                <TableHead>Rate-limited 24h</TableHead>
                <TableHead>Adaptive</TableHead>
                <TableHead>Effective</TableHead>
                <TableHead>Backoff</TableHead>
                <TableHead>Queue</TableHead>
                <TableHead>Cooldown</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {providers.map((provider) => (
                <TableRow key={provider.id}>
                  <TableCell>
                    <Link
                      to={`/providers/${encodeURIComponent(provider.id)}`}
                      className="font-medium text-primary underline-offset-4 hover:underline"
                    >
                      {provider.displayName}
                    </Link>
                    <p className="mt-0.5 font-mono text-xs text-muted-foreground">
                      {provider.id}
                    </p>
                    {provider.testOnly ? <TestOnlyBadge /> : null}
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-wrap gap-1">
                      <ProviderStateBadge state={provider.healthState} />
                      {provider.flags.stale ? (
                        <Badge variant="warning" title="Last successful evidence is older than the provider's effective stale threshold">
                          Stale evidence
                        </Badge>
                      ) : null}
                    </div>
                  </TableCell>
                  <TableCell className="whitespace-nowrap text-xs text-muted-foreground">
                    {formatElapsed(provider.lastProviderCheckAt)}
                  </TableCell>
                  <TableCell className="tabular-nums">
                    {provider.providerChecks.last1m}
                  </TableCell>
                  <TableCell className="tabular-nums">
                    {provider.providerChecks.last1h}
                  </TableCell>
                  <TableCell className="tabular-nums">
                    {provider.providerChecks.rateLimitedChecks24h}
                  </TableCell>
                  <TableCell className="whitespace-nowrap">
                    {formatInterval(provider.adaptive.adaptiveIntervalSeconds)}
                  </TableCell>
                  <TableCell className="whitespace-nowrap">
                    {formatInterval(provider.adaptive.effectiveIntervalSeconds)}
                  </TableCell>
                  <TableCell className="tabular-nums">
                    {provider.adaptive.level}
                  </TableCell>
                  <TableCell className="tabular-nums">
                    {provider.queue ? provider.queue.queued : "—"}
                    {provider.queue?.stale ? (
                      <span className="ml-1 text-xs text-muted-foreground">(stale)</span>
                    ) : null}
                  </TableCell>
                  <TableCell className="whitespace-nowrap">
                    {cooldownLabel(provider)}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {providers.length === 0 ? (
            <p className="mt-4 text-sm text-muted-foreground">
              No providers are registered.
            </p>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Scheduler state</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-2 text-sm sm:grid-cols-2 lg:grid-cols-4">
          <p>Last execution: {formatTehranTimestamp(scheduler.lastExecutionAt)}</p>
          <p>Last dispatch: {formatTehranTimestamp(scheduler.lastDispatchAt)}</p>
          <p>Due targets: {scheduler.dueTargets}</p>
          <p>Overdue targets: {scheduler.overdueTargets}</p>
          <p>In-flight targets: {scheduler.inFlightTargets}</p>
          <p>Workers: {scheduling.workers}</p>
          <p>Provider spacing: {formatSeconds(scheduling.providerSpacingSeconds)}</p>
          <p>Snapshot: {formatElapsed(scheduler.sampledAt)}</p>
        </CardContent>
      </Card>
    </div>
  );
}

function Summary({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <CardContent className="py-4">
        <p className="text-xs font-medium text-muted-foreground">{label}</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums">{value}</p>
      </CardContent>
    </Card>
  );
}
