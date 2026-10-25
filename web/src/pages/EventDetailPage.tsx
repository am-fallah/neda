import { useCallback } from "react";
import { AlertTriangle, ArrowLeft, History } from "lucide-react";
import { Link, useParams } from "react-router-dom";
import { eventsApi, providersApi } from "@/api/endpoints";
import type { EventRecord, ProviderInfo } from "@/api/types";
import { BookingLink } from "@/components/BookingLink";
import { ProviderPrice } from "@/components/ProviderPrice";
import { DeliveryTable } from "@/components/events/DeliveryTable";
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
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 text-sm">{children}</dd>
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
        <PageHeader title="Event detail" description="Loading event…" />
        <LoadingState rows={3} label="Loading event" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="space-y-6">
        <PageHeader title="Event detail" />
        <ErrorState
          error={error}
          title={error.status === 404 ? "Event not found" : "Could not load the event"}
          onRetry={refresh}
        />
        <Button asChild variant="outline">
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
    <div className="space-y-6">
      <PageHeader
        eyebrow="History"
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

      <Card>
        <CardHeader>
          <CardTitle>Event details</CardTitle>
          <CardDescription>
            Event ID <code className="font-mono">{event.id}</code>
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
                    className="text-primary underline underline-offset-4 hover:text-primary/80"
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
                  {event.originName || event.destinationName
                    ? `${event.originName ?? "?"} → ${event.destinationName ?? "?"}`
                    : "—"}
                </DetailRow>
                <DetailRow label="Train">
                  {event.trainNumber || "Unknown"}
                  {event.operator ? ` · ${event.operator}` : ""}
                </DetailRow>
                <DetailRow label="Class">
                  {[event.wagonName, event.className]
                    .filter(Boolean)
                    .join(" · ") || "Unknown"}
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
                <DetailRow label="Episode">
                  {event.episodeId ?? "—"}
                </DetailRow>
              </>
            )}
          </dl>
        </CardContent>
      </Card>

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

          <div className="flex flex-wrap gap-2 border-t pt-4">
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
