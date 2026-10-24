import type { PushRegistrationRequest } from "@/api/types";

export class PushPermissionError extends Error {
  readonly permission: NotificationPermission;

  constructor(permission: NotificationPermission) {
    super(
      permission === "denied"
        ? "Browser notification permission is blocked. Allow notifications for this site and try again."
        : "Browser notification permission was not granted.",
    );
    this.name = "PushPermissionError";
    this.permission = permission;
  }
}

export function isPushSupported(): boolean {
  return (
    typeof window !== "undefined" &&
    "serviceWorker" in navigator &&
    "PushManager" in window &&
    "Notification" in window
  );
}

export function pushSupportMessage(): string {
  if (typeof window === "undefined") return "Browser Push is unavailable.";
  if (!window.isSecureContext && window.location.hostname !== "localhost") {
    return "Web Push requires HTTPS (localhost is allowed for development).";
  }
  if (!("serviceWorker" in navigator)) {
    return "This browser does not support service workers.";
  }
  if (!("PushManager" in window)) {
    return "This browser does not support Web Push.";
  }
  if (!("Notification" in window)) {
    return "This browser does not support notifications.";
  }
  return "";
}

/** Converts an unpadded base64url VAPID key to the bytes PushManager expects. */
export function urlBase64ToUint8Array(value: string): Uint8Array<ArrayBuffer> {
  const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = normalized.padEnd(
    normalized.length + ((4 - (normalized.length % 4)) % 4),
    "=",
  );
  const binary = window.atob(padded);
  const bytes = new Uint8Array(new ArrayBuffer(binary.length));
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

async function registration(): Promise<ServiceWorkerRegistration> {
  if (!isPushSupported()) {
    throw new Error(pushSupportMessage() || "Browser Push is unavailable.");
  }
  if (!window.isSecureContext && window.location.hostname !== "localhost") {
    throw new Error(
      "Web Push requires HTTPS (localhost is allowed for development).",
    );
  }
  await navigator.serviceWorker.register("/sw.js");
  return navigator.serviceWorker.ready;
}

export async function currentPushSubscription(): Promise<PushSubscription | null> {
  if (!isPushSupported()) return null;
  const active = await registration();
  return active.pushManager.getSubscription();
}

/**
 * Requests permission and subscribes this browser. Permission is requested
 * only from an explicit user action in the caller.
 */
export async function subscribeThisBrowser(
  publicKey: string,
  label: string,
): Promise<PushRegistrationRequest> {
  if (!publicKey) {
    throw new Error("The server did not provide a VAPID public key.");
  }
  const active = await registration();
  const permission = await Notification.requestPermission();
  if (permission !== "granted") {
    throw new PushPermissionError(permission);
  }
  const existing = await active.pushManager.getSubscription();
  const subscription =
    existing ??
    (await active.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(publicKey),
    }));
  const json = subscription.toJSON();
  if (!json.endpoint || !json.keys?.p256dh || !json.keys.auth) {
    throw new Error("The browser returned an incomplete Push subscription.");
  }
  return {
    endpoint: json.endpoint,
    keys: { p256dh: json.keys.p256dh, auth: json.keys.auth },
    label: label.trim() || "This browser",
  };
}

export async function unsubscribeThisBrowser(): Promise<boolean> {
  if (!isPushSupported()) return false;
  const active = await registration();
  const subscription = await active.pushManager.getSubscription();
  if (!subscription) return false;
  return subscription.unsubscribe();
}

export function browserLabel(): string {
  if (typeof navigator === "undefined") return "This browser";
  const agent = navigator.userAgent;
  if (/Edg\//.test(agent)) return "Microsoft Edge";
  if (/Firefox\//.test(agent)) return "Firefox";
  if (/Chrome\//.test(agent)) return "Chrome";
  if (/Safari\//.test(agent)) return "Safari";
  return "This browser";
}
