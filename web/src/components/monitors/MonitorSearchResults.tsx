import { useEffect, useId, useMemo, useRef, useState } from "react";
import {
  Check,
  ChevronDown,
  Clock3,
  Minus,
} from "lucide-react";
import type {
  ProviderSearchClass,
  SelectedOption,
} from "@/api/types";
import { ProviderPrice } from "@/components/ProviderPrice";
import { ObservationStateBadge } from "@/components/status";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { NativeSelect } from "@/components/ui/input";
import {
  MonitorPriceFilter,
} from "@/components/monitors/MonitorPriceFilter";
import { cn } from "@/lib/utils";
import {
  formatJourneyTimes,
  optionOrdinal,
  selectionKey,
  wagonMeta,
  wagonSelectionState,
  type SearchJourneyGroup,
  type SearchSort,
  type SearchView,
  type SearchWagonGroup,
  type WagonSelectionState,
} from "@/lib/searchResults";
import { formatTehranTimestamp } from "@/lib/dates";
import type { ObservationState } from "@/api/types";

interface MonitorSearchResultsProps {
  groups: SearchJourneyGroup[];
  allGroups: SearchJourneyGroup[];
  view: SearchView;
  selected: Record<string, SelectedOption>;
  disabled: boolean;
  priceMin: string;
  priceMax: string;
  includeUnpriced: boolean;
  priceError?: string;
  selectedOutsideView: number;
  sort: SearchSort;
  activePriceFilter: boolean;
  onPriceMinChange: (value: string) => void;
  onPriceMaxChange: (value: string) => void;
  onIncludeUnpricedChange: (value: boolean) => void;
  onClearPrice: () => void;
  onSortChange: (value: SearchSort) => void;
  onToggleWagon: (
    journey: SearchJourneyGroup,
    wagon: SearchWagonGroup,
  ) => void;
  onToggleOffering: (
    journey: SearchJourneyGroup,
    offering: ProviderSearchClass,
  ) => void;
}

function capacityLabel(capacity: number | null): string {
  return capacity === null ? "Capacity unavailable" : `Capacity ${capacity}`;
}

function journeyTitle(train: SearchJourneyGroup["train"]): string {
  if (train.trainNumber) return `Train ${train.trainNumber}`;
  if (train.nativeId) return `Train ${train.nativeId}`;
  return "Train details unavailable";
}

function JourneyTimes({ train }: { train: SearchJourneyGroup["train"] }) {
  const times = formatJourneyTimes(train.departureAt, train.arrivalAt);
  if (!times.departure && !times.arrival) {
    return <span>Time unavailable</span>;
  }
  const nextDay =
    times.dayOffset !== null && times.dayOffset > 0
      ? `+${times.dayOffset} day${times.dayOffset === 1 ? "" : "s"}`
      : null;
  return (
    <span className="flex flex-wrap items-center gap-x-1.5 gap-y-1">
      <span className="tabular-nums">
        <time dateTime={train.departureAt ?? undefined}>
          {times.departure ?? "—"}
        </time>
        <span aria-hidden="true" className="px-1 text-muted-foreground/60">
          →
        </span>
        <time dateTime={train.arrivalAt ?? undefined}>
          {times.arrival ?? "—"}
        </time>
      </span>
      {nextDay ? <span className="text-foreground/70">{nextDay}</span> : null}
      {times.duration ? (
        <span className="inline-flex items-center gap-1">
          <Clock3 className="size-3" aria-hidden="true" />
          {times.duration}
        </span>
      ) : null}
      <span className="sr-only">
        {formatTehranTimestamp(train.departureAt, "Departure time unavailable")}{" "}
        to {formatTehranTimestamp(train.arrivalAt, "arrival time unavailable")}
      </span>
    </span>
  );
}

function AvailabilityCounts({
  reservable,
  notReservable,
  unknown,
  optionCount,
}: {
  reservable: number;
  notReservable: number;
  unknown: number;
  optionCount: number;
}) {
  return (
    <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[0.7rem] text-muted-foreground">
      <span>
        {reservable} of {optionCount} reservable
      </span>
      {notReservable > 0 ? <span>{notReservable} not reservable</span> : null}
      {unknown > 0 ? (
        <span className="inline-flex items-center gap-1.5 text-warning-foreground dark:text-warning">
          <span className="size-1.5 rounded-full bg-warning" aria-hidden="true" />
          {unknown} unknown
        </span>
      ) : null}
    </span>
  );
}

