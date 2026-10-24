import { useCallback } from "react";
import {
  Activity,
  AlertTriangle,
  ArrowRight,
  BellRing,
  Clock3,
  History,
  Plus,
  RefreshCw,
  Send,
  Server,
  TrainFront,
} from "lucide-react";
import { Link } from "react-router-dom";
import { overviewApi, providersApi } from "@/api/endpoints";
import type { Overview, ProviderInfo } from "@/api/types";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { PageHeader } from "@/components/layout/PageHeader";
import { EventListItem } from "@/components/events/EventListItem";
import { ProviderHealthTable } from "@/components/providers/ProviderHealthTable";
import {
  DeliveryChannelLabel,
  DeliveryStatusBadge,
  EventKindBadge,
} from "@/components/status";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { useApiResource } from "@/hooks/useApiResource";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";
import { formatTehranTimestamp } from "@/lib/dates";

interface OverviewData {
  overview: Overview;
  providers: ProviderInfo[];
}

function StatCard({
  label,
  value,
  hint,
  icon: Icon,
  tone = "default",
}: {
  label: string;
  value: number | string;
  hint: string;
  icon: typeof Activity;
  tone?: "default" | "warning" | "destructive" | "success";
}) {
  const toneClass =
    tone === "warning"
      ? "text-warning-foreground dark:text-warning"
      : tone === "destructive"
        ? "text-destructive"
        : tone === "success"
          ? "text-success"
          : "text-muted-foreground";
  return (
    <Card className="gap-3 py-4">
      <CardContent className="space-y-1">
        <div className="flex items-center justify-between gap-2">
          <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
            {label}
          </p>
          <Icon className={`size-4 ${toneClass}`} aria-hidden="true" />
        </div>
        <p className="text-2xl font-semibold tabular-nums">{value}</p>
        <p className="text-xs text-muted-foreground">{hint}</p>
      </CardContent>
    </Card>
  );
}

