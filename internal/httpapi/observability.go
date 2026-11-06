package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/observability"
	"github.com/am-fallah/neda/internal/provider"
	"github.com/am-fallah/neda/internal/store"
)

const p95Method = "disjoint_histogram_upper_bound"

type observabilityResponse struct {
	GeneratedAt    time.Time                   `json:"generatedAt"`
	Coverage       observabilityCoverage       `json:"coverage"`
	Scheduling     observabilityScheduling     `json:"scheduling"`
	Scheduler      observabilityScheduler      `json:"scheduler"`
	TrafficSummary observabilityTrafficSummary `json:"trafficSummary"`
	Providers      []observabilityProvider     `json:"providers"`
}

type observabilityCoverage struct {
	CheckDetailDays      int `json:"checkDetailDays"`
	RollupDays           int `json:"rollupDays"`
	RateLimitHistoryDays int `json:"rateLimitHistoryDays"`
}

type observabilityScheduling struct {
	IntervalScope             string `json:"intervalScope"`
	ConfiguredIntervalSeconds int64  `json:"configuredIntervalSeconds"`
	ProviderSpacingSeconds    int64  `json:"providerSpacingSeconds"`
	QueueSampleIntervalSecs   int64  `json:"queueSampleIntervalSeconds"`
	Workers                   int    `json:"workers"`
}

type observabilityScheduler struct {
	Available              bool       `json:"available"`
	Running                bool       `json:"running"`
	Stopped                bool       `json:"stopped"`
	MonitoringEnabled      bool       `json:"monitoringEnabled"`
	Paused                 bool       `json:"paused"`
	LastExecutionAt        *time.Time `json:"lastExecutionAt"`
	LastDispatchAt         *time.Time `json:"lastDispatchAt"`
	DueTargets             int        `json:"dueTargets"`
	OverdueTargets         int        `json:"overdueTargets"`
	QueuedTargets          int        `json:"queuedTargets"`
	DeferredTargets        int        `json:"deferredTargets"`
	InFlightTargets        int        `json:"inFlightTargets"`
	NextEligibleAt         *time.Time `json:"nextEligibleDispatchAt"`
	SampledAt              *time.Time `json:"sampledAt"`
	Stale                  bool       `json:"stale"`
	ActiveCooldowns        int        `json:"activeCooldowns"`
	DegradedProviders      int        `json:"degradedProviders"`
	IncompleteChecks       int64      `json:"incompleteChecks"`
	RateLimitEventsDropped uint64     `json:"rateLimitEventsDropped"`
	LastErrorKind          string     `json:"lastErrorKind,omitempty"`
}

type observabilityTrafficSummary struct {
	WindowSeconds      int64    `json:"windowSeconds"`
	ProviderChecks     int64    `json:"providerChecks"`
	Successes          int64    `json:"successes"`
	Failures           int64    `json:"failures"`
	Discarded          int64    `json:"discarded"`
	RateLimitedChecks  int64    `json:"rateLimitedChecks"`
	Interrupted        int64    `json:"interrupted"`
	Incomplete         int64    `json:"incomplete"`
	SuccessRate        *float64 `json:"successRate"`
	AverageDurationMS  *int64   `json:"averageDurationMs"`
	P95DurationMS      *int64   `json:"p95DurationMs"`
	P95Method          string   `json:"p95Method"`
	DispatchAttempts   int64    `json:"dispatchAttempts"`
	Recorded7d         int64    `json:"recorded7d"`
	RecordedWindowDays int      `json:"recordedWindowDays"`
	HistoryAvailable   bool     `json:"historyAvailable"`
}

type observabilityProvider struct {
	ID                  string                      `json:"id"`
	DisplayName         string                      `json:"displayName"`
	TestOnly            bool                        `json:"testOnly"`
	HealthState         observability.ProviderState `json:"healthState"`
	HealthReasons       []string                    `json:"healthReasons"`
	Flags               observability.ProviderFlags `json:"flags"`
	ActiveTargets       int                         `json:"activeTargets"`
	LastProviderCheckAt *time.Time                  `json:"lastProviderCheckAt"`
	LastSuccessAt       *time.Time                  `json:"lastSuccessAt"`
	LastFailureAt       *time.Time                  `json:"lastFailureAt"`
	LastErrorKind       string                      `json:"lastErrorKind,omitempty"`
	ConsecutiveFailures int                         `json:"consecutiveFailures"`
	OpenEpisode         bool                        `json:"openEpisode"`
	Adaptive            observabilityAdaptive       `json:"adaptive"`
	ProviderChecks      observabilityChecks         `json:"providerChecks"`
	Queue               *observabilityQueue         `json:"queue"`
}

