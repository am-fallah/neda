import type {
  PassengerCounts,
  ProviderSearchResult,
} from "@/api/types";
import type { SearchWarningSummary } from "@/lib/searchResults";

export const PASSENGER_QUERY_KEYS = [
  "adultCount",
  "childCount",
  "infantCount",
] as const;

type PassengerQueryKey = (typeof PASSENGER_QUERY_KEYS)[number];

export type PassengerSource =
  | "url"
  | "url-defaults"
  | "default"
  | "manual";

export interface UrlPassengerResult {
  status: "absent" | "invalid" | "applied";
  counts?: PassengerCounts;
  presentKeys: PassengerQueryKey[];
  invalidKeys: PassengerQueryKey[];
  defaultedKeys: PassengerQueryKey[];
}

/**
 * Reads passenger counts from a supported MrBilit train URL.
 *
 * MrBilit only emits non-default values in its public search URLs:
 * adultCount is omitted at 1, and childCount/infantCount at 0. Its own URL
 * resolver therefore treats omitted values as those defaults. Missing values
 * are filled with the same provider-defined defaults, but only when at least
 * one passenger parameter is present. A URL with no passenger parameters is
 * not treated as extracted data; the UI labels frontend defaults explicitly.
 *
 * Present-but-invalid values are never silently corrected.
 */
export function parsePassengerCountsFromUrl(rawUrl: string): UrlPassengerResult {
  const presentKeys: PassengerQueryKey[] = [];
  const invalidKeys: PassengerQueryKey[] = [];
  const defaultedKeys: PassengerQueryKey[] = [];
  const value = rawUrl.trim();
  if (!value) {
    return { status: "absent", presentKeys, invalidKeys, defaultedKeys };
  }

  let parsed: URL;
  try {
    parsed = new URL(value);
  } catch {
    return { status: "absent", presentKeys, invalidKeys, defaultedKeys };
  }
  if (
    parsed.protocol !== "https:" ||
    parsed.hostname !== "mrbilit.com" ||
    parsed.port !== "" ||
    parsed.username !== "" ||
    parsed.password !== "" ||
    parsed.hash !== "" ||
    !parsed.pathname.startsWith("/trains/")
  ) {
    return { status: "absent", presentKeys, invalidKeys, defaultedKeys };
  }

  const values = new Map<PassengerQueryKey, number>();
  for (const key of PASSENGER_QUERY_KEYS) {
    const result = readPassengerCount(parsed.searchParams, key);
    if (result.present) {
      presentKeys.push(key);
      if (result.value === null) {
        invalidKeys.push(key);
      } else {
        values.set(key, result.value);
      }
    } else {
      defaultedKeys.push(key);
    }
  }

  if (presentKeys.length === 0) {
    return { status: "absent", presentKeys, invalidKeys, defaultedKeys };
  }
  if (invalidKeys.length > 0) {
    return { status: "invalid", presentKeys, invalidKeys, defaultedKeys };
  }

  const counts: PassengerCounts = {
    adults: values.get("adultCount") ?? 1,
    children: values.get("childCount") ?? 0,
    infants: values.get("infantCount") ?? 0,
  };
  if (!passengerCountsValid(counts)) {
    return { status: "invalid", presentKeys, invalidKeys, defaultedKeys };
  }
  return { status: "applied", counts, presentKeys, invalidKeys, defaultedKeys };
}

function passengerCountsValid(counts: PassengerCounts): boolean {
  if (
    !Number.isInteger(counts.adults) ||
    !Number.isInteger(counts.children) ||
    !Number.isInteger(counts.infants)
  ) {
    return false;
  }
  if (counts.adults < 1 || counts.adults > 9) return false;
  if (counts.children < 0 || counts.children > 9) return false;
  if (counts.infants < 0 || counts.infants > 9) return false;
  return counts.adults + counts.children + counts.infants <= 9;
}