export function OverviewPage() {
  useDocumentTitle("Overview");
  const loader = useCallback(async (signal: AbortSignal): Promise<OverviewData> => {
    const [overview, providerList] = await Promise.all([
      overviewApi.get(signal),
      providersApi.list(signal),
    ]);
    return { overview, providers: providerList.providers };
  }, []);
  const { data, error, loading, refreshing, updatedAt, refresh } =
    useApiResource(loader, { refetchIntervalMs: 30_000 });

  if (loading) {
    return (
      <div className="space-y-6">
        <PageHeader title="Overview" description="Loading dashboard status…" />
        <LoadingState rows={4} label="Loading overview" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="space-y-6">
        <PageHeader title="Overview" />
        <ErrorState
          error={error}
          title="Could not load the overview"
          onRetry={refresh}
        />
      </div>
    );
  }

  if (!data) return null;
  const { overview, providers } = data;
  const attention =
    overview.providers.degraded + overview.deliveries.failed;
  const hasNoMonitors = overview.monitors.total === 0;

  return (
    <div className="space-y-8">
      <PageHeader
        title="Overview"
        description={
          <>
            Monitoring status and recent activity. Data generated{" "}
            <time dateTime={overview.generatedAt}>
              {formatTehranTimestamp(overview.generatedAt)}
            </time>
            {updatedAt ? (
              <span className="block text-xs">
                Refreshes every 30 seconds while this tab is visible.
              </span>
            ) : null}
          </>
        }
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

      {overview.providers.total === 0 ? (
        <Alert variant="warning">
          <Server aria-hidden="true" />
          <AlertTitle>No ticket providers are registered</AlertTitle>
          <AlertDescription>
            Monitors require a registered provider before they can poll.
            Production provider registration is still pending its validation
            gate; monitors can be created only when a provider is available.
          </AlertDescription>
        </Alert>
      ) : null}

      {overview.providers.degraded > 0 ? (
        <Alert variant="destructive">
          <AlertTriangle aria-hidden="true" />
          <AlertTitle>
            {overview.providers.degraded} provider
            {overview.providers.degraded === 1 ? " is" : "s are"} degraded
          </AlertTitle>
          <AlertDescription>
            Provider health is separate from ticket availability. Checks are
            still scheduled; degraded health does not mean tickets are
            unavailable.{" "}
            <a href="#providers" className="underline underline-offset-2">
              Review provider health
            </a>
            .
          </AlertDescription>
        </Alert>
      ) : null}

      {overview.deliveries.failed > 0 ? (
        <Alert variant="warning">
          <Send aria-hidden="true" />
          <AlertTitle>
            {overview.deliveries.failed} notification
            {overview.deliveries.failed === 1 ? "" : "s"} failed to deliver
          </AlertTitle>
          <AlertDescription>
            A failed delivery is not a delivered notification.{" "}
            <Link
              to="/events?tab=deliveries&status=failed"
              className="underline underline-offset-2"
            >
              Inspect failed deliveries
            </Link>
            .
          </AlertDescription>
        </Alert>
      ) : null}

      {overview.deliveries.retryScheduled > 0 ? (
        <Alert variant="info">
          <Clock3 aria-hidden="true" />
          <AlertTitle>
            {overview.deliveries.retryScheduled} delivery retry
            {overview.deliveries.retryScheduled === 1 ? "" : "ies"} scheduled
          </AlertTitle>
          <AlertDescription>
            Retry-scheduled deliveries are not yet delivered.
          </AlertDescription>
        </Alert>
      ) : null}

      <section aria-label="Dashboard counts" className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <StatCard
          label="Monitors"
          value={overview.monitors.total}
          hint={`${overview.monitors.active} active · ${overview.monitors.paused} paused · ${overview.monitors.expired} expired`}
          icon={TrainFront}
        />
        <StatCard
          label="Providers"
          value={overview.providers.total}
          hint={`${overview.providers.degraded} degraded · ${overview.providers.stale} stale`}
          icon={Server}
          tone={overview.providers.degraded > 0 ? "warning" : "default"}
        />
        <StatCard
          label="Deliveries"
          value={overview.deliveries.total}
          hint={`${overview.deliveries.sent} sent · ${overview.deliveries.pending + overview.deliveries.retryScheduled} queued/retry · ${overview.deliveries.failed} failed`}
          icon={BellRing}
          tone={overview.deliveries.failed > 0 ? "warning" : "default"}
        />
        <StatCard
          label="Needs attention"
          value={attention}
          hint={
            attention === 0
              ? "No degraded providers or failed deliveries"
              : `${overview.providers.degraded} provider, ${overview.deliveries.failed} delivery`
          }
          icon={AlertTriangle}
          tone={attention > 0 ? "destructive" : "success"}
        />
      </section>

      <div className="grid gap-6 xl:grid-cols-[1.4fr_1fr]">
        <Card>
          <CardHeader className="flex-row items-start justify-between gap-2">
            <div>
              <CardTitle>Recent events</CardTitle>
              <CardDescription>
                Detection events are historical records, not current
                availability.
              </CardDescription>
            </div>
            <Button asChild variant="ghost" size="sm">
              <Link to="/events">
                View all
                <ArrowRight aria-hidden="true" />
              </Link>
            </Button>
          </CardHeader>
          <CardContent>
            {overview.recentEvents.length === 0 ? (
              <EmptyState
                icon={History}
                title="No events yet"
                description="Availability and provider-health events will appear here after monitoring begins."
              />
            ) : (
              <div className="space-y-3">
                {overview.recentEvents.map((event) => (
                  <EventListItem key={event.id} event={event} />
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex-row items-start justify-between gap-2">
            <div>
              <CardTitle>Recent deliveries</CardTitle>
              <CardDescription>
                Detection and delivery are tracked separately.
              </CardDescription>
            </div>
            <Button asChild variant="ghost" size="sm">
              <Link to="/events?tab=deliveries">
                View all
                <ArrowRight aria-hidden="true" />
              </Link>
            </Button>
          </CardHeader>
          <CardContent>
            {overview.recentDeliveries.length === 0 ? (
              <EmptyState
                icon={Send}
                title="No deliveries yet"
                description="Notifications queued for Telegram or Push will appear here."
              />
            ) : (
              <ul className="divide-y">
                {overview.recentDeliveries.map((delivery) => (
                  <li key={delivery.id} className="py-3 first:pt-0 last:pb-0">
                    <Link
                      to={`/events/${encodeURIComponent(delivery.eventId)}`}
                      className="flex flex-col gap-1.5 rounded-md focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                    >
                      <div className="flex flex-wrap items-center justify-between gap-2">
                        <EventKindBadge kind={delivery.eventKind} />
                        <DeliveryStatusBadge status={delivery.status} />
                      </div>
                      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                        <DeliveryChannelLabel channel={delivery.channel} />
                        <span aria-hidden="true">·</span>
                        <span>
                          Event{" "}
                          <time dateTime={delivery.eventOccurredAt}>
                            {formatTehranTimestamp(delivery.eventOccurredAt)}
                          </time>
                        </span>
                        <span aria-hidden="true">·</span>
                        <Badge variant="outline">
                          {delivery.attemptCount} attempt
                          {delivery.attemptCount === 1 ? "" : "s"}
                        </Badge>
                      </div>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      </div>

      <Card id="providers">
        <CardHeader>
          <CardTitle>Provider health</CardTitle>
          <CardDescription>
            Provider health is independent of ticket availability. A degraded
            or stale provider does not mean tickets are sold out.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ProviderHealthTable providers={providers} />
        </CardContent>
      </Card>

      {hasNoMonitors ? (
        <Card>
          <CardContent className="py-2">
            <EmptyState
              icon={TrainFront}
              title="Create your first monitor"
              description="Choose a route, departure date, passenger counts, and at least one provider. Monitoring runs on the server once created."
              action={
                <Button asChild>
                  <Link to="/monitors/new">
                    <Plus aria-hidden="true" />
                    New monitor
                  </Link>
                </Button>
              }
            />
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}
