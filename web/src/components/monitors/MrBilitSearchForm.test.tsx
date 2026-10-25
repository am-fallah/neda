import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/client";
import { providersApi } from "@/api/endpoints";
import { MrBilitSearchForm } from "./MrBilitSearchForm";
import { makeProvider } from "@/test/fixtures";
import type {
  ProviderSearchClass,
  ProviderSearchResult,
  PushConfig,
  Settings,
} from "@/api/types";

vi.mock("@/api/endpoints", () => ({
  providersApi: { search: vi.fn() },
}));

const searchMock = vi.mocked(providersApi.search);
const SEARCH_URL =
  "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24";

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

function makeClass(
  overrides: Partial<ProviderSearchClass> = {},
): ProviderSearchClass {
  return {
    offeringKey: "id:9102",
    nativeId: "9102",
    wagonId: "9202",
    wagonName: "Five-star Samen",
    className: "Samen",
    compartmentType: "4-person compartment",
    capacity: 3,
    state: "reservable",
    ...overrides,
  };
}

function searchResult(
  overrides: Partial<ProviderSearchResult> = {},
): ProviderSearchResult {
  return {
    url: SEARCH_URL,
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
          makeClass(),
          makeClass({
            offeringKey: "id:9104",
            nativeId: "9104",
            capacity: 0,
            state: "not_reservable",
          }),
        ],
      },
    ],
    warnings: [],
    observedAt: "2026-10-25T12:00:00Z",
    ...overrides,
  };
}

function renderForm(onSubmit = vi.fn().mockResolvedValue(undefined)) {
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
  return { user, onSubmit };
}

async function runSearch(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText(/MrBilit search URL/i), SEARCH_URL);
  await user.click(screen.getByRole("button", { name: /find trains/i }));
  await screen.findByRole("heading", { name: /available journeys/i });
}

