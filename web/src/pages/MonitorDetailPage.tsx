import { useCallback, useState } from "react";
import {
  AlertTriangle,
  ArrowLeft,
  Clock3,
  Info,
  Pause,
  Play,
  RefreshCw,
  Trash2,
} from "lucide-react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { monitorsApi, providersApi } from "@/api/endpoints";
import { isApiError, type ApiError } from "@/api/client";
import type {
  CheckRun,
  InventoryObservation,
  MonitorDetail,
  MonitorProviderDetail,
  ProviderInfo,
} from "@/api/types";
import { BookingLink } from "@/components/BookingLink";
import { ProviderPrice } from "@/components/ProviderPrice";
import { DateDisplay } from "@/components/JalaliDatePicker";
import { Breadcrumbs } from "@/components/layout/Breadcrumbs";
import { EmptyState } from "@/components/layout/EmptyState";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { PageHeader } from "@/components/layout/PageHeader";
import {
  CheckOutcomeBadge,
  MonitorStatusBadge,
  ObservationStateBadge,
  TestOnlyBadge,
} from "@/components/status";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
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
import { useMediaQuery } from "@/hooks/useMediaQuery";
import {
  formatDurationMs,
  formatElapsed,
  formatTehranTimestamp,
} from "@/lib/dates";
import {
  groupMonitorSelections,
  monitorSelectionMeta,
  monitorSelectionTitle,
} from "@/lib/searchResults";
import {
  latestCheck,
  summarizeMonitorChecks,
  summarizeObservations,
  type Tone,
} from "@/lib/monitorSummary";
import { cn } from "@/lib/utils";

interface DetailData {
  monitor: MonitorDetail;
  providers: ProviderInfo[];
}

function alertVariantForTone(
  tone: Tone,
): "success" | "warning" | "destructive" | "info" | "default" {
  if (tone === "muted") return "default";
  return tone;
}

