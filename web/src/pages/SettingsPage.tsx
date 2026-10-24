import { useCallback, useEffect, useState, type FormEvent } from "react";
import {
  AlertTriangle,
  BellOff,
  CheckCircle2,
  KeyRound,
  Loader2,
  LockKeyhole,
  MessageCircle,
  Save,
  Send,
  Settings2,
  SunMoon,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { authApi, settingsApi } from "@/api/endpoints";
import { isApiError } from "@/api/client";
import type { Settings } from "@/api/types";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { PageHeader } from "@/components/layout/PageHeader";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { FormField, fieldProps } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";
import { useApiResource } from "@/hooks/useApiResource";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";
import { useTheme } from "@/lib/theme";
import { formatTehranTimestamp } from "@/lib/dates";

function TelegramStatus({ settings }: { settings: Settings }) {
  const telegram = settings.telegram;
  if (!telegram.available) {
    return (
      <Alert variant="warning">
        <AlertTriangle aria-hidden="true" />
        <AlertTitle>Telegram settings are unavailable</AlertTitle>
        <AlertDescription>
          The server has no encryption key configured, so credentials cannot be
          stored or read. Ask the operator to configure the encryption key file.
        </AlertDescription>
      </Alert>
    );
  }
  if (telegram.configured && !telegram.readable) {
    return (
      <Alert variant="destructive">
        <AlertTriangle aria-hidden="true" />
        <AlertTitle>Stored Telegram settings cannot be read</AlertTitle>
        <AlertDescription>
          The stored credentials cannot be decrypted with the current key. Clear
          them and enter the token and chat ID again.
        </AlertDescription>
      </Alert>
    );
  }
  if (!telegram.configured) {
    return (
      <Alert variant="info">
        <MessageCircle aria-hidden="true" />
        <AlertTitle>Telegram is not configured</AlertTitle>
        <AlertDescription>
          Add a bot token and chat ID below to enable Telegram delivery and
          testing.
        </AlertDescription>
      </Alert>
    );
  }
  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="success">
          <CheckCircle2 aria-hidden="true" />
          Configured
        </Badge>
        {telegram.chatLabel ? (
          <Badge variant="outline">Chat: {telegram.chatLabel}</Badge>
        ) : null}
      </div>
      <dl className="grid gap-2 text-xs text-muted-foreground sm:grid-cols-2">
        <div>
          <dt className="font-medium text-foreground">Last test</dt>
          <dd>
            {telegram.lastTestAt
              ? formatTehranTimestamp(telegram.lastTestAt)
              : "No test yet"}
          </dd>
        </div>
        <div>
          <dt className="font-medium text-foreground">Test result</dt>
          <dd>
            {telegram.lastTestOk === true
              ? "Succeeded"
              : telegram.lastTestOk === false
                ? `Failed${telegram.lastErrorKind ? ` (${telegram.lastErrorKind})` : ""}`
                : "No test yet"}
          </dd>
        </div>
      </dl>
      <p className="text-xs text-muted-foreground">
        The bot token and raw chat ID are never returned to this browser. Only
        this masked label is shown.
      </p>
    </div>
  );
}

function PollingIntervalForm({
  settings,
  onUpdated,
}: {
  settings: Settings;
  onUpdated: (settings: Settings) => void;
}) {
  const [value, setValue] = useState(String(settings.pollIntervalSeconds));
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setValue(String(settings.pollIntervalSeconds));
  }, [settings.pollIntervalSeconds]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    const seconds = Number(value);
    if (!Number.isInteger(seconds) || seconds < 60 || seconds > 3600) {
      setError("Enter a whole number between 60 and 3600 seconds.");
      return;
    }
    setSaving(true);
    try {
      const updated = await settingsApi.update({
        pollIntervalSeconds: seconds,
      });
      onUpdated(updated);
      toast.success("Polling interval saved", {
        description: `Monitors poll every ${Math.round(seconds / 60)} minute${
          Math.round(seconds / 60) === 1 ? "" : "s"
        } unless backed off after failures.`,
      });
    } catch (caught) {
      if (isApiError(caught)) {
        const fieldError = caught.fields.pollIntervalSeconds;
        setError(fieldError ?? caught.message);
      } else {
        setError("The polling interval could not be saved.");
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="space-y-3">
      <div className="flex flex-wrap items-end gap-3">
        <FormField
          id="pollIntervalSeconds"
          label="Polling interval (seconds)"
          error={error}
          description="Allowed range: 60–3600 seconds. The default is five minutes. Provider backoff can delay individual checks."
          className="w-full max-w-xs"
        >
          <Input
            {...fieldProps("pollIntervalSeconds", error)}
            type="number"
            min={60}
            max={3600}
            step={1}
            inputMode="numeric"
            value={value}
            onChange={(event) => {
              setValue(event.target.value);
              setError("");
            }}
          />
        </FormField>
        <Button type="submit" disabled={saving}>
          {saving ? (
            <Loader2 className="animate-spin" aria-hidden="true" />
          ) : (
            <Save aria-hidden="true" />
          )}
          {saving ? "Saving…" : "Save interval"}
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        {Number(value) >= 60 && Number(value) <= 3600
          ? `Approximately ${(Number(value) / 60).toFixed(
              Number(value) % 60 === 0 ? 0 : 1,
            )} minutes between checks.`
          : "Enter a value between 60 and 3600 seconds."}
      </p>
    </form>
  );
}

