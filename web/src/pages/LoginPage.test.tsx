import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/client";
import { useAuth } from "@/auth/AuthProvider";
import { LoginPage } from "./LoginPage";

vi.mock("@/auth/AuthProvider", () => ({
  useAuth: vi.fn(),
}));

const useAuthMock = vi.mocked(useAuth);

function auth(overrides: Partial<ReturnType<typeof useAuth>> = {}) {
  return {
    status: "anonymous" as const,
    setupRequired: false,
    expiresAt: null,
    error: null,
    login: vi.fn().mockResolvedValue(undefined),
    logout: vi.fn().mockResolvedValue(undefined),
    refresh: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

function renderLogin() {
  return render(
    <MemoryRouter
      initialEntries={[
        { pathname: "/login", state: { from: "/monitors" } },
      ]}
    >
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/monitors" element={<div>Monitors destination</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("LoginPage", () => {
  beforeEach(() => {
    useAuthMock.mockReturnValue(auth());
  });

  it("submits the password and navigates to the intended page", async () => {
    const login = vi.fn().mockResolvedValue(undefined);
    useAuthMock.mockReturnValue(auth({ login }));
    const user = userEvent.setup();
    renderLogin();

    const field = screen.getByLabelText("Password");
    expect(field).toHaveAttribute("type", "password");
    expect(field).toHaveAttribute("autocomplete", "current-password");
    await user.type(field, "correct horse battery staple");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(login).toHaveBeenCalledWith("correct horse battery staple");
    expect(await screen.findByText("Monitors destination")).toBeInTheDocument();
  });

  it("shows a generic field error for invalid credentials", async () => {
    const login = vi.fn().mockRejectedValue(
      new ApiError({
        status: 401,
        code: "invalid_credentials",
        message: "invalid credentials",
      }),
    );
    useAuthMock.mockReturnValue(auth({ login }));
    const user = userEvent.setup();
    renderLogin();

    await user.type(screen.getByLabelText("Password"), "wrong password");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(await screen.findByText("Incorrect password.")).toBeInTheDocument();
  });

  it("explains rate limiting with the retry delay", async () => {
    const login = vi.fn().mockRejectedValue(
      new ApiError({
        status: 429,
        code: "rate_limited",
        message: "too many login attempts",
        retryAfterSeconds: 30,
      }),
    );
    useAuthMock.mockReturnValue(auth({ login }));
    const user = userEvent.setup();
    renderLogin();

    await user.type(screen.getByLabelText("Password"), "wrong password");
    await user.click(screen.getByRole("button", { name: "Sign in" }));

    expect(
      await screen.findByText(/Too many sign-in attempts/),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Try again in 30s/ })).toBeDisabled();
  });

  it("shows setup-required guidance without a login form", () => {
    useAuthMock.mockReturnValue(auth({ setupRequired: true }));
    renderLogin();
    expect(
      screen.getByText("Initial password setup required"),
    ).toBeInTheDocument();
    expect(screen.getByText("neda set-password")).toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  });

  it("surfaces a session-probe failure and offers a retry", async () => {
    const refresh = vi.fn().mockResolvedValue(undefined);
    useAuthMock.mockReturnValue(
      auth({
        status: "error",
        error: new ApiError({
          status: 0,
          code: "network_error",
          message: "Unable to reach the Neda server. Check your connection.",
        }),
        refresh,
      }),
    );
    const user = userEvent.setup();
    renderLogin();
    expect(screen.getByText("Neda is unavailable")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Try again/ }));
    expect(refresh).toHaveBeenCalledOnce();
  });
});
