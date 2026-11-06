export type MonitorStatus = "active" | "paused" | "expired";
export type ObservationState = "reservable" | "not_reservable" | "unknown";
export type CheckOutcome =
  | "running"
  | "success"
  | "empty"
  | "error"
  | "discarded"
  | "abandoned";
export type EventKind =
  | "availability"
  | "unavailable"
  | "provider_degraded"
  | "provider_recovered";
export type DeliveryChannel = "telegram" | "push";
export type DeliveryStatus =
  | "pending"
  | "sending"
  | "sent"
  | "retry_scheduled"
  | "failed"
  | "skipped";

export interface ErrorBody {
  error: {
    code: string;
    message: string;
    fields?: Record<string, string>;
  };
  requestId?: string;
}

export interface SessionResponse {
  authenticated: boolean;
  csrfToken?: string;
  setupRequired?: boolean;
  expiresAt?: string | null;
}

export interface LoginResponse {
  authenticated: boolean;
  csrfToken: string;
  expiresAt: string;
}

export interface SelectedOption {
  journeyKey: string;
  offeringKey: string;
  journeyNativeId?: string;
  offeringNativeId?: string;
  trainNumber?: string;
  operator?: string;
  originName?: string;
  destinationName?: string;
  departureAt?: string | null;
  arrivalAt?: string | null;
  wagonId?: string;
  wagonName?: string;
  className?: string;
  compartmentType?: string;
}

export interface MonitorProviderInput {
  providerId: string;
  originId: string;
  destinationId: string;
  bookingSlug?: string;
  genderCode?: string;
  selections?: SelectedOption[];
}

export interface MonitorCreateRequest {
  label: string;
  originName: string;
  destinationName: string;
  departureDate: string;
  adults: number;
  children: number;
  infants: number;
  providers: MonitorProviderInput[];
}

export interface MonitorPatchRequest {
  label?: string;
  originName?: string;
  destinationName?: string;
  departureDate?: string;
  adults?: number;
  children?: number;
  infants?: number;
  providers?: MonitorProviderInput[];
}

export interface MonitorProvider {
  providerId: string;
  active: boolean;
  originId: string;
  destinationId: string;
  bookingSlug?: string;
  genderCode?: string;
  configVersion: number;
  nextDueAt?: string | null;
  lastAttemptAt?: string | null;
  lastSuccessAt?: string | null;
  lastOutcome?: "success" | "empty" | "error";
  lastErrorKind?: string;
  lastErrorAt?: string | null;
  consecutiveFailures: number;
  selections: SelectedOption[];
  createdAt: string;
  updatedAt: string;
}

export interface Monitor {
  id: string;
  label: string;
  originName: string;
  destinationName: string;
  departureDate: string;
  adults: number;
  children: number;
  infants: number;
  enabled: boolean;
  status: MonitorStatus;
  expiresAt: string;
  expiredAt?: string | null;
  createdAt: string;
  updatedAt: string;
  providers: MonitorProvider[];
}

export interface InventoryObservation {
  journeyKey: string;
  offeringKey: string;
  state: ObservationState;
  stateSince: string;
  lastObservedAt: string;
  lastDecisiveAt?: string | null;
  lastMissingAt?: string | null;
  missing: boolean;
  lastReason?: string;
  lastCapacity?: number | null;
  trainNumber?: string;
  operator?: string;
  originName?: string;
  destinationName?: string;
  departureAt?: string | null;
  arrivalAt?: string | null;
  wagonName?: string;
  className?: string;
  priceAmount?: number | null;
  priceCurrency?: string;
  bookingUrl?: string;
  updatedAt: string;
}

export interface CheckRun {
  id: number;
  configVersion: number;
  startedAt: string;
  finishedAt?: string | null;
  outcome: CheckOutcome;
  errorKind?: string;
  httpStatus?: number | null;
  durationMs?: number | null;
  journeyCount: number;
  offeringCount: number;
  reservableCount: number;
  notReservableCount: number;
  unknownCount: number;
  warningCount: number;
  createdAt: string;
}

export interface MonitorProviderDetail extends MonitorProvider {
  observations: InventoryObservation[];
  observationsTruncated: boolean;
  recentChecks: CheckRun[];
  recentChecksTruncated: boolean;
}

export interface MonitorDetail extends Monitor {
  providers: MonitorProviderDetail[];
}

export interface MonitorListResponse {
  monitors: Monitor[];
}

