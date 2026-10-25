import { useMemo, useState } from "react";
import { AlertTriangle, Loader2, Search, TrainFront } from "lucide-react";
import { Link } from "react-router-dom";
import { isApiError } from "@/api/client";
import { providersApi } from "@/api/endpoints";
import type {
  MonitorCreateRequest,
  ProviderInfo,
  ProviderSearchClass,
  ProviderSearchResult,
  ProviderSearchTrain,
  PushConfig,
  SelectedOption,
  Settings,
} from "@/api/types";
import { DateDisplay } from "@/components/JalaliDatePicker";
import { ObservationStateBadge } from "@/components/status";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { FormField, fieldProps } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";
import { formatTehranTimestamp } from "@/lib/dates";

interface MrBilitSearchFormProps {
  providers: ProviderInfo[];
  settings: Settings;
  pushConfig: PushConfig;
  onSubmit: (payload: MonitorCreateRequest) => Promise<void>;
}

function optionKey(journeyKey: string, offeringKey: string): string {
  return `${journeyKey}\u0000${offeringKey}`;
}

function toSelectedOption(
  train: ProviderSearchTrain,
  item: ProviderSearchClass,
): SelectedOption {
  return {
    journeyKey: train.journeyKey,
    offeringKey: item.offeringKey,
    journeyNativeId: train.nativeId,
    offeringNativeId: item.nativeId,
    trainNumber: train.trainNumber,
    operator: train.operator,
    originName: train.originName,
    destinationName: train.destinationName,
    departureAt: train.departureAt,
    arrivalAt: train.arrivalAt,
    wagonId: item.wagonId,
    wagonName: item.wagonName,
    className: item.className,
    compartmentType: item.compartmentType,
  };
}

function formatCapacity(capacity: number | null): string {
  if (capacity === null) return "Capacity unavailable";
  return `${capacity} seat${capacity === 1 ? "" : "s"}`;
}

