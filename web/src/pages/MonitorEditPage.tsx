import { useCallback } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { monitorsApi, providersApi } from "@/api/endpoints";
import type {
  MonitorCreateRequest,
  MonitorDetail,
  ProviderInfo,
} from "@/api/types";
import { ErrorState } from "@/components/layout/ErrorState";
import { LoadingState } from "@/components/layout/LoadingState";
import { PageHeader } from "@/components/layout/PageHeader";
import { MonitorForm } from "@/components/monitors/MonitorForm";
import { useApiResource } from "@/hooks/useApiResource";
import { useDocumentTitle } from "@/hooks/useDocumentTitle";
import { formValuesFromMonitor } from "@/lib/monitorForm";

interface EditData {
  monitor: MonitorDetail;
  providers: ProviderInfo[];
}

export function MonitorEditPage() {
  const { id = "" } = useParams();
  useDocumentTitle("Edit monitor");
  const navigate = useNavigate();
  const loader = useCallback(
    async (signal: AbortSignal): Promise<EditData> => {
      const [monitor, providerList] = await Promise.all([
        monitorsApi.get(id, signal),
        providersApi.list(signal),
      ]);
      return { monitor, providers: providerList.providers };
    },
    [id],
  );
  const { data, error, loading, refresh } = useApiResource(loader);

  async function handleSubmit(payload: MonitorCreateRequest) {
    await monitorsApi.update(id, payload);
    toast.success("Monitor updated", {
      description:
        "Material changes reset availability state and schedule a fresh check.",
    });
    navigate(`/monitors/${encodeURIComponent(id)}`, { replace: true });
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title={data ? `Edit ${data.monitor.label}` : "Edit monitor"}
        description="Changing the route, date, passengers, or providers resets availability state. Label-only changes do not."
      />
      {loading ? (
        <LoadingState rows={3} label="Loading monitor" />
      ) : error ? (
        <ErrorState
          error={error}
          title={
            error.status === 404
              ? "Monitor not found"
              : "Could not load the monitor"
          }
          onRetry={refresh}
        />
      ) : data ? (
        <MonitorForm
          mode="edit"
          initialValues={formValuesFromMonitor(data.monitor)}
          providers={data.providers}
          onSubmit={handleSubmit}
          cancelTo={`/monitors/${encodeURIComponent(id)}`}
          expiredWarning={data.monitor.status === "expired"}
        />
      ) : null}
    </div>
  );
}