export interface ProviderCapabilities {
  requiresOriginId?: boolean;
  requiresDestinationId?: boolean;
  supportsUrlSearch?: boolean;
  supportsPrice?: boolean;
  supportsBookingUrl?: boolean;
  supportsGender?: boolean;
  requiresOptionSelection?: boolean;
  maxPassengers?: number;
}

export interface PassengerCounts {
  adults: number;
  children: number;
  infants: number;
}

export interface ProviderSearchRequest extends PassengerCounts {
  url: string;
}

export interface ProviderSearchRoute {
  originId: string;
  destinationId: string;
  originName: string;
  destinationName: string;
}

export interface ProviderSearchClass {
  offeringKey: string;
  nativeId?: string;
  wagonId?: string;
  wagonName?: string;
  className?: string;
  compartmentType?: string;
  capacity: number | null;
  state: ObservationState;
  reason?: string;
  priceAmount?: number | null;
  priceCurrency?: string;
}

export interface ProviderSearchTrain {
  journeyKey: string;
  nativeId?: string;
  trainNumber?: string;
  operator?: string;
  originName?: string;
  destinationName?: string;
  departureAt?: string | null;
  arrivalAt?: string | null;
  classes: ProviderSearchClass[];
}

export interface ProviderSearchResult {
  url: string;
  slug: string;
  jalaliDate: string;
  departureDate: string;
  route: ProviderSearchRoute;
  passengers: PassengerCounts;
  trains: ProviderSearchTrain[];
  warnings: string[];
  observedAt: string;
}

export interface ProviderHealthEpisode {
  id: number;
  openedAt: string;
  failureCountAtOpen: number;
  lastErrorKind?: string;
}

export interface ProviderHealth {
  lastAttemptAt?: string | null;
  lastSuccessAt?: string | null;
  lastFailureAt?: string | null;
  lastErrorKind?: string;
  lastDurationMs?: number | null;
  consecutiveFailures: number;
  openEpisode?: ProviderHealthEpisode | null;
  stale: boolean;
  staleThresholdSeconds: number;
}

export interface ProviderInfo {
  id: string;
  displayName: string;
  testOnly: boolean;
  capabilities: ProviderCapabilities;
  health?: ProviderHealth | null;
}

export interface ProviderListResponse {
  providers: ProviderInfo[];
}

export type ObservabilityProviderState =
  | "healthy"
  | "degraded"
  | "rate_limited"
  | "cooling_down"
  | "recovering"
  | "idle"
  | "unknown";

export interface ObservabilityFlags {
  degraded: boolean;
  stale: boolean;
  rateLimited: boolean;
  coolingDown: boolean;
  recovering: boolean;
  idle: boolean;
  unknown: boolean;
}

export interface ObservabilityCooldown {
  status: "none" | "active" | "expired";
  until: string | null;
  remainingSeconds: number;
}

export interface ObservabilityAdaptive {
  level: number;
  adaptiveIntervalSeconds: number;
  effectiveIntervalSeconds: number;
  recoverySuccesses: number;
  requiredSuccesses: number;
  remainingSuccesses: number;
  cooldown: ObservabilityCooldown;
  last429At: string | null;
  lastSuccessAt: string | null;
  historyCompleteness: "best_effort" | "degraded" | "unavailable";
  historyAvailable: boolean;
}

export interface ObservabilityChecks {
  last1m: number;
  last5m: number;
  last15m: number;
  last1h: number;
  last24h: number;
  successes24h: number;
  failures24h: number;
  discarded24h: number;
  rateLimitedChecks24h: number;
  interrupted24h: number;
  incomplete24h: number;
  recorded7d: number;
  successRate: number | null;
  averageDurationMs: number | null;
  p95DurationMs: number | null;
  p95Method: string;
}

export interface ObservabilityQueue {
  available: boolean;
  sampledAt: string | null;
  stale: boolean;
  sampleIntervalSeconds: number;
  aggregation: "sampled_point_in_time";
  due: number;
  inFlight: number;
  queued: number;
  overdue: number;
  deferred: number;
  deferredByCooldown: number;
  deferredBySpacing: number;
  deferredByConcurrency: number;
  oldestPendingAgeSeconds: number | null;
  nextEligibleAt: string | null;
  blockedByConcurrency: boolean;
}

