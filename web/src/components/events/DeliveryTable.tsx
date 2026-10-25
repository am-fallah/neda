import { useId } from "react";
import type { DeliveryState, EventRecord } from "@/api/types";
import {
  DeliveryChannelLabel,
  DeliveryStatusBadge,
} from "@/components/status";
import { Badge } from "@/components/ui/badge";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatTehranTimestamp } from "@/lib/dates";
import { deliverySummaryText } from "@/lib/monitorSummary";

function stateNote(delivery: DeliveryState): string {
  switch (delivery.status) {
    case "pending":
      return "Queued; not sent yet.";
    case "sending":
      return "A send attempt is in progress.";
    case "sent":
      return "The channel accepted this message.";
    case "retry_scheduled":
      return "Not delivered yet; another attempt is scheduled.";
    case "failed":
      return "Not delivered.";
    case "skipped":
      return "Not sent; channel or monitor conditions skipped it.";
  }
}

function DeliveryCards({ deliveries }: { deliveries: DeliveryState[] }) {
  return (
    <ul
      className="divide-y rounded-lg border bg-card"
      aria-label="Notification deliveries"
    >
      {deliveries.map((delivery) => (
        <li key={delivery.id} className="space-y-2.5 p-3.5">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <DeliveryChannelLabel channel={delivery.channel} />
            <DeliveryStatusBadge status={delivery.status} />
          </div>
          <p className="text-xs leading-relaxed text-muted-foreground">
            {stateNote(delivery)}
          </p>
          <dl className="grid grid-cols-2 gap-x-3 gap-y-2 text-xs">
            <div>
              <dt className="eyebrow text-[0.58rem]">Attempts</dt>
              <dd className="mt-0.5 font-mono tabular-nums">
                {delivery.attemptCount}
              </dd>
            </div>
            <div>
              <dt className="eyebrow text-[0.58rem]">Last attempt</dt>
              <dd className="mt-0.5 font-mono text-[0.7rem] text-muted-foreground">
                {formatTehranTimestamp(delivery.lastAttemptAt)}
              </dd>
            </div>
            <div>
              <dt className="eyebrow text-[0.58rem]">Sent</dt>
              <dd className="mt-0.5 font-mono text-[0.7rem] text-muted-foreground">
                {formatTehranTimestamp(delivery.sentAt)}
              </dd>
            </div>
            {delivery.nextAttemptAt ? (
              <div>
                <dt className="eyebrow text-[0.58rem]">Next attempt</dt>
                <dd className="mt-0.5 font-mono text-[0.7rem] text-muted-foreground">
                  {formatTehranTimestamp(delivery.nextAttemptAt)}
                </dd>
              </div>
            ) : null}
            {delivery.lastErrorKind ? (
              <div className="col-span-2">
                <dt className="eyebrow text-[0.58rem]">Error</dt>
                <dd className="mt-0.5 font-mono text-[0.7rem] break-all text-muted-foreground">
                  {delivery.lastErrorKind}
                </dd>
              </div>
            ) : null}
          </dl>
        </li>
      ))}
    </ul>
  );
}

export function DeliveryTable({
  deliveries,
  caption,
}: {
  deliveries: DeliveryState[];
  caption?: string;
}) {
  const headingId = useId();
  const wide = useMediaQuery("(min-width: 900px)");
  if (deliveries.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        {caption ?? "No delivery records for this event."}
      </p>
    );
  }
  return (
    <div className="space-y-2">
      <p id={headingId} className="sr-only">
        Notification deliveries
      </p>
      {wide ? (
        <Table aria-labelledby={headingId}>
          <TableHeader>
            <TableRow>
              <TableHead>Channel</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Attempts</TableHead>
              <TableHead>Last attempt</TableHead>
              <TableHead>Sent</TableHead>
              <TableHead>Detail</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {deliveries.map((delivery) => (
              <TableRow key={delivery.id}>
                <TableCell>
                  <DeliveryChannelLabel channel={delivery.channel} />
                </TableCell>
                <TableCell>
                  <DeliveryStatusBadge status={delivery.status} />
                </TableCell>
                <TableCell className="font-mono tabular-nums">
                  {delivery.attemptCount}
                </TableCell>
                <TableCell className="whitespace-nowrap font-mono text-[0.7rem] text-muted-foreground">
                  {formatTehranTimestamp(delivery.lastAttemptAt)}
                </TableCell>
                <TableCell className="whitespace-nowrap font-mono text-[0.7rem] text-muted-foreground">
                  {formatTehranTimestamp(delivery.sentAt)}
                </TableCell>
                <TableCell className="min-w-48 text-xs text-muted-foreground">
                  <p>{stateNote(delivery)}</p>
                  {delivery.lastErrorKind ? (
                    <p className="mt-0.5 font-mono">{delivery.lastErrorKind}</p>
                  ) : null}
                  {delivery.nextAttemptAt ? (
                    <p className="mt-0.5">
                      Next attempt: {formatTehranTimestamp(delivery.nextAttemptAt)}
                    </p>
                  ) : null}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      ) : (
        <DeliveryCards deliveries={deliveries} />
      )}
    </div>
  );
}

export function EventDeliverySummary({ event }: { event: EventRecord }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Badge variant="outline">Delivery summary</Badge>
      <span className="text-sm text-muted-foreground">
        {deliverySummaryText(event.deliverySummary)}
      </span>
    </div>
  );
}
