import { useCallback } from "react";
import { AlertTriangle, ArrowLeft, History } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { eventsApi, providersApi } from "@/api/endpoints";
import type { EventRecord, ProviderInfo } from "@/api/types";
import { BookingLink } from "@/components/BookingLink";
import { ProviderPrice } from "@/components/ProviderPrice";
import { DeliveryTable } from "@/components/events/DeliveryTable";
import { Breadcrumbs } from "@/components/layout/Breadcrumbs";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { PageHeader } from "@/components/layout/PageHeader";
import { EventKindBadge } from "@/components/status";
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
import {
  deliverySummaryText,
  deliverySummaryTone,
  isFullyDelivered,
} from "@/lib/monitorSummary";

interface EventData {
  event: EventRecord;
  providers: ProviderInfo[];
}

function DetailRow({
  label,
  children,
  className,
}: {
  label: string;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={className}>
      <dt className="eyebrow text-[0.6rem]">{label}</dt>
      <dd className="mt-1 text-sm">{children}</dd>
    </div>
  );
}

export function EventDetailPage() {
  const { id = "" } = useParams();
  useDocumentTitle("Event detail");
  const loader = useCallback(
    async (signal: AbortSignal): Promise<EventData> => {
      const [event, providerList] = await Promise.all([
        eventsApi.get(id, signal),
        providersApi.list(signal),
      ]);
      return { event, providers: providerList.providers };
    },
    [id],
  );
  const { data, error, loading, refresh } = useApiResource(loader);

  if (loading) {
    return (
      <div className="space-y-6">
        <Breadcrumbs items={[{ label: "History", to: "/events" }, { label: "Event detail" }]} />
        <PageHeader title="Event detail" description="Loading event…" />
        <LoadingState rows={3} label="Loading event" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="space-y-6">
        <Breadcrumbs items={[{ label: "History", to: "/events" }, { label: "Event detail" }]} />
        <PageHeader title="Event detail" />
        <ErrorState
          error={error}
          title={error.status === 404 ? "Event not found" : "Could not load the event"}
          onRetry={refresh}
        />
        <Button asChild variant="outline" className="w-fit">
          <Link to="/events">
            <ArrowLeft aria-hidden="true" />
            Back to history
          </Link>
        </Button>
      </div>
    );
  }

  if (!data) return null;
  const { event, providers } = data;
  const providerName =
    providers.find((provider) => provider.id === event.providerId)?.displayName ??
    event.providerId;
  const fullyDelivered = isFullyDelivered(event.deliverySummary);

  return (
    <div className="space-y-5">
      <Breadcrumbs
        items={[{ label: "History", to: "/events" }, { label: "Event detail" }]}
      />
      <PageHeader
        eyebrow="Event record"
        title={
          event.kind === "availability"
            ? "Availability event"
            : event.kind === "provider_degraded"
              ? "Provider degraded event"
              : "Provider recovered event"
        }
        description={
          <span className="flex flex-wrap items-center gap-2">
            <EventKindBadge kind={event.kind} />
            <span>
              Detected{" "}
              <time dateTime={event.occurredAt}>
                {formatTehranTimestamp(event.occurredAt)}
              </time>
            </span>
          </span>
        }
        actions={
          <Button asChild variant="outline" size="sm">
            <Link to="/events">
              <ArrowLeft aria-hidden="true" />
              Back to history
            </Link>
          </Button>
        }
      />

      <Alert variant="info">
        <History aria-hidden="true" />
        <AlertTitle>Historical record</AlertTitle>
        <AlertDescription>
          This event records what was detected at that time. It is not current
          availability and does not guarantee that a ticket can still be
          booked.
        </AlertDescription>
      </Alert>

      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_22rem]">
        <Card>
          <CardHeader>
            <CardTitle>Event details</CardTitle>
            <CardDescription>
              Recorded facts and identifiers for this detection.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <dl className="grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-3">
              <DetailRow label="Provider">{providerName}</DetailRow>
              <DetailRow label="Occurred">
                <time dateTime={event.occurredAt}>
                  {formatTehranTimestamp(event.occurredAt)}
                </time>
              </DetailRow>
              <DetailRow label="Recorded">
                <time dateTime={event.createdAt}>
                  {formatTehranTimestamp(event.createdAt)}
                </time>
              </DetailRow>
              {event.monitorLabel || event.monitorId ? (
                <DetailRow label="Monitor">
                  {event.monitorId ? (
                    <Link
                      to={`/monitors/${encodeURIComponent(event.monitorId)}`}
                      className="font-medium text-primary underline-offset-4 hover:underline"
                    >
                      {event.monitorLabel ?? event.monitorId}
                    </Link>
                  ) : (
                    (event.monitorLabel ?? "—")
                  )}
                </DetailRow>
              ) : null}

              {event.kind === "availability" ? (
                <>
                  <DetailRow label="State">
                    <Badge variant="success">Reservable at detection</Badge>
                  </DetailRow>
                  <DetailRow label="Route">
                    {event.originName || event.destinationName ? (
                      <>
                        <bdi dir="auto">{event.originName ?? "?"}</bdi>
                        <span aria-hidden="true"> → </span>
                        <bdi dir="auto">{event.destinationName ?? "?"}</bdi>
                      </>
                    ) : (
                      "—"
                    )}
                  </DetailRow>
                  <DetailRow label="Train">
                    {event.trainNumber || "Unknown"}
                    {event.operator ? (
                      <span className="text-muted-foreground"> · {event.operator}</span>
                    ) : null}
                  </DetailRow>
                  <DetailRow label="Class">
                    <bdi dir="auto">
                      {[event.wagonName, event.className]
                        .filter(Boolean)
                        .join(" · ") || "Unknown"}
                    </bdi>
                  </DetailRow>
                  <DetailRow label="Departure">
                    {event.departureAt ? (
                      <time dateTime={event.departureAt}>
                        {formatTehranTimestamp(event.departureAt)}
                      </time>
                    ) : (
                      "Time unavailable"
                    )}
                  </DetailRow>
                  <DetailRow label="Arrival">
                    {event.arrivalAt ? (
                      <time dateTime={event.arrivalAt}>
                        {formatTehranTimestamp(event.arrivalAt)}
                      </time>
                    ) : (
                      "Time unavailable"
                    )}
                  </DetailRow>
                  <DetailRow label="Price">
                    {event.priceAmount !== null &&
                    event.priceAmount !== undefined ? (
                      <ProviderPrice
                        amount={event.priceAmount}
                        currency={event.priceCurrency}
                        emptyLabel="—"
                        showUnverifiedMarker
                      />
                    ) : (
                      "—"
                    )}
                  </DetailRow>
                  <DetailRow label="Booking">
                    <BookingLink url={event.bookingUrl} />
                  </DetailRow>
                </>
              ) : (
                <>
                  <DetailRow label="Error category">
                    {event.errorKind ? (
                      <code className="font-mono text-xs">{event.errorKind}</code>
                    ) : (
                      "—"
                    )}
                  </DetailRow>
                  <DetailRow label="Failure count">
                    {event.failureCount ?? "—"}
                  </DetailRow>
                  <DetailRow label="Episode">{event.episodeId ?? "—"}</DetailRow>
                </>
              )}
            </dl>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Delivery summary</CardTitle>
            <CardDescription>
              Detection and delivery are separate outcomes.
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <Badge variant={deliverySummaryTone(event.deliverySummary)}>
              {deliverySummaryText(event.deliverySummary)}
            </Badge>
            <dl className="space-y-2.5 text-sm">
              <div className="flex items-center justify-between gap-2">
                <dt className="text-muted-foreground">Attempted</dt>
                <dd className="font-mono tabular-nums">
                  {event.deliverySummary.attempted}
                </dd>
              </div>
              <div className="flex items-center justify-between gap-2">
                <dt className="text-muted-foreground">Sent</dt>
                <dd className="font-mono tabular-nums">
                  {event.deliverySummary.sent}
                </dd>
              </div>
              <div className="flex items-center justify-between gap-2">
                <dt className="text-muted-foreground">Pending</dt>
                <dd className="font-mono tabular-nums">
                  {event.deliverySummary.pending +
                    event.deliverySummary.retryScheduled}
                </dd>
              </div>
              <div className="flex items-center justify-between gap-2">
                <dt className="text-muted-foreground">Failed</dt>
                <dd className="font-mono tabular-nums">
                  {event.deliverySummary.failed}
                </dd>
              </div>
            </dl>
            <p className="border-t pt-3 font-mono text-[0.65rem] break-all text-muted-foreground">
              Event ID {event.id}
            </p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div>
              <CardTitle>Notification deliveries</CardTitle>
              <CardDescription>
                Delivery status is tracked per channel. Detection and delivery
                are separate outcomes.
              </CardDescription>
            </div>
            <Badge variant={deliverySummaryTone(event.deliverySummary)}>
              {deliverySummaryText(event.deliverySummary)}
            </Badge>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {!fullyDelivered && event.deliverySummary.total > 0 ? (
            <Alert variant="warning">
              <AlertTriangle aria-hidden="true" />
              <AlertTitle>Not fully delivered</AlertTitle>
              <AlertDescription>
                At least one notification is queued, retrying, failed, or
                skipped. Those states are never reported as delivered.
              </AlertDescription>
            </Alert>
          ) : null}

          {event.deliveries.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No notification deliveries were created for this event.
            </p>
          ) : (
            <DeliveryTable deliveries={event.deliveries} />
          )}

          <div className="flex flex-wrap items-center gap-2 border-t pt-4">
            <Button asChild variant="outline" size="sm">
              <Link
                to={`/events?tab=deliveries&eventId=${encodeURIComponent(event.id)}`}
              >
                View deliveries for this event
              </Link>
            </Button>
            {event.kind === "availability" ? (
              <p className="text-xs text-muted-foreground">
                Telegram and Push channels are delivered independently.
              </p>
            ) : null}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
