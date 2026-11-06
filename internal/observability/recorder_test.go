package observability

import (
	"testing"
	"time"
)

func passAt(now, due time.Time, reason DeferralReason) PassObservation {
	return PassObservation{
		Now:                now,
		MonitoringEnabled:  true,
		ConfiguredInterval: 5 * time.Minute,
		ProviderSpacing:    5 * time.Second,
		Workers:            2,
		TargetsComplete:    true,
		Targets: []TargetObservation{{
			TargetID: 1, ProviderID: "p", NextDueAt: timePtr(due),
		}},
		Providers: []ProviderPassObservation{{
			ProviderID: "p", Due: 1, Queued: 1, Deferred: 1,
			Deferrals: []DeferralObservation{{
				TargetID: 1, ProviderID: "p", Due: due, Reason: reason,
			}},
		}},
	}
}

func TestRecorderDeferralEpisodeIdentityIsDueIndependent(t *testing.T) {
	recorder := NewRecorderWithSampleInterval(0)
	now := time.Date(2026, 11, 6, 12, 0, 10, 0, time.UTC)
	due := now.Add(-time.Minute)
	recorder.ObservePass(passAt(now, due, ReasonCooldown))
	recorder.ObservePass(passAt(now.Add(time.Second), due, ReasonCooldown))

	samples, episodes := recorder.Drain()
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want 2", len(samples))
	}
	if len(episodes) != 1 {
		t.Fatalf("episode updates = %d, want one created episode", len(episodes))
	}
	firstID := episodes[0].Episode.ID

	// A changed nextDueAt must not create a new episode.
	recorder.ObservePass(passAt(now.Add(2*time.Second), due.Add(10*time.Minute), ReasonCooldown))
	_, episodes = recorder.Drain()
	if len(episodes) != 0 {
		t.Fatalf("due change created %d episode updates, want 0", len(episodes))
	}

	// A genuinely different reason closes the old episode and opens a new one.
	recorder.ObservePass(passAt(now.Add(3*time.Second), due.Add(20*time.Minute), ReasonSpacing))
	_, episodes = recorder.Drain()
	if len(episodes) != 2 {
		t.Fatalf("reason transition updates = %d, want resolve+create", len(episodes))
	}
	if episodes[0].Episode.ID != firstID || episodes[0].Episode.ResolvedAt == nil {
		t.Fatalf("old episode was not resolved: %+v", episodes[0].Episode)
	}
	secondID := episodes[1].Episode.ID
	if secondID == firstID || episodes[1].Episode.ResolvedAt != nil {
		t.Fatalf("new episode = %+v", episodes[1].Episode)
	}

	// Resolution and later re-entry create a distinct episode.
	recorder.ObservePass(PassObservation{
		Now: now.Add(4 * time.Second), MonitoringEnabled: true,
		ConfiguredInterval: 5 * time.Minute, TargetsComplete: true,
		Targets:   []TargetObservation{{TargetID: 1, ProviderID: "p", NextDueAt: timePtr(due)}},
		Providers: []ProviderPassObservation{{ProviderID: "p"}},
	})
	_, episodes = recorder.Drain()
	if len(episodes) != 1 || episodes[0].Episode.ID != secondID || episodes[0].Episode.ResolvedAt == nil {
		t.Fatalf("resolution updates = %+v", episodes)
	}

	recorder.ObservePass(passAt(now.Add(5*time.Second), due, ReasonSpacing))
	_, episodes = recorder.Drain()
	if len(episodes) != 1 || episodes[0].Episode.ID == secondID || episodes[0].Episode.ResolvedAt != nil {
		t.Fatalf("re-entry updates = %+v", episodes)
	}
}

