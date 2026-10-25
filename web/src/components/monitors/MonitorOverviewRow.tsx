import {
  AlertTriangle,
  ChevronRight,
  CircleDashed,
  Clock3,
  type LucideIcon,
} from "lucide-react";
import { Link } from "react-router-dom";
import type { Monitor } from "@/api/types";
import { MonitorStatusBadge } from "@/components/status";
import {
  activeProviders,
  summarizeMonitorChecks,
  type Summary,
} from "@/lib/monitorSummary";
import { formatElapsed, formatJalaliDate } from "@/lib/dates";
import { cn } from "@/lib/utils";

interface CheckPresentation {
  icon: LucideIcon;
  text: string;
  className: string;
}

function checkPresentation(
  monitor: Monitor,
  summary: Summary,
  lastAttempt: string | undefined,
): CheckPresentation {
  const checkedText = lastAttempt
    ? `Checked ${formatElapsed(lastAttempt)}`
    : "No checks yet";

  if (monitor.status !== "active") {
    return {
      icon: Clock3,
      text: checkedText,
      className: "text-muted-foreground",
    };
  }

  if (activeProviders(monitor).length === 0) {
    return {
      icon: AlertTriangle,
      text: "No active providers",
      className: "text-warning-foreground dark:text-warning",
    };
  }

  switch (summary.tone) {
    case "destructive":
      return {
        icon: AlertTriangle,
        text: "Last check failed · availability unknown",
        className: "text-destructive",
      };
    case "warning":
      return {
        icon: AlertTriangle,
        text: "Some checks failed",
        className: "text-warning-foreground dark:text-warning",
      };
    case "muted":
      return {
        icon: CircleDashed,
        text: "No checks yet",
        className: "text-muted-foreground",
      };
    default:
      return {
        icon: Clock3,
        text: checkedText,
        className: "text-muted-foreground",
      };
  }
}

export function MonitorOverviewRow({ monitor }: { monitor: Monitor }) {
  const summary = summarizeMonitorChecks(monitor);
  const lastAttempt = activeProviders(monitor)
    .map((provider) => provider.lastAttemptAt)
    .filter((value): value is string => Boolean(value))
    .sort()
    .at(-1);
  const check = checkPresentation(monitor, summary, lastAttempt);
  const CheckIcon = check.icon;
  const date = formatJalaliDate(monitor.departureDate);

  return (
    <Link
      to={`/monitors/${encodeURIComponent(monitor.id)}`}
      className="group flex items-start gap-3 px-4 py-4 transition-colors hover:bg-muted/40 focus-visible:bg-muted/40 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset focus-visible:outline-none sm:items-center"
    >
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 truncate text-sm font-medium">
            {monitor.label}
          </span>
          <MonitorStatusBadge status={monitor.status} />
        </div>
        <p className="mt-1 truncate text-sm text-muted-foreground">
          {monitor.originName} <span aria-hidden="true">→</span>{" "}
          <span className="sr-only">to </span>
          {monitor.destinationName}
        </p>
        <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground sm:hidden">
          <time
            dateTime={monitor.departureDate}
            title={`Gregorian ${monitor.departureDate}`}
          >
            {date}
          </time>
          <span className={cn("inline-flex items-center gap-1", check.className)}>
            <CheckIcon className="size-3.5 shrink-0" aria-hidden="true" />
            {check.text}
          </span>
        </div>
      </div>

      <div className="hidden shrink-0 items-center gap-4 sm:flex">
        <time
          className="text-sm text-muted-foreground"
          dateTime={monitor.departureDate}
          title={`Gregorian ${monitor.departureDate}`}
        >
          {date}
        </time>
        <span className={cn("inline-flex items-center gap-1 text-sm", check.className)}>
          <CheckIcon className="size-3.5 shrink-0" aria-hidden="true" />
          {check.text}
        </span>
      </div>

      <ChevronRight
        className="mt-0.5 size-4 shrink-0 text-muted-foreground group-hover:text-foreground sm:mt-0"
        aria-hidden="true"
      />
    </Link>
  );
}
