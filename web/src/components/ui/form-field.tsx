import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import { Label } from "@/components/ui/input";

interface FormFieldProps {
  id: string;
  label: string;
  description?: ReactNode;
  error?: string;
  required?: boolean;
  className?: string;
  children: ReactNode;
}

export function FormField({
  id,
  label,
  description,
  error,
  required,
  className,
  children,
}: FormFieldProps) {
  const descriptionId = description ? `${id}-description` : undefined;
  const errorId = error ? `${id}-error` : undefined;
  return (
    <div className={cn("space-y-2", className)}>
      <Label htmlFor={id}>
        {label}
        {required ? (
          <>
            <span className="text-destructive" aria-hidden="true">
              *
            </span>
            <span className="sr-only"> (required)</span>
          </>
        ) : null}
      </Label>
      {children}
      {description ? (
        <p
          id={descriptionId}
          className="text-xs leading-relaxed text-muted-foreground"
        >
          {description}
        </p>
      ) : null}
      {error ? (
        <p
          id={errorId}
          role="alert"
          className="text-xs leading-relaxed font-medium text-destructive"
        >
          {error}
        </p>
      ) : null}
    </div>
  );
}

export function fieldProps(
  id: string,
  error?: string,
  description?: ReactNode,
) {
  const describedBy = [
    description ? `${id}-description` : null,
    error ? `${id}-error` : null,
  ]
    .filter(Boolean)
    .join(" ");
  return {
    id,
    "aria-invalid": error ? true : undefined,
    "aria-describedby": describedBy || undefined,
  } as const;
}
