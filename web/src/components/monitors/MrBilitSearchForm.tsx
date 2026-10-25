import { useEffect, useMemo, useRef, useState } from "react";
import {
  AlertTriangle,
  ArrowRight,
  Info,
  RotateCcw,
  TrainFront,
} from "lucide-react";
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
import { EmptyState } from "@/components/layout/EmptyState";
import { MonitorCreateStepper, type MonitorCreateStep } from "@/components/monitors/MonitorCreateStepper";
import {
  MonitorSearchPanel,
  type SearchErrorDisplay,
} from "@/components/monitors/MonitorSearchPanel";
import { MonitorSearchResults } from "@/components/monitors/MonitorSearchResults";
import { MonitorSummaryPanel } from "@/components/monitors/MonitorSummaryPanel";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  defaultMonitorLabel,
  describeSearchWarnings,
  formatPassengerSummary,
  groupSearchTrains,
  parsePassengerCountsFromUrl,
  passengerCountsValid,
  resultDestinationName,
  resultOriginName,
  searchInputSignature,
  selectionKey,
  toSelectedOption,
  type SearchInputs,
  type SearchWarningSummary,
  type UrlPassengerResult,
} from "@/lib/mrbilitSearch";

interface MrBilitSearchFormProps {
  providers: ProviderInfo[];
  settings: Settings;
  pushConfig: PushConfig;
  onSubmit: (payload: MonitorCreateRequest) => Promise<void>;
}

type PassengerSource = "url" | "manual";

type FocusTarget =
  | "url"
  | "adults"
  | "children"
  | "infants"
  | "search-error"
  | "results"
  | "label"
  | "selections"
  | "create-error";

function focusElement(target: FocusTarget) {
  const ids: Record<FocusTarget, string> = {
    url: "mrbilitUrl",
    adults: "mrbilitAdults",
    children: "mrbilitChildren",
    infants: "mrbilitInfants",
    "search-error": "search-error",
    results: "search-results-heading",
    label: "mrbilitLabel",
    selections: "search-results-heading",
    "create-error": "create-error",
  };
  const element = document.getElementById(ids[target]);
  if (element instanceof HTMLElement) {
    element.focus();
  }
}

function searchErrorDisplay(caught: unknown): SearchErrorDisplay {
  if (!isApiError(caught)) {
    return {
      title: "Search failed",
      message: "The MrBilit search could not be completed.",
    };
  }
  switch (caught.code) {
    case "url_required":
    case "unsupported_url":
    case "invalid_url":
    case "missing_date":
    case "invalid_date":
    case "past_date":
      return { title: "Check the search URL", message: caught.message };
    case "route_not_found":
      return { title: "No matching route found", message: caught.message };
    case "route_resolution_failed":
      return { title: "Could not resolve this route", message: caught.message };
    case "provider_timeout":
      return { title: "MrBilit timed out", message: caught.message };
    case "provider_rate_limited":
      return { title: "Too many searches", message: caught.message };
    case "provider_configuration":
      return { title: "Search is not configured", message: caught.message };
    case "provider_error":
      return {
        title: "Could not complete the search",
        message: caught.message,
      };
    case "network_error":
      return { title: "Network error", message: caught.message };
    default:
      return { title: "Search failed", message: caught.message };
  }
}

function passengerErrors(
  adults: number,
  children: number,
  infants: number,
): Record<string, string> {
  const errors: Record<string, string> = {};
  if (!Number.isInteger(adults) || adults < 1 || adults > 9) {
    errors.adults = "Adults must be between 1 and 9.";
  }
  if (!Number.isInteger(children) || children < 0 || children > 9) {
    errors.children = "Children must be between 0 and 9.";
  }
  if (!Number.isInteger(infants) || infants < 0 || infants > 9) {
    errors.infants = "Infants must be between 0 and 9.";
  }
  if (
    Object.keys(errors).length === 0 &&
    adults + children + infants > 9
  ) {
    errors.passengers = "Total passengers must be at most 9.";
  }
  return errors;
}

function firstPassengerFocus(errors: Record<string, string>): FocusTarget {
  if (errors.adults) return "adults";
  if (errors.children) return "children";
  if (errors.infants) return "infants";
  return "adults";
}

