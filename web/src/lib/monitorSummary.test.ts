import { describe, expect, it } from "vitest";
import {
  deliverySummaryText,
  deliverySummaryTone,
  isFullyDelivered,
  summarizeMonitorChecks,
  summarizeObservations,
} from "./monitorSummary";
import {
  makeCheck,
  makeMonitor,
  makeObservation,
  makeProviderRow,
} from "@/test/fixtures";

describe("monitor check summaries", () => {
  it("reports a failed last check as unknown, never sold out", () => {
    const monitor = makeMonitor({
      providers: [makeProviderRow({ lastOutcome: "error", lastErrorKind: "timeout" })],
    });
    const summary = summarizeMonitorChecks(monitor);
    expect(summary.tone).toBe("destructive");
    expect(summary.title).toContain("availability unknown");
    expect(summary.detail).toContain("never treated");
    expect(`${summary.title} ${summary.detail}`.toLowerCase()).not.toContain(
      "sold out",
    );
  });

  it("does not infer ticket availability from a successful check", () => {
    const summary = summarizeMonitorChecks(makeMonitor());
    expect(summary.tone).toBe("info");
    expect(summary.title).toBe("Last check succeeded");
    expect(summary.detail).toContain("not a live guarantee");
  });

  it("distinguishes paused, expired, and never-checked monitors", () => {
    expect(summarizeMonitorChecks(makeMonitor({ status: "paused" })).title).toBe(
      "Monitoring is paused",
    );
    expect(
      summarizeMonitorChecks(makeMonitor({ status: "expired" })).title,
    ).toBe("Departure day has ended");
    expect(
      summarizeMonitorChecks(
        makeMonitor({
          providers: [
            makeProviderRow({
              lastOutcome: undefined,
              lastAttemptAt: null,
              lastSuccessAt: null,
            }),
          ],
        }),
      ).title,
    ).toBe("No checks have completed yet");
  });

  it("warns when only some provider checks failed", () => {
    const summary = summarizeMonitorChecks(
      makeMonitor({
        providers: [
          makeProviderRow({ providerId: "fake", lastOutcome: "error" }),
          makeProviderRow({ providerId: "other", lastOutcome: "success" }),
        ],
      }),
    );
    expect(summary.tone).toBe("warning");
    expect(summary.detail).toContain("only known for providers");
  });
});

describe("observation summaries", () => {
  it("treats a failed check as unknown even when old observations exist", () => {
    const summary = summarizeObservations(
      [makeObservation({ state: "not_reservable" })],
      makeCheck({ outcome: "error", errorKind: "timeout" }),
    );
    expect(summary.tone).toBe("destructive");
    expect(summary.title).toContain("availability unknown");
  });

  it("does not call a valid empty result sold out", () => {
    const summary = summarizeObservations(
      [],
      makeCheck({
        outcome: "empty",
        offeringCount: 0,
        reservableCount: 0,
        notReservableCount: 0,
        unknownCount: 0,
      }),
    );
    expect(summary.title).toContain("no offerings");
    expect(summary.detail).toContain("not proof");
    expect(summary.detail.toLowerCase()).toContain("sold out");
  });

  it("separates confirmed not-reservable from unknown", () => {
    const notReservable = summarizeObservations(
      [
        makeObservation({ state: "not_reservable", missing: false }),
        makeObservation({
          journeyKey: "journey-2",
          offeringKey: "offering-2",
          state: "not_reservable",
          missing: false,
        }),
      ],
      makeCheck(),
    );
    expect(notReservable.title).toBe("No reservable tickets confirmed");
    expect(notReservable.detail).toContain("not proof");

    const unknown = summarizeObservations(
      [
        makeObservation({ state: "unknown" }),
        makeObservation({
          journeyKey: "journey-2",
          offeringKey: "offering-2",
          state: "not_reservable",
        }),
      ],
      makeCheck(),
    );
    expect(unknown.tone).toBe("warning");
    expect(unknown.detail).toContain("Unknown is not treated as unavailable");
  });

  it("marks missing offerings as unknown", () => {
    const summary = summarizeObservations(
      [makeObservation({ missing: true })],
      makeCheck({ outcome: "success" }),
    );
    expect(summary.tone).toBe("warning");
    expect(summary.title).toBe("Some inventory is unknown");
  });

  it("reports reservable observations as historical, not live", () => {
    const summary = summarizeObservations([makeObservation()], makeCheck());
    expect(summary.tone).toBe("success");
    expect(summary.detail).toContain("not a live guarantee");
  });
});

describe("delivery summaries", () => {
  const base = {
    total: 2,
    pending: 0,
    sending: 0,
    sent: 0,
    retryScheduled: 0,
    failed: 0,
    skipped: 0,
    attempted: 0,
    terminal: 0,
  };

  it("never counts queued or failed deliveries as delivered", () => {
    const summary = { ...base, pending: 1, failed: 1 };
    expect(isFullyDelivered(summary)).toBe(false);
    expect(deliverySummaryTone(summary)).toBe("destructive");
    expect(deliverySummaryText(summary)).toContain("1 failed");
    expect(deliverySummaryText(summary)).toContain("1 queued");
  });

  it("reports retry-scheduled deliveries as not fully delivered", () => {
    const summary = { ...base, sent: 1, retryScheduled: 1 };
    expect(isFullyDelivered(summary)).toBe(false);
    expect(deliverySummaryTone(summary)).toBe("warning");
    expect(deliverySummaryText(summary)).toContain("retry scheduled");
  });

  it("recognizes a fully delivered event", () => {
    const summary = { ...base, sent: 2, attempted: 2, terminal: 2 };
    expect(isFullyDelivered(summary)).toBe(true);
    expect(deliverySummaryTone(summary)).toBe("success");
    expect(deliverySummaryText(summary)).toBe("2 sent");
  });
});