type observabilityAdaptive struct {
	Level                    int                   `json:"level"`
	AdaptiveIntervalSeconds  int64                 `json:"adaptiveIntervalSeconds"`
	EffectiveIntervalSeconds int64                 `json:"effectiveIntervalSeconds"`
	RecoverySuccesses        int                   `json:"recoverySuccesses"`
	RequiredSuccesses        int                   `json:"requiredSuccesses"`
	RemainingSuccesses       int                   `json:"remainingSuccesses"`
	Cooldown                 observabilityCooldown `json:"cooldown"`
	Last429At                *time.Time            `json:"last429At"`
	LastSuccessAt            *time.Time            `json:"lastSuccessAt"`
	HistoryCompleteness      string                `json:"historyCompleteness"`
	HistoryAvailable         bool                  `json:"historyAvailable"`
}

type observabilityCooldown struct {
	Status           string     `json:"status"`
	Until            *time.Time `json:"until"`
	RemainingSeconds int64      `json:"remainingSeconds"`
}

type observabilityChecks struct {
	Last1m            int64    `json:"last1m"`
	Last5m            int64    `json:"last5m"`
	Last15m           int64    `json:"last15m"`
	Last1h            int64    `json:"last1h"`
	Last24h           int64    `json:"last24h"`
	Successes24h      int64    `json:"successes24h"`
	Failures24h       int64    `json:"failures24h"`
	Discarded24h      int64    `json:"discarded24h"`
	RateLimited24h    int64    `json:"rateLimitedChecks24h"`
	Interrupted24h    int64    `json:"interrupted24h"`
	Incomplete24h     int64    `json:"incomplete24h"`
	Recorded7d        int64    `json:"recorded7d"`
	SuccessRate       *float64 `json:"successRate"`
	AverageDurationMS *int64   `json:"averageDurationMs"`
	P95DurationMS     *int64   `json:"p95DurationMs"`
	P95Method         string   `json:"p95Method"`
}

type observabilityQueue struct {
	Available               bool       `json:"available"`
	SampledAt               *time.Time `json:"sampledAt"`
	Stale                   bool       `json:"stale"`
	SampleIntervalSeconds   int64      `json:"sampleIntervalSeconds"`
	Aggregation             string     `json:"aggregation"`
	Due                     int        `json:"due"`
	InFlight                int        `json:"inFlight"`
	Queued                  int        `json:"queued"`
	Overdue                 int        `json:"overdue"`
	Deferred                int        `json:"deferred"`
	DeferredByCooldown      int        `json:"deferredByCooldown"`
	DeferredBySpacing       int        `json:"deferredBySpacing"`
	DeferredByConcurrency   int        `json:"deferredByConcurrency"`
	OldestPendingAgeSeconds *int64     `json:"oldestPendingAgeSeconds"`
	NextEligibleAt          *time.Time `json:"nextEligibleAt"`
	BlockedByConcurrency    bool       `json:"blockedByConcurrency"`
}

type observabilityProviderDetail struct {
	observabilityProvider
	WindowSeconds   int64                         `json:"windowSeconds"`
	TrafficHistory  []observabilityTrafficPoint   `json:"trafficHistory"`
	QueueHistory    []observabilityQueuePoint     `json:"queueHistory"`
	RateLimitEvents []observabilityRateLimitEvent `json:"rateLimitEvents"`
	RecentChecks    []observabilityCheckActivity  `json:"recentChecks"`
	Targets         []observabilityTarget         `json:"targets"`
}

type observabilityCheckActivity struct {
	OccurredAt        time.Time `json:"occurredAt"`
	Outcome           string    `json:"outcome"`
	ProviderErrorKind string    `json:"providerErrorKind,omitempty"`
	DurationMS        *int64    `json:"durationMs"`
	QueueWaitMS       *int64    `json:"queueWaitMs"`
	MonitorID         string    `json:"monitorId"`
	MonitorLabel      string    `json:"monitorLabel"`
	Incomplete        bool      `json:"incomplete"`
}

type observabilityTrafficPoint struct {
	BucketStart       time.Time `json:"bucketStart"`
	ProviderChecks    int64     `json:"providerChecks"`
	Successes         int64     `json:"successes"`
	Failures          int64     `json:"failures"`
	Discarded         int64     `json:"discarded"`
	RateLimited       int64     `json:"rateLimitedChecks"`
	Interrupted       int64     `json:"interrupted"`
	Incomplete        int64     `json:"incomplete"`
	AverageDurationMS *int64    `json:"averageDurationMs"`
	P95DurationMS     *int64    `json:"p95DurationMs"`
}

type observabilityQueuePoint struct {
	BucketStart         time.Time `json:"bucketStart"`
	DueAvg              int64     `json:"dueAvg"`
	DueMax              int64     `json:"dueMax"`
	OverdueMax          int64     `json:"overdueMax"`
	DeferredAvg         int64     `json:"deferredAvg"`
	DeferredMax         int64     `json:"deferredMax"`
	InFlightMax         int64     `json:"inFlightMax"`
	OldestDueAgeMaxMS   int64     `json:"oldestDueAgeMaxMs"`
	Dispatched          int64     `json:"dispatchAttempts"`
	DeferralCooldown    int64     `json:"deferralEpisodesCooldown"`
	DeferralSpacing     int64     `json:"deferralEpisodesSpacing"`
	DeferralConcurrency int64     `json:"deferralEpisodesConcurrency"`
}

