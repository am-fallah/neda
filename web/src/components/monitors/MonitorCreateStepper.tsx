import { Check } from "lucide-react";
import { cn } from "@/lib/utils";

const STEPS = [
  { id: 1, label: "Search", hint: "Find trains" },
  { id: 2, label: "Select", hint: "Choose wagons" },
  { id: 3, label: "Confirm", hint: "Review & create" },
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
      className="grid w-full grid-cols-3 gap-2 rounded-lg border bg-card p-2 shadow-xs"
    >
      {STEPS.map((step) => {
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
              "relative flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5",
              status === "current" && "bg-accent/70",
            )}
          >
            <span
              aria-hidden="true"
              className={cn(
                "flex size-6 shrink-0 items-center justify-center rounded-full border font-mono text-[0.65rem] font-semibold transition-colors",
                status === "current" &&
                  "border-primary bg-primary text-primary-foreground",
                status === "complete" &&
                  "border-primary/25 bg-primary/10 text-primary",
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
            <span className="min-w-0">
              <span
                className={cn(
                  "block truncate text-[0.8rem] font-medium",
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
              <span className="hidden truncate text-[0.65rem] text-muted-foreground sm:block">
                {step.hint}
              </span>
            </span>
          </li>
        );
      })}
    </ol>
  );
}
