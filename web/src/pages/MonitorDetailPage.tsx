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
import { DateDisplay } from "@/components/JalaliDatePicker";
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
import {
  formatDurationMs,
  formatElapsed,
  formatTehranTimestamp,
} from "@/lib/dates";
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
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between gap-2">
          <div>
            <CardTitle className="flex flex-wrap items-center gap-2">
              {provider?.displayName ?? target.providerId}
              {provider?.testOnly ? <TestOnlyBadge /> : null}
              <Badge variant="outline">
                {target.active ? "Active target" : "Removed target"}
              </Badge>
            </CardTitle>
            <CardDescription>
              Provider ID <code className="font-mono">{target.providerId}</code>{" "}
              · config version {target.configVersion}
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
      <CardContent className="space-y-4">
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
            <ul className="space-y-1 text-sm">
              {target.selections.map((selection) => (
                <li
                  key={`${selection.journeyKey}:${selection.offeringKey}`}
                  className="flex flex-wrap items-center gap-2 rounded-md border px-3 py-2"
                >
                  <span className="font-medium">
                    Train {selection.trainNumber || selection.journeyNativeId || "unknown"}
                  </span>
                  <span className="text-muted-foreground">
                    {selection.wagonName || selection.className || selection.offeringNativeId}
                  </span>
                  {selection.departureAt ? (
                    <span className="text-xs text-muted-foreground">
                      {formatTehranTimestamp(selection.departureAt)}
                    </span>
                  ) : null}
                </li>
              ))}
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

function InventoryTable({
  observations,
}: {
  observations: InventoryObservation[];
}) {
  return (
    <div className="space-y-2">
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
                  <>
                    {observation.priceAmount.toLocaleString("en-US")}{" "}
                    <span className="text-xs text-muted-foreground">
                      {observation.priceCurrency || ""}
                    </span>
                  </>
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

function CheckRunTable({ checks }: { checks: CheckRun[] }) {
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
    <div className="space-y-6">
      <PageHeader
        eyebrow="Monitor"
        title={monitor.label}
        description={
          <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span>
              {monitor.originName} → {monitor.destinationName}
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
            <Button asChild variant="outline" size="sm">
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
                  className="text-destructive"
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

      <Card>
        <CardHeader>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <CardTitle>Status</CardTitle>
            <MonitorStatusBadge status={monitor.status} />
          </div>
          <CardDescription>
            Availability reflects saved check results, not a live query. A
            failed check always leaves availability unknown.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
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

          <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm sm:grid-cols-4">
            <div>
              <dt className="text-xs text-muted-foreground">Adults</dt>
              <dd className="font-medium tabular-nums">{monitor.adults}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">Children</dt>
              <dd className="font-medium tabular-nums">{monitor.children}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">Infants</dt>
              <dd className="font-medium tabular-nums">{monitor.infants}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">Providers</dt>
              <dd className="font-medium">{activeTargets.length} active</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">Departure date</dt>
              <dd>
                <DateDisplay value={monitor.departureDate} />
              </dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">
                Expires (Tehran)
              </dt>
              <dd>{formatTehranTimestamp(monitor.expiresAt)}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">Created</dt>
              <dd>{formatTehranTimestamp(monitor.createdAt)}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">Updated</dt>
              <dd>{formatTehranTimestamp(monitor.updatedAt)}</dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <div className="space-y-4">
        <h2 className="text-lg font-semibold">Provider observations</h2>
        {monitor.providers.length === 0 ? (
          <EmptyState
            icon={AlertTriangle}
            title="No provider targets"
            description="This monitor has no provider target rows."
          />
        ) : (
          monitor.providers.map((target) => (
            <ProviderTarget
              key={target.providerId}
              target={target}
              provider={providerMap.get(target.providerId)}
            />
          ))
        )}
      </div>
    </div>
  );
}
