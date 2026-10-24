/* Neda service worker: display server-side Web Push and handle clicks. */
/* global self, clients */

const FALLBACK_PATH = "/events";

function fallbackTarget() {
  return new URL(FALLBACK_PATH, self.location.origin).href;
}

function safeTarget(raw) {
  if (typeof raw !== "string" || raw.length === 0) return fallbackTarget();
  try {
    const target = new URL(raw, self.location.origin);
    if (target.origin === self.location.origin) return target.href;
    if (
      target.protocol === "https:" &&
      (target.hostname === "mrbilit.com" ||
        target.hostname.endsWith(".mrbilit.com"))
    ) {
      return target.href;
    }
  } catch {
    // Fall through to the safe same-origin event page.
  }
  return fallbackTarget();
}

self.addEventListener("install", (event) => {
  event.waitUntil(self.skipWaiting());
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

self.addEventListener("push", (event) => {
  let data = {};
  try {
    data = event.data ? event.data.json() : {};
  } catch {
    data = {};
  }
  const title =
    typeof data.title === "string" && data.title.trim()
      ? data.title.trim().slice(0, 120)
      : "Neda notification";
  const body =
    typeof data.body === "string" ? data.body.slice(0, 1200) : "";
  const tag =
    typeof data.tag === "string" && data.tag
      ? data.tag.slice(0, 160)
      : "neda-event";
  const options = {
    body,
    tag,
    data: {
      url: safeTarget(data.url),
      eventId: typeof data.eventId === "string" ? data.eventId : "",
      kind: typeof data.kind === "string" ? data.kind : "",
    },
    icon: "/favicon.svg",
    badge: "/favicon.svg",
    renotify: false,
  };
  event.waitUntil(self.registration.showNotification(title, options));
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const target = safeTarget(
    event.notification.data && event.notification.data.url,
  );
  event.waitUntil(
    (async () => {
      const windows = await clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });
      for (const client of windows) {
        if (client.url && new URL(client.url).origin === self.location.origin) {
          await client.focus();
          if ("navigate" in client) {
            await client.navigate(target);
          }
          return;
        }
      }
      await clients.openWindow(target);
    })(),
  );
});

// Browsers rotate Push subscriptions periodically. The API requires the
// in-memory CSRF token, so an open dashboard performs the authenticated
// re-registration; otherwise the server disables the dead subscription on the
// next 404/410 delivery attempt.
self.addEventListener("pushsubscriptionchange", (event) => {
  event.waitUntil(
    (async () => {
      const windows = await clients.matchAll({
        type: "window",
        includeUncontrolled: true,
      });
      const subscription = event.newSubscription
        ? event.newSubscription.toJSON()
        : null;
      windows.forEach((client) => {
        client.postMessage({
          type: "neda:push-subscription-change",
          subscription,
        });
      });
    })(),
  );
});
