import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom";
import {
  ChevronDown,
  History,
  LayoutDashboard,
  LogOut,
  Menu,
  Moon,
  Plus,
  Settings,
  Sun,
  TrainFront,
  UserRound,
  X,
} from "lucide-react";
import { useAuth } from "@/auth/AuthProvider";
import { useTheme } from "@/lib/theme";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

const NAV_GROUPS = [
  {
    label: "Workspace",
    items: [
      { to: "/", label: "Overview", icon: LayoutDashboard, end: true },
      { to: "/monitors", label: "Monitors", icon: TrainFront },
      { to: "/events", label: "History", icon: History },
    ],
  },
  {
    label: "System",
    items: [{ to: "/settings", label: "Settings", icon: Settings }],
  },
] as const;

function BrandMark({ compact = false }: { compact?: boolean }) {
  return (
    <div className="flex items-center gap-2.5">
      <span
        aria-hidden="true"
        className={cn(
          "relative flex items-center justify-center rounded-lg bg-primary font-mono text-primary-foreground",
          compact ? "size-8 text-xs" : "size-9 text-sm",
        )}
      >
        <span className="relative -translate-y-px font-semibold">N</span>
        <span className="absolute inset-x-2 bottom-1 h-px bg-primary-foreground/35" />
      </span>
      <div className="min-w-0 leading-tight">
        <span className="block text-sm font-semibold tracking-[-0.01em]">
          Neda
        </span>
        <span className="eyebrow hidden text-[0.58rem] tracking-[0.14em] min-[360px]:block">
          Train operations
        </span>
      </div>
    </div>
  );
}

function RailLine() {
  return <span aria-hidden="true" className="brand-rule h-px w-full" />;
}

function ThemeToggle() {
  const { resolvedTheme, toggleTheme } = useTheme();
  const next = resolvedTheme === "dark" ? "light" : "dark";
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      onClick={toggleTheme}
      aria-label={`Switch to ${next} theme`}
      title={`Switch to ${next} theme`}
    >
      {resolvedTheme === "dark" ? (
        <Sun aria-hidden="true" />
      ) : (
        <Moon aria-hidden="true" />
      )}
    </Button>
  );
}

