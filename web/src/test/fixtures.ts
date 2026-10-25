import type {
  CheckRun,
  DeliveryRecord,
  DeliveryState,
  EventRecord,
  InventoryObservation,
  Monitor,
  MonitorDetail,
  MonitorProvider,
  MonitorProviderDetail,
  Overview,
  ProviderInfo,
  Settings,
} from "@/api/types";

const NOW = "2026-10-24T12:00:00Z";

export function makeProvider(
  overrides: Partial<ProviderInfo> = {},
): ProviderInfo {
  return {
    id: "fake",
    displayName: "Test provider — not a real ticket source",
    testOnly: true,
    capabilities: {
      requiresOriginId: true,
      requiresDestinationId: true,
      supportsBookingUrl: false,
      supportsGender: false,
      maxPassengers: 9,
    },
    health: {
      lastAttemptAt: NOW,
      lastSuccessAt: NOW,
      lastFailureAt: null,
      lastErrorKind: "",
      lastDurationMs: 42,
      consecutiveFailures: 0,
      openEpisode: null,
      stale: false,
      staleThresholdSeconds: 1800,
    },
    ...overrides,
  };
}

export function makeProviderRow(
  overrides: Partial<MonitorProvider> = {},
): MonitorProvider {
  return {
    providerId: "fake",
    active: true,
    originId: "test-origin-available",
    destinationId: "test-destination-default",
    configVersion: 1,
    nextDueAt: "2026-10-24T12:05:00Z",
    lastAttemptAt: "2026-10-24T11:55:00Z",
    lastSuccessAt: "2026-10-24T11:55:00Z",
    lastOutcome: "success",
    lastErrorKind: "",
    lastErrorAt: null,
    consecutiveFailures: 0,
    selections: [],
    createdAt: "2026-10-20T10:00:00Z",
    updatedAt: "2026-10-24T11:55:00Z",
    ...overrides,
  };
}

export function makeMonitor(overrides: Partial<Monitor> = {}): Monitor {
  return {
    id: "monitor-1",
    label: "Tehran to Mashhad",
    originName: "Tehran",
    destinationName: "Mashhad",
    departureDate: "2026-10-28",
    adults: 1,
    children: 0,
    infants: 0,
    enabled: true,
    status: "active",
    expiresAt: "2026-10-29T20:30:00Z",
    expiredAt: null,
    createdAt: "2026-10-20T10:00:00Z",
    updatedAt: "2026-10-24T10:00:00Z",
    providers: [makeProviderRow()],
    ...overrides,
  };
}

export function makeObservation(
  overrides: Partial<InventoryObservation> = {},
): InventoryObservation {
  return {
    journeyKey: "journey-1",
    offeringKey: "offering-1",
    state: "reservable",
    stateSince: "2026-10-24T11:00:00Z",
    lastObservedAt: "2026-10-24T11:55:00Z",
    lastDecisiveAt: "2026-10-24T11:55:00Z",
    lastMissingAt: null,
    missing: false,
    lastReason: "capacity_positive",
    lastCapacity: 4,
    trainNumber: "401",
    operator: "RAJA",
    originName: "Tehran",
    destinationName: "Mashhad",
    departureAt: "2026-10-28T06:00:00Z",
    arrivalAt: "2026-10-28T14:00:00Z",
    wagonName: "Wagon 4",
    className: "Economy",
    priceAmount: 1250000,
    priceCurrency: "IRR",
    bookingUrl: "https://mrbilit.com/trains/tehran-mashhad?departureDate=1405-08-06",
    updatedAt: "2026-10-24T11:55:00Z",
    ...overrides,
  };
}

export function makeCheck(overrides: Partial<CheckRun> = {}): CheckRun {
  return {
    id: 1,
    configVersion: 1,
    startedAt: "2026-10-24T11:55:00Z",
    finishedAt: "2026-10-24T11:55:02Z",
    outcome: "success",
    errorKind: "",
    httpStatus: 200,
    durationMs: 2000,
    journeyCount: 1,
    offeringCount: 1,
    reservableCount: 1,
    notReservableCount: 0,
    unknownCount: 0,
    warningCount: 0,
    createdAt: "2026-10-24T11:55:00Z",
    ...overrides,
  };
}

