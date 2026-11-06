package observability

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/am-fallah/neda/internal/provider"
)

// DeferralReason is the primary constraint that kept a due target queued.
type DeferralReason string

const (
	ReasonCooldown    DeferralReason = "cooldown"
	ReasonSpacing     DeferralReason = "spacing"
	ReasonConcurrency DeferralReason = "concurrency"
)

// DeferralObservation is one due target that was not dispatched in a pass.
// Due is diagnostic only; episode identity is target + reason.
type DeferralObservation struct {
	TargetID   int64
	ProviderID provider.ID
	Due        time.Time
	Reason     DeferralReason
}

// TargetObservation is one active target examined in a pass. The recorder uses
// it to resolve episodes when a target is no longer deferred, deleted, or
// inactive. Due-time changes do not affect episode identity.
type TargetObservation struct {
	TargetID   int64
	ProviderID provider.ID
	NextDueAt  *time.Time
}

// ProviderPassObservation is the per-provider runtime state for one pass.
type ProviderPassObservation struct {
	ProviderID            provider.ID
	Due                   int
	InFlight              int
	Queued                int
	Overdue               int
	OldestPendingAge      time.Duration
	Dispatched            int
	Deferred              int
	DeferredByCooldown    int
	DeferredBySpacing     int
	DeferredByConcurrency int
	NextEligibleAt        *time.Time
	BlockedByConcurrency  bool
	Deferrals             []DeferralObservation
}

// PassObservation is the scheduler state published once per loop pass.
type PassObservation struct {
	Now                time.Time
	MonitoringEnabled  bool
	Paused             bool
	ConfiguredInterval time.Duration
	ProviderSpacing    time.Duration
	Workers            int
	RunningWorkers     int
	LastDispatchAt     *time.Time
	LastErrorKind      string
	Providers          []ProviderPassObservation
	Targets            []TargetObservation
	// TargetsComplete is true only when the scheduler successfully loaded the
	// complete active-target list. Without it, a target's absence is unknown
	// rather than evidence that its deferral episode resolved.
	TargetsComplete bool
}

// ProviderQueueSnapshot is the current queue state for one provider.
type ProviderQueueSnapshot struct {
	Due                   int           `json:"due"`
	InFlight              int           `json:"inFlight"`
	Queued                int           `json:"queued"`
	Overdue               int           `json:"overdue"`
	Deferred              int           `json:"deferred"`
	DeferredByCooldown    int           `json:"deferredByCooldown"`
	DeferredBySpacing     int           `json:"deferredBySpacing"`
	DeferredByConcurrency int           `json:"deferredByConcurrency"`
	OldestPendingAge      time.Duration `json:"-"`
	NextEligibleAt        *time.Time    `json:"nextEligibleAt"`
	BlockedByConcurrency  bool          `json:"blockedByConcurrency"`
	Dispatched            int           `json:"dispatched"`
}

// SchedulerSnapshot is the immutable current scheduler view returned to the API.
type SchedulerSnapshot struct {
	Available          bool
	Running            bool
	Stopped            bool
	MonitoringEnabled  bool
	Paused             bool
	ConfiguredInterval time.Duration
	ProviderSpacing    time.Duration
	Workers            int
	RunningWorkers     int
	LastExecutionAt    *time.Time
	LastDispatchAt     *time.Time
	DueTargets         int
	OverdueTargets     int
	QueuedTargets      int
	InFlightTargets    int
	DeferredTargets    int
	DispatchAttempts   int64
	NextEligibleAt     *time.Time
	SampledAt          *time.Time
	LastErrorKind      string
	DroppedSamples     int64
	Providers          map[provider.ID]ProviderQueueSnapshot
}

// QueueSample is one durable point-in-time scheduler sample.
type QueueSample struct {
	ID                    string
	ProviderID            provider.ID
	SampledAt             time.Time
	MinuteStart           time.Time
	Due                   int
	InFlight              int
	Queued                int
	Overdue               int
	Deferred              int
	DeferredByCooldown    int
	DeferredBySpacing     int
	DeferredByConcurrency int
	OldestPendingAge      time.Duration
	NextEligibleAt        *time.Time
	BlockedByConcurrency  bool
	Dispatched            int
}

