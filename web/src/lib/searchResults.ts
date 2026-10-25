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

/**
 * Provider-neutral presentation model for a completed train search.
 *
 * A journey is a train. A wagon is a display group of one or more exact
 * offerings. Offerings remain the unit of selection, persistence, polling, and
 * availability; grouping never merges their identities or states.
 */

function normalizeText(value: string | undefined): string {
  return (value ?? "").normalize("NFC").trim().replace(/\s+/g, " ");
}

export type SearchSort = "departure" | "price";

export interface OfferingSummary {
  optionCount: number;
  reservableCount: number;
  notReservableCount: number;
  unknownCount: number;
  pricedCount: number;
  unpricedCount: number;
  lowestKnownPrice: number | null;
  allPricesKnown: boolean;
  state: ObservationState;
}

export interface SearchWagonGroup {
  key: string;
  title: string;
  wagonId: string;
  wagonName: string;
  className: string;
  compartmentType: string;
  offerings: ProviderSearchClass[];
  summary: OfferingSummary;
}

export interface SearchJourneyGroup {
  key: string;
  train: ProviderSearchTrain;
  wagons: SearchWagonGroup[];
  summary: OfferingSummary;
}

export interface SelectionPosition {
  journey: SearchJourneyGroup;
  wagon: SearchWagonGroup;
  offering: ProviderSearchClass;
  index: number;
  total: number;
}

export interface SelectedWagonGroup {
  journey: SearchJourneyGroup;
  wagon: SearchWagonGroup;
  selectedOfferings: ProviderSearchClass[];
  partial: boolean;
  summary: OfferingSummary;
}

export function knownPrice(value: number | null | undefined): number | null {
  // The provider reports a raw amount. Zero, negatives, fractions, and
  // non-finite values are not usable price observations.
  if (
    typeof value !== "number" ||
    !Number.isSafeInteger(value) ||
    value <= 0
  ) {
    return null;
  }
  return value;
}

export function summarizeOfferings(
  offerings: ProviderSearchClass[],
): OfferingSummary {
  let reservable = 0;
  let notReservable = 0;
  let unknown = 0;
  let priced = 0;
  let lowest: number | null = null;

  for (const offering of offerings) {
    switch (offering.state) {
      case "reservable":
        reservable++;
        break;
      case "not_reservable":
        notReservable++;
        break;
      case "unknown":
        unknown++;
        break;
    }
    const amount = knownPrice(offering.priceAmount);
    if (amount !== null) {
      priced++;
      lowest = lowest === null ? amount : Math.min(lowest, amount);
    }
  }

  return {
    optionCount: offerings.length,
    reservableCount: reservable,
    notReservableCount: notReservable,
    unknownCount: unknown,
    pricedCount: priced,
    unpricedCount: offerings.length - priced,
    lowestKnownPrice: lowest,
    allPricesKnown: offerings.length > 0 && priced === offerings.length,
    state: wagonState(reservable, unknown),
  };
}