function StateBadge({ state }: { state: ObservationState }) {
  return <ObservationStateBadge state={state} />;
}

function SelectionCheckbox({
  checked,
  indeterminate = false,
  disabled,
  label,
  onChange,
}: {
  checked: boolean;
  indeterminate?: boolean;
  disabled: boolean;
  label: string;
  onChange: () => void;
}) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = indeterminate;
  }, [indeterminate]);

  return (
    <label className="relative -m-2 flex size-10 shrink-0 items-center justify-center max-sm:size-11">
      <input
        ref={ref}
        type="checkbox"
        checked={checked}
        disabled={disabled}
        aria-label={label}
        onChange={onChange}
        className={cn(
          "peer size-[18px] cursor-pointer appearance-none rounded border border-input bg-card transition-colors checked:border-primary checked:bg-primary focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50",
          indeterminate && "border-primary bg-primary",
        )}
      />
      {indeterminate ? (
        <Minus
          className="pointer-events-none absolute size-3.5 text-primary-foreground"
          strokeWidth={3}
          aria-hidden="true"
        />
      ) : (
        <Check
          className="pointer-events-none absolute size-3.5 text-primary-foreground opacity-0 peer-checked:opacity-100"
          strokeWidth={3}
          aria-hidden="true"
        />
      )}
    </label>
  );
}

function FareRow({
  journey,
  wagon,
  offering,
  index,
  total,
  selected,
  disabled,
  onToggle,
}: {
  journey: SearchJourneyGroup;
  wagon: SearchWagonGroup;
  offering: ProviderSearchClass;
  index: number;
  total: number;
  selected: Record<string, SelectedOption>;
  disabled: boolean;
  onToggle: () => void;
}) {
  const checked = Boolean(
    selected[selectionKey(journey.train.journeyKey, offering.offeringKey)],
  );
  const ordinal = optionOrdinal(index, total) ?? "Fare option";
  const classLabel =
    offering.className && offering.className !== wagon.title
      ? offering.className
      : null;

  return (
    <li className="flex items-start gap-3 py-2.5">
      <SelectionCheckbox
        checked={checked}
        disabled={disabled}
        label={`Select ${wagon.title}, ${ordinal}${classLabel ? `, ${classLabel}` : ""}, ${offering.state.replaceAll("_", " ")}`}
        onChange={onToggle}
      />
      <div className="flex min-w-0 flex-1 flex-wrap items-start justify-between gap-x-4 gap-y-1">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="text-sm">
              {ordinal}
              {classLabel ? (
                <span className="text-muted-foreground"> · {classLabel}</span>
              ) : null}
            </span>
            <StateBadge state={offering.state} />
            {checked ? (
              <span className="text-[0.7rem] font-medium text-muted-foreground">
                Selected
              </span>
            ) : null}
          </div>
          <p className="mt-0.5 text-[0.7rem] text-muted-foreground">
            {capacityLabel(offering.capacity)}
          </p>
        </div>
        <ProviderPrice
          amount={offering.priceAmount}
          currency={offering.priceCurrency}
          className="shrink-0 text-right text-xs"
          showUnverifiedMarker
        />
      </div>
    </li>
  );
}

