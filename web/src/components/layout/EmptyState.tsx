import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

interface EmptyStateProps {
  icon?: LucideIcon;
  title: string;
  description?: ReactNode;
  action?: ReactNode;
  framed?: boolean;
  className?: string;
}

export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  framed = true,
  className,
}: EmptyStateProps) {
  return (
    <div
      className={cn(
        "flex flex-col items-start justify-center gap-3 px-1 py-6 text-left sm:px-2",
        framed &&
          "rounded-lg border border-dashed bg-surface-raised px-5 py-8 sm:px-6",
        className,
      )}
    >
      {Icon ? (
        <span className="flex size-9 items-center justify-center rounded-lg border bg-card text-muted-foreground shadow-xs">
          <Icon className="size-4" aria-hidden="true" />
        </span>
      ) : null}
      <div className="space-y-1">
        <p className="text-sm font-semibold">{title}</p>
        {description ? (
          <div className="max-w-xl text-[0.8125rem] leading-relaxed text-muted-foreground">
            {description}
          </div>
        ) : null}
      </div>
      {action ? <div className="pt-1">{action}</div> : null}
    </div>
  );
}
