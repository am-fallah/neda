import { useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { monitorsApi, providersApi, pushApi, settingsApi } from "@/api/endpoints";
import type {
  MonitorCreateRequest,
  ProviderInfo,
  PushConfig,
  Settings,
} from "@/api/types";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { PageHeader } from "@/components/layout/PageHeader";
import { MonitorForm } from "@/components/monitors/MonitorForm";
import { MrBilitSearchForm } from "@/components/monitors/MrBilitSearchForm";
import { useApiResource } from "@/hooks/useApiResource";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";
import { emptyMonitorForm } from "@/lib/monitorForm";

interface CreatePageData {
  providers: ProviderInfo[];
  settings: Settings;
  pushConfig: PushConfig;
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
  const mrbilitAvailable = providers.some((provider) => provider.id === "mrbilit");
  const testProviders = providers.filter((provider) => provider.testOnly);

  return (
    <div className="space-y-6">
      <PageHeader
        title="New monitor"
        description="Paste a MrBilit train search URL, choose the exact trains and classes to monitor, then confirm the route, date, passengers, interval, and notification settings."
      />
      {loading ? (
        <LoadingState rows={3} label="Loading providers" />
      ) : error ? (
        <ErrorState
          error={error}
          title="Could not load monitor setup"
          onRetry={refresh}
        />
      ) : mrbilitAvailable && data ? (
        <div className="space-y-8">
          <MrBilitSearchForm
            providers={providers}
            settings={data.settings}
            pushConfig={data.pushConfig}
            onSubmit={handleSubmit}
          />
          {testProviders.length > 0 ? (
            <details className="rounded-lg border p-4">
              <summary className="cursor-pointer text-sm font-medium">
                Create a test-provider monitor instead
              </summary>
              <p className="mt-1 text-xs text-muted-foreground">
                The test provider is a deterministic fixture and never contacts a real ticket source.
              </p>
              <div className="mt-4">
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
        <MonitorForm
          mode="create"
          initialValues={emptyMonitorForm()}
          providers={providers}
          onSubmit={handleSubmit}
          cancelTo="/monitors"
        />
      )}
    </div>
  );
}
