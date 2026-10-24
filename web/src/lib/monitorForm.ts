import type {
  Monitor,
  MonitorCreateRequest,
  MonitorDetail,
  ProviderInfo,
} from "@/api/types";
import { isIsoDate, todayInTehran } from "@/lib/dates";

export interface ProviderFormConfig {
  originId: string;
  destinationId: string;
  bookingSlug: string;
  genderCode: string;
}

export interface MonitorFormValues {
  label: string;
  originName: string;
  destinationName: string;
  departureDate: string;
  adults: number;
  children: number;
  infants: number;
  selectedProviderIds: string[];
  providerConfigs: Record<string, ProviderFormConfig>;
}

export type MonitorFormErrors = Record<string, string>;

function hasControlCharacters(value: string): boolean {
  for (const character of value) {
    const code = character.codePointAt(0) ?? 0;
    if (code <= 0x1f || code === 0x7f) return true;
  }
  return false;
}

export function emptyProviderConfig(): ProviderFormConfig {
  return {
    originId: "",
    destinationId: "",
    bookingSlug: "",
    genderCode: "",
  };
}

export function emptyMonitorForm(): MonitorFormValues {
  return {
    label: "",
    originName: "",
    destinationName: "",
    departureDate: "",
    adults: 1,
    children: 0,
    infants: 0,
    selectedProviderIds: [],
    providerConfigs: {},
  };
}

export function formValuesFromMonitor(
  monitor: Monitor | MonitorDetail,
): MonitorFormValues {
  const configs: Record<string, ProviderFormConfig> = {};
  const selected: string[] = [];
  for (const provider of monitor.providers) {
    if (!provider.active) continue;
    selected.push(provider.providerId);
    configs[provider.providerId] = {
      originId: provider.originId,
      destinationId: provider.destinationId,
      bookingSlug: provider.bookingSlug ?? "",
      genderCode: provider.genderCode ?? "",
    };
  }
  return {
    label: monitor.label,
    originName: monitor.originName,
    destinationName: monitor.destinationName,
    departureDate: monitor.departureDate,
    adults: monitor.adults,
    children: monitor.children,
    infants: monitor.infants,
    selectedProviderIds: selected,
    providerConfigs: configs,
  };
}

