import type { ComponentType } from "react";
import type {
  MonitorCreateRequest,
  ProviderInfo,
  PushConfig,
  Settings,
} from "@/api/types";
import { MrBilitSearchForm } from "@/components/monitors/MrBilitSearchForm";

export interface ProviderUrlSearchFlowProps {
  provider: ProviderInfo;
  settings: Settings;
  pushConfig: PushConfig;
  onSubmit: (payload: MonitorCreateRequest) => Promise<void>;
}

/**
 * Small explicit registry, not a plugin system. A provider without a flow can
 * still be monitored through the generic API, but this build does not guess a
 * search form for it.
 */
const URL_SEARCH_FLOWS: Record<
  string,
  ComponentType<ProviderUrlSearchFlowProps>
> = {
  mrbilit: MrBilitSearchForm,
};

export function urlSearchFlowFor(
  provider: ProviderInfo,
): ComponentType<ProviderUrlSearchFlowProps> | null {
  return URL_SEARCH_FLOWS[provider.id] ?? null;
}
