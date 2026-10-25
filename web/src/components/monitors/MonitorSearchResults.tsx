import { useId } from "react";
import { Check, Clock3, TrainFront } from "lucide-react";
import type {
  ProviderSearchClass,
  ProviderSearchTrain,
  SelectedOption,
} from "@/api/types";
import { ObservationStateBadge } from "@/components/status";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import {
  classFamilyMeta,
  formatJourneyTimes,
  type SearchClassFamily,
  type SearchJourneyGroup,
  optionOrdinal,
  selectionKey,
} from "@/lib/mrbilitSearch";
import { formatTehranTimestamp } from "@/lib/dates";

interface MonitorSearchResultsProps {
  groups: SearchJourneyGroup[];
  selected: Record<string, SelectedOption>;
  disabled: boolean;
  onToggle: (
    train: ProviderSearchTrain,
    option: ProviderSearchClass,
  ) => void;
}

function capacityLabel(capacity: number | null): string {
  return capacity === null ? "Capacity unavailable" : `Capacity ${capacity}`;
}

function journeyTitle(train: ProviderSearchTrain): string {
  if (train.trainNumber) return `Train ${train.trainNumber}`;
  if (train.nativeId) return `Train ${train.nativeId}`;
  return "Train details unavailable";
}

function JourneyTimes({ train }: { train: ProviderSearchTrain }) {
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

function StateCounts({ journey }: { journey: SearchJourneyGroup }) {
  const items = [
    {
      count: journey.reservableCount,
      label: "reservable",
      dot: "bg-success",
    },
    {
      count: journey.unknownCount,
      label: "unknown",
      dot: "bg-warning",
    },
    {
      count: journey.notReservableCount,
      label: "not reservable",
      dot: "bg-muted-foreground/40",
    },
  ].filter((item) => item.count > 0);
  return (
    <span className="flex flex-wrap items-center justify-end gap-x-2 gap-y-1 text-[0.7rem] text-muted-foreground">
      {items.map((item) => (
        <span key={item.label} className="inline-flex items-center gap-1">
          <span
            className={cn("size-1.5 rounded-full", item.dot)}
            aria-hidden="true"
          />
          {item.count} {item.label}
        </span>
      ))}
    </span>
  );
}

function SelectionBadge() {
  return (
    <Badge
      variant="outline"
      className="gap-1 border-primary/30 bg-background font-medium"
    >
      <Check className="size-3" aria-hidden="true" />
      Selected
    </Badge>
  );
}

function ClassOptionRow({
  journey,
  family,
  option,
  optionIndex,
  optionTotal,
  checked,
  disabled,
  grouped,
  onToggle,
}: {
  journey: SearchJourneyGroup;
  family: SearchClassFamily;
  option: ProviderSearchClass;
  optionIndex: number;
  optionTotal: number;
  checked: boolean;
  disabled: boolean;
  grouped: boolean;
  onToggle: (
    train: ProviderSearchTrain,
    option: ProviderSearchClass,
  ) => void;
}) {
  const ordinal = optionOrdinal(optionIndex, optionTotal);
  const meta = classFamilyMeta(family);
  const providerReference = option.nativeId || option.offeringKey;
  return (
    <label
      title={`Provider option ${providerReference}`}
      className={cn(
        "flex min-w-0 scroll-mt-24 cursor-pointer items-start gap-3 px-4 py-3.5 transition-colors sm:gap-4",
        checked ? "bg-muted/45" : "hover:bg-muted/25",
        disabled &&
          "cursor-not-allowed opacity-55 hover:bg-transparent focus-within:bg-transparent",
        "focus-within:bg-muted/35",
      )}
    >
      <span className="relative mt-0.5 flex size-4 shrink-0 items-center justify-center">
        <input
          type="checkbox"
          name="mrbilit-selection"
          value={option.offeringKey}
          checked={checked}
          disabled={disabled}
          onChange={() => onToggle(journey.train, option)}
          aria-describedby={disabled ? "search-stale-notice" : undefined}
          className="peer size-4 cursor-pointer appearance-none rounded border border-muted-foreground/70 bg-background checked:border-primary checked:bg-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed"
        />
        <Check
          className="pointer-events-none absolute top-1/2 left-1/2 size-3 -translate-x-1/2 -translate-y-1/2 text-primary-foreground opacity-0 peer-checked:opacity-100"
          strokeWidth={3}
          aria-hidden="true"
        />
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
          <span className="min-w-0 break-words text-sm font-medium">
            <bdi dir="auto">
              {grouped ? (ordinal ?? family.title) : family.title}
            </bdi>
          </span>
          <ObservationStateBadge state={option.state} />
          {checked ? <SelectionBadge /> : null}
        </span>
        <span className="mt-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
          {!grouped && meta ? (
            <>
              <bdi dir="auto">{meta}</bdi>
              <span aria-hidden="true">·</span>
            </>
          ) : null}
          <span title="Provider-reported capacity">
            {capacityLabel(option.capacity)}
          </span>
        </span>
      </span>
    </label>
  );
}

function ClassFamilyBlock({
  journey,
  family,
  selected,
  disabled,
  onToggle,
}: {
  journey: SearchJourneyGroup;
  family: SearchClassFamily;
  selected: Record<string, SelectedOption>;
  disabled: boolean;
  onToggle: (
    train: ProviderSearchTrain,
    option: ProviderSearchClass,
  ) => void;
}) {
  const headingId = useId();
  const meta = classFamilyMeta(family);

  if (family.options.length === 1) {
    return (
      <ClassOptionRow
        journey={journey}
        family={family}
        option={family.options[0]}
        optionIndex={0}
        optionTotal={1}
        checked={Boolean(
          selected[
            selectionKey(
              journey.train.journeyKey,
              family.options[0].offeringKey,
            )
          ],
        )}
        disabled={disabled}
        grouped={false}
        onToggle={onToggle}
      />
    );
  }
  return (
    <div role="group" aria-labelledby={headingId}>
      <div className="flex flex-wrap items-start justify-between gap-2 border-b bg-muted/25 px-4 py-2.5">
        <div className="min-w-0">
          <h4 id={headingId} className="text-xs font-semibold">
            <bdi dir="auto">{family.title}</bdi>
          </h4>
          {meta ? (
            <p className="mt-0.5 break-words text-[0.7rem] text-muted-foreground">
              <bdi dir="auto">{meta}</bdi>
            </p>
          ) : null}
        </div>
        <span className="text-[0.7rem] font-medium text-muted-foreground">
          {family.options.length} options
        </span>
      </div>
      <div className="divide-y">
        {family.options.map((option, index) => (
          <ClassOptionRow
            key={option.offeringKey}
            journey={journey}
            family={family}
            option={option}
            optionIndex={index}
            optionTotal={family.options.length}
            checked={Boolean(
              selected[
                selectionKey(journey.train.journeyKey, option.offeringKey)
              ],
            )}
            disabled={disabled}
            grouped
            onToggle={onToggle}
          />
        ))}
      </div>
    </div>
  );
}

function ResultsSummary({
  totalOptions,
  totalReservable,
  totalNotReservable,
  totalUnknown,
}: {
  totalOptions: number;
  totalReservable: number;
  totalNotReservable: number;
  totalUnknown: number;
}) {
  const items = [
    { count: totalReservable, label: "reservable", dot: "bg-success" },
    { count: totalUnknown, label: "unknown", dot: "bg-warning" },
    {
      count: totalNotReservable,
      label: "not reservable",
      dot: "bg-muted-foreground/40",
    },
  ].filter((item) => item.count > 0);
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
      <span>
        {totalOptions} class option{totalOptions === 1 ? "" : "s"}
      </span>
      {items.map((item) => (
        <span key={item.label} className="inline-flex items-center gap-1.5">
          <span
            className={cn("size-1.5 rounded-full", item.dot)}
            aria-hidden="true"
          />
          {item.count} {item.label}
        </span>
      ))}
    </div>
  );
}