function SearchContextBar({
  result,
  stale,
  onEdit,
}: {
  result: ProviderSearchResult;
  stale: boolean;
  onEdit: () => void;
}) {
  const origin = resultOriginName(result);
  const destination = resultDestinationName(result);
  return (
    <div className="rounded-xl border bg-card p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1">
          <p
            dir="ltr"
            className="flex min-w-0 flex-wrap items-center gap-1.5 text-sm font-semibold"
          >
            <bdi dir="auto" className="min-w-0 break-words">
              {origin}
            </bdi>
            {destination ? (
              <>
                <ArrowRight
                  className="size-3.5 shrink-0 text-muted-foreground"
                  aria-hidden="true"
                />
                <bdi dir="auto" className="min-w-0 break-words">
                  {destination}
                </bdi>
              </>
            ) : null}
          </p>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
            <DateDisplay value={result.departureDate} />
            <span aria-hidden="true">·</span>
            <span>{formatPassengerSummary(result.passengers)}</span>
            <span aria-hidden="true">·</span>
            <span>
              {result.trains.length === 0
                ? "No trains"
                : `${result.trains.length} train${
                    result.trains.length === 1 ? "" : "s"
                  }`}
            </span>
            {stale ? <Badge variant="warning">Out of date</Badge> : null}
          </div>
        </div>
        <Button type="button" variant="outline" size="sm" onClick={onEdit}>
          Change search
        </Button>
      </div>
    </div>
  );
}

function StaleNotice({ onSearch }: { onSearch: () => void }) {
  return (
    <div
      id="search-stale-notice"
      role="status"
      className="flex flex-col gap-3 rounded-xl border border-warning/40 bg-warning/10 p-4 sm:flex-row sm:items-center sm:justify-between"
    >
      <div className="flex items-start gap-2.5">
        <AlertTriangle
          className="mt-0.5 size-4 shrink-0 text-warning-foreground dark:text-warning"
          aria-hidden="true"
        />
        <div>
          <p className="text-sm font-medium">Search settings changed</p>
          <p className="mt-0.5 text-xs leading-relaxed text-muted-foreground">
            These results no longer match the current URL or passenger counts.
            Search again to refresh them.
          </p>
        </div>
      </div>
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="w-fit shrink-0"
        onClick={onSearch}
      >
        <RotateCcw aria-hidden="true" />
        Search again
      </Button>
    </div>
  );
}

function ProviderNotes({ warnings }: { warnings: SearchWarningSummary }) {
  if (warnings.notices.length === 0 && warnings.diagnostics.length === 0) {
    return null;
  }
  return (
    <details className="rounded-xl border bg-card px-4 py-3 text-xs">
      <summary className="flex cursor-pointer items-center gap-2 font-medium">
        <Info className="size-3.5 text-muted-foreground" aria-hidden="true" />
        Provider notes
        <span className="text-muted-foreground">
          ({warnings.notices.length + warnings.diagnostics.length})
        </span>
      </summary>
      <div className="mt-2 flex flex-col gap-1.5 text-muted-foreground">
        {warnings.notices.map((notice) => (
          <p key={notice} className="leading-relaxed">
            {notice}
          </p>
        ))}
        {warnings.diagnostics.length > 0 ? (
          <p className="font-mono text-[0.7rem] break-words">
            Diagnostics: {warnings.diagnostics.join(", ")}
          </p>
        ) : null}
      </div>
    </details>
  );
}