// DeferralEpisode is one logical deferral episode. It is persisted so a process
// restart does not recount the same still-active condition.
type DeferralEpisode struct {
	ID         string
	ProviderID provider.ID
	TargetID   *int64
	Reason     DeferralReason
	StartedAt  time.Time
	LastSeenAt time.Time
	ResolvedAt *time.Time
	CreatedAt  time.Time
}

// EpisodeUpdate is one created, refreshed, or resolved episode.
type EpisodeUpdate struct {
	Episode DeferralEpisode
}

// Recorder owns bounded in-memory scheduler metrics. Updates are expected from
// the single scheduler loop goroutine; Snapshot may be called concurrently by
// HTTP handlers. No database or network work occurs under its lock.
type Recorder struct {
	mu       sync.RWMutex
	snapshot SchedulerSnapshot
	active   map[int64]*DeferralEpisode

	bootID       string
	seq          uint64
	sampleEvery  time.Duration
	nextSampleAt time.Time

	pendingSamples  []QueueSample
	pendingEpisodes []EpisodeUpdate
	droppedSamples  uint64
	droppedEpisodes uint64
}

const (
	maxPendingSamples  = 4096
	maxPendingEpisodes = 4096
)

// NewRecorder returns a recorder with the production sampling cadence.
func NewRecorder() *Recorder {
	return NewRecorderWithSampleInterval(DefaultQueueSampleInterval)
}

// NewRecorderWithSampleInterval returns a recorder with an explicit sampling
// cadence. A zero interval samples every pass; it is intended for deterministic
// tests. A negative interval uses the production cadence.
func NewRecorderWithSampleInterval(interval time.Duration) *Recorder {
	if interval < 0 {
		interval = DefaultQueueSampleInterval
	}
	return &Recorder{
		snapshot:    SchedulerSnapshot{Providers: map[provider.ID]ProviderQueueSnapshot{}},
		active:      map[int64]*DeferralEpisode{},
		bootID:      newIdentity(),
		sampleEvery: interval,
	}
}

// RestoreEpisodes seeds active episodes loaded from durable storage. It must be
// called before the scheduler starts observing passes.
func (r *Recorder) RestoreEpisodes(episodes []DeferralEpisode) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, episode := range episodes {
		if episode.TargetID == nil || episode.ResolvedAt != nil {
			continue
		}
		copy := episode
		r.active[*episode.TargetID] = &copy
	}
}