export function MonitorSearchResults({
  groups,
  selected,
  disabled,
  onToggle,
}: MonitorSearchResultsProps) {
  const totalOptions = groups.reduce(
    (total, journey) => total + journey.optionCount,
    0,
  );
  const totalReservable = groups.reduce(
    (total, journey) => total + journey.reservableCount,
    0,
  );
  const totalNotReservable = groups.reduce(
    (total, journey) => total + journey.notReservableCount,
    0,
  );
  const totalUnknown = groups.reduce(
    (total, journey) => total + journey.unknownCount,
    0,
  );

  return (
    <section
      aria-labelledby="search-results-heading"
      aria-disabled={disabled || undefined}
      className="flex flex-col gap-4"
    >
      <div className="flex flex-col gap-2">
        <div className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
          <h2
            id="search-results-heading"
            tabIndex={-1}
            className="scroll-mt-24 text-lg font-semibold tracking-tight text-balance focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
          >
            Search results
          </h2>
          <ResultsSummary
            totalOptions={totalOptions}
            totalReservable={totalReservable}
            totalNotReservable={totalNotReservable}
            totalUnknown={totalUnknown}
          />
        </div>
        <p className="text-xs text-muted-foreground">
          Select any classes to monitor. Unavailable classes remain selectable
          so you can watch for future availability.
        </p>
      </div>

      <div className="flex flex-col gap-4">
        {groups.map((journey) => (
          <Card key={journey.key} className="gap-0 overflow-hidden py-0">
            <CardHeader className="border-b bg-muted/15 px-4 py-3.5 sm:px-5">
              <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between sm:gap-3">
                <div className="flex min-w-0 items-start gap-3">
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-background text-muted-foreground">
                    <TrainFront className="size-4" aria-hidden="true" />
                  </span>
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
                    <div className="mt-1 text-xs text-muted-foreground tabular-nums">
                      <JourneyTimes train={journey.train} />
                    </div>
                  </div>
                </div>
                <div className="flex flex-wrap items-center gap-2 sm:flex-col sm:items-end sm:gap-1.5">
                  <Badge variant="outline" className="font-normal">
                    {journey.optionCount} option
                    {journey.optionCount === 1 ? "" : "s"}
                  </Badge>
                  <StateCounts journey={journey} />
                </div>
              </div>
            </CardHeader>
            <CardContent className="px-0 py-0">
              {journey.families.length > 0 ? (
                <div className="divide-y">
                  {journey.families.map((family) => (
                    <ClassFamilyBlock
                      key={family.key}
                      journey={journey}
                      family={family}
                      selected={selected}
                      disabled={disabled}
                      onToggle={onToggle}
                    />
                  ))}
                </div>
              ) : (
                <p className="px-4 py-6 text-sm text-muted-foreground">
                  No class offerings were returned for this train.
                </p>
              )}
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  );
}
