import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { monitorsApi, providersApi } from "@/api/endpoints";
import {
  makeCheck,
  makeMonitor,
  makeMonitorDetail,
  makeObservation,
  makeProvider,
  makeProviderDetail,
} from "@/test/fixtures";
import { MonitorDetailPage } from "./MonitorDetailPage";

vi.mock("@/api/endpoints", () => ({
  monitorsApi: {
    get: vi.fn(),
    pause: vi.fn(),
    resume: vi.fn(),
    remove: vi.fn(),
  },
  providersApi: { list: vi.fn() },
}));

const getMock = vi.mocked(monitorsApi.get);
const pauseMock = vi.mocked(monitorsApi.pause);
const resumeMock = vi.mocked(monitorsApi.resume);
const removeMock = vi.mocked(monitorsApi.remove);
const providersMock = vi.mocked(providersApi.list);

function renderPage() {
  return render(
    <MemoryRouter initialEntries={["/monitors/monitor-1"]}>
      <Routes>
        <Route path="/monitors/:id" element={<MonitorDetailPage />} />
        <Route path="/monitors" element={<div>Monitors list</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("MonitorDetailPage", () => {
  beforeEach(() => {
    getMock.mockReset();
    pauseMock.mockReset();
    resumeMock.mockReset();
    removeMock.mockReset();
    providersMock.mockReset();
    providersMock.mockResolvedValue({ providers: [makeProvider()] });
  });

  it("shows a failed check as unknown instead of no tickets", async () => {
    getMock.mockResolvedValue(
      makeMonitorDetail({
        providers: [
          makeProviderDetail({
            lastOutcome: "error",
            lastErrorKind: "timeout",
            lastSuccessAt: null,
            observations: [],
            recentChecks: [
              makeCheck({
                outcome: "error",
                errorKind: "timeout",
                reservableCount: 0,
                offeringCount: 0,
                unknownCount: 0,
              }),
            ],
          }),
        ],
      }),
    );
    renderPage();
    expect(
      (await screen.findAllByText("Last check failed; availability unknown"))
        .length,
    ).toBeGreaterThan(0);
    expect(
      screen.queryByText("No reservable tickets confirmed"),
    ).not.toBeInTheDocument();
  });

  it("distinguishes confirmed not-reservable observations from unknown", async () => {
    getMock.mockResolvedValue(
      makeMonitorDetail({
        providers: [
          makeProviderDetail({
            observations: [
              makeObservation({
                state: "not_reservable",
                bookingUrl: undefined,
                lastCapacity: 0,
              }),
            ],
            recentChecks: [makeCheck({ reservableCount: 0, notReservableCount: 1 })],
          }),
        ],
      }),
    );
    renderPage();
    expect(
      await screen.findByText("No reservable tickets confirmed"),
    ).toBeInTheDocument();
    expect(screen.getByText("Not reservable")).toBeInTheDocument();
    expect(
      screen.queryByText("Last check failed; availability unknown"),
    ).not.toBeInTheDocument();
  });

  it("shows unknown and missing observations without treating them as sold out", async () => {
    getMock.mockResolvedValue(
      makeMonitorDetail({
        providers: [
          makeProviderDetail({
            observations: [
              makeObservation({
                state: "unknown",
                missing: true,
                lastReason: "contradictory_evidence",
              }),
            ],
            recentChecks: [makeCheck({ reservableCount: 0, unknownCount: 1 })],
          }),
        ],
      }),
    );
    renderPage();
    expect(await screen.findByText("Some inventory is unknown")).toBeInTheDocument();
    expect(screen.getByText("Unknown · not returned")).toBeInTheDocument();
    expect(
      screen.getByText(/Unknown is not treated as unavailable/),
    ).toBeInTheDocument();
  });

  it("pauses and resumes an active monitor", async () => {
    pauseMock.mockResolvedValue(makeMonitor());
    getMock.mockResolvedValue(makeMonitorDetail());
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { level: 1, name: "Tehran to Mashhad" });
    await user.click(screen.getByRole("button", { name: "Pause" }));
    await waitFor(() =>
      expect(pauseMock).toHaveBeenCalledWith("monitor-1"),
    );
  });

  it("resumes a paused monitor", async () => {
    getMock.mockResolvedValue(
      makeMonitorDetail({ status: "paused", enabled: false }),
    );
    resumeMock.mockResolvedValue(makeMonitor());
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { level: 1, name: "Tehran to Mashhad" });
    await user.click(screen.getByRole("button", { name: "Resume" }));
    await waitFor(() =>
      expect(resumeMock).toHaveBeenCalledWith("monitor-1"),
    );
  });

  it("requires confirmation before deleting", async () => {
    getMock.mockResolvedValue(makeMonitorDetail());
    removeMock.mockResolvedValue(undefined);
    const user = userEvent.setup();
    renderPage();
    await screen.findByRole("heading", { level: 1, name: "Tehran to Mashhad" });
    await user.click(screen.getByRole("button", { name: "Delete" }));
    expect(
      screen.getByRole("alertdialog", { name: /Delete “Tehran to Mashhad”/ }),
    ).toBeInTheDocument();
    expect(removeMock).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Delete monitor" }));
    await waitFor(() =>
      expect(removeMock).toHaveBeenCalledWith("monitor-1"),
    );
  });
});
