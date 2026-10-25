import { useMemo } from "react";
import {
  AlertTriangle,
  CheckCircle2,
  Loader2,
  Plus,
  Search,
  Settings2,
  TrainFront,
} from "lucide-react";
import { Link } from "react-router-dom";
import type {
  ProviderSearchResult,
  PushConfig,
  SelectedOption,
  Settings,
} from "@/api/types";
import { ProviderPrice } from "@/components/ProviderPrice";
import { ObservationStateBadge } from "@/components/status";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { FormField, fieldProps } from "@/components/ui/form-field";
import { Input } from "@/components/ui/input";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import {
  describeSelectedWagon,
  groupSelectedWagons,
  notificationReadiness,
  pollIntervalLabel,
  selectedOfferingCount,
  type ChannelTone,
  type SearchJourneyGroup,
} from "@/lib/searchResults";

interface MonitorSummaryPanelProps {
  result: ProviderSearchResult | null;
  groups: SearchJourneyGroup[];
  selected: Record<string, SelectedOption>;
  label: string;
  labelError?: string;
  selectionError?: string;
  createError: string;
  submitting: boolean;
  stale: boolean;
  searching: boolean;
  emptyResult: boolean;
  settings: Settings;
  pushConfig: PushConfig;
  onLabelChange: (value: string) => void;
  onCreate: () => void;
}

function badgeVariant(tone: ChannelTone): "success" | "warning" | "muted" {
  return tone;
}

function SettingsSummary({
  settings,
  pushConfig,
}: {
  settings: Settings;
  pushConfig: PushConfig;
}) {
  const notifications = notificationReadiness(settings, pushConfig);
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
            Delivery
          </h3>
          <p className="mt-0.5 text-[0.7rem] text-muted-foreground">
            Applies to every monitor.
          </p>
        </div>
        <Badge variant="outline" className="font-normal">
          Global
        </Badge>
      </div>
      <dl className="flex flex-col gap-2.5 text-sm">
        <div className="flex items-center justify-between gap-3">
          <dt className="text-muted-foreground">Polling interval</dt>
          <dd className="text-right font-medium tabular-nums">
            {pollIntervalLabel(settings.pollIntervalSeconds)}
          </dd>
        </div>
        <div className="flex items-center justify-between gap-3">
          <dt className="text-muted-foreground">Telegram</dt>
          <dd>
            <Badge variant={badgeVariant(notifications.telegram.tone)}>
              {notifications.telegram.label}
            </Badge>
          </dd>
        </div>
        <div className="flex items-center justify-between gap-3">
          <dt className="text-muted-foreground">Browser Push</dt>
          <dd>
            <Badge variant={badgeVariant(notifications.push.tone)}>
              {notifications.push.label}
            </Badge>
          </dd>
        </div>
      </dl>
      <div className="flex flex-wrap items-center justify-between gap-2">
        {notifications.ready ? (
          <p className="flex items-start gap-1.5 text-[0.7rem] text-muted-foreground">
            <CheckCircle2
              className="mt-0.5 size-3.5 shrink-0 text-success"
              aria-hidden="true"
            />
            At least one channel is ready.
          </p>
        ) : (
          <p className="flex items-start gap-1.5 text-[0.7rem] text-warning-foreground dark:text-warning">
            <AlertTriangle
              className="mt-0.5 size-3.5 shrink-0"
              aria-hidden="true"
            />
            No notification channel is ready. You can still create the monitor.
          </p>
        )}
        <Link
          to="/settings"
          className="inline-flex w-fit items-center gap-1.5 py-1 text-xs font-medium text-primary underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
        >
          <Settings2 className="size-3.5" aria-hidden="true" />
          Manage
        </Link>
      </div>
    </div>
  );
}

const LABEL_DESCRIPTION = "Shown in the monitor list and notifications.";

