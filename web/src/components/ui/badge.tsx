import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const badgeVariants = cva(
  "inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-md border px-2 py-0.5 text-[0.6875rem] leading-5 font-medium whitespace-nowrap [&>svg]:size-3.5 [&>svg]:pointer-events-none",
  {
    variants: {
      variant: {
        default: "border-transparent bg-primary text-primary-foreground",
        secondary:
          "border-transparent bg-secondary text-secondary-foreground",
        outline: "border-input/70 bg-card text-foreground",
        success:
          "border-success/30 bg-success/10 text-success dark:border-success/40 dark:bg-success/15 dark:text-success",
        warning:
          "border-warning/40 bg-warning/10 text-warning-foreground dark:border-warning/45 dark:bg-warning/15 dark:text-warning",
        destructive:
          "border-destructive/30 bg-destructive/10 text-destructive dark:border-destructive/40 dark:bg-destructive/15 dark:text-destructive",
        info: "border-info/30 bg-info/10 text-info dark:border-info/40 dark:bg-info/15 dark:text-info",
        muted: "border-transparent bg-muted text-muted-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  },
);

function Badge({
  className,
  variant,
  ...props
}: React.ComponentProps<"span"> & VariantProps<typeof badgeVariants>) {
  return (
    <span
      data-slot="badge"
      className={cn(badgeVariants({ variant }), className)}
      {...props}
    />
  );
}

export { Badge, badgeVariants };
