import { useMemo, useState, type FormEvent } from "react";
import { AlertTriangle, Loader2, Save } from "lucide-react";
import { Link } from "react-router-dom";
import { isApiError } from "@/api/client";
import type { MonitorCreateRequest, ProviderInfo } from "@/api/types";
import { JalaliDatePicker } from "@/components/JalaliDatePicker";
import { TestOnlyBadge } from "@/components/status";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { FormField, fieldProps } from "@/components/ui/form-field";
import { Input, Label } from "@/components/ui/input";
import {
  buildMonitorPayload,
  emptyProviderConfig,
  mapServerFieldErrors,
  validateMonitorForm,
  type MonitorFormErrors,
  type MonitorFormValues,
} from "@/lib/monitorForm";
import { todayInTehran } from "@/lib/dates";
import { cn } from "@/lib/utils";

interface MonitorFormProps {
  mode: "create" | "edit";
  initialValues: MonitorFormValues;
  providers: ProviderInfo[];
  onSubmit: (payload: MonitorCreateRequest) => Promise<void>;
  cancelTo: string;
  expiredWarning?: boolean;
}

function formFieldID(key: string, providers: ProviderInfo[]): string {
  if (key.startsWith("providerConfigs.")) {
    const [, providerId, field] = key.split(".");
    return `provider-${providerId}-${field}`;
  }
  if (key.startsWith("provider.")) {
    return `provider-${key.slice("provider.".length)}-enabled`;
  }
  if (key === "passengers") return "adults";
  if (key === "providers") {
    return providers.length > 0
      ? `provider-${providers[0].id}-enabled`
      : "providers";
  }
  return key;
}