export function MrBilitSearchForm({
  providers,
  settings,
  pushConfig,
  onSubmit,
}: MrBilitSearchFormProps) {
  const mrbilit = providers.find((provider) => provider.id === "mrbilit");
  const [url, setUrl] = useState("");
  const [adults, setAdults] = useState(1);
  const [children, setChildren] = useState(0);
  const [infants, setInfants] = useState(0);
  const [result, setResult] = useState<ProviderSearchResult | null>(null);
  const [selected, setSelected] = useState<Record<string, SelectedOption>>({});
  const [label, setLabel] = useState("");
  const [searching, setSearching] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [searchError, setSearchError] = useState("");
  const [createError, setCreateError] = useState("");

  const selectedOptions = useMemo(() => Object.values(selected), [selected]);
  const selectedCount = selectedOptions.length;

  function resetResults() {
    setResult(null);
    setSelected({});
    setLabel("");
    setFieldErrors({});
    setSearchError("");
    setCreateError("");
  }

  function passengersValid(): boolean {
    if (adults < 1 || adults > 9) return false;
    if (children < 0 || children > 9) return false;
    if (infants < 0 || infants > 9) return false;
    return adults + children + infants <= 9;
  }

  async function handleSearch() {
    setSearchError("");
    setCreateError("");
    setFieldErrors({});
    if (!url.trim()) {
      setFieldErrors({ url: "Paste a MrBilit train search URL." });
      return;
    }
    if (!passengersValid()) {
      setFieldErrors({ passengers: "Passengers must be between 1 and 9 total." });
      return;
    }
    setSearching(true);
    try {
      const next = await providersApi.search("mrbilit", {
        url: url.trim(),
        adults,
        children,
        infants,
      });
      setResult(next);
      setSelected({});
      setLabel(
        `${next.route.originName || next.slug} → ${next.route.destinationName || ""} — ${next.departureDate}`.trim(),
      );
    } catch (caught) {
      setResult(null);
      setSelected({});
      if (isApiError(caught)) {
        setFieldErrors(caught.fields);
        setSearchError(caught.message);
      } else {
        setSearchError("The MrBilit search could not be completed.");
      }
    } finally {
      setSearching(false);
    }
  }

  function toggle(train: ProviderSearchTrain, item: ProviderSearchClass) {
    const key = optionKey(train.journeyKey, item.offeringKey);
    setSelected((current) => {
      const next = { ...current };
      if (next[key]) {
        delete next[key];
      } else {
        next[key] = toSelectedOption(train, item);
      }
      return next;
    });
  }

  async function handleCreate() {
    setCreateError("");
    setFieldErrors({});
    if (!result) {
      setCreateError("Search for trains before creating a monitor.");
      return;
    }
    if (!label.trim()) {
      setFieldErrors({ label: "Enter a monitor label." });
      return;
    }
    if (selectedCount === 0) {
      setFieldErrors({ selections: "Select at least one train and class." });
      return;
    }
    const payload: MonitorCreateRequest = {
      label: label.trim(),
      originName: result.route.originName || result.slug,
      destinationName: result.route.destinationName || "",
      departureDate: result.departureDate,
      adults: result.passengers.adults,
      children: result.passengers.children,
      infants: result.passengers.infants,
      providers: [
        {
          providerId: "mrbilit",
          originId: result.route.originId,
          destinationId: result.route.destinationId,
          bookingSlug: result.slug,
          selections: selectedOptions,
        },
      ],
    };
    setSubmitting(true);
    try {
      await onSubmit(payload);
    } catch (caught) {
      if (isApiError(caught)) {
        const nextFields = { ...caught.fields };
        for (const [field, message] of Object.entries(caught.fields)) {
          if (field.includes("selections") || field.startsWith("providers")) {
            nextFields.selections = nextFields.selections ?? message;
          }
        }
        setFieldErrors(nextFields);
        setCreateError(caught.message);
      } else {
        setCreateError("The monitor could not be created.");
      }
    } finally {
      setSubmitting(false);
    }
  }

  if (!mrbilit) {
    return (
      <Alert variant="warning">
        <AlertTriangle aria-hidden="true" />
        <AlertTitle>MrBilit is not available</AlertTitle>
        <AlertDescription>
          MrBilit is registered only outside production in this build.
        </AlertDescription>
      </Alert>
    );
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <TrainFront aria-hidden="true" />
            Search MrBilit by URL
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <FormField
            id="mrbilitUrl"
            label="MrBilit train search URL"
            error={fieldErrors.url}
            required
            description="Example: https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24. Tracking parameters are ignored."
          >
            <Input
              {...fieldProps("mrbilitUrl", fieldErrors.url)}
              value={url}
              onChange={(event) => {
                setUrl(event.target.value);
                resetResults();
              }}
              placeholder="https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24"
              autoComplete="off"
              spellCheck={false}
            />
          </FormField>

          <div className="grid gap-4 sm:grid-cols-3">
            <FormField
              id="mrbilitAdults"
              label="Adults"
              error={fieldErrors.adults}
              required
              description="1–9"
            >
              <Input
                {...fieldProps("mrbilitAdults", fieldErrors.adults)}
                type="number"
                min={1}
                max={9}
                value={adults}
                onChange={(event) => {
                  setAdults(Number(event.target.value));
                  resetResults();
                }}
              />
            </FormField>
            <FormField
              id="mrbilitChildren"
              label="Children"
              error={fieldErrors.children}
              description="0–9"
            >
              <Input
                {...fieldProps("mrbilitChildren", fieldErrors.children)}
                type="number"
                min={0}
                max={9}
                value={children}
                onChange={(event) => {
                  setChildren(Number(event.target.value));
                  resetResults();
                }}
              />
            </FormField>
            <FormField
              id="mrbilitInfants"
              label="Infants"
              error={fieldErrors.infants}
              description="0–9"
            >
              <Input
                {...fieldProps("mrbilitInfants", fieldErrors.infants)}
                type="number"
                min={0}
                max={9}
                value={infants}
                onChange={(event) => {
                  setInfants(Number(event.target.value));
                  resetResults();
                }}
              />
            </FormField>
          </div>
          {fieldErrors.passengers ? (
            <p className="text-xs font-medium text-destructive" role="alert">
              {fieldErrors.passengers}
            </p>
          ) : null}
          <p className="text-xs text-muted-foreground">
            Search results are normalized for these passengers. Changing them clears the results so the next search uses the same counts.
          </p>

          {searchError ? (
            <Alert variant="destructive">
              <AlertTriangle aria-hidden="true" />
              <AlertTitle>Search failed</AlertTitle>
              <AlertDescription>{searchError}</AlertDescription>
            </Alert>
          ) : null}

          <Button type="button" onClick={handleSearch} disabled={searching}>
            {searching ? (
              <Loader2 className="animate-spin" aria-hidden="true" />
            ) : (
              <Search aria-hidden="true" />
            )}
            {searching ? "Searching MrBilit…" : "Find trains"}
          </Button>
        </CardContent>
      </Card>

      {result && result.trains.length === 0 ? (
        <Alert variant="warning">
          <AlertTriangle aria-hidden="true" />
          <AlertTitle>No trains returned</AlertTitle>
          <AlertDescription>
            MrBilit returned no trains for this route and date. This is not treated as sold out. Try another date or URL.
          </AlertDescription>
        </Alert>
      ) : null}

      {result && result.trains.length > 0 ? (
        <>
          <Card>
            <CardHeader>
              <CardTitle>Available trains and classes</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
                <span>
                  {result.route.originName || result.slug} → {result.route.destinationName || ""}
                </span>
                <span aria-hidden="true">·</span>
                <DateDisplay value={result.departureDate} />
                <span aria-hidden="true">·</span>
                <span>Jalali {result.jalaliDate}</span>
                <span aria-hidden="true">·</span>
                <span>
                  {result.passengers.adults} adult{result.passengers.adults === 1 ? "" : "s"}
                  {result.passengers.children ? `, ${result.passengers.children} children` : ""}
                  {result.passengers.infants ? `, ${result.passengers.infants} infants` : ""}
                </span>
              </div>
              {result.warnings.length > 0 ? (
                <p className="text-xs text-muted-foreground">
                  Provider warnings: {result.warnings.join(", ")}
                </p>
              ) : null}

              {result.trains.map((train) => (
                <div key={train.journeyKey} className="rounded-lg border p-4">
                  <div className="flex flex-wrap items-start justify-between gap-2">
                    <div>
                      <p className="font-medium">
                        Train {train.trainNumber || train.nativeId || train.journeyKey}
                        {train.operator ? ` · ${train.operator}` : ""}
                      </p>
                      <p className="text-xs text-muted-foreground">
                        {formatTehranTimestamp(train.departureAt)} → {formatTehranTimestamp(train.arrivalAt)}
                      </p>
                    </div>
                    <Badge variant="outline">{train.classes.length} classes</Badge>
                  </div>
                  <div className="mt-3 space-y-2">
                    {train.classes.map((item) => {
                      const key = optionKey(train.journeyKey, item.offeringKey);
                      const checked = Boolean(selected[key]);
                      return (
                        <label
                          key={item.offeringKey}
                          className="flex cursor-pointer items-start gap-3 rounded-md border p-3 transition-colors hover:bg-accent/30"
                        >
                          <input
                            type="checkbox"
                            checked={checked}
                            onChange={() => toggle(train, item)}
                            className="mt-1 size-4 rounded border-input accent-[var(--primary)]"
                          />
                          <span className="min-w-0 flex-1">
                            <span className="flex flex-wrap items-center gap-2">
                              <span className="font-medium">
                                {item.wagonName || item.className || item.offeringKey}
                              </span>
                              <ObservationStateBadge state={item.state} />
                            </span>
                            <span className="mt-1 block text-xs text-muted-foreground">
                              {item.className || "Class unavailable"}
                              {item.compartmentType ? ` · ${item.compartmentType}` : ""}
                              {` · ${formatCapacity(item.capacity)}`}
                            </span>
                          </span>
                        </label>
                      );
                    })}
                    {train.classes.length === 0 ? (
                      <p className="text-sm text-muted-foreground">
                        No normalized classes were returned for this train.
                      </p>
                    ) : null}
                  </div>
                </div>
              ))}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Confirm monitor</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <FormField id="mrbilitLabel" label="Monitor label" error={fieldErrors.label} required>
                <Input
                  {...fieldProps("mrbilitLabel", fieldErrors.label)}
                  value={label}
                  onChange={(event) => setLabel(event.target.value)}
                  maxLength={100}
                />
              </FormField>
              <div className="grid gap-3 text-sm sm:grid-cols-2">
                <div>
                  <p className="text-xs font-medium text-muted-foreground">Route</p>
                  <p>
                    {result.route.originName || result.slug} → {result.route.destinationName || ""}
                  </p>
                </div>
                <div>
                  <p className="text-xs font-medium text-muted-foreground">Departure</p>
                  <p>
                    <DateDisplay value={result.departureDate} /> · Jalali {result.jalaliDate}
                  </p>
                </div>
                <div>
                  <p className="text-xs font-medium text-muted-foreground">Passengers</p>
                  <p>
                    {result.passengers.adults} adults, {result.passengers.children} children, {result.passengers.infants} infants
                  </p>
                </div>
                <div>
                  <p className="text-xs font-medium text-muted-foreground">Polling interval</p>
                  <p>Every {settings.pollIntervalSeconds} seconds (global setting)</p>
                </div>
                <div>
                  <p className="text-xs font-medium text-muted-foreground">Telegram</p>
                  <p>
                    {settings.telegram.configured
                      ? "Configured"
                      : settings.telegram.available
                        ? "Not configured"
                        : "Unavailable"}
                  </p>
                </div>
                <div>
                  <p className="text-xs font-medium text-muted-foreground">Browser Push</p>
                  <p>
                    {!pushConfig.available
                      ? "Unavailable"
                      : pushConfig.enabled
                        ? `Enabled · ${pushConfig.subscriptions.length} device${pushConfig.subscriptions.length === 1 ? "" : "s"}`
                        : "Available but disabled"}
                  </p>
                </div>
              </div>
              <p className="text-xs text-muted-foreground">
                Notification and interval settings are global. Change them in <Link to="/settings" className="underline">Settings</Link>.
              </p>

              <div>
                <p className="text-xs font-medium text-muted-foreground">
                  Selected options ({selectedCount})
                </p>
                {selectedCount === 0 ? (
                  <p className="text-sm text-muted-foreground">
                    Select one or more classes above.
                  </p>
                ) : (
                  <ul className="mt-1 space-y-1 text-sm">
                    {selectedOptions.map((option) => (
                      <li key={optionKey(option.journeyKey, option.offeringKey)}>
                        Train {option.trainNumber || option.journeyNativeId} · {option.wagonName || option.className}
                      </li>
                    ))}
                  </ul>
                )}
                {fieldErrors.selections ? (
                  <p className="mt-1 text-xs font-medium text-destructive" role="alert">
                    {fieldErrors.selections}
                  </p>
                ) : null}
              </div>

              {createError ? (
                <Alert variant="destructive">
                  <AlertTriangle aria-hidden="true" />
                  <AlertTitle>Monitor not created</AlertTitle>
                  <AlertDescription>{createError}</AlertDescription>
                </Alert>
              ) : null}

              <Button
                type="button"
                onClick={handleCreate}
                disabled={submitting || selectedCount === 0}
              >
                {submitting ? <Loader2 className="animate-spin" aria-hidden="true" /> : null}
                {submitting ? "Creating…" : "Create monitor"}
              </Button>
            </CardContent>
          </Card>
        </>
      ) : null}
    </div>
  );
}