type observabilityRateLimitEvent struct {
	OccurredAt            time.Time  `json:"occurredAt"`
	Kind                  string     `json:"kind"`
	PreviousLevel         int        `json:"previousLevel"`
	EscalationLevel       int        `json:"escalationLevel"`
	Escalated             bool       `json:"escalated"`
	Maintained            bool       `json:"maintained"`
	PreviousAdaptiveMS    int64      `json:"previousAdaptiveMs"`
	AdaptiveMS            int64      `json:"adaptiveMs"`
	PreviousCooldownUntil *time.Time `json:"previousCooldownUntil"`
	CooldownUntil         *time.Time `json:"cooldownUntil"`
	RetryAfterMS          int64      `json:"retryAfterMs"`
	Derived               bool       `json:"derived"`
}

type observabilityTarget struct {
	MonitorID           string     `json:"monitorId"`
	MonitorLabel        string     `json:"monitorLabel"`
	NextDueAt           *time.Time `json:"nextDueAt"`
	ConsecutiveFailures int        `json:"consecutiveFailures"`
	LastOutcome         string     `json:"lastOutcome,omitempty"`
	LastErrorKind       string     `json:"lastErrorKind,omitempty"`
	QueueState          string     `json:"queueState"`
}

func (s *Server) handleObservability(w http.ResponseWriter, r *http.Request) {
	if s.observability == nil {
		writeError(w, r, http.StatusServiceUnavailable, "observability_unavailable", "observability is temporarily unavailable")
		return
	}
	if !requireGet(w, r) {
		return
	}
	if r.URL.Path == "/api/v1/observability" {
		s.handleObservabilityOverview(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/v1/observability/providers/") {
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/observability/providers/")
		if rest == "" || strings.Contains(rest, "/") {
			writeError(w, r, http.StatusNotFound, "not_found", "not found")
			return
		}
		s.handleObservabilityProvider(w, r, provider.ID(rest))
		return
	}
	writeError(w, r, http.StatusNotFound, "not_found", "not found")
}

func (s *Server) handleObservabilityOverview(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	now := s.now().UTC()
	interval, monitoring, err := s.observabilitySettings(ctx)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	healthRecords, err := s.observability.ListProviderHealth(ctx)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	limits, err := s.observability.ListProviderRateLimits(ctx)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	summaries, err := s.observability.ProviderScheduleSummaries(ctx, now)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	incomplete, err := s.observability.CountIncompleteChecksByProvider(ctx, now.Add(-24*time.Hour))
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	shortTrafficRows, err := s.observability.AggregateProviderTraffic(ctx, now.Add(-time.Hour), now)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	traffic24, err := s.observability.SumProviderTraffic(ctx, now.Add(-24*time.Hour))
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	traffic7d, err := s.observability.SumProviderTraffic(ctx, now.Add(-7*24*time.Hour))
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	snapshot := observability.SchedulerSnapshot{Available: false, Providers: map[provider.ID]observability.ProviderQueueSnapshot{}}
	if s.schedulerSnapshot != nil {
		snapshot = s.schedulerSnapshot()
	}
	snapshotStale := snapshot.SampledAt == nil || now.Sub(*snapshot.SampledAt) > observability.SnapshotStaleAfter
	schedulerUsable := snapshot.Available && snapshot.Running && !snapshot.Stopped && !snapshotStale
	healthByID := map[provider.ID]store.ProviderHealthRecord{}
	for _, record := range healthRecords {
		healthByID[provider.ID(record.ProviderID)] = record
	}
	limitsByID := strictestRateLimits(limits)
	shortByID := aggregateShortTraffic(shortTrafficRows, now)
	meta := []provider.Metadata{}
	if s.providers != nil {
		meta = s.providers.List()
	}
	rows := make([]observabilityProvider, 0, len(meta))
	global := observabilityTrafficSummary{
		WindowSeconds: 86400, P95Method: p95Method, RecordedWindowDays: 7,
	}
	var activeCooldowns, degraded int
	var globalIncomplete int64
	for _, item := range meta {
		health := healthByID[item.ID]
		limit := limitsByID[item.ID]
		summary := summaries[item.ID]
		state := observability.DeriveProviderState(observability.ProviderStateInput{
			Now:                now,
			ConfiguredInterval: interval,
			MonitoringEnabled:  monitoring,
			ActiveTargets:      summary.ActiveTargets,
			NextDueAt:          summary.EarliestNextDueAt,
			Health:             healthSnapshot(health),
			RateLimit:          limit,
			RequiredSuccesses:  3,
			SchedulerAvailable: schedulerUsable,
		})
		queue, ok := snapshot.Providers[item.ID]
		var queueDTO *observabilityQueue
		if ok && snapshot.Available {
			queueDTO = queueResponse(queue, snapshot.SampledAt, now)
		}
		short := shortByID[item.ID]
		t24 := traffic24[item.ID]
		t7 := traffic7d[item.ID]
		incomplete24 := incomplete[item.ID]
		checks := checksResponse(short, t24, t7, incomplete24)
		adaptive := adaptiveResponse(limit, interval, now)
		row := observabilityProvider{
			ID:                  string(item.ID),
			DisplayName:         item.DisplayName,
			TestOnly:            item.TestOnly,
			HealthState:         state.State,
			HealthReasons:       state.Reasons,
			Flags:               state.Flags,
			ActiveTargets:       summary.ActiveTargets,
			LastProviderCheckAt: utcOptional(health.LastAttemptAt),
			LastSuccessAt:       utcOptional(health.LastSuccessAt),
			LastFailureAt:       utcOptional(health.LastFailureAt),
			LastErrorKind:       health.LastErrorKind,
			ConsecutiveFailures: health.ConsecutiveFailures,
			OpenEpisode:         health.OpenEpisode != nil,
			Adaptive:            adaptive,
			ProviderChecks:      checks,
			Queue:               queueDTO,
		}
		if state.State == observability.StateCoolingDown {
			activeCooldowns++
		}
		if state.Flags.Degraded {
			degraded++
		}
		rows = append(rows, row)

		global.ProviderChecks += t24.Checks
		global.Successes += t24.Successes
		global.Failures += t24.Failures
		global.Discarded += t24.Discarded
		global.RateLimitedChecks += t24.RateLimited
		global.Interrupted += t24.Interrupted
		global.Incomplete += t24.Incomplete
		global.Recorded7d += t7.Checks
		global.DispatchAttempts += t24.DispatchAttempts
		globalIncomplete += incomplete24
	}
	global.Incomplete += globalIncomplete
	global.HistoryAvailable = global.Recorded7d > 0 || global.ProviderChecks > 0
	global.SuccessRate = ratio(global.Successes, global.Successes+global.Failures)
	{
		var sum, count int64
		var buckets [10]int64
		for _, totals := range traffic24 {
			sum += totals.DurationSumMS
			count += totals.DurationCount
			for i := range buckets {
				buckets[i] += totals.Buckets[i]
			}
		}
		if count > 0 {
			value := sum / count
			global.AverageDurationMS = &value
			global.P95DurationMS = p95FromBuckets(buckets, count)
		}
	}
	writeJSON(w, http.StatusOK, observabilityResponse{
		GeneratedAt: now,
		Coverage: observabilityCoverage{
			CheckDetailDays: 14, RollupDays: 7, RateLimitHistoryDays: 90,
		},
		Scheduling: observabilityScheduling{
			IntervalScope:             "global",
			ConfiguredIntervalSeconds: int64(interval / time.Second),
			ProviderSpacingSeconds:    int64(snapshot.ProviderSpacing / time.Second),
			QueueSampleIntervalSecs:   int64(observability.DefaultQueueSampleInterval / time.Second),
			Workers:                   snapshot.Workers,
		},
		Scheduler: observabilityScheduler{
			Available:              snapshot.Available,
			Running:                snapshot.Running,
			Stopped:                snapshot.Stopped,
			MonitoringEnabled:      monitoring,
			Paused:                 snapshot.Paused,
			LastExecutionAt:        utcOptional(snapshot.LastExecutionAt),
			LastDispatchAt:         utcOptional(snapshot.LastDispatchAt),
			DueTargets:             snapshot.DueTargets,
			OverdueTargets:         snapshot.OverdueTargets,
			QueuedTargets:          snapshot.QueuedTargets,
			DeferredTargets:        snapshot.DeferredTargets,
			InFlightTargets:        snapshot.InFlightTargets,
			NextEligibleAt:         utcOptional(snapshot.NextEligibleAt),
			SampledAt:              utcOptional(snapshot.SampledAt),
			Stale:                  snapshotStale,
			ActiveCooldowns:        activeCooldowns,
			DegradedProviders:      degraded,
			IncompleteChecks:       global.Incomplete,
			RateLimitEventsDropped: s.rateLimitEventsDropped(),
			LastErrorKind:          snapshot.LastErrorKind,
		},
		TrafficSummary: global,
		Providers:      rows,
	})
}

func (s *Server) handleObservabilityProvider(w http.ResponseWriter, r *http.Request, providerID provider.ID) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	now := s.now().UTC()
	interval, monitoring, err := s.observabilitySettings(ctx)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	meta, ok := s.providerMetadata(providerID)
	if !ok {
		writeError(w, r, http.StatusNotFound, "not_found", "provider not found")
		return
	}
	window, bucket, err := parseObservabilityWindow(r.URL.Query().Get("window"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_error", err.Error())
		return
	}
	since := now.Add(-window)
	rows, err := s.observability.ListProviderMinuteMetrics(ctx, providerID, since)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	healthRecords, err := s.observability.ListProviderHealth(ctx)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	limits, err := s.observability.ListProviderRateLimits(ctx)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	summaries, err := s.observability.ProviderScheduleSummaries(ctx, now)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	targets, err := s.observability.ListProviderActiveTargets(ctx, providerID, now, 100)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	events, err := s.observability.ListProviderRateLimitEvents(ctx, providerID, now.Add(-90*24*time.Hour), 100)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	checksActivity, err := s.observability.ListProviderCheckActivity(ctx, providerID, now.Add(-24*time.Hour), 50)
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	incomplete, err := s.observability.CountIncompleteChecksByProvider(ctx, now.Add(-24*time.Hour))
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	traffic24, err := s.observability.SumProviderTraffic(ctx, now.Add(-24*time.Hour))
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	traffic7d, err := s.observability.SumProviderTraffic(ctx, now.Add(-7*24*time.Hour))
	if err != nil {
		s.writeReadModelError(w, r, err, "")
		return
	}
	snapshot := observability.SchedulerSnapshot{Available: false, Providers: map[provider.ID]observability.ProviderQueueSnapshot{}}
	if s.schedulerSnapshot != nil {
		snapshot = s.schedulerSnapshot()
	}
	snapshotStale := snapshot.SampledAt == nil || now.Sub(*snapshot.SampledAt) > observability.SnapshotStaleAfter
	schedulerUsable := snapshot.Available && snapshot.Running && !snapshot.Stopped && !snapshotStale
	var health store.ProviderHealthRecord
	for _, record := range healthRecords {
		if record.ProviderID == string(providerID) {
			health = record
			break
		}
	}
	limit := strictestRateLimits(limits)[providerID]
	summary := summaries[providerID]
	state := observability.DeriveProviderState(observability.ProviderStateInput{
		Now: now, ConfiguredInterval: interval, MonitoringEnabled: monitoring,
		ActiveTargets: summary.ActiveTargets, NextDueAt: summary.EarliestNextDueAt,
		Health: healthSnapshot(health), RateLimit: limit, RequiredSuccesses: 3,
		SchedulerAvailable: schedulerUsable,
	})
	queue, hasQueue := snapshot.Providers[providerID]
	var queueDTO *observabilityQueue
	if hasQueue && snapshot.Available {
		queueDTO = queueResponse(queue, snapshot.SampledAt, now)
	}
	trafficPoints := trafficHistory(rows, bucket)
	queuePoints := queueHistory(rows, bucket)
	eventDTOs := make([]observabilityRateLimitEvent, 0, len(events))
	for _, event := range events {
		escalated := event.Kind == store.RateLimitEventRateLimited && event.EscalationLevel > event.PreviousLevel
		maintained := event.Kind == store.RateLimitEventRateLimited && event.EscalationLevel <= event.PreviousLevel
		eventDTOs = append(eventDTOs, observabilityRateLimitEvent{
			OccurredAt: event.OccurredAt, Kind: event.Kind,
			PreviousLevel: event.PreviousLevel, EscalationLevel: event.EscalationLevel,
			Escalated: escalated, Maintained: maintained,
			PreviousAdaptiveMS:    event.PreviousAdaptive.Milliseconds(),
			AdaptiveMS:            event.Adaptive.Milliseconds(),
			PreviousCooldownUntil: utcOptional(event.PreviousCooldownUntil),
			CooldownUntil:         utcOptional(event.CooldownUntil),
			RetryAfterMS:          event.RetryAfter.Milliseconds(),
		})
	}
	checkDTOs := make([]observabilityCheckActivity, 0, len(checksActivity))
	for _, check := range checksActivity {
		checkDTOs = append(checkDTOs, observabilityCheckActivity{
			OccurredAt: check.StartedAt, Outcome: check.Outcome,
			ProviderErrorKind: check.ProviderErrorKind, DurationMS: check.DurationMS,
			QueueWaitMS: check.QueueWaitMS, MonitorID: check.MonitorID,
			MonitorLabel: check.MonitorLabel, Incomplete: check.Incomplete,
		})
	}
	targetDTOs := make([]observabilityTarget, 0, len(targets))
	for _, target := range targets {
		targetDTOs = append(targetDTOs, observabilityTarget{
			MonitorID: target.MonitorID, MonitorLabel: target.MonitorLabel,
			NextDueAt: utcOptional(target.NextDueAt), ConsecutiveFailures: target.ConsecutiveFailures,
			LastOutcome: target.LastOutcome, LastErrorKind: target.LastErrorKind,
			QueueState: targetQueueState(target.NextDueAt, now),
		})
	}
	checks := checksResponse(
		shortWindowsFromRows(rows, now),
		traffic24[providerID],
		traffic7d[providerID],
		incomplete[providerID],
	)
	detail := observabilityProviderDetail{
		observabilityProvider: observabilityProvider{
			ID: string(meta.ID), DisplayName: meta.DisplayName, TestOnly: meta.TestOnly,
			HealthState: state.State, HealthReasons: state.Reasons, Flags: state.Flags,
			ActiveTargets: summary.ActiveTargets, LastProviderCheckAt: utcOptional(health.LastAttemptAt),
			LastSuccessAt: utcOptional(health.LastSuccessAt), LastFailureAt: utcOptional(health.LastFailureAt),
			LastErrorKind: health.LastErrorKind, ConsecutiveFailures: health.ConsecutiveFailures,
			OpenEpisode: health.OpenEpisode != nil, Adaptive: adaptiveResponse(limit, interval, now),
			ProviderChecks: checks, Queue: queueDTO,
		},
		WindowSeconds: int64(window / time.Second), TrafficHistory: trafficPoints,
		QueueHistory: queuePoints, RateLimitEvents: eventDTOs, RecentChecks: checkDTOs,
		Targets: targetDTOs,
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) rateLimitEventsDropped() uint64 {
	if s == nil || s.rateLimitDropped == nil {
		return 0
	}
	return s.rateLimitDropped()
}

func (s *Server) observabilitySettings(ctx context.Context) (time.Duration, bool, error) {
	interval, err := s.observability.PollInterval(ctx)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			interval = domain.DefaultPollInterval
		} else {
			return 0, false, err
		}
	}
	monitoring, err := s.observability.MonitoringEnabled(ctx)
	if err != nil {
		return 0, false, err
	}
	return interval, monitoring, nil
}