export interface ObservabilityProvider {
  id: string;
  displayName: string;
  testOnly: boolean;
  healthState: ObservabilityProviderState;
  healthReasons: string[];
  flags: ObservabilityFlags;
  activeTargets: number;
  lastProviderCheckAt: string | null;
  lastSuccessAt: string | null;
  lastFailureAt: string | null;
  lastErrorKind?: string;
  consecutiveFailures: number;
  openEpisode: boolean;
  adaptive: ObservabilityAdaptive;
  providerChecks: ObservabilityChecks;
  queue: ObservabilityQueue | null;
}

export interface ObservabilityScheduler {
  available: boolean;
  running: boolean;
  stopped: boolean;
  monitoringEnabled: boolean;
  paused: boolean;
  lastExecutionAt: string | null;
  lastDispatchAt: string | null;
  dueTargets: number;
  overdueTargets: number;
  queuedTargets: number;
  deferredTargets: number;
  inFlightTargets: number;
  nextEligibleDispatchAt: string | null;
  sampledAt: string | null;
  stale: boolean;
  activeCooldowns: number;
  degradedProviders: number;
  incompleteChecks: number;
  rateLimitEventsDropped: number;
  lastErrorKind?: string;
}

export interface ObservabilityScheduling {
  intervalScope: "global";
  configuredIntervalSeconds: number;
  providerSpacingSeconds: number;
  queueSampleIntervalSeconds: number;
  workers: number;
}

export interface ObservabilityTrafficSummary {
  windowSeconds: number;
  providerChecks: number;
  successes: number;
  failures: number;
  discarded: number;
  rateLimitedChecks: number;
  interrupted: number;
  incomplete: number;
  successRate: number | null;
  averageDurationMs: number | null;
  p95DurationMs: number | null;
  p95Method: string;
  dispatchAttempts: number;
  recorded7d: number;
  recordedWindowDays: number;
  historyAvailable: boolean;
}

export interface ObservabilityCoverage {
  checkDetailDays: number;
  rollupDays: number;
  rateLimitHistoryDays: number;
}

export interface ObservabilityOverview {
  generatedAt: string;
  coverage: ObservabilityCoverage;
  scheduling: ObservabilityScheduling;
  scheduler: ObservabilityScheduler;
  trafficSummary: ObservabilityTrafficSummary;
  providers: ObservabilityProvider[];
}

export interface ObservabilityTrafficPoint {
  bucketStart: string;
  providerChecks: number;
  successes: number;
  failures: number;
  discarded: number;
  rateLimitedChecks: number;
  interrupted: number;
  incomplete: number;
  averageDurationMs: number | null;
  p95DurationMs: number | null;
}

export interface ObservabilityQueuePoint {
  bucketStart: string;
  dueAvg: number;
  dueMax: number;
  overdueMax: number;
  deferredAvg: number;
  deferredMax: number;
  inFlightMax: number;
  oldestDueAgeMaxMs: number;
  dispatchAttempts: number;
  deferralEpisodesCooldown: number;
  deferralEpisodesSpacing: number;
  deferralEpisodesConcurrency: number;
}

export type ObservabilityRateLimitEventKind =
  | "rate_limited"
  | "cooldown_extended"
  | "recovery_progress"
  | "recovery_step"
  | "recovered";

export interface ObservabilityRateLimitEvent {
  occurredAt: string;
  kind: ObservabilityRateLimitEventKind;
  previousLevel: number;
  escalationLevel: number;
  escalated: boolean;
  maintained: boolean;
  previousAdaptiveMs: number;
  adaptiveMs: number;
  previousCooldownUntil: string | null;
  cooldownUntil: string | null;
  retryAfterMs: number;
  derived: boolean;
}

export interface ObservabilityTarget {
  monitorId: string;
  monitorLabel: string;
  nextDueAt: string | null;
  consecutiveFailures: number;
  lastOutcome?: string;
  lastErrorKind?: string;
  queueState: string;
}

export interface ObservabilityCheckActivity {
  occurredAt: string;
  outcome: string;
  providerErrorKind?: string;
  durationMs: number | null;
  queueWaitMs: number | null;
  monitorId: string;
  monitorLabel: string;
  incomplete: boolean;
}

export interface ObservabilityProviderDetail extends ObservabilityProvider {
  windowSeconds: number;
  trafficHistory: ObservabilityTrafficPoint[];
  queueHistory: ObservabilityQueuePoint[];
  rateLimitEvents: ObservabilityRateLimitEvent[];
  recentChecks: ObservabilityCheckActivity[];
  targets: ObservabilityTarget[];
}

