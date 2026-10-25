import { useMemo } from "react";
import {
  AlertTriangle,
  CheckCircle2,
  ClipboardCheck,
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
  classFamilyMeta,
  describeSelection,
  notificationReadiness,
  pollIntervalLabel,
  selectionKey,
  type ChannelTone,
  type SearchJourneyGroup,
  type SelectionPosition,
} from "@/lib/mrbilitSearch";

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
      <Link
        to="/settings"
        className="inline-flex w-fit items-center gap-1.5 py-1 text-xs font-medium text-primary underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
      >
        <Settings2 className="size-3.5" aria-hidden="true" />
        Manage in Settings
      </Link>
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
  const selectedPositions = useMemo(() => {
    const positions: SelectionPosition[] = [];
    for (const journey of groups) {
      for (const family of journey.families) {
        for (const [index, option] of family.options.entries()) {
          const key = selectionKey(
            journey.train.journeyKey,
            option.offeringKey,
          );
          if (selected[key]) {
            positions.push({
              journey,
              family,
              option,
              index,
              total: family.options.length,
            });
          }
        }
      }
    }
    return positions;
  }, [groups, selected]);
  const selectedCount = selectedPositions.length;
  const totalOptions = groups.reduce(
    (total, journey) => total + journey.optionCount,
    0,
  );
  const notifications = notificationReadiness(settings, pushConfig);
  const labelReady = label.trim().length > 0;
  const canCreate =
    Boolean(result) && !stale && !submitting && selectedCount > 0 && labelReady;
  const helper = stale
    ? "Search again to refresh these results before creating the monitor."
    : selectedCount === 0
      ? "Select at least one class to continue."
      : !labelReady
        ? "Enter a monitor label to continue."
        : "The first check is scheduled shortly after creation.";

  return (
    <Card
      aria-busy={submitting}
      className="gap-0 overflow-hidden py-0 xl:sticky xl:top-6 xl:max-h-[calc(100dvh-3rem)] xl:overflow-y-auto xl:overscroll-contain"
    >
      <CardHeader className="border-b px-5 py-4">
        <div className="flex items-start gap-3">
          <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted/40 text-muted-foreground">
            <ClipboardCheck className="size-4" aria-hidden="true" />
          </span>
          <div className="min-w-0">
            <CardTitle
              className="text-base"
              role="heading"
              aria-level={2}
            >
              Review &amp; create
            </CardTitle>
            <CardDescription className="mt-1">
              Name the monitor and confirm the classes to watch.
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-5 px-5 py-5">
        {!result ? (
          searching ? (
            <div className="flex flex-col gap-3">
              <p className="flex items-center gap-2 text-sm font-medium">
                <Search
                  className="size-4 text-muted-foreground"
                  aria-hidden="true"
                />
                Searching MrBilit…
              </p>
              <div className="flex flex-col gap-2">
                <Skeleton className="h-4 w-3/4" />
                <Skeleton className="h-4 w-1/2" />
                <Skeleton className="h-9 w-full" />
              </div>
            </div>
          ) : (
            <div className="flex flex-col gap-2 rounded-xl border border-dashed bg-muted/15 px-4 py-5">
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
                {emptyResult ? "No classes to review" : "Start with a search"}
              </p>
              <p className="text-xs leading-relaxed text-muted-foreground">
                {emptyResult
                  ? "MrBilit returned no trains for this route and date. Try another date or URL."
                  : "Search for trains, then select the classes you want to monitor."}
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
                className="h-10"
                maxLength={100}
                value={label}
                onChange={(event) => onLabelChange(event.target.value)}
                placeholder="Yazd to Mashhad — Friday"
              />
            </FormField>

            <div className="flex flex-col gap-2.5">
              <div className="flex items-center justify-between gap-2">
                <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
                  Selected options
                </h3>
                <span
                  className="text-xs text-muted-foreground tabular-nums"
                  aria-live="polite"
                >
                  {selectedCount === 0
                    ? "None"
                    : `${selectedCount} of ${totalOptions}`}
                </span>
              </div>
              {selectedCount === 0 ? (
                <p className="rounded-lg border border-dashed bg-muted/15 px-3 py-3 text-xs leading-relaxed text-muted-foreground">
                  {stale
                    ? "Search again to refresh these results before selecting classes."
                    : "Choose at least one class from the results. Unavailable classes are still valid monitoring targets."}
                </p>
              ) : (
                <ul
                  aria-label="Selected offerings"
                  tabIndex={0}
                  className="max-h-56 divide-y overflow-y-auto rounded-lg border text-xs focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none"
                >
                  {selectedPositions.map((position) => (
                    <li
                      key={selectionKey(
                        position.journey.train.journeyKey,
                        position.option.offeringKey,
                      )}
                      className="flex flex-col gap-1.5 px-3 py-2.5"
                    >
                      <span className="flex min-w-0 flex-wrap items-center gap-1.5">
                        <span className="break-words font-medium">
                          <bdi dir="auto">{describeSelection(position)}</bdi>
                        </span>
                        <ObservationStateBadge state={position.option.state} />
                      </span>
                      <span className="flex flex-wrap items-center gap-x-1.5 text-muted-foreground">
                        <bdi dir="auto">
                          {classFamilyMeta(position.family) ||
                            position.family.compartmentType ||
                            "Class details unavailable"}
                        </bdi>
                        {position.option.capacity !== null ? (
                          <>
                            <span aria-hidden="true">·</span>
                            <span className="tabular-nums">
                              Capacity {position.option.capacity}
                            </span>
                          </>
                        ) : null}
                      </span>
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

        {result && !notifications.ready ? (
          <Alert variant="warning">
            <AlertTriangle aria-hidden="true" />
            <AlertTitle>No notification channel is ready</AlertTitle>
            <AlertDescription>
              You can still create the monitor. Configure Telegram or enable
              Browser Push in Settings to receive alerts.
            </AlertDescription>
          </Alert>
        ) : result ? (
          <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
            <CheckCircle2
              className="mt-0.5 size-3.5 shrink-0 text-success"
              aria-hidden="true"
            />
            At least one notification channel is ready.
          </p>
        ) : null}

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