// ObservePass replaces the current runtime snapshot, advances the durable
// episode lifecycle, and (at the bounded cadence) appends one queue sample per
// provider. It is called from the scheduler loop only.
func (r *Recorder) ObservePass(pass PassObservation) {
	if r == nil {
		return
	}
	now := pass.Now.UTC()
	if now.IsZero() {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if pass.MonitoringEnabled && !pass.Paused {
		r.observeEpisodesLocked(pass, now)
	}

	r.snapshot = buildSnapshot(pass, now)
	if pass.MonitoringEnabled && !pass.Paused && pass.TargetsComplete && r.shouldSampleLocked(now) {
		r.appendSamplesLocked(pass, now)
	}
}

func (r *Recorder) observeEpisodesLocked(pass PassObservation, now time.Time) {
	targets := make(map[int64]TargetObservation, len(pass.Targets))
	for _, target := range pass.Targets {
		targets[target.TargetID] = target
	}
	deferrals := make(map[int64]DeferralObservation, len(pass.Providers))
	for _, providerPass := range pass.Providers {
		for _, deferral := range providerPass.Deferrals {
			deferrals[deferral.TargetID] = deferral
		}
	}

	// Close episodes that are no longer deferred, or whose target disappeared
	// from a complete target list. A reason transition closes the old episode
	// and opens a new one below.
	for targetID, episode := range r.active {
		if pass.TargetsComplete {
			target, ok := targets[targetID]
			if !ok || target.ProviderID != episode.ProviderID {
				r.resolveEpisodeLocked(episode, now)
				continue
			}
		} else if _, observed := targets[targetID]; !observed {
			// The pass did not load a complete target list, so absence is not
			// evidence that the episode resolved.
			continue
		}
		deferral, deferred := deferrals[targetID]
		if !deferred {
			r.resolveEpisodeLocked(episode, now)
			continue
		}
		if deferral.ProviderID != episode.ProviderID || episode.Reason != deferral.Reason {
			r.resolveEpisodeLocked(episode, now)
		} else {
			episode.LastSeenAt = now
		}
	}
	for targetID, deferral := range deferrals {
		if _, ok := r.active[targetID]; ok {
			continue
		}
		r.startEpisodeLocked(targetID, deferral.ProviderID, deferral.Reason, now)
	}
}

func (r *Recorder) startEpisodeLocked(targetID int64, providerID provider.ID, reason DeferralReason, now time.Time) {
	episode := &DeferralEpisode{
		ID:         newIdentity(),
		ProviderID: providerID,
		TargetID:   timeInt64Pointer(targetID),
		Reason:     reason,
		StartedAt:  now,
		LastSeenAt: now,
		CreatedAt:  now,
	}
	r.active[targetID] = episode
	r.appendEpisodeUpdateLocked(episode)
}

func (r *Recorder) resolveEpisodeLocked(episode *DeferralEpisode, now time.Time) {
	if episode.ResolvedAt == nil {
		resolved := now.UTC()
		episode.ResolvedAt = &resolved
		r.appendEpisodeUpdateLocked(episode)
	}
	if episode.TargetID != nil {
		delete(r.active, *episode.TargetID)
	}
}

func (r *Recorder) appendEpisodeUpdateLocked(episode *DeferralEpisode) {
	if len(r.pendingEpisodes) >= maxPendingEpisodes {
		r.droppedEpisodes++
		return
	}
	copy := *episode
	if episode.TargetID != nil {
		targetID := *episode.TargetID
		copy.TargetID = &targetID
	}
	copy.ResolvedAt = utcPointer(episode.ResolvedAt)
	r.pendingEpisodes = append(r.pendingEpisodes, EpisodeUpdate{Episode: copy})
}

func (r *Recorder) shouldSampleLocked(now time.Time) bool {
	if r.sampleEvery == 0 {
		return true
	}
	if !r.nextSampleAt.IsZero() && now.Before(r.nextSampleAt) {
		return false
	}
	return true
}

func (r *Recorder) appendSamplesLocked(pass PassObservation, now time.Time) {
	minute := now.Truncate(time.Minute)
	sampled := 0
	for _, providerPass := range pass.Providers {
		r.seq++
		sample := QueueSample{
			ID:                    fmt.Sprintf("%s-%d", r.bootID, r.seq),
			ProviderID:            providerPass.ProviderID,
			SampledAt:             now,
			MinuteStart:           minute,
			Due:                   providerPass.Due,
			InFlight:              providerPass.InFlight,
			Queued:                providerPass.Queued,
			Overdue:               providerPass.Overdue,
			Deferred:              providerPass.Deferred,
			DeferredByCooldown:    providerPass.DeferredByCooldown,
			DeferredBySpacing:     providerPass.DeferredBySpacing,
			DeferredByConcurrency: providerPass.DeferredByConcurrency,
			OldestPendingAge:      providerPass.OldestPendingAge,
			NextEligibleAt:        utcPointer(providerPass.NextEligibleAt),
			BlockedByConcurrency:  providerPass.BlockedByConcurrency,
			Dispatched:            providerPass.Dispatched,
		}
		if len(r.pendingSamples) >= maxPendingSamples {
			r.droppedSamples++
			continue
		}
		r.pendingSamples = append(r.pendingSamples, sample)
		sampled++
	}
	if sampled > 0 {
		if r.sampleEvery == 0 {
			r.nextSampleAt = now
		} else {
			r.nextSampleAt = now.Add(r.sampleEvery)
		}
	}
}

// Heartbeat advances snapshot freshness without pretending a new queue sample
// was taken. The scheduler calls it on a bounded cadence while idle.
func (r *Recorder) Heartbeat(now time.Time) {
	if r == nil {
		return
	}
	now = now.UTC()
	if now.IsZero() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.snapshot.Stopped {
		return
	}
	r.snapshot.Available = true
	r.snapshot.Running = true
	r.snapshot.SampledAt = timePointerValue(now)
}

// MarkStopped records a graceful scheduler stop. The last queue values remain
// visible but are no longer presented as a running scheduler.
func (r *Recorder) MarkStopped(now time.Time) {
	if r == nil {
		return
	}
	now = now.UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshot.Available = true
	r.snapshot.Running = false
	r.snapshot.Stopped = true
	r.snapshot.Paused = true
	r.snapshot.SampledAt = timePointerValue(now)
}

// Drain returns buffered samples and episode updates and clears the buffers.
func (r *Recorder) Drain() ([]QueueSample, []EpisodeUpdate) {
	if r == nil {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	samples := r.pendingSamples
	episodes := r.pendingEpisodes
	r.pendingSamples = nil
	r.pendingEpisodes = nil
	return samples, episodes
}

// PendingCounts returns buffered sample and episode counts for diagnostics.
func (r *Recorder) PendingCounts() (samples, episodes, droppedSamples, droppedEpisodes int) {
	if r == nil {
		return 0, 0, 0, 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.pendingSamples), len(r.pendingEpisodes), int(r.droppedSamples), int(r.droppedEpisodes)
}

// SampleInterval returns the configured sampling cadence.
func (r *Recorder) SampleInterval() time.Duration {
	if r == nil {
		return DefaultQueueSampleInterval
	}
	return r.sampleEvery
}

// Snapshot returns a copy of the current scheduler snapshot.
func (r *Recorder) Snapshot() SchedulerSnapshot {
	if r == nil {
		return SchedulerSnapshot{Available: false, Providers: map[provider.ID]ProviderQueueSnapshot{}}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return copySnapshot(r.snapshot)
}

func buildSnapshot(pass PassObservation, now time.Time) SchedulerSnapshot {
	snapshot := SchedulerSnapshot{
		Available:          true,
		Running:            true,
		MonitoringEnabled:  pass.MonitoringEnabled,
		Paused:             pass.Paused,
		ConfiguredInterval: pass.ConfiguredInterval,
		ProviderSpacing:    pass.ProviderSpacing,
		Workers:            pass.Workers,
		RunningWorkers:     pass.RunningWorkers,
		LastDispatchAt:     utcPointer(pass.LastDispatchAt),
		SampledAt:          timePointerValue(now),
		LastExecutionAt:    timePointerValue(now),
		LastErrorKind:      pass.LastErrorKind,
		Providers:          make(map[provider.ID]ProviderQueueSnapshot, len(pass.Providers)),
	}
	for _, providerPass := range pass.Providers {
		queue := ProviderQueueSnapshot{
			Due:                   providerPass.Due,
			InFlight:              providerPass.InFlight,
			Queued:                providerPass.Queued,
			Overdue:               providerPass.Overdue,
			Deferred:              providerPass.Deferred,
			DeferredByCooldown:    providerPass.DeferredByCooldown,
			DeferredBySpacing:     providerPass.DeferredBySpacing,
			DeferredByConcurrency: providerPass.DeferredByConcurrency,
			OldestPendingAge:      providerPass.OldestPendingAge,
			NextEligibleAt:        utcPointer(providerPass.NextEligibleAt),
			BlockedByConcurrency:  providerPass.BlockedByConcurrency,
			Dispatched:            providerPass.Dispatched,
		}
		snapshot.Providers[providerPass.ProviderID] = queue
		snapshot.DueTargets += queue.Due
		snapshot.OverdueTargets += queue.Overdue
		snapshot.QueuedTargets += queue.Queued
		snapshot.InFlightTargets += queue.InFlight
		snapshot.DeferredTargets += queue.Deferred
		snapshot.DispatchAttempts += int64(queue.Dispatched)
		if queue.NextEligibleAt != nil &&
			(snapshot.NextEligibleAt == nil || queue.NextEligibleAt.Before(*snapshot.NextEligibleAt)) {
			next := *queue.NextEligibleAt
			snapshot.NextEligibleAt = &next
		}
	}
	return snapshot
}

func copySnapshot(snapshot SchedulerSnapshot) SchedulerSnapshot {
	copy := snapshot
	copy.Providers = make(map[provider.ID]ProviderQueueSnapshot, len(snapshot.Providers))
	for id, queue := range snapshot.Providers {
		queue.NextEligibleAt = utcPointer(queue.NextEligibleAt)
		copy.Providers[id] = queue
	}
	copy.LastExecutionAt = utcPointer(snapshot.LastExecutionAt)
	copy.LastDispatchAt = utcPointer(snapshot.LastDispatchAt)
	copy.NextEligibleAt = utcPointer(snapshot.NextEligibleAt)
	copy.SampledAt = utcPointer(snapshot.SampledAt)
	return copy
}

func utcPointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return timePointerValue(value.UTC())
}

func timePointerValue(value time.Time) *time.Time {
	copy := value.UTC()
	return &copy
}

func timeInt64Pointer(value int64) *int64 {
	copy := value
	return &copy
}

func newIdentity() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
}
