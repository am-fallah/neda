import { useCallback } from "react";
import { AlertTriangle, FlaskConical } from "lucide-react";
import { Link, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { monitorsApi, providersApi, pushApi, settingsApi } from "@/api/endpoints";
import type {
  MonitorCreateRequest,
  ProviderInfo,
  PushConfig,
  Settings,
} from "@/api/types";
import { Breadcrumbs } from "@/components/layout/Breadcrumbs";
import { ErrorState } from "@/components/layout/ErrorState";
import { PageHeader } from "@/components/layout/PageHeader";
import { MonitorForm } from "@/components/monitors/MonitorForm";
import { TestOnlyBadge } from "@/components/status";
import { urlSearchFlowFor } from "@/providers/registry";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useApiResource } from "@/hooks/useApiResource";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";
import { emptyMonitorForm } from "@/lib/monitorForm";

interface CreatePageData {
  providers: ProviderInfo[];
  settings: Settings;
  pushConfig: PushConfig;
}

function MonitorCreateSkeleton() {
  return (
    <div
      className="flex flex-col gap-6"
      role="status"
      aria-live="polite"
      aria-label="Loading monitor setup"
    >
      <span className="sr-only">Loading monitor setup…</span>
      <div className="flex items-center gap-2">
        <Skeleton className="size-6 rounded-full" />
        <Skeleton className="h-3 w-16" />
        <Skeleton className="h-px flex-1" />
        <Skeleton className="size-6 rounded-full" />
        <Skeleton className="h-3 w-14" />
        <Skeleton className="h-px flex-1" />
        <Skeleton className="size-6 rounded-full" />
        <Skeleton className="h-3 w-16" />
      </div>
      <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_22rem] 2xl:grid-cols-[minmax(0,1fr)_24rem]">
        <div className="rounded-xl border bg-card p-5 sm:p-6">
          <div className="flex items-start gap-3">
            <Skeleton className="size-9 rounded-lg" />
            <div className="flex flex-1 flex-col gap-2">
              <Skeleton className="h-5 w-28" />
              <Skeleton className="h-3 w-80 max-w-full" />
            </div>
          </div>
          <div className="mt-6 flex flex-col gap-2">
            <Skeleton className="h-3 w-28" />
            <Skeleton className="h-11 w-full" />
            <Skeleton className="h-3 w-64 max-w-full" />
          </div>
          <div className="mt-5 flex items-center gap-3 rounded-xl border bg-muted/20 px-4 py-3">
            <Skeleton className="size-9 rounded-lg" />
            <div className="flex flex-1 flex-col gap-2">
              <Skeleton className="h-3.5 w-40" />
              <Skeleton className="h-3 w-56 max-w-full" />
            </div>
            <Skeleton className="h-8 w-28 rounded-md" />
          </div>
          <div className="mt-5 flex justify-end border-t pt-5">
            <Skeleton className="h-10 w-32" />
          </div>
        </div>
        <div className="rounded-xl border bg-card p-5">
          <div className="flex items-start gap-3">
            <Skeleton className="size-9 rounded-lg" />
            <div className="flex flex-1 flex-col gap-2">
              <Skeleton className="h-5 w-32" />
              <Skeleton className="h-3 w-56 max-w-full" />
            </div>
          </div>
          <div className="mt-6 flex flex-col gap-3">
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-5/6" />
            <Skeleton className="h-4 w-2/3" />
            <Skeleton className="h-10 w-full" />
          </div>
        </div>
      </div>
    </div>
  );
}

export function MonitorCreatePage() {
  useDocumentTitle("New monitor");
  const navigate = useNavigate();
  const loader = useCallback(async (signal: AbortSignal): Promise<CreatePageData> => {
    const [providerList, settings, pushConfig] = await Promise.all([
      providersApi.list(signal),
      settingsApi.get(signal),
      pushApi.config(signal),
    ]);
    return {
      providers: providerList.providers,
      settings,
      pushConfig,
    };
  }, []);
  const { data, error, loading, refresh } = useApiResource(loader);

  async function handleSubmit(payload: MonitorCreateRequest) {
    const created = await monitorsApi.create(payload);
    toast.success("Monitor created", {
      description: "The first check is scheduled shortly.",
    });
    navigate(`/monitors/${encodeURIComponent(created.id)}`, {
      replace: true,
    });
  }

  const providers = data?.providers ?? [];
  const urlProvider = providers.find(
    (provider) => provider.capabilities.supportsUrlSearch,
  );
  const UrlSearchFlow = urlProvider ? urlSearchFlowFor(urlProvider) : null;
  const testProviders = providers.filter((provider) => provider.testOnly);

  return (
    <div className="monitor-create flex flex-col gap-5 pb-8">
      <Breadcrumbs
        items={[{ label: "Monitors", to: "/monitors" }, { label: "New monitor" }]}
      />
      <PageHeader
        eyebrow="Monitor setup"
        title="New monitor"
        description="Search the selected provider, choose the wagons or fare options to watch, and confirm the monitor."
        actions={
          <Button asChild variant="outline">
            <Link to="/monitors">Cancel</Link>
          </Button>
        }
      />

      {loading ? (
        <MonitorCreateSkeleton />
      ) : error ? (
        <ErrorState
          error={error}
          title="Could not load monitor setup"
          onRetry={refresh}
        />
      ) : urlProvider && UrlSearchFlow && data ? (
        <div className="flex flex-col gap-6">
          <UrlSearchFlow
            provider={urlProvider}
            settings={data.settings}
            pushConfig={data.pushConfig}
            onSubmit={handleSubmit}
          />
          {testProviders.length > 0 ? (
            <details className="border-t pt-4">
              <summary className="flex w-fit cursor-pointer flex-wrap items-center gap-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground">
                <FlaskConical className="size-3.5" aria-hidden="true" />
                Local test-provider flow
                <TestOnlyBadge />
              </summary>
              <p className="mt-3 max-w-3xl text-xs leading-relaxed text-muted-foreground">
                This deterministic fixture provider is available only in
                development. It never contacts a real ticket source and is not
                part of the provider search flow above.
              </p>
              <div className="mt-4 rounded-xl border bg-muted/15 p-4">
                <MonitorForm
                  mode="create"
                  initialValues={emptyMonitorForm()}
                  providers={testProviders}
                  onSubmit={handleSubmit}
                  cancelTo="/monitors"
                />
              </div>
            </details>
          ) : null}
        </div>
      ) : (
        <div className="flex flex-col gap-6">
          <Alert variant="warning">
            <AlertTriangle aria-hidden="true" />
            <AlertTitle>No URL-search provider is available</AlertTitle>
            <AlertDescription>
              {urlProvider
                ? `${urlProvider.displayName} is registered, but this build does not include its search form.`
                : "Production provider registration remains deferred. Use an available test-only provider only when the development environment explicitly registers one."}
            </AlertDescription>
          </Alert>
          <MonitorForm
            mode="create"
            initialValues={emptyMonitorForm()}
            providers={providers}
            onSubmit={handleSubmit}
            cancelTo="/monitors"
          />
        </div>
      )}
    </div>
  );
}
