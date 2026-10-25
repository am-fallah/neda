import { Skeleton } from "@/components/ui/skeleton";

interface LoadingStateProps {
  rows?: number;
  label?: string;
}

export function LoadingState({
  rows = 3,
  label = "Loading",
}: LoadingStateProps) {
  return (
    <div
      className="space-y-3"
      role="status"
      aria-live="polite"
      aria-label={label}
    >
      <span className="sr-only">{label}…</span>
      {Array.from({ length: rows }, (_, index) => (
        <div
          key={index}
          className="flex items-center gap-4 rounded-xl border bg-card p-5"
        >
          <Skeleton className="size-10 rounded-full" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-4 w-2/5" />
            <Skeleton className="h-3 w-3/5" />
          </div>
        </div>
      ))}
    </div>
  );
}
