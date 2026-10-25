import { useId, type RefObject } from "react";
import { AlertTriangle } from "lucide-react";
import { cn } from "@/lib/utils";

export interface ErrorSummaryItem {
  fieldId: string;
  label: string;
  message: string;
}

export function ErrorSummary({
  title = "There is a problem",
  items,
  summaryRef,
  className,
}: {
  title?: string;
  items: ErrorSummaryItem[];
  summaryRef?: RefObject<HTMLDivElement | null>;
  className?: string;
}) {
  const titleId = useId();
  if (items.length === 0) return null;
  return (
    <div
      ref={summaryRef}
      tabIndex={-1}
      role="alert"
      aria-labelledby={titleId}
      className={cn(
        "scroll-mt-24 rounded-lg border border-destructive/35 bg-destructive/5 p-4 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none",
        className,
      )}
    >
      <h2
        id={titleId}
        className="flex items-center gap-2 text-sm font-semibold text-destructive"
      >
        <AlertTriangle className="size-4" aria-hidden="true" />
        {title}
      </h2>
      <ul className="mt-2 space-y-1 text-[0.8125rem]">
        {items.map((item) => (
          <li key={item.fieldId}>
            <a
              href={`#${item.fieldId}`}
              className="text-destructive underline underline-offset-4 hover:text-destructive/80 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
            >
              {item.label}: {item.message}
            </a>
          </li>
        ))}
      </ul>
    </div>
  );
}
