import {
  AlertTriangle,
  Check,
  ChevronUp,
  Info,
  Link2,
  Loader2,
  Pencil,
  Search,
  Users,
  type LucideIcon,
} from "lucide-react";
import type {
  PassengerSource,
  UrlPassengerResult,
} from "@/providers/mrbilit";
import { formatPassengerSummary } from "@/lib/searchResults";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { FormField, fieldProps } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";

export interface SearchErrorDisplay {
  title: string;
  message: string;
}

interface MonitorSearchPanelProps {
  providerName: string;
  url: string;
  adults: number;
  children: number;
  infants: number;
  passengerSource: PassengerSource;
  passengerEditorOpen: boolean;
  urlPassenger: UrlPassengerResult;
  fieldErrors: Record<string, string>;
  searching: boolean;
  searchError: SearchErrorDisplay | null;
  onUrlChange: (value: string) => void;
  onAdultsChange: (value: number) => void;
  onChildrenChange: (value: number) => void;
  onInfantsChange: (value: number) => void;
  onTogglePassengerEditor: () => void;
  onSearch: () => void;
}

interface PassengerNote {
  icon: LucideIcon;
  text: string;
  warning?: boolean;
}

function passengerNote(
  url: string,
  source: PassengerSource,
  parsed: UrlPassengerResult,
): PassengerNote {
  if (source === "manual") {
    return parsed.status === "invalid"
      ? {
          icon: AlertTriangle,
          text: "Using manually edited counts. The URL counts are invalid and were not applied.",
          warning: true,
        }
      : {
          icon: Info,
          text: "Using manually edited passenger counts for this search.",
        };
  }
  if (parsed.status === "invalid") {
    return {
      icon: AlertTriangle,
      text: "The URL passenger counts are invalid. Fix the URL or edit passengers.",
      warning: true,
    };
  }
  if (parsed.status === "applied") {
    return parsed.defaultedKeys.length > 0
      ? {
          icon: Check,
          text: "Loaded from the URL. Missing counts use the provider's URL defaults.",
        }
      : {
          icon: Check,
          text: "Passenger counts loaded from the URL.",
        };
  }
  if (url.trim()) {
    return {
      icon: Info,
      text: "No passenger counts in this URL. Using defaults (1 adult, 0 children, 0 infants).",
    };
  }
  return {
    icon: Info,
    text: "Defaults are used until the URL provides passenger counts.",
  };
}

const URL_DESCRIPTION =
  "The route and Jalali date are read automatically. Tracking parameters are ignored.";