function ProviderTarget({
  target,
  provider,
}: {
  target: MonitorProviderDetail;
  provider?: ProviderInfo;
}) {
  const check = latestCheck(target.recentChecks);
  const summary = summarizeObservations(target.observations, check);
  const summaryVariant = alertVariantForTone(summary.tone);

  return (
    <Card className="gap-0 overflow-hidden py-0">
      <CardHeader className="border-b bg-surface-raised/55 px-4 py-3 sm:px-5">
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div className="min-w-0">
            <CardTitle className="flex flex-wrap items-center gap-2">
              {provider?.displayName ?? target.providerId}
              {provider?.testOnly ? <TestOnlyBadge /> : null}
              <Badge variant="outline">
                {target.active ? "Active target" : "Removed target"}
              </Badge>
            </CardTitle>
            <CardDescription className="mt-1.5 flex flex-wrap items-center gap-1.5 font-mono text-[0.65rem]">
              <span className="rounded bg-muted px-1.5 py-0.5">
                Provider {target.providerId}
              </span>
              <span className="rounded bg-muted px-1.5 py-0.5">
                Config v{target.configVersion}
              </span>
              {target.lastAttemptAt ? (
                <span className="text-muted-foreground">
                  Last attempt {formatTehranTimestamp(target.lastAttemptAt)}
                </span>
              ) : null}
            </CardDescription>
          </div>
          <div className="flex flex-wrap gap-1.5">
            {target.lastOutcome ? (
              <Badge variant="outline">
                Last outcome: {target.lastOutcome}
              </Badge>
            ) : (
              <Badge variant="muted">No outcome yet</Badge>
            )}
            {target.consecutiveFailures > 0 ? (
              <Badge variant="warning">
                {target.consecutiveFailures} consecutive failure
                {target.consecutiveFailures === 1 ? "" : "s"}
              </Badge>
            ) : null}
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-5 px-4 py-4 sm:px-5">
        {!target.active ? (
          <Alert>
            <AlertTriangle aria-hidden="true" />
            <AlertTitle>This provider was removed from the monitor</AlertTitle>
            <AlertDescription>
              Historical events are retained, but this target is no longer
              checked and no current observations are shown.
            </AlertDescription>
          </Alert>
        ) : (
          <>
            <Alert variant={summaryVariant}>
              {summary.tone === "info" ? (
                <Info aria-hidden="true" />
              ) : (
                <AlertTriangle
                  className={cn(
                    summary.tone === "success" && "hidden",
                    summary.tone === "muted" && "hidden",
                  )}
                  aria-hidden="true"
                />
              )}
              <AlertTitle>{summary.title}</AlertTitle>
              <AlertDescription>{summary.detail}</AlertDescription>
            </Alert>

            <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs text-muted-foreground sm:grid-cols-3">
              <div>
                <dt className="font-medium text-foreground">Last attempt</dt>
                <dd>{formatTehranTimestamp(target.lastAttemptAt)}</dd>
              </div>
              <div>
                <dt className="font-medium text-foreground">Last success</dt>
                <dd>{formatTehranTimestamp(target.lastSuccessAt)}</dd>
              </div>
              <div>
                <dt className="font-medium text-foreground">Next check</dt>
                <dd>{formatTehranTimestamp(target.nextDueAt)}</dd>
              </div>
              <div>
                <dt className="font-medium text-foreground">Last error</dt>
                <dd className="font-mono">
                  {target.lastErrorKind || "—"}
                  {target.lastErrorAt ? (
                    <span className="block font-sans">
                      {formatTehranTimestamp(target.lastErrorAt)}
                    </span>
                  ) : null}
                </dd>
              </div>
              <div>
                <dt className="font-medium text-foreground">Booking slug</dt>
                <dd className="font-mono">
                  {target.bookingSlug ? "configured" : "not configured"}
                </dd>
              </div>
              <div>
                <dt className="font-medium text-foreground">Updated</dt>
                <dd>{formatTehranTimestamp(target.updatedAt)}</dd>
              </div>
            </dl>
          </>
        )}

        {target.active && target.selections.length > 0 ? (
          <section aria-labelledby={`selections-${target.providerId}`}>
            <h3
              id={`selections-${target.providerId}`}
              className="mb-2 text-sm font-semibold"
            >
              Selected options
            </h3>
            <ul className="flex flex-col divide-y overflow-hidden rounded-lg border bg-card text-sm">
              {groupMonitorSelections(target.selections).map((group) => {
                const first = group.selections[0];
                const meta = monitorSelectionMeta(group);
                return (
                  <li
                    key={group.key}
                    className="flex flex-wrap items-center gap-x-2 gap-y-1 px-3 py-2.5"
                  >
                    <span className="font-medium">
                      Train{" "}
                      {group.trainNumber || first.journeyNativeId || "unknown"}
                    </span>
                    <span className="text-muted-foreground">
                      {monitorSelectionTitle(group)}
                    </span>
                    {meta ? (
                      <span className="text-xs text-muted-foreground">
                        {meta}
                      </span>
                    ) : null}
                    <span className="ml-auto font-mono text-[0.65rem] text-muted-foreground">
                      {group.selections.length} fare option
                      {group.selections.length === 1 ? "" : "s"}
                    </span>
                    {first.departureAt ? (
                      <time
                        dateTime={first.departureAt}
                        className="w-full font-mono text-[0.65rem] text-muted-foreground"
                      >
                        {formatTehranTimestamp(first.departureAt)}
                      </time>
                    ) : null}
                  </li>
                );
              })}
            </ul>
          </section>
        ) : null}

        {target.active ? (
          <section aria-labelledby={`observations-${target.providerId}`}>
            <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
              <h3
                id={`observations-${target.providerId}`}
                className="text-sm font-semibold"
              >
                Last observed inventory
              </h3>
              <span className="text-xs text-muted-foreground">
                {target.observations.length} offering
                {target.observations.length === 1 ? "" : "s"}
                {target.observationsTruncated ? " (truncated)" : ""}
              </span>
            </div>
            <p className="mb-3 text-xs text-muted-foreground">
              These are saved observations from completed checks, not a live
              guarantee. A missing or unknown offering is never treated as
              unavailable.
            </p>
            {target.observations.length === 0 ? (
              <EmptyState
                icon={Clock3}
                title="No inventory observations"
                description={
                  check?.outcome === "empty"
                    ? "The latest successful check returned no offerings. That is not proof that all tickets are sold out."
                    : check?.outcome === "success"
                      ? "The latest successful check returned no inventory offerings."
                      : "No successful observation is available for this provider yet."
                }
                className="py-8"
              />
            ) : (
              <InventoryTable observations={target.observations} />
            )}
          </section>
        ) : null}

        <section aria-labelledby={`checks-${target.providerId}`}>
          <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
            <h3
              id={`checks-${target.providerId}`}
              className="text-sm font-semibold"
            >
              Recent checks
            </h3>
            <span className="text-xs text-muted-foreground">
              Newest first{target.recentChecksTruncated ? " · truncated" : ""}
            </span>
          </div>
          {target.recentChecks.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No check attempts recorded yet.
            </p>
          ) : (
            <CheckRunTable checks={target.recentChecks} />
          )}
        </section>
      </CardContent>
    </Card>
  );
}

