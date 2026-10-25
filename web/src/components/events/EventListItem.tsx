import { Link } from "react-router-dom";
import { ArrowUpRight, Clock3 } from "lucide-react";
import type { EventRecord } from "@/api/types";
import { ProviderPrice } from "@/components/ProviderPrice";
import { EventKindBadge } from "@/components/status";
import { Badge } from "@/components/ui/badge";
import { formatTehranTimestamp } from "@/lib/dates";
import {
  deliverySummaryText,
  deliverySummaryTone,
} from "@/lib/monitorSummary";
import { cn } from "@/lib/utils";

export function EventListItem({
  event,
  framed = true,
}: {
  event: EventRecord;
  framed?: boolean;
}) {
  const tone = deliverySummaryTone(event.deliverySummary);
  return (
    <Link
      to={`/events/${encodeURIComponent(event.id)}`}
      className={cn(
        "group flex min-w-0 flex-col gap-2 p-3.5 transition-colors hover:bg-muted/25 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none motion-reduce:transition-none",
        framed && "rounded-lg border bg-card",
      )}
    >
      <div className="flex min-w-0 items-start justify-between gap-2">
        <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
          <EventKindBadge kind={event.kind} />
          <span className="min-w-0 flex-1 text-[0.8125rem] font-medium break-words">
            {event.monitorLabel ?? "Provider health event"}
          </span>
        </div>
        <ArrowUpRight
          className="mt-0.5 size-3.5 shrink-0 text-muted-foreground/50 transition-colors group-hover:text-foreground"
          aria-hidden="true"
        />
      </div>

      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
        <span className="inline-flex items-center gap-1 font-mono text-[0.65rem]">
          <Clock3 className="size-3" aria-hidden="true" />
          <time dateTime={event.occurredAt}>
            {formatTehranTimestamp(event.occurredAt)}
          </time>
        </span>
        {event.kind === "availability" ? (
          <>
            {event.originName && event.destinationName ? (
              <span className="inline-flex min-w-0 items-center gap-1">
                <bdi dir="auto" className="min-w-0 break-words">
                  {event.originName}
                </bdi>
                <span aria-hidden="true">→</span>
                <bdi dir="auto" className="min-w-0 break-words">
                  {event.destinationName}
                </bdi>
              </span>
            ) : null}
            {event.trainNumber ? (
              <span className="font-mono">Train {event.trainNumber}</span>
            ) : null}
            {event.wagonName || event.className ? (
              <span>
                <bdi dir="auto">
                  {[event.wagonName, event.className].filter(Boolean).join(" · ")}
                </bdi>
              </span>
            ) : null}
            {event.priceAmount !== null && event.priceAmount !== undefined ? (
              <ProviderPrice
                amount={event.priceAmount}
                currency={event.priceCurrency}
                className="text-[0.7rem]"
              />
            ) : null}
          </>
        ) : (
          <>
            <span className="capitalize">{event.providerId}</span>
            {event.errorKind ? (
              <span className="font-mono text-[0.7rem]">{event.errorKind}</span>
            ) : null}
            {event.failureCount !== null && event.failureCount !== undefined ? (
              <span>{event.failureCount} consecutive failures</span>
            ) : null}
          </>
        )}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-2 border-t pt-2">
        <span className="inline-flex flex-wrap items-center gap-2">
          <Badge variant={tone}>{deliverySummaryText(event.deliverySummary)}</Badge>
          <span className="hidden text-[0.65rem] text-muted-foreground sm:inline">
            Detection and delivery are tracked separately.
          </span>
        </span>
        <span className="text-[0.65rem] font-medium text-primary">
          Details
        </span>
      </div>
    </Link>
  );
}
