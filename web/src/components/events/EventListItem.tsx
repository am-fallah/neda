import { Link } from "react-router-dom";
import { ArrowRight, Clock3 } from "lucide-react";
import type { EventRecord } from "@/api/types";
import { EventKindBadge } from "@/components/status";
import { Badge } from "@/components/ui/badge";
import { formatTehranTimestamp } from "@/lib/dates";
import {
  deliverySummaryText,
  deliverySummaryTone,
} from "@/lib/monitorSummary";

export function EventListItem({ event }: { event: EventRecord }) {
  const tone = deliverySummaryTone(event.deliverySummary);
  return (
    <Link
      to={`/events/${encodeURIComponent(event.id)}`}
      className="group flex flex-col gap-2 rounded-xl border bg-card p-4 transition-colors hover:bg-muted/40 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
    >
      <div className="flex flex-wrap items-center gap-2">
        <EventKindBadge kind={event.kind} />
        {event.monitorLabel ? (
          <span className="min-w-0 flex-1 truncate text-sm font-medium">
            {event.monitorLabel}
          </span>
        ) : (
          <span className="min-w-0 flex-1 truncate text-sm font-medium">
            Provider health event
          </span>
        )}
        <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
          <Clock3 className="size-3" aria-hidden="true" />
          <time dateTime={event.occurredAt}>
            {formatTehranTimestamp(event.occurredAt)}
          </time>
        </span>
      </div>

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
        {event.kind === "availability" ? (
          <>
            {event.originName && event.destinationName ? (
              <span>
                {event.originName} → {event.destinationName}
              </span>
            ) : null}
            {event.trainNumber ? <span>Train {event.trainNumber}</span> : null}
            {event.wagonName || event.className ? (
              <span>{[event.wagonName, event.className].filter(Boolean).join(" · ")}</span>
            ) : null}
            {event.priceAmount !== null && event.priceAmount !== undefined ? (
              <span>
                {event.priceAmount.toLocaleString("en-US")}{" "}
                {event.priceCurrency || ""}
              </span>
            ) : null}
          </>
        ) : (
          <>
            <span className="capitalize">{event.providerId}</span>
            {event.errorKind ? (
              <span className="font-mono text-xs">{event.errorKind}</span>
            ) : null}
            {event.failureCount !== null && event.failureCount !== undefined ? (
              <span>{event.failureCount} consecutive failures</span>
            ) : null}
          </>
        )}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2">
        <span className="inline-flex items-center gap-2">
          <Badge variant={tone}>
            {deliverySummaryText(event.deliverySummary)}
          </Badge>
          <span className="text-xs text-muted-foreground">
            Delivery is tracked separately from detection.
          </span>
        </span>
        <span className="inline-flex items-center gap-1 text-xs font-medium text-primary">
          Details
          <ArrowRight className="size-3" aria-hidden="true" />
        </span>
      </div>
    </Link>
  );
}
