import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AlertTriangle,
  ArrowDown,
  FilterX,
  History,
  Loader2,
  RefreshCw,
  Send,
} from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import { deliveriesApi, eventsApi, monitorsApi, providersApi } from "@/api/endpoints";
import { isApiError, type ApiError } from "@/api/client";
import type {
  DeliveryChannel,
  DeliveryRecord,
  DeliveryStatus,
  EventKind,
  EventRecord,
  Monitor,
  ProviderInfo,
} from "@/api/types";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { PageHeader } from "@/components/layout/PageHeader";
import { EventListItem } from "@/components/events/EventListItem";
import {
  DeliveryChannelLabel,
  DeliveryStatusBadge,
  EventKindBadge,
} from "@/components/status";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Card, CardContent } from "@/components/ui/card";
import { NativeSelect } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
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
import { formatTehranTimestamp } from "@/lib/dates";

const PAGE_SIZE = 25;

const EVENT_KINDS: Array<{ value: "" | EventKind; label: string }> = [
  { value: "", label: "All event types" },
  { value: "availability", label: "Availability" },
  { value: "provider_degraded", label: "Provider degraded" },
  { value: "provider_recovered", label: "Provider recovered" },
];

const DELIVERY_STATUSES: Array<{ value: "" | DeliveryStatus; label: string }> = [
  { value: "", label: "All delivery statuses" },
  { value: "pending", label: "Queued" },
  { value: "sending", label: "Sending" },
  { value: "sent", label: "Sent" },
  { value: "retry_scheduled", label: "Retry scheduled" },
  { value: "failed", label: "Failed" },
  { value: "skipped", label: "Skipped" },
];

const CHANNELS: Array<{ value: "" | DeliveryChannel; label: string }> = [
  { value: "", label: "All channels" },
  { value: "telegram", label: "Telegram" },
  { value: "push", label: "Push" },
];

function FilterLabel({
  htmlFor,
  children,
}: {
  htmlFor: string;
  children: React.ReactNode;
}) {
  return (
    <label htmlFor={htmlFor} className="mb-1 block text-xs font-medium">
      {children}
    </label>
  );
}

