import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

interface PageHeaderProps {
  title: string;
  description?: ReactNode;
  actions?: ReactNode;
  eyebrow?: string;
  className?: string;
  titleClassName?: string;
}

export function PageHeader({
  title,
  description,
  actions,
  eyebrow,
  className,
  titleClassName,
}: PageHeaderProps) {
  return (
    <header
      className={cn(
        "flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between",
        className,
      )}
    >
      <div className="min-w-0 flex-1">
        {eyebrow ? (
          <p className="eyebrow mb-1.5 flex items-center gap-2">
            <span
              className="brand-rule h-px w-6"
              aria-hidden="true"
            />
            {eyebrow}
          </p>
        ) : null}
        <h1
          className={cn(
            "text-[1.75rem] leading-[1.15] font-semibold tracking-[-0.025em] text-balance sm:text-[2rem]",
            titleClassName,
          )}
        >
          {title}
        </h1>
        {description ? (
          <div className="mt-2 max-w-[64ch] text-sm leading-relaxed text-muted-foreground">
            {description}
          </div>
        ) : null}
      </div>
      {actions ? (
        <div className="flex flex-wrap items-center gap-2 max-sm:w-full max-sm:flex-col max-sm:items-stretch sm:shrink-0 [&>*]:max-sm:w-full">
          {actions}
        </div>
      ) : null}
    </header>
  );
}
