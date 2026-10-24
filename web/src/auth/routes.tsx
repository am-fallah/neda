import { Navigate, Outlet, useLocation } from "react-router-dom";
import { useAuth } from "@/auth/AuthProvider";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { Button } from "@/components/ui/button";

function FullPageStatus({
  title,
  children,
}: {
  title: string;
  children?: React.ReactNode;
}) {
  return (
    <div className="flex min-h-dvh items-center justify-center px-4">
      <div className="w-full max-w-md space-y-4 text-center">
        <h1 className="text-lg font-semibold">{title}</h1>
        {children}
      </div>
    </div>
  );
}

export function ProtectedRoute() {
  const { status, error, refresh } = useAuth();
  const location = useLocation();

  if (status === "loading") {
    return (
      <FullPageStatus title="Checking your session">
        <LoadingState rows={1} label="Checking your session" />
      </FullPageStatus>
    );
  }

  if (status === "error") {
    return (
      <FullPageStatus title="Neda is unavailable">
        <ErrorState
          error={error}
          title="Could not verify your session"
          onRetry={() => void refresh()}
        />
      </FullPageStatus>
    );
  }

  if (status === "anonymous") {
    return (
      <Navigate
        to="/login"
        replace
        state={{ from: `${location.pathname}${location.search}` }}
      />
    );
  }

  return <Outlet />;
}

export function LoginRoute() {
  const { status } = useAuth();
  const location = useLocation();
  const from =
    (location.state as { from?: string } | null)?.from &&
    (location.state as { from?: string }).from !== "/login"
      ? (location.state as { from: string }).from
      : "/";

  if (status === "authenticated") {
    return <Navigate to={from} replace />;
  }
  return <Outlet />;
}

export function SessionErrorBanner({ onRetry }: { onRetry: () => void }) {
  return (
    <div className="rounded-lg border border-destructive/40 bg-destructive/5 p-4">
      <p className="text-sm font-medium text-destructive">
        Your session could not be verified.
      </p>
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="mt-2"
        onClick={onRetry}
      >
        Try again
      </Button>
    </div>
  );
}
