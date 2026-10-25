import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { monitorsApi, overviewApi, providersApi } from "@/api/endpoints";
import {
  makeDeliveryRecord,
  makeMonitor,
  makeOverview,
  makeProvider,
  makeProviderRow,
} from "@/test/fixtures";
import { formatJalaliDate } from "@/lib/dates";
import { OverviewPage } from "./OverviewPage";

vi.mock("@/api/endpoints", () => ({
  overviewApi: { get: vi.fn() },
  monitorsApi: { list: vi.fn() },
  providersApi: { list: vi.fn() },
}));

const overviewMock = vi.mocked(overviewApi.get);
const monitorsMock = vi.mocked(monitorsApi.list);
const providersMock = vi.mocked(providersApi.list);

function renderPage() {
  return render(
    <MemoryRouter>
      <OverviewPage />
    </MemoryRouter>,
  );
}

function monitorSection() {
  const heading = screen.getByRole("heading", { name: "Monitors" });
  const section = heading.closest("section");
  if (!section) throw new Error("Monitors section not found");
  return section;
}

function monitorLinksInOrder() {
  return within(monitorSection())
    .getAllByRole("link")
    .filter((link) => {
      const href = link.getAttribute("href") ?? "";
      return /^\/monitors\/[^/]+$/.test(href);
    });
}

function degradedProvider() {
  return makeProvider({
    id: "mrbilit",
    displayName: "MrBilit",
    testOnly: false,
    health: {
      ...makeProvider().health!,
      lastFailureAt: "2026-10-24T11:56:00Z",
      lastErrorKind: "timeout",
      consecutiveFailures: 3,
      openEpisode: {
        id: 7,
        openedAt: "2026-10-24T11:56:00Z",
        failureCountAtOpen: 3,
        lastErrorKind: "timeout",
      },
      stale: true,
    },
  });
}

function staleProvider() {
  return makeProvider({
    id: "fake",
    displayName: "Test provider",
    health: {
      ...makeProvider().health!,
      openEpisode: null,
      stale: true,
      lastSuccessAt: "2026-10-24T08:00:00Z",
      consecutiveFailures: 0,
    },
  });
}

