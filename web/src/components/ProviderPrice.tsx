import { cn } from "@/lib/utils";

export interface ProviderPriceProps {
  amount?: number | null;
  currency?: string;
  emptyLabel?: string;
  showUnverifiedMarker?: boolean;
  className?: string;
}

export function providerPriceText(
  amount: number | null | undefined,
  currency?: string,
): string | null {
  if (typeof amount !== "number" || !Number.isFinite(amount) || amount <= 0) {
    return null;
  }
  const value = amount.toLocaleString("en-US");
  const unit = currency?.trim();
  return unit ? `${value} ${unit}` : value;
}

/**
 * Displays a provider-reported amount without inventing a currency. A nonzero
 * amount with no verified unit is marked when requested, and always carries an
 * accessible qualifier.
 */
export function ProviderPrice({
  amount,
  currency,
  emptyLabel = "Price unavailable",
  showUnverifiedMarker = false,
  className,
}: ProviderPriceProps) {
  const text = providerPriceText(amount, currency);
  if (text === null) {
    return (
      <span className={cn("text-xs text-muted-foreground", className)}>
        {emptyLabel}
      </span>
    );
  }
  const unverifiedUnit = !currency?.trim();
  return (
    <span
      className={cn("font-mono tabular-nums", className)}
      title={
        unverifiedUnit
          ? "Provider-reported amount; currency and per-passenger basis are not verified."
          : undefined
      }
    >
      {text}
      {unverifiedUnit && showUnverifiedMarker ? (
        <span aria-hidden="true">*</span>
      ) : null}
      {unverifiedUnit ? (
        <span className="sr-only">
          {" "}
          provider-reported amount; currency and per-passenger basis are not
          verified
        </span>
      ) : null}
    </span>
  );
}
