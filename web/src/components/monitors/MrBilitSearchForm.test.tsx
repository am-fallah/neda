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
const STANDARD_URL = `${SEARCH_URL}&adultCount=2&childCount=1`;

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
        provider={provider}
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
  await screen.findByRole("heading", { name: /search results/i });
}

describe("MrBilitSearchForm", () => {
  beforeEach(() => {
    searchMock.mockReset();
  });

  it("searches, groups offerings by wagon, selects all current wagon offerings, and submits them", async () => {
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
    expect(screen.getAllByRole("checkbox")).toHaveLength(1);
    expect(
      screen.getAllByText(/1 of 2 reservable/i).length,
    ).toBeGreaterThan(0);
    expect(
      screen.queryByText(/loaded from the url/i),
    ).not.toBeInTheDocument();

    await user.click(
      screen.getByRole("checkbox", {
        name: /Select Samen, all current fare options/i,
      }),
    );
    expect(
      screen.getAllByText(/1 wagon · 2 fare options/).length,
    ).toBeGreaterThan(0);
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
            expect.objectContaining({
              journeyKey: "id:9001",
              offeringKey: "id:9102",
              offeringNativeId: "9102",
              trainNumber: "682",
              wagonId: "9202",
              wagonName: "Five-star Samen",
              className: "Samen",
              compartmentType: "4-person compartment",
            }),
            expect.objectContaining({
              journeyKey: "id:9001",
              offeringKey: "id:9104",
              offeringNativeId: "9104",
              wagonId: "9202",
            }),
          ],
        },
      ],
    });
  });

  it("prefills a standard MrBilit URL with omitted passenger defaults and hides the editor", async () => {
    searchMock.mockResolvedValue(
      searchResult({
        passengers: { adults: 2, children: 1, infants: 0 },
      }),
    );
    const { user, onSubmit } = renderForm();

    await user.type(screen.getByLabelText(/MrBilit search URL/i), STANDARD_URL);

    expect(screen.getByText("2 adults · 1 child")).toBeInTheDocument();
    expect(screen.getByText(/loaded from the url/i)).toBeInTheDocument();
    expect(screen.queryByLabelText(/^Adults/i)).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /edit passengers/i }),
    ).toHaveAttribute("aria-expanded", "false");

    await user.click(screen.getByRole("button", { name: /edit passengers/i }));
    expect(screen.getByLabelText(/^Adults/i)).toHaveValue(2);
    expect(screen.getByLabelText(/^Children/i)).toHaveValue(1);
    expect(screen.getByLabelText(/^Infants/i)).toHaveValue(0);

    await user.click(screen.getByRole("button", { name: /find trains/i }));
    await screen.findByRole("heading", { name: /search results/i });
    expect(searchMock).toHaveBeenCalledWith(
      "mrbilit",
      expect.objectContaining({ adults: 2, children: 1, infants: 0 }),
      expect.any(AbortSignal),
    );

    await user.click(screen.getAllByRole("checkbox")[0]);
    await user.click(
      screen.getByRole("button", { name: /create monitor/i }),
    );
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit.mock.calls[0][0]).toMatchObject({
      adults: 2,
      children: 1,
      infants: 0,
    });
  });

  it("uses clearly labelled defaults when the URL has no passenger counts", async () => {
    searchMock.mockResolvedValue(searchResult());
    const { user, onSubmit } = renderForm();

    await user.type(screen.getByLabelText(/MrBilit search URL/i), SEARCH_URL);

    expect(screen.getByText("1 adult")).toBeInTheDocument();
    expect(screen.getByText(/no passenger counts in this url/i)).toBeInTheDocument();
    expect(screen.queryByLabelText(/^Adults/i)).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /find trains/i }));
    await screen.findByRole("heading", { name: /search results/i });
    expect(searchMock).toHaveBeenCalledWith(
      "mrbilit",
      expect.objectContaining({ adults: 1, children: 0, infants: 0 }),
      expect.any(AbortSignal),
    );

    await user.click(screen.getAllByRole("checkbox")[0]);
    await user.click(
      screen.getByRole("button", { name: /create monitor/i }),
    );
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit.mock.calls[0][0]).toMatchObject({
      adults: 1,
      children: 0,
      infants: 0,
    });
  });

  it("marks manual passenger edits and uses them for search and creation", async () => {
    searchMock.mockResolvedValue(
      searchResult({
        passengers: { adults: 3, children: 1, infants: 0 },
      }),
    );
    const { user, onSubmit } = renderForm();

    await user.type(screen.getByLabelText(/MrBilit search URL/i), STANDARD_URL);
    await user.click(screen.getByRole("button", { name: /edit passengers/i }));
    const adults = screen.getByLabelText(/^Adults/i);
    await user.clear(adults);
    await user.type(adults, "3");

    expect(
      screen.getByText(/using manually edited passenger counts/i),
    ).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /find trains/i }));
    await screen.findByRole("heading", { name: /search results/i });
    expect(searchMock).toHaveBeenCalledWith(
      "mrbilit",
      expect.objectContaining({ adults: 3, children: 1, infants: 0 }),
      expect.any(AbortSignal),
    );

    await user.click(screen.getAllByRole("checkbox")[0]);
    await user.click(
      screen.getByRole("button", { name: /create monitor/i }),
    );
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit.mock.calls[0][0]).toMatchObject({
      adults: 3,
      children: 1,
      infants: 0,
    });
  });

  it("resets URL-derived counts but preserves explicit manual edits when a new URL has no counts", async () => {
    const { user } = renderForm();
    const url = screen.getByLabelText(/MrBilit search URL/i);

    await user.type(url, STANDARD_URL);
    expect(screen.getByText("2 adults · 1 child")).toBeInTheDocument();

    await user.clear(url);
    await user.type(url, SEARCH_URL);
    expect(screen.getByText("1 adult")).toBeInTheDocument();
    expect(screen.getByText(/no passenger counts in this url/i)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /edit passengers/i }));
    expect(screen.getByLabelText(/^Adults/i)).toHaveValue(1);
    expect(screen.getByLabelText(/^Children/i)).toHaveValue(0);

    const adults = screen.getByLabelText(/^Adults/i);
    await user.clear(adults);
    await user.type(adults, "3");
    await user.clear(url);
    await user.type(url, SEARCH_URL);
    expect(screen.getByText("3 adults")).toBeInTheDocument();
    expect(
      screen.getByText(/using manually edited passenger counts/i),
    ).toBeInTheDocument();
  });

  it("does not apply invalid URL passenger counts and opens the editor on search", async () => {
    searchMock.mockResolvedValue(searchResult());
    const { user } = renderForm();

    await user.type(
      screen.getByLabelText(/MrBilit search URL/i),
      `${SEARCH_URL}&adultCount=not-a-number`,
    );

    expect(screen.getByText(/passenger counts are invalid/i)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /find trains/i }));

    expect(searchMock).not.toHaveBeenCalled();
    expect(screen.getByLabelText(/^Adults/i)).toBeInTheDocument();
    expect(
      screen.getByText(/fix the passenger counts in the url/i),
    ).toBeInTheDocument();
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

    await user.click(
      screen.getByRole("checkbox", {
        name: /Select Samen, all current fare options/i,
      }),
    );
    expect(
      screen.getAllByText(/1 wagon · 2 fare options/).length,
    ).toBeGreaterThan(0);

    await user.click(screen.getByRole("button", { name: /change search/i }));
    const url = screen.getByLabelText(/MrBilit search URL/i);
    await user.clear(url);
    await user.type(url, SEARCH_URL.replace("1405-08-24", "1405-08-25"));

    expect(screen.getByText(/search settings changed/i)).toBeInTheDocument();
    expect(screen.getByText("None")).toBeInTheDocument();
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
      screen.getByText(/select at least one wagon/i),
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
    expect(screen.getByText(/no wagons to review/i)).toBeInTheDocument();
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
    expect(
      screen.getAllByText(/Use a MrBilit train search URL/i).length,
    ).toBeGreaterThan(0);
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
    expect(screen.getAllByText("Unknown").length).toBeGreaterThan(0);
    await user.click(
      screen.getByRole("button", {
        name: /Show fare options for Samen/i,
      }),
    );
    expect(screen.getAllByText("Not reservable").length).toBeGreaterThan(0);
    const notReservable = screen.getByRole("checkbox", {
      name: /Select Samen, Fare option 2, not reservable/i,
    });
    expect(notReservable).toBeEnabled();
    await user.click(notReservable);
    expect(screen.getByText(/1 of 2 fare options/)).toBeInTheDocument();
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
    await screen.findByRole("heading", { name: /search results/i });

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
    expect(
      screen.getByText(/at least one channel is ready/i),
    ).toBeInTheDocument();
  });
});