function SearchResultsSkeleton() {
  return (
    <div
      className="flex flex-col gap-4"
      role="status"
      aria-live="polite"
      aria-label="Searching MrBilit"
    >
      <span className="sr-only">Searching MrBilit…</span>
      <div className="flex flex-col gap-2">
        <Skeleton className="h-5 w-40" />
        <Skeleton className="h-3 w-64 max-w-full" />
      </div>
      {[0, 1].map((index) => (
        <div key={index} className="rounded-xl border bg-card p-4">
          <div className="flex items-start gap-3">
            <Skeleton className="size-9 rounded-lg" />
            <div className="flex flex-1 flex-col gap-2">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="h-3 w-48 max-w-full" />
            </div>
          </div>
          <div className="mt-4 flex flex-col gap-2">
            <Skeleton className="h-10 w-full rounded-lg" />
            <Skeleton className="h-10 w-full rounded-lg" />
          </div>
        </div>
      ))}
    </div>
  );
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
  const [passengerSource, setPassengerSource] =
    useState<PassengerSource>("manual");
  const [result, setResult] = useState<ProviderSearchResult | null>(null);
  const [lastSearchSignature, setLastSearchSignature] = useState<string | null>(
    null,
  );
  const [selected, setSelected] = useState<Record<string, SelectedOption>>({});
  const [label, setLabel] = useState("");
  const [labelEdited, setLabelEdited] = useState(false);
  const [searching, setSearching] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [searchExpanded, setSearchExpanded] = useState(true);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [searchError, setSearchError] = useState<SearchErrorDisplay | null>(
    null,
  );
  const [createError, setCreateError] = useState("");
  const [pendingFocus, setPendingFocus] = useState<FocusTarget | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const revisionRef = useRef(0);

  const urlPassenger: UrlPassengerResult = useMemo(
    () => parsePassengerCountsFromUrl(url),
    [url],
  );
  const currentSignature = searchInputSignature({
    url,
    adults,
    children,
    infants,
  });
  const stale = Boolean(
    result && lastSearchSignature && lastSearchSignature !== currentSignature,
  );
  const groups = useMemo(
    () => (result ? groupSearchTrains(result.trains) : []),
    [result],
  );
  const warnings = useMemo(
    () => (result ? describeSearchWarnings(result.warnings) : null),
    [result],
  );
  const selectedOptions = useMemo(() => {
    const options: SelectedOption[] = [];
    for (const journey of groups) {
      for (const family of journey.families) {
        for (const option of family.options) {
          const value =
            selected[
              selectionKey(journey.train.journeyKey, option.offeringKey)
            ];
          if (value) options.push(value);
        }
      }
    }
    return options;
  }, [groups, selected]);
  const selectedCount = selectedOptions.length;
  const summaryResult =
    result && result.trains.length > 0 ? result : null;
  const hasSelectableOptions = groups.some(
    (journey) => journey.optionCount > 0,
  );
  const currentStep: MonitorCreateStep =
    !hasSelectableOptions || stale ? 1 : selectedCount > 0 ? 3 : 2;

  useEffect(() => {
    if (!pendingFocus) return;
    focusElement(pendingFocus);
    setPendingFocus(null);
  }, [pendingFocus]);

  useEffect(
    () => () => {
      revisionRef.current += 1;
      abortRef.current?.abort();
    },
    [],
  );

  function invalidateSearch() {
    revisionRef.current += 1;
    abortRef.current?.abort();
    abortRef.current = null;
    setSearching(false);
    setSelected({});
    setFieldErrors({});
    setSearchError(null);
    setCreateError("");
    setSearchExpanded(true);
  }

  function handleUrlChange(next: string) {
    const parsed = parsePassengerCountsFromUrl(next);
    setUrl(next);
    if (parsed.status === "applied" && parsed.counts) {
      setAdults(parsed.counts.adults);
      setChildren(parsed.counts.children);
      setInfants(parsed.counts.infants);
      setPassengerSource("url");
    } else {
      setPassengerSource("manual");
    }
    invalidateSearch();
  }

  function handleAdultsChange(value: number) {
    setAdults(value);
    setPassengerSource("manual");
    invalidateSearch();
  }

  function handleChildrenChange(value: number) {
    setChildren(value);
    setPassengerSource("manual");
    invalidateSearch();
  }

  function handleInfantsChange(value: number) {
    setInfants(value);
    setPassengerSource("manual");
    invalidateSearch();
  }

  async function handleSearch() {
    if (searching) return;
    const currentUrl = url.trim();
    const errors = passengerErrors(adults, children, infants);
    if (!currentUrl) {
      errors.url = "Paste a MrBilit train search URL.";
    }
    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);
      setSearchError(null);
      setSearchExpanded(true);
      setPendingFocus(errors.url ? "url" : firstPassengerFocus(errors));
      return;
    }
    if (!passengerCountsValid({ adults, children, infants })) {
      setFieldErrors({ passengers: "Passengers must be between 1 and 9 total." });
      setSearchError(null);
      setSearchExpanded(true);
      setPendingFocus("adults");
      return;
    }

    const signature = searchInputSignature({
      url: currentUrl,
      adults,
      children,
      infants,
    });
    revisionRef.current += 1;
    const revision = revisionRef.current;
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    setSearching(true);
    setResult(null);
    setLastSearchSignature(null);
    setSelected({});
    setFieldErrors({});
    setSearchError(null);
    setCreateError("");
    setSearchExpanded(true);

    const request: SearchInputs = {
      url: currentUrl,
      adults,
      children,
      infants,
    };
    try {
      const next = await providersApi.search(
        "mrbilit",
        request,
        controller.signal,
      );
      if (revision !== revisionRef.current) return;
      setResult(next);
      setLastSearchSignature(signature);
      setSearchExpanded(false);
      setSelected({});
      if (!labelEdited) {
        setLabel(defaultMonitorLabel(next));
      }
      setPendingFocus("results");
    } catch (caught) {
      if (revision !== revisionRef.current || controller.signal.aborted) return;
      setResult(null);
      setLastSearchSignature(null);
      if (isApiError(caught)) {
        setFieldErrors(caught.fields);
        setSearchError(searchErrorDisplay(caught));
        if (caught.fields.url) {
          setPendingFocus("url");
        } else if (caught.fields.adults) {
          setPendingFocus("adults");
        } else if (caught.fields.children) {
          setPendingFocus("children");
        } else if (caught.fields.infants) {
          setPendingFocus("infants");
        } else if (caught.fields.passengers) {
          setPendingFocus("adults");
        } else {
          setPendingFocus("search-error");
        }
      } else {
        setSearchError(searchErrorDisplay(caught));
        setPendingFocus("search-error");
      }
    } finally {
      if (revision === revisionRef.current) {
        setSearching(false);
        abortRef.current = null;
      }
    }
  }

  function handleToggle(
    train: ProviderSearchTrain,
    option: ProviderSearchClass,
  ) {
    if (stale || searching || submitting) return;
    const key = selectionKey(train.journeyKey, option.offeringKey);
    setSelected((current) => {
      const next = { ...current };
      if (next[key]) {
        delete next[key];
      } else {
        next[key] = toSelectedOption(train, option);
      }
      return next;
    });
    setFieldErrors((current) => {
      if (!current.selections) return current;
      const next = { ...current };
      delete next.selections;
      return next;
    });
    setCreateError("");
  }

  function handleLabelChange(value: string) {
    setLabel(value);
    setLabelEdited(true);
    setCreateError("");
    setFieldErrors((current) => {
      if (!current.label) return current;
      const next = { ...current };
      delete next.label;
      return next;
    });
  }

  function handleEditSearch() {
    setSearchExpanded(true);
    setPendingFocus("url");
  }

  async function handleCreate() {
    if (submitting) return;
    if (!result) {
      setCreateError("Search for trains before creating a monitor.");
      setPendingFocus("create-error");
      return;
    }
    if (stale) {
      setCreateError("Search again before creating this monitor.");
      setPendingFocus("create-error");
      return;
    }
    if (!label.trim()) {
      setFieldErrors({ label: "Enter a monitor label." });
      setPendingFocus("label");
      return;
    }
    if (selectedCount === 0) {
      setFieldErrors({ selections: "Select at least one train and class." });
      setPendingFocus("selections");
      return;
    }

    const payload: MonitorCreateRequest = {
      label: label.trim(),
      originName: resultOriginName(result),
      destinationName: resultDestinationName(result),
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
    setCreateError("");
    setFieldErrors({});
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
        setCreateError(caught.message || "The monitor could not be created.");
        if (nextFields.label) {
          setPendingFocus("label");
        } else if (nextFields.selections) {
          setPendingFocus("selections");
        } else {
          setPendingFocus("create-error");
        }
      } else {
        setCreateError("The monitor could not be created.");
        setPendingFocus("create-error");
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
    <div className="flex flex-col gap-6">
      <MonitorCreateStepper current={currentStep} />
      <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_22rem] xl:grid-cols-[minmax(0,1fr)_24rem]">
        <div className="flex min-w-0 flex-col gap-5">
          {searchExpanded || !result ? (
            <MonitorSearchPanel
              url={url}
              adults={adults}
              children={children}
              infants={infants}
              passengerSource={passengerSource}
              urlPassenger={urlPassenger}
              fieldErrors={fieldErrors}
              searching={searching}
              searchError={searchError}
              onUrlChange={handleUrlChange}
              onAdultsChange={handleAdultsChange}
              onChildrenChange={handleChildrenChange}
              onInfantsChange={handleInfantsChange}
              onSearch={handleSearch}
            />
          ) : result ? (
            <SearchContextBar
              result={result}
              stale={stale}
              onEdit={handleEditSearch}
            />
          ) : null}

          {stale ? <StaleNotice onSearch={handleSearch} /> : null}
          {warnings ? <ProviderNotes warnings={warnings} /> : null}

          {searching ? (
            <SearchResultsSkeleton />
          ) : result && result.trains.length === 0 ? (
            <div
              id="search-results-heading"
              tabIndex={-1}
              className="scroll-mt-24 rounded-xl focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
            >
              <EmptyState
                icon={TrainFront}
                title="No trains returned"
                description="MrBilit returned no trains for this route and date. This is a successful empty result, not a sold-out signal. Try another date or URL."
                action={
                  <Button
                    type="button"
                    variant="outline"
                    onClick={handleEditSearch}
                  >
                    Change search
                  </Button>
                }
              />
            </div>
          ) : groups.length > 0 ? (
            <MonitorSearchResults
              groups={groups}
              selected={selected}
              disabled={stale || submitting}
              onToggle={handleToggle}
            />
          ) : null}
        </div>

        <MonitorSummaryPanel
          result={summaryResult}
          groups={groups}
          selected={selected}
          label={label}
          labelError={fieldErrors.label}
          selectionError={fieldErrors.selections}
          createError={createError}
          submitting={submitting}
          stale={stale}
          settings={settings}
          pushConfig={pushConfig}
          onLabelChange={handleLabelChange}
          onCreate={handleCreate}
        />
      </div>
    </div>
  );
}
