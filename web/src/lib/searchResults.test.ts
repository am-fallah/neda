import { describe, expect, it } from "vitest";
import type {
  ProviderSearchClass,
  ProviderSearchTrain,
  PushConfig,
  Settings,
} from "@/api/types";
import {
  applySearchView,
  countSelectedOutsideView,
  formatJourneyTimes,
  formatPassengerSummary,
  groupSearchTrains,
  groupMonitorSelections,
  groupSelectedWagons,
  knownPrice,
  monitorSelectionMeta,
  monitorSelectionTitle,
  notificationReadiness,
  parsePriceBound,
  pollIntervalLabel,
  selectionKey,
  summarizeOfferings,
  toSelectedOption,
  wagonMeta,
  wagonSelectionState,
} from "./searchResults";

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
    priceAmount: 12345000,
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

function observedClasses(): ProviderSearchClass[] {
  return [
    makeClass({
      offeringKey: "id:9101",
      nativeId: "9101",
      wagonId: "9201",
      wagonName: "Four-star Sepehr",
      className: "Sepehr",
      capacity: 0,
      state: "not_reservable",
      priceAmount: 0,
    }),
    makeClass(),
    makeClass({
      offeringKey: "id:9103",
      nativeId: "9103",
      wagonId: "9201",
      wagonName: "Four-star Sepehr",
      className: "Sepehr",
      capacity: 0,
      state: "not_reservable",
      priceAmount: null,
    }),
    makeClass({
      offeringKey: "id:9104",
      nativeId: "9104",
      capacity: 0,
      state: "not_reservable",
      priceAmount: 0,
    }),
    makeClass({
      offeringKey: "id:9105",
      nativeId: "9105",
      wagonId: "9201",
      wagonName: "Four-star Sepehr",
      className: "Sepehr",
      capacity: 2,
      state: "reservable",
      priceAmount: 2345000,
    }),
    makeClass({
      offeringKey: "id:9106",
      nativeId: "9106",
      capacity: 0,
      state: "not_reservable",
      priceAmount: 0,
    }),
  ];
}

describe("knownPrice and summarizeOfferings", () => {
  it("never treats zero, negatives, fractions, or missing as a price", () => {
    expect(knownPrice(1200)).toBe(1200);
    expect(knownPrice(0)).toBeNull();
    expect(knownPrice(-1)).toBeNull();
    expect(knownPrice(1.5)).toBeNull();
    expect(knownPrice(null)).toBeNull();
    expect(knownPrice(undefined)).toBeNull();
  });

  it("summarizes counts, states, and priced coverage without merging offerings", () => {
    const summary = summarizeOfferings(observedClasses());
    expect(summary).toMatchObject({
      optionCount: 6,
      reservableCount: 2,
      notReservableCount: 4,
      unknownCount: 0,
      pricedCount: 2,
      unpricedCount: 4,
      lowestKnownPrice: 2345000,
      allPricesKnown: false,
      state: "reservable",
    });
  });

  it("keeps Unknown as Unknown when nothing is reservable", () => {
    const summary = summarizeOfferings([
      makeClass({ state: "unknown", priceAmount: null }),
      makeClass({
        offeringKey: "id:not",
        state: "not_reservable",
        priceAmount: 0,
      }),
    ]);
    expect(summary.state).toBe("unknown");
  });
});