function InventoryCards({
  observations,
}: {
  observations: InventoryObservation[];
}) {
  return (
    <ul className="space-y-2" aria-label="Observed inventory">
      {observations.map((observation) => (
        <li
          key={`${observation.journeyKey}:${observation.offeringKey}`}
          className="rounded-lg border bg-surface-raised p-3.5"
        >
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div className="min-w-0">
              <p className="font-medium">
                {observation.trainNumber
                  ? `Train ${observation.trainNumber}`
                  : "Train unknown"}
              </p>
              <p className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-1 text-xs text-muted-foreground">
                <span>{observation.operator || "Operator unknown"}</span>
                <span aria-hidden="true">·</span>
                <bdi dir="auto" className="min-w-0 break-words">
                  {observation.originName || "?"}
                </bdi>
                <span aria-hidden="true">→</span>
                <bdi dir="auto" className="min-w-0 break-words">
                  {observation.destinationName || "?"}
                </bdi>
              </p>
            </div>
            <ObservationStateBadge
              state={observation.state}
              missing={observation.missing}
            />
          </div>

          <p className="mt-2 flex flex-wrap items-center gap-x-1.5 text-sm">
            <bdi dir="auto">{observation.wagonName || "Wagon unknown"}</bdi>
            <span aria-hidden="true" className="text-muted-foreground">
              ·
            </span>
            <bdi dir="auto" className="text-muted-foreground">
              {observation.className || "Class unknown"}
            </bdi>
          </p>

          <dl className="mt-3 grid grid-cols-2 gap-x-3 gap-y-2 border-t pt-3 text-xs">
            <div className="min-w-0">
              <dt className="eyebrow text-[0.58rem]">Departure</dt>
              <dd className="mt-0.5">
                {observation.departureAt ? (
                  <time dateTime={observation.departureAt}>
                    {formatTehranTimestamp(observation.departureAt)}
                  </time>
                ) : (
                  "Time unavailable"
                )}
                {observation.arrivalAt ? (
                  <span className="mt-0.5 block text-muted-foreground">
                    Arrives {formatTehranTimestamp(observation.arrivalAt)}
                  </span>
                ) : null}
              </dd>
            </div>
            <div className="min-w-0">
              <dt className="eyebrow text-[0.58rem]">Price</dt>
              <dd className="mt-0.5">
                {observation.priceAmount !== null &&
                observation.priceAmount !== undefined ? (
                  <ProviderPrice
                    amount={observation.priceAmount}
                    currency={observation.priceCurrency}
                    emptyLabel="—"
                  />
                ) : (
                  "—"
                )}
              </dd>
            </div>
            <div className="min-w-0">
              <dt className="eyebrow text-[0.58rem]">Observed</dt>
              <dd className="mt-0.5">
                <time dateTime={observation.lastObservedAt}>
                  {formatTehranTimestamp(observation.lastObservedAt)}
                </time>
                <span className="mt-0.5 block text-muted-foreground">
                  {formatElapsed(observation.lastObservedAt)}
                </span>
              </dd>
            </div>
            <div className="min-w-0">
              <dt className="eyebrow text-[0.58rem]">Booking</dt>
              <dd className="mt-0.5">
                <BookingLink url={observation.bookingUrl} compact />
              </dd>
            </div>
            {observation.lastCapacity !== null &&
            observation.lastCapacity !== undefined ? (
              <div>
                <dt className="eyebrow text-[0.58rem]">Capacity</dt>
                <dd className="mt-0.5 font-mono tabular-nums">
                  {observation.lastCapacity}
                </dd>
              </div>
            ) : null}
          </dl>

          {observation.lastReason ? (
            <p className="mt-2 font-mono text-[0.65rem] break-all text-muted-foreground">
              {observation.lastReason}
            </p>
          ) : null}
          {observation.missing ? (
            <p className="mt-1 text-[0.7rem] leading-relaxed text-warning-foreground dark:text-warning">
              Not returned by the latest check; availability unknown.
            </p>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

function InventoryTable({
  observations,
}: {
  observations: InventoryObservation[];
}) {
  const wide = useMediaQuery("(min-width: 1280px)");
  return (
    <div className="space-y-2">
      {wide ? (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>State</TableHead>
              <TableHead>Journey</TableHead>
              <TableHead>Offering</TableHead>
              <TableHead>Departure</TableHead>
              <TableHead>Price</TableHead>
              <TableHead>Observed</TableHead>
              <TableHead>Booking</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {observations.map((observation) => (
              <TableRow key={`${observation.journeyKey}:${observation.offeringKey}`}>
                <TableCell>
                  <div className="space-y-1">
                    <ObservationStateBadge
                      state={observation.state}
                      missing={observation.missing}
                    />
                    {observation.lastReason ? (
                      <p className="font-mono text-[0.7rem] text-muted-foreground">
                        {observation.lastReason}
                      </p>
                    ) : null}
                    {observation.missing ? (
                      <p className="max-w-44 text-[0.7rem] text-muted-foreground">
                        Not returned by the latest check; availability unknown.
                      </p>
                    ) : null}
                  </div>
                </TableCell>
                <TableCell>
                  <p className="font-medium">
                    {observation.trainNumber
                      ? `Train ${observation.trainNumber}`
                      : "Train unknown"}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {[observation.operator, observation.originName, observation.destinationName]
                      .filter(Boolean)
                      .join(" · ")}
                  </p>
                  {observation.lastCapacity !== null &&
                  observation.lastCapacity !== undefined ? (
                    <p className="text-xs text-muted-foreground">
                      Capacity {observation.lastCapacity}
                    </p>
                  ) : null}
                </TableCell>
                <TableCell>
                  <p>{observation.wagonName || "Wagon unknown"}</p>
                  <p className="text-xs text-muted-foreground">
                    {observation.className || "Class unknown"}
                  </p>
                </TableCell>
                <TableCell className="min-w-44">
                  {observation.departureAt ? (
                    <time dateTime={observation.departureAt}>
                      {formatTehranTimestamp(observation.departureAt)}
                    </time>
                  ) : (
                    <span className="text-muted-foreground">
                      Time unavailable
                    </span>
                  )}
                  {observation.arrivalAt ? (
                    <p className="text-xs text-muted-foreground">
                      Arrives {formatTehranTimestamp(observation.arrivalAt)}
                    </p>
                  ) : null}
                </TableCell>
                <TableCell className="whitespace-nowrap">
                  {observation.priceAmount !== null &&
                  observation.priceAmount !== undefined ? (
                    <ProviderPrice
                      amount={observation.priceAmount}
                      currency={observation.priceCurrency}
                      emptyLabel="—"
                    />
                  ) : (
                    "—"
                  )}
                </TableCell>
                <TableCell className="min-w-40">
                  <time dateTime={observation.lastObservedAt}>
                    {formatTehranTimestamp(observation.lastObservedAt)}
                  </time>
                  <p className="text-xs text-muted-foreground">
                    {formatElapsed(observation.lastObservedAt)}
                  </p>
                </TableCell>
                <TableCell>
                  <BookingLink url={observation.bookingUrl} compact />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <InventoryCards observations={observations} />
      )}
      <details className="rounded-md border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
        <summary className="cursor-pointer font-medium">
          Diagnostic identifiers
        </summary>
        <ul className="mt-2 space-y-1 font-mono">
          {observations.map((observation) => (
            <li key={`diag-${observation.journeyKey}:${observation.offeringKey}`}>
              journey={observation.journeyKey} offering={observation.offeringKey}
            </li>
          ))}
        </ul>
      </details>
    </div>
  );
}

function CheckRunCards({ checks }: { checks: CheckRun[] }) {
  return (
    <ul className="space-y-2" aria-label="Recent checks">
      {checks.map((check) => (
        <li key={check.id} className="rounded-lg border bg-surface-raised p-3.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <CheckOutcomeBadge outcome={check.outcome} />
            <time
              dateTime={check.startedAt}
              className="font-mono text-[0.65rem] text-muted-foreground"
            >
              {formatTehranTimestamp(check.startedAt)}
            </time>
          </div>
          <dl className="mt-2.5 grid grid-cols-2 gap-x-3 gap-y-2 border-t pt-2.5 text-xs">
            <div>
              <dt className="eyebrow text-[0.58rem]">Duration</dt>
              <dd className="mt-0.5 font-mono tabular-nums">
                {formatDurationMs(check.durationMs)}
              </dd>
            </div>
            <div>
              <dt className="eyebrow text-[0.58rem]">Result</dt>
              <dd className="mt-0.5">
                {check.reservableCount} reservable · {check.notReservableCount} not
                · {check.unknownCount} unknown
              </dd>
            </div>
            <div>
              <dt className="eyebrow text-[0.58rem]">Journeys</dt>
              <dd className="mt-0.5 font-mono tabular-nums">
                {check.journeyCount}
              </dd>
            </div>
            <div>
              <dt className="eyebrow text-[0.58rem]">Offerings</dt>
              <dd className="mt-0.5 font-mono tabular-nums">
                {check.offeringCount}
              </dd>
            </div>
            <div className="col-span-2">
              <dt className="eyebrow text-[0.58rem]">Error</dt>
              <dd className="mt-0.5 font-mono text-[0.7rem] break-all text-muted-foreground">
                {check.errorKind || "—"}
                {check.httpStatus ? ` · HTTP ${check.httpStatus}` : ""}
              </dd>
            </div>
          </dl>
        </li>
      ))}
    </ul>
  );
}

function CheckRunTable({ checks }: { checks: CheckRun[] }) {
  const wide = useMediaQuery("(min-width: 1280px)");
  if (!wide) return <CheckRunCards checks={checks} />;
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Outcome</TableHead>
          <TableHead>Started</TableHead>
          <TableHead>Duration</TableHead>
          <TableHead>Journeys</TableHead>
          <TableHead>Offerings</TableHead>
          <TableHead>States</TableHead>
          <TableHead>Error</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {checks.map((check) => (
          <TableRow key={check.id}>
            <TableCell>
              <CheckOutcomeBadge outcome={check.outcome} />
            </TableCell>
            <TableCell className="min-w-40 whitespace-nowrap text-muted-foreground">
              <time dateTime={check.startedAt}>
                {formatTehranTimestamp(check.startedAt)}
              </time>
            </TableCell>
            <TableCell className="tabular-nums">
              {formatDurationMs(check.durationMs)}
            </TableCell>
            <TableCell className="tabular-nums">{check.journeyCount}</TableCell>
            <TableCell className="tabular-nums">{check.offeringCount}</TableCell>
            <TableCell className="whitespace-nowrap text-xs">
              <span className="text-success">
                {check.reservableCount} reservable
              </span>
              <span className="mx-1 text-muted-foreground">·</span>
              <span>{check.notReservableCount} not reservable</span>
              <span className="mx-1 text-muted-foreground">·</span>
              <span className="text-warning-foreground dark:text-warning">
                {check.unknownCount} unknown
              </span>
            </TableCell>
            <TableCell className="text-muted-foreground">
              {check.errorKind ? (
                <span className="font-mono text-xs">{check.errorKind}</span>
              ) : (
                "—"
              )}
              {check.httpStatus ? (
                <p className="text-xs">HTTP {check.httpStatus}</p>
              ) : null}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

export function MonitorDetailPage() {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const [actionError, setActionError] = useState<ApiError | null>(null);
  const [busyAction, setBusyAction] = useState<"pause" | "resume" | "delete" | null>(
    null,
  );

  const loader = useCallback(
    async (signal: AbortSignal): Promise<DetailData> => {
      const [monitor, providerList] = await Promise.all([
        monitorsApi.get(id, signal),
        providersApi.list(signal),
      ]);
      return { monitor, providers: providerList.providers };
    },
    [id],
  );
  const { data, error, loading, refreshing, refresh } = useApiResource(loader, {
    refetchIntervalMs: 30_000,
  });
  useDocumentTitle(data?.monitor.label ?? "Monitor");

  async function handlePauseResume(action: "pause" | "resume") {
    setActionError(null);
    setBusyAction(action);
    try {
      if (action === "pause") await monitorsApi.pause(id);
      else await monitorsApi.resume(id);
      toast.success(action === "pause" ? "Monitor paused" : "Monitor resumed");
      refresh();
    } catch (caught) {
      setActionError(isApiError(caught) ? caught : null);
    } finally {
      setBusyAction(null);
    }
  }

  async function handleDelete() {
    setActionError(null);
    setBusyAction("delete");
    try {
      await monitorsApi.remove(id);
      toast.success("Monitor deleted");
      navigate("/monitors", { replace: true });
    } catch (caught) {
      setActionError(isApiError(caught) ? caught : null);
      setBusyAction(null);
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <PageHeader title="Monitor" description="Loading monitor…" />
        <LoadingState rows={4} label="Loading monitor" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="space-y-6">
        <PageHeader title="Monitor" />
        <ErrorState
          error={error}
          title={
            error.status === 404
              ? "Monitor not found"
              : "Could not load the monitor"
          }
          onRetry={refresh}
        />
        <Button asChild variant="outline">
          <Link to="/monitors">
            <ArrowLeft aria-hidden="true" />
            Back to monitors
          </Link>
        </Button>
      </div>
    );
  }

  if (!data) return null;
  const { monitor, providers } = data;
  const providerMap = new Map(
    providers.map((provider) => [provider.id, provider]),
  );
  const summary = summarizeMonitorChecks(monitor);
  const totalPassengers = monitor.adults + monitor.children + monitor.infants;
  const activeTargets = monitor.providers.filter((target) => target.active);

  return (
    <div className="space-y-5">
      <Breadcrumbs
        items={[
          { label: "Monitors", to: "/monitors" },
          { label: monitor.label },
        ]}
      />
      <PageHeader
        eyebrow="Monitor detail"
        title={monitor.label}
        description={
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span>
              <bdi dir="auto">{monitor.originName}</bdi>
              <span aria-hidden="true"> → </span>
              <bdi dir="auto">{monitor.destinationName}</bdi>
            </span>
            <span aria-hidden="true">·</span>
            <DateDisplay value={monitor.departureDate} />
            <span aria-hidden="true">·</span>
            <span>
              {totalPassengers} passenger{totalPassengers === 1 ? "" : "s"}
            </span>
          </span>
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
            <Button
              asChild
              variant={monitor.status === "paused" ? "outline" : "default"}
              size="sm"
            >
              <Link to={`/monitors/${encodeURIComponent(id)}/edit`}>Edit</Link>
            </Button>
            {monitor.status === "active" ? (
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => void handlePauseResume("pause")}
                disabled={busyAction !== null}
              >
                <Pause aria-hidden="true" />
                {busyAction === "pause" ? "Pausing…" : "Pause"}
              </Button>
            ) : monitor.status === "paused" ? (
              <Button
                type="button"
                size="sm"
                onClick={() => void handlePauseResume("resume")}
                disabled={busyAction !== null}
              >
                <Play aria-hidden="true" />
                {busyAction === "resume" ? "Resuming…" : "Resume"}
              </Button>
            ) : null}
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="text-destructive hover:bg-destructive/5 hover:text-destructive"
                  disabled={busyAction !== null}
                >
                  <Trash2 aria-hidden="true" />
                  Delete
                </Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Delete “{monitor.label}”?</AlertDialogTitle>
                  <AlertDialogDescription>
                    This permanently deletes the monitor, its observations,
                    availability events, and delivery history. It cannot be
                    undone. Existing notifications already sent are not
                    affected.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel disabled={busyAction === "delete"}>
                    Cancel
                  </AlertDialogCancel>
                  <AlertDialogAction
                    disabled={busyAction === "delete"}
                    onClick={(event) => {
                      event.preventDefault();
                      void handleDelete();
                    }}
                  >
                    {busyAction === "delete" ? "Deleting…" : "Delete monitor"}
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </>
        }
      />

      {actionError ? (
        <ErrorState
          error={actionError}
          title="The action could not be completed"
        />
      ) : null}

      <Card className="gap-0 overflow-hidden py-0">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b bg-surface-raised/55 px-4 py-3 sm:px-5">
          <div className="flex flex-wrap items-center gap-2">
            <MonitorStatusBadge status={monitor.status} />
            <Badge variant="outline">
              {activeTargets.length} active provider
              {activeTargets.length === 1 ? "" : "s"}
            </Badge>
            <Badge variant="outline">
              {totalPassengers} passenger{totalPassengers === 1 ? "" : "s"}
            </Badge>
          </div>
          <span className="font-mono text-[0.65rem] text-muted-foreground">
            Updated {formatTehranTimestamp(monitor.updatedAt)}
          </span>
        </div>
        <CardContent className="space-y-4 px-4 py-4 sm:px-5">
          <Alert variant={alertVariantForTone(summary.tone)}>
            {summary.tone === "info" ? (
              <Info aria-hidden="true" />
            ) : (
              <AlertTriangle
                className={cn(
                  (summary.tone === "success" || summary.tone === "muted") &&
                    "hidden",
                )}
                aria-hidden="true"
              />
            )}
            <AlertTitle>{summary.title}</AlertTitle>
            <AlertDescription>{summary.detail}</AlertDescription>
          </Alert>

          {monitor.status === "expired" ? (
            <Alert variant="warning">
              <AlertTriangle aria-hidden="true" />
              <AlertTitle>Expired monitors cannot resume</AlertTitle>
              <AlertDescription>
                The departure day ended in Tehran. Edit the departure date to a
                future day, then resume monitoring.{" "}
                <Link
                  to={`/monitors/${encodeURIComponent(id)}/edit`}
                  className="underline underline-offset-2"
                >
                  Edit departure date
                </Link>
              </AlertDescription>
            </Alert>
          ) : null}

          <dl className="grid grid-cols-2 gap-x-5 gap-y-3 border-t pt-4 text-sm sm:grid-cols-4">
            <div>
              <dt className="eyebrow text-[0.6rem]">Adults</dt>
              <dd className="mt-1 font-mono font-medium tabular-nums">
                {monitor.adults}
              </dd>
            </div>
            <div>
              <dt className="eyebrow text-[0.6rem]">Children</dt>
              <dd className="mt-1 font-mono font-medium tabular-nums">
                {monitor.children}
              </dd>
            </div>
            <div>
              <dt className="eyebrow text-[0.6rem]">Infants</dt>
              <dd className="mt-1 font-mono font-medium tabular-nums">
                {monitor.infants}
              </dd>
            </div>
            <div>
              <dt className="eyebrow text-[0.6rem]">Providers</dt>
              <dd className="mt-1 font-medium">{activeTargets.length} active</dd>
            </div>
            <div>
              <dt className="eyebrow text-[0.6rem]">Departure date</dt>
              <dd className="mt-1">
                <DateDisplay value={monitor.departureDate} />
              </dd>
            </div>
            <div>
              <dt className="eyebrow text-[0.6rem]">Expires (Tehran)</dt>
              <dd className="mt-1 font-mono text-[0.75rem]">
                {formatTehranTimestamp(monitor.expiresAt)}
              </dd>
            </div>
            <div>
              <dt className="eyebrow text-[0.6rem]">Created</dt>
              <dd className="mt-1 font-mono text-[0.75rem]">
                {formatTehranTimestamp(monitor.createdAt)}
              </dd>
            </div>
            <div>
              <dt className="eyebrow text-[0.6rem]">Updated</dt>
              <dd className="mt-1 font-mono text-[0.75rem]">
                {formatTehranTimestamp(monitor.updatedAt)}
              </dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <section className="space-y-3" aria-labelledby="provider-observations">
        <div className="flex flex-wrap items-end justify-between gap-2">
          <div>
            <h2
              id="provider-observations"
              className="text-lg font-semibold tracking-[-0.015em]"
            >
              Provider observations
            </h2>
            <p className="mt-0.5 text-xs text-muted-foreground">
              Saved check results, not live availability. A missing or unknown
              offering is never treated as unavailable.
            </p>
          </div>
          <span className="font-mono text-[0.65rem] tracking-wide text-muted-foreground uppercase">
            {monitor.providers.length} target
            {monitor.providers.length === 1 ? "" : "s"}
          </span>
        </div>
        {monitor.providers.length === 0 ? (
          <EmptyState
            icon={AlertTriangle}
            title="No provider targets"
            description="This monitor has no provider target rows."
          />
        ) : (
          <div className="space-y-3">
            {monitor.providers.map((target) => (
              <ProviderTarget
                key={target.providerId}
                target={target}
                provider={providerMap.get(target.providerId)}
              />
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
