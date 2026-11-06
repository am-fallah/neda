package observability

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/am-fallah/neda/internal/store"
)

// AggregatorConfig controls the background provider-minute rollup.
type AggregatorConfig struct {
	ReadStore  *store.Store
	WriteStore *store.Store
	Recorder   *Recorder
	Logger     *slog.Logger
	Now        func() time.Time
	Interval   time.Duration
}

// Aggregator derives traffic from check_runs and queue metrics from durable
// scheduler samples. It runs outside the scheduler and never blocks dispatch.
type Aggregator struct {
	read     *store.Store
	write    *store.Store
	recorder *Recorder
	logger   *slog.Logger
	now      func() time.Time
	interval time.Duration

	restoreMu sync.Mutex
	restored  bool

	pendingSamples  []QueueSample
	pendingEpisodes []EpisodeUpdate
}

const maxAggregatorPending = 20000

// NewAggregator validates dependencies. A nil read store disables traffic
// aggregation; queue aggregation still works when a recorder is present.
func NewAggregator(cfg AggregatorConfig) *Aggregator {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Interval <= 0 {
		cfg.Interval = time.Minute
	}
	return &Aggregator{
		read:     cfg.ReadStore,
		write:    cfg.WriteStore,
		recorder: cfg.Recorder,
		logger:   cfg.Logger,
		now:      cfg.Now,
		interval: cfg.Interval,
	}
}

// Restore loads active deferral episodes so a restart does not recount a still
// active episode. It is safe to call once during startup and is also invoked by
// Run for embedders that do not call it explicitly.
func (a *Aggregator) Restore(ctx context.Context) error {
	if a == nil || a.write == nil || a.recorder == nil {
		return nil
	}
	a.restoreMu.Lock()
	defer a.restoreMu.Unlock()
	if a.restored {
		return nil
	}
	episodes, err := a.write.RestoreActiveDeferralEpisodes(ctx, a.now().UTC())
	if err != nil {
		return err
	}
	restored := make([]DeferralEpisode, 0, len(episodes))
	for _, episode := range episodes {
		restored = append(restored, DeferralEpisode{
			ID:         episode.ID,
			ProviderID: episode.ProviderID,
			TargetID:   episode.TargetID,
			Reason:     DeferralReason(episode.Reason),
			StartedAt:  episode.StartedAt,
			LastSeenAt: episode.LastSeenAt,
			CreatedAt:  episode.CreatedAt,
		})
	}
	a.recorder.RestoreEpisodes(restored)
	a.restored = true
	return nil
}

// Run executes one rollup per interval until the context is canceled.
func (a *Aggregator) Run(ctx context.Context) error {
	if a == nil || a.write == nil {
		return nil
	}
	if err := a.Restore(ctx); err != nil && ctx.Err() == nil {
		a.logger.Warn("observability deferral restore failed", "code", "deferral_restore_failed")
	}
	a.backfill(ctx)
	timer := time.NewTimer(a.interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			a.runOnce(ctx)
			timer.Reset(a.interval)
		}
	}
}

// backfill rebuilds up to seven days of traffic and queue rollups in bounded
// daily chunks. It is best-effort and runs off the scheduler path.
func (a *Aggregator) backfill(parent context.Context) {
	if a == nil || a.write == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	end := a.now().UTC().Truncate(time.Minute)
	if a.read != nil {
		for day := 7; day >= 1; day-- {
			from := end.Add(-time.Duration(day) * 24 * time.Hour)
			to := from.Add(24 * time.Hour)
			if to.After(end) {
				to = end
			}
			if !to.After(from) {
				continue
			}
			minutes, err := a.read.AggregateProviderTraffic(ctx, from, to)
			if err != nil {
				a.logger.Warn("observability backfill aggregation failed", "code", "backfill_aggregation_failed")
				return
			}
			if err := a.write.UpsertProviderTrafficMinutes(ctx, minutes, end); err != nil {
				a.logger.Warn("observability backfill persistence failed", "code", "backfill_persistence_failed")
				return
			}
			if ctx.Err() != nil {
				return
			}
		}
	}
	if err := a.write.RecomputeProviderQueueMinutes(ctx, end.Add(-QueueSampleRetention), end.Add(time.Minute)); err != nil {
		a.logger.Warn("observability queue backfill failed", "code", "queue_backfill_failed")
	}
}

