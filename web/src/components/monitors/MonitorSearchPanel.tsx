import {
  AlertTriangle,
  Check,
  Info,
  Loader2,
  Search,
} from "lucide-react";
import type { UrlPassengerResult } from "@/lib/mrbilitSearch";
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
  url: string;
  adults: number;
  children: number;
  infants: number;
  passengerSource: "url" | "manual";
  urlPassenger: UrlPassengerResult;
  fieldErrors: Record<string, string>;
  searching: boolean;
  searchError: SearchErrorDisplay | null;
  onUrlChange: (value: string) => void;
  onAdultsChange: (value: number) => void;
  onChildrenChange: (value: number) => void;
  onInfantsChange: (value: number) => void;
  onSearch: () => void;
}

function PassengerSourceNote({
  url,
  source,
  parsed,
}: {
  url: string;
  source: "url" | "manual";
  parsed: UrlPassengerResult;
}) {
  const baseClass = "flex items-start gap-1.5 text-xs";
  if (parsed.status === "applied" && source === "url") {
    return (
      <p className={cn(baseClass, "text-muted-foreground")}>
        <Check className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
        Loaded from the URL. You can adjust the counts.
      </p>
    );
  }
  if (parsed.status === "partial") {
    return (
      <p
        className={cn(
          baseClass,
          "text-warning-foreground dark:text-warning",
        )}
      >
        <AlertTriangle
          className="mt-0.5 size-3.5 shrink-0"
          aria-hidden="true"
        />
        This URL includes only some passenger counts. Set all passengers below.
      </p>
    );
  }
  if (parsed.status === "invalid") {
    return (
      <p
        className={cn(
          baseClass,
          "text-warning-foreground dark:text-warning",
        )}
      >
        <AlertTriangle
          className="mt-0.5 size-3.5 shrink-0"
          aria-hidden="true"
        />
        The URL&apos;s passenger counts are invalid. Confirm them below.
      </p>
    );
  }
  if (parsed.status === "applied" && source === "manual") {
    return (
      <p className={cn(baseClass, "text-muted-foreground")}>
        <Info className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
        Using manually edited passenger counts for this search.
      </p>
    );
  }
  if (url.trim()) {
    return (
      <p className={cn(baseClass, "text-muted-foreground")}>
        <Info className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
        This URL does not include passengers. Set them below.
      </p>
    );
  }
  return (
    <p className={cn(baseClass, "text-muted-foreground")}>
      <Info className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
      Counts are sent with the search.
    </p>
  );
}

const URL_DESCRIPTION =
  "Expected format: https://mrbilit.com/trains/route?departureDate=1405-08-24. Tracking parameters are ignored.";

export function MonitorSearchPanel({
  url,
  adults,
  children,
  infants,
  passengerSource,
  urlPassenger,
  fieldErrors,
  searching,
  searchError,
  onUrlChange,
  onAdultsChange,
  onChildrenChange,
  onInfantsChange,
  onSearch,
}: MonitorSearchPanelProps) {
  return (
    <Card aria-busy={searching} className="gap-0 overflow-hidden py-0">
      <CardHeader className="border-b px-5 py-4 sm:px-6">
        <CardTitle className="flex items-center gap-2 text-base">
          <Search className="size-4 text-muted-foreground" aria-hidden="true" />
          Search MrBilit
        </CardTitle>
        <CardDescription>
          Paste a public train search URL. The route and Jalali date are read
          from it; availability is only reported after MrBilit responds.
        </CardDescription>
      </CardHeader>
      <CardContent className="px-5 py-5 sm:px-6 sm:py-6">
        <form
          className="flex flex-col gap-5"
          noValidate
          onSubmit={(event) => {
            event.preventDefault();
            onSearch();
          }}
        >
          <FormField
            id="mrbilitUrl"
            label="MrBilit search URL"
            error={fieldErrors.url}
            required
            description={URL_DESCRIPTION}
          >
            <Input
              {...fieldProps("mrbilitUrl", fieldErrors.url, URL_DESCRIPTION)}
              type="url"
              name="mrbilit-search-url"
              inputMode="url"
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              className="h-10"
              value={url}
              onChange={(event) => onUrlChange(event.target.value)}
              placeholder="https://mrbilit.com/trains/yazd-mashhad?departureDate=1405-08-24"
            />
          </FormField>

          <fieldset className="flex flex-col gap-3 border-t pt-5">
            <legend className="text-sm font-medium">Passengers</legend>
            <PassengerSourceNote
              url={url}
              source={passengerSource}
              parsed={urlPassenger}
            />
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
                  className="h-10"
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
                  className="h-10"
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
                  className="h-10"
                  value={infants}
                  onChange={(event) =>
                    onInfantsChange(Number(event.target.value))
                  }
                />
              </FormField>
            </div>
            <p className="text-xs text-muted-foreground">
              Up to 9 passengers total. Passenger counts always come from these
              fields; values are never inferred from the provider page.
            </p>
            {fieldErrors.passengers ? (
              <p
                className="text-xs font-medium text-destructive"
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

          <div className="flex flex-col gap-3 border-t pt-5 sm:flex-row sm:items-center sm:justify-between">
            <p className="text-xs text-muted-foreground">
              Searching does not create a monitor or change any availability
              state.
            </p>
            <Button
              type="submit"
              size="lg"
              className="w-full sm:w-auto"
              disabled={searching}
            >
              {searching ? (
                <Loader2 className="animate-spin" aria-hidden="true" />
              ) : (
                <Search aria-hidden="true" />
              )}
              {searching ? "Searching MrBilit…" : "Find trains"}
            </Button>
          </div>
          <div className="sr-only" role="status" aria-live="polite">
            {searching ? "Searching MrBilit…" : ""}
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