function TelegramForm({
  settings,
  onUpdated,
}: {
  settings: Settings;
  onUpdated: (settings: Settings) => void;
}) {
  const [token, setToken] = useState("");
  const [chatId, setChatId] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState("");
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [clearing, setClearing] = useState(false);
  const telegram = settings.telegram;
  const canConfigure = telegram.available;
  const canTest = telegram.available && telegram.configured && telegram.readable;

  async function handleSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFieldErrors({});
    setFormError("");
    const errors: Record<string, string> = {};
    if (!token.trim()) errors.token = "Enter the bot token.";
    if (!chatId.trim()) errors.chatId = "Enter the chat ID or @username.";
    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);
      return;
    }
    setSaving(true);
    try {
      const updated = await settingsApi.updateTelegram({
        token: token.trim(),
        chatId: chatId.trim(),
      });
      setToken("");
      setChatId("");
      onUpdated(updated);
      toast.success("Telegram settings saved", {
        description: "Credentials are encrypted at rest and never shown again.",
      });
    } catch (caught) {
      if (isApiError(caught)) {
        if (Object.keys(caught.fields).length > 0) {
          setFieldErrors(caught.fields);
        } else {
          setFormError(caught.message);
        }
      } else {
        setFormError("Telegram settings could not be saved.");
      }
    } finally {
      setSaving(false);
    }
  }

  async function handleTest() {
    setFormError("");
    setTesting(true);
    try {
      const updated = await settingsApi.testTelegram();
      onUpdated(updated);
      if (updated.telegram.lastTestOk) {
        toast.success("Telegram test succeeded", {
          description: "The test message was accepted by Telegram.",
        });
      } else {
        toast.error("Telegram test failed", {
          description: updated.telegram.lastErrorKind
            ? `Sanitized error category: ${updated.telegram.lastErrorKind}`
            : "The bot could not validate or send the test message.",
        });
      }
    } catch (caught) {
      if (isApiError(caught)) {
        setFormError(caught.message);
      } else {
        setFormError("The Telegram test could not be completed.");
      }
    } finally {
      setTesting(false);
    }
  }

  async function handleClear() {
    setFormError("");
    setClearing(true);
    try {
      const updated = await settingsApi.updateTelegram({ clear: true });
      onUpdated(updated);
      setToken("");
      setChatId("");
      toast.success("Telegram settings cleared");
    } catch (caught) {
      if (isApiError(caught)) setFormError(caught.message);
      else setFormError("Telegram settings could not be cleared.");
    } finally {
      setClearing(false);
    }
  }

  return (
    <div className="space-y-4">
      <TelegramStatus settings={settings} />
      {formError ? (
        <Alert variant="destructive">
          <AlertTriangle aria-hidden="true" />
          <AlertTitle>Telegram request failed</AlertTitle>
          <AlertDescription>{formError}</AlertDescription>
        </Alert>
      ) : null}
      <form onSubmit={handleSave} noValidate className="space-y-3">
        <div className="grid gap-4 sm:grid-cols-2">
          <FormField
            id="telegram-token"
            label="Bot token"
            error={fieldErrors.token}
            description="Write-only. Leave blank unless saving new credentials; stored secrets are never prefilled."
          >
            <Input
              {...fieldProps("telegram-token", fieldErrors.token)}
              type="password"
              autoComplete="off"
              autoCapitalize="none"
              spellCheck={false}
              value={token}
              onChange={(event) => {
                setToken(event.target.value);
                setFieldErrors((current) => ({ ...current, token: "" }));
              }}
              placeholder={telegram.configured ? "Enter a new token" : "123456:ABC…"}
              disabled={!canConfigure || saving}
            />
          </FormField>
          <FormField
            id="telegram-chat"
            label="Chat ID or @username"
            error={fieldErrors.chatId}
            description="Write-only. The API returns only a masked chat label."
          >
            <Input
              {...fieldProps("telegram-chat", fieldErrors.chatId)}
              autoComplete="off"
              autoCapitalize="none"
              spellCheck={false}
              value={chatId}
              onChange={(event) => {
                setChatId(event.target.value);
                setFieldErrors((current) => ({ ...current, chatId: "" }));
              }}
              placeholder={telegram.configured ? "Enter a new chat ID" : "123456789 or @channel"}
              disabled={!canConfigure || saving}
            />
          </FormField>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={!canConfigure || saving}>
            {saving ? (
              <Loader2 className="animate-spin" aria-hidden="true" />
            ) : (
              <Save aria-hidden="true" />
            )}
            {saving ? "Saving…" : "Save Telegram credentials"}
          </Button>
          <Button
            type="button"
            variant="outline"
            onClick={() => void handleTest()}
            disabled={!canTest || testing || saving || clearing}
          >
            {testing ? (
              <Loader2 className="animate-spin" aria-hidden="true" />
            ) : (
              <Send aria-hidden="true" />
            )}
            {testing ? "Testing…" : "Send test message"}
          </Button>
          {telegram.configured ? (
            <AlertDialog>
              <AlertDialogTrigger asChild>
                <Button
                  type="button"
                  variant="outline"
                  className="text-destructive"
                  disabled={clearing || saving}
                >
                  <Trash2 aria-hidden="true" />
                  {clearing ? "Clearing…" : "Clear credentials"}
                </Button>
              </AlertDialogTrigger>
              <AlertDialogContent>
                <AlertDialogHeader>
                  <AlertDialogTitle>Clear Telegram credentials?</AlertDialogTitle>
                  <AlertDialogDescription>
                    This removes the encrypted token and chat ID and stops
                    Telegram delivery. Existing delivery history is retained.
                    This cannot be undone without re-entering the credentials.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel>Cancel</AlertDialogCancel>
                  <AlertDialogAction
                    onClick={(event) => {
                      event.preventDefault();
                      void handleClear();
                    }}
                  >
                    Clear credentials
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          ) : null}
        </div>
        <p className="text-xs text-muted-foreground">
          A test validates the bot with Telegram and sends one message. Test
          failures are recorded only as a sanitized category.
        </p>
      </form>
    </div>
  );
}

