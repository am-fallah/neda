import { describe, expect, it } from "vitest";
import type {
  ProviderSearchClass,
  ProviderSearchResult,
  ProviderSearchTrain,
  PushConfig,
  Settings,
} from "@/api/types";
import {
  buildSelectionIndex,
  classFamilyKey,
  classFamilyMeta,
  defaultMonitorLabel,
  describeSearchWarnings,
  describeSelection,
  formatJourneyTimes,
  formatPassengerSummary,
  groupSearchTrains,
  notificationReadiness,
  parsePassengerCountsFromUrl,
  passengerCountsValid,
  pollIntervalLabel,
  searchInputSignature,
  selectionKey,
  toSelectedOption,
} from "./mrbilitSearch";

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

function makeTrain(
  overrides: Partial<ProviderSearchTrain> = {},
): ProviderSearchTrain {
  return {
    journeyKey: "id:9001",
    nativeId: "9001",
    trainNumber: "682",
    operator: "Raja",
    originName: "Yazd",
    destinationName: "Mashhad",
    departureAt: "2026-11-15T11:20:00Z",
    arrivalAt: "2026-11-16T03:00:00Z",
    classes: [makeClass()],
    ...overrides,
  };
}

function makeResult(
  overrides: Partial<ProviderSearchResult> = {},
): ProviderSearchResult {
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
    trains: [makeTrain()],
    warnings: [],
    observedAt: "2026-10-25T12:00:00Z",
    ...overrides,
  };
}

describe("parsePassengerCountsFromUrl", () => {
  it("applies all three valid passenger counts and ignores tracking parameters", () => {
    const parsed = parsePassengerCountsFromUrl(
      "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24&adultCount=2&childCount=1&infantCount=1&utm_source=test",
    );
    expect(parsed.status).toBe("applied");
    expect(parsed.counts).toEqual({ adults: 2, children: 1, infants: 1 });
  });

  it("does not guess when the URL has no passenger counts", () => {
    const parsed = parsePassengerCountsFromUrl(
      "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24",
    );
    expect(parsed.status).toBe("absent");
    expect(parsed.counts).toBeUndefined();
  });

  it("reports partial counts without applying them", () => {
    const parsed = parsePassengerCountsFromUrl(
      "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24&adultCount=2",
    );
    expect(parsed.status).toBe("partial");
    expect(parsed.counts).toBeUndefined();
    expect(parsed.presentKeys).toEqual(["adultCount"]);
  });

  it.each([
    "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24&adultCount=0&childCount=0&infantCount=0",
    "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24&adultCount=8&childCount=1&infantCount=1",
    "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24&adultCount=two&childCount=0&infantCount=0",
    "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24&adultCount=1&adultCount=2&childCount=0&infantCount=0",
  ])("rejects invalid or ambiguous URL counts: %s", (url) => {
    const parsed = parsePassengerCountsFromUrl(url);
    expect(parsed.status).toBe("invalid");
    expect(parsed.counts).toBeUndefined();
  });

  it.each([
    "",
    "not a url",
    "http://mrbilit.com/trains/yazd-mashhad?adultCount=1&childCount=0&infantCount=0",
    "https://example.com/trains/yazd-mashhad?adultCount=1&childCount=0&infantCount=0",
    "https://mrbilit.com/trains/yazd-mashhad?adultCount=1&childCount=0&infantCount=0#x",
  ])("does not apply counts outside the supported URL shape: %s", (url) => {
    expect(parsePassengerCountsFromUrl(url).status).toBe("absent");
  });
});

describe("passenger counts and search signatures", () => {
  it("enforces the existing passenger bounds", () => {
    expect(passengerCountsValid({ adults: 1, children: 0, infants: 0 })).toBe(
      true,
    );
    expect(passengerCountsValid({ adults: 9, children: 0, infants: 0 })).toBe(
      true,
    );
    expect(passengerCountsValid({ adults: 0, children: 0, infants: 0 })).toBe(
      false,
    );
    expect(passengerCountsValid({ adults: 8, children: 1, infants: 1 })).toBe(
      false,
    );
    expect(passengerCountsValid({ adults: 1.5, children: 0, infants: 0 })).toBe(
      false,
    );
  });

  it("builds a stable signature from URL and counts only", () => {
    const base = {
      url: "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24",
      adults: 1,
      children: 0,
      infants: 0,
    };
    expect(searchInputSignature(base)).toBe(
      searchInputSignature({ ...base, url: ` ${base.url} ` }),
    );
    expect(searchInputSignature(base)).not.toBe(
      searchInputSignature({ ...base, adults: 2 }),
    );
    expect(searchInputSignature(base)).not.toBe(
      searchInputSignature({ ...base, url: `${base.url}&x=1` }),
    );
  });
});

