import { describe, expect, it } from "vitest";
import type { ProviderSearchResult } from "@/api/types";
import {
  defaultMrBilitMonitorLabel,
  describeMrBilitWarnings,
  mrBilitDestinationName,
  mrBilitOriginName,
  parsePassengerCountsFromUrl,
  prettifySlugPart,
} from "./mrbilit";

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
    trains: [],
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

  it("applies MrBilit's omitted defaults for a standard adult-and-child URL", () => {
    const parsed = parsePassengerCountsFromUrl(
      "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24&adultCount=2&childCount=1",
    );
    expect(parsed.status).toBe("applied");
    expect(parsed.counts).toEqual({ adults: 2, children: 1, infants: 0 });
    expect(parsed.presentKeys).toEqual(["adultCount", "childCount"]);
    expect(parsed.defaultedKeys).toEqual(["infantCount"]);
  });

  it("uses the provider default of one adult when only child or infant counts are present", () => {
    expect(
      parsePassengerCountsFromUrl(
        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24&childCount=1",
      ).counts,
    ).toEqual({ adults: 1, children: 1, infants: 0 });
    expect(
      parsePassengerCountsFromUrl(
        "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24&infantCount=1",
      ).counts,
    ).toEqual({ adults: 1, children: 0, infants: 1 });
  });

  it("applies an adult-only URL with zero children and infants", () => {
    const parsed = parsePassengerCountsFromUrl(
      "https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24&adultCount=2",
    );
    expect(parsed.status).toBe("applied");
    expect(parsed.counts).toEqual({ adults: 2, children: 0, infants: 0 });
    expect(parsed.presentKeys).toEqual(["adultCount"]);
    expect(parsed.defaultedKeys).toEqual(["childCount", "infantCount"]);
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

describe("MrBilit presentation helpers", () => {
  it("uses route names and falls back to the slug without inventing cities", () => {
    const result = makeResult();
    expect(mrBilitOriginName(result)).toBe("Yazd");
    expect(mrBilitDestinationName(result)).toBe("Mashhad");
    expect(
      mrBilitOriginName(
        makeResult({
          route: {
            originId: "1",
            destinationId: "2",
            originName: "",
            destinationName: "",
          },
        }),
      ),
    ).toBe("Yazd");
    expect(prettifySlugPart("yazd-mashhad", 1)).toBe("Mashhad");
  });

  it("builds a stable label from the slug and Gregorian date", () => {
    expect(defaultMrBilitMonitorLabel(makeResult())).toBe(
      "Yazd → Mashhad — 2026-11-15",
    );
    expect(
      defaultMrBilitMonitorLabel(
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

  it("translates known warning codes and separates diagnostics", () => {
    const summary = describeMrBilitWarnings([
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
    expect(describeMrBilitWarnings(["some_future_code"]).notices).toEqual([
      "MrBilit returned diagnostic warnings; some fields were not used.",
    ]);
  });
});