function readPassengerCount(
  params: URLSearchParams,
  key: PassengerQueryKey,
): { present: boolean; value: number | null } {
  const values = params.getAll(key);
  if (values.length === 0) return { present: false, value: null };
  if (values.length !== 1) return { present: true, value: null };
  const raw = values[0];
  if (!/^\d{1,2}$/.test(raw)) return { present: true, value: null };
  const value = Number(raw);
  return {
    present: true,
    value: Number.isSafeInteger(value) ? value : null,
  };
}

export function prettifySlugPart(slug: string, index: number): string {
  const part = slug.split("-")[index] ?? "";
  if (!part) return "";
  return part.charAt(0).toUpperCase() + part.slice(1);
}

export function mrBilitOriginName(result: ProviderSearchResult): string {
  return (
    result.route.originName ||
    prettifySlugPart(result.slug, 0) ||
    result.slug ||
    "Origin"
  );
}

export function mrBilitDestinationName(result: ProviderSearchResult): string {
  return (
    result.route.destinationName ||
    prettifySlugPart(result.slug, 1) ||
    "Destination"
  );
}

export function defaultMrBilitMonitorLabel(
  result: ProviderSearchResult,
): string {
  // Prefer the URL slug for the generated label. Provider display names may be
  // Persian, and mixing them with a Gregorian date in a text input produces
  // confusing bidi reordering. The full provider route is still shown in the
  // summary, and the owner can replace this label with any text.
  const origin = prettifySlugPart(result.slug, 0) || mrBilitOriginName(result);
  const destination =
    prettifySlugPart(result.slug, 1) || mrBilitDestinationName(result);
  const route = destination ? `${origin} → ${destination}` : origin;
  return `${route} — ${result.departureDate}`.trim();
}

export function describeMrBilitWarnings(
  warnings: string[],
): SearchWarningSummary {
  const notices = new Set<string>();
  const diagnostics = new Set<string>();
  for (const warning of warnings) {
    const notice = MRBILIT_WARNING_NOTICES[warning];
    if (notice) {
      notices.add(notice);
    } else {
      diagnostics.add(warning);
    }
  }
  if (notices.size === 0 && diagnostics.size > 0) {
    notices.add(
      "MrBilit returned diagnostic warnings; some fields were not used.",
    );
  }
  return {
    notices: [...notices],
    diagnostics: [...diagnostics],
  };
}

const MRBILIT_WARNING_NOTICES: Record<string, string> = {
  duplicate_journey_key:
    "MrBilit returned duplicate train entries; matching entries were consolidated.",
  duplicate_offering_key:
    "MrBilit returned duplicate class entries; identical duplicates were ignored, and conflicting ones remain unknown.",
  offering_identity_insufficient:
    "Some class entries lacked enough identity information and were skipped.",
  offerings_missing: "At least one train did not include class offerings.",
  meta_missing: "MrBilit returned incomplete response metadata.",
  meta_always_has_train:
    "MrBilit indicated this route always has a train; this is not availability proof.",
  unexpected_display_field_type:
    "Some provider display fields had unexpected values.",
  unexpected_sell_type:
    "Some provider fare-group metadata had an unexpected value.",
  unexpected_train_id_type: "A train identifier had an unexpected value.",
  unexpected_train_number_type:
    "A train number had an unexpected value.",
  unexpected_offer_identity_type:
    "Some class identity fields had unexpected values.",
  unexpected_availability_type:
    "Some availability fields had unexpected values; those classes remain unknown.",
  unexpected_offer_price_type:
    "Some price fields were unusable, so those prices are not shown.",
  invalid_offer_price:
    "Some price fields were invalid, so those prices are not shown.",
  invalid_offering: "A class entry could not be interpreted and was skipped.",
  invalid_train: "A train entry could not be interpreted and was skipped.",
  unexpected_time_type:
    "Some departure or arrival times had unexpected values.",
  unparseable_time:
    "Some departure or arrival times could not be interpreted.",
};