function WagonRow({
  journey,
  wagon,
  selectionWagon,
  selected,
  disabled,
  onToggleWagon,
  onToggleOffering,
}: {
  journey: SearchJourneyGroup;
  wagon: SearchWagonGroup;
  selectionWagon: SearchWagonGroup;
  selected: Record<string, SelectedOption>;
  disabled: boolean;
  onToggleWagon: () => void;
  onToggleOffering: (offering: ProviderSearchClass) => void;
}) {
  const [open, setOpen] = useState(false);
  const contentId = useId();
  const selectionState: WagonSelectionState = wagonSelectionState(
    journey.train.journeyKey,
    selectionWagon,
    selected,
  );
  const selectedCount = selectionWagon.offerings.filter(
    (offering) =>
      selected[selectionKey(journey.train.journeyKey, offering.offeringKey)],
  ).length;
  const checked = selectionState === "all";
  const indeterminate = selectionState === "partial";
  const meta = wagonMeta(wagon);
  const canExpand = wagon.offerings.length > 1;

  useEffect(() => {
    if (!canExpand) setOpen(false);
  }, [canExpand]);

  return (
    <div
      className={cn(
        "border-t",
        selectionState !== "none" && "bg-muted/25",
      )}
    >
      <div className="flex items-start gap-3 px-4 py-3 sm:px-5">
        <SelectionCheckbox
          checked={checked}
          indeterminate={indeterminate}
          disabled={disabled}
          label={`Select ${wagon.title}, all current fare options`}
          onChange={onToggleWagon}
        />
        <div className="min-w-0 flex-1">
          <div className="flex items-start justify-between gap-2">
            <span className="min-w-0 text-sm font-medium">
              <bdi dir="auto">{wagon.title}</bdi>
            </span>
            <div className="flex shrink-0 items-start gap-2">
              <div className="text-right">
                <ProviderPrice
                  amount={wagon.summary.lowestKnownPrice}
                  currency={wagon.offerings.find(
                    (offering) =>
                      offering.priceAmount ===
                      wagon.summary.lowestKnownPrice,
                  )?.priceCurrency}
                  className="text-sm"
                  showUnverifiedMarker
                />
                {wagon.summary.unpricedCount > 0 ? (
                  <p className="mt-0.5 text-[0.68rem] text-muted-foreground">
                    {wagon.summary.unpricedCount} unpriced
                  </p>
                ) : null}
              </div>
              {canExpand ? (
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="shrink-0"
                  aria-expanded={open}
                  aria-controls={contentId}
                  aria-label={`${open ? "Hide" : "Show"} fare options for ${wagon.title}`}
                  onClick={() => setOpen((value) => !value)}
                >
                  <ChevronDown
                    className={cn(
                      "transition-transform",
                      open && "rotate-180",
                    )}
                    aria-hidden="true"
                  />
                </Button>
              ) : (
                <span className="size-9" aria-hidden="true" />
              )}
            </div>
          </div>
          {meta ? (
            <p className="mt-0.5 text-xs text-muted-foreground">
              <bdi dir="auto">{meta}</bdi>
            </p>
          ) : null}
          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1">
            <StateBadge state={wagon.summary.state} />
            <AvailabilityCounts
              reservable={wagon.summary.reservableCount}
              notReservable={wagon.summary.notReservableCount}
              unknown={wagon.summary.unknownCount}
              optionCount={wagon.summary.optionCount}
            />
            {selectionState === "all" ? (
              <Badge variant="secondary" className="font-normal">
                Selected
              </Badge>
            ) : null}
            {selectionState === "partial" ? (
              <Badge variant="secondary" className="font-normal">
                {selectedCount} of {selectionWagon.offerings.length} selected
              </Badge>
            ) : null}
          </div>
        </div>
      </div>

      {open && canExpand ? (
        <div
          id={contentId}
          className="border-t bg-muted/10 px-4 pt-2 pb-3 sm:px-5"
        >
          <p className="text-[0.7rem] leading-relaxed text-muted-foreground">
            Selecting this wagon tracks all {selectionWagon.offerings.length}{" "}
            current fare options. Choose individual fare options only when you need
            narrower monitoring.
          </p>
          <ul className="mt-1 divide-y divide-border/70" aria-label={`Fare options for ${wagon.title}`}>
            {wagon.offerings.map((offering, index) => (
              <FareRow
                key={offering.offeringKey}
                journey={journey}
                wagon={wagon}
                offering={offering}
                index={index}
                total={wagon.offerings.length}
                selected={selected}
                disabled={disabled}
                onToggle={() => onToggleOffering(offering)}
              />
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  );
}

function JourneySection({
  journey,
  fullWagons,
  selected,
  disabled,
  onToggleWagon,
  onToggleOffering,
}: {
  journey: SearchJourneyGroup;
  fullWagons: Map<string, SearchWagonGroup>;
  selected: Record<string, SelectedOption>;
  disabled: boolean;
  onToggleWagon: (
    journey: SearchJourneyGroup,
    wagon: SearchWagonGroup,
  ) => void;
  onToggleOffering: (
    journey: SearchJourneyGroup,
    offering: ProviderSearchClass,
  ) => void;
}) {
  return (
      <section className="border-t first:border-t-0">
      <header className="flex flex-col gap-2 border-b bg-surface-raised/70 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5">
        <div className="min-w-0">
          <h3 className="text-sm font-semibold">
            <bdi dir="auto">{journeyTitle(journey.train)}</bdi>
            {journey.train.operator ? (
              <span className="font-normal text-muted-foreground">
                {" "}
                · <bdi dir="auto">{journey.train.operator}</bdi>
              </span>
            ) : null}
          </h3>
          <div className="mt-0.5 text-xs text-muted-foreground">
            <JourneyTimes train={journey.train} />
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span>
            {journey.wagons.length} wagon
            {journey.wagons.length === 1 ? "" : "s"}
          </span>
          <span>
            {journey.summary.optionCount} fare option
            {journey.summary.optionCount === 1 ? "" : "s"}
          </span>
          <AvailabilityCounts
            reservable={journey.summary.reservableCount}
            notReservable={journey.summary.notReservableCount}
            unknown={journey.summary.unknownCount}
            optionCount={journey.summary.optionCount}
          />
        </div>
      </header>
      <div>
        {journey.wagons.map((wagon) => {
          const selectionWagon =
            fullWagons.get(`${journey.key}\u0000${wagon.key}`) ?? wagon;
          return (
            <WagonRow
              key={wagon.key}
              journey={journey}
              wagon={wagon}
              selectionWagon={selectionWagon}
              selected={selected}
              disabled={disabled}
              onToggleWagon={() => onToggleWagon(journey, selectionWagon)}
              onToggleOffering={(offering) =>
                onToggleOffering(journey, offering)
              }
            />
          );
        })}
      </div>
    </section>
  );
}

function StateCounts({ view }: { view: SearchView }) {
  const totals = view.groups.reduce(
    (result, journey) => {
      result.wagons += journey.wagons.length;
      result.reservable += journey.summary.reservableCount;
      result.notReservable += journey.summary.notReservableCount;
      result.unknown += journey.summary.unknownCount;
      result.options += journey.summary.optionCount;
      return result;
    },
    { wagons: 0, reservable: 0, notReservable: 0, unknown: 0, options: 0 },
  );
  return (
    <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
      <span>
        {totals.wagons} wagon{totals.wagons === 1 ? "" : "s"}
      </span>
      <span>
        {totals.options} fare option{totals.options === 1 ? "" : "s"}
      </span>
      <span className="inline-flex items-center gap-1.5">
        <span className="size-1.5 rounded-full bg-success" aria-hidden="true" />
        {totals.reservable} reservable
      </span>
      {totals.unknown > 0 ? (
        <span className="inline-flex items-center gap-1.5">
          <span className="size-1.5 rounded-full bg-warning" aria-hidden="true" />
          {totals.unknown} unknown
        </span>
      ) : null}
      {totals.notReservable > 0 ? (
        <span className="inline-flex items-center gap-1.5">
          <span
            className="size-1.5 rounded-full bg-muted-foreground/40"
            aria-hidden="true"
          />
          {totals.notReservable} not reservable
        </span>
      ) : null}
    </span>
  );
}

export function MonitorSearchResults({
  groups,
  allGroups,
  view,
  selected,
  disabled,
  priceMin,
  priceMax,
  includeUnpriced,
  priceError,
  selectedOutsideView,
  sort,
  activePriceFilter,
  onPriceMinChange,
  onPriceMaxChange,
  onIncludeUnpricedChange,
  onClearPrice,
  onSortChange,
  onToggleWagon,
  onToggleOffering,
}: MonitorSearchResultsProps) {
  const fullWagons = useMemo(() => {
    const index = new Map<string, SearchWagonGroup>();
    for (const journey of allGroups) {
      for (const wagon of journey.wagons) {
        index.set(`${journey.key}\u0000${wagon.key}`, wagon);
      }
    }
    return index;
  }, [allGroups]);

  return (
    <section
      aria-labelledby="search-results-heading"
      aria-disabled={disabled || undefined}
      className="flex flex-col gap-3"
    >
      <div className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
        <div>
          <h2
            id="search-results-heading"
            tabIndex={-1}
            className="scroll-mt-24 text-lg font-semibold tracking-tight text-balance focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
          >
            Search results
          </h2>
          <p className="mt-1 text-xs text-muted-foreground">
            Select a wagon to watch all its current fare options. Expand only
            when you need individual control.
          </p>
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-card px-3 py-2 shadow-xs">
        <StateCounts view={view} />
        <div className="flex flex-wrap items-center gap-2">
          <MonitorPriceFilter
            min={priceMin}
            max={priceMax}
            includeUnpriced={includeUnpriced}
            error={priceError}
            disabled={disabled}
            hiddenUnpriced={view.hiddenUnpriced}
            hiddenPricedOutOfRange={view.hiddenPricedOutOfRange}
            knownMin={view.knownPriceMin}
            knownMax={view.knownPriceMax}
            onMinChange={onPriceMinChange}
            onMaxChange={onPriceMaxChange}
            onIncludeUnpricedChange={onIncludeUnpricedChange}
            onClear={onClearPrice}
          />
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <span className="eyebrow text-[0.6rem]">Sort</span>
            <NativeSelect
              aria-label="Sort search results"
              className="h-9 w-36 bg-card text-xs max-sm:h-11"
              value={sort}
              onChange={(event) =>
                onSortChange(event.target.value as SearchSort)
              }
              disabled={disabled}
            >
              <option value="departure">Departure</option>
              <option value="price">Lowest known price</option>
            </NativeSelect>
          </label>
        </div>
      </div>

      {activePriceFilter &&
      (view.hiddenUnpriced > 0 || view.hiddenPricedOutOfRange > 0) ? (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span>
            {view.hiddenUnpriced > 0
              ? `${view.hiddenUnpriced} unpriced fare option${
                  view.hiddenUnpriced === 1 ? "" : "s"
                } hidden`
              : ""}
            {view.hiddenUnpriced > 0 && view.hiddenPricedOutOfRange > 0
              ? " · "
              : ""}
            {view.hiddenPricedOutOfRange > 0
              ? `${view.hiddenPricedOutOfRange} fare option${
                  view.hiddenPricedOutOfRange === 1 ? "" : "s"
                } outside the range`
              : ""}
          </span>
          {view.hiddenUnpriced > 0 && !includeUnpriced ? (
            <Button
              type="button"
              variant="link"
              size="sm"
              className="h-auto p-0 text-xs"
              onClick={() => onIncludeUnpricedChange(true)}
            >
              Include unpriced
            </Button>
          ) : null}
        </div>
      ) : null}

      {selectedOutsideView > 0 ? (
        <p className="rounded-md border border-border/70 bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
          {selectedOutsideView} selected fare option
          {selectedOutsideView === 1 ? " is" : "s are"} outside this view. The
          selection is unchanged and will still be monitored.
        </p>
      ) : null}

      {activePriceFilter && view.groups.length === 0 ? (
        <div className="flex flex-col items-start gap-2 rounded-lg border bg-card px-5 py-8">
          <h3 className="text-sm font-semibold">No wagons in this price range</h3>
          <p className="max-w-xl text-sm text-muted-foreground">
            The train result is still valid. Clear the price filter to see all
            wagons. Unpriced fare options are never treated as zero or as the
            cheapest.
          </p>
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="mt-1"
            onClick={onClearPrice}
          >
            Clear price filter
          </Button>
        </div>
      ) : groups.length > 0 ? (
        <div className="overflow-hidden rounded-[10px] border bg-card shadow-xs">
          {groups.map((journey) => (
            <JourneySection
              key={journey.key}
              journey={journey}
              fullWagons={fullWagons}
              selected={selected}
              disabled={disabled}
              onToggleWagon={onToggleWagon}
              onToggleOffering={onToggleOffering}
            />
          ))}
        </div>
      ) : null}

      <p className="text-[0.7rem] leading-relaxed text-muted-foreground">
        Prices are provider-reported amounts from this search. Currency and
        per-passenger basis are not verified, so amounts are shown without a
        unit. Prices may change after the search.
      </p>
    </section>
  );
}
