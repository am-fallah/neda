import { useEffect, useMemo, useState } from "react";
import { CalendarDays, ChevronLeft, ChevronRight } from "lucide-react";
import { jalaaliToDateObject } from "jalaali-js";
import { Button } from "@/components/ui/button";
import { NativeSelect } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  formatJalaliDate,
  gregorianToJalali,
  isIsoDate,
  jalaliMonthDays,
  jalaliMonthName,
  jalaliToGregorian,
  todayInTehran,
  type JalaliDate,
} from "@/lib/dates";
import { cn } from "@/lib/utils";

interface JalaliDatePickerProps {
  id: string;
  value: string;
  onChange: (value: string) => void;
  min?: string;
  disabled?: boolean;
  invalid?: boolean;
  describedBy?: string;
  className?: string;
}

const WEEKDAYS = ["Sat", "Sun", "Mon", "Tue", "Wed", "Thu", "Fri"];

function viewFor(value: string, fallback: JalaliDate): JalaliDate {
  return (
    (value && isIsoDate(value) ? gregorianToJalali(value) : null) ?? fallback
  );
}

export function JalaliDatePicker({
  id,
  value,
  onChange,
  min,
  disabled,
  invalid,
  describedBy,
  className,
}: JalaliDatePickerProps) {
  const todayIso = todayInTehran();
  const minIso = min ?? todayIso;
  const todayJalali = useMemo(
    () => gregorianToJalali(todayIso) ?? { jy: 1400, jm: 1, jd: 1 },
    [todayIso],
  );
  const [open, setOpen] = useState(false);
  const [view, setView] = useState<JalaliDate>(() =>
    viewFor(value, todayJalali),
  );

  useEffect(() => {
    if (value && isIsoDate(value)) {
      setView(viewFor(value, todayJalali));
    }
  }, [value, todayJalali]);

  const selected = value && isIsoDate(value) ? gregorianToJalali(value) : null;
  const days = jalaliMonthDays(view.jy, view.jm);
  const firstWeekday = jalaaliToDateObject(view.jy, view.jm, 1).getDay();
  const leading = (firstWeekday + 1) % 7;

  const yearOptions = useMemo(() => {
    const start = Math.min(todayJalali.jy - 1, view.jy, selected?.jy ?? view.jy);
    const end = Math.max(todayJalali.jy + 10, view.jy, selected?.jy ?? view.jy);
    const years: number[] = [];
    for (let year = start; year <= end; year += 1) years.push(year);
    return years;
  }, [todayJalali.jy, view.jy, selected?.jy]);

  function shiftMonth(delta: number) {
    setView((current) => {
      const nextMonth = current.jm + delta;
      if (nextMonth < 1) return { ...current, jy: current.jy - 1, jm: 12 };
      if (nextMonth > 12) return { ...current, jy: current.jy + 1, jm: 1 };
      return { ...current, jm: nextMonth };
    });
  }

  function selectDay(day: number) {
    const iso = jalaliToGregorian(view.jy, view.jm, day);
    if (!iso || iso < minIso) return;
    onChange(iso);
    setOpen(false);
  }

  function goToday() {
    setView(todayJalali);
    onChange(todayIso);
    setOpen(false);
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          id={id}
          type="button"
          disabled={disabled}
          aria-invalid={invalid || undefined}
          aria-describedby={describedBy}
          className={cn(
            "flex h-10 w-full items-center justify-between gap-2 rounded-md border border-input bg-background px-3 text-left text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-50",
            invalid && "border-destructive",
            className,
          )}
        >
          <span className="flex min-w-0 items-center gap-2">
            <CalendarDays
              className="size-4 shrink-0 text-muted-foreground"
              aria-hidden="true"
            />
            {value && isIsoDate(value) ? (
              <span className="flex min-w-0 flex-col">
                <span className="truncate font-medium">
                  {formatJalaliDate(value)}
                </span>
                <span className="text-xs text-muted-foreground">
                  Gregorian {value}
                </span>
              </span>
            ) : (
              <span className="text-muted-foreground">
                Select a departure date
              </span>
            )}
          </span>
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-[19rem]">
        <div className="space-y-3">
          <div className="flex items-center justify-between gap-2">
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="Previous month"
              onClick={() => shiftMonth(-1)}
            >
              <ChevronLeft aria-hidden="true" />
            </Button>
            <div className="flex min-w-0 flex-1 items-center justify-center gap-2">
              <NativeSelect
                aria-label="Jalali month"
                value={view.jm}
                onChange={(event) =>
                  setView((current) => ({
                    ...current,
                    jm: Number(event.target.value),
                  }))
                }
                className="h-8 w-auto text-xs"
              >
                {Array.from({ length: 12 }, (_, index) => index + 1).map(
                  (month) => (
                    <option key={month} value={month}>
                      {jalaliMonthName(month)}
                    </option>
                  ),
                )}
              </NativeSelect>
              <NativeSelect
                aria-label="Jalali year"
                value={view.jy}
                onChange={(event) =>
                  setView((current) => ({
                    ...current,
                    jy: Number(event.target.value),
                  }))
                }
                className="h-8 w-auto text-xs"
              >
                {yearOptions.map((year) => (
                  <option key={year} value={year}>
                    {year}
                  </option>
                ))}
              </NativeSelect>
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              aria-label="Next month"
              onClick={() => shiftMonth(1)}
            >
              <ChevronRight aria-hidden="true" />
            </Button>
          </div>

          <div
            className="grid grid-cols-7 gap-1 text-center"
            role="group"
            aria-label={`${jalaliMonthName(view.jm)} ${view.jy}`}
          >
            {WEEKDAYS.map((weekday) => (
              <span
                key={weekday}
                className="py-1 text-[0.65rem] font-medium tracking-wide text-muted-foreground uppercase"
                aria-hidden="true"
              >
                {weekday}
              </span>
            ))}
            {Array.from({ length: leading }, (_, index) => (
              <span key={`pad-${index}`} aria-hidden="true" />
            ))}
            {Array.from({ length: days }, (_, index) => {
              const day = index + 1;
              const iso = jalaliToGregorian(view.jy, view.jm, day);
              const isSelected =
                selected?.jy === view.jy &&
                selected?.jm === view.jm &&
                selected?.jd === day;
              const isToday = iso === todayIso;
              const isDisabled = !iso || iso < minIso;
              return (
                <button
                  key={day}
                  type="button"
                  disabled={isDisabled}
                  aria-pressed={isSelected}
                  aria-label={`${day} ${jalaliMonthName(view.jm)} ${view.jy} (${iso})`}
                  onClick={() => selectDay(day)}
                  className={cn(
                    "flex size-9 items-center justify-center rounded-md text-sm transition-colors hover:bg-accent hover:text-accent-foreground disabled:pointer-events-none disabled:opacity-30",
                    isSelected &&
                      "bg-primary text-primary-foreground hover:bg-primary/90 hover:text-primary-foreground",
                    isToday && !isSelected && "border border-primary/50",
                  )}
                >
                  {day}
                </button>
              );
            })}
          </div>

          <div className="flex items-center justify-between border-t pt-3">
            <span className="text-xs text-muted-foreground">
              Today: {formatJalaliDate(todayIso)}
            </span>
            <Button
              type="button"
              size="sm"
              variant="outline"
              onClick={goToday}
              disabled={todayIso < minIso}
            >
              Today
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}

export function DateDisplay({
  value,
  className,
}: {
  value: string;
  className?: string;
}) {
  if (!isIsoDate(value)) {
    return <span className={className}>{value || "—"}</span>;
  }
  return (
    <span className={className} title={`Gregorian ${value}`}>
      <time dateTime={value}>{formatJalaliDate(value)}</time>
      <span className="ml-1 text-xs text-muted-foreground">({value})</span>
    </span>
  );
}