function PasswordForm() {
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState("");
  const [saving, setSaving] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFieldErrors({});
    setFormError("");
    const errors: Record<string, string> = {};
    if (!currentPassword) errors.currentPassword = "Enter your current password.";
    if ([...newPassword].length < 12) {
      errors.newPassword = "Use at least 12 characters.";
    }
    if (newPassword !== confirmPassword) {
      errors.confirmPassword = "Passwords do not match.";
    }
    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);
      return;
    }
    setSaving(true);
    try {
      await authApi.changePassword(currentPassword, newPassword);
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
      toast.success("Password changed", {
        description: "Other sessions were revoked. This session remains active.",
      });
    } catch (caught) {
      if (isApiError(caught)) {
        if (caught.code === "invalid_credentials") {
          setFieldErrors({ currentPassword: "Current password is incorrect." });
        } else if (caught.code === "validation_error") {
          setFieldErrors({ newPassword: caught.message });
        } else {
          setFormError(caught.message);
        }
      } else {
        setFormError("The password could not be changed.");
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} noValidate className="space-y-4">
      {formError ? (
        <Alert variant="destructive">
          <AlertTriangle aria-hidden="true" />
          <AlertTitle>Password change failed</AlertTitle>
          <AlertDescription>{formError}</AlertDescription>
        </Alert>
      ) : null}
      <FormField
        id="currentPassword"
        label="Current password"
        error={fieldErrors.currentPassword}
      >
        <Input
          {...fieldProps("currentPassword", fieldErrors.currentPassword)}
          type="password"
          autoComplete="current-password"
          value={currentPassword}
          onChange={(event) => {
            setCurrentPassword(event.target.value);
            setFieldErrors((current) => ({ ...current, currentPassword: "" }));
          }}
        />
      </FormField>
      <div className="grid gap-4 sm:grid-cols-2">
        <FormField
          id="newPassword"
          label="New password"
          error={fieldErrors.newPassword}
          description="At least 12 characters. No password is stored in the browser."
        >
          <Input
            {...fieldProps("newPassword", fieldErrors.newPassword)}
            type="password"
            autoComplete="new-password"
            value={newPassword}
            onChange={(event) => {
              setNewPassword(event.target.value);
              setFieldErrors((current) => ({ ...current, newPassword: "" }));
            }}
          />
        </FormField>
        <FormField
          id="confirmPassword"
          label="Confirm new password"
          error={fieldErrors.confirmPassword}
        >
          <Input
            {...fieldProps("confirmPassword", fieldErrors.confirmPassword)}
            type="password"
            autoComplete="new-password"
            value={confirmPassword}
            onChange={(event) => {
              setConfirmPassword(event.target.value);
              setFieldErrors((current) => ({ ...current, confirmPassword: "" }));
            }}
          />
        </FormField>
      </div>
      <Button type="submit" disabled={saving}>
        {saving ? (
          <Loader2 className="animate-spin" aria-hidden="true" />
        ) : (
          <KeyRound aria-hidden="true" />
        )}
        {saving ? "Changing…" : "Change password"}
      </Button>
    </form>
  );
}

