import { render, screen } from "@testing-library/react";
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

describe("AppLayout", () => {
  beforeEach(() => {
    vi.spyOn(window, "scrollTo").mockImplementation(() => undefined);
  });

  afterEach(() => {
    document.body.className = "";
    vi.restoreAllMocks();
  });

  it("renders the shared navigation without route-scoped palette classes", () => {
    renderAt("/monitors/new");
    expect(
      screen.getByRole("navigation", { name: "Primary" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Monitors" })).toBeInTheDocument();
    expect(document.body).not.toHaveClass("monitor-create-page");
  });

  it("keeps the shared shell when navigating between routes", async () => {
    const user = userEvent.setup();
    renderAt("/monitors/new");

    await user.click(
      screen.getByRole("button", { name: /back to monitors/i }),
    );

    expect(await screen.findByText("Monitor list")).toBeInTheDocument();
    expect(
      screen.getByRole("navigation", { name: "Primary" }),
    ).toBeInTheDocument();
    expect(document.body).not.toHaveClass("monitor-create-page");
  });
});