export function validateMonitorForm(
  values: MonitorFormValues,
  providers: ProviderInfo[],
  now: Date = new Date(),
): MonitorFormErrors {
  const errors: MonitorFormErrors = {};
  const label = values.label.trim();
  if (!label) errors.label = "Enter a monitor label.";
  else if ([...label].length > 100) {
    errors.label = "Use at most 100 characters.";
  }

  const origin = values.originName.trim();
  const destination = values.destinationName.trim();
  if (!origin) errors.originName = "Enter the origin name.";
  if (!destination) errors.destinationName = "Enter the destination name.";
  if (origin && destination && origin.toLowerCase() === destination.toLowerCase()) {
    errors.destinationName = "Destination must differ from origin.";
  }

  if (!values.departureDate) {
    errors.departureDate = "Select a departure date.";
  } else if (!isIsoDate(values.departureDate)) {
    errors.departureDate = "Enter a valid Gregorian date.";
  } else if (values.departureDate < todayInTehran(now)) {
    errors.departureDate = "Departure date must not be before today in Tehran.";
  }

  if (!Number.isInteger(values.adults) || values.adults < 1 || values.adults > 9) {
    errors.adults = "Adults must be between 1 and 9.";
  }
  if (
    !Number.isInteger(values.children) ||
    values.children < 0 ||
    values.children > 9
  ) {
    errors.children = "Children must be between 0 and 9.";
  }
  if (
    !Number.isInteger(values.infants) ||
    values.infants < 0 ||
    values.infants > 9
  ) {
    errors.infants = "Infants must be between 0 and 9.";
  }
  const total = values.adults + values.children + values.infants;
  if (total > 9) {
    errors.passengers = "Total passengers must be at most 9.";
  }

  if (values.selectedProviderIds.length === 0) {
    errors.providers = "Select at least one provider.";
  }

  const providerByID = new Map(providers.map((provider) => [provider.id, provider]));
  let allowedMax = 9;
  for (const providerId of values.selectedProviderIds) {
    const provider = providerByID.get(providerId);
    const config = values.providerConfigs[providerId] ?? emptyProviderConfig();
    if (!provider) {
      errors.providers = "A selected provider is not registered.";
      continue;
    }
    if (
      provider.capabilities.maxPassengers &&
      provider.capabilities.maxPassengers > 0
    ) {
      allowedMax = Math.min(allowedMax, provider.capabilities.maxPassengers);
    }
    if (!config.originId.trim()) {
      errors[`providerConfigs.${providerId}.originId`] =
        "Enter the provider origin ID.";
    }
    if (!config.destinationId.trim()) {
      errors[`providerConfigs.${providerId}.destinationId`] =
        "Enter the provider destination ID.";
    }
    if (
      config.originId.trim() &&
      config.destinationId.trim() &&
      config.originId.trim() === config.destinationId.trim()
    ) {
      errors[`providerConfigs.${providerId}.destinationId`] =
        "Destination ID must differ from origin ID.";
    }
    if (config.bookingSlug.length > 256) {
      errors[`providerConfigs.${providerId}.bookingSlug`] =
        "Use at most 256 characters.";
    } else if (hasControlCharacters(config.bookingSlug)) {
      errors[`providerConfigs.${providerId}.bookingSlug`] =
        "Control characters are not allowed.";
    } else if (
      config.bookingSlug.trim() &&
      provider.capabilities.supportsBookingUrl === false
    ) {
      errors[`providerConfigs.${providerId}.bookingSlug`] =
        "This provider does not support booking slugs.";
    }
    if (config.genderCode.length > 64) {
      errors[`providerConfigs.${providerId}.genderCode`] =
        "Use at most 64 characters.";
    } else if (hasControlCharacters(config.genderCode)) {
      errors[`providerConfigs.${providerId}.genderCode`] =
        "Control characters are not allowed.";
    } else if (
      config.genderCode.trim() &&
      provider.capabilities.supportsGender === false
    ) {
      errors[`providerConfigs.${providerId}.genderCode`] =
        "This provider does not support a gender code.";
    }
  }
  if (total > allowedMax && !errors.passengers) {
    errors.passengers = `The selected provider supports at most ${allowedMax} passengers.`;
  }

  return errors;
}

export function buildMonitorPayload(
  values: MonitorFormValues,
): MonitorCreateRequest {
  return {
    label: values.label.trim(),
    originName: values.originName.trim(),
    destinationName: values.destinationName.trim(),
    departureDate: values.departureDate,
    adults: values.adults,
    children: values.children,
    infants: values.infants,
    providers: values.selectedProviderIds.map((providerId) => {
      const config =
        values.providerConfigs[providerId] ?? emptyProviderConfig();
      return {
        providerId,
        originId: config.originId.trim(),
        destinationId: config.destinationId.trim(),
        ...(config.bookingSlug.trim()
          ? { bookingSlug: config.bookingSlug.trim() }
          : {}),
        ...(config.genderCode.trim()
          ? { genderCode: config.genderCode.trim() }
          : {}),
      };
    }),
  };
}

/** Maps server field keys such as `providers[0].originId` to form keys. */
export function mapServerFieldErrors(
  fieldErrors: Record<string, string>,
  providers: ProviderInfo[],
): { errors: MonitorFormErrors; general: string[] } {
  const errors: MonitorFormErrors = {};
  const general: string[] = [];
  for (const [field, message] of Object.entries(fieldErrors)) {
    const providerMatch = /^providers\[(\d+)\](?:\.(.+))?$/.exec(field);
    if (providerMatch) {
      const index = Number(providerMatch[1]);
      const provider = providers[index];
      if (!provider) {
        general.push(message);
        continue;
      }
      const suffix = providerMatch[2];
      if (!suffix) {
        errors[`provider.${provider.id}`] = message;
        general.push(`${provider.displayName}: ${message}`);
      } else if (suffix === "providerId") {
        errors[`provider.${provider.id}`] = message;
        general.push(`${provider.displayName}: ${message}`);
      } else {
        errors[`providerConfigs.${provider.id}.${suffix}`] = message;
      }
      continue;
    }
    errors[field] = message;
  }
  return { errors, general };
}
