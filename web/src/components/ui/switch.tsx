import * as React from "react";
import { cn } from "@/lib/utils";

interface SwitchProps
  extends Omit<React.ComponentProps<"button">, "onChange" | "value"> {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}

/**
 * Small accessible switch built on a native button. Using role="switch" and
 * aria-checked gives assistive technology the same state as the visual track.
 */
function Switch({
  checked,
  onCheckedChange,
  className,
  disabled,
  ...props
}: SwitchProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      data-slot="switch"
      data-state={checked ? "checked" : "unchecked"}
      disabled={disabled}
      onClick={() => onCheckedChange(!checked)}
      className={cn(
        "inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full border border-transparent p-0.5 transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-50",
        checked ? "bg-primary" : "bg-muted-foreground/35",
        className,
      )}
      {...props}
    >
      <span
        aria-hidden="true"
        className={cn(
          "pointer-events-none block size-5 rounded-full bg-background shadow-xs transition-transform",
          checked ? "translate-x-5" : "translate-x-0",
        )}
      />
    </button>
  );
}

export { Switch };