function AccountMenu() {
  const { logout, expiresAt } = useAuth();
  const navigate = useNavigate();
  const [busy, setBusy] = useState(false);

  async function handleLogout() {
    setBusy(true);
    try {
      await logout();
      navigate("/login", { replace: true });
    } finally {
      setBusy(false);
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size="sm"
          className="gap-1.5 px-2"
          title="Account menu"
        >
          <span
            className="flex size-6 items-center justify-center rounded-full bg-primary/10 text-primary"
            aria-hidden="true"
          >
            <UserRound className="size-3.5" />
          </span>
          <span className="hidden sm:inline">Owner</span>
          <ChevronDown aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuLabel>
          <span className="block text-sm font-medium">Owner</span>
          <span className="block text-xs font-normal text-muted-foreground">
            {expiresAt
              ? `Session expires ${new Date(expiresAt).toLocaleString("en-GB", {
                  timeZone: "Asia/Tehran",
                })} Tehran`
              : "Private single-user session"}
          </span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem
          onSelect={() => void handleLogout()}
          disabled={busy}
          className="gap-2"
        >
          <LogOut aria-hidden="true" />
          {busy ? "Signing out…" : "Sign out"}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function NavigationLinks({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <nav aria-label="Primary" className="space-y-5">
      {NAV_GROUPS.map((group) => (
        <div key={group.label} className="space-y-1">
          <p className="eyebrow px-2.5 pb-1 text-[0.6rem]">{group.label}</p>
          {group.items.map((item) => {
            const Icon = item.icon;
            return (
              <NavLink
                key={item.to}
                to={item.to}
                end={"end" in item ? item.end : false}
                onClick={onNavigate}
                className={({ isActive }) =>
                  cn(
                    "relative flex min-h-10 items-center gap-2.5 rounded-md px-2.5 text-[0.8125rem] font-medium transition-[background-color,color] duration-150 max-sm:min-h-11 motion-reduce:transition-none",
                    isActive
                      ? "bg-accent text-accent-foreground before:absolute before:inset-y-2 before:left-0 before:w-0.5 before:rounded-full before:bg-primary"
                      : "text-muted-foreground hover:bg-muted/70 hover:text-foreground",
                  )
                }
              >
                <Icon
                  className="size-4 shrink-0"
                  strokeWidth={1.9}
                  aria-hidden="true"
                />
                {item.label}
              </NavLink>
            );
          })}
        </div>
      ))}
    </nav>
  );
}

function SidebarFooter() {
  return (
    <div className="space-y-3 border-t pt-4">
      <div className="flex items-center justify-between gap-2 px-1">
        <ThemeToggle />
        <AccountMenu />
      </div>
      <div className="space-y-1.5 px-1">
        <p className="flex items-center gap-2 text-[0.7rem] font-medium text-foreground">
          <span className="status-dot" aria-hidden="true" />
          Server-side monitoring
        </p>
        <p className="text-[0.68rem] leading-relaxed text-muted-foreground">
          Private workspace. Monitoring continues while this browser is closed.
        </p>
      </div>
    </div>
  );
}

export function AppLayout() {
  const [mobileOpen, setMobileOpen] = useState(false);
  const location = useLocation();
  const mainRef = useRef<HTMLElement>(null);
  const mobileTriggerRef = useRef<HTMLButtonElement>(null);
  const navigatedFromDrawer = useRef(false);
  const previousPath = useRef(location.pathname);
  const newMonitorPage =
    location.pathname.replace(/\/+$/, "") === "/monitors/new";

  // Approved route-scoped neutral workflow (WEB-010..WEB-013). Portaled
  // overlays inherit from body, so the whole shell stays visually consistent.
  useLayoutEffect(() => {
    if (typeof document === "undefined") return;
    const className = "monitor-create-page";
    document.body.classList.toggle(className, newMonitorPage);
    return () => document.body.classList.remove(className);
  }, [newMonitorPage]);

  useEffect(() => {
    if (previousPath.current !== location.pathname) {
      previousPath.current = location.pathname;
      window.scrollTo({ top: 0, behavior: "auto" });
      mainRef.current?.focus({ preventScroll: true });
    }
  }, [location.pathname]);

  useEffect(() => {
    setMobileOpen(false);
  }, [location.pathname]);

  function closeDrawerAndNavigate() {
    navigatedFromDrawer.current = true;
    setMobileOpen(false);
  }

  return (
    <div className="min-h-dvh">
      <a
        href="#main-content"
        className="sr-only z-50 rounded-md bg-primary px-4 py-2 text-primary-foreground focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:ring-[3px] focus:ring-ring/40 focus:outline-none"
      >
        Skip to main content
      </a>

      <header className="sticky top-0 z-30 border-b bg-background/95 backdrop-blur lg:hidden">
        <div className="flex h-16 items-center justify-between gap-2 px-4">
          <div className="flex min-w-0 items-center gap-2">
            <button
              ref={mobileTriggerRef}
              type="button"
              onClick={() => setMobileOpen(true)}
              aria-expanded={mobileOpen}
              aria-controls="mobile-navigation"
              aria-haspopup="dialog"
              aria-label="Open navigation"
              className="inline-flex size-11 items-center justify-center rounded-lg text-foreground transition-colors hover:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/40 focus-visible:outline-none"
            >
              <Menu className="size-5" aria-hidden="true" />
            </button>
            <BrandMark compact />
          </div>
          <div className="flex items-center gap-1">
            <ThemeToggle />
            <AccountMenu />
          </div>
        </div>
      </header>

      <Dialog open={mobileOpen} onOpenChange={setMobileOpen}>
        <DialogContent
          showCloseButton={false}
          onCloseAutoFocus={(event) => {
            event.preventDefault();
            if (navigatedFromDrawer.current) {
              navigatedFromDrawer.current = false;
              mainRef.current?.focus({ preventScroll: true });
            } else {
              mobileTriggerRef.current?.focus();
            }
          }}
          className="fixed inset-y-0 left-0 top-0 h-dvh w-[min(20rem,88vw)] max-w-none translate-x-0 translate-y-0 gap-0 rounded-none border-y-0 border-l-0 bg-card p-0 shadow-[var(--shadow-overlay)] sm:max-w-none"
        >
          <DialogTitle className="sr-only">Navigation</DialogTitle>
          <DialogDescription className="sr-only">
            Neda workspace navigation and account controls.
          </DialogDescription>
          <div
            id="mobile-navigation"
            className="flex h-full min-h-0 flex-col px-4 py-5"
          >
            <div className="flex items-center justify-between gap-2">
              <BrandMark />
              <Button
                type="button"
                variant="ghost"
                size="icon"
                onClick={() => setMobileOpen(false)}
                aria-label="Close navigation"
              >
                <X aria-hidden="true" />
              </Button>
            </div>
            <div className="mt-6">
              <RailLine />
            </div>
            <Button asChild className="mt-4 w-full justify-start">
              <NavLink
                to="/monitors/new"
                onClick={closeDrawerAndNavigate}
              >
                <Plus aria-hidden="true" />
                New monitor
              </NavLink>
            </Button>
            <div className="mt-6 flex-1 overflow-y-auto">
              <NavigationLinks onNavigate={closeDrawerAndNavigate} />
            </div>
            <SidebarFooter />
          </div>
        </DialogContent>
      </Dialog>

      <div className="lg:flex">
        <aside
          aria-label="Workspace sidebar"
          className="fixed inset-y-0 left-0 z-20 hidden w-60 flex-col border-r bg-card/70 px-4 py-5 lg:flex"
        >
          <div className="px-1.5">
            <BrandMark />
          </div>
          <div className="mt-5 px-1.5">
            <RailLine />
          </div>
          <Button asChild variant="outline" className="mt-4 w-full justify-start">
            <NavLink to="/monitors/new">
              <Plus aria-hidden="true" />
              New monitor
            </NavLink>
          </Button>
          <div className="mt-7 flex-1 overflow-y-auto">
            <NavigationLinks />
          </div>
          <SidebarFooter />
        </aside>

        <div className="min-w-0 flex-1 lg:pl-60">
          <main
            id="main-content"
            ref={mainRef}
            tabIndex={-1}
            className="mx-auto w-full max-w-[80rem] px-4 py-6 outline-none sm:px-6 lg:px-8 lg:py-8"
          >
            <Outlet />
          </main>
        </div>
      </div>
    </div>
  );
}
