import {
  AlertTriangle,
  CheckCircle2,
  Clock3,
  Info,
  PauseCircle,
  TrainFront,
} from "lucide-react";
import { Link } from "react-router-dom";
import type { Monitor, ProviderInfo } from "@/api/types";
import { DateDisplay } from "@/components/JalaliDatePicker";
import { MonitorStatusBadge, TestOnlyBadge } from "@/components/status";
import { Badge } from "@/components/ui/badge";
import { activeProviders, summarizeMonitorChecks, type Tone } from "@/lib/monitorSummary";
import { formatTehranTimestamp } from "@/lib/dates";
import { cn } from "@/lib/utils";

function toneIcon(tone: Tone) {
  switch (tone) {
    case "destructive":
      return AlertTriangle;
    case "warning":
      return AlertTriangle;
    case "success":
      return CheckCircle2;
    case "muted":
      return PauseCircle;
    default:
      return Info;
  }
}

function toneClass(tone: Tone): string {
  switch (tone) {
    case "destructive":
      return "text-destructive";
    case "warning":
      return "text-warning-foreground dark:text-warning";
    case "success":
      return "text-success";
    case "muted":
      return "text-muted-foreground";
    default:
      return "text-muted-foreground";
  }
}

export function MonitorCard({
  monitor,
  providerMap,
}: {
  monitor: Monitor;
  providerMap: Map<string, ProviderInfo>;
}) {
  const summary = summarizeMonitorChecks(monitor);
  const SummaryIcon = toneIcon(summary.tone);
  const providers = activeProviders(monitor);
  const nextDue = providers
    .map((provider) => provider.nextDueAt)
    .filter((value): value is string => Boolean(value))
    .sort()[0];
  const lastAttempt = providers
    .map((provider) => provider.lastAttemptAt)
    .filter((value): value is string => Boolean(value))
    .sort()
    .at(-1);

  return (
    <Link
      to={`/monitors/${encodeURIComponent(monitor.id)}`}
      className="group flex flex-col gap-3 rounded-xl border bg-card p-5 transition-colors hover:bg-muted/30 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="truncate text-base font-semibold">{monitor.label}</h2>
            <MonitorStatusBadge status={monitor.status} />
          </div>
          <p className="mt-1 flex flex-wrap items-center gap-1.5 text-sm text-muted-foreground">
            <TrainFront className="size-3.5" aria-hidden="true" />
            {monitor.originName} → {monitor.destinationName}
          </p>
          <p className="mt-1 text-sm text-muted-foreground">
            <DateDisplay value={monitor.departureDate} />
          </p>
        </div>
        <Badge variant="outline">
          {monitor.adults + monitor.children + monitor.infants} passenger
          {monitor.adults + monitor.children + monitor.infants === 1 ? "" : "s"}
        </Badge>
      </div>

      <div className={cn("flex items-start gap-2 text-sm", toneClass(summary.tone))}>
        <SummaryIcon className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
        <div>
          <p className="font-medium">{summary.title}</p>
          <p className="text-xs text-muted-foreground">{summary.detail}</p>
        </div>
      </div>

      <div className="flex flex-wrap gap-1.5">
        {providers.length === 0 ? (
          <Badge variant="warning">No active providers</Badge>
        ) : (
          providers.map((provider) => {
            const info = providerMap.get(provider.providerId);
            return (
              <Badge key={provider.providerId} variant="outline" className="gap-1">
                {info?.displayName ?? provider.providerId}
                {info?.testOnly ? <TestOnlyBadge /> : null}
              </Badge>
            );
          })
        )}
      </div>

      <dl className="grid grid-cols-2 gap-x-4 gap-y-1 border-t pt-3 text-xs text-muted-foreground">
        <div>
          <dt className="inline">Last attempt: </dt>
          <dd className="inline">
            <time dateTime={lastAttempt ?? undefined}>
              {lastAttempt ? formatTehranTimestamp(lastAttempt) : "Never"}
            </time>
          </dd>
        </div>
        <div>
          <dt className="inline">Next check: </dt>
          <dd className="inline">
            {monitor.status === "active" && nextDue
              ? formatTehranTimestamp(nextDue)
              : "Not scheduled"}
          </dd>
        </div>
        <div className="flex items-center gap-1">
          <Clock3 className="size-3" aria-hidden="true" />
          <dt className="sr-only">Last update</dt>
          <dd>
            Updated{" "}
            <time dateTime={monitor.updatedAt}>
              {formatTehranTimestamp(monitor.updatedAt)}
            </time>
          </dd>
        </div>
      </dl>
    </Link>
  );
}
