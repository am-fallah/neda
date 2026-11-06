package scheduler

import (
	"testing"
	"time"

	"github.com/am-fallah/neda/internal/domain"
	"github.com/am-fallah/neda/internal/store"
)

type recordingRateLimitSink struct {
	events []store.ProviderRateLimitEvent
}

func (s *recordingRateLimitSink) RecordRateLimitEvent(event store.ProviderRateLimitEvent) {
	s.events = append(s.events, event)
}

func TestRateLimitEventSemantics(t *testing.T) {
	sink := &recordingRateLimitSink{}
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	runner := &OutcomeRunner{
		rateLimitEvents: sink,
		now:             func() time.Time { return now },
	}
	attempt := CheckAttempt{ProviderID: "p", CheckRunID: 42}
	state := domain.ProviderRateLimit{
		ProviderID: "p", Scope: domain.ProviderRateLimitScopeProvider,
		CooldownUntil: timePointerValue(now.Add(30 * time.Minute)),
	}
	previousCooldown := now.Add(10 * time.Minute)

	// A repeated 429 at the same level is rate_limited + maintained, not an
	// escalation.
	runner.emitRateLimit429Event(attempt, RateLimitChange{
		Maintained: true, PreviousLevel: 2, Level: 2,
		PreviousInterval: 20 * time.Minute, NewInterval: 20 * time.Minute,
		PreviousCooldownUntil: &previousCooldown,
		CooldownUntil:         now.Add(30 * time.Minute),
		CooldownExtended:      true,
	}, state)
	if len(sink.events) != 2 {
		t.Fatalf("events = %d, want rate_limited + cooldown_extended", len(sink.events))
	}
	if sink.events[0].Kind != store.RateLimitEventRateLimited ||
		sink.events[0].PreviousLevel != 2 || sink.events[0].EscalationLevel != 2 {
		t.Fatalf("rate_limited event = %+v", sink.events[0])
	}
	if sink.events[1].Kind != store.RateLimitEventCooldownExtended ||
		sink.events[1].PreviousCooldownUntil == nil {
		t.Fatalf("cooldown_extended event = %+v", sink.events[1])
	}

	sink.events = nil
	runner.emitRateLimit429Event(attempt, RateLimitChange{
		Escalated: true, PreviousLevel: 1, Level: 2,
		PreviousInterval: 10 * time.Minute, NewInterval: 20 * time.Minute,
	}, state)
	if len(sink.events) != 1 || sink.events[0].Kind != store.RateLimitEventRateLimited ||
		sink.events[0].PreviousLevel != 1 || sink.events[0].EscalationLevel != 2 {
		t.Fatalf("escalation events = %+v", sink.events)
	}

	sink.events = nil
	runner.emitRateLimitSuccessEvent(attempt, RateLimitChange{
		PreviousLevel: 2, Level: 2, RecoveryProgress: true,
	}, state)
	if len(sink.events) != 1 || sink.events[0].Kind != store.RateLimitEventRecoveryProgress {
		t.Fatalf("progress events = %+v", sink.events)
	}

	sink.events = nil
	runner.emitRateLimitSuccessEvent(attempt, RateLimitChange{
		PreviousLevel: 2, Level: 1, Recovered: true,
	}, state)
	if len(sink.events) != 1 || sink.events[0].Kind != store.RateLimitEventRecoveryStep {
		t.Fatalf("recovery step events = %+v", sink.events)
	}

	sink.events = nil
	runner.emitRateLimitSuccessEvent(attempt, RateLimitChange{
		PreviousLevel: 1, Level: 0, Recovered: true, FullyRecovered: true,
	}, state)
	if len(sink.events) != 1 || sink.events[0].Kind != store.RateLimitEventRecovered {
		t.Fatalf("recovered events = %+v", sink.events)
	}
}