func (s *Server) providerMetadata(id provider.ID) (provider.Metadata, bool) {
	if s.providers == nil {
		return provider.Metadata{}, false
	}
	for _, meta := range s.providers.List() {
		if meta.ID == id {
			return meta, true
		}
	}
	return provider.Metadata{}, false
}

func strictestRateLimits(limits []domain.ProviderRateLimit) map[provider.ID]domain.ProviderRateLimit {
	result := make(map[provider.ID]domain.ProviderRateLimit)
	for _, limit := range limits {
		current, ok := result[limit.ProviderID]
		if !ok {
			result[limit.ProviderID] = limit
			continue
		}
		if limit.AdaptiveInterval > current.AdaptiveInterval {
			current.AdaptiveInterval = limit.AdaptiveInterval
		}
		if limit.Consecutive429s > current.Consecutive429s {
			current.Consecutive429s = limit.Consecutive429s
		}
		if limit.ConsecutiveSuccesses > current.ConsecutiveSuccesses {
			current.ConsecutiveSuccesses = limit.ConsecutiveSuccesses
		}
		if limit.CooldownUntil != nil && (current.CooldownUntil == nil || limit.CooldownUntil.After(*current.CooldownUntil)) {
			current.CooldownUntil = limit.CooldownUntil
		}
		if limit.Last429At != nil && (current.Last429At == nil || limit.Last429At.After(*current.Last429At)) {
			current.Last429At = limit.Last429At
		}
		if limit.LastSuccessAt != nil && (current.LastSuccessAt == nil || limit.LastSuccessAt.After(*current.LastSuccessAt)) {
			current.LastSuccessAt = limit.LastSuccessAt
		}
		if limit.LastRetryAfter > current.LastRetryAfter {
			current.LastRetryAfter = limit.LastRetryAfter
		}
		result[limit.ProviderID] = current
	}
	return result
}

