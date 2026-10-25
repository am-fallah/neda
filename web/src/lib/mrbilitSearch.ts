import type {
  ObservationState,
  PassengerCounts,
  ProviderSearchClass,
  ProviderSearchResult,
  ProviderSearchTrain,
  PushConfig,
  SelectedOption,
  Settings,
} from "@/api/types";
import { TEHRAN_TIME_ZONE } from "@/lib/dates";

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

function normalizeText(value: string | undefined): string {
  return (value ?? "").normalize("NFC").trim().replace(/\s+/g, " ");
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

export function passengerCountsValid(counts: PassengerCounts): boolean {
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

export interface SearchInputs {
  url: string;
  adults: number;
  children: number;
  infants: number;
}

/**
 * Stable comparison value for the inputs that determine the meaning of a
 * search response. Label edits are intentionally excluded.
 */
export function searchInputSignature(inputs: SearchInputs): string {
  return JSON.stringify([
    inputs.url.trim(),
    inputs.adults,
    inputs.children,
    inputs.infants,
  ]);
}

export function stateRank(state: ObservationState): number {
  switch (state) {
    case "reservable":
      return 0;
    case "unknown":
      return 1;
    case "not_reservable":
      return 2;
  }
}

export interface SearchClassFamily {
  key: string;
  title: string;
  className: string;
  wagonId: string;
  wagonName: string;
  compartmentType: string;
  options: ProviderSearchClass[];
}

export interface SearchJourneyGroup {
  key: string;
  train: ProviderSearchTrain;
  families: SearchClassFamily[];
  optionCount: number;
  reservableCount: number;
  notReservableCount: number;
  unknownCount: number;
}

export interface SelectionPosition {
  journey: SearchJourneyGroup;
  family: SearchClassFamily;
  option: ProviderSearchClass;
  index: number;
  total: number;
}

function sameJourneyMetadata(
  a: ProviderSearchTrain,
  b: ProviderSearchTrain,
): boolean {
  return (
    (a.nativeId ?? "") === (b.nativeId ?? "") &&
    (a.trainNumber ?? "") === (b.trainNumber ?? "") &&
    (a.operator ?? "") === (b.operator ?? "") &&
    (a.originName ?? "") === (b.originName ?? "") &&
    (a.destinationName ?? "") === (b.destinationName ?? "") &&
    (a.departureAt ?? "") === (b.departureAt ?? "") &&
    (a.arrivalAt ?? "") === (b.arrivalAt ?? "")
  );
}

export function classFamilyKey(option: ProviderSearchClass): string {
  return [
    normalizeText(option.wagonId),
    normalizeText(option.wagonName),
    normalizeText(option.className),
    normalizeText(option.compartmentType),
  ].join("\u0000");
}

function familyTitle(option: ProviderSearchClass): string {
  return normalizeText(option.className) || normalizeText(option.wagonName);
}

function buildFamilies(classes: ProviderSearchClass[]): SearchClassFamily[] {
  const families = new Map<string, SearchClassFamily>();
  for (const option of classes) {
    const key = classFamilyKey(option);
    const existing = families.get(key);
    if (existing) {
      existing.options.push(option);
      continue;
    }
    families.set(key, {
      key,
      title: familyTitle(option) || "Class details unavailable",
      className: normalizeText(option.className),
      wagonId: normalizeText(option.wagonId),
      wagonName: normalizeText(option.wagonName),
      compartmentType: normalizeText(option.compartmentType),
      options: [option],
    });
  }

  const list = [...families.values()];
  for (const family of list) {
    family.options.sort(compareOptions);
  }
  list.sort((a, b) => {
    const stateDifference =
      stateRank(a.options[0].state) - stateRank(b.options[0].state);
    if (stateDifference !== 0) return stateDifference;
    return a.title.localeCompare(b.title, "en", {
      numeric: true,
      sensitivity: "base",
    });
  });
  return list;
}

function compareOptions(a: ProviderSearchClass, b: ProviderSearchClass) {
  const stateDifference = stateRank(a.state) - stateRank(b.state);
  if (stateDifference !== 0) return stateDifference;
  if (a.capacity !== null && b.capacity !== null && a.capacity !== b.capacity) {
    return b.capacity - a.capacity;
  }
  if (a.capacity === null && b.capacity !== null) return 1;
  if (a.capacity !== null && b.capacity === null) return -1;
  return (a.nativeId || a.offeringKey).localeCompare(
    b.nativeId || b.offeringKey,
    "en",
    { numeric: true },
  );
}

function makeGroup(train: ProviderSearchTrain, key: string): SearchJourneyGroup {
  return {
    key,
    train,
    families: [],
    optionCount: 0,
    reservableCount: 0,
    notReservableCount: 0,
    unknownCount: 0,
  };
}

function finalizeGroup(group: SearchJourneyGroup) {
  group.families = buildFamilies(group.train.classes);
  const options = group.families.flatMap((family) => family.options);
  group.optionCount = options.length;
  group.reservableCount = options.filter(
    (option) => option.state === "reservable",
  ).length;
  group.notReservableCount = options.filter(
    (option) => option.state === "not_reservable",
  ).length;
  group.unknownCount = options.filter(
    (option) => option.state === "unknown",
  ).length;
}

function compareJourneys(a: SearchJourneyGroup, b: SearchJourneyGroup): number {
  const aTime = a.train.departureAt ? Date.parse(a.train.departureAt) : NaN;
  const bTime = b.train.departureAt ? Date.parse(b.train.departureAt) : NaN;
  const aSort = Number.isNaN(aTime) ? Number.POSITIVE_INFINITY : aTime;
  const bSort = Number.isNaN(bTime) ? Number.POSITIVE_INFINITY : bTime;
  if (aSort !== bSort) return aSort - bSort;
  const trainDifference = (a.train.trainNumber ?? "").localeCompare(
    b.train.trainNumber ?? "",
    "en",
    { numeric: true },
  );
  if (trainDifference !== 0) return trainDifference;
  return a.key.localeCompare(b.key, "en", { numeric: true });
}

/**
 * Groups provider journeys without merging distinct journey identities. Duplicate
 * journey keys are combined only when their metadata agrees. Classes are then
 * grouped under strict wagon/class/compartment identity; every offering remains
 * a separate selectable option.
 */
export function groupSearchTrains(
  trains: ProviderSearchTrain[],
): SearchJourneyGroup[] {
  const groups: SearchJourneyGroup[] = [];
  const byKey = new Map<string, SearchJourneyGroup>();
  const duplicateCounts = new Map<string, number>();

  for (const train of trains) {
    const baseKey = train.journeyKey || "journey-without-key";
    const existing = byKey.get(baseKey);
    if (existing) {
      if (sameJourneyMetadata(existing.train, train)) {
        existing.train = {
          ...existing.train,
          classes: [...existing.train.classes, ...train.classes],
        };
        continue;
      }
      const duplicate = (duplicateCounts.get(baseKey) ?? 0) + 1;
      duplicateCounts.set(baseKey, duplicate);
      const uniqueKey = `${baseKey}~${duplicate}`;
      const group = makeGroup(train, uniqueKey);
      byKey.set(uniqueKey, group);
      groups.push(group);
      continue;
    }
    const group = makeGroup(train, baseKey);
    byKey.set(baseKey, group);
    groups.push(group);
  }

  for (const group of groups) {
    finalizeGroup(group);
  }
  return groups.sort(compareJourneys);
}

export function selectionKey(
  journeyKey: string,
  offeringKey: string,
): string {
  return `${journeyKey}\u0000${offeringKey}`;
}

export function buildSelectionIndex(
  groups: SearchJourneyGroup[],
): Map<string, SelectionPosition> {
  const index = new Map<string, SelectionPosition>();
  for (const journey of groups) {
    for (const family of journey.families) {
      family.options.forEach((option, optionIndex) => {
        index.set(selectionKey(journey.train.journeyKey, option.offeringKey), {
          journey,
          family,
          option,
          index: optionIndex,
          total: family.options.length,
        });
      });
    }
  }
  return index;
}

export function toSelectedOption(
  train: ProviderSearchTrain,
  item: ProviderSearchClass,
): SelectedOption {
  return {
    journeyKey: train.journeyKey,
    offeringKey: item.offeringKey,
    journeyNativeId: train.nativeId,
    offeringNativeId: item.nativeId,
    trainNumber: train.trainNumber,
    operator: train.operator,
    originName: train.originName,
    destinationName: train.destinationName,
    departureAt: train.departureAt,
    arrivalAt: train.arrivalAt,
    wagonId: item.wagonId,
    wagonName: item.wagonName,
    className: item.className,
    compartmentType: item.compartmentType,
  };
}

export function classFamilyMeta(family: SearchClassFamily): string {
  const parts: string[] = [];
  if (family.wagonName && family.wagonName !== family.title) {
    parts.push(family.wagonName);
  } else if (!family.wagonName && family.wagonId) {
    parts.push(`Wagon ${family.wagonId}`);
  }
  if (family.compartmentType) {
    parts.push(family.compartmentType);
  }
  return parts.join(" · ");
}

export function optionOrdinal(index: number, total: number): string | null {
  return total > 1 ? `Option ${index + 1}` : null;
}

export function describeSelection(position: SelectionPosition): string {
  const train =
    position.journey.train.trainNumber ||
    position.journey.train.nativeId ||
    "Train";
  const ordinal = optionOrdinal(position.index, position.total);
  return [`Train ${train}`, position.family.title, ordinal]
    .filter(Boolean)
    .join(" · ");
}

export interface JourneyTimeDisplay {
  departure: string | null;
  arrival: string | null;
  duration: string | null;
  dayOffset: number | null;
}

const timeFormatter = new Intl.DateTimeFormat("en-GB", {
  timeZone: TEHRAN_TIME_ZONE,
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});

const dateFormatter = new Intl.DateTimeFormat("en-US", {
  timeZone: TEHRAN_TIME_ZONE,
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
});

function dateKeyInTehran(value: Date): string | null {
  if (Number.isNaN(value.getTime())) return null;
  const parts: Record<string, string> = {};
  for (const part of dateFormatter.formatToParts(value)) {
    if (part.type !== "literal") parts[part.type] = part.value;
  }
  if (!parts.year || !parts.month || !parts.day) return null;
  return `${parts.year}-${parts.month}-${parts.day}`;
}

function dayNumber(value: string): number {
  const [year, month, day] = value.split("-").map(Number);
  return Date.UTC(year, month - 1, day) / 86_400_000;
}

function formatDuration(milliseconds: number): string | null {
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return null;
  const minutes = Math.round(milliseconds / 60_000);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remainingMinutes = minutes % 60;
  if (hours >= 24) {
    const days = Math.floor(hours / 24);
    const remainingHours = hours % 24;
    return `${days}d${remainingHours ? ` ${remainingHours}h` : ""}`;
  }
  return `${hours}h${remainingMinutes ? ` ${remainingMinutes}m` : ""}`;
}

export function formatJourneyTimes(
  departureAt: string | null | undefined,
  arrivalAt: string | null | undefined,
): JourneyTimeDisplay {
  const departureDate = departureAt ? new Date(departureAt) : null;
  const arrivalDate = arrivalAt ? new Date(arrivalAt) : null;
  const departureValid =
    departureDate !== null && !Number.isNaN(departureDate.getTime());
  const arrivalValid = arrivalDate !== null && !Number.isNaN(arrivalDate.getTime());
  const departure = departureValid
    ? timeFormatter.format(departureDate)
    : null;
  const arrival = arrivalValid ? timeFormatter.format(arrivalDate) : null;

  if (!departureValid || !arrivalValid) {
    return { departure, arrival, duration: null, dayOffset: null };
  }

  const departureKey = dateKeyInTehran(departureDate);
  const arrivalKey = dateKeyInTehran(arrivalDate);
  const dayOffset =
    departureKey && arrivalKey
      ? dayNumber(arrivalKey) - dayNumber(departureKey)
      : null;
  return {
    departure,
    arrival,
    duration: formatDuration(arrivalDate.getTime() - departureDate.getTime()),
    dayOffset,
  };
}

export function prettifySlugPart(slug: string, index: number): string {
  const part = slug.split("-")[index] ?? "";
  if (!part) return "";
  return part.charAt(0).toUpperCase() + part.slice(1);
}

export function resultOriginName(result: ProviderSearchResult): string {
  return (
    result.route.originName ||
    prettifySlugPart(result.slug, 0) ||
    result.slug
  );
}

export function resultDestinationName(result: ProviderSearchResult): string {
  return result.route.destinationName || prettifySlugPart(result.slug, 1);
}

export function defaultMonitorLabel(result: ProviderSearchResult): string {
  // Prefer the URL slug for the generated label. Provider display names may be
  // Persian, and mixing them with a Gregorian date in a text input produces
  // confusing bidi reordering. The full provider route is still shown in the
  // summary, and the owner can replace this label with any text.
  const origin = prettifySlugPart(result.slug, 0) || resultOriginName(result);
  const destination =
    prettifySlugPart(result.slug, 1) || resultDestinationName(result);
  const route = destination ? `${origin} → ${destination}` : origin;
  return `${route} — ${result.departureDate}`.trim();
}

export function formatPassengerSummary(
  counts: PassengerCounts,
  includeZero = false,
): string {
  const entries = [
    {
      value: counts.adults,
      label: `${counts.adults} adult${counts.adults === 1 ? "" : "s"}`,
      always: true,
    },
    {
      value: counts.children,
      label: `${counts.children} child${counts.children === 1 ? "" : "ren"}`,
      always: false,
    },
    {
      value: counts.infants,
      label: `${counts.infants} infant${counts.infants === 1 ? "" : "s"}`,
      always: false,
    },
  ];
  return entries
    .filter((entry) => includeZero || entry.always || entry.value > 0)
    .map((entry) => entry.label)
    .join(" · ");
}

export function pollIntervalLabel(seconds: number): string {
  if (seconds > 0 && seconds % 3600 === 0) {
    const hours = seconds / 3600;
    return `Every ${hours} hour${hours === 1 ? "" : "s"}`;
  }
  if (seconds > 0 && seconds % 60 === 0) {
    const minutes = seconds / 60;
    return `Every ${minutes} minute${minutes === 1 ? "" : "s"}`;
  }
  return `Every ${seconds} seconds`;
}

export type ChannelTone = "success" | "warning" | "muted";

export interface ChannelStatus {
  label: string;
  tone: ChannelTone;
  ready: boolean;
}

export interface NotificationReadiness {
  telegram: ChannelStatus;
  push: ChannelStatus;
  ready: boolean;
}

export function notificationReadiness(
  settings: Settings,
  pushConfig: PushConfig,
): NotificationReadiness {
  let telegram: ChannelStatus;
  if (!settings.telegram.available) {
    telegram = { label: "Unavailable", tone: "muted", ready: false };
  } else if (settings.telegram.configured && settings.telegram.readable) {
    telegram = { label: "Configured", tone: "success", ready: true };
  } else if (settings.telegram.configured) {
    telegram = { label: "Needs re-entry", tone: "warning", ready: false };
  } else {
    telegram = { label: "Not configured", tone: "muted", ready: false };
  }

  let push: ChannelStatus;
  if (!pushConfig.available) {
    push = { label: "Unavailable", tone: "muted", ready: false };
  } else if (pushConfig.enabled && pushConfig.subscriptions.length > 0) {
    const devices = pushConfig.subscriptions.length;
    push = {
      label: `Enabled · ${devices} device${devices === 1 ? "" : "s"}`,
      tone: "success",
      ready: true,
    };
  } else if (pushConfig.enabled) {
    push = { label: "Enabled · no devices", tone: "warning", ready: false };
  } else {
    push = { label: "Disabled", tone: "muted", ready: false };
  }

  return { telegram, push, ready: telegram.ready || push.ready };
}

export interface SearchWarningSummary {
  notices: string[];
  diagnostics: string[];
}

const WARNING_NOTICES: Record<string, string> = {
  duplicate_journey_key:
    "MrBilit returned duplicate train entries; matching entries were consolidated.",
  duplicate_offering_key:
    "MrBilit returned duplicate class entries; identical duplicates were ignored, and conflicting ones remain unknown.",
  offering_identity_insufficient:
    "Some class entries lacked enough identity information and were skipped.",
  offerings_missing:
    "At least one train did not include class offerings.",
  meta_missing: "MrBilit returned incomplete response metadata.",
  meta_always_has_train:
    "MrBilit indicated this route always has a train; this is not availability proof.",
  unexpected_display_field_type:
    "Some provider display fields had unexpected values.",
  unexpected_sell_type:
    "Some provider fare-group metadata had an unexpected value.",
  unexpected_train_id_type:
    "A train identifier had an unexpected value.",
  unexpected_train_number_type:
    "A train number had an unexpected value.",
  unexpected_offer_identity_type:
    "Some class identity fields had unexpected values.",
  unexpected_availability_type:
    "Some availability fields had unexpected values; those classes remain unknown.",
  unexpected_offer_price_type:
    "Some price fields were unusable, so prices are not shown.",
  invalid_offer_price:
    "Some price fields were invalid, so prices are not shown.",
  invalid_offering: "A class entry could not be interpreted and was skipped.",
  invalid_train: "A train entry could not be interpreted and was skipped.",
  unexpected_time_type:
    "Some departure or arrival times had unexpected values.",
  unparseable_time:
    "Some departure or arrival times could not be interpreted.",
};

export function describeSearchWarnings(warnings: string[]): SearchWarningSummary {
  const notices = new Set<string>();
  const diagnostics = new Set<string>();
  for (const warning of warnings) {
    const notice = WARNING_NOTICES[warning];
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
