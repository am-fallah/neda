import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { providersApi, pushApi, settingsApi } from "@/api/endpoints";
import { makeProvider, makeSettings } from "@/test/fixtures";
import { MonitorCreatePage } from "./MonitorCreatePage";

vi.mock("@/api/endpoints", () => ({
  monitorsApi: { create: vi.fn() },
  providersApi: { list: vi.fn() },
  pushApi: { config: vi.fn() },
  settingsApi: { get: vi.fn() },
}));

const providersMock = vi.mocked(providersApi.list);
const settingsMock = vi.mocked(settingsApi.get);
const pushMock = vi.mocked(pushApi.config);

const mrbilit = makeProvider({
  id: "mrbilit",
  displayName: "MrBilit",
  testOnly: false,
  capabilities: {
    requiresOriginId: true,
    requiresDestinationId: true,
    supportsBookingUrl: true,
    supportsGender: false,
    requiresOptionSelection: true,
    maxPassengers: 9,
  },
});

const pushConfig = {
  enabled: true,
  available: true,
  subjectConfigured: true,
  subscriptions: [],
};

function renderPage() {
  return render(
    <MemoryRouter>
      <MonitorCreatePage />
    </MemoryRouter>,
  );
}

describe("MonitorCreatePage", () => {
  beforeEach(() => {
    providersMock.mockReset();
    settingsMock.mockReset();
    pushMock.mockReset();
    providersMock.mockResolvedValue({ providers: [mrbilit] });
    settingsMock.mockResolvedValue(makeSettings());
    pushMock.mockResolvedValue(pushConfig);
  });

  it("shows a matching setup skeleton before the flow loads", async () => {
    renderPage();
    expect(
      screen.getByRole("status", { name: /loading monitor setup/i }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole("heading", { name: /search mrbilit/i }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/loading monitor setup/i)).not.toBeInTheDocument();
  });

  it("surfaces setup failures with a retry", async () => {
    providersMock.mockRejectedValue(new Error("providers unavailable"));
    renderPage();

    expect(
      await screen.findByText("Could not load monitor setup"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /try again/i })).toBeInTheDocument();
  });

  it("keeps the local test-provider path behind a disclosure", async () => {
    providersMock.mockResolvedValue({
      providers: [mrbilit, makeProvider()],
    });
    renderPage();

    expect(
      await screen.findByText(/local test-provider flow/i),
    ).toBeInTheDocument();
    expect(screen.getAllByText(/^Test-only$/i).length).toBeGreaterThan(0);
  });

  it("shows a clear fallback when MrBilit is not registered", async () => {
    providersMock.mockResolvedValue({ providers: [] });
    renderPage();

    expect(
      await screen.findByText(/MrBilit is not available in this build/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/No providers available/i),
    ).toBeInTheDocument();
  });
});
