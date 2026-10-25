import { useEffect, useRef, useState, type FormEvent } from "react";
import { AlertTriangle, KeyRound, Terminal } from "lucide-react";
import { useLocation, useNavigate } from "react-router-dom";
import { isApiError } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { ErrorState } from "@/components/layout/ErrorState";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Input, Label } from "@/components/ui/input";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";
import { cn } from "@/lib/utils";

export function LoginPage() {
  useDocumentTitle("Sign in");
  const { status, setupRequired, error: sessionError, login, refresh } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const from =
    (location.state as { from?: string } | null)?.from &&
    (location.state as { from?: string }).from !== "/login"
      ? (location.state as { from: string }).from
      : "/";

  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [formError, setFormError] = useState("");
  const [fieldError, setFieldError] = useState("");
  const [setupFromServer, setSetupFromServer] = useState(false);
  const [retrySeconds, setRetrySeconds] = useState(0);
  const passwordRef = useRef<HTMLInputElement>(null);

  const showSetup = setupRequired || setupFromServer;

  useEffect(() => {
    if (!retrySeconds) return;
    const timer = window.setInterval(() => {
      setRetrySeconds((current) => Math.max(0, current - 1));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [retrySeconds]);

  useEffect(() => {
    if (!showSetup) passwordRef.current?.focus();
  }, [showSetup]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!password.trim()) {
      setFieldError("Enter your password.");
      passwordRef.current?.focus();
      return;
    }
    setSubmitting(true);
    setFieldError("");
    setFormError("");
    try {
      await login(password);
      setPassword("");
      navigate(from, { replace: true });
    } catch (caught) {
      if (isApiError(caught)) {
        switch (caught.code) {
          case "invalid_credentials":
            setFieldError("Incorrect password.");
            break;
          case "rate_limited": {
            const seconds = caught.retryAfterSeconds ?? 60;
            setRetrySeconds(seconds);
            setFormError(
              `Too many sign-in attempts. Try again in ${seconds} seconds.`,
            );
            break;
          }
          case "setup_required":
            setSetupFromServer(true);
            break;
          case "origin_not_allowed":
            setFormError(
              "The server rejected this page's origin. Open Neda at its configured address.",
            );
            break;
          case "password_changed":
            setFormError("Your password changed during sign-in. Try again.");
            break;
          default:
            setFormError(caught.message);
        }
      } else {
        setFormError("Sign-in could not be completed. Try again.");
      }
    } finally {
      setSubmitting(false);
    }
  }

  if (status === "error") {
    return (
      <AuthShell>
        <ErrorState
          error={sessionError}
          title="Neda is unavailable"
          onRetry={() => void refresh()}
        />
      </AuthShell>
    );
  }

  return (
    <AuthShell>
      <Card className="w-full max-w-md gap-6 py-7">
        <CardHeader className="px-7">
          <div className="mb-3 flex items-center gap-2.5">
            <span
              aria-hidden="true"
              className="flex size-9 items-center justify-center rounded-lg bg-primary text-sm font-bold text-primary-foreground"
            >
              N
            </span>
            <div>
              <p className="text-sm font-semibold">Neda</p>
              <p className="text-xs text-muted-foreground">
                Train availability monitor
              </p>
            </div>
          </div>
          <CardTitle className="text-xl">
            {showSetup ? "Set up your password" : "Sign in"}
          </CardTitle>
          <CardDescription>
            {showSetup
              ? "Neda has no default password. Create one from the server terminal before signing in."
              : "Private, single-user dashboard. Enter the owner password to continue."}
          </CardDescription>
        </CardHeader>
        <CardContent className="px-7">
          {showSetup ? (
            <div className="space-y-4">
              <Alert variant="warning">
                <Terminal aria-hidden="true" />
                <AlertTitle>Initial password setup required</AlertTitle>
                <AlertDescription>
                  Run this command on the machine hosting Neda. It asks for the
                  password twice without echo and never accepts it as an
                  argument.
                  <code className="mt-2 block overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs text-foreground">
                    neda set-password
                  </code>
                </AlertDescription>
              </Alert>
              <Button
                type="button"
                variant="outline"
                className="w-full"
                onClick={() => {
                  setSetupFromServer(false);
                  void refresh();
                }}
              >
                I have set the password
              </Button>
              <p className="text-xs text-muted-foreground">
                After setup, reload this page and sign in. No credentials are
                exposed through the web interface.
              </p>
            </div>
          ) : (
            <form onSubmit={handleSubmit} noValidate className="space-y-4">
              {formError ? (
                <Alert variant="destructive" role="alert">
                  <AlertTriangle aria-hidden="true" />
                  <AlertTitle>Sign-in failed</AlertTitle>
                  <AlertDescription>{formError}</AlertDescription>
                </Alert>
              ) : null}
              <div className="space-y-1.5">
                <Label htmlFor="password">Password</Label>
                <Input
                  ref={passwordRef}
                  id="password"
                  name="password"
                  type="password"
                  autoComplete="current-password"
                  autoCapitalize="none"
                  autoCorrect="off"
                  spellCheck={false}
                  value={password}
                  onChange={(event) => {
                    setPassword(event.target.value);
                    setFieldError("");
                  }}
                  aria-invalid={fieldError ? true : undefined}
                  aria-describedby={fieldError ? "password-error" : undefined}
                  disabled={submitting || retrySeconds > 0}
                  className={cn("h-10", fieldError && "border-destructive")}
                />
                {fieldError ? (
                  <p
                    id="password-error"
                    className="text-xs font-medium text-destructive"
                  >
                    {fieldError}
                  </p>
                ) : null}
              </div>
              <Button
                type="submit"
                className="w-full"
                disabled={submitting || retrySeconds > 0}
              >
                <KeyRound aria-hidden="true" />
                {submitting
                  ? "Signing in…"
                  : retrySeconds > 0
                    ? `Try again in ${retrySeconds}s`
                    : "Sign in"}
              </Button>
              <p className="text-center text-xs text-muted-foreground">
                Sessions use an HttpOnly cookie. No password or token is stored
                in this browser.
              </p>
            </form>
          )}
        </CardContent>
      </Card>
    </AuthShell>
  );
}

function AuthShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="relative flex min-h-dvh items-center justify-center overflow-hidden bg-background px-4 py-10">
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(circle_at_20%_10%,color-mix(in_oklab,var(--primary)_14%,transparent),transparent_45%),radial-gradient(circle_at_80%_90%,color-mix(in_oklab,var(--info)_10%,transparent),transparent_40%)]"
      />
      <div className="relative z-10 flex w-full max-w-md flex-col items-center gap-4">
        {children}
        <p className="text-center text-xs text-muted-foreground">
          Monitoring runs on the server and continues while this browser is
          closed.
        </p>
      </div>
    </div>
  );
}