export function MonitorSearchPanel({
  providerName,
  url,
  adults,
  children,
  infants,
  passengerSource,
  passengerEditorOpen,
  urlPassenger,
  fieldErrors,
  searching,
  searchError,
  onUrlChange,
  onAdultsChange,
  onChildrenChange,
  onInfantsChange,
  onTogglePassengerEditor,
  onSearch,
}: MonitorSearchPanelProps) {
  const note = passengerNote(url, passengerSource, urlPassenger);
  const NoteIcon = note.icon;
  const passengers = { adults, children, infants };

  return (
    <Card
      aria-busy={searching}
      className="gap-0 overflow-hidden rounded-[10px] py-0 shadow-xs"
    >
      <CardHeader className="border-b bg-surface-raised/55 px-4 py-3.5 sm:px-5">
        <p className="eyebrow text-[0.6rem]">Step 1 · Intake</p>
        <CardTitle className="mt-1 text-base" role="heading" aria-level={2}>
          Search {providerName}
        </CardTitle>
        <CardDescription>
          Paste a public {providerName} train search URL. The route, date, and
          passenger counts are read from it.
        </CardDescription>
      </CardHeader>
      <CardContent className="px-4 py-4 sm:px-5 sm:py-5">
        <form
          className="flex flex-col gap-4"
          noValidate
          onSubmit={(event) => {
            event.preventDefault();
            onSearch();
          }}
        >
          <FormField
            id="mrbilitUrl"
            label={`${providerName} search URL`}
            error={fieldErrors.url}
            required
            description={URL_DESCRIPTION}
          >
            <div className="relative">
              <Link2
                className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
                aria-hidden="true"
              />
              <Input
                {...fieldProps("mrbilitUrl", fieldErrors.url, URL_DESCRIPTION)}
                type="url"
                name="mrbilit-search-url"
                inputMode="url"
                autoComplete="off"
                autoCapitalize="off"
                spellCheck={false}
                className="h-11 pl-9 font-mono text-[13px]"
                value={url}
                onChange={(event) => onUrlChange(event.target.value)}
                placeholder="https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24"
              />
            </div>
          </FormField>

          <fieldset className="rounded-lg border bg-muted/15">
            <legend className="sr-only">Passengers</legend>
            <div className="flex flex-wrap items-center justify-between gap-3 px-3 py-2.5">
              <div className="flex min-w-0 items-center gap-2.5">
                <span className="flex size-8 shrink-0 items-center justify-center rounded-md border bg-card text-muted-foreground">
                  <Users className="size-3.5" aria-hidden="true" />
                </span>
                <div className="min-w-0">
                  <p className="text-[0.8125rem] font-medium">
                    {formatPassengerSummary(passengers)}
                  </p>
                  <p
                    className={cn(
                      "mt-0.5 flex items-start gap-1.5 text-[0.7rem] leading-relaxed",
                      note.warning
                        ? "text-warning-foreground dark:text-warning"
                        : "text-muted-foreground",
                    )}
                  >
                    <NoteIcon
                      className="mt-0.5 size-3.5 shrink-0"
                      aria-hidden="true"
                    />
                    <span>{note.text}</span>
                  </p>
                </div>
              </div>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="h-9 shrink-0 max-sm:h-11"
                aria-expanded={passengerEditorOpen}
                aria-controls="mrbilit-passenger-fields"
                onClick={onTogglePassengerEditor}
              >
                {passengerEditorOpen ? (
                  <>
                    <ChevronUp data-icon="inline-start" aria-hidden="true" />
                    Done
                  </>
                ) : (
                  <>
                    <Pencil data-icon="inline-start" aria-hidden="true" />
                    Edit passengers
                  </>
                )}
              </Button>
            </div>

            {passengerEditorOpen ? (
              <div
                id="mrbilit-passenger-fields"
                className="border-t px-3 pt-3.5 pb-3.5"
              >
                <div className="grid gap-3 sm:grid-cols-3">
                  <FormField
                    id="mrbilitAdults"
                    label="Adults"
                    error={fieldErrors.adults}
                    required
                  >
                    <Input
                      {...fieldProps("mrbilitAdults", fieldErrors.adults)}
                      type="number"
                      name="adults"
                      min={1}
                      max={9}
                      step={1}
                      inputMode="numeric"
                      className="h-10 tabular-nums max-sm:h-11"
                      value={adults}
                      onChange={(event) =>
                        onAdultsChange(Number(event.target.value))
                      }
                    />
                  </FormField>
                  <FormField
                    id="mrbilitChildren"
                    label="Children"
                    error={fieldErrors.children}
                  >
                    <Input
                      {...fieldProps("mrbilitChildren", fieldErrors.children)}
                      type="number"
                      name="children"
                      min={0}
                      max={9}
                      step={1}
                      inputMode="numeric"
                      className="h-10 tabular-nums max-sm:h-11"
                      value={children}
                      onChange={(event) =>
                        onChildrenChange(Number(event.target.value))
                      }
                    />
                  </FormField>
                  <FormField
                    id="mrbilitInfants"
                    label="Infants"
                    error={fieldErrors.infants}
                  >
                    <Input
                      {...fieldProps("mrbilitInfants", fieldErrors.infants)}
                      type="number"
                      name="infants"
                      min={0}
                      max={9}
                      step={1}
                      inputMode="numeric"
                      className="h-10 tabular-nums max-sm:h-11"
                      value={infants}
                      onChange={(event) =>
                        onInfantsChange(Number(event.target.value))
                      }
                    />
                  </FormField>
                </div>
                <p className="mt-3 text-xs text-muted-foreground">
                  Up to 9 passengers total. Adults are required; children and
                  infants may be zero.
                </p>
                {fieldErrors.passengers ? (
                  <p
                    className="mt-2 text-xs font-medium text-destructive"
                    role="alert"
                  >
                    {fieldErrors.passengers}
                  </p>
                ) : null}
              </div>
            ) : fieldErrors.passengers ? (
              <p
                className="border-t px-3 py-3 text-xs font-medium text-destructive"
                role="alert"
              >
                {fieldErrors.passengers}
              </p>
            ) : null}
          </fieldset>

          {searchError ? (
            <div
              id="search-error"
              tabIndex={-1}
              className="scroll-mt-24 rounded-lg focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            >
              <Alert variant="destructive">
                <AlertTriangle aria-hidden="true" />
                <AlertTitle>{searchError.title}</AlertTitle>
                <AlertDescription>{searchError.message}</AlertDescription>
              </Alert>
            </div>
          ) : null}

          <div className="flex flex-col gap-3 border-t pt-4 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-xs leading-relaxed text-muted-foreground">
              Searching does not create a monitor or change availability.
            </p>
            <Button
              type="submit"
              size="lg"
              className="w-full sm:w-auto"
              disabled={searching}
            >
              {searching ? (
                <Loader2
                  data-icon="inline-start"
                  className="animate-spin"
                  aria-hidden="true"
                />
              ) : (
                <Search data-icon="inline-start" aria-hidden="true" />
              )}
              {searching ? `Searching ${providerName}…` : "Find trains"}
            </Button>
          </div>
          <div className="sr-only" role="status" aria-live="polite">
            {searching ? `Searching ${providerName}…` : ""}
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