function DeliveryHistoryTable({ deliveries }: { deliveries: DeliveryRecord[] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Status</TableHead>
          <TableHead>Channel</TableHead>
          <TableHead>Event</TableHead>
          <TableHead>Event time</TableHead>
          <TableHead>Attempts</TableHead>
          <TableHead>Sent</TableHead>
          <TableHead>Next attempt</TableHead>
          <TableHead>Error</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {deliveries.map((delivery) => (
          <TableRow key={delivery.id}>
            <TableCell>
              <DeliveryStatusBadge status={delivery.status} />
            </TableCell>
            <TableCell>
              <DeliveryChannelLabel channel={delivery.channel} />
            </TableCell>
            <TableCell>
              <Link
                to={`/events/${encodeURIComponent(delivery.eventId)}`}
                className="inline-flex flex-col gap-1 font-medium text-primary underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
              >
                <EventKindBadge kind={delivery.eventKind} />
                <span className="font-mono text-[0.7rem] text-muted-foreground">
                  {delivery.eventId}
                </span>
              </Link>
            </TableCell>
            <TableCell className="min-w-40 whitespace-nowrap text-muted-foreground">
              <time dateTime={delivery.eventOccurredAt}>
                {formatTehranTimestamp(delivery.eventOccurredAt)}
              </time>
            </TableCell>
            <TableCell className="tabular-nums">
              {delivery.attemptCount}
            </TableCell>
            <TableCell className="whitespace-nowrap text-muted-foreground">
              {formatTehranTimestamp(delivery.sentAt)}
            </TableCell>
            <TableCell className="whitespace-nowrap text-muted-foreground">
              {formatTehranTimestamp(delivery.nextAttemptAt)}
            </TableCell>
            <TableCell className="text-muted-foreground">
              {delivery.lastErrorKind ? (
                <span className="font-mono text-xs">
                  {delivery.lastErrorKind}
                </span>
              ) : (
                "—"
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function HistoryPage() {
  useDocumentTitle("History");
  const [searchParams, setSearchParams] = useSearchParams();
  const tab = searchParams.get("tab") === "deliveries" ? "deliveries" : "events";

  const kind = searchParams.get("kind") ?? "";
  const monitorId = searchParams.get("monitorId") ?? "";
  const providerId = searchParams.get("providerId") ?? "";
  const channel = searchParams.get("channel") ?? "";
  const status = searchParams.get("status") ?? "";
  const deliveryKind = searchParams.get("deliveryKind") ?? "";
  const eventIdFilter = searchParams.get("eventId") ?? "";

  const optionsLoader = useCallback(
    async (
      signal: AbortSignal,
    ): Promise<{ monitors: Monitor[]; providers: ProviderInfo[] }> => {
      const [monitorList, providerList] = await Promise.all([
        monitorsApi.list(signal),
        providersApi.list(signal),
      ]);
      return {
        monitors: monitorList.monitors,
        providers: providerList.providers,
      };
    },
    [],
  );
  const options = useApiResource(optionsLoader);
  const monitorMap = useMemo(
    () => new Map(options.data?.monitors.map((monitor) => [monitor.id, monitor]) ?? []),
    [options.data?.monitors],
  );
  const providerMap = useMemo(
    () =>
      new Map(
        options.data?.providers.map((provider) => [provider.id, provider]) ?? [],
      ),
    [options.data?.providers],
  );

  const [events, setEvents] = useState<EventRecord[]>([]);
  const [eventsCursor, setEventsCursor] = useState("");
  const [eventsLoading, setEventsLoading] = useState(true);
  const [eventsLoadingMore, setEventsLoadingMore] = useState(false);
  const [eventsError, setEventsError] = useState<ApiError | null>(null);
  const [eventsRefreshKey, setEventsRefreshKey] = useState(0);

  const [deliveries, setDeliveries] = useState<DeliveryRecord[]>([]);
  const [deliveriesCursor, setDeliveriesCursor] = useState("");
  const [deliveriesLoading, setDeliveriesLoading] = useState(true);
  const [deliveriesLoadingMore, setDeliveriesLoadingMore] = useState(false);
  const [deliveriesError, setDeliveriesError] = useState<ApiError | null>(null);
  const [deliveriesRefreshKey, setDeliveriesRefreshKey] = useState(0);

  useEffect(() => {
    if (tab !== "events") return;
    const controller = new AbortController();
    setEventsLoading(true);
    setEventsError(null);
    eventsApi
      .list(
        {
          kind: kind as EventKind | "",
          monitorId,
          providerId,
          limit: PAGE_SIZE,
        },
        controller.signal,
      )
      .then((page) => {
        setEvents(page.events);
        setEventsCursor(page.nextCursor);
        setEventsLoading(false);
      })
      .catch((caught: unknown) => {
        if (caught instanceof DOMException && caught.name === "AbortError") return;
        setEventsError(isApiError(caught) ? caught : null);
        setEventsLoading(false);
      });
    return () => controller.abort();
  }, [tab, kind, monitorId, providerId, eventsRefreshKey]);

  useEffect(() => {
    if (tab !== "deliveries") return;
    const controller = new AbortController();
    setDeliveriesLoading(true);
    setDeliveriesError(null);
    deliveriesApi
      .list(
        {
          channel: channel as DeliveryChannel | "",
          status: status as DeliveryStatus | "",
          kind: deliveryKind as EventKind | "",
          eventId: eventIdFilter,
          limit: PAGE_SIZE,
        },
        controller.signal,
      )
      .then((page) => {
        setDeliveries(page.deliveries);
        setDeliveriesCursor(page.nextCursor);
        setDeliveriesLoading(false);
      })
      .catch((caught: unknown) => {
        if (caught instanceof DOMException && caught.name === "AbortError") return;
        setDeliveriesError(isApiError(caught) ? caught : null);
        setDeliveriesLoading(false);
      });
    return () => controller.abort();
  }, [tab, channel, status, deliveryKind, eventIdFilter, deliveriesRefreshKey]);

  async function loadMoreEvents() {
    if (!eventsCursor || eventsLoadingMore) return;
    setEventsLoadingMore(true);
    try {
      const page = await eventsApi.list({
        kind: kind as EventKind | "",
        monitorId,
        providerId,
        limit: PAGE_SIZE,
        cursor: eventsCursor,
      });
      setEvents((current) => [...current, ...page.events]);
      setEventsCursor(page.nextCursor);
    } catch (caught) {
      setEventsError(isApiError(caught) ? caught : null);
    } finally {
      setEventsLoadingMore(false);
    }
  }

  async function loadMoreDeliveries() {
    if (!deliveriesCursor || deliveriesLoadingMore) return;
    setDeliveriesLoadingMore(true);
    try {
      const page = await deliveriesApi.list({
        channel: channel as DeliveryChannel | "",
        status: status as DeliveryStatus | "",
        kind: deliveryKind as EventKind | "",
        eventId: eventIdFilter,
        limit: PAGE_SIZE,
        cursor: deliveriesCursor,
      });
      setDeliveries((current) => [...current, ...page.deliveries]);
      setDeliveriesCursor(page.nextCursor);
    } catch (caught) {
      setDeliveriesError(isApiError(caught) ? caught : null);
    } finally {
      setDeliveriesLoadingMore(false);
    }
  }

  function updateParam(key: string, value: string) {
    const params = new URLSearchParams(searchParams);
    if (value) params.set(key, value);
    else params.delete(key);
    setSearchParams(params, { replace: true });
  }

  function switchTab(next: string) {
    updateParam("tab", next === "events" ? "" : next);
  }

  function clearEventsFilters() {
    setSearchParams({}, { replace: true });
  }

  function clearDeliveryFilters() {
    const params = new URLSearchParams(searchParams);
    for (const key of ["channel", "status", "deliveryKind", "eventId"]) {
      params.delete(key);
    }
    setSearchParams(params, { replace: true });
  }

  const eventsHaveFilters = Boolean(kind || monitorId || providerId);
  const deliveriesHaveFilters = Boolean(
    channel || status || deliveryKind || eventIdFilter,
  );

  return (
    <div className="space-y-6">
      <PageHeader
        title="History"
        description="Durable availability and provider-health events, plus per-channel notification delivery status. Historical events are not current availability."
        actions={
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              if (tab === "events") setEventsRefreshKey((key) => key + 1);
              else setDeliveriesRefreshKey((key) => key + 1);
            }}
            disabled={tab === "events" ? eventsLoading : deliveriesLoading}
          >
            <RefreshCw
              className={
                (tab === "events" ? eventsLoading : deliveriesLoading)
                  ? "animate-spin"
                  : undefined
              }
              aria-hidden="true"
            />
            Refresh
          </Button>
        }
      />

      <Tabs value={tab} onValueChange={switchTab}>
        <TabsList aria-label="History sections">
          <TabsTrigger value="events">
            <History aria-hidden="true" />
            Events
          </TabsTrigger>
          <TabsTrigger value="deliveries">
            <Send aria-hidden="true" />
            Deliveries
          </TabsTrigger>
        </TabsList>

      {tab === "events" ? (
        <TabsContent value="events" className="space-y-4">
          <Card>
            <CardContent className="grid gap-3 py-4 sm:grid-cols-3">
              <div>
                <FilterLabel htmlFor="event-kind">Event type</FilterLabel>
                <NativeSelect
                  id="event-kind"
                  value={kind}
                  onChange={(event) => updateParam("kind", event.target.value)}
                >
                  {EVENT_KINDS.map((option) => (
                    <option key={option.value || "all"} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </NativeSelect>
              </div>
              <div>
                <FilterLabel htmlFor="event-monitor">Monitor</FilterLabel>
                <NativeSelect
                  id="event-monitor"
                  value={monitorId}
                  onChange={(event) =>
                    updateParam("monitorId", event.target.value)
                  }
                  disabled={options.loading || options.error !== null}
                >
                  <option value="">All monitors</option>
                  {options.data?.monitors.map((monitor) => (
                    <option key={monitor.id} value={monitor.id}>
                      {monitor.label}
                    </option>
                  ))}
                </NativeSelect>
              </div>
              <div>
                <FilterLabel htmlFor="event-provider">Provider</FilterLabel>
                <NativeSelect
                  id="event-provider"
                  value={providerId}
                  onChange={(event) =>
                    updateParam("providerId", event.target.value)
                  }
                  disabled={options.loading || options.error !== null}
                >
                  <option value="">All providers</option>
                  {options.data?.providers.map((provider) => (
                    <option key={provider.id} value={provider.id}>
                      {provider.displayName}
                    </option>
                  ))}
                </NativeSelect>
              </div>
              {eventsHaveFilters ? (
                <div className="sm:col-span-3">
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={clearEventsFilters}
                  >
                    <FilterX aria-hidden="true" />
                    Clear filters
                  </Button>
                </div>
              ) : null}
            </CardContent>
          </Card>

          {eventsLoading ? (
            <LoadingState rows={3} label="Loading events" />
          ) : eventsError ? (
            <ErrorState
              error={eventsError}
              title="Could not load events"
              onRetry={() => setEventsRefreshKey((key) => key + 1)}
            />
          ) : events.length === 0 ? (
            <EmptyState
              icon={History}
              title={
                eventsHaveFilters
                  ? "No events match these filters"
                  : "No events yet"
              }
              description={
                eventsHaveFilters
                  ? "Adjust the event type, monitor, or provider filter."
                  : "Availability transitions and provider-health events will appear here."
              }
              action={
                eventsHaveFilters ? (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={clearEventsFilters}
                  >
                    Clear filters
                  </Button>
                ) : undefined
              }
            />
          ) : (
            <div className="space-y-3" aria-live="polite">
              {events.map((event) => (
                <div key={event.id} className="space-y-1">
                  <EventListItem event={event} />
                  {event.monitorId ? (
                    <p className="px-1 text-xs text-muted-foreground">
                      Monitor:{" "}
                      <Link
                        to={`/monitors/${encodeURIComponent(event.monitorId)}`}
                        className="text-primary underline underline-offset-4 hover:text-primary/80"
                      >
                        {monitorMap.get(event.monitorId)?.label ??
                          event.monitorLabel ??
                          event.monitorId}
                      </Link>
                      {event.providerId ? (
                        <>
                          {" "}
                          · Provider:{" "}
                          {providerMap.get(event.providerId)?.displayName ??
                            event.providerId}
                        </>
                      ) : null}
                    </p>
                  ) : null}
                </div>
              ))}
              <p className="text-xs text-muted-foreground">
                Showing {events.length} loaded event
                {events.length === 1 ? "" : "s"}
                {eventsCursor ? " · more available" : ""}.
              </p>
              {eventsCursor ? (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => void loadMoreEvents()}
                  disabled={eventsLoadingMore}
                >
                  {eventsLoadingMore ? (
                    <Loader2 className="animate-spin" aria-hidden="true" />
                  ) : (
                    <ArrowDown aria-hidden="true" />
                  )}
                  {eventsLoadingMore ? "Loading…" : "Load more events"}
                </Button>
              ) : null}
            </div>
          )}
        </TabsContent>
      ) : (
        <TabsContent value="deliveries" className="space-y-4">
          <Card>
            <CardContent className="grid gap-3 py-4 sm:grid-cols-4">
              <div>
                <FilterLabel htmlFor="delivery-channel">Channel</FilterLabel>
                <NativeSelect
                  id="delivery-channel"
                  value={channel}
                  onChange={(event) => updateParam("channel", event.target.value)}
                >
                  {CHANNELS.map((option) => (
                    <option key={option.value || "all"} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </NativeSelect>
              </div>
              <div>
                <FilterLabel htmlFor="delivery-status">Status</FilterLabel>
                <NativeSelect
                  id="delivery-status"
                  value={status}
                  onChange={(event) => updateParam("status", event.target.value)}
                >
                  {DELIVERY_STATUSES.map((option) => (
                    <option key={option.value || "all"} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </NativeSelect>
              </div>
              <div>
                <FilterLabel htmlFor="delivery-kind">Event type</FilterLabel>
                <NativeSelect
                  id="delivery-kind"
                  value={deliveryKind}
                  onChange={(event) =>
                    updateParam("deliveryKind", event.target.value)
                  }
                >
                  {EVENT_KINDS.map((option) => (
                    <option key={option.value || "all"} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </NativeSelect>
              </div>
              <div className="flex items-end">
                {deliveriesHaveFilters ? (
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={clearDeliveryFilters}
                  >
                    <FilterX aria-hidden="true" />
                    Clear filters
                  </Button>
                ) : null}
              </div>
              {eventIdFilter ? (
                <div className="sm:col-span-4">
                  <Badge variant="outline" className="gap-2">
                    Filtered to event{" "}
                    <code className="font-mono">{eventIdFilter}</code>
                    <button
                      type="button"
                      className="underline underline-offset-2"
                      onClick={() => updateParam("eventId", "")}
                    >
                      Clear
                    </button>
                  </Badge>
                </div>
              ) : null}
            </CardContent>
          </Card>

          <Alert variant="info">
            <AlertTriangle aria-hidden="true" />
            <AlertTitle>Delivery status is not detection</AlertTitle>
            <AlertDescription>
              Queued, sending, retry-scheduled, failed, and skipped deliveries
              are not delivered notifications. Only “Sent” means the channel
              accepted the message.
            </AlertDescription>
          </Alert>

          {deliveriesLoading ? (
            <LoadingState rows={3} label="Loading deliveries" />
          ) : deliveriesError ? (
            <ErrorState
              error={deliveriesError}
              title="Could not load deliveries"
              onRetry={() => setDeliveriesRefreshKey((key) => key + 1)}
            />
          ) : deliveries.length === 0 ? (
            <EmptyState
              icon={Send}
              title={
                deliveriesHaveFilters
                  ? "No deliveries match these filters"
                  : "No deliveries yet"
              }
              description={
                deliveriesHaveFilters
                  ? "Adjust the channel, status, or event type filter."
                  : "Notification deliveries appear here after events are detected and queued."
              }
              action={
                deliveriesHaveFilters ? (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={clearDeliveryFilters}
                  >
                    Clear filters
                  </Button>
                ) : undefined
              }
            />
          ) : (
            <div className="space-y-3">
              <Card>
                <CardContent className="py-2">
                  <DeliveryHistoryTable deliveries={deliveries} />
                </CardContent>
              </Card>
              <p className="text-xs text-muted-foreground">
                Showing {deliveries.length} loaded deliver
                {deliveries.length === 1 ? "y" : "ies"}
                {deliveriesCursor ? " · more available" : ""}.
              </p>
              {deliveriesCursor ? (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => void loadMoreDeliveries()}
                  disabled={deliveriesLoadingMore}
                >
                  {deliveriesLoadingMore ? (
                    <Loader2 className="animate-spin" aria-hidden="true" />
                  ) : (
                    <ArrowDown aria-hidden="true" />
                  )}
                  {deliveriesLoadingMore ? "Loading…" : "Load more deliveries"}
                </Button>
              ) : null}
            </div>
          )}
        </TabsContent>
      )}
      </Tabs>
    </div>
  );
}