func healthSnapshot(record store.ProviderHealthRecord) *observability.HealthSnapshot {
	if record.ProviderID == "" {
		return nil
	}
	return &observability.HealthSnapshot{
		LastAttemptAt: record.LastAttemptAt, LastSuccessAt: record.LastSuccessAt,
		LastFailureAt: record.LastFailureAt, LastErrorKind: record.LastErrorKind,
		ConsecutiveFailures: record.ConsecutiveFailures, OpenEpisode: record.OpenEpisode != nil,
	}
}

func adaptiveResponse(limit domain.ProviderRateLimit, configured time.Duration, now time.Time) observabilityAdaptive {
	required := 3
	remaining := required - limit.ConsecutiveSuccesses
	if remaining < 0 {
		remaining = 0
	}
	cooldown := observabilityCooldown{Status: "none"}
	if limit.CooldownUntil != nil {
		cooldown.Until = utcOptional(limit.CooldownUntil)
		if limit.CooldownUntil.After(now) {
			cooldown.Status = "active"
			cooldown.RemainingSeconds = int64(observability.CooldownRemaining(now, limit.CooldownUntil) / time.Second)
		} else {
			cooldown.Status = "expired"
		}
	}
	return observabilityAdaptive{
		Level: limit.Consecutive429s, AdaptiveIntervalSeconds: int64(limit.AdaptiveInterval / time.Second),
		EffectiveIntervalSeconds: int64(observability.EffectiveInterval(configured, limit) / time.Second),
		RecoverySuccesses:        limit.ConsecutiveSuccesses, RequiredSuccesses: required,
		RemainingSuccesses: remaining, Cooldown: cooldown,
		Last429At: utcOptional(limit.Last429At), LastSuccessAt: utcOptional(limit.LastSuccessAt),
		HistoryCompleteness: "best_effort", HistoryAvailable: true,
	}
}