describe("wagon hierarchy, price filter, and partial selection", () => {
  const pricedResult = () =>
    searchResult({
      trains: [
        {
          ...searchResult().trains[0],
          classes: [
            makeClass({
              offeringKey: "id:sepehr-cheap",
              nativeId: "sepehr-cheap",
              wagonId: "sepehr",
              wagonName: "Four-star Sepehr",
              className: "Sepehr",
              priceAmount: 2345000,
            }),
            makeClass({
              offeringKey: "id:sepehr-unpriced",
              nativeId: "sepehr-unpriced",
              wagonId: "sepehr",
              wagonName: "Four-star Sepehr",
              className: "Sepehr",
              capacity: 0,
              state: "not_reservable",
              priceAmount: null,
            }),
            makeClass({
              offeringKey: "id:samen-expensive",
              nativeId: "samen-expensive",
              wagonId: "samen",
              wagonName: "Five-star Samen",
              className: "Samen",
              priceAmount: 12345000,
            }),
            makeClass({
              offeringKey: "id:samen-unpriced",
              nativeId: "samen-unpriced",
              wagonId: "samen",
              wagonName: "Five-star Samen",
              className: "Samen",
              capacity: 0,
              state: "not_reservable",
              priceAmount: null,
            }),
          ],
        },
      ],
    });

  it("keeps a selected wagon selected when the price filter hides it", async () => {
    searchMock.mockResolvedValue(pricedResult());
    const { user, onSubmit } = renderForm();
    await runSearch(user);

    expect(screen.getAllByText(/2 wagon/).length).toBeGreaterThan(0);
    await user.click(
      screen.getByRole("checkbox", {
        name: /Select Samen, all current fare options/i,
      }),
    );

    await user.click(
      screen.getByRole("button", { name: /filter by price/i }),
    );
    await user.type(screen.getByLabelText(/^Minimum$/), "2000000");
    await user.type(screen.getByLabelText(/^Maximum$/), "3000000");
    await user.click(screen.getByRole("button", { name: /^Done$/ }));

    expect(screen.queryByText(/^Samen$/)).not.toBeInTheDocument();
    expect(screen.getByText(/^Sepehr$/)).toBeInTheDocument();
    expect(
      screen.getByText(/2 selected fare options are outside this view/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/2 unpriced fare options hidden/i)).toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: /create monitor/i }),
    );
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const payload = onSubmit.mock.calls[0][0];
    expect(payload.providers[0].selections?.map((item: { offeringKey: string }) => item.offeringKey)).toEqual([
      "id:samen-expensive",
      "id:samen-unpriced",
    ]);
  });

  it("selects every underlying offering when a filtered wagon is selected", async () => {
    searchMock.mockResolvedValue(pricedResult());
    const { user, onSubmit } = renderForm();
    await runSearch(user);

    await user.click(
      screen.getByRole("button", { name: /filter by price/i }),
    );
    await user.type(screen.getByLabelText(/^Minimum$/), "2000000");
    await user.type(screen.getByLabelText(/^Maximum$/), "3000000");
    await user.click(screen.getByRole("button", { name: /^Done$/ }));

    await user.click(
      screen.getByRole("checkbox", {
        name: /Select Sepehr, all current fare options/i,
      }),
    );
    expect(
      screen.getAllByText(/1 wagon · 2 fare options/).length,
    ).toBeGreaterThan(0);
    expect(
      screen.getByText(/1 selected fare option is outside this view/i),
    ).toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: /create monitor/i }),
    );
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const payload = onSubmit.mock.calls[0][0];
    expect(payload.providers[0].selections?.map((item: { offeringKey: string }) => item.offeringKey)).toEqual([
      "id:sepehr-cheap",
      "id:sepehr-unpriced",
    ]);
  });

  it("can include unpriced offerings without treating them as cheap", async () => {
    searchMock.mockResolvedValue(pricedResult());
    const { user } = renderForm();
    await runSearch(user);

    await user.click(
      screen.getByRole("button", { name: /filter by price/i }),
    );
    await user.type(screen.getByLabelText(/^Minimum$/), "2000000");
    await user.type(screen.getByLabelText(/^Maximum$/), "3000000");
    await user.click(
      screen.getByRole("checkbox", {
        name: /include 2 unpriced fare options/i,
      }),
    );
    await user.click(screen.getByRole("button", { name: /^Done$/ }));

    expect(screen.getByText(/^Samen$/)).toBeInTheDocument();
    expect(
      screen.getByText(/1 fare option outside the range/i),
    ).toBeInTheDocument();
    expect(screen.queryByText(/^0$/)).not.toBeInTheDocument();
  });

  it("sorts wagons by lowest known price without changing selection", async () => {
    searchMock.mockResolvedValue(pricedResult());
    const { user } = renderForm();
    await runSearch(user);

    await user.selectOptions(
      screen.getByLabelText(/sort search results/i),
      "price",
    );
    const labels = screen
      .getAllByRole("checkbox")
      .map((element) => element.getAttribute("aria-label"));
    expect(labels).toEqual([
      "Select Sepehr, all current fare options",
      "Select Samen, all current fare options",
    ]);
  });

  it("supports partial fare selection and preserves exact offering keys", async () => {
    searchMock.mockResolvedValue(pricedResult());
    const { user, onSubmit } = renderForm();
    await runSearch(user);

    await user.click(
      screen.getByRole("button", {
        name: /Show fare options for Sepehr/i,
      }),
    );
    await user.click(
      screen.getByRole("checkbox", {
        name: /Select Sepehr, Fare option 2, not reservable/i,
      }),
    );
    expect(screen.getByText(/1 of 2 selected/i)).toBeInTheDocument();
    expect(
      screen.getByText(/1 of 2 fare options/),
    ).toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: /create monitor/i }),
    );
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    const payload = onSubmit.mock.calls[0][0];
    expect(payload.providers[0].selections?.map((item: { offeringKey: string }) => item.offeringKey)).toEqual([
      "id:sepehr-unpriced",
    ]);
  });

  it("rejects an inverted price range without filtering the result", async () => {
    searchMock.mockResolvedValue(pricedResult());
    const { user } = renderForm();
    await runSearch(user);

    await user.click(
      screen.getByRole("button", { name: /filter by price/i }),
    );
    await user.type(screen.getByLabelText(/^Minimum$/), "3000000");
    await user.type(screen.getByLabelText(/^Maximum$/), "2000000");

    expect(
      screen.getByText(/minimum price must not exceed maximum price/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/^Samen$/)).toBeInTheDocument();
  });
});
