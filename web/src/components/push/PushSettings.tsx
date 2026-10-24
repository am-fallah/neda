import { useCallback, useEffect, useState } from "react";
import {
  AlertTriangle,
  Bell,
  BellOff,
  BellRing,
  CheckCircle2,
  Loader2,
  Send,
  Smartphone,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { isApiError } from "@/api/client";
import { pushApi, settingsApi } from "@/api/endpoints";
import type { PushConfig, PushSubscriptionRecord, Settings } from "@/api/types";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { FormField, fieldProps } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";
import { formatTehranTimestamp } from "@/lib/dates";
import {
  PushPermissionError,
  browserLabel,
  isPushSupported,
  pushSupportMessage,
  subscribeThisBrowser,
  unsubscribeThisBrowser,
} from "@/lib/push";

function permissionLabel(): string {
  if (!isPushSupported()) return "Unsupported";
  switch (Notification.permission) {
    case "granted":
      return "Allowed";
    case "denied":
      return "Blocked";
    default:
      return "Not requested";
  }
}

export function PushSettings({
  settings,
  onUpdated,
}: {
  settings: Settings;
  onUpdated: (settings: Settings) => void;
}) {
  const [config, setConfig] = useState<PushConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [label, setLabel] = useState(browserLabel());
  const [busy, setBusy] = useState("");

  const load = useCallback(async (signal?: AbortSignal) => {
    setError("");
    try {
      const next = await pushApi.config(signal);
      setConfig(next);
    } catch (caught) {
      if (signal?.aborted) return;
      setError(
        isApiError(caught)
          ? caught.message
          : "Push configuration could not be loaded.",
      );
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void load(controller.signal);
    return () => controller.abort();
  }, [load]);

  // The service worker cannot perform an authenticated API write because the
  // CSRF token lives in memory. An open dashboard completes the re-registration.
  useEffect(() => {
    if (!isPushSupported() || !navigator.serviceWorker) return;
    const onMessage = (event: MessageEvent) => {
      const data = event.data as
        | { type?: string; subscription?: { endpoint?: string; keys?: { p256dh?: string; auth?: string } } | null }
        | undefined;
      if (data?.type !== "neda:push-subscription-change") return;
      const subscription = data.subscription;
      if (!subscription?.endpoint || !subscription.keys?.p256dh || !subscription.keys.auth) {
        toast.info("Browser Push subscription changed", {
          description: "Enable this browser again to receive notifications.",
        });
        return;
      }
      void pushApi
        .subscribe({
          endpoint: subscription.endpoint,
          keys: { p256dh: subscription.keys.p256dh, auth: subscription.keys.auth },
          label: browserLabel(),
        })
        .then(() => {
          toast.success("Browser Push subscription refreshed");
          void load();
        })
        .catch(() => {
          toast.error("Could not refresh the browser Push subscription");
        });
    };
    navigator.serviceWorker.addEventListener("message", onMessage);
    return () =>
      navigator.serviceWorker.removeEventListener("message", onMessage);
  }, [load]);

  async function enableThisBrowser() {
    if (!config) return;
    if (!config.available || !config.publicKey) {
      setError(
        "Web Push is not available. The server must have an encryption key and NEDA_VAPID_SUBJECT configured.",
      );
      return;
    }
    setBusy("enable");
    setError("");
    try {
      const registration = await subscribeThisBrowser(config.publicKey, label);
      await pushApi.subscribe(registration);
      const updated = await settingsApi.update({ pushEnabled: true });
      onUpdated(updated);
      await load();
      toast.success("Browser Push enabled", {
        description:
          "Availability and provider-health notifications can now reach this browser.",
      });
    } catch (caught) {
      if (caught instanceof PushPermissionError) {
        setError(caught.message);
      } else if (isApiError(caught)) {
        setError(caught.message);
      } else {
        setError("Browser Push could not be enabled.");
      }
    } finally {
      setBusy("");
    }
  }

  async function toggleGlobal(enabled: boolean) {
    setBusy(enabled ? "resume" : "pause");
    setError("");
    try {
      const updated = await settingsApi.update({ pushEnabled: enabled });
      onUpdated(updated);
      await load();
      toast.success(enabled ? "Push delivery resumed" : "Push delivery paused");
    } catch (caught) {
      setError(
        isApiError(caught) ? caught.message : "Push settings could not be saved.",
      );
    } finally {
      setBusy("");
    }
  }

  async function removeDevice(subscription: PushSubscriptionRecord) {
    setBusy(`remove:${subscription.id}`);
    setError("");
    try {
      await pushApi.unsubscribe(subscription.id);
      await load();
      toast.success("Device removed", {
        description: "No further Push deliveries will be sent to this device.",
      });
    } catch (caught) {
      setError(
        isApiError(caught) ? caught.message : "The device could not be removed.",
      );
    } finally {
      setBusy("");
    }
  }

  async function forgetThisBrowser() {
    setBusy("forget");
    setError("");
    try {
      const removed = await unsubscribeThisBrowser();
      await load();
      toast.success(
        removed
          ? "This browser unsubscribed from Push"
          : "This browser had no Push subscription",
      );
    } catch {
      setError("The browser subscription could not be removed.");
    } finally {
      setBusy("");
    }
  }

  async function sendTest(subscriptionId?: string) {
    setBusy(subscriptionId ? `test:${subscriptionId}` : "test-all");
    setError("");
    try {
      const result = await pushApi.test(subscriptionId);
      if (result.sent > 0 && result.failed === 0) {
        toast.success("Test Push sent", {
          description: `${result.sent} device${result.sent === 1 ? "" : "s"} accepted the notification.`,
        });
      } else if (result.sent > 0) {
        toast.warning("Test Push partially delivered", {
          description: `${result.sent} sent, ${result.failed} failed${result.errorKinds?.length ? ` (${result.errorKinds.join(", ")})` : ""}.`,
        });
      } else {
        toast.error("Test Push failed", {
          description: result.errorKinds?.length
            ? `Sanitized categories: ${result.errorKinds.join(", ")}`
            : "No device accepted the notification.",
        });
      }
      await load();
    } catch (caught) {
      if (isApiError(caught)) {
        if (caught.code === "push_disabled") {
          setError("Resume Push delivery before sending a test notification.");
        } else {
          setError(caught.message);
        }
      } else {
        setError("The test Push could not be sent.");
      }
    } finally {
      setBusy("");
    }
  }

  if (loading) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" aria-hidden="true" />
        Loading browser Push status…
      </div>
    );
  }

  const supportMessage = pushSupportMessage();
  const available = config?.available ?? false;
  const enabled = settings.pushEnabled;
  const devices = config?.subscriptions ?? [];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        {enabled ? (
          <Badge variant="success">
            <CheckCircle2 aria-hidden="true" />
            Delivery enabled
          </Badge>
        ) : (
          <Badge variant="secondary">
            <BellOff aria-hidden="true" />
            Delivery paused
          </Badge>
        )}
        <Badge variant="outline">Permission: {permissionLabel()}</Badge>
        <Badge variant="outline">{devices.length} device{devices.length === 1 ? "" : "s"}</Badge>
      </div>

      {supportMessage ? (
        <Alert variant="warning">
          <AlertTriangle aria-hidden="true" />
          <AlertTitle>This browser cannot use Web Push</AlertTitle>
          <AlertDescription>{supportMessage}</AlertDescription>
        </Alert>
      ) : null}

      {!available ? (
        <Alert variant="warning">
          <AlertTriangle aria-hidden="true" />
          <AlertTitle>Push is not configured on the server</AlertTitle>
          <AlertDescription>
            Configure the encryption key and <code>NEDA_VAPID_SUBJECT</code>,
            then restart the service. The server returns only the public VAPID
            key; the private key never leaves the server.
          </AlertDescription>
        </Alert>
      ) : null}

      {error ? (
        <Alert variant="destructive">
          <AlertTriangle aria-hidden="true" />
          <AlertTitle>Push request failed</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      <div className="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-end">
        <FormField
          id="push-label"
          label="Device label"
          description="Shown only in this dashboard so you can recognize each browser."
        >
          <Input
            {...fieldProps("push-label", "")}
            value={label}
            maxLength={100}
            onChange={(event) => setLabel(event.target.value)}
            disabled={!available || busy !== ""}
          />
        </FormField>
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            onClick={() => void enableThisBrowser()}
            disabled={!available || Boolean(supportMessage) || busy !== ""}
          >
            {busy === "enable" ? (
              <Loader2 className="animate-spin" aria-hidden="true" />
            ) : (
              <Bell aria-hidden="true" />
            )}
            {busy === "enable" ? "Enabling…" : "Enable this browser"}
          </Button>
          {enabled ? (
            <Button
              type="button"
              variant="outline"
              onClick={() => void toggleGlobal(false)}
              disabled={busy !== ""}
            >
              {busy === "pause" ? (
                <Loader2 className="animate-spin" aria-hidden="true" />
              ) : (
                <BellOff aria-hidden="true" />
              )}
              Pause delivery
            </Button>
          ) : (
            <Button
              type="button"
              variant="outline"
              onClick={() => void toggleGlobal(true)}
              disabled={busy !== "" || devices.length === 0}
            >
              {busy === "resume" ? (
                <Loader2 className="animate-spin" aria-hidden="true" />
              ) : (
                <BellRing aria-hidden="true" />
              )}
              Resume delivery
            </Button>
          )}
        </div>
      </div>

      <div className="flex flex-wrap gap-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => void sendTest()}
          disabled={!enabled || devices.length === 0 || busy !== ""}
        >
          {busy === "test-all" ? (
            <Loader2 className="animate-spin" aria-hidden="true" />
          ) : (
            <Send aria-hidden="true" />
          )}
          Send test to all devices
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          onClick={() => void forgetThisBrowser()}
          disabled={busy !== ""}
        >
          <BellOff aria-hidden="true" />
          Unsubscribe this browser
        </Button>
      </div>

      {devices.length === 0 ? (
        <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">
          No browser is registered yet. Enabling this browser requests
          notification permission and stores only encrypted subscription
          material on the server.
        </div>
      ) : (
        <ul className="space-y-2" aria-label="Registered Push devices">
          {devices.map((device) => (
            <li
              key={device.id}
              className="flex flex-col gap-3 rounded-md border p-3 sm:flex-row sm:items-center sm:justify-between"
            >
              <div className="min-w-0 space-y-1">
                <div className="flex flex-wrap items-center gap-2">
                  <Smartphone className="size-4" aria-hidden="true" />
                  <span className="font-medium">
                    {device.label || "Unnamed device"}
                  </span>
                  {device.failureCount > 0 ? (
                    <Badge variant="warning">
                      {device.failureCount} failure
                      {device.failureCount === 1 ? "" : "s"}
                    </Badge>
                  ) : null}
                </div>
                <p className="text-xs text-muted-foreground">
                  Added {formatTehranTimestamp(device.createdAt)} · Last success{" "}
                  {formatTehranTimestamp(device.lastSuccessAt)}
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => void sendTest(device.id)}
                  disabled={!enabled || busy !== ""}
                >
                  {busy === `test:${device.id}` ? (
                    <Loader2 className="animate-spin" aria-hidden="true" />
                  ) : (
                    <Send aria-hidden="true" />
                  )}
                  Test
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="text-destructive"
                  onClick={() => void removeDevice(device)}
                  disabled={busy !== ""}
                >
                  {busy === `remove:${device.id}` ? (
                    <Loader2 className="animate-spin" aria-hidden="true" />
                  ) : (
                    <Trash2 aria-hidden="true" />
                  )}
                  Remove
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}

      <p className="text-xs text-muted-foreground">
        Push requires HTTPS (localhost is allowed for development). The service
        worker shows notifications while this dashboard is closed. Clicking an
        availability notification opens the validated booking link or the event
        detail page. Subscription endpoints and encryption keys are never
        returned to this page.
      </p>
    </div>
  );
}