function AppearanceForm() {
  const { theme, setTheme } = useTheme();
  const choices: Array<{ value: "light" | "dark" | "system"; label: string }> = [
    { value: "light", label: "Light" },
    { value: "dark", label: "Dark" },
    { value: "system", label: "System" },
  ];
  return (
    <div className="flex flex-wrap gap-2" role="group" aria-label="Theme">
      {choices.map((choice) => (
        <Button
          key={choice.value}
          type="button"
          variant={theme === choice.value ? "default" : "outline"}
          size="sm"
          aria-pressed={theme === choice.value}
          onClick={() => setTheme(choice.value)}
        >
          {choice.value === theme ? (
            <CheckCircle2 aria-hidden="true" />
          ) : (
            <SunMoon aria-hidden="true" />
          )}
          {choice.label}
        </Button>
      ))}
    </div>
  );
}

export function SettingsPage() {
  useDocumentTitle("Settings");
  const loader = useCallback(
    (signal: AbortSignal): Promise<Settings> => settingsApi.get(signal),
    [],
  );
  const { data, error, loading, refresh } = useApiResource(loader);
  const [settings, setSettings] = useState<Settings | null>(null);

  useEffect(() => {
    if (data) setSettings(data);
  }, [data]);

  if (loading) {
    return (
      <div className="space-y-6">
        <PageHeader title="Settings" description="Loading settings…" />
        <LoadingState rows={3} label="Loading settings" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="space-y-6">
        <PageHeader title="Settings" />
        <ErrorState
          error={error}
          title="Could not load settings"
          onRetry={refresh}
        />
      </div>
    );
  }

  if (!settings) return null;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Settings"
        description="Non-secret application settings and encrypted notification configuration. Stored secrets are never shown again."
      />

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Settings2 className="size-4" aria-hidden="true" />
            Monitoring
          </CardTitle>
          <CardDescription>
            Global polling interval for all active monitors. The server enforces
            the 60–3600 second bounds.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <PollingIntervalForm settings={settings} onUpdated={setSettings} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <MessageCircle className="size-4" aria-hidden="true" />
            Telegram
          </CardTitle>
          <CardDescription>
            Configure the bot token and chat ID for availability and
            provider-health notifications. Credentials are encrypted at rest.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <TelegramForm settings={settings} onUpdated={setSettings} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <BellOff className="size-4" aria-hidden="true" />
            Browser Push
          </CardTitle>
          <CardDescription>
            Web Push is not available in this build.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Alert variant="info">
            <BellOff aria-hidden="true" />
            <AlertTitle>Push setup is not implemented yet</AlertTitle>
            <AlertDescription>
              Server-side Web Push, VAPID keys, subscription management, and the
              service worker are tracked separately. This page does not expose a
              Push enable control that would imply working delivery. Telegram
              notifications are independent.
            </AlertDescription>
          </Alert>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <LockKeyhole className="size-4" aria-hidden="true" />
            Security
          </CardTitle>
          <CardDescription>
            Change the owner password. Other sessions are revoked on success.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <PasswordForm />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <SunMoon className="size-4" aria-hidden="true" />
            Appearance
          </CardTitle>
          <CardDescription>
            Light, dark, or follow the system preference. The choice is stored in
            this browser only.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <AppearanceForm />
        </CardContent>
      </Card>

      <p className="text-xs text-muted-foreground">
        Last loaded settings for polling every{" "}
        <span className="font-medium text-foreground">
          {settings.pollIntervalSeconds} seconds
        </span>
        . Telegram Push state is owned by separate backend tasks; no secret or
        ciphertext is included in this page.
      </p>
    </div>
  );
}