describe("groupSearchTrains", () => {
  it("groups repeated class names under strict wagon/class identity while preserving offerings", () => {
    const samens = [
      makeClass({ offeringKey: "id:9102", nativeId: "9102", capacity: 3 }),
      makeClass({
        offeringKey: "id:9104",
        nativeId: "9104",
        capacity: 0,
        state: "not_reservable",
      }),
      makeClass({
        offeringKey: "id:9106",
        nativeId: "9106",
        capacity: 0,
        state: "not_reservable",
      }),
    ];
    const sepehr = makeClass({
      offeringKey: "id:9101",
      nativeId: "9101",
      wagonId: "9201",
      wagonName: "Four-star Sepehr",
      className: "Sepehr",
      capacity: 0,
      state: "not_reservable",
    });
    const groups = groupSearchTrains([
      makeTrain({ classes: [...samens, sepehr] }),
    ]);

    expect(groups).toHaveLength(1);
    expect(groups[0].families).toHaveLength(2);
    const samenFamily = groups[0].families.find(
      (family) => family.title === "Samen",
    );
    expect(samenFamily?.options.map((option) => option.offeringKey)).toEqual([
      "id:9102",
      "id:9104",
      "id:9106",
    ]);
    expect(groups[0].optionCount).toBe(4);
    expect(groups[0].reservableCount).toBe(1);
    expect(groups[0].notReservableCount).toBe(3);
  });

  it("does not merge identical class names when wagon IDs differ", () => {
    const groups = groupSearchTrains([
      makeTrain({
        classes: [
          makeClass({ offeringKey: "id:a", nativeId: "a", wagonId: "w1" }),
          makeClass({ offeringKey: "id:b", nativeId: "b", wagonId: "w2" }),
        ],
      }),
    ]);
    expect(groups[0].families).toHaveLength(2);
    expect(groups[0].families.map((family) => family.wagonId)).toEqual([
      "w1",
      "w2",
    ]);
  });

  it("does not merge different compartment types even with the same class name", () => {
    const groups = groupSearchTrains([
      makeTrain({
        classes: [
          makeClass({ offeringKey: "id:a", compartmentType: "4-person" }),
          makeClass({
            offeringKey: "id:b",
            compartmentType: "6-person",
            state: "not_reservable",
            capacity: 0,
          }),
        ],
      }),
    ]);
    expect(groups[0].families).toHaveLength(2);
  });

  it("keeps distinct journey keys separate even when train number and time match", () => {
    const groups = groupSearchTrains([
      makeTrain({ journeyKey: "id:9001", classes: [makeClass()] }),
      makeTrain({
        journeyKey: "id:9002",
        classes: [makeClass({ offeringKey: "id:9202", nativeId: "9202" })],
      }),
    ]);
    expect(groups).toHaveLength(2);
    expect(groups.map((group) => group.key)).toEqual(["id:9001", "id:9002"]);
  });

  it("merges repeated identical journey keys without losing offerings", () => {
    const groups = groupSearchTrains([
      makeTrain({ classes: [makeClass({ offeringKey: "id:a", nativeId: "a" })] }),
      makeTrain({ classes: [makeClass({ offeringKey: "id:b", nativeId: "b" })] }),
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0].optionCount).toBe(2);
    expect(groups[0].families[0].options).toHaveLength(2);
  });

  it("keeps conflicting duplicate journey keys as separate display groups", () => {
    const groups = groupSearchTrains([
      makeTrain({ classes: [makeClass()] }),
      makeTrain({
        operator: "Another operator",
        classes: [makeClass({ offeringKey: "id:9202", nativeId: "9202" })],
      }),
    ]);
    expect(groups).toHaveLength(2);
    expect(new Set(groups.map((group) => group.key)).size).toBe(2);
  });

  it("sorts journeys by departure time and options by state and capacity", () => {
    const groups = groupSearchTrains([
      makeTrain({
        journeyKey: "id:late",
        departureAt: "2026-11-15T15:00:00Z",
        classes: [],
      }),
      makeTrain({
        journeyKey: "id:early",
        departureAt: "2026-11-15T05:00:00Z",
        classes: [
          makeClass({
            offeringKey: "id:not",
            nativeId: "not",
            state: "not_reservable",
            capacity: 0,
          }),
          makeClass({
            offeringKey: "id:unknown",
            nativeId: "unknown",
            state: "unknown",
            capacity: null,
          }),
          makeClass({
            offeringKey: "id:res",
            nativeId: "res",
            capacity: 5,
          }),
        ],
      }),
    ]);
    expect(groups.map((group) => group.key)).toEqual(["id:early", "id:late"]);
    expect(
      groups[0].families[0].options.map((option) => option.offeringKey),
    ).toEqual(["id:res", "id:unknown", "id:not"]);
  });

  it("creates exact selection positions and selected-option payloads", () => {
    const groups = groupSearchTrains([
      makeTrain({
        classes: [
          makeClass({ offeringKey: "id:a", nativeId: "a" }),
          makeClass({ offeringKey: "id:b", nativeId: "b", capacity: 0, state: "not_reservable" }),
        ],
      }),
    ]);
    const index = buildSelectionIndex(groups);
    const position = index.get(selectionKey("id:9001", "id:b"));
    expect(position).toBeDefined();
    expect(describeSelection(position!)).toBe(
      "Train 682 · Samen · Option 2",
    );
    expect(toSelectedOption(groups[0].train, position!.option)).toMatchObject({
      journeyKey: "id:9001",
      offeringKey: "id:b",
      offeringNativeId: "b",
      wagonId: "9202",
      className: "Samen",
    });
  });

  it("uses all identity metadata in the class-family key", () => {
    expect(
      classFamilyKey(
        makeClass({
          wagonId: "",
          wagonName: "Wagon 1",
          className: "Economy",
          compartmentType: "open",
        }),
      ),
    ).toBe(
      ["", "Wagon 1", "Economy", "open"].join("\u0000"),
    );
    expect(
      classFamilyMeta({
        key: "x",
        title: "Economy",
        className: "Economy",
        wagonId: "w1",
        wagonName: "",
        compartmentType: "open",
        options: [],
      }),
    ).toBe("Wagon w1 · open");
  });
});

