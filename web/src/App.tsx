import { Component, lazy, Suspense, type ErrorInfo, type ReactNode } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { Toaster } from "sonner";
import { AuthProvider } from "@/auth/AuthProvider";
import { LoginRoute, ProtectedRoute } from "@/auth/routes";
import { AppLayout } from "@/components/layout/AppLayout";
import { LoadingState } from "@/components/layout/LoadingState";
import { Button } from "@/components/ui/button";
import { ThemeProvider, useTheme } from "@/lib/theme";

const LoginPage = lazy(() =>
  import("@/pages/LoginPage").then((module) => ({ default: module.LoginPage })),
);
const OverviewPage = lazy(() =>
  import("@/pages/OverviewPage").then((module) => ({
    default: module.OverviewPage,
  })),
);
const MonitorListPage = lazy(() =>
  import("@/pages/MonitorListPage").then((module) => ({
    default: module.MonitorListPage,
  })),
);
const MonitorCreatePage = lazy(() =>
  import("@/pages/MonitorCreatePage").then((module) => ({
    default: module.MonitorCreatePage,
  })),
);
const MonitorDetailPage = lazy(() =>
  import("@/pages/MonitorDetailPage").then((module) => ({
    default: module.MonitorDetailPage,
  })),
);
const MonitorEditPage = lazy(() =>
  import("@/pages/MonitorEditPage").then((module) => ({
    default: module.MonitorEditPage,
  })),
);
const HistoryPage = lazy(() =>
  import("@/pages/HistoryPage").then((module) => ({
    default: module.HistoryPage,
  })),
);
const EventDetailPage = lazy(() =>
  import("@/pages/EventDetailPage").then((module) => ({
    default: module.EventDetailPage,
  })),
);
const SettingsPage = lazy(() =>
  import("@/pages/SettingsPage").then((module) => ({
    default: module.SettingsPage,
  })),
);
const NotFoundPage = lazy(() =>
  import("@/pages/NotFoundPage").then((module) => ({
    default: module.NotFoundPage,
  })),
);

function RouteFallback() {
  return (
    <div className="mx-auto w-full max-w-6xl px-4 py-8 sm:px-6 lg:px-8">
      <LoadingState rows={3} label="Loading page" />
    </div>
  );
}

class AppErrorBoundary extends Component<
  { children: ReactNode },
  { failed: boolean }
> {
  state = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Keep browser logs free of tokens and server payloads.
    console.error("Neda UI error", error.name, info.componentStack);
  }

  render() {
    if (this.state.failed) {
      return (
        <div className="flex min-h-dvh items-center justify-center px-4">
          <div className="max-w-md space-y-3 text-center">
            <h1 className="text-lg font-semibold">The dashboard hit an error</h1>
            <p className="text-sm text-muted-foreground">
              Reload the page to continue. No monitoring data was changed.
            </p>
            <Button type="button" onClick={() => window.location.reload()}>
              Reload dashboard
            </Button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

function AppToaster() {
  const { resolvedTheme } = useTheme();
  return (
    <Toaster
      theme={resolvedTheme}
      position="top-right"
      closeButton
      richColors
    />
  );
}

export function App() {
  return (
    <AppErrorBoundary>
      <ThemeProvider>
        <AuthProvider>
          <BrowserRouter>
            <Suspense fallback={<RouteFallback />}>
              <Routes>
                <Route element={<LoginRoute />}>
                  <Route path="/login" element={<LoginPage />} />
                </Route>
                <Route element={<ProtectedRoute />}>
                  <Route element={<AppLayout />}>
                    <Route index element={<OverviewPage />} />
                    <Route path="monitors" element={<MonitorListPage />} />
                    <Route path="monitors/new" element={<MonitorCreatePage />} />
                    <Route path="monitors/:id" element={<MonitorDetailPage />} />
                    <Route
                      path="monitors/:id/edit"
                      element={<MonitorEditPage />}
                    />
                    <Route path="events" element={<HistoryPage />} />
                    <Route path="events/:id" element={<EventDetailPage />} />
                    <Route path="settings" element={<SettingsPage />} />
                    <Route path="*" element={<NotFoundPage />} />
                  </Route>
                </Route>
              </Routes>
            </Suspense>
          </BrowserRouter>
          <AppToaster />
        </AuthProvider>
      </ThemeProvider>
    </AppErrorBoundary>
  );
}