describe("MrBilitSearchForm", () => {
  beforeEach(() => {
    searchMock.mockReset();
  });

  it("searches, groups duplicate class names, selects an exact option, and submits it", async () => {
    searchMock.mockResolvedValue(searchResult());
    const { user, onSubmit } = renderForm();

    await runSearch(user);

    expect(screen.getByText(/Train 682/)).toBeInTheDocument();
    expect(screen.getByText(/^Samen$/)).toBeInTheDocument();
    await waitFor(() =>
      expect(document.activeElement).toHaveAttribute(
        "id",
        "search-results-heading",
      ),
    );
    expect(screen.getAllByRole("checkbox")).toHaveLength(2);
    expect(screen.getByText(/^Option 1$/)).toBeInTheDocument();
    expect(screen.getByText(/^Option 2$/)).toBeInTheDocument();
    expect(
      screen.queryByText(/loaded from the url/i),
    ).not.toBeInTheDocument();

    await user.click(screen.getAllByRole("checkbox")[0]);
    expect(screen.getByText("1 of 2 selected")).toBeInTheDocument();
    expect(screen.getByLabelText(/monitor label/i)).toHaveValue(
      "Yazd → Mashhad — 2026-11-15",
    );

    await user.click(
      screen.getByRole("button", { name: /create monitor/i }),
    );
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
              offeringNativeId: "9102",
              trainNumber: "682",
              wagonId: "9202",
              wagonName: "Five-star Samen",
              className: "Samen",
              compartmentType: "4-person compartment",
            },
          ],
        },
      ],
    });
  });

  it("prefills passenger counts only when all three valid URL values exist", async () => {
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

    const url = screen.getByLabelText(/MrBilit search URL/i);
    await user.type(
      url,
      `${SEARCH_URL}&adultCount=2&childCount=1&infantCount=0`,
    );
    expect(screen.getByLabelText(/adults/i)).toHaveValue(2);
    expect(screen.getByLabelText(/children/i)).toHaveValue(1);
    expect(screen.getByLabelText(/infants/i)).toHaveValue(0);
    expect(screen.getByText(/loaded from the url/i)).toBeInTheDocument();

    await user.clear(url);
    await user.type(
      url,
      "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24&adultCount=2",
    );
    expect(screen.getByText(/only some passenger counts/i)).toBeInTheDocument();
    // Partial counts are not applied and do not silently overwrite the
    // explicit values that were previously loaded.
    expect(screen.getByLabelText(/adults/i)).toHaveValue(2);
    expect(screen.getByLabelText(/children/i)).toHaveValue(1);
  });

  it("keeps distinct wagon identities separate even when the class name matches", async () => {
    searchMock.mockResolvedValue(
      searchResult({
        trains: [
          {
            ...searchResult().trains[0],
            classes: [
              makeClass({
                offeringKey: "id:a",
                nativeId: "a",
                wagonId: "w1",
                wagonName: "",
              }),
              makeClass({
                offeringKey: "id:b",
                nativeId: "b",
                wagonId: "w2",
                wagonName: "Other wagon",
              }),
            ],
          },
        ],
      }),
    );
    const { user } = renderForm();
    await runSearch(user);

    expect(screen.getAllByRole("checkbox")).toHaveLength(2);
    expect(
      screen.getByText("Wagon w1 · 4-person compartment"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Other wagon · 4-person compartment"),
    ).toBeInTheDocument();
  });

  it("clears selections and disables stale results when the URL changes", async () => {
    searchMock.mockResolvedValue(searchResult());
    const { user } = renderForm();
    await runSearch(user);

    await user.click(screen.getAllByRole("checkbox")[0]);
    expect(screen.getByText("1 of 2 selected")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /change search/i }));
    const url = screen.getByLabelText(/MrBilit search URL/i);
    await user.clear(url);
    await user.type(url, SEARCH_URL.replace("1405-08-24", "1405-08-25"));

    expect(screen.getByText(/search settings changed/i)).toBeInTheDocument();
    expect(screen.getByText("None selected")).toBeInTheDocument();
    for (const box of screen.getAllByRole("checkbox")) {
      expect(box).toBeDisabled();
    }
    expect(
      screen.getByRole("button", { name: /create monitor/i }),
    ).toBeDisabled();
    expect(
      screen.getAllByText(/search again to refresh/i).length,
    ).toBeGreaterThan(0);
  });

  it("ignores an in-flight search response after the inputs change", async () => {
    let resolveSearch: (value: ProviderSearchResult) => void = () => undefined;
    searchMock.mockReturnValue(
      new Promise<ProviderSearchResult>((resolve) => {
        resolveSearch = resolve;
      }),
    );
    const { user } = renderForm();
    const url = screen.getByLabelText(/MrBilit search URL/i);
    await user.type(url, SEARCH_URL);
    await user.click(screen.getByRole("button", { name: /find trains/i }));

    expect(
      screen.getByRole("button", { name: /searching mrbilit/i }),
    ).toBeDisabled();
    expect(
      screen.getByRole("status", { name: /searching mrbilit/i }),
    ).toBeInTheDocument();

    await user.clear(url);
    await act(async () => {
      resolveSearch(searchResult());
    });

    await waitFor(() => {
      expect(screen.queryByText(/Train 682/)).not.toBeInTheDocument();
    });
    expect(
      screen.getByRole("button", { name: /find trains/i }),
    ).toBeEnabled();
  });

  it("does not permit creating without a selection", async () => {
    searchMock.mockResolvedValue(searchResult());
    const { user } = renderForm();
    await runSearch(user);

    expect(
      screen.getByRole("button", { name: /create monitor/i }),
    ).toBeDisabled();
    expect(
      screen.getByText(/select at least one class/i),
    ).toBeInTheDocument();
  });

  it("shows a valid empty result without treating it as sold out", async () => {
    searchMock.mockResolvedValue({ ...searchResult(), trains: [] });
    const { user } = renderForm();
    await user.type(screen.getByLabelText(/MrBilit search URL/i), SEARCH_URL);
    await user.click(screen.getByRole("button", { name: /find trains/i }));
    await screen.findByText(/No trains returned/i);

    expect(screen.getByText(/No trains returned/i)).toBeInTheDocument();
    expect(screen.getByText(/not a sold-out signal/i)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /create monitor/i }),
    ).not.toBeInTheDocument();
  });

  it("shows an actionable URL error from the server and focuses the URL", async () => {
    searchMock.mockRejectedValue(
      new ApiError({
        status: 422,
        code: "unsupported_url",
        message: "Use a MrBilit train search URL.",
        fields: { url: "Use a MrBilit train search URL." },
      }),
    );
    const { user } = renderForm();
    await user.type(
      screen.getByLabelText(/MrBilit search URL/i),
      "https://example.com/nope",
    );
    await user.click(screen.getByRole("button", { name: /find trains/i }));

    expect(
      await screen.findByText(/Check the search URL/i),
    ).toBeInTheDocument();
    expect(screen.getAllByText(/Use a MrBilit train search URL/i).length).toBeGreaterThan(0);
    await waitFor(() =>
      expect(document.activeElement).toBe(
        screen.getByLabelText(/MrBilit search URL/i),
      ),
    );
  });

  it("renders all three availability states and allows unavailable classes to be selected", async () => {
    searchMock.mockResolvedValue(
      searchResult({
        trains: [
          {
            ...searchResult().trains[0],
            classes: [
              makeClass(),
              makeClass({
                offeringKey: "id:not",
                nativeId: "not",
                capacity: 0,
                state: "not_reservable",
              }),
              makeClass({
                offeringKey: "id:unknown",
                nativeId: "unknown",
                wagonId: "w3",
                wagonName: "Unknown wagon",
                capacity: null,
                state: "unknown",
              }),
            ],
          },
        ],
      }),
    );
    const { user } = renderForm();
    await runSearch(user);

    expect(screen.getAllByText("Reservable").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Not reservable").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Unknown").length).toBeGreaterThan(0);
    const notReservable = screen.getByRole("checkbox", {
      name: /Option 2.*Not reservable/i,
    });
    expect(notReservable).toBeEnabled();
    await user.click(notReservable);
    expect(screen.getByText("1 of 3 selected")).toBeInTheDocument();
  });

  it("preserves a manually edited label after another search", async () => {
    searchMock.mockResolvedValue(searchResult());
    const { user } = renderForm();
    await runSearch(user);

    const label = screen.getByLabelText(/monitor label/i);
    await user.clear(label);
    await user.type(label, "My custom monitor");

    await user.click(screen.getByRole("button", { name: /change search/i }));
    await user.click(screen.getByRole("button", { name: /find trains/i }));
    await screen.findByRole("heading", { name: /available journeys/i });

    expect(screen.getByLabelText(/monitor label/i)).toHaveValue(
      "My custom monitor",
    );
  });

  it("shows creation errors and re-enables the create action", async () => {
    searchMock.mockResolvedValue(searchResult());
    const onSubmit = vi.fn().mockRejectedValue(
      new ApiError({
        status: 422,
        code: "validation_failed",
        message: "The server rejected this monitor.",
        fields: {
          "providers[0].selections[0].journeyKey":
            "This selection is no longer valid.",
        },
      }),
    );
    const { user } = renderForm(onSubmit);
    await runSearch(user);
    await user.click(screen.getAllByRole("checkbox")[0]);
    await user.click(
      screen.getByRole("button", { name: /create monitor/i }),
    );

    expect(await screen.findByText(/Monitor not created/i)).toBeInTheDocument();
    expect(
      screen.getByText(/This selection is no longer valid/i),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(
        screen.getByRole("button", { name: /create monitor/i }),
      ).toBeEnabled(),
    );
  });

  it("shows the global delivery settings and humanized polling interval", async () => {
    searchMock.mockResolvedValue(searchResult());
    const { user } = renderForm();
    await runSearch(user);

    expect(screen.getByText("Every 5 minutes")).toBeInTheDocument();
    expect(screen.getByText("Configured")).toBeInTheDocument();
    expect(screen.getByText("Enabled · no devices")).toBeInTheDocument();
    expect(screen.getByText(/at least one notification channel is ready/i)).toBeInTheDocument();
  });
});
