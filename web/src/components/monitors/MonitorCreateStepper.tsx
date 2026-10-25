import { Check } from "lucide-react";
import { cn } from "@/lib/utils";

const STEPS = [
  { id: 1, label: "Search" },
  { id: 2, label: "Select" },
  { id: 3, label: "Confirm" },
] as const;

export type MonitorCreateStep = (typeof STEPS)[number]["id"];

export function MonitorCreateStepper({
  current,
}: {
  current: MonitorCreateStep;
}) {
  return (
    <ol
      aria-label="Monitor creation progress"
      className="flex w-full items-center gap-2 sm:gap-2.5"
    >
      {STEPS.map((step, index) => {
        const status =
          step.id < current
            ? "complete"
            : step.id === current
              ? "current"
              : "upcoming";
        return (
          <li
            key={step.id}
            aria-current={status === "current" ? "step" : undefined}
            className={cn(
              "flex min-w-0 items-center gap-2",
              index < STEPS.length - 1 ? "flex-1" : "shrink-0",
            )}
          >
            <span
              aria-hidden="true"
              className={cn(
                "flex size-5 shrink-0 items-center justify-center rounded-full border text-[0.65rem] font-semibold transition-colors",
                status === "current" &&
                  "border-primary bg-primary text-primary-foreground",
                status === "complete" &&
                  "border-primary/20 bg-primary/5 text-primary",
                status === "upcoming" &&
                  "border-border bg-background text-muted-foreground",
              )}
            >
              {status === "complete" ? (
                <Check className="size-3.5" strokeWidth={2.5} />
              ) : (
                step.id
              )}
            </span>
            <span
              className={cn(
                "truncate text-[0.8rem] font-medium",
                status === "current" && "text-foreground",
                status === "complete" && "text-muted-foreground",
                status === "upcoming" && "text-muted-foreground",
              )}
            >
              <span className="sr-only">
                {status === "complete"
                  ? "Completed: "
                  : status === "current"
                    ? "Current step: "
                    : "Upcoming: "}
              </span>
              {step.label}
            </span>
            {index < STEPS.length - 1 ? (
              <span
                aria-hidden="true"
                className="h-px min-w-2 flex-1 bg-border"
              />
            ) : null}
          </li>
        );
      })}
    </ol>
  );
}