describe("journey display helpers", () => {
  it("formats Tehran times, duration, and next-day offsets", () => {
    const times = formatJourneyTimes(
      "2026-11-15T11:20:00Z",
      "2026-11-16T03:00:00Z",
    );
    expect(times).toEqual({
      departure: "14:50",
      arrival: "06:30",
      duration: "15h 40m",
      dayOffset: 1,
    });
  });

  it("handles missing or invalid times without inventing values", () => {
    expect(formatJourneyTimes(null, "bad")).toEqual({
      departure: null,
      arrival: null,
      duration: null,
      dayOffset: null,
    });
  });

  it("builds the default label and route fallbacks", () => {
    const result = makeResult();
    expect(defaultMonitorLabel(result)).toBe(
      "Yazd → Mashhad — 2026-11-15",
    );
    expect(
      defaultMonitorLabel(
        makeResult({
          route: {
            originId: "1",
            destinationId: "2",
            originName: "",
            destinationName: "",
          },
        }),
      ),
    ).toBe("Yazd → Mashhad — 2026-11-15");
    expect(
      defaultMonitorLabel(
        makeResult({
          route: {
            originId: "219",
            destinationId: "191",
            originName: "یزد",
            destinationName: "مشهد",
          },
        }),
      ),
    ).toBe("Yazd → Mashhad — 2026-11-15");
  });

  it("formats passengers and polling intervals", () => {
    expect(formatPassengerSummary({ adults: 1, children: 0, infants: 0 })).toBe(
      "1 adult",
    );
    expect(
      formatPassengerSummary(
        { adults: 2, children: 1, infants: 0 },
        true,
      ),
    ).toBe("2 adults · 1 child · 0 infants");
    expect(pollIntervalLabel(300)).toBe("Every 5 minutes");
    expect(pollIntervalLabel(90)).toBe("Every 90 seconds");
    expect(pollIntervalLabel(3600)).toBe("Every 1 hour");
  });
});

describe("notificationReadiness", () => {
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
    subscriptions: [
      { id: "p1", createdAt: "2026-10-24T12:00:00Z", failureCount: 0 },
    ],
  };

  it("reports ready channels and device counts", () => {
    expect(notificationReadiness(settings, pushConfig)).toMatchObject({
      ready: true,
      telegram: { label: "Configured", tone: "success", ready: true },
      push: { label: "Enabled · 1 device", tone: "success", ready: true },
    });
  });

  it("does not treat unavailable or unconfigured channels as ready", () => {
    const readiness = notificationReadiness(
      {
        ...settings,
        telegram: {
          available: true,
          configured: true,
          readable: false,
        },
      },
      {
        ...pushConfig,
        enabled: false,
      },
    );
    expect(readiness.ready).toBe(false);
    expect(readiness.telegram.tone).toBe("warning");
    expect(readiness.push).toMatchObject({
      label: "Disabled",
      tone: "muted",
      ready: false,
    });
  });
});

describe("describeSearchWarnings", () => {
  it("translates known codes and keeps unknown diagnostics separate", () => {
    const summary = describeSearchWarnings([
      "duplicate_offering_key",
      "meta_missing",
      "some_future_code",
    ]);
    expect(summary.notices).toEqual([
      "MrBilit returned duplicate class entries; identical duplicates were ignored, and conflicting ones remain unknown.",
      "MrBilit returned incomplete response metadata.",
    ]);
    expect(summary.diagnostics).toEqual(["some_future_code"]);
  });

  it("does not expose only unknown codes as primary notices", () => {
    const summary = describeSearchWarnings(["some_future_code"]);
    expect(summary.notices).toEqual([
      "MrBilit returned diagnostic warnings; some fields were not used.",
    ]);
    expect(summary.diagnostics).toEqual(["some_future_code"]);
  });
});
