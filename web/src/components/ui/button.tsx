import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "inline-flex shrink-0 cursor-pointer items-center justify-center gap-2 whitespace-nowrap rounded-lg text-sm font-medium transition-[background-color,color,border-color,box-shadow,opacity] duration-150 ease-out focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/35 focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-45 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0 motion-reduce:transition-none",
  {
    variants: {
      variant: {
        default:
          "bg-primary text-primary-foreground shadow-xs hover:bg-primary/90 active:bg-primary/80",
        destructive:
          "bg-destructive text-destructive-foreground shadow-xs hover:bg-destructive/90 active:bg-destructive/80",
        outline:
          "border border-input bg-card text-foreground hover:border-primary/45 hover:bg-accent hover:text-accent-foreground active:bg-secondary",
        secondary:
          "bg-secondary text-secondary-foreground hover:bg-secondary/75 active:bg-secondary/60",
        ghost:
          "text-foreground hover:bg-accent hover:text-accent-foreground active:bg-secondary",
        link: "text-primary underline-offset-4 hover:underline hover:text-primary/85",
      },
      size: {
        default: "h-10 px-4 py-2 max-sm:h-11",
        sm: "h-9 rounded-lg px-3 text-[0.8125rem] max-sm:h-11",
        lg: "h-11 rounded-lg px-5 text-sm",
        icon: "size-10 max-sm:size-11",
        "icon-sm": "size-9 max-sm:size-11",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  },
);

export interface ButtonProps
  extends React.ComponentProps<"button">,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

function Button({
  className,
  variant,
  size,
  asChild = false,
  ...props
}: ButtonProps) {
  const Comp = asChild ? Slot : "button";
  return (
    <Comp
      data-slot="button"
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  );
}

export { Button, buttonVariants };
