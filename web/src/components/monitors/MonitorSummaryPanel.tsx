import { useMemo } from "react";
import {
  AlertTriangle,
  ArrowRight,
  CheckCircle2,
  ClipboardCheck,
  Loader2,
  Plus,
  Settings2,
} from "lucide-react";
import { Link } from "react-router-dom";
import type {
  ProviderSearchResult,
  PushConfig,
  SelectedOption,
  Settings,
} from "@/api/types";
import { DateDisplay } from "@/components/JalaliDatePicker";
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
import {
  describeSelection,
  formatPassengerSummary,
  notificationReadiness,
  pollIntervalLabel,
  resultDestinationName,
  resultOriginName,
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
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-xs font-semibold tracking-wide text-muted-foreground uppercase">
          Delivery settings
        </h3>
        <Badge variant="outline" className="font-normal">
          Global
        </Badge>
      </div>
      <dl className="flex flex-col gap-2.5 text-sm">
        <div className="flex items-center justify-between gap-3">
          <dt className="text-muted-foreground">Polling interval</dt>
          <dd className="text-right font-medium">
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
        className="inline-flex w-fit items-center gap-1.5 text-xs font-medium text-primary underline-offset-4 hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
      >
        <Settings2 className="size-3.5" aria-hidden="true" />
        Manage in Settings
      </Link>
    </div>
  );
}

const LABEL_DESCRIPTION = "Used in the monitor list and notifications.";

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
    <Card className="gap-0 overflow-hidden py-0 lg:sticky lg:top-6 lg:max-h-[calc(100dvh-3rem)] lg:overflow-y-auto">
      <CardHeader className="border-b px-5 py-4">
        <CardTitle className="flex items-center gap-2 text-base">
          <ClipboardCheck
            className="size-4 text-muted-foreground"
            aria-hidden="true"
          />
          Monitor summary
        </CardTitle>
        <CardDescription>
          Review the exact route, passengers, classes, and delivery settings
          before creating the monitor.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5 px-5 py-5">
        {!result ? (
          <div className="flex flex-col items-center gap-3 py-6 text-center">
            <span className="flex size-10 items-center justify-center rounded-full bg-muted text-muted-foreground">
              <ClipboardCheck className="size-5" aria-hidden="true" />
            </span>
            <div className="flex flex-col gap-1">
              <p className="text-sm font-medium">No journey selected yet</p>
              <p className="text-xs leading-relaxed text-muted-foreground">
                Search for a train to review the route, date, passengers, and
                exact classes here.
              </p>
            </div>
          </div>
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

            <dl className="flex flex-col gap-3.5 border-t pt-4 text-sm">
              <div className="flex flex-col gap-0.5">
                <dt className="text-[0.7rem] font-medium tracking-wide text-muted-foreground uppercase">
                  Route
                </dt>
                <dd
                  dir="ltr"
                  className="flex min-w-0 items-center gap-1.5 font-medium"
                >
                  <bdi dir="auto" className="min-w-0 break-words">
                    {resultOriginName(result)}
                  </bdi>
                  {resultDestinationName(result) ? (
                    <>
                      <ArrowRight
                        className="size-3.5 shrink-0 text-muted-foreground"
                        aria-hidden="true"
                      />
                      <bdi dir="auto" className="min-w-0 break-words">
                        {resultDestinationName(result)}
                      </bdi>
                    </>
                  ) : null}
                </dd>
              </div>
              <div className="flex flex-col gap-0.5">
                <dt className="text-[0.7rem] font-medium tracking-wide text-muted-foreground uppercase">
                  Departure
                </dt>
                <dd>
                  <DateDisplay value={result.departureDate} />
                </dd>
              </div>
              <div className="flex flex-col gap-0.5">
                <dt className="text-[0.7rem] font-medium tracking-wide text-muted-foreground uppercase">
                  Passengers
                </dt>
                <dd className="font-medium">
                  {formatPassengerSummary(result.passengers, true)}
                </dd>
              </div>
            </dl>

            <div className="border-t pt-4">
              <div className="mb-2 flex items-center justify-between gap-2">
                <h3 className="text-xs font-semibold">Selected options</h3>
                <span
                  className="text-xs text-muted-foreground"
                  aria-live="polite"
                >
                  {selectedCount === 0
                    ? "None selected"
                    : `${selectedCount} of ${totalOptions} selected`}
                </span>
              </div>
              {selectedCount === 0 ? (
                <p className="text-xs leading-relaxed text-muted-foreground">
                  Choose at least one class from the results. Unavailable
                  classes are still valid monitoring targets.
                </p>
              ) : (
                <ul className="max-h-56 divide-y overflow-y-auto rounded-lg border text-xs">
                  {selectedPositions.map((position) => (
                    <li
                      key={selectionKey(
                        position.journey.train.journeyKey,
                        position.option.offeringKey,
                      )}
                      className="flex items-start justify-between gap-2 px-3 py-2.5"
                    >
                      <span className="min-w-0">
                        <span className="block break-words font-medium">
                          <bdi dir="auto">{describeSelection(position)}</bdi>
                        </span>
                        <span className="mt-0.5 block text-muted-foreground">
                          {position.option.wagonName ||
                            position.family.wagonName ||
                            position.option.className ||
                            "Class details unavailable"}
                        </span>
                      </span>
                      <ObservationStateBadge state={position.option.state} />
                    </li>
                  ))}
                </ul>
              )}
              {selectionError ? (
                <p
                  className="mt-2 text-xs font-medium text-destructive"
                  role="alert"
                >
                  {selectionError}
                </p>
              ) : null}
            </div>
          </>
        )}

        <div className="border-t pt-4">
          <SettingsSummary settings={settings} pushConfig={pushConfig} />
        </div>

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
                <Loader2 className="animate-spin" aria-hidden="true" />
              ) : (
                <Plus aria-hidden="true" />
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
