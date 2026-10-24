import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { pushApi, settingsApi } from "@/api/endpoints";
import type { PushConfig } from "@/api/types";
import { makeSettings } from "@/test/fixtures";
import {
  PushPermissionError,
  isPushSupported,
  pushSupportMessage,
  subscribeThisBrowser,
  unsubscribeThisBrowser,
} from "@/lib/push";
import { PushSettings } from "./PushSettings";

vi.mock("@/api/endpoints", () => ({
  pushApi: {
    config: vi.fn(),
    subscribe: vi.fn(),
    unsubscribe: vi.fn(),
    test: vi.fn(),
  },
  settingsApi: { update: vi.fn() },
}));

vi.mock("@/lib/push", () => {
  class MockPushPermissionError extends Error {
    permission: NotificationPermission;
    constructor(permission: NotificationPermission) {
      super(
        permission === "denied"
          ? "Browser notification permission is blocked. Allow notifications for this site and try again."
          : "Browser notification permission was not granted.",
      );
      this.permission = permission;
    }
  }
  return {
    PushPermissionError: MockPushPermissionError,
    browserLabel: () => "Chrome",
    isPushSupported: vi.fn(() => true),
    pushSupportMessage: vi.fn(() => ""),
    subscribeThisBrowser: vi.fn(),
    unsubscribeThisBrowser: vi.fn(),
  };
});

const configMock = vi.mocked(pushApi.config);
const subscribeMock = vi.mocked(pushApi.subscribe);
const unsubscribeMock = vi.mocked(pushApi.unsubscribe);
const testMock = vi.mocked(pushApi.test);
const updateMock = vi.mocked(settingsApi.update);
const supportedMock = vi.mocked(isPushSupported);
const supportMessageMock = vi.mocked(pushSupportMessage);
const subscribeBrowserMock = vi.mocked(subscribeThisBrowser);
const unsubscribeBrowserMock = vi.mocked(unsubscribeThisBrowser);

const baseConfig: PushConfig = {
  enabled: true,
  available: true,
  subjectConfigured: true,
  publicKey: "public-key",
  subscriptions: [
    {
      id: "sub-1",
      label: "Laptop",
      createdAt: "2026-10-24T12:00:00Z",
      lastSuccessAt: null,
      failureCount: 0,
    },
  ],
};

function renderSettings(overrides?: Partial<PushConfig>) {
  if (overrides) configMock.mockResolvedValue({ ...baseConfig, ...overrides });
  const onUpdated = vi.fn();
  render(
    <PushSettings
      settings={makeSettings({ pushEnabled: true })}
      onUpdated={onUpdated}
    />,
  );
  return { onUpdated };
}

describe("PushSettings", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal("Notification", { permission: "granted" });
    supportedMock.mockReturnValue(true);
    supportMessageMock.mockReturnValue("");
    configMock.mockResolvedValue(baseConfig);
    subscribeMock.mockResolvedValue(baseConfig.subscriptions[0]);
    unsubscribeMock.mockResolvedValue(undefined);
    testMock.mockResolvedValue({ attempted: 1, sent: 1, failed: 0 });
    updateMock.mockResolvedValue(makeSettings({ pushEnabled: true }));
    subscribeBrowserMock.mockResolvedValue({
      endpoint: "https://fcm.googleapis.com/fcm/send/abc",
      keys: { p256dh: "p256dh", auth: "auth" },
      label: "Chrome",
    });
    unsubscribeBrowserMock.mockResolvedValue(true);
  });

  it("lists devices and removes one", async () => {
    const user = userEvent.setup();
    renderSettings();
    expect(await screen.findByText("Laptop")).toBeInTheDocument();
    expect(screen.getByText("Delivery enabled")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(unsubscribeMock).toHaveBeenCalledWith("sub-1"));
  });

  it("enables this browser and persists the global switch", async () => {
    const user = userEvent.setup();
    const { onUpdated } = renderSettings({ subscriptions: [] });
    await screen.findByRole("button", { name: "Enable this browser" });
    await user.click(
      screen.getByRole("button", { name: "Enable this browser" }),
    );
    await waitFor(() => expect(subscribeBrowserMock).toHaveBeenCalledWith(
      "public-key",
      "Chrome",
    ));
    expect(subscribeMock).toHaveBeenCalledWith({
      endpoint: "https://fcm.googleapis.com/fcm/send/abc",
      keys: { p256dh: "p256dh", auth: "auth" },
      label: "Chrome",
    });
    expect(updateMock).toHaveBeenCalledWith({ pushEnabled: true });
    expect(onUpdated).toHaveBeenCalled();
  });

  it("shows permission denial guidance without saving", async () => {
    const user = userEvent.setup();
    subscribeBrowserMock.mockRejectedValue(
      new PushPermissionError("denied"),
    );
    renderSettings({ subscriptions: [] });
    await user.click(
      await screen.findByRole("button", { name: "Enable this browser" }),
    );
    expect(
      await screen.findByText(/Browser notification permission is blocked/),
    ).toBeInTheDocument();
    expect(subscribeMock).not.toHaveBeenCalled();
    expect(updateMock).not.toHaveBeenCalled();
  });

  it("sends a per-device test and reports sanitized failures", async () => {
    testMock.mockResolvedValue({
      attempted: 1,
      sent: 0,
      failed: 1,
      errorKinds: ["push_gone"],
    });
    const user = userEvent.setup();
    renderSettings();
    await user.click(await screen.findByRole("button", { name: "Test" }));
    await waitFor(() => expect(testMock).toHaveBeenCalledWith("sub-1"));
  });

  it("shows server guidance when Push is unavailable", async () => {
    configMock.mockResolvedValue({
      enabled: false,
      available: false,
      subjectConfigured: false,
      subscriptions: [],
    });
    renderSettings();
    expect(
      await screen.findByText("Push is not configured on the server"),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Enable this browser" }),
    ).toBeDisabled();
  });

  it("shows unsupported-browser guidance", async () => {
    supportedMock.mockReturnValue(false);
    supportMessageMock.mockReturnValue("This browser does not support Web Push.");
    renderSettings();
    expect(
      await screen.findByText("This browser cannot use Web Push"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("This browser does not support Web Push."),
    ).toBeInTheDocument();
  });
});
