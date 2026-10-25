import {
  AlertTriangle,
  ArrowUpRight,
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
import {
  activeProviders,
  summarizeMonitorChecks,
  type Tone,
} from "@/lib/monitorSummary";
import { formatTehranTimestamp } from "@/lib/dates";
import { cn } from "@/lib/utils";

function toneIcon(tone: Tone) {
  switch (tone) {
    case "destructive":
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

function toneClasses(tone: Tone): { text: string; rail: string; surface: string } {
  switch (tone) {
    case "destructive":
      return {
        text: "text-destructive",
        rail: "bg-destructive",
        surface: "bg-destructive/5",
      };
    case "warning":
      return {
        text: "text-warning-foreground dark:text-warning",
        rail: "bg-warning",
        surface: "bg-warning/5",
      };
    case "success":
      return {
        text: "text-success",
        rail: "bg-success",
        surface: "bg-success/5",
      };
    case "muted":
      return {
        text: "text-muted-foreground",
        rail: "bg-muted-foreground/40",
        surface: "bg-muted/30",
      };
    default:
      return {
        text: "text-muted-foreground",
        rail: "bg-info",
        surface: "bg-info/5",
      };
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
  const tone = toneClasses(summary.tone);
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
  const passengerCount =
    monitor.adults + monitor.children + monitor.infants;

  return (
    <Link
      to={`/monitors/${encodeURIComponent(monitor.id)}`}
      className="group relative flex min-w-0 flex-col gap-3 overflow-hidden rounded-[10px] border bg-card py-4 pr-4 pl-5 shadow-xs transition-[border-color,background-color] duration-150 hover:border-primary/30 hover:bg-surface-raised focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none sm:pr-5 sm:pl-6 motion-reduce:transition-none"
    >
      <span
        className={cn("absolute inset-y-0 left-0 w-0.5", tone.rail)}
        aria-hidden="true"
      />

      <div className="flex min-w-0 items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h2 className="min-w-0 text-[0.9375rem] font-semibold tracking-[-0.01em] break-words">
              {monitor.label}
            </h2>
            <MonitorStatusBadge status={monitor.status} />
          </div>
          <p className="mt-1.5 flex min-w-0 flex-wrap items-center gap-x-1.5 gap-y-1 text-sm text-muted-foreground">
            <TrainFront className="size-3.5 shrink-0" aria-hidden="true" />
            <bdi dir="auto" className="min-w-0 break-words">
              {monitor.originName}
            </bdi>
            <span aria-hidden="true" className="text-muted-foreground/60">
              →
            </span>
            <bdi dir="auto" className="min-w-0 break-words">
              {monitor.destinationName}
            </bdi>
          </p>
          <p className="mt-1 text-xs text-muted-foreground">
            <DateDisplay value={monitor.departureDate} />
          </p>
        </div>
        <ArrowUpRight
          className="size-4 shrink-0 text-muted-foreground/60 transition-colors group-hover:text-foreground"
          aria-hidden="true"
        />
      </div>

      <div className={cn("flex items-start gap-2 rounded-md px-2.5 py-2", tone.surface)}>
        <SummaryIcon
          className={cn("mt-0.5 size-3.5 shrink-0", tone.text)}
          aria-hidden="true"
        />
        <div className="min-w-0">
          <p className={cn("text-[0.8125rem] font-medium", tone.text)}>
            {summary.title}
          </p>
          <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
            {summary.detail}
          </p>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-1.5">
        <Badge variant="outline">
          {passengerCount} passenger{passengerCount === 1 ? "" : "s"}
        </Badge>
        {providers.length === 0 ? (
          <Badge variant="warning">No active providers</Badge>
        ) : (
          providers.map((provider) => {
            const info = providerMap.get(provider.providerId);
            return (
              <span key={provider.providerId} className="inline-flex items-center gap-1">
                <Badge variant="outline">
                  {info?.displayName ?? provider.providerId}
                </Badge>
                {info?.testOnly ? <TestOnlyBadge /> : null}
              </span>
            );
          })
        )}
      </div>

      <dl className="mt-auto grid grid-cols-2 gap-x-4 gap-y-1.5 border-t pt-3 font-mono text-[0.65rem] text-muted-foreground">
        <div className="min-w-0">
          <dt className="text-[0.6rem] tracking-wide uppercase">Last attempt</dt>
          <dd className="mt-0.5 break-words">
            <time dateTime={lastAttempt ?? undefined}>
              {lastAttempt ? formatTehranTimestamp(lastAttempt) : "Never"}
            </time>
          </dd>
        </div>
        <div className="min-w-0">
          <dt className="text-[0.6rem] tracking-wide uppercase">Next check</dt>
          <dd className="mt-0.5 break-words">
            {monitor.status === "active" && nextDue
              ? formatTehranTimestamp(nextDue)
              : "Not scheduled"}
          </dd>
        </div>
        <div className="col-span-2 flex items-center gap-1.5 pt-0.5">
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
