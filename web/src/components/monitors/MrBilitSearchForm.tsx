import { useEffect, useMemo, useRef, useState } from "react";
import {
  AlertTriangle,
  ArrowRight,
  Info,
  PencilLine,
  RotateCcw,
  TrainFront,
  Users,
} from "lucide-react";
import { isApiError } from "@/api/client";
import { providersApi } from "@/api/endpoints";
import type {
  MonitorCreateRequest,
  ProviderInfo,
  ProviderSearchClass,
  ProviderSearchResult,
  PushConfig,
  SelectedOption,
  Settings,
} from "@/api/types";
import { DateDisplay } from "@/components/JalaliDatePicker";
import {
  MonitorCreateStepper,
  type MonitorCreateStep,
} from "@/components/monitors/MonitorCreateStepper";
import {
  MonitorSearchPanel,
  type SearchErrorDisplay,
} from "@/components/monitors/MonitorSearchPanel";
import { MonitorSearchResults } from "@/components/monitors/MonitorSearchResults";
import { MonitorSummaryPanel } from "@/components/monitors/MonitorSummaryPanel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import {
  applySearchView,
  countSelectedOutsideView,
  formatPassengerSummary,
  groupSearchTrains,
  groupSelectedWagons,
  parsePriceBound,
  passengerCountsValid,
  searchInputSignature,
  selectionKey,
  toSelectedOption,
  wagonSelectionState,
  type SearchInputs,
  type SearchJourneyGroup,
  type SearchSort,
  type SearchWarningSummary,
  type SearchWagonGroup,
} from "@/lib/searchResults";
import {
  defaultMrBilitMonitorLabel,
  describeMrBilitWarnings,
  mrBilitDestinationName,
  mrBilitOriginName,
  parsePassengerCountsFromUrl,
  type PassengerSource,
  type UrlPassengerResult,
} from "@/providers/mrbilit";

interface MrBilitSearchFormProps {
  provider: ProviderInfo;
  settings: Settings;
  pushConfig: PushConfig;
  onSubmit: (payload: MonitorCreateRequest) => Promise<void>;
}

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

