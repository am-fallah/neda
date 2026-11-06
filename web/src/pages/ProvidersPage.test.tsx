import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { observabilityApi } from "@/api/endpoints";
import type { ObservabilityOverview } from "@/api/types";
import { ProvidersPage } from "./ProvidersPage";

vi.mock("@/api/endpoints", () => ({
  observabilityApi: { overview: vi.fn(), provider: vi.fn() },
}));

const overviewMock = vi.mocked(observabilityApi.overview);

function makeOverview(): ObservabilityOverview {
  return {
    generatedAt: "2026-11-06T12:00:00Z",
    coverage: {
      checkDetailDays: 14,
      rollupDays: 7,
      rateLimitHistoryDays: 90,
    },
    scheduling: {
      intervalScope: "global",
      configuredIntervalSeconds: 300,
      providerSpacingSeconds: 5,
      queueSampleIntervalSeconds: 15,
      workers: 2,
    },
    scheduler: {
      available: true,
      running: true,
      stopped: false,
      monitoringEnabled: true,
      paused: false,
      lastExecutionAt: "2026-11-06T11:59:58Z",
      lastDispatchAt: "2026-11-06T11:59:40Z",
      dueTargets: 2,
      overdueTargets: 0,
      queuedTargets: 1,
      deferredTargets: 1,
      inFlightTargets: 1,
      nextEligibleDispatchAt: "2026-11-06T12:00:10Z",
      sampledAt: "2026-11-06T11:59:58Z",
      stale: false,
      activeCooldowns: 1,
      degradedProviders: 0,
      incompleteChecks: 0,
      rateLimitEventsDropped: 0,
    },
    trafficSummary: {
      windowSeconds: 86400,
      providerChecks: 288,
      successes: 280,
      failures: 8,
      discarded: 0,
      rateLimitedChecks: 2,
      interrupted: 0,
      incomplete: 0,
      successRate: 0.97,
      averageDurationMs: 812,
      p95DurationMs: 1800,
      p95Method: "disjoint_histogram_upper_bound",
      dispatchAttempts: 290,
      recorded7d: 1400,
      recordedWindowDays: 7,
      historyAvailable: true,
    },
    providers: [
      {
        id: "mrbilit",
        displayName: "MrBilit",
        testOnly: false,
        healthState: "cooling_down",
        healthReasons: ["cooldown_active"],
        flags: {
          degraded: false,
          stale: false,
          rateLimited: true,
          coolingDown: true,
          recovering: false,
          idle: false,
          unknown: false,
        },
        activeTargets: 2,
        lastProviderCheckAt: "2026-11-06T11:55:00Z",
        lastSuccessAt: "2026-11-06T11:50:00Z",
        lastFailureAt: "2026-11-06T11:55:00Z",
        lastErrorKind: "rate_limited",
        consecutiveFailures: 1,
        openEpisode: false,
        adaptive: {
          level: 1,
          adaptiveIntervalSeconds: 600,
          effectiveIntervalSeconds: 600,
          recoverySuccesses: 0,
          requiredSuccesses: 3,
          remainingSuccesses: 3,
          cooldown: {
            status: "active",
            until: "2026-11-06T12:05:00Z",
            remainingSeconds: 300,
          },
          last429At: "2026-11-06T11:55:00Z",
          lastSuccessAt: "2026-11-06T11:50:00Z",
          historyCompleteness: "best_effort",
          historyAvailable: true,
        },
        providerChecks: {
          last1m: 0,
          last5m: 1,
          last15m: 3,
          last1h: 12,
          last24h: 288,
          successes24h: 280,
          failures24h: 8,
          discarded24h: 0,
          rateLimitedChecks24h: 2,
          interrupted24h: 0,
          incomplete24h: 0,
          recorded7d: 1400,
          successRate: 0.97,
          averageDurationMs: 812,
          p95DurationMs: 1800,
          p95Method: "disjoint_histogram_upper_bound",
        },
        queue: {
          available: true,
          sampledAt: "2026-11-06T11:59:58Z",
          stale: false,
          sampleIntervalSeconds: 15,
          aggregation: "sampled_point_in_time",
          due: 2,
          inFlight: 1,
          queued: 1,
          overdue: 0,
          deferred: 1,
          deferredByCooldown: 1,
          deferredBySpacing: 0,
          deferredByConcurrency: 0,
          oldestPendingAgeSeconds: 42,
          nextEligibleAt: "2026-11-06T12:05:00Z",
          blockedByConcurrency: false,
        },
      },
    ],
  };
}

describe("ProvidersPage", () => {
  beforeEach(() => {
    overviewMock.mockReset();
    overviewMock.mockResolvedValue(makeOverview());
  });

  it("labels logical provider checks instead of HTTP requests", async () => {
    render(
      <MemoryRouter>
        <ProvidersPage />
      </MemoryRouter>,
    );

    expect(await screen.findByRole("link", { name: "MrBilit" })).toHaveAttribute(
      "href",
      "/providers/mrbilit",
    );
    expect(screen.getByText("Provider checks, 24h")).toBeInTheDocument();
    expect(screen.getByText("Rate-limited checks, 24h")).toBeInTheDocument();
    expect(screen.queryByText(/HTTP requests/i)).not.toBeInTheDocument();
    expect(screen.getByText("Cooling down")).toBeInTheDocument();
    expect(screen.getByText("5m left")).toBeInTheDocument();
  });

  it("distinguishes degraded and stale evidence from a healthy state", async () => {
    const overview = makeOverview();
    overview.providers[0].healthState = "degraded";
    overview.providers[0].flags.degraded = true;
    overview.providers[0].flags.stale = true;
    overview.providers[0].flags.coolingDown = false;
    overview.providers[0].flags.rateLimited = false;
    overview.providers[0].adaptive.cooldown = {
      status: "none",
      until: null,
      remainingSeconds: 0,
    };
    overviewMock.mockResolvedValueOnce(overview);

    render(
      <MemoryRouter>
        <ProvidersPage />
      </MemoryRouter>,
    );

    expect(await screen.findByText("Degraded")).toBeInTheDocument();
    expect(screen.getByText("Stale evidence")).toBeInTheDocument();
  });
});
