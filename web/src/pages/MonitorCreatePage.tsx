import { useCallback } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { monitorsApi, providersApi } from "@/api/endpoints";
import type { MonitorCreateRequest, ProviderInfo } from "@/api/types";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { PageHeader } from "@/components/layout/PageHeader";
import { MonitorForm } from "@/components/monitors/MonitorForm";
import { useApiResource } from "@/hooks/useApiResource";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";
import { emptyMonitorForm } from "@/lib/monitorForm";

export function MonitorCreatePage() {
  useDocumentTitle("New monitor");
  const navigate = useNavigate();
  const loader = useCallback(
    (signal: AbortSignal): Promise<ProviderInfo[]> =>
      providersApi.list(signal).then((response) => response.providers),
    [],
  );
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

  return (
    <div className="space-y-6">
      <PageHeader
        title="New monitor"
        description="Monitoring is server-side. Choose a route, departure date in the Jalali calendar, passengers, and one or more providers."
      />
      {loading ? (
        <LoadingState rows={3} label="Loading providers" />
      ) : error ? (
        <ErrorState
          error={error}
          title="Could not load providers"
          onRetry={refresh}
        />
      ) : (
        <MonitorForm
          mode="create"
          initialValues={emptyMonitorForm()}
          providers={data ?? []}
          onSubmit={handleSubmit}
          cancelTo="/monitors"
        />
      )}
    </div>
  );
}