function searchErrorDisplay(
  caught: unknown,
  providerName: string,
): SearchErrorDisplay {
  if (!isApiError(caught)) {
    return {
      title: "Search failed",
      message: `The ${providerName} search could not be completed.`,
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
      return { title: `${providerName} timed out`, message: caught.message };
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
  const origin = mrBilitOriginName(result);
  const destination = mrBilitDestinationName(result);
  return (
    <div className="rounded-lg border bg-card/95 px-4 py-3 backdrop-blur supports-[backdrop-filter]:bg-card/85 xl:sticky xl:top-4 xl:z-10">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 items-start gap-2.5">
          <TrainFront
            className="mt-0.5 size-4 shrink-0 text-muted-foreground"
            aria-hidden="true"
          />
          <div className="min-w-0">
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
            <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
              <DateDisplay value={result.departureDate} />
              <span aria-hidden="true">·</span>
              <span className="inline-flex items-center gap-1">
                <Users className="size-3" aria-hidden="true" />
                {formatPassengerSummary(result.passengers)}
              </span>
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
        </div>
        <Button
          type="button"
          variant="ghost"
          size="sm"
          className="h-9"
          onClick={onEdit}
        >
          <PencilLine data-icon="inline-start" aria-hidden="true" />
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
      className="flex flex-col gap-3 rounded-lg border border-warning/40 bg-warning/10 px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
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
        className="h-9 w-fit shrink-0"
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
    <details className="rounded-lg border border-dashed bg-muted/20 px-4 py-3 text-xs">
      <summary className="flex w-fit cursor-pointer items-center gap-2 font-medium">
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

function SearchResultsSkeleton({ providerName }: { providerName: string }) {
  return (
    <div
      className="flex flex-col gap-4"
      role="status"
      aria-live="polite"
      aria-label={`Searching ${providerName}`}
    >
      <span className="sr-only">Searching {providerName}…</span>
      <div className="flex flex-col gap-2">
        <Skeleton className="h-5 w-40" />
        <Skeleton className="h-3 w-64 max-w-full" />
      </div>
      <div className="rounded-xl border bg-card p-4">
        <div className="flex items-start gap-3">
          <Skeleton className="size-9 rounded-lg" />
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton className="h-4 w-32" />
            <Skeleton className="h-3 w-48 max-w-full" />
          </div>
        </div>
        <div className="mt-4 flex flex-col divide-y rounded-lg border">
          {[0, 1, 2].map((index) => (
            <div key={index} className="flex items-center gap-3 px-3 py-3.5">
              <Skeleton className="size-4 rounded" />
              <div className="flex flex-1 flex-col gap-2">
                <Skeleton className="h-3.5 w-36 max-w-full" />
                <Skeleton className="h-3 w-52 max-w-full" />
              </div>
              <Skeleton className="h-5 w-16 rounded-full" />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export function MrBilitSearchForm({
  provider,
  settings,
  pushConfig,
  onSubmit,
}: MrBilitSearchFormProps) {
  const providerName = provider.displayName;
  const [url, setUrl] = useState("");
  const [adults, setAdults] = useState(1);
  const [children, setChildren] = useState(0);
  const [infants, setInfants] = useState(0);
  const [passengerSource, setPassengerSource] =
    useState<PassengerSource>("default");
  const [passengerEditorOpen, setPassengerEditorOpen] = useState(false);
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
  const [priceMin, setPriceMin] = useState("");
  const [priceMax, setPriceMax] = useState("");
  const [includeUnpriced, setIncludeUnpriced] = useState(false);
  const [sort, setSort] = useState<SearchSort>("departure");
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
    () => (result ? describeMrBilitWarnings(result.warnings) : null),
    [result],
  );
  const minBound = parsePriceBound(priceMin);
  const maxBound = parsePriceBound(priceMax);
  let priceError: string | undefined;
  if (minBound.invalid || maxBound.invalid) {
    priceError = "Enter whole numbers without a currency or unit.";
  } else if (
    minBound.value !== null &&
    maxBound.value !== null &&
    minBound.value > maxBound.value
  ) {
    priceError = "Minimum price must not exceed maximum price.";
  }
  const activePriceFilter = priceError
    ? false
    : minBound.value !== null || maxBound.value !== null;
  const view = useMemo(
    () =>
      applySearchView(groups, {
        priceMin: priceError ? null : minBound.value,
        priceMax: priceError ? null : maxBound.value,
        includeUnpriced,
        sort,
      }),
    [groups, priceError, minBound.value, maxBound.value, includeUnpriced, sort],
  );
  const selectedOutsideView = countSelectedOutsideView(
    view.groups,
    selected,
  );
  const selectedOptions = useMemo(() => {
    const options: SelectedOption[] = [];
    for (const journey of groups) {
      for (const wagon of journey.wagons) {
        for (const option of wagon.offerings) {
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
  const selectedWagonCount = useMemo(
    () => groupSelectedWagons(groups, selected).length,
    [groups, selected],
  );
  const summaryResult =
    result && result.trains.length > 0 ? result : null;
  const hasSelectableOptions = groups.some(
    (journey) => journey.summary.optionCount > 0,
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
      setPassengerSource(
        parsed.defaultedKeys.length > 0 ? "url-defaults" : "url",
      );
      setPassengerEditorOpen(false);
      setFieldErrors((current) => {
        if (
          !current.adults &&
          !current.children &&
          !current.infants &&
          !current.passengers
        ) {
          return current;
        }
        const nextErrors = { ...current };
        delete nextErrors.adults;
        delete nextErrors.children;
        delete nextErrors.infants;
        delete nextErrors.passengers;
        return nextErrors;
      });
    } else if (parsed.status === "absent") {
      if (passengerSource !== "manual") {
        setAdults(1);
        setChildren(0);
        setInfants(0);
        setPassengerSource("default");
        setPassengerEditorOpen(false);
      }
    }
    invalidateSearch();
  }

  function handleTogglePassengerEditor() {
    setPassengerEditorOpen((open) => !open);
  }

  function handleAdultsChange(value: number) {
    setAdults(value);
    setPassengerSource("manual");
    setPassengerEditorOpen(true);
    invalidateSearch();
  }

  function handleChildrenChange(value: number) {
    setChildren(value);
    setPassengerSource("manual");
    setPassengerEditorOpen(true);
    invalidateSearch();
  }

  function handleInfantsChange(value: number) {
    setInfants(value);
    setPassengerSource("manual");
    setPassengerEditorOpen(true);
    invalidateSearch();
  }

  async function handleSearch() {
    if (searching) return;
    const currentUrl = url.trim();
    const errors = passengerErrors(adults, children, infants);
    if (!currentUrl) {
      errors.url = `Paste a ${providerName} train search URL.`;
    } else if (
      urlPassenger.status === "invalid" &&
      passengerSource !== "manual"
    ) {
      errors.passengers =
        "Fix the passenger counts in the URL or edit them manually.";
    }
    if (Object.keys(errors).length > 0) {
      if (
        errors.adults ||
        errors.children ||
        errors.infants ||
        errors.passengers
      ) {
        setPassengerEditorOpen(true);
      }
      setFieldErrors(errors);
      setSearchError(null);
      setSearchExpanded(true);
      setPendingFocus(errors.url ? "url" : firstPassengerFocus(errors));
      return;
    }
    if (!passengerCountsValid({ adults, children, infants })) {
      setPassengerEditorOpen(true);
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
    setPriceMin("");
    setPriceMax("");
    setIncludeUnpriced(false);

    const request: SearchInputs = {
      url: currentUrl,
      adults,
      children,
      infants,
    };
    try {
      const next = await providersApi.search(
        provider.id,
        request,
        controller.signal,
      );
      if (revision !== revisionRef.current) return;
      setResult(next);
      setLastSearchSignature(signature);
      setSearchExpanded(false);
      setSelected({});
      if (!labelEdited) {
        setLabel(defaultMrBilitMonitorLabel(next));
      }
      setPendingFocus("results");
    } catch (caught) {
      if (revision !== revisionRef.current || controller.signal.aborted) return;
      setResult(null);
      setLastSearchSignature(null);
      if (isApiError(caught)) {
        setFieldErrors(caught.fields);
        setSearchError(searchErrorDisplay(caught, providerName));
        if (
          caught.fields.adults ||
          caught.fields.children ||
          caught.fields.infants ||
          caught.fields.passengers
        ) {
          setPassengerEditorOpen(true);
        }
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
        setSearchError(searchErrorDisplay(caught, providerName));
        setPendingFocus("search-error");
      }
    } finally {
      if (revision === revisionRef.current) {
        setSearching(false);
        abortRef.current = null;
      }
    }
  }

  function clearSelectionErrors() {
    setFieldErrors((current) => {
      if (!current.selections) return current;
      const next = { ...current };
      delete next.selections;
      return next;
    });
    setCreateError("");
  }

  function handleToggleWagon(
    journey: SearchJourneyGroup,
    wagon: SearchWagonGroup,
  ) {
    if (stale || searching || submitting) return;
    const state = wagonSelectionState(
      journey.train.journeyKey,
      wagon,
      selected,
    );
    setSelected((current) => {
      const next = { ...current };
      for (const offering of wagon.offerings) {
        const key = selectionKey(
          journey.train.journeyKey,
          offering.offeringKey,
        );
        if (state === "all") {
          delete next[key];
        } else {
          next[key] = toSelectedOption(journey.train, offering);
        }
      }
      return next;
    });
    clearSelectionErrors();
  }

  function handleToggleOffering(
    journey: SearchJourneyGroup,
    option: ProviderSearchClass,
  ) {
    if (stale || searching || submitting) return;
    const key = selectionKey(
      journey.train.journeyKey,
      option.offeringKey,
    );
    setSelected((current) => {
      const next = { ...current };
      if (next[key]) {
        delete next[key];
      } else {
        next[key] = toSelectedOption(journey.train, option);
      }
      return next;
    });
    clearSelectionErrors();
  }

  function handleClearPrice() {
    setPriceMin("");
    setPriceMax("");
    setIncludeUnpriced(false);
  }

  function handleReview() {
    document
      .getElementById("monitor-summary")
      ?.scrollIntoView({ behavior: "smooth", block: "start" });
    setPendingFocus("label");
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
      originName: mrBilitOriginName(result),
      destinationName: mrBilitDestinationName(result),
      departureDate: result.departureDate,
      adults: result.passengers.adults,
      children: result.passengers.children,
      infants: result.passengers.infants,
      providers: [
        {
          providerId: provider.id,
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

  return (
    <div className="flex flex-col gap-6 pb-24 xl:pb-8">
      <MonitorCreateStepper current={currentStep} />
      <div className="grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_22rem] 2xl:grid-cols-[minmax(0,1fr)_24rem]">
        <div className="flex min-w-0 flex-col gap-5">
          {searchExpanded || !result ? (
            <MonitorSearchPanel
              providerName={providerName}
              url={url}
              adults={adults}
              children={children}
              infants={infants}
              passengerSource={passengerSource}
              passengerEditorOpen={passengerEditorOpen}
              urlPassenger={urlPassenger}
              fieldErrors={fieldErrors}
              searching={searching}
              searchError={searchError}
              onUrlChange={handleUrlChange}
              onAdultsChange={handleAdultsChange}
              onChildrenChange={handleChildrenChange}
              onInfantsChange={handleInfantsChange}
              onTogglePassengerEditor={handleTogglePassengerEditor}
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
            <SearchResultsSkeleton providerName={providerName} />
          ) : result && result.trains.length === 0 ? (
            <div className="flex flex-col gap-4 rounded-lg border bg-card p-5 sm:flex-row sm:items-center sm:justify-between">
              <div className="flex items-start gap-3">
                <TrainFront
                  className="mt-0.5 size-4 shrink-0 text-muted-foreground"
                  aria-hidden="true"
                />
                <div className="min-w-0">
                  <h2
                    id="search-results-heading"
                    tabIndex={-1}
                    className="scroll-mt-24 text-lg font-semibold tracking-tight focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
                  >
                    No trains returned
                  </h2>
                  <p className="mt-1 max-w-2xl text-xs leading-relaxed text-muted-foreground">
                    {providerName} returned no trains for this route and date.
                    This is a successful empty result, not a sold-out signal.
                    Try another date or URL.
                  </p>
                </div>
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="h-9 w-fit shrink-0"
                onClick={handleEditSearch}
              >
                Change search
              </Button>
            </div>
          ) : result && result.trains.length > 0 && groups.length === 0 ? (
            <div className="flex flex-col gap-2 rounded-lg border bg-card p-5">
              <h2
                id="search-results-heading"
                tabIndex={-1}
                className="scroll-mt-24 text-lg font-semibold tracking-tight focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
              >
                No fare options returned
              </h2>
              <p className="max-w-2xl text-xs leading-relaxed text-muted-foreground">
                {providerName} returned the train but no usable fare or class
                offerings. This is not a sold-out verdict. Try another search
                or date.
              </p>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="mt-1 h-9 w-fit"
                onClick={handleEditSearch}
              >
                Change search
              </Button>
            </div>
          ) : groups.length > 0 ? (
            <MonitorSearchResults
              groups={view.groups}
              allGroups={groups}
              view={view}
              selected={selected}
              disabled={stale || submitting}
              priceMin={priceMin}
              priceMax={priceMax}
              includeUnpriced={includeUnpriced}
              priceError={priceError}
              selectedOutsideView={selectedOutsideView}
              sort={sort}
              activePriceFilter={activePriceFilter}
              onPriceMinChange={setPriceMin}
              onPriceMaxChange={setPriceMax}
              onIncludeUnpricedChange={setIncludeUnpriced}
              onClearPrice={handleClearPrice}
              onSortChange={setSort}
              onToggleWagon={handleToggleWagon}
              onToggleOffering={handleToggleOffering}
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
          searching={searching}
          emptyResult={Boolean(result && result.trains.length === 0)}
          settings={settings}
          pushConfig={pushConfig}
          onLabelChange={handleLabelChange}
          onCreate={handleCreate}
        />
      </div>

      {summaryResult && selectedWagonCount > 0 && !stale ? (
        <div className="fixed inset-x-0 bottom-0 z-20 border-t bg-background/95 px-4 py-3 backdrop-blur xl:hidden">
          <div className="mx-auto flex max-w-6xl items-center justify-between gap-3">
            <span className="text-xs text-muted-foreground">
              {selectedWagonCount} wagon
              {selectedWagonCount === 1 ? "" : "s"} · {selectedCount} selected
            </span>
            <Button type="button" onClick={handleReview}>
              Review &amp; create
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  );
}