export function makeProviderDetail(
  overrides: Partial<MonitorProviderDetail> = {},
): MonitorProviderDetail {
  return {
    ...makeProviderRow(),
    observations: [makeObservation()],
    observationsTruncated: false,
    recentChecks: [makeCheck()],
    recentChecksTruncated: false,
    ...overrides,
  };
}

export function makeMonitorDetail(
  overrides: Partial<MonitorDetail> = {},
): MonitorDetail {
  return {
    ...makeMonitor(),
    providers: [makeProviderDetail()],
    ...overrides,
  };
}

export function makeDeliveryState(
  overrides: Partial<DeliveryState> = {},
): DeliveryState {
  return {
    id: "delivery-1",
    channel: "telegram",
    status: "sent",
    attemptCount: 1,
    nextAttemptAt: null,
    lastAttemptAt: NOW,
    sentAt: NOW,
    lastErrorKind: "",
    createdAt: NOW,
    updatedAt: NOW,
    ...overrides,
  };
}

export function makeEvent(overrides: Partial<EventRecord> = {}): EventRecord {
  return {
    id: "event-1",
    kind: "availability",
    providerId: "fake",
    monitorId: "monitor-1",
    monitorLabel: "Tehran to Mashhad",
    episodeId: 1,
    occurredAt: NOW,
    state: "reservable",
    trainNumber: "401",
    operator: "RAJA",
    originName: "Tehran",
    destinationName: "Mashhad",
    departureAt: "2026-10-28T06:00:00Z",
    arrivalAt: "2026-10-28T14:00:00Z",
    wagonName: "Wagon 4",
    className: "Economy",
    priceAmount: 1250000,
    priceCurrency: "IRR",
    bookingUrl: "https://mrbilit.com/trains/tehran-mashhad?departureDate=1405-08-06",
    errorKind: "",
    failureCount: null,
    createdAt: NOW,
    deliveries: [makeDeliveryState()],
    deliverySummary: {
      total: 1,
      pending: 0,
      sending: 0,
      sent: 1,
      retryScheduled: 0,
      failed: 0,
      skipped: 0,
      attempted: 1,
      terminal: 1,
    },
    ...overrides,
  };
}

export function makeDeliveryRecord(
  overrides: Partial<DeliveryRecord> = {},
): DeliveryRecord {
  return {
    id: "delivery-1",
    eventId: "event-1",
    eventKind: "availability",
    eventOccurredAt: NOW,
    providerId: "fake",
    monitorId: "monitor-1",
    channel: "telegram",
    status: "sent",
    attemptCount: 1,
    nextAttemptAt: null,
    lastAttemptAt: NOW,
    sentAt: NOW,
    lastErrorKind: "",
    createdAt: NOW,
    updatedAt: NOW,
    ...overrides,
  };
}

export function makeOverview(overrides: Partial<Overview> = {}): Overview {
  return {
    generatedAt: NOW,
    monitors: { total: 1, active: 1, paused: 0, expired: 0 },
    providers: { total: 1, degraded: 0, stale: 0 },
    deliveries: {
      total: 1,
      pending: 0,
      sending: 0,
      sent: 1,
      retryScheduled: 0,
      failed: 0,
      skipped: 0,
    },
    recentEvents: [makeEvent()],
    recentDeliveries: [makeDeliveryRecord()],
    ...overrides,
  };
}

export function makeSettings(overrides: Partial<Settings> = {}): Settings {
  return {
    pollIntervalSeconds: 300,
    pushEnabled: false,
    telegram: {
      available: true,
      configured: true,
      readable: true,
      chatLabel: "Chat •••• 1234",
      settingsErrorKind: "",
      lastTestAt: NOW,
      lastTestOk: true,
      lastErrorKind: "",
    },
    ...overrides,
  };
}