describe("OverviewPage", () => {
  beforeEach(() => {
    overviewMock.mockReset();
    monitorsMock.mockReset();
    providersMock.mockReset();

    overviewMock.mockResolvedValue(
      makeOverview({ recentEvents: [], recentDeliveries: [] }),
    );
    monitorsMock.mockResolvedValue({ monitors: [makeMonitor()] });
    providersMock.mockResolvedValue({ providers: [makeProvider()] });
  });

  it("renders the empty state without metric cards or empty history panels", async () => {
    overviewMock.mockResolvedValue(
      makeOverview({
        monitors: { total: 0, active: 0, paused: 0, expired: 0 },
        providers: { total: 0, degraded: 0, stale: 0 },
        recentEvents: [],
        recentDeliveries: [],
      }),
    );
    monitorsMock.mockResolvedValue({ monitors: [] });
    providersMock.mockResolvedValue({ providers: [] });

    renderPage();

    expect(await screen.findByText("Create your first monitor")).toBeInTheDocument();
    expect(
      screen.getByText(/No ticket providers are registered yet/),
    ).toBeInTheDocument();
    expect(screen.getByText("No monitors yet")).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Recent availability" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Provider health" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Recent deliveries" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText("Needs attention")).not.toBeInTheDocument();
  });

  it("renders a compact populated monitor list with Jalali date and last check", async () => {
    renderPage();

    const row = await screen.findByRole("link", {
      name: /Tehran to Mashhad/,
    });
    expect(row).toHaveAttribute("href", "/monitors/monitor-1");
    expect(screen.getByText("1 monitor · 1 active")).toBeInTheDocument();
    expect(
      within(row).getAllByText(formatJalaliDate("2026-10-28")).length,
    ).toBeGreaterThan(0);
    expect(within(row).getByText("Active")).toBeInTheDocument();
    expect(
      within(row).getAllByText(/Checked .* ago/).length,
    ).toBeGreaterThan(0);
    expect(screen.queryByText("View all monitors")).not.toBeInTheDocument();
  });

  it("prioritizes active checks needing attention, then other active monitors, then paused", async () => {
    const paused = makeMonitor({
      id: "paused-route",
      label: "Paused route",
      status: "paused",
      enabled: false,
      departureDate: "2026-10-27",
    });
    const active = makeMonitor({
      id: "active-route",
      label: "Active route",
      status: "active",
      departureDate: "2026-10-28",
      providers: [
        makeProviderRow({
          lastOutcome: "success",
          lastAttemptAt: "2026-10-24T11:55:00Z",
        }),
      ],
    });
    const failed = makeMonitor({
      id: "failed-route",
      label: "Failed route",
      status: "active",
      departureDate: "2026-10-29",
      providers: [
        makeProviderRow({
          lastOutcome: "error",
          lastAttemptAt: "2026-10-24T11:55:00Z",
        }),
      ],
    });

    overviewMock.mockResolvedValue(
      makeOverview({
        monitors: { total: 3, active: 2, paused: 1, expired: 0 },
        recentEvents: [],
        recentDeliveries: [],
      }),
    );
    monitorsMock.mockResolvedValue({ monitors: [paused, active, failed] });

    renderPage();

    await screen.findByText("Failed route");
    expect(monitorLinksInOrder().map((link) => link.getAttribute("href"))).toEqual([
      "/monitors/failed-route",
      "/monitors/active-route",
      "/monitors/paused-route",
    ]);
    expect(screen.getByText("Needs attention")).toBeInTheDocument();
  });

  it("surfaces provider and recent delivery exceptions without using lifetime totals", async () => {
    overviewMock.mockResolvedValue(
      makeOverview({
        providers: { total: 2, degraded: 1, stale: 1 },
        recentEvents: [],
        recentDeliveries: [
          makeDeliveryRecord({ id: "d1", status: "sent", eventId: "event-1" }),
          makeDeliveryRecord({ id: "d2", status: "failed", eventId: "event-2" }),
          makeDeliveryRecord({
            id: "d3",
            status: "retry_scheduled",
            eventId: "event-3",
          }),
          makeDeliveryRecord({ id: "d4", status: "pending", eventId: "event-4" }),
          makeDeliveryRecord({ id: "d5", status: "skipped", eventId: "event-5" }),
        ],
      }),
    );
    providersMock.mockResolvedValue({
      providers: [degradedProvider(), staleProvider()],
    });

    renderPage();

    expect(
      await screen.findByText(/1 degraded provider · 1 stale provider/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/1 recent failed delivery · 1 recent retry-scheduled delivery/),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "Review provider health" }),
    ).toHaveAttribute("href", "#provider-health");
    expect(
      screen.getByRole("link", { name: "Inspect recent deliveries" }),
    ).toHaveAttribute("href", "/events?tab=deliveries");
    expect(
      screen.getByText("2 providers · 1 degraded · 1 stale"),
    ).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText("Retry scheduled")).toBeInTheDocument();
    expect(screen.getByText("Sent")).toBeInTheDocument();
    expect(screen.getByText("Queued")).toBeInTheDocument();
    expect(screen.getByText("Skipped")).toBeInTheDocument();
  });

  it("opens the existing provider health table in a focused dialog", async () => {
    const user = userEvent.setup();
    renderPage();

    await screen.findByText("Tehran to Mashhad");
    await user.click(
      screen.getByRole("button", { name: "View provider details" }),
    );

    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByRole("heading", { name: "Provider health" }),
    ).toBeInTheDocument();
    expect(within(dialog).getByRole("table")).toBeInTheDocument();
    expect(
      within(dialog).getByText("Test provider — not a real ticket source"),
    ).toBeInTheDocument();
  });

  it("does not claim healthy providers when provider health fails to load", async () => {
    providersMock.mockRejectedValue(new Error("provider endpoint unavailable"));

    renderPage();

    expect(await screen.findByText("Tehran to Mashhad")).toBeInTheDocument();
    expect(
      screen.getByText("Provider health could not be loaded."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
    expect(screen.queryByText(/healthy/)).not.toBeInTheDocument();
  });

  it("keeps the monitor list usable when only overview data fails", async () => {
    overviewMock.mockRejectedValue(new Error("overview unavailable"));

    renderPage();

    expect(await screen.findByText("Tehran to Mashhad")).toBeInTheDocument();
    expect(
      screen.getByText("Recent availability could not be loaded."),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Recent deliveries could not be loaded."),
    ).toBeInTheDocument();
    expect(screen.getByText("1 monitor · 1 active")).toBeInTheDocument();
    expect(
      screen.queryByText("Could not load the overview"),
    ).not.toBeInTheDocument();
  });

  it("renders loading without removing the primary action", () => {
    overviewMock.mockReturnValue(new Promise(() => {}));
    monitorsMock.mockReturnValue(new Promise(() => {}));
    providersMock.mockReturnValue(new Promise(() => {}));

    renderPage();

    expect(
      screen.getByRole("status", { name: "Loading overview" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "New monitor" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Refresh overview" }),
    ).toBeDisabled();
  });

  it("shows a global error with retry only when all required resources fail", async () => {
    overviewMock.mockRejectedValue(new Error("overview unavailable"));
    monitorsMock.mockRejectedValue(new Error("monitors unavailable"));
    providersMock.mockRejectedValue(new Error("providers unavailable"));

    renderPage();

    expect(
      await screen.findByText("Could not load the overview"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Try again/ })).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "New monitor" }),
    ).toBeInTheDocument();
  });

  it("labels a failed check as availability unknown, not unavailable", async () => {
    const failedMonitor = makeMonitor({
      providers: [
        makeProviderRow({
          lastOutcome: "error",
          lastAttemptAt: "2026-10-24T11:55:00Z",
        }),
      ],
    });
    monitorsMock.mockResolvedValue({ monitors: [failedMonitor] });

    renderPage();

    expect(
      await screen.findAllByText("Last check failed · availability unknown"),
    ).not.toHaveLength(0);
    expect(screen.queryByText(/sold out/i)).not.toBeInTheDocument();
    expect(screen.queryByText("Not reservable")).not.toBeInTheDocument();
  });

  it("caps the monitor list at five and links to the full monitor list", async () => {
    const monitors = Array.from({ length: 6 }, (_, index) =>
      makeMonitor({
        id: `monitor-${index + 1}`,
        label: `Monitor ${index + 1}`,
        departureDate: `2026-10-${String(20 + index).padStart(2, "0")}`,
      }),
    );
    overviewMock.mockResolvedValue(
      makeOverview({
        monitors: { total: 6, active: 6, paused: 0, expired: 0 },
        recentEvents: [],
        recentDeliveries: [],
      }),
    );
    monitorsMock.mockResolvedValue({ monitors });

    renderPage();

    await screen.findByText("Monitor 1");
    expect(monitorLinksInOrder()).toHaveLength(5);
    expect(screen.queryByText("Monitor 6")).not.toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "View all monitors" }),
    ).toHaveAttribute("href", "/monitors");
    expect(screen.getByText("6 monitors · 6 active")).toBeInTheDocument();
  });
});