func TestRecorderDoesNotResolveOnIncompleteTargetList(t *testing.T) {
	recorder := NewRecorderWithSampleInterval(0)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	pass := passAt(now, now.Add(-time.Minute), ReasonSpacing)
	recorder.ObservePass(pass)
	recorder.Drain()

	pass.Now = now.Add(time.Second)
	pass.Targets = nil
	pass.TargetsComplete = false
	pass.Providers = nil
	recorder.ObservePass(pass)
	if _, episodes := recorder.Drain(); len(episodes) != 0 {
		t.Fatalf("incomplete pass resolved episodes: %+v", episodes)
	}

	pass.Now = now.Add(2 * time.Second)
	pass.Targets = []TargetObservation{{TargetID: 1, ProviderID: "p", NextDueAt: timePtr(now)}}
	pass.TargetsComplete = true
	recorder.ObservePass(pass)
	_, episodes := recorder.Drain()
	if len(episodes) != 1 || episodes[0].Episode.ResolvedAt == nil {
		t.Fatalf("complete pass did not resolve episode: %+v", episodes)
	}
}

func TestRecorderSamplesAtBoundedCadenceAndAcrossMinuteBoundary(t *testing.T) {
	recorder := NewRecorderWithSampleInterval(15 * time.Second)
	start := time.Date(2026, 11, 6, 12, 0, 50, 0, time.UTC)
	recorder.ObservePass(passAt(start, start.Add(-time.Minute), ReasonSpacing))
	recorder.ObservePass(passAt(start.Add(5*time.Second), start.Add(-time.Minute), ReasonSpacing))
	recorder.ObservePass(passAt(start.Add(15*time.Second), start.Add(-time.Minute), ReasonSpacing))
	recorder.ObservePass(passAt(start.Add(16*time.Second), start.Add(-time.Minute), ReasonSpacing))

	samples, _ := recorder.Drain()
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want 2 at 15s cadence", len(samples))
	}
	if samples[0].MinuteStart.Minute() != 0 || samples[1].MinuteStart.Minute() != 1 {
		t.Fatalf("minute starts = %v, %v; want 12:00 and 12:01", samples[0].MinuteStart, samples[1].MinuteStart)
	}
}

func TestRecorderSampleIDsAreUniquePerProvider(t *testing.T) {
	recorder := NewRecorderWithSampleInterval(0)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	recorder.ObservePass(PassObservation{
		Now: now, MonitoringEnabled: true, ConfiguredInterval: 5 * time.Minute,
		TargetsComplete: true,
		Targets: []TargetObservation{
			{TargetID: 1, ProviderID: "p1", NextDueAt: timePtr(now)},
			{TargetID: 2, ProviderID: "p2", NextDueAt: timePtr(now)},
		},
		Providers: []ProviderPassObservation{
			{ProviderID: "p1", Due: 1, Queued: 1},
			{ProviderID: "p2", Due: 1, Queued: 1},
		},
	})
	samples, _ := recorder.Drain()
	if len(samples) != 2 || samples[0].ID == samples[1].ID {
		t.Fatalf("samples = %+v, want unique IDs", samples)
	}
}

func TestRecorderSamplesEmptyQueueAndSustainedQueue(t *testing.T) {
	recorder := NewRecorderWithSampleInterval(0)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	recorder.ObservePass(PassObservation{
		Now: now, MonitoringEnabled: true, ConfiguredInterval: 5 * time.Minute,
		TargetsComplete: true,
		Targets:         []TargetObservation{{TargetID: 1, ProviderID: "p", NextDueAt: timePtr(now.Add(time.Minute))}},
		Providers:       []ProviderPassObservation{{ProviderID: "p", Due: 0, Queued: 0}},
	})
	recorder.ObservePass(PassObservation{
		Now: now.Add(time.Second), MonitoringEnabled: true, ConfiguredInterval: 5 * time.Minute,
		TargetsComplete: true,
		Targets:         []TargetObservation{{TargetID: 1, ProviderID: "p", NextDueAt: timePtr(now)}},
		Providers: []ProviderPassObservation{{
			ProviderID: "p", Due: 3, InFlight: 1, Queued: 2, Overdue: 1, Deferred: 2,
			DeferredBySpacing: 2, OldestPendingAge: 45 * time.Second, Dispatched: 1,
		}},
	})
	samples, _ := recorder.Drain()
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want 2", len(samples))
	}
	if samples[0].Due != 0 || samples[0].Queued != 0 {
		t.Fatalf("empty queue sample = %+v", samples[0])
	}
	if samples[1].Due != 3 || samples[1].Queued != 2 || samples[1].Deferred != 2 ||
		samples[1].OldestPendingAge != 45*time.Second || samples[1].Dispatched != 1 {
		t.Fatalf("sustained queue sample = %+v", samples[1])
	}
}

