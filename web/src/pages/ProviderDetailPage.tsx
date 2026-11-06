import { useCallback, useState } from "react";
import { ArrowLeft, RefreshCw } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { observabilityApi } from "@/api/endpoints";
import type {
  ObservabilityProviderDetail,
  ObservabilityQueuePoint,
  ObservabilityTrafficPoint,
} from "@/api/types";
import { EmptyState } from "@/components/layout/EmptyState";
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
import { formatDurationMs, formatTehranTimestamp } from "@/lib/dates";

type Window = "1h" | "6h" | "24h" | "7d";

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

const RATE_LIMIT_EVENT_LABELS: Record<string, string> = {
  rate_limited: "429 observed",
  cooldown_extended: "Cooldown extended",
  recovery_progress: "Recovery progress",
  recovery_step: "Recovery step",
  recovered: "Recovery complete",
};

export function ProviderDetailPage() {
  const { id = "" } = useParams();
  const [window, setWindow] = useState<Window>("24h");
  useDocumentTitle("Provider detail");
  const loader = useCallback(
    async (signal: AbortSignal): Promise<ObservabilityProviderDetail> =>
      observabilityApi.provider(id, window, signal),
    [id, window],
  );
  const { data, error, loading, refreshing, refresh } = useApiResource(loader, {
    refetchIntervalMs: 30_000,
    enabled: id !== "",
  });

  if (!id) {
    return <ErrorState error={new Error("Provider id is missing.")} title="Provider not found" />;
  }
  if (loading) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="Provider detail" description="Loading provider telemetry…" />
        <LoadingState rows={4} label="Loading provider detail" />
      </div>
    );
  }
  if (error || !data) {
    return (
      <div className="flex flex-col gap-6">
        <PageHeader title="Provider detail" />
        <ErrorState
          error={error ?? new Error("Provider telemetry is unavailable.")}
          title="Could not load provider detail"
          onRetry={refresh}
        />
      </div>
    );
  }

  const traffic = data.trafficHistory;
  const queue = data.queueHistory;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={data.displayName}
        description={`${data.id} · ${data.activeTargets} active target${data.activeTargets === 1 ? "" : "s"} · effective interval ${formatSeconds(data.adaptive.effectiveIntervalSeconds)}`}
        actions={
          <>
            <Button asChild variant="ghost" size="sm">
              <Link to="/providers">
                <ArrowLeft aria-hidden="true" />
                Providers
              </Link>
            </Button>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={refresh}
              disabled={refreshing}
            >
              <RefreshCw className={refreshing ? "animate-spin" : undefined} aria-hidden="true" />
              Refresh
            </Button>
          </>
        }
      />

      <div className="flex flex-wrap items-center gap-3">
        <ProviderStateBadge state={data.healthState} />
        {data.testOnly ? <TestOnlyBadge /> : null}
        {data.flags.stale ? (
          <Badge variant="warning" title="Last successful evidence is older than the provider's effective stale threshold">
            Stale evidence
          </Badge>
        ) : null}
        <span className="text-xs text-muted-foreground">
          Last provider check {formatTehranTimestamp(data.lastProviderCheckAt)}
        </span>
        <span className="text-xs text-muted-foreground">
          Last success {formatTehranTimestamp(data.lastSuccessAt)}
        </span>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <Metric label="Checks, 24h" value={data.providerChecks.last24h.toLocaleString()} />
        <Metric label="Success rate" value={formatPercent(data.providerChecks.successRate)} />
        <Metric label="Rate-limited, 24h" value={data.providerChecks.rateLimitedChecks24h.toLocaleString()} />
        <Metric label="Incomplete, 24h" value={data.providerChecks.incomplete24h.toLocaleString()} />
        <Metric label="P95" value={formatDurationMs(data.providerChecks.p95DurationMs)} />
      </div>

      <Card>
        <CardHeader className="flex-row items-center justify-between gap-3">
          <CardTitle>Traffic</CardTitle>
          <div className="flex gap-1">
            {(["1h", "6h", "24h", "7d"] as const).map((value) => (
              <Button
                key={value}
                type="button"
                size="sm"
                variant={window === value ? "secondary" : "ghost"}
                onClick={() => setWindow(value)}
              >
                {value}
              </Button>
            ))}
          </div>
        </CardHeader>
        <CardContent>
          <p className="mb-3 text-xs text-muted-foreground">
            Logical provider checks only. Adapter-internal HTTP retries are not counted
            as separate requests in this phase.
          </p>
          <TrafficTable points={traffic} />
        </CardContent>
      </Card>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Adaptive rate limiting</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <p>Level: {data.adaptive.level}</p>
            <p>Adaptive interval: {formatInterval(data.adaptive.adaptiveIntervalSeconds)}</p>
            <p>Effective interval: {formatInterval(data.adaptive.effectiveIntervalSeconds)}</p>
            <p>
              Recovery: {data.adaptive.recoverySuccesses} / {data.adaptive.requiredSuccesses}
            </p>
            <p>
              Cooldown: {data.adaptive.cooldown.status}
              {data.adaptive.cooldown.until
                ? ` · until ${formatTehranTimestamp(data.adaptive.cooldown.until)}`
                : ""}
            </p>
            <p>Last 429: {formatTehranTimestamp(data.adaptive.last429At)}</p>
            <p className="text-xs text-muted-foreground">
              History is {data.adaptive.historyCompleteness.replace("_", " ")}; current
              state is authoritative.
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Queue health</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            {data.queue ? (
              <>
                <p className="text-xs text-muted-foreground">
                  Sampled point-in-time every {data.queue.sampleIntervalSeconds}s.
                  Averages are arithmetic means over available samples, not
                  time-weighted measurements. Missed samples are gaps, not zeros.
                </p>
                {data.queue.stale ? (
                  <Badge variant="warning">Snapshot stale</Badge>
                ) : null}
                <p>Due: {data.queue.due} · queued: {data.queue.queued} · in flight: {data.queue.inFlight}</p>
                <p>Overdue: {data.queue.overdue} · deferred: {data.queue.deferred}</p>
                <p>Deferred by cooldown: {data.queue.deferredByCooldown}</p>
                <p>Deferred by spacing: {data.queue.deferredBySpacing}</p>
                <p>Deferred by concurrency: {data.queue.deferredByConcurrency}</p>
                <p>
                  Oldest pending:{" "}
                  {data.queue.oldestPendingAgeSeconds === null
                    ? "—"
                    : formatSeconds(data.queue.oldestPendingAgeSeconds)}
                </p>
              </>
            ) : (
              <p className="text-muted-foreground">Queue snapshot unavailable.</p>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Target schedule</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="mb-3 text-xs text-muted-foreground">
            Target next-due times can differ because of target backoff, jitter, and
            provider cooldowns. The configured polling interval itself is global.
          </p>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Monitor</TableHead>
                <TableHead>Next due</TableHead>
                <TableHead>Failures</TableHead>
                <TableHead>Last outcome</TableHead>
                <TableHead>Error</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.targets.map((target) => (
                <TableRow key={target.monitorId}>
                  <TableCell>
                    <Link
                      to={`/monitors/${encodeURIComponent(target.monitorId)}`}
                      className="font-medium text-primary underline-offset-4 hover:underline"
                    >
                      {target.monitorLabel}
                    </Link>
                  </TableCell>
                  <TableCell>{formatTehranTimestamp(target.nextDueAt)}</TableCell>
                  <TableCell className="tabular-nums">{target.consecutiveFailures}</TableCell>
                  <TableCell>{target.lastOutcome || "—"}</TableCell>
                  <TableCell className="font-mono text-xs">{target.lastErrorKind || "—"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Queue history</CardTitle>
        </CardHeader>
        <CardContent>
          <QueueTable points={queue} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Recent provider checks</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="mb-3 text-xs text-muted-foreground">
            “Interrupted / incomplete” means the attempt has no durable
            completion record (for example, a process crash). It is excluded from
            success, failure, and duration metrics.
          </p>
          {data.recentChecks.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No recent provider checks recorded.
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Started</TableHead>
                  <TableHead>Outcome</TableHead>
                  <TableHead>Provider error</TableHead>
                  <TableHead>Duration</TableHead>
                  <TableHead>Queue wait</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.recentChecks.map((check, index) => (
                  <TableRow key={`${check.occurredAt}-${index}`}>
                    <TableCell>{formatTehranTimestamp(check.occurredAt)}</TableCell>
                    <TableCell>
                      {check.incomplete ? (
                        <Badge variant="muted">Interrupted / incomplete</Badge>
                      ) : (
                        <span className="capitalize">{check.outcome}</span>
                      )}
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {check.providerErrorKind || "—"}
                    </TableCell>
                    <TableCell>{formatDurationMs(check.durationMs)}</TableCell>
                    <TableCell>{formatDurationMs(check.queueWaitMs)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Rate-limit and cooldown history</CardTitle>
        </CardHeader>
        <CardContent>
          {data.rateLimitEvents.length === 0 ? (
            <p className="text-sm text-muted-foreground">No transition history recorded.</p>
          ) : (
            <ul className="space-y-2 text-sm">
              {data.rateLimitEvents.map((event, index) => (
                <li key={`${event.occurredAt}-${index}`} className="flex flex-wrap gap-2">
                  <span className="font-medium">
                    {RATE_LIMIT_EVENT_LABELS[event.kind] ?? event.kind}
                  </span>
                  {event.kind === "rate_limited" ? (
                    <span className="text-muted-foreground">
                      level {event.previousLevel} → {event.escalationLevel}
                      {event.escalated ? " (escalated)" : event.maintained ? " (maintained)" : ""}
                    </span>
                  ) : null}
                  <span className="text-muted-foreground">
                    adaptive {formatDurationMs(event.previousAdaptiveMs)} →{" "}
                    {formatDurationMs(event.adaptiveMs)}
                  </span>
                  {event.kind === "cooldown_extended" ? (
                    <span className="text-muted-foreground">
                      cooldown → {formatTehranTimestamp(event.cooldownUntil)}
                    </span>
                  ) : null}
                  <span className="text-muted-foreground">{formatTehranTimestamp(event.occurredAt)}</span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <Card>
      <CardContent className="py-4">
        <p className="text-xs font-medium text-muted-foreground">{label}</p>
        <p className="mt-1 text-2xl font-semibold tabular-nums">{value}</p>
      </CardContent>
    </Card>
  );
}

function TrafficTable({ points }: { points: ObservabilityTrafficPoint[] }) {
  if (points.length === 0) {
    return <EmptyState title="No traffic recorded" description="No provider checks were recorded in this window." />;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Bucket</TableHead>
          <TableHead>Checks</TableHead>
          <TableHead>Success</TableHead>
          <TableHead>Failures</TableHead>
          <TableHead>Discarded</TableHead>
          <TableHead>429</TableHead>
          <TableHead>Interrupted</TableHead>
          <TableHead>Avg</TableHead>
          <TableHead>P95</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {points.map((point) => (
          <TableRow key={point.bucketStart}>
            <TableCell>{formatTehranTimestamp(point.bucketStart)}</TableCell>
            <TableCell className="tabular-nums">{point.providerChecks}</TableCell>
            <TableCell className="tabular-nums">{point.successes}</TableCell>
            <TableCell className="tabular-nums">{point.failures}</TableCell>
            <TableCell className="tabular-nums">{point.discarded}</TableCell>
            <TableCell className="tabular-nums">{point.rateLimitedChecks}</TableCell>
            <TableCell className="tabular-nums">{point.interrupted}</TableCell>
            <TableCell>{formatDurationMs(point.averageDurationMs)}</TableCell>
            <TableCell>{formatDurationMs(point.p95DurationMs)}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function QueueTable({ points }: { points: ObservabilityQueuePoint[] }) {
  if (points.length === 0) {
    return <p className="text-sm text-muted-foreground">No scheduler samples recorded.</p>;
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Bucket</TableHead>
          <TableHead>Due avg/max</TableHead>
          <TableHead>Deferred avg/max</TableHead>
          <TableHead>Overdue max</TableHead>
          <TableHead>Dispatches</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {points.map((point) => (
          <TableRow key={point.bucketStart}>
            <TableCell>{formatTehranTimestamp(point.bucketStart)}</TableCell>
            <TableCell className="tabular-nums">{point.dueAvg} / {point.dueMax}</TableCell>
            <TableCell className="tabular-nums">{point.deferredAvg} / {point.deferredMax}</TableCell>
            <TableCell className="tabular-nums">{point.overdueMax}</TableCell>
            <TableCell className="tabular-nums">{point.dispatchAttempts}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
