import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { observabilityApi } from "@/api/endpoints";
import type { ObservabilityProviderDetail } from "@/api/types";
import { ProviderDetailPage } from "./ProviderDetailPage";

vi.mock("@/api/endpoints", () => ({
  observabilityApi: { overview: vi.fn(), provider: vi.fn() },
}));

const providerMock = vi.mocked(observabilityApi.provider);

function makeDetail(): ObservabilityProviderDetail {
  return {
    id: "p",
    displayName: "Provider P",
    testOnly: false,
    healthState: "rate_limited",
    healthReasons: ["rate_limit_penalty"],
    flags: {
      degraded: false,
      stale: true,
      rateLimited: true,
      coolingDown: false,
      recovering: false,
      idle: false,
      unknown: false,
    },
    activeTargets: 1,
    lastProviderCheckAt: "2026-11-06T11:55:00Z",
    lastSuccessAt: "2026-11-06T11:50:00Z",
    lastFailureAt: "2026-11-06T11:55:00Z",
    lastErrorKind: "rate_limited",
    consecutiveFailures: 1,
    openEpisode: false,
    adaptive: {
      level: 2,
      adaptiveIntervalSeconds: 1200,
      effectiveIntervalSeconds: 1200,
      recoverySuccesses: 0,
      requiredSuccesses: 3,
      remainingSuccesses: 3,
      cooldown: { status: "none", until: null, remainingSeconds: 0 },
      last429At: "2026-11-06T11:55:00Z",
      lastSuccessAt: "2026-11-06T11:50:00Z",
      historyCompleteness: "best_effort",
      historyAvailable: true,
    },
    providerChecks: {
      last1m: 0,
      last5m: 0,
      last15m: 0,
      last1h: 0,
      last24h: 0,
      successes24h: 0,
      failures24h: 0,
      discarded24h: 0,
      rateLimitedChecks24h: 0,
      interrupted24h: 0,
      incomplete24h: 1,
      recorded7d: 0,
      successRate: null,
      averageDurationMs: null,
      p95DurationMs: null,
      p95Method: "disjoint_histogram_upper_bound",
    },
    queue: {
      available: true,
      sampledAt: "2026-11-06T11:59:58Z",
      stale: true,
      sampleIntervalSeconds: 15,
      aggregation: "sampled_point_in_time",
      due: 0,
      inFlight: 0,
      queued: 0,
      overdue: 0,
      deferred: 0,
      deferredByCooldown: 0,
      deferredBySpacing: 0,
      deferredByConcurrency: 0,
      oldestPendingAgeSeconds: null,
      nextEligibleAt: null,
      blockedByConcurrency: false,
    },
    windowSeconds: 86400,
    trafficHistory: [],
    queueHistory: [],
    rateLimitEvents: [
      {
        occurredAt: "2026-11-06T11:55:00Z",
        kind: "rate_limited",
        previousLevel: 1,
        escalationLevel: 2,
        escalated: true,
        maintained: false,
        previousAdaptiveMs: 600000,
        adaptiveMs: 1200000,
        previousCooldownUntil: null,
        cooldownUntil: null,
        retryAfterMs: 0,
        derived: false,
      },
      {
        occurredAt: "2026-11-06T11:55:01Z",
        kind: "cooldown_extended",
        previousLevel: 2,
        escalationLevel: 2,
        escalated: false,
        maintained: false,
        previousAdaptiveMs: 1200000,
        adaptiveMs: 1200000,
        previousCooldownUntil: "2026-11-06T12:05:00Z",
        cooldownUntil: "2026-11-06T12:15:00Z",
        retryAfterMs: 900000,
        derived: false,
      },
    ],
    recentChecks: [
      {
        occurredAt: "2026-11-06T11:55:00Z",
        outcome: "abandoned",
        durationMs: null,
        queueWaitMs: null,
        monitorId: "m1",
        monitorLabel: "Monitor",
        incomplete: true,
      },
    ],
    targets: [],
  };
}

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/providers/p"]}>
      <Routes>
        <Route path="/providers/:id" element={<ProviderDetailPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("ProviderDetailPage", () => {
  beforeEach(() => {
    providerMock.mockReset();
    providerMock.mockResolvedValue(makeDetail());
  });

  it("distinguishes stale, incomplete, and empty history", async () => {
    renderPage();
    expect(await screen.findByText("Stale evidence")).toBeInTheDocument();
    expect(screen.getByText("Snapshot stale")).toBeInTheDocument();
    expect(screen.getAllByText("No traffic recorded").length).toBeGreaterThan(0);
    expect(screen.getByText("Interrupted / incomplete")).toBeInTheDocument();
    expect(
      screen.getByText(/excluded from\s+success, failure, and duration metrics/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/not\s+time-weighted measurements/i)).toBeInTheDocument();
  });

  it("labels rate-limit events by transition semantics", async () => {
    renderPage();
    expect(await screen.findByText("429 observed")).toBeInTheDocument();
    expect(screen.getByText(/level 1 → 2 \(escalated\)/)).toBeInTheDocument();
    expect(screen.getByText("Cooldown extended")).toBeInTheDocument();
    expect(screen.queryByText(/HTTP requests/i)).not.toBeInTheDocument();
  });
});
