import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  deliveriesApi,
  eventsApi,
  monitorsApi,
  providersApi,
} from "@/api/endpoints";
import {
  makeDeliveryRecord,
  makeEvent,
  makeMonitor,
  makeProvider,
} from "@/test/fixtures";
import { HistoryPage } from "./HistoryPage";

vi.mock("@/api/endpoints", () => ({
  eventsApi: { list: vi.fn(), get: vi.fn() },
  deliveriesApi: { list: vi.fn() },
  monitorsApi: { list: vi.fn() },
  providersApi: { list: vi.fn() },
}));

const eventsMock = vi.mocked(eventsApi.list);
const deliveriesMock = vi.mocked(deliveriesApi.list);
const monitorsMock = vi.mocked(monitorsApi.list);
const providersMock = vi.mocked(providersApi.list);

function renderPage(url = "/events") {
  return render(
    <MemoryRouter initialEntries={[url]}>
      <Routes>
        <Route path="/events" element={<HistoryPage />} />
        <Route path="/events/:id" element={<div>Event detail destination</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("HistoryPage", () => {
  beforeEach(() => {
    eventsMock.mockReset();
    deliveriesMock.mockReset();
    monitorsMock.mockReset();
    providersMock.mockReset();
    monitorsMock.mockResolvedValue({ monitors: [makeMonitor()] });
    providersMock.mockResolvedValue({ providers: [makeProvider()] });
    eventsMock.mockResolvedValue({ events: [makeEvent()], nextCursor: "" });
    deliveriesMock.mockResolvedValue({
      deliveries: [makeDeliveryRecord()],
      nextCursor: "",
    });
  });

  it("loads delivery history with the failed status filter", async () => {
    deliveriesMock.mockResolvedValue({
      deliveries: [
        makeDeliveryRecord({
          status: "failed",
          sentAt: null,
          lastErrorKind: "unauthorized",
        }),
      ],
      nextCursor: "",
    });
    renderPage("/events?tab=deliveries&status=failed");
    expect(screen.getByText("Delivery status is not detection")).toBeInTheDocument();
    const table = await screen.findByRole("table");
    const rows = within(table).getAllByRole("row");
    expect(within(rows[1]).getByText("Failed")).toBeInTheDocument();
    expect(within(rows[1]).queryByText("Sent")).not.toBeInTheDocument();
    expect(deliveriesMock).toHaveBeenCalledWith(
      expect.objectContaining({
        status: "failed",
        channel: "",
        limit: 25,
      }),
      expect.any(AbortSignal),
    );
  });

  it("filters events and loads additional pages by cursor", async () => {
    eventsMock.mockImplementation(async (params) =>
      params.cursor
        ? {
            events: [makeEvent({ id: "event-2", kind: "provider_degraded" })],
            nextCursor: "",
          }
        : { events: [makeEvent()], nextCursor: "next-1" },
    );
    const user = userEvent.setup();
    renderPage();
    expect(
      (await screen.findAllByText("Tehran to Mashhad")).length,
    ).toBeGreaterThan(0);

    await user.selectOptions(
      screen.getByLabelText("Event type"),
      "provider_degraded",
    );
    await waitFor(() =>
      expect(eventsMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ kind: "provider_degraded" }),
        expect.any(AbortSignal),
      ),
    );

    await user.selectOptions(screen.getByLabelText("Event type"), "");
    const loadMore = await screen.findByRole("button", {
      name: "Load more events",
    });
    await user.click(loadMore);
    await waitFor(() =>
      expect(eventsMock).toHaveBeenLastCalledWith(
        expect.objectContaining({ cursor: "next-1" }),
      ),
    );
    expect(
      (await screen.findAllByText("Provider degraded")).length,
    ).toBeGreaterThan(0);
  });
});
