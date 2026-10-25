import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

interface LoadingStateProps {
  rows?: number;
  label?: string;
  className?: string;
}

export function LoadingState({
  rows = 3,
  label = "Loading",
  className,
}: LoadingStateProps) {
  return (
    <div
      className={cn("space-y-3", className)}
      role="status"
      aria-live="polite"
      aria-label={label}
    >
      <span className="sr-only">{label}…</span>
      {Array.from({ length: rows }, (_, index) => (
        <div
          key={index}
          className="flex items-center gap-4 rounded-[10px] border bg-card p-4 shadow-xs"
        >
          <Skeleton className="size-9 rounded-lg" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-3.5 w-2/5" />
            <Skeleton className="h-3 w-3/5" />
          </div>
          <Skeleton className="hidden h-6 w-16 rounded-md sm:block" />
        </div>
      ))}
    </div>
  );
}
