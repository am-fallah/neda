import { AlertTriangle, RotateCcw } from "lucide-react";
import { isApiError, type ApiError } from "@/api/client";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";

interface ErrorStateProps {
  error: ApiError | Error | null;
  title?: string;
  onRetry?: () => void;
  className?: string;
}

function messageFor(error: ApiError | Error | null): string {
  if (!error) return "The request could not be completed.";
  if (isApiError(error)) {
    switch (error.code) {
      case "unauthenticated":
        return "Your session has expired. Sign in again to continue.";
      case "csrf_failed":
      case "origin_not_allowed":
        return "The request was rejected for security reasons. Reload the page and try again.";
      case "setup_required":
        return "Password setup is required on the server before you can sign in.";
      case "telegram_not_configured":
        return "Telegram is not configured yet.";
      case "telegram_settings_unreadable":
        return "Stored Telegram settings cannot be read. Clear and re-enter them.";
      case "network_error":
        return error.message;
      default:
        return error.message;
    }
  }
  return "The request could not be completed.";
}

export function ErrorState({
  error,
  title = "Something went wrong",
  onRetry,
  className,
}: ErrorStateProps) {
  const requestId = isApiError(error) ? error.requestId : undefined;
  return (
    <Alert variant="destructive" className={className}>
      <AlertTriangle aria-hidden="true" />
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription>
        <p>{messageFor(error)}</p>
        {requestId ? (
          <p className="font-mono text-[0.7rem] break-all opacity-80">
            Request ID: {requestId}
          </p>
        ) : null}
        {onRetry ? (
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="mt-2 border-destructive/30 text-destructive hover:bg-destructive/5 hover:text-destructive"
            onClick={onRetry}
          >
            <RotateCcw aria-hidden="true" />
            Try again
          </Button>
        ) : null}
      </AlertDescription>
    </Alert>
  );
}