func (a *Aggregator) runOnce(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	now := a.now().UTC()

	// Recompute a short traffic window so a check that commits just after a
	// minute boundary is included in the next run. The upsert is idempotent.
	if a.read != nil {
		from := now.Add(-5 * time.Minute).Truncate(time.Minute)
		to := now.Truncate(time.Minute).Add(time.Minute)
		if to.After(from) {
			minutes, err := a.read.AggregateProviderTraffic(ctx, from, to)
			if err != nil {
				a.logger.Warn("observability traffic aggregation failed", "code", "traffic_aggregation_failed")
			} else if err := a.write.UpsertProviderTrafficMinutes(ctx, minutes, now); err != nil {
				a.logger.Warn("observability traffic persistence failed", "code", "traffic_persistence_failed")
			}
		}
	}

	if a.recorder != nil {
		samples, episodes := a.recorder.Drain()
		a.pendingSamples = append(a.pendingSamples, samples...)
		a.pendingEpisodes = append(a.pendingEpisodes, episodes...)
		a.trimPending()
		if len(a.pendingSamples) > 0 || len(a.pendingEpisodes) > 0 {
			if err := a.write.PersistObservabilityBatch(ctx, storeSamples(a.pendingSamples), storeEpisodes(a.pendingEpisodes)); err != nil {
				// Keep the batch and retry on the next tick. Source samples are not
				// lost and the eventual recompute is deterministic.
				a.logger.Warn("observability sample persistence failed", "code", "queue_persistence_failed")
			} else {
				a.pendingSamples = nil
				a.pendingEpisodes = nil
			}
		}
	}

	// Recompute a short durable window every tick. This corrects partial
	// failures and incorporates late samples or episode updates.
	from := now.Add(-10 * time.Minute).Truncate(time.Minute)
	to := now.Truncate(time.Minute).Add(time.Minute)
	if err := a.write.RecomputeProviderQueueMinutes(ctx, from, to); err != nil {
		a.logger.Warn("observability queue recompute failed", "code", "queue_recompute_failed")
	}
}

func (a *Aggregator) trimPending() {
	if len(a.pendingSamples) <= maxAggregatorPending && len(a.pendingEpisodes) <= maxAggregatorPending {
		return
	}
	if len(a.pendingSamples) > maxAggregatorPending {
		a.pendingSamples = a.pendingSamples[len(a.pendingSamples)-maxAggregatorPending:]
	}
	if len(a.pendingEpisodes) > maxAggregatorPending {
		a.pendingEpisodes = a.pendingEpisodes[len(a.pendingEpisodes)-maxAggregatorPending:]
	}
	a.logger.Warn("observability pending batch truncated", "code", "observability_pending_truncated")
}

func storeSamples(samples []QueueSample) []store.ProviderQueueSample {
	result := make([]store.ProviderQueueSample, 0, len(samples))
	for _, sample := range samples {
		result = append(result, store.ProviderQueueSample{
			ID:                    sample.ID,
			ProviderID:            sample.ProviderID,
			SampledAt:             sample.SampledAt,
			MinuteStart:           sample.MinuteStart,
			Due:                   int64(sample.Due),
			InFlight:              int64(sample.InFlight),
			Queued:                int64(sample.Queued),
			Overdue:               int64(sample.Overdue),
			Deferred:              int64(sample.Deferred),
			DeferredByCooldown:    int64(sample.DeferredByCooldown),
			DeferredBySpacing:     int64(sample.DeferredBySpacing),
			DeferredByConcurrency: int64(sample.DeferredByConcurrency),
			OldestPendingAgeMS:    sample.OldestPendingAge.Milliseconds(),
			NextEligibleAt:        sample.NextEligibleAt,
			BlockedByConcurrency:  sample.BlockedByConcurrency,
			Dispatched:            int64(sample.Dispatched),
			CreatedAt:             sample.SampledAt,
		})
	}
	return result
}

func storeEpisodes(updates []EpisodeUpdate) []store.ProviderDeferralEpisode {
	result := make([]store.ProviderDeferralEpisode, 0, len(updates))
	for _, update := range updates {
		episode := update.Episode
		result = append(result, store.ProviderDeferralEpisode{
			ID:         episode.ID,
			ProviderID: episode.ProviderID,
			TargetID:   episode.TargetID,
			Reason:     string(episode.Reason),
			StartedAt:  episode.StartedAt,
			LastSeenAt: episode.LastSeenAt,
			ResolvedAt: episode.ResolvedAt,
			CreatedAt:  episode.CreatedAt,
		})
	}
	return result
}
