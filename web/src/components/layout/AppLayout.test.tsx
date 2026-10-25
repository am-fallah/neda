import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes, useNavigate } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AppLayout } from "./AppLayout";

vi.mock("@/auth/AuthProvider", () => ({
  useAuth: () => ({
    logout: vi.fn(),
    expiresAt: null,
  }),
}));

vi.mock("@/lib/theme", () => ({
  useTheme: () => ({
    resolvedTheme: "light",
    toggleTheme: vi.fn(),
  }),
}));

function NewMonitorPage() {
  const navigate = useNavigate();
  return (
    <button type="button" onClick={() => navigate("/monitors")}>
      Back to monitors
    </button>
  );
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route element={<AppLayout />}>
          <Route path="/monitors/new" element={<NewMonitorPage />} />
          <Route path="/monitors" element={<div>Monitor list</div>} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
}

describe("AppLayout New Monitor palette scope", () => {
  beforeEach(() => {
    vi.spyOn(window, "scrollTo").mockImplementation(() => undefined);
  });

  afterEach(() => {
    document.body.className = "";
    vi.restoreAllMocks();
  });

  it("applies the page class only while the New Monitor route is mounted",() => {
    renderAt("/monitors/new");
    expect(document.body).toHaveClass("monitor-create-page");
  });

  it("applies the page class with a trailing slash", () => {
    renderAt("/monitors/new/");
    expect(document.body).toHaveClass("monitor-create-page");
  });

  it("removes the page class after navigating away", async () => {
    const user = userEvent.setup();
    renderAt("/monitors/new");

    await user.click(
      screen.getByRole("button", { name: /back to monitors/i }),
    );

    await waitFor(() =>
      expect(document.body).not.toHaveClass("monitor-create-page"),
    );
  });

  it("removes the page class on unmount", () => {
    const view = renderAt("/monitors/new");
    expect(document.body).toHaveClass("monitor-create-page");
    view.unmount();
    expect(document.body).not.toHaveClass("monitor-create-page");
  });

  it("does not apply the page class on other routes", () => {
    renderAt("/monitors");
    expect(document.body).not.toHaveClass("monitor-create-page");
  });
});
