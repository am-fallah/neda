import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import { describe, expect, it, vi } from "vitest";

const source = readFileSync("public/sw.js", "utf8");

interface ServiceWorkerEvent {
  waitUntil?: (promise: Promise<unknown>) => void;
  data?: { json: () => unknown };
  notification?: {
    close: () => void;
    data?: { url?: string };
  };
  newSubscription?: { toJSON: () => unknown } | null;
}

type Listener = (event: ServiceWorkerEvent) => void;

interface Sandbox {
  listeners: Record<string, Listener>;
  showNotification: ReturnType<typeof vi.fn>;
  openWindow: ReturnType<typeof vi.fn>;
  matchAll: ReturnType<typeof vi.fn>;
  skipWaiting: ReturnType<typeof vi.fn>;
  claim: ReturnType<typeof vi.fn>;
}

function loadServiceWorker(origin = "https://neda.example.com"): Sandbox {
  const listeners: Record<string, Listener> = {};
  const showNotification = vi.fn().mockResolvedValue(undefined);
  const openWindow = vi.fn().mockResolvedValue(undefined);
  const matchAll = vi.fn().mockResolvedValue([]);
  const clients = {
    matchAll,
    openWindow,
    claim: vi.fn().mockResolvedValue(undefined),
  };
  const self = {
    location: { origin },
    registration: { showNotification },
    clients,
    addEventListener: (name: string, handler: Listener) => {
      listeners[name] = handler;
    },
    skipWaiting: vi.fn().mockResolvedValue(undefined),
  };
  runInNewContext(source, {
    self,
    clients,
    URL,
    console,
    setTimeout,
    clearTimeout,
  });
  return {
    listeners,
    showNotification,
    openWindow,
    matchAll,
    skipWaiting: self.skipWaiting,
    claim: clients.claim,
  };
}

function waitUntil() {
  let resolve!: () => void;
  const done = new Promise<void>((doneResolve) => {
    resolve = doneResolve;
  });
  return {
    waitUntil: (promise: Promise<unknown>) => {
      void promise.finally(resolve);
    },
    done,
  };
}

describe("service worker", () => {
  it("registers install/activate lifecycles", () => {
    const worker = loadServiceWorker();
    expect(worker.listeners.install).toBeTypeOf("function");
    expect(worker.listeners.activate).toBeTypeOf("function");
    const install = waitUntil();
    worker.listeners.install({ waitUntil: install.waitUntil });
    expect(worker.skipWaiting).toHaveBeenCalled();
    const activate = waitUntil();
    worker.listeners.activate({ waitUntil: activate.waitUntil });
    expect(worker.claim).toHaveBeenCalled();
  });

  it("shows a bounded notification with a safe same-origin target", async () => {
    const worker = loadServiceWorker();
    const event: ServiceWorkerEvent = {
      data: {
        json: () => ({
          title: "Ticket available",
          body: "Yazd → Mashhad",
          tag: "neda-event-1",
          url: "/events/1",
          eventId: "1",
          kind: "availability",
        }),
      },
    };
    const wait = waitUntil();
    worker.listeners.push({ ...event, waitUntil: wait.waitUntil });
    await wait.done;
    expect(worker.showNotification).toHaveBeenCalledWith(
      "Ticket available",
      expect.objectContaining({
        body: "Yazd → Mashhad",
        tag: "neda-event-1",
        data: {
          url: "https://neda.example.com/events/1",
          eventId: "1",
          kind: "availability",
        },
      }),
    );
  });

  it("falls back to /events for unsafe or unvalidated click URLs", async () => {
    const worker = loadServiceWorker();
    const event: ServiceWorkerEvent = {
      data: {
        json: () => ({
          title: "Unsafe",
          body: "test",
          url: "https://evil.example/steal",
        }),
      },
    };
    const wait = waitUntil();
    worker.listeners.push({ ...event, waitUntil: wait.waitUntil });
    await wait.done;
    expect(worker.showNotification).toHaveBeenCalledWith(
      "Unsafe",
      expect.objectContaining({
        data: expect.objectContaining({
          url: "https://neda.example.com/events",
        }),
      }),
    );
  });

  it("focuses an existing window and navigates to the notification target", async () => {
    const worker = loadServiceWorker();
    const client = {
      url: "https://neda.example.com/monitors",
      focus: vi.fn().mockResolvedValue(undefined),
      navigate: vi.fn().mockResolvedValue(undefined),
    };
    worker.matchAll.mockResolvedValue([client]);
    const event: ServiceWorkerEvent = {
      notification: {
        close: vi.fn(),
        data: { url: "https://mrbilit.com/trains/route?departureDate=1405-09-29" },
      },
    };
    const wait = waitUntil();
    worker.listeners.notificationclick({ ...event, waitUntil: wait.waitUntil });
    await wait.done;
    expect(event.notification?.close).toHaveBeenCalled();
    expect(client.focus).toHaveBeenCalled();
    expect(client.navigate).toHaveBeenCalledWith(
      "https://mrbilit.com/trains/route?departureDate=1405-09-29",
    );
    expect(worker.openWindow).not.toHaveBeenCalled();
  });

  it("opens a window when no dashboard client exists", async () => {
    const worker = loadServiceWorker();
    const event: ServiceWorkerEvent = {
      notification: { close: vi.fn(), data: { url: "/events/2" } },
    };
    const wait = waitUntil();
    worker.listeners.notificationclick({ ...event, waitUntil: wait.waitUntil });
    await wait.done;
    expect(worker.openWindow).toHaveBeenCalledWith(
      "https://neda.example.com/events/2",
    );
  });

  it("asks open dashboards to refresh a rotated subscription", async () => {
    const worker = loadServiceWorker();
    const client = { postMessage: vi.fn() };
    worker.matchAll.mockResolvedValue([client]);
    const subscription = {
      toJSON: () => ({
        endpoint: "https://fcm.googleapis.com/fcm/send/x",
        keys: { p256dh: "p", auth: "a" },
      }),
    };
    const event: ServiceWorkerEvent = { newSubscription: subscription };
    const wait = waitUntil();
    worker.listeners.pushsubscriptionchange({
      ...event,
      waitUntil: wait.waitUntil,
    });
    await wait.done;
    expect(client.postMessage).toHaveBeenCalledWith({
      type: "neda:push-subscription-change",
      subscription: {
        endpoint: "https://fcm.googleapis.com/fcm/send/x",
        keys: { p256dh: "p", auth: "a" },
      },
    });
  });
});
