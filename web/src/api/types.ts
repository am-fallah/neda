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

export interface MonitorProviderInput {
  providerId: string;
  originId: string;
  destinationId: string;
  bookingSlug?: string;
  genderCode?: string;
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
  supportsBookingUrl?: boolean;
  supportsGender?: boolean;
  maxPassengers?: number;
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
  state?: "reservable";
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

export interface Settings {
  pollIntervalSeconds: number;
  pushEnabled: boolean;
  telegram: TelegramSettings;
}

export interface SettingsUpdateRequest {
  pollIntervalSeconds?: number;
  pushEnabled?: boolean;
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