func queueResponse(queue observability.ProviderQueueSnapshot, sampledAt *time.Time, now time.Time) *observabilityQueue {
	var oldest *int64
	if queue.OldestPendingAge > 0 {
		value := int64(queue.OldestPendingAge / time.Second)
		oldest = &value
	}
	stale := sampledAt == nil || sampledAt.IsZero() || now.Sub(*sampledAt) > observability.QueueStaleAfter
	return &observabilityQueue{
		Available: true, SampledAt: utcOptional(sampledAt), Stale: stale,
		SampleIntervalSeconds: int64(observability.DefaultQueueSampleInterval / time.Second),
		Aggregation:           "sampled_point_in_time",
		Due:                   queue.Due, InFlight: queue.InFlight, Queued: queue.Queued,
		Overdue: queue.Overdue, Deferred: queue.Deferred,
		DeferredByCooldown: queue.DeferredByCooldown, DeferredBySpacing: queue.DeferredBySpacing,
		DeferredByConcurrency:   queue.DeferredByConcurrency,
		OldestPendingAgeSeconds: oldest, NextEligibleAt: utcOptional(queue.NextEligibleAt),
		BlockedByConcurrency: queue.BlockedByConcurrency,
	}
}

func checksResponse(short shortWindows, totals24, totals7 store.ProviderTrafficTotals, incomplete24 int64) observabilityChecks {
	rate := ratio(totals24.Successes, totals24.Successes+totals24.Failures)
	var avg *int64
	if totals24.DurationCount > 0 {
		value := totals24.DurationSumMS / totals24.DurationCount
		avg = &value
	}
	return observabilityChecks{
		Last1m: short.Last1m.Checks, Last5m: short.Last5m.Checks,
		Last15m: short.Last15m.Checks, Last1h: short.Last1h.Checks,
		Last24h: totals24.Checks, Successes24h: totals24.Successes, Failures24h: totals24.Failures,
		Discarded24h: totals24.Discarded, RateLimited24h: totals24.RateLimited,
		Interrupted24h: totals24.Interrupted, Incomplete24h: totals24.Incomplete + incomplete24,
		Recorded7d: totals7.Checks, SuccessRate: rate, AverageDurationMS: avg,
		P95DurationMS: p95FromBuckets(totals24.Buckets, totals24.DurationCount), P95Method: p95Method,
	}
}

