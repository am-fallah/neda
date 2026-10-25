import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/client";
import { providersApi } from "@/api/endpoints";
import { MrBilitSearchForm } from "./MrBilitSearchForm";
import { makeProvider } from "@/test/fixtures";
import type { ProviderSearchResult, PushConfig, Settings } from "@/api/types";

vi.mock("@/api/endpoints", () => ({
  providersApi: { search: vi.fn() },
}));

const searchMock = vi.mocked(providersApi.search);

const provider = makeProvider({
  id: "mrbilit",
  displayName: "MrBilit",
  testOnly: false,
  capabilities: {
    requiresOriginId: true,
    requiresDestinationId: true,
    supportsBookingUrl: true,
    supportsGender: false,
    requiresOptionSelection: true,
    maxPassengers: 9,
  },
});

const settings: Settings = {
  pollIntervalSeconds: 300,
  pushEnabled: true,
  telegram: {
    available: true,
    configured: true,
    readable: true,
  },
};

const pushConfig: PushConfig = {
  enabled: true,
  available: true,
  subjectConfigured: true,
  subscriptions: [],
};

function searchResult(): ProviderSearchResult {
  return {
    url: "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24",
    slug: "yazd-mashhad",
    jalaliDate: "1405-08-24",
    departureDate: "2026-11-15",
    route: {
      originId: "219",
      destinationId: "191",
      originName: "Yazd",
      destinationName: "Mashhad",
    },
    passengers: { adults: 1, children: 0, infants: 0 },
    trains: [
      {
        journeyKey: "id:9001",
        nativeId: "9001",
        trainNumber: "682",
        operator: "Raja",
        originName: "Yazd",
        destinationName: "Mashhad",
        departureAt: "2026-11-15T11:20:00Z",
        arrivalAt: "2026-11-16T03:00:00Z",
        classes: [
          {
            offeringKey: "id:9102",
            nativeId: "9102",
            wagonId: "9202",
            wagonName: "Five-star Samen",
            className: "Samen",
            compartmentType: "4-person compartment",
            capacity: 3,
            state: "reservable",
            reason: "positive_capacity_and_flags",
          },
        ],
      },
    ],
    warnings: [],
    observedAt: "2026-10-25T12:00:00Z",
  };
}

describe("MrBilitSearchForm", () => {
  beforeEach(() => {
    searchMock.mockReset();
  });

  it("searches, selects a class, and submits the selected option", async () => {
    searchMock.mockResolvedValue(searchResult());
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <MrBilitSearchForm
          providers={[provider]}
          settings={settings}
          pushConfig={pushConfig}
          onSubmit={onSubmit}
        />
      </MemoryRouter>,
    );

    await user.type(
      screen.getByLabelText(/MrBilit train search URL/i),
      "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24&utm_source=test",
    );
    await user.click(screen.getByRole("button", { name: /find trains/i }));
    expect(await screen.findByText(/Train 682/)).toBeInTheDocument();
    expect(screen.getByText(/Five-star Samen/)).toBeInTheDocument();

    await user.click(screen.getByRole("checkbox"));
    expect(screen.getByText(/Selected options \(1\)/)).toBeInTheDocument();
    expect(screen.getByText(/Every 300 seconds/)).toBeInTheDocument();
    expect(screen.getByText(/Configured/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /create monitor/i }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const payload = onSubmit.mock.calls[0][0];
    expect(payload).toMatchObject({
      label: "Yazd → Mashhad — 2026-11-15",
      originName: "Yazd",
      destinationName: "Mashhad",
      departureDate: "2026-11-15",
      adults: 1,
      children: 0,
      infants: 0,
      providers: [
        {
          providerId: "mrbilit",
          originId: "219",
          destinationId: "191",
          bookingSlug: "yazd-mashhad",
          selections: [
            {
              journeyKey: "id:9001",
              offeringKey: "id:9102",
              trainNumber: "682",
              wagonId: "9202",
            },
          ],
        },
      ],
    });
  });

  it("shows an actionable URL error from the server", async () => {
    searchMock.mockRejectedValue(
      new ApiError({
        status: 422,
        code: "unsupported_url",
        message: "Use a MrBilit train search URL.",
        fields: { url: "Use a MrBilit train search URL." },
      }),
    );
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <MrBilitSearchForm
          providers={[provider]}
          settings={settings}
          pushConfig={pushConfig}
          onSubmit={vi.fn()}
        />
      </MemoryRouter>,
    );
    await user.type(
      screen.getByLabelText(/MrBilit train search URL/i),
      "https://example.com/nope",
    );
    await user.click(screen.getByRole("button", { name: /find trains/i }));
    expect(
      await screen.findAllByText(/Use a MrBilit train search URL/),
    ).not.toHaveLength(0);
  });

  it("shows a valid empty result without treating it as sold out", async () => {
    searchMock.mockResolvedValue({ ...searchResult(), trains: [] });
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <MrBilitSearchForm
          providers={[provider]}
          settings={settings}
          pushConfig={pushConfig}
          onSubmit={vi.fn()}
        />
      </MemoryRouter>,
    );
    await user.type(
      screen.getByLabelText(/MrBilit train search URL/i),
      "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24",
    );
    await user.click(screen.getByRole("button", { name: /find trains/i }));
    expect(await screen.findByText(/No trains returned/i)).toBeInTheDocument();
    expect(screen.getByText(/not treated as sold out/i)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /create monitor/i }),
    ).not.toBeInTheDocument();
  });

  it("does not permit creating without a selection", async () => {
    searchMock.mockResolvedValue(searchResult());
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <MrBilitSearchForm
          providers={[provider]}
          settings={settings}
          pushConfig={pushConfig}
          onSubmit={vi.fn()}
        />
      </MemoryRouter>,
    );
    await user.type(
      screen.getByLabelText(/MrBilit train search URL/i),
      "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24",
    );
    await user.click(screen.getByRole("button", { name: /find trains/i }));
    expect(
      await screen.findByRole("button", { name: /create monitor/i }),
    ).toBeDisabled();
  });
});