export function MonitorForm({
  mode,
  initialValues,
  providers,
  onSubmit,
  cancelTo,
  expiredWarning,
}: MonitorFormProps) {
  const [values, setValues] = useState<MonitorFormValues>(initialValues);
  const [errors, setErrors] = useState<MonitorFormErrors>({});
  const [formError, setFormError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const minDate = todayInTehran();
  const providerMap = useMemo(
    () => new Map(providers.map((provider) => [provider.id, provider])),
    [providers],
  );

  function update<K extends keyof MonitorFormValues>(
    key: K,
    value: MonitorFormValues[K],
  ) {
    setValues((current) => ({ ...current, [key]: value }));
    setErrors((current) => {
      if (!(key in current)) return current;
      const next = { ...current };
      delete next[key as string];
      return next;
    });
  }

  function updateProviderConfig(
    providerId: string,
    field: keyof ReturnType<typeof emptyProviderConfig>,
    value: string,
  ) {
    setValues((current) => ({
      ...current,
      providerConfigs: {
        ...current.providerConfigs,
        [providerId]: {
          ...(current.providerConfigs[providerId] ?? emptyProviderConfig()),
          [field]: value,
        },
      },
    }));
    const key = `providerConfigs.${providerId}.${field}`;
    setErrors((current) => {
      if (!(key in current)) return current;
      const next = { ...current };
      delete next[key];
      return next;
    });
  }

  function toggleProvider(providerId: string, checked: boolean) {
    setValues((current) => {
      const selected = checked
        ? [...current.selectedProviderIds, providerId]
        : current.selectedProviderIds.filter((id) => id !== providerId);
      return {
        ...current,
        selectedProviderIds: selected,
        providerConfigs: {
          ...current.providerConfigs,
          [providerId]:
            current.providerConfigs[providerId] ?? emptyProviderConfig(),
        },
      };
    });
    setErrors((current) => {
      const next = { ...current };
      delete next.providers;
      delete next[`provider.${providerId}`];
      return next;
    });
  }

  function focusFirstError(nextErrors: MonitorFormErrors) {
    const key = Object.keys(nextErrors)[0];
    if (!key) return;
    const id = formFieldID(key, providers);
    const element = document.getElementById(id);
    if (element instanceof HTMLElement) {
      element.focus();
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError("");
    const clientErrors = validateMonitorForm(values, providers);
    if (Object.keys(clientErrors).length > 0) {
      setErrors(clientErrors);
      setFormError(
        `Please fix ${Object.keys(clientErrors).length} field${
          Object.keys(clientErrors).length === 1 ? "" : "s"
        } before saving.`,
      );
      focusFirstError(clientErrors);
      return;
    }
    const payload = buildMonitorPayload(values);
    setSubmitting(true);
    try {
      await onSubmit(payload);
    } catch (caught) {
      if (isApiError(caught)) {
        const selectedProviders = values.selectedProviderIds
          .map((id) => providerMap.get(id))
          .filter((provider): provider is ProviderInfo => Boolean(provider));
        const mapped = mapServerFieldErrors(caught.fields, selectedProviders);
        const serverErrors = { ...mapped.errors };
        if (mapped.general.length > 0) {
          serverErrors.providers = mapped.general.join("; ");
        }
        setErrors(serverErrors);
        if (Object.keys(serverErrors).length > 0) {
          setFormError(
            "The server rejected some values. Review the highlighted fields.",
          );
          focusFirstError(serverErrors);
        } else {
          setFormError(caught.message);
        }
      } else {
        setFormError("The monitor could not be saved. Try again.");
      }
    } finally {
      setSubmitting(false);
    }
  }

  const totalPassengers = values.adults + values.children + values.infants;

  return (
    <form onSubmit={handleSubmit} noValidate className="space-y-6">
      {formError ? (
        <Alert variant="destructive">
          <AlertTriangle aria-hidden="true" />
          <AlertTitle>Check the form</AlertTitle>
          <AlertDescription>{formError}</AlertDescription>
        </Alert>
      ) : null}

      {expiredWarning ? (
        <Alert variant="warning">
          <AlertTriangle aria-hidden="true" />
          <AlertTitle>This monitor has expired</AlertTitle>
          <AlertDescription>
            Choose a future departure date to make it eligible for monitoring
            again. You can then resume it from the monitor page.
          </AlertDescription>
        </Alert>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Route and departure</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <FormField
            id="label"
            label="Monitor label"
            error={errors.label}
            required
            description="A short name to recognise this monitor."
          >
            <Input
              {...fieldProps("label", errors.label)}
              value={values.label}
              onChange={(event) => update("label", event.target.value)}
              placeholder="Tehran to Mashhad — Friday"
              maxLength={100}
              autoComplete="off"
            />
          </FormField>

          <div className="grid gap-4 sm:grid-cols-2">
            <FormField
              id="originName"
              label="Origin name"
              error={errors.originName}
              required
            >
              <Input
                {...fieldProps("originName", errors.originName)}
                value={values.originName}
                onChange={(event) => update("originName", event.target.value)}
                placeholder="Tehran"
                autoComplete="off"
              />
            </FormField>
            <FormField
              id="destinationName"
              label="Destination name"
              error={errors.destinationName}
              required
            >
              <Input
                {...fieldProps("destinationName", errors.destinationName)}
                value={values.destinationName}
                onChange={(event) =>
                  update("destinationName", event.target.value)
                }
                placeholder="Mashhad"
                autoComplete="off"
              />
            </FormField>
          </div>

          <FormField
            id="departureDate"
            label="Departure date"
            error={errors.departureDate}
            required
            description="Selected in the Jalali calendar. Sent to the server as a Gregorian YYYY-MM-DD date."
          >
            <JalaliDatePicker
              id="departureDate"
              value={values.departureDate}
              onChange={(value) => update("departureDate", value)}
              min={minDate}
              invalid={Boolean(errors.departureDate)}
              describedBy={
                errors.departureDate ? "departureDate-error" : undefined
              }
            />
          </FormField>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Passengers</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="grid gap-4 sm:grid-cols-3">
            <FormField
              id="adults"
              label="Adults"
              error={errors.adults}
              required
              description="1–9"
            >
              <Input
                {...fieldProps("adults", errors.adults)}
                type="number"
                min={1}
                max={9}
                inputMode="numeric"
                value={values.adults}
                onChange={(event) =>
                  update("adults", Number(event.target.value))
                }
              />
            </FormField>
            <FormField
              id="children"
              label="Children"
              error={errors.children}
              description="0–9"
            >
              <Input
                {...fieldProps("children", errors.children)}
                type="number"
                min={0}
                max={9}
                inputMode="numeric"
                value={values.children}
                onChange={(event) =>
                  update("children", Number(event.target.value))
                }
              />
            </FormField>
            <FormField
              id="infants"
              label="Infants"
              error={errors.infants}
              description="0–9"
            >
              <Input
                {...fieldProps("infants", errors.infants)}
                type="number"
                min={0}
                max={9}
                inputMode="numeric"
                value={values.infants}
                onChange={(event) =>
                  update("infants", Number(event.target.value))
                }
              />
            </FormField>
          </div>
          <p className="text-xs text-muted-foreground">
            Total passengers: {totalPassengers} (maximum 9).
          </p>
          {errors.passengers ? (
            <p className="text-xs font-medium text-destructive" role="alert">
              {errors.passengers}
            </p>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Providers</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {providers.length === 0 ? (
            <Alert variant="warning">
              <AlertTriangle aria-hidden="true" />
              <AlertTitle>No providers available</AlertTitle>
              <AlertDescription>
                A monitor needs at least one registered provider. In this build,
                no provider is registered in production; the development
                environment may enable the test-only provider explicitly.
              </AlertDescription>
            </Alert>
          ) : null}
          {errors.providers ? (
            <p className="text-xs font-medium text-destructive" role="alert">
              {errors.providers}
            </p>
          ) : null}

          <fieldset className="space-y-3">
            <legend className="sr-only">Select providers</legend>
            {providers.map((provider) => {
              const checked = values.selectedProviderIds.includes(provider.id);
              const config =
                values.providerConfigs[provider.id] ?? emptyProviderConfig();
              const supportsBooking =
                provider.capabilities.supportsBookingUrl !== false;
              const supportsGender = Boolean(
                provider.capabilities.supportsGender,
              );
              return (
                <div
                  key={provider.id}
                  className={cn(
                    "rounded-lg border p-4 transition-colors",
                    checked && "border-primary/40 bg-accent/20",
                  )}
                >
                  <div className="flex items-start gap-3">
                    <input
                      id={`provider-${provider.id}-enabled`}
                      type="checkbox"
                      checked={checked}
                      onChange={(event) =>
                        toggleProvider(provider.id, event.target.checked)
                      }
                      className="mt-1 size-4 rounded border-input accent-[var(--primary)] focus-visible:ring-2 focus-visible:ring-ring"
                    />
                    <div className="min-w-0 flex-1">
                      <Label
                        htmlFor={`provider-${provider.id}-enabled`}
                        className="flex flex-wrap items-center gap-2"
                      >
                        {provider.displayName}
                        {provider.testOnly ? <TestOnlyBadge /> : null}
                      </Label>
                      <p className="mt-0.5 text-xs text-muted-foreground">
                        Provider ID:{" "}
                        <code className="font-mono">{provider.id}</code>
                        {provider.capabilities.maxPassengers
                          ? ` · up to ${provider.capabilities.maxPassengers} passengers`
                          : ""}
                        {supportsBooking
                          ? " · optional booking slug"
                          : " · no booking slug"}
                        {supportsGender ? " · gender code" : ""}
                      </p>
                      {provider.testOnly ? (
                        <p className="mt-1 text-xs text-warning-foreground dark:text-warning">
                          Test-only source, not a real ticket provider. Route IDs
                          must start with <code>test-origin-</code> and{" "}
                          <code>test-destination-</code>.
                        </p>
                      ) : null}
                      {errors[`provider.${provider.id}`] ? (
                        <p className="mt-1 text-xs font-medium text-destructive">
                          {errors[`provider.${provider.id}`]}
                        </p>
                      ) : null}
                    </div>
                  </div>

                  {checked ? (
                    <div className="mt-4 grid gap-4 border-t pt-4 sm:grid-cols-2">
                      <FormField
                        id={`provider-${provider.id}-originId`}
                        label="Provider origin ID"
                        error={errors[`providerConfigs.${provider.id}.originId`]}
                        required
                        description={
                          provider.testOnly
                            ? "For example, test-origin-available."
                            : "Numeric or provider-specific origin identifier."
                        }
                      >
                        <Input
                          {...fieldProps(
                            `provider-${provider.id}-originId`,
                            errors[`providerConfigs.${provider.id}.originId`],
                          )}
                          value={config.originId}
                          onChange={(event) =>
                            updateProviderConfig(
                              provider.id,
                              "originId",
                              event.target.value,
                            )
                          }
                          placeholder={
                            provider.testOnly
                              ? "test-origin-available"
                              : "Origin ID"
                          }
                          autoComplete="off"
                        />
                      </FormField>
                      <FormField
                        id={`provider-${provider.id}-destinationId`}
                        label="Provider destination ID"
                        error={
                          errors[`providerConfigs.${provider.id}.destinationId`]
                        }
                        required
                        description={
                          provider.testOnly
                            ? "For example, test-destination-default."
                            : "Numeric or provider-specific destination identifier."
                        }
                      >
                        <Input
                          {...fieldProps(
                            `provider-${provider.id}-destinationId`,
                            errors[
                              `providerConfigs.${provider.id}.destinationId`
                            ],
                          )}
                          value={config.destinationId}
                          onChange={(event) =>
                            updateProviderConfig(
                              provider.id,
                              "destinationId",
                              event.target.value,
                            )
                          }
                          placeholder={
                            provider.testOnly
                              ? "test-destination-default"
                              : "Destination ID"
                          }
                          autoComplete="off"
                        />
                      </FormField>
                      {supportsBooking ? (
                        <FormField
                          id={`provider-${provider.id}-bookingSlug`}
                          label="Booking slug (optional)"
                          error={
                            errors[`providerConfigs.${provider.id}.bookingSlug`]
                          }
                          description="Only when a validated route slug is known. Unverified slugs are not used in notifications."
                        >
                          <Input
                            {...fieldProps(
                              `provider-${provider.id}-bookingSlug`,
                              errors[
                                `providerConfigs.${provider.id}.bookingSlug`
                              ],
                            )}
                            value={config.bookingSlug}
                            onChange={(event) =>
                              updateProviderConfig(
                                provider.id,
                                "bookingSlug",
                                event.target.value,
                              )
                            }
                            placeholder="Provider route slug"
                            autoComplete="off"
                            maxLength={256}
                          />
                        </FormField>
                      ) : null}
                      {supportsGender ? (
                        <FormField
                          id={`provider-${provider.id}-genderCode`}
                          label="Gender code (optional)"
                          error={
                            errors[`providerConfigs.${provider.id}.genderCode`]
                          }
                          description="Only if this provider genuinely requires it."
                        >
                          <Input
                            {...fieldProps(
                              `provider-${provider.id}-genderCode`,
                              errors[
                                `providerConfigs.${provider.id}.genderCode`
                              ],
                            )}
                            value={config.genderCode}
                            onChange={(event) =>
                              updateProviderConfig(
                                provider.id,
                                "genderCode",
                                event.target.value,
                              )
                            }
                            placeholder="Provider gender code"
                            autoComplete="off"
                            maxLength={64}
                          />
                        </FormField>
                      ) : null}
                    </div>
                  ) : null}
                </div>
              );
            })}
          </fieldset>
        </CardContent>
      </Card>

      <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <Button asChild variant="outline">
          <Link to={cancelTo}>Cancel</Link>
        </Button>
        <Button type="submit" disabled={submitting || providers.length === 0}>
          {submitting ? (
            <Loader2 className="animate-spin" aria-hidden="true" />
          ) : (
            <Save aria-hidden="true" />
          )}
          {submitting
            ? "Saving…"
            : mode === "create"
              ? "Create monitor"
              : "Save changes"}
        </Button>
      </div>
    </form>
  );
}