type shortWindows struct {
	Last1m, Last5m, Last15m, Last1h store.ProviderTrafficTotals
}

func aggregateShortTraffic(minutes []store.ProviderTrafficMinute, now time.Time) map[provider.ID]shortWindows {
	result := make(map[provider.ID]shortWindows)
	for _, minute := range minutes {
		windows := result[minute.ProviderID]
		if !minute.MinuteStart.Before(now.Add(-time.Hour)) {
			addTrafficMinute(&windows.Last1h, minute)
		}
		if !minute.MinuteStart.Before(now.Add(-15 * time.Minute)) {
			addTrafficMinute(&windows.Last15m, minute)
		}
		if !minute.MinuteStart.Before(now.Add(-5 * time.Minute)) {
			addTrafficMinute(&windows.Last5m, minute)
		}
		if !minute.MinuteStart.Before(now.Add(-time.Minute)) {
			addTrafficMinute(&windows.Last1m, minute)
		}
		result[minute.ProviderID] = windows
	}
	return result
}

func addTrafficMinute(totals *store.ProviderTrafficTotals, minute store.ProviderTrafficMinute) {
	totals.Checks += minute.Checks
	totals.Successes += minute.Successes
	totals.Failures += minute.Failures
	totals.RateLimited += minute.RateLimited
	totals.Interrupted += minute.Interrupted
	totals.Discarded += minute.Discarded
	totals.Incomplete += minute.Incomplete
	totals.DurationSumMS += minute.DurationSumMS
	totals.DurationCount += minute.DurationCount
	for i := range totals.Buckets {
		totals.Buckets[i] += minute.Buckets[i]
	}
}

func shortWindowsFromRows(rows []store.ProviderMinuteMetrics, now time.Time) shortWindows {
	var result shortWindows
	for _, row := range rows {
		addRow := func(target *store.ProviderTrafficTotals) {
			target.Checks += row.ProviderChecks
			target.Successes += row.Successes
			target.Failures += row.Failures
			target.RateLimited += row.RateLimited
			target.Interrupted += row.Interrupted
			target.Discarded += row.Discarded
			target.Incomplete += row.Incomplete
			target.DurationSumMS += row.DurationSumMS
			target.DurationCount += row.DurationCount
			for i := range target.Buckets {
				target.Buckets[i] += row.Buckets[i]
			}
		}
		if !row.MinuteStart.Before(now.Add(-time.Hour)) {
			addRow(&result.Last1h)
		}
		if !row.MinuteStart.Before(now.Add(-15 * time.Minute)) {
			addRow(&result.Last15m)
		}
		if !row.MinuteStart.Before(now.Add(-5 * time.Minute)) {
			addRow(&result.Last5m)
		}
		if !row.MinuteStart.Before(now.Add(-time.Minute)) {
			addRow(&result.Last1m)
		}
	}
	return result
}

