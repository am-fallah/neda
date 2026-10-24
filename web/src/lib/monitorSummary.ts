import type {
  DeliverySummary,
  InventoryObservation,
  Monitor,
  MonitorProvider,
  CheckRun,
} from "@/api/types";

export type Tone = "success" | "warning" | "destructive" | "info" | "muted";

export interface Summary {
  tone: Tone;
  title: string;
  detail: string;
}

export function activeProviders(monitor: Monitor): MonitorProvider[] {
  return monitor.providers.filter((provider) => provider.active);
}

/**
 * Summarizes check outcomes for a monitor without inferring ticket
 * availability from provider errors.
 */
export function summarizeMonitorChecks(monitor: Monitor): Summary {
  if (monitor.status === "paused") {
    return {
      tone: "muted",
      title: "Monitoring is paused",
      detail: "No new checks run until the monitor is resumed.",
    };
  }
  if (monitor.status === "expired") {
    return {
      tone: "muted",
      title: "Departure day has ended",
      detail:
        "Monitoring expired after the departure day ended in Tehran. Edit the date to monitor a future departure.",
    };
  }
  const providers = activeProviders(monitor);
  if (providers.length === 0) {
    return {
      tone: "warning",
      title: "No active providers",
      detail: "This monitor has no active provider target to check.",
    };
  }
  const failed = providers.filter(
    (provider) => provider.lastOutcome === "error",
  );
  const succeeded = providers.filter(
    (provider) =>
      provider.lastOutcome === "success" || provider.lastOutcome === "empty",
  );
  if (failed.length > 0 && succeeded.length === 0) {
    return {
      tone: "destructive",
      title: "Last check failed; availability unknown",
      detail:
        "A provider error or timeout is never treated as evidence that tickets are unavailable.",
    };
  }
  if (failed.length > 0) {
    return {
      tone: "warning",
      title: "Some provider checks failed",
      detail:
        "Availability is only known for providers whose last check succeeded. Failed checks are not sold-out signals.",
    };
  }
  if (succeeded.length > 0) {
    return {
      tone: "info",
      title: "Last check succeeded",
      detail:
        "Open the monitor for the last observed inventory. Successful checks are not a live guarantee.",
    };
  }
  return {
    tone: "muted",
    title: "No checks have completed yet",
    detail: "The first scheduled check has not finished.",
  };
}

export function summarizeObservations(
  observations: InventoryObservation[],
  latestCheck: CheckRun | undefined,
): Summary {
  if (latestCheck?.outcome === "error") {
    return {
      tone: "destructive",
      title: "Last check failed; availability unknown",
      detail:
        "Provider errors, timeouts, and malformed responses never mean sold out.",
    };
  }
  if (observations.length === 0) {
    if (latestCheck?.outcome === "empty") {
      return {
        tone: "muted",
        title: "Last successful check returned no offerings",
        detail:
          "A valid empty result is not proof that every ticket is sold out.",
      };
    }
    if (latestCheck?.outcome === "success") {
      return {
        tone: "muted",
        title: "No inventory offerings were returned",
        detail:
          "This is the last successful observation, not a live guarantee.",
      };
    }
    return {
      tone: "muted",
      title: "No observations yet",
      detail: "Wait for the first successful provider check.",
    };
  }

  const reservable = observations.filter(
    (observation) =>
      observation.state === "reservable" && !observation.missing,
  ).length;
  const unknown = observations.filter(
    (observation) =>
      observation.state === "unknown" || observation.missing,
  ).length;
  const notReservable = observations.filter(
    (observation) =>
      observation.state === "not_reservable" && !observation.missing,
  ).length;

  if (reservable > 0) {
    return {
      tone: "success",
      title: `Reservable offering${reservable === 1 ? "" : "s"} observed`,
      detail:
        "This is the last observed state, not a live guarantee. Booking links are shown only when validated.",
    };
  }
  if (unknown > 0) {
    return {
      tone: "warning",
      title: "Some inventory is unknown",
      detail: `${unknown} of ${observations.length} ${
        observations.length === 1 ? "offering is" : "offerings are"
      } unknown or no longer returned. Unknown is not treated as unavailable.`,
    };
  }
  if (notReservable > 0) {
    return {
      tone: "muted",
      title: "No reservable tickets confirmed",
      detail:
        "The last successful check classified every returned offering as not reservable. This is not proof that tickets are sold out.",
    };
  }
  return {
    tone: "muted",
    title: "No decisive inventory state",
    detail: "The last observation did not provide enough evidence.",
  };
}

export function latestCheck(
  checks: CheckRun[] | undefined,
): CheckRun | undefined {
  return checks && checks.length > 0 ? checks[0] : undefined;
}

export function deliverySummaryText(summary: DeliverySummary): string {
  if (!summary || summary.total === 0) return "No delivery records";
  const parts: string[] = [];
  if (summary.sent > 0) parts.push(`${summary.sent} sent`);
  if (summary.failed > 0) parts.push(`${summary.failed} failed`);
  if (summary.retryScheduled > 0) {
    parts.push(`${summary.retryScheduled} retry scheduled`);
  }
  if (summary.pending > 0) parts.push(`${summary.pending} queued`);
  if (summary.sending > 0) parts.push(`${summary.sending} sending`);
  if (summary.skipped > 0) parts.push(`${summary.skipped} skipped`);
  return parts.length > 0 ? parts.join(", ") : "No delivery attempts";
}

export function deliverySummaryTone(summary: DeliverySummary): Tone {
  if (!summary || summary.total === 0) return "muted";
  if (summary.failed > 0) return "destructive";
  if (summary.retryScheduled > 0 || summary.pending > 0 || summary.sending > 0) {
    return "warning";
  }
  if (summary.sent === summary.total) return "success";
  return "muted";
}

export function isFullyDelivered(summary: DeliverySummary): boolean {
  return Boolean(summary) && summary.total > 0 && summary.sent === summary.total;
}
