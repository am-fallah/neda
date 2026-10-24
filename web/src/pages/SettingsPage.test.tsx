import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { authApi, pushApi, settingsApi } from "@/api/endpoints";
import { ThemeProvider } from "@/lib/theme";
import { makeSettings } from "@/test/fixtures";
import { SettingsPage } from "./SettingsPage";

vi.mock("@/api/endpoints", () => ({
  settingsApi: {
    get: vi.fn(),
    update: vi.fn(),
    updateTelegram: vi.fn(),
    testTelegram: vi.fn(),
  },
  authApi: { changePassword: vi.fn() },
  pushApi: {
    config: vi.fn(),
    subscribe: vi.fn(),
    unsubscribe: vi.fn(),
    test: vi.fn(),
  },
}));

const pushConfigMock = vi.mocked(pushApi.config);

const getMock = vi.mocked(settingsApi.get);
const updateMock = vi.mocked(settingsApi.update);
const updateTelegramMock = vi.mocked(settingsApi.updateTelegram);
const testTelegramMock = vi.mocked(settingsApi.testTelegram);
const changePasswordMock = vi.mocked(authApi.changePassword);

function renderPage() {
  return render(
    <ThemeProvider>
      <SettingsPage />
    </ThemeProvider>,
  );
}

describe("SettingsPage", () => {
  beforeEach(() => {
    getMock.mockReset();
    updateMock.mockReset();
    updateTelegramMock.mockReset();
    testTelegramMock.mockReset();
    changePasswordMock.mockReset();
    pushConfigMock.mockReset();
    pushConfigMock.mockResolvedValue({
      enabled: false,
      available: true,
      subjectConfigured: true,
      publicKey: "test-public-key",
      subscriptions: [],
    });
    getMock.mockResolvedValue(makeSettings());
  });

  it("never prefills or displays stored Telegram secrets", async () => {
    const settingsWithUnexpectedSecret = makeSettings() as ReturnType<
      typeof makeSettings
    > & { token?: string };
    settingsWithUnexpectedSecret.token = "super-secret-token";
    getMock.mockResolvedValue(settingsWithUnexpectedSecret);
    renderPage();

    const token = await screen.findByLabelText("Bot token");
    const chat = screen.getByLabelText(/Chat ID or @username/);
    expect(token).toHaveValue("");
    expect(chat).toHaveValue("");
    expect(token).toHaveAttribute("type", "password");
    expect(screen.queryByText("super-secret-token")).not.toBeInTheDocument();
    expect(screen.getByText(/Chat: Chat •••• 1234/)).toBeInTheDocument();
  });

  it("requires both Telegram fields before saving", async () => {
    const user = userEvent.setup();
    renderPage();
    await screen.findByLabelText("Bot token");
    await user.click(
      screen.getByRole("button", { name: "Save Telegram credentials" }),
    );
    expect(screen.getByText("Enter the bot token.")).toBeInTheDocument();
    expect(
      screen.getByText("Enter the chat ID or @username."),
    ).toBeInTheDocument();
    expect(updateTelegramMock).not.toHaveBeenCalled();
  });

  it("saves Telegram credentials and clears the write-only fields", async () => {
    updateTelegramMock.mockResolvedValue(makeSettings());
    const user = userEvent.setup();
    renderPage();
    const token = await screen.findByLabelText("Bot token");
    const chat = screen.getByLabelText(/Chat ID or @username/);
    await user.type(token, "123456:secret-token");
    await user.type(chat, "987654321");
    await user.click(
      screen.getByRole("button", { name: "Save Telegram credentials" }),
    );
    await waitFor(() =>
      expect(updateTelegramMock).toHaveBeenCalledWith({
        token: "123456:secret-token",
        chatId: "987654321",
      }),
    );
    await waitFor(() => expect(token).toHaveValue(""));
    expect(chat).toHaveValue("");
  });

  it("runs an explicit Telegram test and shows the sanitized result", async () => {
    testTelegramMock.mockResolvedValue(
      makeSettings({
        telegram: {
          available: true,
          configured: true,
          readable: true,
          chatLabel: "Chat •••• 1234",
          lastTestAt: "2026-10-24T12:00:00Z",
          lastTestOk: false,
          lastErrorKind: "unauthorized",
        },
      }),
    );
    const user = userEvent.setup();
    renderPage();
    await screen.findByLabelText("Bot token");
    await user.click(screen.getByRole("button", { name: "Send test message" }));
    expect(testTelegramMock).toHaveBeenCalledOnce();
    expect(await screen.findByText("Failed (unauthorized)")).toBeInTheDocument();
  });

  it("clears Telegram credentials only after confirmation", async () => {
    updateTelegramMock.mockResolvedValue(
      makeSettings({
        telegram: {
          available: true,
          configured: false,
          readable: true,
          lastTestAt: null,
          lastTestOk: null,
        },
      }),
    );
    const user = userEvent.setup();
    renderPage();
    await screen.findByLabelText("Bot token");
    await user.click(screen.getByRole("button", { name: "Clear credentials" }));
    const dialog = screen.getByRole("alertdialog");
    expect(updateTelegramMock).not.toHaveBeenCalled();
    await user.click(
      within(dialog).getByRole("button", { name: "Clear credentials" }),
    );
    await waitFor(() =>
      expect(updateTelegramMock).toHaveBeenCalledWith({ clear: true }),
    );
    expect(
      await screen.findByText("Telegram is not configured"),
    ).toBeInTheDocument();
  });

  it("validates and saves the polling interval", async () => {
    updateMock.mockResolvedValue(
      makeSettings({ pollIntervalSeconds: 600 }),
    );
    const user = userEvent.setup();
    renderPage();
    const interval = await screen.findByLabelText("Polling interval (seconds)");
    expect(interval).toHaveValue(300);

    await user.clear(interval);
    await user.type(interval, "30");
    await user.click(screen.getByRole("button", { name: "Save interval" }));
    expect(
      screen.getByText("Enter a whole number between 60 and 3600 seconds."),
    ).toBeInTheDocument();
    expect(updateMock).not.toHaveBeenCalled();

    await user.clear(interval);
    await user.type(interval, "600");
    await user.click(screen.getByRole("button", { name: "Save interval" }));
    await waitFor(() =>
      expect(updateMock).toHaveBeenCalledWith({ pollIntervalSeconds: 600 }),
    );
  });

  it("changes the password with client confirmation", async () => {
    changePasswordMock.mockResolvedValue(undefined);
    const user = userEvent.setup();
    renderPage();
    await screen.findByLabelText("Current password");
    await user.type(screen.getByLabelText("Current password"), "old password 123");
    await user.type(screen.getByLabelText("New password"), "new password 456");
    await user.type(
      screen.getByLabelText("Confirm new password"),
      "different password",
    );
    await user.click(screen.getByRole("button", { name: "Change password" }));
    expect(screen.getByText("Passwords do not match.")).toBeInTheDocument();
    expect(changePasswordMock).not.toHaveBeenCalled();

    await user.clear(screen.getByLabelText("Confirm new password"));
    await user.type(
      screen.getByLabelText("Confirm new password"),
      "new password 456",
    );
    await user.click(screen.getByRole("button", { name: "Change password" }));
    await waitFor(() =>
      expect(changePasswordMock).toHaveBeenCalledWith(
        "old password 123",
        "new password 456",
      ),
    );
  });
});
