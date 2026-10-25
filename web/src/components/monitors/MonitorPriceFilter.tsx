import { useState } from "react";
import { Filter } from "lucide-react";
import { Button } from "@/components/ui/button";
import { FormField } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

interface MonitorPriceFilterProps {
  min: string;
  max: string;
  includeUnpriced: boolean;
  error?: string;
  disabled: boolean;
  hiddenUnpriced: number;
  hiddenPricedOutOfRange: number;
  knownMin: number | null;
  knownMax: number | null;
  onMinChange: (value: string) => void;
  onMaxChange: (value: string) => void;
  onIncludeUnpricedChange: (value: boolean) => void;
  onClear: () => void;
}

function formatAmount(value: number): string {
  return value.toLocaleString("en-US");
}

export function MonitorPriceFilter({
  min,
  max,
  includeUnpriced,
  error,
  disabled,
  hiddenUnpriced,
  hiddenPricedOutOfRange,
  knownMin,
  knownMax,
  onMinChange,
  onMaxChange,
  onIncludeUnpricedChange,
  onClear,
}: MonitorPriceFilterProps) {
  const [open, setOpen] = useState(false);
  const hasValues = min.trim() !== "" || max.trim() !== "";
  const active = !error && hasValues;
  const hiddenTotal = hiddenUnpriced + hiddenPricedOutOfRange;

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant={active ? "secondary" : "outline"}
          size="sm"
          className="h-9 max-sm:h-11"
          disabled={disabled}
          aria-label={active ? "Edit price filter" : "Filter by price"}
        >
          <Filter data-icon="inline-start" aria-hidden="true" />
          Price
          {active ? (
            <>
              <span
                className="size-1.5 rounded-full bg-primary"
                aria-hidden="true"
              />
              <span className="sr-only">price filter active</span>
            </>
          ) : null}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[min(20rem,calc(100vw-2rem))] p-4">
        <div className="flex flex-col gap-3">
          <div>
            <h3 className="text-sm font-semibold">Price range</h3>
            <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
              Provider-reported amount. Currency and per-passenger basis are
              not verified. Zero and missing values are never treated as
              prices.
            </p>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <FormField id="price-filter-min" label="Minimum">
              <Input
                id="price-filter-min"
                type="text"
                inputMode="numeric"
                autoComplete="off"
                placeholder={
                  knownMin !== null ? formatAmount(knownMin) : "Min amount"
                }
                className="h-10 font-mono tabular-nums max-sm:h-11"
                value={min}
                onChange={(event) => onMinChange(event.target.value)}
                aria-invalid={error ? true : undefined}
                aria-describedby={error ? "price-filter-error" : undefined}
              />
            </FormField>
            <FormField id="price-filter-max" label="Maximum">
              <Input
                id="price-filter-max"
                type="text"
                inputMode="numeric"
                autoComplete="off"
                placeholder={
                  knownMax !== null ? formatAmount(knownMax) : "Max amount"
                }
                className="h-10 font-mono tabular-nums max-sm:h-11"
                value={max}
                onChange={(event) => onMaxChange(event.target.value)}
                aria-invalid={error ? true : undefined}
                aria-describedby={error ? "price-filter-error" : undefined}
              />
            </FormField>
          </div>

          {error ? (
            <p
              id="price-filter-error"
              role="alert"
              className="text-xs font-medium text-destructive"
            >
              {error}
            </p>
          ) : null}

          <label className="flex min-h-11 cursor-pointer items-start gap-2 rounded-md border border-border/70 px-3 py-2.5 text-xs sm:min-h-0">
            <input
              type="checkbox"
              className="mt-0.5 size-4 shrink-0 accent-primary disabled:cursor-not-allowed disabled:opacity-50"
              checked={includeUnpriced}
              disabled={!active}
              onChange={(event) =>
                onIncludeUnpricedChange(event.target.checked)
              }
            />
            <span>
              {hiddenUnpriced > 0
                ? `Include ${hiddenUnpriced} unpriced fare option${
                    hiddenUnpriced === 1 ? "" : "s"
                  }`
                : "Include fare options with no price"}
            </span>
          </label>

          <div className="flex items-center justify-between gap-3 border-t pt-3">
            <p className="text-[0.7rem] leading-relaxed text-muted-foreground">
              {active
                ? hiddenTotal > 0
                  ? `${hiddenTotal} fare option${hiddenTotal === 1 ? "" : "s"} hidden`
                  : "All fare options are in range"
                : "No price filter applied"}
            </p>
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-9 max-sm:h-11"
                onClick={onClear}
                disabled={!hasValues && !includeUnpriced}
              >
                Clear
              </Button>
              <Button
                type="button"
                size="sm"
                className="h-9 max-sm:h-11"
                onClick={() => setOpen(false)}
              >
                Done
              </Button>
            </div>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
