# Browser Push Validation — Evidence

**Date:** 2026-10-25
**Tasks:** `PSH-003`, `PSH-005`
**Environment:** macOS arm64, installed Google Chrome via Playwright
persistent context, local `neda serve` on `http://localhost`, temporary SQLite
database and VAPID key. No real Push service subscription, real user, Telegram
message, provider request, or production data was used.

## Exercised

1. Signed in to the dashboard with a temporary test owner password.
2. Opened Settings and confirmed the Browser Push card renders:
   - "Delivery paused" badge;
   - enabled "Enable this browser" control;
   - empty-device guidance.
3. Registered `/sw.js` manually and confirmed an active service worker with
   scope `/`.
4. Enabled Chrome DevTools Protocol `ServiceWorker.deliverPushMessage` with:
   - title `Neda browser test`;
   - body `Synthetic CDP push for service-worker validation`;
   - tag `neda-browser-test`;
   - URL `/events`.
   `registration.getNotifications()` returned the same title/body/tag and a
   normalized `http://localhost:.../events` data URL.
5. Set the notification permission to `denied` through CDP, reloaded, clicked
   "Enable this browser", and confirmed the UI showed:
   "Browser notification permission is blocked. Allow notifications for this
   site and try again."
6. Confirmed no page/console errors in the persistent (non-incognito) context.
7. Added `web/src/lib/serviceWorker.test.ts`, which executes the real
   `public/sw.js` in a VM and covers install/activate, push payload display,
   unsafe-URL fallback, existing-window focus/navigate, `openWindow` fallback,
   and `pushsubscriptionchange` re-registration messaging.

## Results

| Check | Result |
|---|---|
| Push control present / enabled | pass |
| Empty-device guidance | pass |
| Service worker registration/active | pass |
| Synthetic push displayed with expected title/body/tag/URL | pass |
| Unsafe click target fallback | pass (VM test) |
| Existing-window focus + navigation | pass (VM test) |
| No dashboard client → `openWindow` | pass (VM test) |
| Rotated subscription → open-dashboard message | pass (VM test) |
| Real permission denial guidance | pass |
| Console/page errors | none |
| Real FCM/Mozilla/Apple subscription | **not run** |
| Closed-browser delivery through a real Push service | **not run** |
| OS notification click | **not run** |

## Limitations

- `ServiceWorker.deliverPushMessage` is a browser simulation of a Push event; it
  does not prove end-to-end delivery through FCM/Mozilla/Apple, VAPID acceptance
  by a live Push service, or behavior with the browser fully closed.
- A real subscription would contact an external Push service and may create a
  real device subscription. That was not authorized in this session.
- The click path is covered by executing the actual service-worker source with
  controlled `clients`/notification objects; it was not clicked through the OS
  notification center.
- The test password was temporary and is not recorded here. No secret material
  is included in this document.
