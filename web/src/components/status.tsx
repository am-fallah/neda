import {
  AlertTriangle,
  CheckCircle2,
  CircleDashed,
  CircleSlash,
  Clock3,
  Inbox,
  Send,
  ShieldCheck,
  TimerOff,
  XCircle,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type {
  CheckOutcome,
  DeliveryStatus,
  EventKind,
  MonitorStatus,
  ObservationState,
  ProviderInfo,
} from "@/api/types";

export function MonitorStatusBadge({ status }: { status: MonitorStatus }) {
  switch (status) {
    case "active":
      return <Badge variant="success">Active</Badge>;
    case "paused":
      return <Badge variant="muted">Paused</Badge>;
    case "expired":
      return <Badge variant="outline">Expired</Badge>;
  }
}

export function ObservationStateBadge({
  state,
  missing = false,
}: {
  state: ObservationState;
  missing?: boolean;
}) {
  if (missing) {
    return (
      <Badge variant="warning">
        <AlertTriangle aria-hidden="true" />
        Unknown · not returned
      </Badge>
    );
  }
  switch (state) {
    case "reservable":
      return (
        <Badge variant="success">
          <CheckCircle2 aria-hidden="true" />
          Reservable
        </Badge>
      );
    case "not_reservable":
      return <Badge variant="secondary">Not reservable</Badge>;
    case "unknown":
      return (
        <Badge variant="warning">
          <CircleDashed aria-hidden="true" />
          Unknown
        </Badge>
      );
  }
}

export function CheckOutcomeBadge({ outcome }: { outcome: CheckOutcome }) {
  switch (outcome) {
    case "success":
      return (
        <Badge variant="success">
          <CheckCircle2 aria-hidden="true" />
          Success
        </Badge>
      );
    case "empty":
      return <Badge variant="success">Success · no offerings</Badge>;
    case "error":
      return (
        <Badge variant="destructive">
          <XCircle aria-hidden="true" />
          Failed
        </Badge>
      );
    case "running":
      return (
        <Badge variant="info">
          <Clock3 aria-hidden="true" />
          Running
        </Badge>
      );
    case "discarded":
      return <Badge variant="muted">Discarded</Badge>;
    case "abandoned":
      return (
        <Badge variant="muted">
          <CircleSlash aria-hidden="true" />
          Abandoned
        </Badge>
      );
  }
}

const DELIVERY_LABELS: Record<
  DeliveryStatus,
  { label: string; variant: "success" | "warning" | "destructive" | "info" | "muted"; icon: typeof Send }
> = {
  pending: { label: "Queued", variant: "info", icon: Inbox },
  sending: { label: "Sending", variant: "info", icon: Send },
  sent: { label: "Sent", variant: "success", icon: CheckCircle2 },
  retry_scheduled: {
    label: "Retry scheduled",
    variant: "warning",
    icon: Clock3,
  },
  failed: { label: "Failed", variant: "destructive", icon: XCircle },
  skipped: { label: "Skipped", variant: "muted", icon: CircleSlash },
};

export function DeliveryStatusBadge({ status }: { status: DeliveryStatus }) {
  const entry = DELIVERY_LABELS[status] ?? DELIVERY_LABELS.pending;
  const Icon = entry.icon;
  return (
    <Badge variant={entry.variant}>
      <Icon aria-hidden="true" />
      {entry.label}
    </Badge>
  );
}

export function deliveryStatusLabel(status: DeliveryStatus): string {
  return DELIVERY_LABELS[status]?.label ?? "Unknown";
}

export function DeliveryChannelLabel({ channel }: { channel: string }) {
  return <span className="capitalize">{channel}</span>;
}

export function EventKindBadge({ kind }: { kind: EventKind }) {
  switch (kind) {
    case "availability":
      return (
        <Badge variant="success">
          <CheckCircle2 aria-hidden="true" />
          Availability
        </Badge>
      );
    case "provider_degraded":
      return (
        <Badge variant="warning">
          <AlertTriangle aria-hidden="true" />
          Provider degraded
        </Badge>
      );
    case "provider_recovered":
      return (
        <Badge variant="info">
          <ShieldCheck aria-hidden="true" />
          Provider recovered
        </Badge>
      );
  }
}

export function ProviderHealthBadge({ provider }: { provider: ProviderInfo }) {
  const health = provider.health;
  if (!health) {
    return <Badge variant="muted">No checks yet</Badge>;
  }
  if (health.openEpisode) {
    return (
      <Badge variant="destructive">
        <AlertTriangle aria-hidden="true" />
        Degraded
      </Badge>
    );
  }
  if (health.stale) {
    return (
      <Badge variant="warning">
        <TimerOff aria-hidden="true" />
        Stale
      </Badge>
    );
  }
  if (health.lastSuccessAt) {
    return (
      <Badge variant="success">
        <CheckCircle2 aria-hidden="true" />
        Healthy
      </Badge>
    );
  }
  if (health.consecutiveFailures > 0) {
    return (
      <Badge variant="warning">
        <AlertTriangle aria-hidden="true" />
        Failing
      </Badge>
    );
  }
  return <Badge variant="muted">No success yet</Badge>;
}

export function TestOnlyBadge() {
  return (
    <Badge variant="warning" title="This provider is a test fixture, not a real ticket source">
      Test-only
    </Badge>
  );
}