export interface DeliveryState {
  id: string;
  channel: DeliveryChannel;
  status: DeliveryStatus;
  attemptCount: number;
  nextAttemptAt?: string | null;
  lastAttemptAt?: string | null;
  sentAt?: string | null;
  lastErrorKind?: string;
  createdAt: string;
  updatedAt: string;
}

export interface DeliverySummary {
  total: number;
  pending: number;
  sending: number;
  sent: number;
  retryScheduled: number;
  failed: number;
  skipped: number;
  attempted: number;
  terminal: number;
}

export interface EventRecord {
  id: string;
  kind: EventKind;
  providerId: string;
  monitorId?: string;
  monitorLabel?: string;
  episodeId?: number | null;
  occurredAt: string;
  state?: "reservable" | "not_reservable";
  trainNumber?: string;
  operator?: string;
  originName?: string;
  destinationName?: string;
  departureAt?: string | null;
  arrivalAt?: string | null;
  wagonName?: string;
  className?: string;
  priceAmount?: number | null;
  priceCurrency?: string;
  bookingUrl?: string;
  errorKind?: string;
  failureCount?: number | null;
  silent: boolean;
  createdAt: string;
  deliveries: DeliveryState[];
  deliverySummary: DeliverySummary;
}

export interface EventListResponse {
  events: EventRecord[];
  nextCursor: string;
}

export interface DeliveryRecord {
  id: string;
  eventId: string;
  eventKind: EventKind;
  eventOccurredAt: string;
  providerId: string;
  monitorId?: string;
  channel: DeliveryChannel;
  status: DeliveryStatus;
  attemptCount: number;
  nextAttemptAt?: string | null;
  lastAttemptAt?: string | null;
  sentAt?: string | null;
  lastErrorKind?: string;
  createdAt: string;
  updatedAt: string;
}

export interface DeliveryListResponse {
  deliveries: DeliveryRecord[];
  nextCursor: string;
}

export interface MonitorCounts {
  total: number;
  active: number;
  paused: number;
  expired: number;
}

export interface ProviderCounts {
  total: number;
  degraded: number;
  stale: number;
}

export interface DeliveryCounts {
  total: number;
  pending: number;
  sending: number;
  sent: number;
  retryScheduled: number;
  failed: number;
  skipped: number;
}

export interface Overview {
  generatedAt: string;
  monitors: MonitorCounts;
  providers: ProviderCounts;
  deliveries: DeliveryCounts;
  recentEvents: EventRecord[];
  recentDeliveries: DeliveryRecord[];
}

export interface TelegramSettings {
  available: boolean;
  configured: boolean;
  readable: boolean;
  chatLabel?: string;
  settingsErrorKind?: string;
  lastTestAt?: string | null;
  lastTestOk?: boolean | null;
  lastErrorKind?: string;
}

export interface DailyReportSettings {
  time: string;
  lastReportDate?: string;
  lastStatus?: string;
  lastSentAt?: string | null;
  lastErrorKind?: string;
}

export interface Settings {
  pollIntervalSeconds: number;
  pushEnabled: boolean;
  monitoringEnabled: boolean;
  unavailableNotifyAfterMinutes: number;
  dailyReport: DailyReportSettings;
  telegram: TelegramSettings;
}

export interface SettingsUpdateRequest {
  pollIntervalSeconds?: number;
  pushEnabled?: boolean;
  monitoringEnabled?: boolean;
  unavailableNotifyAfterMinutes?: number;
  dailyReportTime?: string;
}

export interface TelegramConfigRequest {
  token?: string;
  chatId?: string;
  clear?: boolean;
}

export interface EventListParams {
  kind?: EventKind | "";
  monitorId?: string;
  providerId?: string;
  limit?: number;
  cursor?: string;
}

export interface DeliveryListParams {
  channel?: DeliveryChannel | "";
  status?: DeliveryStatus | "";
  eventId?: string;
  kind?: EventKind | "";
  limit?: number;
  cursor?: string;
}

export interface PushSubscriptionRecord {
  id: string;
  label?: string;
  createdAt: string;
  lastSuccessAt?: string | null;
  failureCount: number;
}

export interface PushConfig {
  enabled: boolean;
  available: boolean;
  subjectConfigured: boolean;
  publicKey?: string;
  subscriptions: PushSubscriptionRecord[];
}

export interface PushRegistrationRequest {
  endpoint: string;
  keys: {
    p256dh: string;
    auth: string;
  };
  label?: string;
}

export interface PushTestResult {
  attempted: number;
  sent: number;
  failed: number;
  errorKinds?: string[];
}