export function MonitorSummaryPanel({
  result,
  groups,
  selected,
  label,
  labelError,
  selectionError,
  createError,
  submitting,
  stale,
  searching,
  emptyResult,
  settings,
  pushConfig,
  onLabelChange,
  onCreate,
}: MonitorSummaryPanelProps) {
  const selectedWagons = useMemo(
    () => groupSelectedWagons(groups, selected),
    [groups, selected],
  );
  const selectedOfferings = selectedOfferingCount(selected);
  const labelReady = label.trim().length > 0;
  const canCreate =
    Boolean(result) &&
    !stale &&
    !submitting &&
    selectedWagons.length > 0 &&
    labelReady;
  const helper = stale
    ? "Search again to refresh these results before creating the monitor."
    : selectedWagons.length === 0
      ? "Select at least one wagon to continue."
      : !labelReady
        ? "Enter a monitor label to continue."
        : "The first check is scheduled shortly after creation.";

  return (
    <Card
      id="monitor-summary"
      aria-busy={submitting}
      className="scroll-mt-24 gap-0 overflow-hidden rounded-[10px] py-0 shadow-xs xl:sticky xl:top-6 xl:max-h-[calc(100dvh-3rem)] xl:overflow-y-auto xl:overscroll-contain"
    >
      <CardHeader className="border-b bg-surface-raised/55 px-4 py-3.5 sm:px-5">
        <div>
          <p className="eyebrow text-[0.6rem]">Review &amp; create</p>
          <CardTitle className="mt-1 text-base" role="heading" aria-level={2}>
            Review
          </CardTitle>
          <CardDescription>
            Name the monitor and confirm the wagons to watch.
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 px-4 py-4 sm:px-5">
        {!result ? (
          searching ? (
            <div className="flex flex-col gap-3">
              <p className="flex items-center gap-2 text-sm font-medium">
                <Search
                  className="size-4 text-muted-foreground"
                  aria-hidden="true"
                />
                Searching…
              </p>
              <div className="flex flex-col gap-2">
                <Skeleton className="h-4 w-3/4" />
                <Skeleton className="h-4 w-1/2" />
                <Skeleton className="h-9 w-full" />
              </div>
            </div>
          ) : (
            <div className="flex flex-col gap-1.5">
              <p className="flex items-center gap-2 text-sm font-medium">
                {emptyResult ? (
                  <TrainFront
                    className="size-4 text-muted-foreground"
                    aria-hidden="true"
                  />
                ) : (
                  <Search
                    className="size-4 text-muted-foreground"
                    aria-hidden="true"
                  />
                )}
                {emptyResult ? "No wagons to review" : "Review appears here"}
              </p>
              <p className="text-xs leading-relaxed text-muted-foreground">
                {emptyResult
                  ? "The provider returned no trains for this route and date. Try another date or URL."
                  : "Search first, then choose the wagons or individual fare options you want Neda to monitor."}
              </p>
            </div>
          )
        ) : (
          <>
            <FormField
              id="mrbilitLabel"
              label="Monitor label"
              error={labelError}
              required
              description={LABEL_DESCRIPTION}
            >
              <Input
                {...fieldProps("mrbilitLabel", labelError, LABEL_DESCRIPTION)}
                type="text"
                name="monitor-label"
                autoComplete="off"
                className="h-10 max-sm:h-11"
                maxLength={100}
                value={label}
                onChange={(event) => onLabelChange(event.target.value)}
                placeholder="Yazd to Mashhad — Friday"
              />
            </FormField>

            <div className="flex flex-col gap-2.5">
              <div className="flex items-center justify-between gap-2">
                <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
                  Selected wagons
                </h3>
                <span
                  className="text-xs text-muted-foreground tabular-nums"
                  aria-live="polite"
                >
                  {selectedWagons.length === 0
                    ? "None"
                    : `${selectedWagons.length} wagon${
                        selectedWagons.length === 1 ? "" : "s"
                      } · ${selectedOfferings} fare option${
                        selectedOfferings === 1 ? "" : "s"
                      }`}
                </span>
              </div>
              {selectedWagons.length === 0 ? (
                <p className="rounded-lg border border-dashed bg-muted/15 px-3 py-3 text-xs leading-relaxed text-muted-foreground">
                  {stale
                    ? "Search again to refresh these results before selecting wagons."
                    : "Choose at least one wagon from the results. Selecting a wagon tracks all its current fare options."}
                </p>
              ) : (
                <ul
                  aria-label="Selected wagons"
                  tabIndex={0}
                  className="max-h-60 divide-y overflow-y-auto rounded-lg border text-xs focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none"
                >
                  {selectedWagons.map((item) => (
                    <li
                      key={`${item.journey.train.journeyKey}\u0000${item.wagon.key}`}
                      className="flex flex-col gap-1.5 px-3 py-2.5"
                    >
                      <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
                        <span className="min-w-0 break-words font-medium">
                          <bdi dir="auto">{describeSelectedWagon(item)}</bdi>
                        </span>
                        <ObservationStateBadge state={item.summary.state} />
                      </div>
                      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-muted-foreground">
                        <span>
                          {item.partial
                            ? `${item.selectedOfferings.length} of ${item.wagon.offerings.length} fare options`
                            : `${item.summary.optionCount} fare options`}
                        </span>
                        <span aria-hidden="true">·</span>
                        <span>
                          {item.summary.reservableCount} reservable
                          {item.summary.unknownCount > 0
                            ? ` · ${item.summary.unknownCount} unknown`
                            : ""}
                        </span>
                        <span aria-hidden="true">·</span>
                        <ProviderPrice
                          amount={item.summary.lowestKnownPrice}
                          className="text-[0.7rem]"
                          emptyLabel="Price unavailable"
                        />
                      </div>
                    </li>
                  ))}
                </ul>
              )}
              {selectionError ? (
                <p className="text-xs font-medium text-destructive" role="alert">
                  {selectionError}
                </p>
              ) : null}
            </div>
          </>
        )}

        <Separator />

        <SettingsSummary settings={settings} pushConfig={pushConfig} />

        {createError ? (
          <div
            id="create-error"
            tabIndex={-1}
            className="scroll-mt-24 rounded-lg focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          >
            <Alert variant="destructive">
              <AlertTriangle aria-hidden="true" />
              <AlertTitle>Monitor not created</AlertTitle>
              <AlertDescription>{createError}</AlertDescription>
            </Alert>
          </div>
        ) : null}

        {result ? (
          <div className="flex flex-col gap-2">
            <Button
              type="button"
              size="lg"
              variant={canCreate ? "default" : "outline"}
              className="w-full"
              onClick={onCreate}
              disabled={!canCreate}
              aria-describedby="create-monitor-help"
            >
              {submitting ? (
                <Loader2
                  data-icon="inline-start"
                  className="animate-spin"
                  aria-hidden="true"
                />
              ) : (
                <Plus data-icon="inline-start" aria-hidden="true" />
              )}
              {submitting ? "Creating…" : "Create monitor"}
            </Button>
            <p
              id="create-monitor-help"
              className="text-center text-xs leading-relaxed text-muted-foreground"
            >
              {helper}
            </p>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}
