import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { overviewApi, providersApi } from "@/api/endpoints";
import {
  makeDeliveryRecord,
  makeEvent,
  makeOverview,
  makeProvider,
} from "@/test/fixtures";
import { OverviewPage } from "./OverviewPage";

vi.mock("@/api/endpoints", () => ({
  overviewApi: { get: vi.fn() },
  providersApi: { list: vi.fn() },
}));

const overviewMock = vi.mocked(overviewApi.get);
const providersMock = vi.mocked(providersApi.list);

function renderPage() {
  return render(
    <MemoryRouter>
      <OverviewPage />
    </MemoryRouter>,
  );
}

describe("OverviewPage", () => {
  beforeEach(() => {
    overviewMock.mockReset();
    providersMock.mockReset();
    providersMock.mockResolvedValue({ providers: [makeProvider()] });
  });

  it("separates provider health and failed deliveries from ticket status", async () => {
    overviewMock.mockResolvedValue(
      makeOverview({
        providers: { total: 1, degraded: 1, stale: 0 },
        deliveries: {
          total: 2,
          pending: 0,
          sending: 0,
          sent: 1,
          retryScheduled: 0,
          failed: 1,
          skipped: 0,
        },
        recentEvents: [makeEvent()],
        recentDeliveries: [
          makeDeliveryRecord({ id: "d1", status: "failed", sentAt: null }),
          makeDeliveryRecord({ id: "d2", status: "sent" }),
        ],
      }),
    );
    renderPage();

    expect(
      await screen.findByText("1 provider is degraded"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/Provider health is separate from ticket availability/),
    ).toBeInTheDocument();
    expect(
      screen.getByText("1 notification failed to deliver"),
    ).toBeInTheDocument();
    expect(screen.getAllByText("Failed").length).toBeGreaterThan(0);
  });

  it("shows a clear empty state and provider warning for a fresh install", async () => {
    overviewMock.mockResolvedValue(
      makeOverview({
        monitors: { total: 0, active: 0, paused: 0, expired: 0 },
        providers: { total: 0, degraded: 0, stale: 0 },
        deliveries: {
          total: 0,
          pending: 0,
          sending: 0,
          sent: 0,
          retryScheduled: 0,
          failed: 0,
          skipped: 0,
        },
        recentEvents: [],
        recentDeliveries: [],
      }),
    );
    providersMock.mockResolvedValue({ providers: [] });
    renderPage();

    expect(
      await screen.findByText("No ticket providers are registered"),
    ).toBeInTheDocument();
    expect(screen.getByText("Create your first monitor")).toBeInTheDocument();
    expect(screen.getAllByText("No events yet").length).toBeGreaterThan(0);
    expect(screen.getByText("No deliveries yet")).toBeInTheDocument();
  });

  it("surfaces overview request failures with a retry", async () => {
    overviewMock.mockRejectedValue(
      new Error("server unavailable"),
    );
    renderPage();
    expect(
      await screen.findByText("Could not load the overview"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Try again/ })).toBeInTheDocument();
  });
});