func trafficHistory(rows []store.ProviderMinuteMetrics, bucket time.Duration) []observabilityTrafficPoint {
	type bucketValue struct {
		point         observabilityTrafficPoint
		durationSum   int64
		durationCount int64
		buckets       [10]int64
	}
	byBucket := map[int64]*bucketValue{}
	for _, row := range rows {
		start := row.MinuteStart.Truncate(bucket)
		value := byBucket[start.Unix()]
		if value == nil {
			value = &bucketValue{point: observabilityTrafficPoint{BucketStart: start}}
			byBucket[start.Unix()] = value
		}
		value.point.ProviderChecks += row.ProviderChecks
		value.point.Successes += row.Successes
		value.point.Failures += row.Failures
		value.point.Discarded += row.Discarded
		value.point.RateLimited += row.RateLimited
		value.point.Interrupted += row.Interrupted
		value.point.Incomplete += row.Incomplete
		value.durationSum += row.DurationSumMS
		value.durationCount += row.DurationCount
		for i := range value.buckets {
			value.buckets[i] += row.Buckets[i]
		}
	}
	result := make([]observabilityTrafficPoint, 0, len(byBucket))
	for _, value := range byBucket {
		if value.durationCount > 0 {
			average := value.durationSum / value.durationCount
			value.point.AverageDurationMS = &average
			value.point.P95DurationMS = p95FromBuckets(value.buckets, value.durationCount)
		}
		result = append(result, value.point)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].BucketStart.Before(result[j].BucketStart) })
	return result
}

func queueHistory(rows []store.ProviderMinuteMetrics, bucket time.Duration) []observabilityQueuePoint {
	byBucket := map[int64]*observabilityQueuePoint{}
	samplesByBucket := map[int64]int64{}
	for _, row := range rows {
		if row.SchedulerSamples == 0 {
			continue
		}
		start := row.MinuteStart.Truncate(bucket)
		point := byBucket[start.Unix()]
		if point == nil {
			point = &observabilityQueuePoint{BucketStart: start}
			byBucket[start.Unix()] = point
		}
		samplesByBucket[start.Unix()] += row.SchedulerSamples
		point.DueAvg += row.DueAvg * row.SchedulerSamples
		if row.DueMax > point.DueMax {
			point.DueMax = row.DueMax
		}
		if row.OverdueMax > point.OverdueMax {
			point.OverdueMax = row.OverdueMax
		}
		point.DeferredAvg += row.DeferredAvg * row.SchedulerSamples
		if row.DeferredMax > point.DeferredMax {
			point.DeferredMax = row.DeferredMax
		}
		if row.InFlightMax > point.InFlightMax {
			point.InFlightMax = row.InFlightMax
		}
		if row.OldestDueAgeMaxMS > point.OldestDueAgeMaxMS {
			point.OldestDueAgeMaxMS = row.OldestDueAgeMaxMS
		}
		point.Dispatched += row.DispatchAttempts
		point.DeferralCooldown += row.DeferralCooldown
		point.DeferralSpacing += row.DeferralSpacing
		point.DeferralConcurrency += row.DeferralConcurrency
	}
	result := make([]observabilityQueuePoint, 0, len(byBucket))
	for key, point := range byBucket {
		divisor := samplesByBucket[key]
		if divisor <= 0 {
			divisor = 1
		}
		point.DueAvg /= divisor
		point.DeferredAvg /= divisor
		result = append(result, *point)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].BucketStart.Before(result[j].BucketStart) })
	return result
}

func targetQueueState(nextDue *time.Time, now time.Time) string {
	if nextDue == nil {
		return "unscheduled"
	}
	if !nextDue.After(now) {
		return "due"
	}
	return "scheduled"
}

func parseObservabilityWindow(raw string) (time.Duration, time.Duration, error) {
	switch raw {
	case "", "24h":
		return 24 * time.Hour, 15 * time.Minute, nil
	case "1h":
		return time.Hour, time.Minute, nil
	case "6h":
		return 6 * time.Hour, 5 * time.Minute, nil
	case "7d":
		return 7 * 24 * time.Hour, time.Hour, nil
	default:
		return 0, 0, errors.New("window must be one of 1h, 6h, 24h, 7d")
	}
}

// p95FromBuckets implements nearest-rank P95 over disjoint buckets. The
// returned value is the upper bound of the containing bucket. The overflow
// bucket has no finite upper bound, so P95 is nil when it falls there.
func p95FromBuckets(buckets [10]int64, count int64) *int64 {
	if count <= 0 {
		return nil
	}
	bounds := []int64{50, 100, 250, 500, 1000, 2000, 5000, 10000, 20000}
	target := (count*95 + 99) / 100
	var cumulative int64
	for index, bucket := range buckets {
		cumulative += bucket
		if cumulative >= target {
			if index >= len(bounds) {
				return nil
			}
			value := bounds[index]
			return &value
		}
	}
	return nil
}

func ratio(numerator, denominator int64) *float64 {
	if denominator <= 0 {
		return nil
	}
	value := float64(numerator) / float64(denominator)
	return &value
}
