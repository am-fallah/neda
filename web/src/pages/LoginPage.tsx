import { useEffect, useRef, useState, type FormEvent } from "react";
import {
  AlertTriangle,
  CheckCircle2,
  Eye,
  EyeOff,
  KeyRound,
  LockKeyhole,
  Server,
  Terminal,
  TrainFront,
} from "lucide-react";
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
} from "@/components/ui/card";
import { Input, Label } from "@/components/ui/input";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";
import { cn } from "@/lib/utils";

function BrandMark() {
  return (
    <div className="flex items-center gap-3">
      <span
        aria-hidden="true"
        className="relative flex size-10 items-center justify-center rounded-lg bg-primary font-mono text-sm font-semibold text-primary-foreground"
      >
        N
        <span className="absolute inset-x-2.5 bottom-1.5 h-px bg-primary-foreground/35" />
      </span>
      <div className="leading-tight">
        <p className="text-sm font-semibold tracking-[-0.01em]">Neda</p>
        <p className="eyebrow text-[0.58rem] tracking-[0.14em]">
          Train operations
        </p>
      </div>
    </div>
  );
}

function AuthStory() {
  const points = [
    {
      icon: Server,
      title: "Server-side checks",
      body: "Monitoring keeps running after this browser closes.",
    },
    {
      icon: TrainFront,
      title: "Exact fare options",
      body: "Watch specific wagons, classes, and provider offerings.",
    },
    {
      icon: CheckCircle2,
      title: "Honest state",
      body: "Reservable, not reservable, and unknown stay distinct.",
    },
  ];
  return (
    <aside className="relative hidden min-h-dvh w-[44%] flex-col justify-between overflow-hidden bg-primary px-10 py-10 text-primary-foreground lg:flex dark:bg-card dark:text-foreground xl:px-14">
      <div className="flex items-center gap-3">
        <span
          aria-hidden="true"
          className="relative flex size-10 items-center justify-center rounded-lg bg-primary-foreground/10 font-mono text-sm font-semibold ring-1 ring-primary-foreground/20 dark:bg-primary/15 dark:ring-primary/30"
        >
          N
          <span className="absolute inset-x-2.5 bottom-1.5 h-px bg-primary-foreground/40 dark:bg-primary/40" />
        </span>
        <div className="leading-tight">
          <p className="text-sm font-semibold tracking-[-0.01em]">Neda</p>
          <p className="font-mono text-[0.58rem] tracking-[0.16em] text-primary-foreground/80 dark:text-muted-foreground uppercase">
            Train operations
          </p>
        </div>
      </div>

      <div className="max-w-md">
        <p className="font-mono text-[0.65rem] tracking-[0.16em] text-primary-foreground/80 dark:text-muted-foreground uppercase">
          Availability monitoring
        </p>
        <p className="mt-4 text-[2.35rem] leading-[1.08] font-semibold tracking-[-0.035em] text-balance">
          Know when seats open. Not after they do.
        </p>
        <p className="mt-4 max-w-sm text-sm leading-relaxed text-primary-foreground/85 dark:text-muted-foreground">
          A private operations console for train availability: explicit states,
          exact fare options, and notification delivery you can audit.
        </p>
        <div className="mt-8 h-px w-20 bg-primary-foreground/25 dark:bg-border" />
        <ul className="mt-8 space-y-4">
          {points.map((point) => {
            const Icon = point.icon;
            return (
              <li key={point.title} className="flex items-start gap-3">
                <span className="mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md border border-primary-foreground/20 bg-primary-foreground/10 dark:border-border dark:bg-muted">
                  <Icon className="size-3.5" aria-hidden="true" />
                </span>
                <div>
                  <p className="text-sm font-medium">{point.title}</p>
                  <p className="mt-0.5 text-xs leading-relaxed text-primary-foreground/80 dark:text-muted-foreground">
                    {point.body}
                  </p>
                </div>
              </li>
            );
          })}
        </ul>
      </div>

      <p className="flex items-center gap-2 font-mono text-[0.6rem] tracking-[0.12em] text-primary-foreground/80 dark:text-muted-foreground uppercase">
        <LockKeyhole className="size-3" aria-hidden="true" />
        Private single-user workspace
      </p>
    </aside>
  );
}

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
  const [showPassword, setShowPassword] = useState(false);
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
      <Card className="w-full max-w-md gap-0 py-0 shadow-[var(--shadow-overlay)] sm:py-0">
        <CardHeader className="gap-4 border-b px-5 py-5 sm:px-6">
          <div className="lg:hidden">
            <BrandMark />
          </div>
          <div>
            <p className="eyebrow text-[0.6rem]">Owner access</p>
            <h1 className="mt-1.5 text-xl leading-tight font-semibold tracking-[-0.015em]">
              {showSetup ? "Set up your password" : "Sign in"}
            </h1>
            <CardDescription className="mt-1">
              {showSetup
                ? "Neda has no default password. Create one from the server terminal before signing in."
                : "Private, single-user dashboard. Enter the owner password to continue."}
            </CardDescription>
          </div>
        </CardHeader>
        <CardContent className="px-5 py-5 sm:px-6">
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
              <p className="text-xs leading-relaxed text-muted-foreground">
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
              <div className="space-y-2">
                <Label htmlFor="password">Password</Label>
                <div className="relative">
                  <Input
                    ref={passwordRef}
                    id="password"
                    name="password"
                    type={showPassword ? "text" : "password"}
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
                    className={cn(
                      "h-11 pr-12 text-base",
                      fieldError && "border-destructive",
                    )}
                  />
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    className="absolute top-1/2 right-1 size-9 -translate-y-1/2 max-sm:size-10"
                    onClick={() => setShowPassword((visible) => !visible)}
                    aria-label={showPassword ? "Hide password" : "Show password"}
                    aria-pressed={showPassword}
                    disabled={submitting || retrySeconds > 0}
                  >
                    {showPassword ? (
                      <EyeOff aria-hidden="true" />
                    ) : (
                      <Eye aria-hidden="true" />
                    )}
                  </Button>
                </div>
                {fieldError ? (
                  <p
                    id="password-error"
                    role="alert"
                    className="text-xs font-medium text-destructive"
                  >
                    {fieldError}
                  </p>
                ) : null}
              </div>
              <Button
                type="submit"
                className="w-full"
                size="lg"
                disabled={submitting || retrySeconds > 0}
              >
                <KeyRound aria-hidden="true" />
                {submitting
                  ? "Signing in…"
                  : retrySeconds > 0
                    ? `Try again in ${retrySeconds}s`
                    : "Sign in"}
              </Button>
              <p className="flex items-start gap-2 text-xs leading-relaxed text-muted-foreground">
                <LockKeyhole
                  className="mt-0.5 size-3.5 shrink-0"
                  aria-hidden="true"
                />
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
    <div className="flex min-h-dvh bg-background">
      <AuthStory />
      <main className="relative flex min-h-dvh flex-1 items-center justify-center px-4 py-10 sm:px-6">
        <div
          aria-hidden="true"
          className="surface-grid pointer-events-none absolute inset-0 opacity-[0.35] [mask-image:radial-gradient(ellipse_at_center,black,transparent_75%)]"
        />
        <div className="relative z-10 flex w-full max-w-md flex-col items-center gap-4">
          {children}
          <p className="max-w-sm text-center text-xs leading-relaxed text-muted-foreground">
            Monitoring runs on the server and continues while this browser is
            closed.
          </p>
        </div>
      </main>
    </div>
  );
}
