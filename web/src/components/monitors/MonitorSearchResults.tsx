import { useId } from "react";
import { Clock3, TrainFront } from "lucide-react";
import type {
  ProviderSearchClass,
  ProviderSearchTrain,
  SelectedOption,
} from "@/api/types";
import { ObservationStateBadge } from "@/components/status";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardHeader,
} from "@/components/ui/card";
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
      <span>
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
        {formatTehranTimestamp(train.departureAt, "Departure time unavailable")} to{" "}
        {formatTehranTimestamp(train.arrivalAt, "arrival time unavailable")}
      </span>
    </span>
  );
}

function StateCounts({ journey }: { journey: SearchJourneyGroup }) {
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      {journey.reservableCount > 0 ? (
        <Badge variant="success">
          {journey.reservableCount} reservable
        </Badge>
      ) : null}
      {journey.unknownCount > 0 ? (
        <Badge variant="warning">{journey.unknownCount} unknown</Badge>
      ) : null}
      {journey.notReservableCount > 0 ? (
        <Badge variant="secondary">
          {journey.notReservableCount} not reservable
        </Badge>
      ) : null}
    </div>
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
        "group flex min-w-0 cursor-pointer items-start gap-3 p-3 transition-colors sm:gap-4 sm:p-4",
        grouped ? "rounded-none" : "rounded-lg border",
        checked ? "bg-muted/60" : "hover:bg-muted/30",
        disabled && "cursor-not-allowed opacity-60 hover:bg-transparent",
        "focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2 focus-within:ring-offset-background",
      )}
    >
      <input
        type="checkbox"
        name="mrbilit-selection"
        value={option.offeringKey}
        checked={checked}
        disabled={disabled}
        onChange={() => onToggle(journey.train, option)}
        aria-describedby={disabled ? "search-stale-notice" : undefined}
        className="mt-0.5 size-4 shrink-0 cursor-pointer rounded border-input accent-[var(--primary)] disabled:cursor-not-allowed"
      />
      <span className="min-w-0 flex-1">
        <span className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1.5">
          <span className="flex min-w-0 flex-wrap items-center gap-2">
            <span className="min-w-0 break-words text-sm font-medium">
              <bdi dir="auto">{ordinal ?? family.title}</bdi>
            </span>
            <ObservationStateBadge state={option.state} />
          </span>
          <span
            className="shrink-0 text-xs font-medium text-muted-foreground tabular-nums"
            title="Provider-reported capacity"
          >
            {capacityLabel(option.capacity)}
          </span>
        </span>
        {meta ? (
          <span className="mt-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
            <bdi dir="auto">{meta}</bdi>
          </span>
        ) : null}
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
    <div
      role="group"
      aria-labelledby={headingId}
      className="overflow-hidden rounded-lg border bg-background/40"
    >
      <div className="flex flex-wrap items-start justify-between gap-2 border-b bg-muted/25 px-3 py-2.5 sm:px-4">
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
      <div className="flex flex-col gap-1.5">
        <h2
          id="search-results-heading"
          tabIndex={-1}
          className="scroll-mt-24 text-lg font-semibold tracking-tight text-balance focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
        >
          Available journeys
        </h2>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted-foreground">
          <span>
            {groups.length} train{groups.length === 1 ? "" : "s"} ·{" "}
            {totalOptions} class option{totalOptions === 1 ? "" : "s"}
          </span>
          {totalReservable > 0 ? (
            <span className="text-success">
              · {totalReservable} reservable
            </span>
          ) : null}
          {totalUnknown > 0 ? (
            <span className="text-warning-foreground dark:text-warning">
              · {totalUnknown} unknown
            </span>
          ) : null}
          {totalNotReservable > 0 ? (
            <span>· {totalNotReservable} not reservable</span>
          ) : null}
        </div>
        <p className="text-xs text-muted-foreground">
          Select any classes to monitor. Unavailable classes remain selectable
          so you can watch for future availability.
        </p>
      </div>

      <div className="flex flex-col gap-4">
        {groups.map((journey) => (
          <Card
            key={journey.key}
            className="gap-0 overflow-hidden py-0"
          >
            <CardHeader className="border-b px-4 py-4 sm:px-5">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="flex min-w-0 items-start gap-3">
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted/40 text-muted-foreground">
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
                <div className="flex max-w-full flex-wrap items-center justify-end gap-1.5">
                  <Badge variant="outline">
                    {journey.optionCount} option
                    {journey.optionCount === 1 ? "" : "s"}
                  </Badge>
                  <StateCounts journey={journey} />
                </div>
              </div>
            </CardHeader>
            <CardContent className="px-3 py-3 sm:px-4">
              {journey.families.length > 0 ? (
                <div className="flex flex-col gap-2">
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
                <p className="rounded-lg border border-dashed px-3 py-4 text-sm text-muted-foreground">
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