describe("groupSearchTrains wagon hierarchy", () => {
  it("groups the observed six offerings into two wagons while retaining every exact offering", () => {
    const groups = groupSearchTrains([
      makeTrain({ classes: observedClasses() }),
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0].wagons).toHaveLength(2);
    expect(groups[0].summary.optionCount).toBe(6);

    const samen = groups[0].wagons.find((wagon) => wagon.title === "Samen");
    const sepehr = groups[0].wagons.find((wagon) => wagon.title === "Sepehr");
    expect(samen?.offerings.map((item) => item.offeringKey)).toEqual([
      "id:9102",
      "id:9104",
      "id:9106",
    ]);
    expect(sepehr?.offerings.map((item) => item.offeringKey)).toEqual([
      "id:9105",
      "id:9101",
      "id:9103",
    ]);
    expect(samen?.summary).toMatchObject({
      reservableCount: 1,
      notReservableCount: 2,
      pricedCount: 1,
      unpricedCount: 2,
      lowestKnownPrice: 12345000,
    });
    expect(sepehr?.summary).toMatchObject({
      reservableCount: 1,
      notReservableCount: 2,
      pricedCount: 1,
      unpricedCount: 2,
      lowestKnownPrice: 2345000,
    });
    expect(wagonMeta(sepehr!)).toBe(
      "Four-star Sepehr · 4-person compartment",
    );
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
    expect(groups[0].wagons).toHaveLength(2);
    expect(groups[0].wagons.map((wagon) => wagon.wagonId)).toEqual([
      "w1",
      "w2",
    ]);
  });

  it("splits conflicting wagon attributes even when the wagon ID matches", () => {
    const groups = groupSearchTrains([
      makeTrain({
        classes: [
          makeClass({
            offeringKey: "id:a",
            nativeId: "a",
            wagonId: "w1",
            compartmentType: "4-person",
          }),
          makeClass({
            offeringKey: "id:b",
            nativeId: "b",
            wagonId: "w1",
            compartmentType: "6-person",
          }),
        ],
      }),
    ]);
    expect(groups[0].wagons).toHaveLength(2);
    expect(groups[0].summary.optionCount).toBe(2);
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
});

describe("selection helpers", () => {
  it("reports none, partial, and all wagon selection without changing identity", () => {
    const groups = groupSearchTrains([
      makeTrain({ classes: observedClasses() }),
    ]);
    const sepehr = groups[0].wagons.find((wagon) => wagon.title === "Sepehr")!;
    const selected: Record<string, ReturnType<typeof toSelectedOption>> = {};
    expect(
      wagonSelectionState("id:9001", sepehr, selected),
    ).toBe("none");
    selected[selectionKey("id:9001", "id:9105")] = toSelectedOption(
      groups[0].train,
      sepehr.offerings[0],
    );
    expect(wagonSelectionState("id:9001", sepehr, selected)).toBe(
      "partial",
    );
    for (const offering of sepehr.offerings.slice(1)) {
      selected[selectionKey("id:9001", offering.offeringKey)] =
        toSelectedOption(groups[0].train, offering);
    }
    expect(wagonSelectionState("id:9001", sepehr, selected)).toBe("all");
    expect(Object.keys(selected)).toHaveLength(3);
  });

  it("groups saved monitor selections by journey and wagon for display", () => {
    const groups = groupSearchTrains([
      makeTrain({ classes: observedClasses() }),
    ]);
    const sepehr = groups[0].wagons.find((wagon) => wagon.title === "Sepehr")!;
    const selections = sepehr.offerings.slice(0, 2).map((offering) =>
      toSelectedOption(groups[0].train, offering),
    );
    selections.push({
      ...toSelectedOption(groups[0].train, sepehr.offerings[2]),
      journeyKey: "id:other",
      wagonId: "other-wagon",
      wagonName: "Other wagon",
      className: "Other class",
    });
    const monitorGroups = groupMonitorSelections(selections);
    expect(monitorGroups).toHaveLength(2);
    expect(monitorGroups[0].selections).toHaveLength(2);
    expect(monitorSelectionTitle(monitorGroups[0])).toBe("Four-star Sepehr");
    expect(monitorSelectionMeta(monitorGroups[0])).toBe(
      "Sepehr · 4-person compartment",
    );
    expect(monitorSelectionTitle(monitorGroups[1])).toBe("Other wagon");
  });

  it("groups selected wagons and preserves partial selection counts", () => {
    const groups = groupSearchTrains([
      makeTrain({ classes: observedClasses() }),
    ]);
    const sepehr = groups[0].wagons.find((wagon) => wagon.title === "Sepehr")!;
    const selected = {
      [selectionKey("id:9001", "id:9105")]: toSelectedOption(
        groups[0].train,
        sepehr.offerings[0],
      ),
      [selectionKey("id:9001", "id:9103")]: toSelectedOption(
        groups[0].train,
        sepehr.offerings[1],
      ),
    };
    const summary = groupSelectedWagons(groups, selected);
    expect(summary).toHaveLength(1);
    expect(summary[0].partial).toBe(true);
    expect(summary[0].selectedOfferings).toHaveLength(2);
    expect(summary[0].summary).toMatchObject({
      optionCount: 2,
      reservableCount: 1,
      notReservableCount: 1,
    });
  });
});

describe("applySearchView", () => {
  const groups = groupSearchTrains([
    makeTrain({ classes: observedClasses() }),
  ]);

  it("does not filter when no bounds are present", () => {
    const view = applySearchView(groups, {
      priceMin: null,
      priceMax: null,
      includeUnpriced: false,
      sort: "departure",
    });
    expect(view.groups).toHaveLength(1);
    expect(view.groups[0].wagons).toHaveLength(2);
    expect(view.visibleOfferings).toBe(6);
    expect(view.hiddenUnpriced).toBe(0);
    expect(view.hiddenPricedOutOfRange).toBe(0);
    expect(view.knownPriceMin).toBe(2345000);
    expect(view.knownPriceMax).toBe(12345000);
  });

  it("filters at offering level and hides unpriced entries by default", () => {
    const view = applySearchView(groups, {
      priceMin: 2000000,
      priceMax: 3000000,
      includeUnpriced: false,
      sort: "departure",
    });
    expect(view.groups).toHaveLength(1);
    expect(view.groups[0].wagons[0].title).toBe("Sepehr");
    expect(view.groups[0].wagons[0].offerings.map((item) => item.nativeId)).toEqual([
      "9105",
    ]);
    expect(view.visibleOfferings).toBe(1);
    expect(view.hiddenUnpriced).toBe(4);
    expect(view.hiddenPricedOutOfRange).toBe(1);
  });

  it("can include unpriced entries without pretending they are priced", () => {
    const view = applySearchView(groups, {
      priceMin: 2000000,
      priceMax: 3000000,
      includeUnpriced: true,
      sort: "departure",
    });
    expect(view.visibleOfferings).toBe(5);
    expect(view.hiddenUnpriced).toBe(0);
    expect(view.hiddenPricedOutOfRange).toBe(1);
    expect(
      view.groups
        .flatMap((journey) => journey.wagons)
        .flatMap((wagon) => wagon.offerings)
        .filter((offering) => knownPrice(offering.priceAmount) === null),
    ).toHaveLength(4);
  });

  it("sorts by lowest known price and puts unknown prices last", () => {
    const withUnknown = groupSearchTrains([
      makeTrain({
        classes: [
          makeClass({
            offeringKey: "id:unknown",
            nativeId: "unknown",
            wagonId: "w3",
            className: "Unknown wagon",
            state: "unknown",
            priceAmount: null,
          }),
          makeClass({
            offeringKey: "id:cheap",
            nativeId: "cheap",
            wagonId: "w1",
            className: "Cheap wagon",
            priceAmount: 1000,
          }),
        ],
      }),
    ]);
    const view = applySearchView(withUnknown, {
      priceMin: null,
      priceMax: null,
      includeUnpriced: false,
      sort: "price",
    });
    expect(view.groups[0].wagons.map((wagon) => wagon.title)).toEqual([
      "Cheap wagon",
      "Unknown wagon",
    ]);
  });

  it("does not mutate the source groups or selections", () => {
    const before = JSON.stringify(groups);
    applySearchView(groups, {
      priceMin: 2000000,
      priceMax: 3000000,
      includeUnpriced: false,
      sort: "price",
    });
    expect(JSON.stringify(groups)).toBe(before);
    expect(countSelectedOutsideView(groups, {})).toBe(0);
  });

  it("counts selected offerings hidden by the current view", () => {
    const view = applySearchView(groups, {
      priceMin: 2000000,
      priceMax: 3000000,
      includeUnpriced: false,
      sort: "departure",
    });
    const samen = groups[0].wagons.find((wagon) => wagon.title === "Samen")!;
    const selected = {
      [selectionKey("id:9001", "id:9102")]: toSelectedOption(
        groups[0].train,
        samen.offerings[0],
      ),
    };
    expect(countSelectedOutsideView(view.groups, selected)).toBe(1);
  });
});

describe("parsePriceBound", () => {
  it("accepts whole provider amounts with optional commas and spaces", () => {
    expect(parsePriceBound("")).toEqual({ value: null, invalid: false });
    expect(parsePriceBound("2,345,000")).toEqual({
      value: 2345000,
      invalid: false,
    });
    expect(parsePriceBound(" 2 345 000 ")).toEqual({
      value: 2345000,
      invalid: false,
    });
    expect(parsePriceBound("0")).toEqual({ value: 0, invalid: false });
  });

  it("rejects negative, fractional, and non-numeric bounds", () => {
    for (const value of ["-1", "1.5", "abc", "۱۰۰"]) {
      expect(parsePriceBound(value)).toEqual({
        value: null,
        invalid: true,
      });
    }
  });
});

describe("display helpers", () => {
  it("formats Tehran times and durations without inventing values", () => {
    expect(
      formatJourneyTimes(
        "2026-11-15T11:20:00Z",
        "2026-11-16T03:00:00Z",
      ),
    ).toEqual({
      departure: "14:50",
      arrival: "06:30",
      duration: "15h 40m",
      dayOffset: 1,
    });
    expect(formatJourneyTimes(null, "bad")).toEqual({
      departure: null,
      arrival: null,
      duration: null,
      dayOffset: null,
    });
  });

  it("formats passengers and polling intervals", () => {
    expect(
      formatPassengerSummary({ adults: 1, children: 0, infants: 0 }),
    ).toBe("1 adult");
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

  it("reports notification readiness without treating missing channels as ready", () => {
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
        {
          id: "p1",
          createdAt: "2026-10-24T12:00:00Z",
          failureCount: 0,
        },
      ],
    };
    expect(notificationReadiness(settings, pushConfig)).toMatchObject({
      ready: true,
      telegram: { label: "Configured", tone: "success", ready: true },
      push: { label: "Enabled · 1 device", tone: "success", ready: true },
    });
    const readiness = notificationReadiness(
      {
        ...settings,
        telegram: {
          available: true,
          configured: true,
          readable: false,
        },
      },
      { ...pushConfig, enabled: false },
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