function wagonState(
  reservableCount: number,
  unknownCount: number,
): ObservationState {
  if (reservableCount > 0) return "reservable";
  if (unknownCount > 0) return "unknown";
  return "not_reservable";
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

/** Stable comparison value for the inputs that determine a search response. */
export function searchInputSignature(inputs: SearchInputs): string {
  return JSON.stringify([
    inputs.url.trim(),
    inputs.adults,
    inputs.children,
    inputs.infants,
  ]);
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

function wagonBaseKey(option: ProviderSearchClass): string {
  const wagonId = normalizeText(option.wagonId);
  if (wagonId) return `id\u0000${wagonId}`;
  const wagonName = normalizeText(option.wagonName);
  if (wagonName) {
    return `name\u0000${wagonName}\u0000${normalizeText(option.compartmentType)}`;
  }
  return `class\u0000${normalizeText(option.className)}\u0000${normalizeText(
    option.compartmentType,
  )}`;
}

function sameWagonIdentity(
  a: ProviderSearchClass,
  b: ProviderSearchClass,
): boolean {
  const aWagonName = normalizeText(a.wagonName);
  const bWagonName = normalizeText(b.wagonName);
  const aCompartment = normalizeText(a.compartmentType);
  const bCompartment = normalizeText(b.compartmentType);
  if (aWagonName && bWagonName && aWagonName !== bWagonName) return false;
  if (
    aCompartment &&
    bCompartment &&
    aCompartment !== bCompartment
  ) {
    return false;
  }
  return true;
}

function wagonTitle(option: ProviderSearchClass): string {
  return (
    normalizeText(option.className) ||
    normalizeText(option.wagonName) ||
    "Wagon details unavailable"
  );
}

function makeWagon(
  key: string,
  option: ProviderSearchClass,
  offerings: ProviderSearchClass[],
): SearchWagonGroup {
  return {
    key,
    title: wagonTitle(option),
    wagonId: normalizeText(option.wagonId),
    wagonName: normalizeText(option.wagonName),
    className: normalizeText(option.className),
    compartmentType: normalizeText(option.compartmentType),
    offerings,
    summary: summarizeOfferings(offerings),
  };
}

function compareOfferings(a: ProviderSearchClass, b: ProviderSearchClass) {
  const stateDifference = stateRank(a.state) - stateRank(b.state);
  if (stateDifference !== 0) return stateDifference;
  const aPrice = knownPrice(a.priceAmount);
  const bPrice = knownPrice(b.priceAmount);
  if (aPrice !== null && bPrice !== null && aPrice !== bPrice) {
    return aPrice - bPrice;
  }
  if (aPrice === null && bPrice !== null) return 1;
  if (aPrice !== null && bPrice === null) return -1;
  return (a.nativeId || a.offeringKey).localeCompare(
    b.nativeId || b.offeringKey,
    "en",
    { numeric: true },
  );
}

function buildWagons(classes: ProviderSearchClass[]): SearchWagonGroup[] {
  const groups: SearchWagonGroup[] = [];
  const duplicateCounts = new Map<string, number>();

  for (const option of classes) {
    const baseKey = wagonBaseKey(option);
    const existing = groups.find(
      (group) =>
        group.key === baseKey ||
        (group.key.startsWith(`${baseKey}~`) &&
          sameWagonIdentity(group.offerings[0], option)),
    );
    if (existing && sameWagonIdentity(existing.offerings[0], option)) {
      existing.offerings.push(option);
      continue;
    }

    let key = baseKey;
    if (existing) {
      const duplicate = (duplicateCounts.get(baseKey) ?? 0) + 1;
      duplicateCounts.set(baseKey, duplicate);
      key = `${baseKey}~${duplicate}`;
    }
    groups.push(makeWagon(key, option, [option]));
  }

  for (const group of groups) {
    group.offerings.sort(compareOfferings);
    group.summary = summarizeOfferings(group.offerings);
  }
  groups.sort((a, b) => {
    const stateDifference =
      stateRank(a.summary.state) - stateRank(b.summary.state);
    if (stateDifference !== 0) return stateDifference;
    return a.title.localeCompare(b.title, "en", {
      numeric: true,
      sensitivity: "base",
    });
  });
  return groups;
}

function makeJourney(
  train: ProviderSearchTrain,
  key: string,
): SearchJourneyGroup {
  return {
    key,
    train,
    wagons: [],
    summary: summarizeOfferings([]),
  };
}

function finalizeJourney(group: SearchJourneyGroup) {
  group.wagons = buildWagons(group.train.classes);
  group.summary = summarizeOfferings(
    group.wagons.flatMap((wagon) => wagon.offerings),
  );
}

function compareJourneys(
  a: SearchJourneyGroup,
  b: SearchJourneyGroup,
): number {
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
 * grouped by wagon identity; every exact offering remains subordinate.
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
      const group = makeJourney(train, uniqueKey);
      byKey.set(uniqueKey, group);
      groups.push(group);
      continue;
    }
    const group = makeJourney(train, baseKey);
    byKey.set(baseKey, group);
    groups.push(group);
  }

  for (const group of groups) {
    finalizeJourney(group);
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
    for (const wagon of journey.wagons) {
      wagon.offerings.forEach((offering, offeringIndex) => {
        index.set(
          selectionKey(journey.train.journeyKey, offering.offeringKey),
          {
            journey,
            wagon,
            offering,
            index: offeringIndex,
            total: wagon.offerings.length,
          },
        );
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

export function optionOrdinal(index: number, total: number): string | null {
  return total > 1 ? `Fare option ${index + 1}` : null;
}

export function wagonMeta(wagon: SearchWagonGroup): string {
  const parts: string[] = [];
  if (wagon.wagonName && wagon.wagonName !== wagon.title) {
    parts.push(wagon.wagonName);
  } else if (!wagon.wagonName && wagon.wagonId) {
    parts.push(`Wagon ${wagon.wagonId}`);
  }
  if (wagon.compartmentType) {
    parts.push(wagon.compartmentType);
  }
  return parts.join(" · ");
}

export type WagonSelectionState = "none" | "partial" | "all";

export function wagonSelectionState(
  journeyKey: string,
  wagon: SearchWagonGroup,
  selected: Record<string, SelectedOption>,
): WagonSelectionState {
  let count = 0;
  for (const offering of wagon.offerings) {
    if (selected[selectionKey(journeyKey, offering.offeringKey)]) count++;
  }
  if (count === 0) return "none";
  return count === wagon.offerings.length ? "all" : "partial";
}

export function selectedOfferingCount(
  selected: Record<string, SelectedOption>,
): number {
  return Object.keys(selected).length;
}

export interface PriceBound {
  value: number | null;
  invalid: boolean;
}

/** Parses a user-entered provider amount without applying a currency. */
export function parsePriceBound(raw: string): PriceBound {
  const normalized = raw.trim().replaceAll(",", "").replaceAll(" ", "");
  if (!normalized) return { value: null, invalid: false };
  if (!/^\d+$/.test(normalized)) return { value: null, invalid: true };
  const value = Number(normalized);
  if (!Number.isSafeInteger(value) || value < 0) {
    return { value: null, invalid: true };
  }
  return { value, invalid: false };
}

export interface SearchViewOptions {
  priceMin: number | null;
  priceMax: number | null;
  includeUnpriced: boolean;
  sort: SearchSort;
}

export interface SearchView {
  groups: SearchJourneyGroup[];
  visibleOfferings: number;
  hiddenUnpriced: number;
  hiddenPricedOutOfRange: number;
  knownPriceMin: number | null;
  knownPriceMax: number | null;
}

function filteredWagon(
  wagon: SearchWagonGroup,
  options: SearchViewOptions,
  filterActive: boolean,
  counters: { hiddenUnpriced: number; hiddenPricedOutOfRange: number },
): SearchWagonGroup | null {
  const offerings = wagon.offerings.filter((offering) => {
    const amount = knownPrice(offering.priceAmount);
    if (amount === null) {
      if (!filterActive) return true;
      if (options.includeUnpriced) return true;
      counters.hiddenUnpriced++;
      return false;
    }
    if (!filterActive) return true;
    if (options.priceMin !== null && amount < options.priceMin) {
      counters.hiddenPricedOutOfRange++;
      return false;
    }
    if (options.priceMax !== null && amount > options.priceMax) {
      counters.hiddenPricedOutOfRange++;
      return false;
    }
    return true;
  });
  if (offerings.length === 0) return null;
  return {
    ...wagon,
    offerings,
    summary: summarizeOfferings(offerings),
  };
}

function sortWagons(
  wagons: SearchWagonGroup[],
  sort: SearchSort,
): SearchWagonGroup[] {
  return [...wagons].sort((a, b) => {
    if (sort === "price") {
      const aPrice = a.summary.lowestKnownPrice;
      const bPrice = b.summary.lowestKnownPrice;
      if (aPrice !== null && bPrice !== null && aPrice !== bPrice) {
        return aPrice - bPrice;
      }
      if (aPrice === null && bPrice !== null) return 1;
      if (aPrice !== null && bPrice === null) return -1;
    }
    const stateDifference =
      stateRank(a.summary.state) - stateRank(b.summary.state);
    if (stateDifference !== 0) return stateDifference;
    return a.title.localeCompare(b.title, "en", {
      numeric: true,
      sensitivity: "base",
    });
  });
}

function viewJourney(
  journey: SearchJourneyGroup,
  options: SearchViewOptions,
  filterActive: boolean,
  counters: { hiddenUnpriced: number; hiddenPricedOutOfRange: number },
): SearchJourneyGroup | null {
  const wagons = journey.wagons
    .map((wagon) => filteredWagon(wagon, options, filterActive, counters))
    .filter((wagon): wagon is SearchWagonGroup => wagon !== null);
  if (wagons.length === 0) return null;
  const sortedWagons = sortWagons(wagons, options.sort);
  return {
    ...journey,
    wagons: sortedWagons,
    summary: summarizeOfferings(
      sortedWagons.flatMap((wagon) => wagon.offerings),
    ),
  };
}

/**
 * Applies view-only price filtering and sorting. The returned journeys are
 * copies; no offering identity or selection is changed.
 */
export function applySearchView(
  groups: SearchJourneyGroup[],
  options: SearchViewOptions,
): SearchView {
  const filterActive =
    options.priceMin !== null || options.priceMax !== null;
  const counters = { hiddenUnpriced: 0, hiddenPricedOutOfRange: 0 };
  const knownPrices = groups
    .flatMap((journey) => journey.wagons)
    .flatMap((wagon) => wagon.offerings)
    .map((offering) => knownPrice(offering.priceAmount))
    .filter((value): value is number => value !== null);

  let visible = groups
    .map((journey) =>
      viewJourney(journey, options, filterActive, counters),
    )
    .filter((journey): journey is SearchJourneyGroup => journey !== null);

  if (options.sort === "price") {
    visible = [...visible].sort((a, b) => {
      const aPrice = a.summary.lowestKnownPrice;
      const bPrice = b.summary.lowestKnownPrice;
      if (aPrice !== null && bPrice !== null && aPrice !== bPrice) {
        return aPrice - bPrice;
      }
      if (aPrice === null && bPrice !== null) return 1;
      if (aPrice !== null && bPrice === null) return -1;
      return compareJourneys(a, b);
    });
  } else {
    visible = [...visible].sort(compareJourneys);
  }

  return {
    groups: visible,
    visibleOfferings: visible.reduce(
      (total, journey) => total + journey.summary.optionCount,
      0,
    ),
    hiddenUnpriced: filterActive ? counters.hiddenUnpriced : 0,
    hiddenPricedOutOfRange: filterActive
      ? counters.hiddenPricedOutOfRange
      : 0,
    knownPriceMin:
      knownPrices.length > 0 ? Math.min(...knownPrices) : null,
    knownPriceMax:
      knownPrices.length > 0 ? Math.max(...knownPrices) : null,
  };
}

export function countSelectedOutsideView(
  visibleGroups: SearchJourneyGroup[],
  selected: Record<string, SelectedOption>,
): number {
  const visible = new Set<string>();
  for (const journey of visibleGroups) {
    for (const wagon of journey.wagons) {
      for (const offering of wagon.offerings) {
        visible.add(
          selectionKey(journey.train.journeyKey, offering.offeringKey),
        );
      }
    }
  }
  let count = 0;
  for (const key of Object.keys(selected)) {
    if (!visible.has(key)) count++;
  }
  return count;
}

export function groupSelectedWagons(
  groups: SearchJourneyGroup[],
  selected: Record<string, SelectedOption>,
): SelectedWagonGroup[] {
  const result: SelectedWagonGroup[] = [];
  for (const journey of groups) {
    for (const wagon of journey.wagons) {
      const selectedOfferings = wagon.offerings.filter(
        (offering) =>
          Boolean(
            selected[
              selectionKey(journey.train.journeyKey, offering.offeringKey)
            ],
          ),
      );
      if (selectedOfferings.length === 0) continue;
      result.push({
        journey,
        wagon,
        selectedOfferings,
        partial: selectedOfferings.length < wagon.offerings.length,
        summary: summarizeOfferings(selectedOfferings),
      });
    }
  }
  return result;
}

export function describeSelectedWagon(item: SelectedWagonGroup): string {
  const train =
    item.journey.train.trainNumber ||
    item.journey.train.nativeId ||
    "Train";
  return [`Train ${train}`, item.wagon.title].filter(Boolean).join(" · ");
}


export interface MonitorSelectionGroup {
  key: string;
  journeyKey: string;
  trainNumber: string;
  wagonName: string;
  className: string;
  compartmentType: string;
  selections: SelectedOption[];
}

/** Groups saved monitor selections by journey and wagon for display only. */
export function groupMonitorSelections(
  selections: SelectedOption[],
): MonitorSelectionGroup[] {
  const groups: MonitorSelectionGroup[] = [];
  const index = new Map<string, MonitorSelectionGroup>();
  for (const selection of selections) {
    const wagonIdentity = [
      selection.wagonId ?? "",
      selection.wagonName ?? "",
      selection.compartmentType ?? "",
    ].join("\u0000");
    const key = `${selection.journeyKey}\u0000${
      wagonIdentity ||
      [selection.className ?? "", selection.offeringKey].join("\u0000")
    }`;
    const existing = index.get(key);
    if (existing) {
      existing.selections.push(selection);
      continue;
    }
    const group: MonitorSelectionGroup = {
      key,
      journeyKey: selection.journeyKey,
      trainNumber: selection.trainNumber ?? "",
      wagonName: selection.wagonName ?? "",
      className: selection.className ?? "",
      compartmentType: selection.compartmentType ?? "",
      selections: [selection],
    };
    index.set(key, group);
    groups.push(group);
  }
  return groups;
}

export function monitorSelectionTitle(group: MonitorSelectionGroup): string {
  return group.wagonName || group.className || "Wagon/class details unavailable";
}

export function monitorSelectionMeta(group: MonitorSelectionGroup): string {
  const parts: string[] = [];
  if (group.className && group.className !== group.wagonName) {
    parts.push(group.className);
  }
  if (group.compartmentType) parts.push(group.compartmentType);
  return parts.join(" · ");
}

export function resultOriginName(result: ProviderSearchResult): string {
  return result.route.originName || "Origin";
}

export function resultDestinationName(result: ProviderSearchResult): string {
  return result.route.destinationName || "Destination";
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
  const arrivalValid =
    arrivalDate !== null && !Number.isNaN(arrivalDate.getTime());
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