func TestRecorderRestartRestoresActiveEpisode(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	first := NewRecorderWithSampleInterval(0)
	first.ObservePass(passAt(now, now.Add(-time.Minute), ReasonCooldown))
	_, updates := first.Drain()
	if len(updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(updates))
	}

	second := NewRecorderWithSampleInterval(0)
	second.RestoreEpisodes([]DeferralEpisode{updates[0].Episode})
	second.ObservePass(passAt(now.Add(time.Minute), now.Add(-2*time.Minute), ReasonCooldown))
	_, updates = second.Drain()
	if len(updates) != 0 {
		t.Fatalf("restored active episode was recounted: %+v", updates)
	}
}

func TestRecorderPausedAndStoppedSemantics(t *testing.T) {
	recorder := NewRecorderWithSampleInterval(0)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	recorder.ObservePass(PassObservation{
		Now: now, MonitoringEnabled: false, Paused: true,
		ConfiguredInterval: 5 * time.Minute, TargetsComplete: true,
		Providers: []ProviderPassObservation{{ProviderID: "p"}},
	})
	samples, _ := recorder.Drain()
	if len(samples) != 0 {
		t.Fatalf("paused scheduler recorded %d samples", len(samples))
	}
	snapshot := recorder.Snapshot()
	if !snapshot.Available || !snapshot.Running || !snapshot.Paused || snapshot.MonitoringEnabled {
		t.Fatalf("paused snapshot = %+v", snapshot)
	}

	recorder.MarkStopped(now.Add(time.Minute))
	snapshot = recorder.Snapshot()
	if snapshot.Running || !snapshot.Stopped || !snapshot.Paused {
		t.Fatalf("stopped snapshot = %+v", snapshot)
	}
	recorder.Heartbeat(now.Add(2 * time.Minute))
	snapshot = recorder.Snapshot()
	if snapshot.Running || !snapshot.Stopped {
		t.Fatalf("heartbeat resurrected stopped scheduler: %+v", snapshot)
	}
}

func TestRecorderDropsSamplesAtBoundedCapacity(t *testing.T) {
	recorder := NewRecorderWithSampleInterval(0)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	pass := PassObservation{
		Now: now, MonitoringEnabled: true, ConfiguredInterval: 5 * time.Minute,
		TargetsComplete: true,
		Targets:         []TargetObservation{{TargetID: 1, ProviderID: "p", NextDueAt: timePtr(now)}},
		Providers:       []ProviderPassObservation{{ProviderID: "p"}},
	}
	for index := 0; index < maxPendingSamples+10; index++ {
		pass.Now = now.Add(time.Duration(index) * time.Millisecond)
		recorder.ObservePass(pass)
	}
	samples, _, droppedSamples, _ := recorder.PendingCounts()
	if samples != maxPendingSamples || droppedSamples != 10 {
		t.Fatalf("samples=%d dropped=%d, want %d/10", samples, droppedSamples, maxPendingSamples)
	}
}

func TestRecorderSnapshotCopiesPointers(t *testing.T) {
	recorder := NewRecorderWithSampleInterval(0)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	next := now.Add(time.Minute)
	recorder.ObservePass(PassObservation{
		Now: now, MonitoringEnabled: true, ConfiguredInterval: 5 * time.Minute,
		TargetsComplete: true,
		Targets:         []TargetObservation{{TargetID: 1, ProviderID: "p", NextDueAt: timePtr(next)}},
		Providers: []ProviderPassObservation{{
			ProviderID: "p", Due: 1, NextEligibleAt: &next,
		}},
	})
	snapshot := recorder.Snapshot()
	*snapshot.Providers["p"].NextEligibleAt = now
	if recorder.Snapshot().Providers["p"].NextEligibleAt.Equal(now) {
		t.Fatal("snapshot pointers alias recorder state")
	}
}
